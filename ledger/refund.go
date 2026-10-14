package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// PartialRefundInput refunds part of a posted transaction.
type PartialRefundInput struct {
	Amount      Money
	Reference   string // REQUIRED, one per partial refund
	Reason      string
	Description string
	MetaData    map[string]any
}

// metaRefunded is the running total (smallest unit) refunded on an original.
const metaRefunded = "refunded_precise"

// RefundPartial sends part of a posted transaction back: destination → source,
// linked to the original with meta_data.refund_of. The original's
// meta_data.refunded_precise tracks the total refunded, and refunds beyond the
// original amount are rejected. Use Refund for Blnk's one-shot full reversal.
//
// Two partial refunds of the same transaction racing each other can both pass
// the remaining-amount check; serialise refunds per transaction in your app
// (e.g. one worker or a row lock) if they can happen concurrently.
func (s *Service) RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error) {
	const op = "ledger.RefundPartial"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	orig, err := s.c.GetTransaction(ctx, txID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if orig.Status != blnk.StatusApplied {
		return nil, invalid(op, "only posted transactions can be refunded (status %s)", orig.Status)
	}
	if orig.Destination == "" {
		return nil, invalid(op, "transaction %s has several destinations; refund the leg's transaction id", txID)
	}
	amount := in.Amount
	if amount.Precision == 0 {
		amount.Precision = s.txPrecision(orig)
	}
	amt, err := s.resolveIn(op, amount, orig.Currency)
	if err != nil {
		return nil, err
	}
	refunded := new(big.Int)
	if v, ok := new(big.Int).SetString(str(orig.MetaData[metaRefunded]), 10); ok {
		refunded = v
	}
	left := new(big.Int).Sub(&orig.PreciseAmount.Int, refunded)
	if amt.PreciseAmount.Cmp(left) > 0 {
		return nil, &OpError{Op: op, Kind: ErrAmountExceeded,
			Cause: fmt.Errorf("%s exceeds the %s not yet refunded", amt, Precise(left, amt.Precision, amt.Currency))}
	}

	md := map[string]any{"refund_of": txID, "refund_reason": in.Reason}
	for k, v := range in.MetaData {
		md[k] = v
	}
	r, err := s.move(ctx, op, TransferInput{
		From: orig.Destination, To: orig.Source, Amount: amt, Reference: in.Reference,
		AllowOverdraft: strings.HasPrefix(orig.Destination, "@"), // reversing out of a system account
		Description:    firstNonEmpty(in.Description, "Refund of "+orig.Reference), MetaData: md,
	}, "refund")
	if err != nil {
		return nil, err
	}
	if !r.Replayed {
		total := new(big.Int).Add(refunded, amt.PreciseAmount)
		if _, err := s.c.UpdateMetadata(ctx, txID, blnk.MetaData{metaRefunded: total.String()}); err != nil {
			return r, mapErr(op, fmt.Errorf("refund %s posted but refunded total not recorded: %w", r.TransactionID, err))
		}
	}
	return r, nil
}
