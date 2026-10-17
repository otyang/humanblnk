package ledger

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// RefundInput describes a full refund (Wallet().Refund).
type RefundInput struct {
	Reason      string
	Description string
	MetaData    map[string]any
}

// Refund reverses a posted (APPLIED) transaction once. For a confirmed hold,
// pass the transaction id Confirm returned, not the hold's.
// For split transactions created through the queue, pass the queued parent id.
// Blnk reverses the whole amount, so a transaction that already has partial
// refunds (RefundPartial), or a dispute still open or upheld, is refused;
// refund the rest with RefundPartial.
func (s walletAPI) Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error) {
	const op = "ledger.Wallet.Refund"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	// A payment with a fee, or a split, is stored by Blnk only as its legs:
	// reverse each leg. Anything Blnk stored as one transaction (including a
	// queued split's parent) is reversed in one call.
	t, err := s.c.GetTransaction(ctx, txID)
	stored := err == nil
	if blnk.IsNotFound(err) {
		t, err = s.byID(ctx, txID)
	}
	if err != nil {
		return nil, mapErr(op, err)
	}
	var legs []refundLeg
	if stored {
		legs = []refundLeg{{id: txID, tx: t, owed: owedTo(t)}}
	} else {
		for _, d := range t.Destinations {
			amt, ok := new(big.Int).SetString(d.PreciseDistribution, 10)
			if !ok || d.TransactionID == "" {
				return nil, &OpError{Op: op, Kind: ErrUnavailable, Cause: fmt.Errorf("transaction %s: leg %q can't be read", txID, d.Identifier)}
			}
			legs = append(legs, refundLeg{id: d.TransactionID, owed: map[AccountID]*big.Int{AccountID(d.Identifier): amt}})
		}
	}

	// Check everything before reversing anything. A leg already reversed (by
	// an earlier Refund, even one whose answer was lost) is skipped.
	var todo []refundLeg
	for _, l := range legs {
		if l.tx == nil {
			if l.tx, err = s.c.GetTransaction(ctx, l.id); err != nil {
				return nil, mapErr(op, err)
			}
		}
		if l.tx.Status != blnk.StatusApplied {
			hint := ""
			if l.tx.Status == blnk.StatusInflight {
				hint = "; to refund a confirmed hold, pass the transaction id Confirm returned"
			}
			return nil, invalid(op, "only posted transactions can be refunded (%s is %s%s)", l.id, l.tx.Status, hint)
		}
		reversed, err := s.reversed(ctx, l.tx)
		if err != nil {
			return nil, mapErr(op, err)
		}
		if reversed {
			continue
		}
		c, err := s.claimsOn(ctx, l.tx, "")
		if err != nil {
			return nil, mapErr(op, err)
		}
		if done := c.total(&l.tx.PreciseAmount.Int); done.Sign() > 0 {
			return nil, &OpError{Op: op, Kind: ErrAlreadyFinalized, Cause: fmt.Errorf(
				"transaction %s already has partial refunds or disputes (%s); refund the rest with RefundPartial", l.id, s.money(done, s.txPrecision(l.tx), l.tx.Currency))}
		}
		todo = append(todo, l)
	}
	if len(todo) == 0 {
		return nil, &OpError{Op: op, Kind: ErrAlreadyFinalized, Cause: fmt.Errorf("transaction %s has already been refunded", txID)}
	}
	// Largest first, so the payee's leg comes before the fee and the order is
	// the same on every run.
	sort.SliceStable(todo, func(i, j int) bool {
		if c := todo[i].total().Cmp(todo[j].total()); c != 0 {
			return c > 0
		}
		return todo[i].id < todo[j].id
	})
	// Money leaves the original's recipients and returns to its payer.
	from, to := refundSides(t)
	if err := s.checkActive(ctx, op, from, to...); err != nil {
		return nil, err
	}
	// Blnk reverses with overdraft allowed, so make sure no customer account
	// is taken below zero: each must still hold what it received. This is
	// checked just before reversing; a payment the recipient makes at the
	// same moment can still get in first.
	owed := map[AccountID]*big.Int{}
	for _, l := range todo {
		for id, amt := range l.owed {
			if id == "" || id.isIndicator() {
				continue
			}
			if owed[id] == nil {
				owed[id] = new(big.Int)
			}
			owed[id].Add(owed[id], amt)
		}
	}
	for id, amt := range owed {
		b, err := s.c.GetBalance(ctx, string(id))
		if err != nil {
			return nil, mapErr(op, err)
		}
		if strings.HasPrefix(b.Indicator, "@") {
			continue // a house account given by its balance id
		}
		if b.Available().Cmp(amt) < 0 {
			return nil, &OpError{Op: op, Kind: ErrInsufficientFunds, Cause: fmt.Errorf(
				"account %s has %s %s available but must return %s", id, s.money(b.Available(), s.precisionOf(b), b.Currency).Display,
				strings.ToUpper(b.Currency), s.money(amt, s.precisionOf(b), b.Currency).Display)}
		}
	}

	md := blnk.MetaData{}
	for k, v := range in.MetaData {
		md[k] = v
	}
	if in.Reason != "" {
		md["refund_reason"] = in.Reason
	}
	md["txn_type"] = "refund" // so limit usage doesn't count the reversal
	var receipts []*Receipt
	for _, l := range todo {
		r, err := s.c.Refund(ctx, l.id, &blnk.RefundRequest{
			SkipQueue:   !s.cfg.UseQueue,
			Description: in.Description,
			MetaData:    md,
		})
		if err != nil {
			e := mapErr(op, err)
			if errors.Is(e, ErrAlreadyFinalized) {
				_, _ = s.c.UpdateMetadata(ctx, l.id, blnk.MetaData{metaRefundedBy: "unknown"})
				continue // reversed meanwhile: carry on with the rest
			}
			if len(receipts) > 0 {
				return nil, fmt.Errorf("%w (legs reversed so far: %d of %d; call Refund again to finish)", e, len(receipts), len(todo))
			}
			return nil, e
		}
		// Mark the leg so a retry skips it without asking Blnk again.
		_, _ = s.c.UpdateMetadata(ctx, l.id, blnk.MetaData{metaRefundedBy: r.GetID()})
		receipts = append(receipts, s.toReceipt(r))
	}
	if len(receipts) == 0 {
		return nil, &OpError{Op: op, Kind: ErrAlreadyFinalized, Cause: fmt.Errorf("transaction %s has already been refunded", txID)}
	}
	out := receipts[0]
	if len(legs) > 1 {
		// The receipt is the first reversal's, with the total refunded by
		// this call as its Amount and every reversal listed in Legs.
		total := new(big.Int)
		for _, r := range receipts {
			total.Add(total, r.Amount.PreciseAmount)
			out.Legs = append(out.Legs, Leg{Account: r.Source, Amount: r.Amount, TransactionID: r.TransactionID, Narration: r.Description})
		}
		out.Amount = s.money(total, out.Amount.Precision, out.Amount.Currency)
	}
	return out, nil
}

