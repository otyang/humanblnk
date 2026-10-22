package transactions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

// Reversals are tagged with the entry they undo and the caller's reference,
// and numbered per entry.
const (
	metaReversalOf  = "reversal_of"
	metaReversalRef = "reversal_ref"
)

// maxReversalAttempts bounds how often a reversal retries after losing its
// number to a concurrent one.
const maxReversalAttempts = 5

// Reverse moves money back the way an entry moved it. Refunds,
// corrections, chargebacks and bounced payouts all use it.
//
// In full (the zero Amount) it undoes every posting, a split's fee
// included, or all that earlier partial reversals left. In part it moves
// Amount back between the entry's two accounts. Reversals of one entry,
// full and partial, can't add up to more than it moved, even when they run
// at the same time: each claims the next number in the entry's sequence,
// and Blnk lets only one write take a number. Once nothing is left, Reverse
// is ErrAlreadyReversed.
//
// A reversal is idempotent on its Ref: repeating it returns the reversal
// already made. Ref is required in part and recommended in full, where
// without one a retry after a timeout can only report ErrAlreadyReversed.
// Only a single-posting entry can be reversed in part: which postings of a
// split to undo is a business decision, so reverse the split in full and
// record what should stand.
//
// A reversal takes the money from where the entry put it, so it fails with
// ErrInsufficientFunds if a customer account there no longer holds it.
// System accounts, such as @World for a payout, may go below zero.
func (s *Service) Reverse(ctx context.Context, r types.Reversal) (types.Entry, error) {
	if r.Entry == "" || r.Reason == "" {
		return types.Entry{}, core.Invalid("a reversal needs the entry and a reason")
	}
	original, err := s.Get(ctx, r.Entry)
	if err != nil {
		return types.Entry{}, err
	}
	if original.Status != types.StatusApplied {
		return types.Entry{}, core.Invalid("entry %s is %s; only applied entries can be reversed (Cancel a hold)", r.Entry, original.Status)
	}
	split := len(original.From) > 1 || len(original.To) > 1
	if split && r.Amount.Valid() {
		return types.Entry{}, core.Invalid("entry %s is a split; it can only be reversed in full", r.Entry)
	}
	if r.Amount.Valid() && r.Ref == "" {
		return types.Entry{}, core.Invalid("a partial reversal needs a reference")
	}

	tried := 0 // the highest number already tried
	for attempt := 0; attempt < maxReversalAttempts; attempt++ {
		prior, err := s.reversalsOf(ctx, original)
		if err != nil {
			return types.Entry{}, err
		}
		if ref, ok := prior.byRef[r.Ref]; ok && r.Ref != "" {
			return s.k.GetEntryByReference(ctx, ref) // this reversal already happened
		}
		left, err := original.Amount.Sub(prior.applied)
		if err != nil {
			return types.Entry{}, err
		}
		if left.IsZeroAmount() {
			return types.Entry{}, fmt.Errorf("%w: %s", types.ErrAlreadyReversed, r.Entry)
		}
		amount := r.Amount
		if !amount.Valid() {
			amount = left // all of it, or what earlier partial reversals left
		}
		if !amount.IsPositive() {
			return types.Entry{}, core.Invalid("reversal amount %s must be positive", amount)
		}
		if cmp, err := amount.Cmp(left); err != nil {
			return types.Entry{}, err
		} else if cmp > 0 {
			return types.Entry{}, fmt.Errorf("%w: only %s %s of %s is left to reverse", types.ErrAlreadyReversed, left, left.Code(), r.Entry)
		}

		t := types.Transaction{
			From:        types.Single(original.To[0].Account, amount),
			To:          types.Single(original.From[0].Account, amount),
			Description: r.Reason,
			Meta:        types.Meta{metaReversalOf: r.Entry, metaReversalRef: r.Ref, "reversal_reason": r.Reason},
		}
		if amount.Equal(original.Amount) {
			t.From, t.To = original.To, original.From // every posting, a split's included
		}
		// The number makes concurrent reversals of one entry take turns:
		// Blnk keeps references unique, so only one can claim it, and the
		// other recounts what is left and tries a higher number (never one
		// it already lost, in case the winner isn't visible yet).
		tried = max(prior.highest+1, tried+1)
		t.Ref = types.Reference("reversal", r.Entry, strconv.Itoa(tried))
		e, err := s.Record(ctx, t)
		if errors.Is(err, types.ErrDuplicate) || errors.Is(err, types.ErrReferenceUsed) {
			continue // the number is taken; perhaps by this reversal, which the recount finds
		}
		return e, err
	}
	return types.Entry{}, fmt.Errorf("humanblnk: reversing %s: still contended after %d attempts; try again", r.Entry, maxReversalAttempts)
}

// reversals is what has already been done to undo an entry.
type reversals struct {
	highest int               // the highest number claimed, whatever became of it
	applied money.Money       // the sum of the reversals that applied
	byRef   map[string]string // their references, by the caller's reference
}

func (s *Service) reversalsOf(ctx context.Context, e types.Entry) (reversals, error) {
	out := reversals{applied: money.Zero(e.Amount.Currency()), byRef: map[string]string{}}
	prefix := types.Reference("reversal", e.ID) + ":"
	byNumber := map[string][]blnk.Transaction{} // a split reversal is a transaction per posting
	var order []string
	for offset := 0; ; offset += 100 {
		txs, _, err := s.k.C.Transactions.Filter(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{{Field: "meta_data." + metaReversalOf, Operator: "eq", Value: e.ID}},
			Limit:   100, Offset: offset,
		})
		if err != nil {
			return out, core.MapErr(err)
		}
		for _, tx := range txs {
			// Reversals are "reversal:<entry>:<n>", with "-<posting>" for a
			// split's. Anything else carrying the tag, such as Blnk's refund
			// rolling one back (it copies the metadata) or its queue copy,
			// isn't a reversal.
			ref := core.BaseRef(tx.Reference)
			n, err := strconv.Atoi(strings.TrimPrefix(ref, prefix))
			if !strings.HasPrefix(ref, prefix) || err != nil || n < 1 {
				continue
			}
			out.highest = max(out.highest, n)
			if _, seen := byNumber[ref]; !seen {
				order = append(order, ref)
			}
			byNumber[ref] = append(byNumber[ref], tx)
		}
		if len(txs) < 100 {
			break
		}
	}
	for _, ref := range order {
		txs := byNumber[ref]
		// Gather marks a split reversal missing a posting as not applied.
		r, err := core.Gather(ref, txs)
		if err != nil {
			return out, err
		}
		ids := make([]string, len(txs))
		for i := range txs {
			ids[i] = txs[i].GetID()
		}
		if r, err = s.k.MarkRolledBack(ctx, r, ids...); err != nil {
			return out, err
		}
		if r.Status != types.StatusApplied {
			continue
		}
		if out.applied, err = out.applied.Add(r.Amount); err != nil {
			return out, err
		}
		if caller, _ := r.Meta[metaReversalRef].(string); caller != "" {
			out.byRef[caller] = ref
		}
	}
	return out, nil
}
