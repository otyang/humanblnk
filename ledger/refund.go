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
// linked to the original with meta_data.refund_of. Refunds beyond the original
// amount (counting the partial refunds linked to it) are rejected, as is a
// transaction already reversed with Refund; the original's
// meta_data.refunded_precise records the total refunded. Use Refund for
// Blnk's one-shot full reversal.
//
// Two partial refunds of the same transaction racing each other can both pass
// the remaining-amount check; serialise refunds per transaction in your app
// (e.g. one worker or a row lock) if they can happen concurrently.
func (s transactionsAPI) RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error) {
	const op = "ledger.Transactions.RefundPartial"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	orig, err := s.byID(ctx, txID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if orig.Status != blnk.StatusApplied {
		return nil, invalid(op, "only posted transactions can be refunded (status %s)", orig.Status)
	}
	if orig, err = s.payeeLeg(ctx, op, orig); err != nil {
		return nil, err
	}
	txID = orig.GetID() // the leg, when the original had a fee
	amt, err := s.resolveInAt(op, in.Amount, orig.Currency, s.txPrecision(orig))
	if err != nil {
		return nil, err
	}
	if _, err := s.c.GetTransactionByReference(ctx, txID+"_refund"); err == nil {
		return nil, &OpError{Op: op, Kind: ErrAlreadyFinalized, Cause: fmt.Errorf("transaction %s was refunded in full", txID)}
	} else if !blnk.IsNotFound(err) {
		return nil, mapErr(op, err)
	}
	refunded, err := s.refundedSoFar(ctx, orig, in.Reference)
	if err != nil {
		return nil, mapErr(op, err)
	}
	left := new(big.Int).Sub(&orig.PreciseAmount.Int, refunded)
	if amt.PreciseAmount.Cmp(left) > 0 {
		return nil, &OpError{Op: op, Kind: ErrAmountExceeded,
			Cause: fmt.Errorf("%s exceeds the %s not yet refunded", amt, s.money(left, amt.Precision, amt.Currency))}
	}

	md := map[string]any{"refund_of": txID, "refund_reason": in.Reason}
	for k, v := range in.MetaData {
		md[k] = v
	}
	names := map[string]string{}
	from := s.indicatorOf(ctx, orig.Destination, names) // "@Name" for system accounts
	r, err := s.move(ctx, op, TransferInput{
		From: AccountID(from), To: AccountID(s.indicatorOf(ctx, orig.Source, names)), Amount: amt, Reference: in.Reference,
		AllowOverdraft: strings.HasPrefix(from, "@"), // reversing out of a system account
		Description:    firstNonEmpty(in.Description, "Refund of "+orig.Reference), MetaData: md,
	}, "refund")
	if err != nil {
		return nil, err
	}
	// Written on replays too, so a total lost to an earlier failure is
	// recorded when the refund is retried.
	total := new(big.Int).Add(refunded, amt.PreciseAmount)
	if _, err := s.c.UpdateMetadata(ctx, txID, blnk.MetaData{metaRefunded: total.String()}); err != nil {
		return r, mapErr(op, fmt.Errorf("refund %s posted but refunded total not recorded: %w", r.TransactionID, err))
	}
	return r, nil
}

// refundedSoFar sums the partial refunds linked to orig by
// meta_data.refund_of, leaving out the one with reference except (so retrying
// it replays rather than counting itself). Summing the refunds themselves,
// rather than trusting the running total on orig, also covers a refund whose
// total was never recorded.
func (s *Service) refundedSoFar(ctx context.Context, orig *blnk.Transaction, except string) (*big.Int, error) {
	id := orig.GetID()
	linked := new(big.Int)
	for offset := 0; ; offset += 100 {
		page, _, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{{Field: metaField("refund_of"), Operator: "eq", Value: id}},
			SortBy:  "created_at", SortOrder: "asc", Limit: 100, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for i := range page {
			t := &page[i]
			switch {
			case t.Reference == except || t.Reference == except+"_q":
			case t.Status == blnk.StatusVoid || t.Status == blnk.StatusRejected:
			case t.QueuedParent() != "" && t.QueuedParent() != t.GetID(): // the queue's copy of a counted refund
			default:
				linked.Add(linked, &t.PreciseAmount.Int)
			}
		}
		if len(page) < 100 {
			break
		}
	}
	return linked, nil
}
