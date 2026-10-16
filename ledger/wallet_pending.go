package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

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
func (s walletAPI) ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error) {
	const op = "ledger.Wallet.ConfirmMany"
	if len(items) == 0 || len(items) > maxInflightBatch {
		return nil, invalid(op, "between 1 and %d transactions per call", maxInflightBatch)
	}
	// An item whose receiver is frozen or closed fails on its own; the rest go
	// to Blnk. Results come back in the order of items.
	out := make([]BatchResult, len(items))
	req := blnk.BulkCommitInflightRequest{SkipQueue: !s.cfg.UseQueue}
	var sent []int
	for i, it := range items {
		if it.TransactionID == "" {
			return nil, invalid(op, "item %d: transaction id is required", i)
		}
		if err := s.checkReceivers(ctx, op, it.TransactionID); err != nil {
			if !refusal(err) {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			out[i] = BatchResult{TransactionID: it.TransactionID, Err: err}
			continue
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
		sent = append(sent, i)
	}
	if len(sent) == 0 {
		return out, nil
	}
	resp, err := s.c.BulkCommitInflight(ctx, req)
	if err != nil {
		return nil, mapErr(op, err)
	}
	got := batchResults(op, resp)
	byID := make(map[string]BatchResult, len(got))
	for _, r := range got {
		byID[r.TransactionID] = r
	}
	for _, i := range sent {
		if r, ok := byID[items[i].TransactionID]; ok {
			out[i] = r
		} else {
			out[i] = BatchResult{TransactionID: items[i].TransactionID, Err: &OpError{Op: op, Kind: ErrUnavailable,
				Cause: fmt.Errorf("no result from Blnk for %s", items[i].TransactionID)}}
		}
	}
	return out, nil
}

// CancelMany cancels up to 100 independent pending transactions in one call.
func (s walletAPI) CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error) {
	const op = "ledger.Wallet.CancelMany"
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
	held, err := s.byID(ctx, txID)
	if err != nil {
		return Money{}, mapErr(op, err)
	}
	if held.Destination == "" && len(held.Destinations) > 1 {
		// Blnk applies a partial amount to each leg in turn and stops at the
		// first leg smaller than it, leaving the earlier legs committed.
		return Money{}, invalid(op, "transaction %s has several legs (e.g. a fee); confirm it in full, or confirm one leg by its transaction id", txID)
	}
	if partial.Currency != "" && !strings.EqualFold(partial.Currency, held.Currency) {
		return Money{}, invalid(op, "partial amount currency %s differs from held currency %s", partial.Currency, held.Currency)
	}
	amt, err := s.resolveAt(op, partial, held.Currency, s.txPrecision(held))
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

// HoldInput reserves money on an account (Wallet().Hold).
type HoldInput struct {
	AccountID   AccountID // customer account the money is held on
	Destination AccountID // where captured funds go: a merchant account, or e.g. AccountID("@CardSettlement")
	Amount      Money
	Reference   string
	ExpiresAt   time.Time // optional auto-release
	CaptureAt   time.Time // optional auto-capture (requires Blnk inflight commit queue)
	Description string
	MetaData    map[string]any
}

// Hold reserves money on an account without moving it, like a card
// authorisation. Wallet().Confirm captures all or part of it; Cancel releases
// it. ExpiresAt releases it automatically, CaptureAt captures it.
func (s walletAPI) Hold(ctx context.Context, in HoldInput) (*Receipt, error) {
	const op = "ledger.Wallet.Hold"
	if err := customerOnly(op, in.AccountID); err != nil {
		return nil, err
	}
	return s.hold(ctx, op, in, "hold")
}

func (s *Service) hold(ctx context.Context, op string, in HoldInput, kind string) (*Receipt, error) {
	amount, _, err := s.prepare(op, in.AccountID, in.Amount, Money{}, in.Reference)
	if err != nil {
		return nil, err
	}
	if in.Destination == "" {
		return nil, invalid(op, "destination is required")
	}
	if !in.CaptureAt.IsZero() && !in.ExpiresAt.IsZero() && !in.CaptureAt.Before(in.ExpiresAt) {
		return nil, invalid(op, "CaptureAt must be before ExpiresAt or the hold is released first")
	}
	// A dispute hold points back at the payer, who may be frozen for the very
	// reason the payment is disputed; ring-fencing the money must still work.
	var to []AccountID
	if kind != "dispute" {
		to = append(to, in.Destination)
	}
	if err := s.checkActive(ctx, op, []AccountID{in.AccountID}, to...); err != nil {
		return nil, err
	}
	if limited(kind) {
		if err := s.checkLimits(ctx, op, in.AccountID, amount.PreciseAmount); err != nil {
			return nil, err
		}
	}
	req := s.baseReq(amount, in.Reference, in.Description, in.MetaData, kind)
	req.Source = string(in.AccountID)
	req.Destination = string(in.Destination)
	req.Inflight = true
	if !in.ExpiresAt.IsZero() {
		req.InflightExpiryDate = in.ExpiresAt.Format(time.RFC3339)
	}
	if !in.CaptureAt.IsZero() {
		req.InflightCommitDate = in.CaptureAt.Format(time.RFC3339)
	}
	return s.post(ctx, op, req)
}

// Confirm settles an INFLIGHT transaction. With partial set, only that amount is
// settled and the rest stays held (call Confirm again or Cancel the remainder).
// A transaction with several legs (one with a fee) can only be confirmed in
// full, or one leg at a time by the leg's transaction id.
func (s walletAPI) Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error) {
	const op = "ledger.Wallet.Confirm"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	if err := s.checkReceivers(ctx, op, txID); err != nil {
		return nil, err
	}
	req := blnk.UpdateInflightRequest{Status: blnk.ActionCommit, SkipQueue: !s.cfg.UseQueue}
	if partial != nil && partial.isSet() {
		amt, err := s.partialAmount(ctx, op, txID, *partial)
		if err != nil {
			return nil, err
		}
		req.PreciseAmount = blnk.BigFrom(amt.PreciseAmount)
	}
	resp, err := s.c.UpdateInflight(ctx, txID, req)
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.inflightReceipt(txID, resp), nil
}

// checkReceivers refuses to settle a pending transaction into a customer
// account that has been frozen or closed since it was created. Money already
// on its way out (a withdrawal the provider has paid) is not held back: only
// the receiving accounts are checked. A transaction Blnk doesn't know is left
// for the commit itself to report.
func (s *Service) checkReceivers(ctx context.Context, op, txID string) error {
	held, err := s.byID(ctx, txID)
	if blnk.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return mapErr(op, err)
	}
	if kind, _ := held.MetaData["txn_type"].(string); kind == "dispute" {
		return nil // a dispute's outcome must post even to a frozen payer
	}
	var to []AccountID
	if held.Destination != "" {
		to = append(to, AccountID(held.Destination))
	}
	for _, d := range held.Destinations {
		if d.Identifier != "" {
			to = append(to, AccountID(d.Identifier))
		}
	}
	return s.checkActive(ctx, op, nil, to...)
}

// Cancel releases whatever is still held on an INFLIGHT transaction.
func (s walletAPI) Cancel(ctx context.Context, txID string) (*Receipt, error) {
	const op = "ledger.Wallet.Cancel"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	resp, err := s.c.UpdateInflight(ctx, txID, blnk.UpdateInflightRequest{
		Status: blnk.ActionVoid, SkipQueue: !s.cfg.UseQueue,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.inflightReceipt(txID, resp), nil
}
