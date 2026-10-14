package ledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// ConfirmItem is one pending transaction to confirm, optionally partially.
type ConfirmItem struct {
	TransactionID string
	Partial       *Money
}

// BatchResult is the outcome for one transaction in ConfirmMany / CancelMany.
type BatchResult struct {
	TransactionID string
	OK            bool
	Queued        bool
	Err           error // a ledger sentinel (errors.Is) when OK is false
}

// maxInflightBatch is Blnk's limit for bulk inflight commit/void.
const maxInflightBatch = 100

// ConfirmMany confirms up to 100 independent pending transactions in one call.
// Each succeeds or fails on its own; check every BatchResult.
func (s *Service) ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error) {
	const op = "ledger.ConfirmMany"
	if len(items) == 0 || len(items) > maxInflightBatch {
		return nil, invalid(op, "between 1 and %d transactions per call", maxInflightBatch)
	}
	req := blnk.BulkCommitInflightRequest{SkipQueue: !s.cfg.UseQueue}
	for i, it := range items {
		if it.TransactionID == "" {
			return nil, invalid(op, "item %d: transaction id is required", i)
		}
		ci := blnk.BulkCommitItem{TransactionID: it.TransactionID}
		if it.Partial != nil && it.Partial.isSet() {
			amt, err := s.partialAmount(ctx, op, it.TransactionID, *it.Partial)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			ci.PreciseAmount = blnk.BigFrom(amt.PreciseAmount)
		}
		req.Transactions = append(req.Transactions, ci)
	}
	resp, err := s.c.BulkCommitInflight(ctx, req)
	if err != nil {
		return nil, mapErr(op, err)
	}
	return batchResults(op, resp), nil
}

// CancelMany cancels up to 100 independent pending transactions in one call.
func (s *Service) CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error) {
	const op = "ledger.CancelMany"
	if len(txIDs) == 0 || len(txIDs) > maxInflightBatch {
		return nil, invalid(op, "between 1 and %d transactions per call", maxInflightBatch)
	}
	resp, err := s.c.BulkVoidInflight(ctx, blnk.BulkVoidInflightRequest{
		SkipQueue: !s.cfg.UseQueue, TransactionIDs: txIDs,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return batchResults(op, resp), nil
}

func batchResults(op string, resp *blnk.BulkInflightResponse) []BatchResult {
	out := make([]BatchResult, len(resp.Results))
	for i, r := range resp.Results {
		out[i] = BatchResult{TransactionID: r.TransactionID, OK: r.Status != "failed", Queued: r.Status == "queued"}
		if !out[i].OK {
			out[i].Err = mapErr(op, &blnk.APIError{Status: 400, Code: r.Code, Message: r.Message})
		}
	}
	return out
}

// partialAmount resolves a partial confirm against the held transaction's own
// precision and currency, so a typed "0.1" means what it meant when the
// transaction was created, and rejects more than was held.
func (s *Service) partialAmount(ctx context.Context, op, txID string, partial Money) (Money, error) {
	held, err := s.c.GetTransaction(ctx, txID)
	if err != nil {
		return Money{}, mapErr(op, err)
	}
	if partial.Currency != "" && !strings.EqualFold(partial.Currency, held.Currency) {
		return Money{}, invalid(op, "partial amount currency %s differs from held currency %s", partial.Currency, held.Currency)
	}
	if partial.Precision == 0 {
		partial.Precision = s.txPrecision(held)
	}
	amt, err := s.resolve(op, partial, held.Currency)
	if err != nil {
		return Money{}, err
	}
	if !amt.IsPositive() {
		return Money{}, invalid(op, "partial amount must be positive")
	}
	if amt.PreciseAmount.Cmp(&held.PreciseAmount.Int) > 0 {
		return Money{}, &OpError{Op: op, Kind: ErrAmountExceeded,
			Cause: fmt.Errorf("%s exceeds held %s", amt, s.txMoney(held))}
	}
	return amt, nil
}