// reversed reports whether Refund has already reversed t: it carries the
// refunded_by mark, or Blnk has its reversal (reference "<id>_refund").
func (s *Service) reversed(ctx context.Context, t *blnk.Transaction) (bool, error) {
	if str(t.MetaData[metaRefundedBy]) != "" {
		return true, nil
	}
	_, err := s.c.GetTransactionByReference(ctx, t.GetID()+"_refund")
	if err == nil {
		return true, nil
	}
	if blnk.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// owedTo is what each recipient of a stored transaction received, and must
// hold to give it back. A stored split (a queued parent) names its recipients
// in Destinations; a distribution that isn't an exact amount is skipped.
func owedTo(t *blnk.Transaction) map[AccountID]*big.Int {
	out := map[AccountID]*big.Int{}
	if t.Destination != "" {
		out[AccountID(t.Destination)] = new(big.Int).Set(&t.PreciseAmount.Int)
		return out
	}
	for _, d := range t.Destinations {
		if amt, ok := new(big.Int).SetString(d.PreciseDistribution, 10); ok {
			out[AccountID(d.Identifier)] = amt
		}
	}
	return out
}

// metaRefundedBy marks a transaction Refund has reversed, with the reversal's id.
const metaRefundedBy = "refunded_by"

// refundLeg is one transaction a full refund reverses, and what each of its
// recipients must pay back.
type refundLeg struct {
	id   string
	tx   *blnk.Transaction
	owed map[AccountID]*big.Int
}

func (l refundLeg) total() *big.Int {
	sum := new(big.Int)
	for _, v := range l.owed {
		sum.Add(sum, v)
	}
	return sum
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

// GetTransaction returns a transaction by its Blnk id. For a scheduled
// transfer whose time has come it returns the outcome Blnk recorded
// (posted, or rejected for lack of funds), whose ParentTransaction is txID.
func (s walletAPI) GetTransaction(ctx context.Context, txID string) (*Receipt, error) {
	const op = "ledger.Wallet.GetTransaction"
	t, err := s.byID(ctx, txID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if t, err = s.ranOrSelf(ctx, t); err != nil {
		return nil, mapErr(op, err)
	}
	return s.toReceipt(t), nil
}

// ranOrSelf is t's outcome once a scheduled t has run, else t.
func (s *Service) ranOrSelf(ctx context.Context, t *blnk.Transaction) (*blnk.Transaction, error) {
	out, err := s.scheduledOutcome(ctx, t)
	if err != nil || out == nil {
		return t, err
	}
	if out.ParentTransaction == "" {
		out.ParentTransaction = t.GetID()
	}
	return out, nil
}

// GetTransactionByReference tries the reference as given, then with Blnk's "_q"
// suffix used for records produced by the queue. Like GetTransaction, it
// returns a scheduled transfer's outcome once it has run.
func (s walletAPI) GetTransactionByReference(ctx context.Context, reference string) (*Receipt, error) {
	const op = "ledger.Wallet.GetTransactionByReference"
	t, err := s.byReference(ctx, reference)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if t, err = s.ranOrSelf(ctx, t); err != nil {
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
// amount (counting the partial refunds linked to it and disputes still open
// or upheld) are rejected, as is a transaction already reversed with Refund; the original's
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
	c, err := s.claimsOn(ctx, orig, in.Reference)
	if err != nil {
		return nil, mapErr(op, err)
	}
	refunded := c.refunded
	left := new(big.Int).Sub(&orig.PreciseAmount.Int, c.total(&orig.PreciseAmount.Int))
	if amt.PreciseAmount.Cmp(left) > 0 {
		return nil, &OpError{Op: op, Kind: ErrAmountExceeded,
			Cause: fmt.Errorf("%s exceeds the %s not yet refunded or disputed", amt, s.money(left, amt.Precision, amt.Currency))}
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
