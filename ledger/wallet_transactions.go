package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// RefundInput describes a full refund (Wallet().Refund).
type RefundInput struct {
	Reason      string
	Description string
	MetaData    map[string]any
}

// Refund reverses a settled (or confirmed/cancelled pending) transaction once.
// For split transactions created through the queue, pass the queued parent id.
// Blnk reverses the whole amount, so a transaction that already has partial
// refunds (RefundPartial) is refused; refund the rest with RefundPartial.
func (s walletAPI) Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error) {
	const op = "ledger.Wallet.Refund"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	if orig, err := s.c.GetTransaction(ctx, txID); err == nil {
		done, err := s.refundedSoFar(ctx, orig, "")
		if err != nil {
			return nil, mapErr(op, err)
		}
		if done.Sign() > 0 {
			return nil, &OpError{Op: op, Kind: ErrAlreadyFinalized, Cause: fmt.Errorf(
				"transaction %s already has partial refunds (%s); refund the rest with RefundPartial", txID, s.money(done, s.txPrecision(orig), orig.Currency))}
		}
	} else if !blnk.IsNotFound(err) {
		return nil, mapErr(op, err)
	}
	// Blnk reverses it directly, so check the accounts here like any other
	// movement: money leaves the original's recipients and returns to its payer.
	if t, err := s.byID(ctx, txID); err == nil {
		from, to := refundSides(t)
		if err := s.checkActive(ctx, op, from, to...); err != nil {
			return nil, err
		}
	} else if !blnk.IsNotFound(err) {
		return nil, mapErr(op, err)
	}
	md := blnk.MetaData{}
	for k, v := range in.MetaData {
		md[k] = v
	}
	if in.Reason != "" {
		md["refund_reason"] = in.Reason
	}
	t, err := s.c.Refund(ctx, txID, &blnk.RefundRequest{
		SkipQueue:   !s.cfg.UseQueue,
		Description: in.Description,
		MetaData:    nilIfEmpty(md),
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toReceipt(t), nil
}

// refundSides returns the accounts a reversal of t takes money from (t's
// recipients) and gives it back to (t's payers).
func refundSides(t *blnk.Transaction) (from, to []AccountID) {
	add := func(out []AccountID, one string, many []blnk.Distribution) []AccountID {
		if one != "" {
			out = append(out, AccountID(one))
		}
		for _, d := range many {
			out = append(out, AccountID(d.Identifier))
		}
		return out
	}
	return add(nil, t.Destination, t.Destinations), add(nil, t.Source, t.Sources)
}

// GetTransaction returns a transaction by its Blnk id.
func (s walletAPI) GetTransaction(ctx context.Context, txID string) (*Receipt, error) {
	t, err := s.byID(ctx, txID)
	if err != nil {
		return nil, mapErr("ledger.Wallet.GetTransaction", err)
	}
	return s.toReceipt(t), nil
}

// GetTransactionByReference tries the reference as given, then with Blnk's "_q"
// suffix used for records produced by the queue.
func (s walletAPI) GetTransactionByReference(ctx context.Context, reference string) (*Receipt, error) {
	const op = "ledger.Wallet.GetTransactionByReference"
	t, err := s.byReference(ctx, reference)
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toReceipt(t), nil
}

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
func (s walletAPI) RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error) {
	const op = "ledger.Wallet.RefundPartial"
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
	from := s.indicatorOf(ctx, orig.Destination, names) // "@Name" for house accounts
	r, err := s.move(ctx, op, TransferInput{
		From: AccountID(from), To: AccountID(s.indicatorOf(ctx, orig.Source, names)), Amount: amt, Reference: in.Reference,
		AllowOverdraft: strings.HasPrefix(from, "@"), // reversing out of a house account
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
