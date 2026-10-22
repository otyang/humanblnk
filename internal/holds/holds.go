// Package holds places, captures and cancels holds: money reserved now and
// settled later. Use it through humanblnk.Client.Holds.
//
// Blnk never changes a hold's own transaction: capturing or cancelling one
// records a new transaction (APPLIED or VOID) that names the hold as its
// parent. So a hold read with Transactions.Get stays StatusHeld for good;
// what happened to it is in those children.
package holds

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"net/http"
	"sync"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

// Service places, captures and cancels holds.
type Service struct {
	k *core.Core

	// capturing serialises Capture calls for the same hold within this
	// process, so two different-amount captures can't both commit.
	capturing [64]sync.Mutex
	seed      maphash.Seed
}

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k: k, seed: maphash.MakeSeed()} }

// Place reserves t's amount: it leaves From's Available at once but reaches
// To only when captured. If expires is set and passes first, Blnk cancels
// the hold. A split (a payout and its fee, say) is held, captured and
// cancelled as one.
//
// Place is safe to repeat with the same t: if t.Ref already holds this
// hold, its id comes back, captured or not, so a whole purchase (Place,
// deliver, Capture) is safe to retry. If it was released instead, cancelled
// or expired without any capture, that is ErrHoldSettled: the money is no
// longer held, so don't deliver against it. A reference holding anything
// else, including this split hold rolled back, is ErrReferenceUsed.
func (s *Service) Place(ctx context.Context, t types.Transaction, expires time.Time) (types.HoldID, error) {
	r, err := s.k.Request(ctx, t)
	if err != nil {
		return "", err
	}
	r.Inflight = true
	r.InflightExpiryDate = core.Time(expires)
	if items, ok, err := s.k.PerSource(ctx, r); err != nil {
		return "", err
	} else if ok {
		// A split from system accounts and others: an inflight batch, held,
		// captured and cancelled by its id as a split hold is.
		res, err := s.k.C.Transactions.CreateBulk(ctx, blnk.BulkTransactionRequest{Atomic: true, Inflight: true, SkipQueue: true, Transactions: items})
		if blnk.HasCode(err, blnk.CodeDuplicateReference) {
			return s.placed(ctx, t)
		}
		if err != nil {
			return "", core.MapErr(err)
		}
		return types.HoldID(res.BatchID), nil
	}
	tx, err := s.k.C.Transactions.Create(ctx, r)
	if blnk.HasCode(err, blnk.CodeDuplicateReference) {
		return s.placed(ctx, t)
	}
	if err != nil {
		return "", core.MapErr(err)
	}
	return types.HoldID(tx.GetID()), nil
}

// placed is the hold an earlier Place of t made: its answer was lost, and
// the retry met the reference already used.
func (s *Service) placed(ctx context.Context, t types.Transaction) (types.HoldID, error) {
	e, err := s.k.GetEntryByReference(ctx, t.Ref)
	if errors.Is(err, types.ErrNotFound) {
		// Blnk holds the reference, but nothing is recorded under it: it
		// holds "<ref>-1" of something else, such as a single transaction.
		return "", fmt.Errorf("%w: %s is held by something not recorded under it", types.ErrReferenceUsed, t.Ref)
	}
	if err != nil {
		return "", fmt.Errorf("humanblnk: reference %s is already used, and looking it up failed (check again before retrying): %w", t.Ref, err)
	}
	same, err := s.k.Matches(ctx, e, t)
	if err != nil {
		return "", fmt.Errorf("humanblnk: reference %s is already used, and checking it failed (check again before retrying): %w", t.Ref, err)
	}
	// A split hold Blnk rolled back keeps its references, with a posting
	// missing, so it doesn't match.
	if !same || e.Status != types.StatusHeld {
		return "", fmt.Errorf("%w: %s is %s %s, not this hold", types.ErrReferenceUsed, t.Ref, e.ID, e.Status)
	}
	// A hold's own transaction stays INFLIGHT after it is settled; what
	// happened to it is in its children.
	split := len(e.From) > 1 || len(e.To) > 1
	if released, err := s.released(ctx, types.HoldID(e.ID), split); err != nil {
		return "", err
	} else if released {
		return "", fmt.Errorf("%w: %s holds hold %s, cancelled or expired", types.ErrHoldSettled, t.Ref, e.ID)
	}
	return types.HoldID(e.ID), nil
}

// released reports whether hold h was released without any capture:
// cancelled or expired. It, or for a split each of its postings, then has a
// VOID child and no APPLIED one.
func (s *Service) released(ctx context.Context, h types.HoldID, split bool) (bool, error) {
	children := func(parent string) ([]blnk.Transaction, error) {
		txs, _, err := s.k.C.Transactions.Filter(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{{Field: "parent_transaction", Operator: "eq", Value: parent}},
			Limit:   100,
		})
		return txs, core.MapErr(err)
	}
	holds := []string{string(h)}
	if split {
		legs, err := children(string(h))
		if err != nil {
			return false, err
		}
		holds = holds[:0]
		for _, l := range legs {
			holds = append(holds, l.GetID())
		}
	}
	voided := false
	for _, id := range holds {
		kids, err := children(id)
		if err != nil {
			return false, err
		}
		for _, k := range kids {
			switch k.Status {
			case types.StatusApplied:
				return false, nil // captured, at least in part: the money moved
			case types.StatusVoid:
				voided = true
			}
		}
	}
	return voided, nil
}

// Capture settles a hold: amount moves to its To account, and whatever of
// the hold is left is released, so a capture is final. The zero Money
// captures all of it; a split hold can only be captured in full.
//
// Capture is safe to repeat with the same amount, even after a timeout: it
// finds the capture an earlier attempt made and finishes it rather than
// moving the money again, including a split whose earlier commit stopped
// partway. A different amount is ErrHoldSettled, and so is a split with a
// posting released (say, by its expiry): it can't be captured whole. One gap
// remains: a partial capture retried while Blnk is still working on the
// first attempt can capture twice, so wait a few seconds before retrying one.
func (s *Service) Capture(ctx context.Context, h types.HoldID, amount money.Money) (types.Entry, error) {
	mu := &s.capturing[maphash.String(s.seed, string(h))%uint64(len(s.capturing))]
	mu.Lock()
	defer mu.Unlock()

	held, err := s.k.GetEntry(ctx, string(h))
	if err != nil {
		return types.Entry{}, err
	}
	if held.Status != types.StatusHeld {
		// A hold stays INFLIGHT after capture or cancel, so this is some
		// other transaction's id, such as a capture's.
		return types.Entry{}, fmt.Errorf("%w: %s is %s, not a hold", types.ErrHoldSettled, h, held.Status)
	}
	split := len(held.From) > 1 || len(held.To) > 1
	want := held.Amount
	if amount.Valid() {
		cmp, err := amount.Cmp(held.Amount)
		switch {
		case err != nil:
			return types.Entry{}, err
		case !amount.IsPositive():
			return types.Entry{}, core.Invalid("capture amount %s must be positive", amount)
		case cmp > 0:
			return types.Entry{}, core.Invalid("capture %s is more than the %s held", amount, held.Amount)
		}
		want = amount
	}
	full := want.Equal(held.Amount)
	if split && !full {
		return types.Entry{}, core.Invalid("hold %s is a split; it can only be captured in full", h)
	}

	// An earlier attempt may have committed and lost the answer, and a
	// partial commit sent again would capture twice. A split's captures sit
	// under its postings, not under h, and a full commit can't repeat, so
	// only a single hold needs the look.
	var captured types.Entry
	done := false
	if !split {
		if captured, done, err = s.captured(ctx, h); err != nil {
			return types.Entry{}, err
		}
	} else if leg, released, err := s.legReleased(ctx, h); err != nil {
		return types.Entry{}, err
	} else if released {
		// Blnk would commit the other postings and answer ALREADY_VOIDED:
		// part of the split moved without the rest.
		return types.Entry{}, fmt.Errorf("%w: posting %s of split hold %s was released (cancelled or expired), so the split can't be captured whole", types.ErrHoldSettled, leg, h)
	}
	if !done {
		if captured, err = s.commit(ctx, h, held, want, full, split); err != nil {
			return types.Entry{}, err
		}
	}
	if !captured.Amount.Equal(want) {
		settled := fmt.Errorf("%w: %s already captured %s", types.ErrHoldSettled, h, captured.Amount)
		// An earlier partial capture may have stopped before releasing the
		// rest; release it now, or it stays held until expiry.
		if !captured.Amount.Equal(held.Amount) {
			if err := s.Cancel(ctx, h); err != nil && !errors.Is(err, types.ErrHoldSettled) {
				return types.Entry{}, errors.Join(settled, fmt.Errorf("releasing the rest failed (Cancel it again): %w", err))
			}
		}
		return types.Entry{}, settled
	}
	if !full {
		if err := s.Cancel(ctx, h); err != nil {
			return captured, fmt.Errorf("humanblnk: captured %s of hold %s, but releasing the rest failed (Capture or Cancel it again): %w", want, h, err)
		}
	}
	return captured, nil
}

// commit sends the commit. If its answer is lost, or Blnk says the hold is
// already committed, it looks for the capture before reporting failure.
func (s *Service) commit(ctx context.Context, h types.HoldID, held types.Entry, want money.Money, full, split bool) (types.Entry, error) {
	req := blnk.UpdateInflightRequest{Status: blnk.ActionCommit, SkipQueue: true}
	if !full {
		var err error
		if req.PreciseAmount, _, err = core.ToBlnk(want); err != nil {
			return types.Entry{}, err
		}
	}
	res, err := s.k.C.Inflight.Update(ctx, string(h), req)
	switch {
	case err == nil && split:
		// Blnk commits each posting separately and answers with one of them;
		// the split as a whole is what was held, now applied.
		held.Status = types.StatusApplied
		return held, nil
	case err == nil:
		return core.Entry(&res.Transaction)
	case split && (blnk.HasCode(err, blnk.CodeAlreadyCommitted) || unanswered(err)):
		// A split committed partway (an earlier attempt cut short) is
		// finished by this commit: Blnk commits the postings still held, then
		// answers ALREADY_COMMITTED for the ones that weren't. It answers so
		// even when another was voided, so check every one was captured.
		if all, lerr := s.splitCaptured(ctx, h); lerr == nil && all {
			held.Status = types.StatusApplied // an earlier attempt captured it
			return held, nil
		}
	case !split && (blnk.HasCode(err, blnk.CodeAlreadyCommitted) || unanswered(err)):
		if e, ok, lerr := s.captured(ctx, h); lerr == nil && ok {
			return e, nil
		}
	}
	return types.Entry{}, core.MapErr(err)
}

// legReleased reports whether a posting of split hold h was released,
// cancelled or expired, and which. Committing h settles every posting still
// held, whatever became of the others, so a split with one released can
// only be captured in part.
func (s *Service) legReleased(ctx context.Context, h types.HoldID) (string, bool, error) {
	legs, _, err := s.k.C.Transactions.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{
			{Field: "parent_transaction", Operator: "eq", Value: string(h)},
			{Field: "status", Operator: "eq", Value: types.StatusHeld},
		},
		Limit: 100,
	})
	if err != nil {
		return "", false, core.MapErr(err)
	}
	for i := range legs {
		voids, _, err := s.k.C.Transactions.Filter(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{
				{Field: "parent_transaction", Operator: "eq", Value: legs[i].GetID()},
				{Field: "status", Operator: "eq", Value: types.StatusVoid},
			},
			Limit: 1,
		})
		if err != nil {
			return "", false, core.MapErr(err)
		}
		if len(voids) > 0 {
			return legs[i].Reference, true, nil
		}
	}
	return "", false, nil
}

// splitCaptured reports whether every posting of split hold h has been
// captured. Blnk holds a split as one INFLIGHT transaction per posting,
// each naming h as its parent, and captures each separately.
func (s *Service) splitCaptured(ctx context.Context, h types.HoldID) (bool, error) {
	legs, _, err := s.k.C.Transactions.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{
			{Field: "parent_transaction", Operator: "eq", Value: string(h)},
			{Field: "status", Operator: "eq", Value: types.StatusHeld},
		},
		Limit: 100,
	})
	if err != nil || len(legs) == 0 {
		return false, core.MapErr(err)
	}
	for i := range legs {
		if _, ok, err := s.captured(ctx, types.HoldID(legs[i].GetID())); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// captured finds the capture recorded against a single hold h, if any.
func (s *Service) captured(ctx context.Context, h types.HoldID) (types.Entry, bool, error) {
	txs, _, err := s.k.C.Transactions.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{
			{Field: "parent_transaction", Operator: "eq", Value: string(h)},
			{Field: "status", Operator: "eq", Value: types.StatusApplied},
		},
		Limit: 1,
	})
	if err != nil {
		return types.Entry{}, false, core.MapErr(err)
	}
	if len(txs) == 0 {
		return types.Entry{}, false, nil
	}
	e, err := core.Entry(&txs[0])
	return e, err == nil, err
}

// unanswered reports whether err leaves it unknown if Blnk acted: a network
// error or a gateway's 502 or 504, not an answer from Blnk itself.
func unanswered(err error) bool {
	var ae *blnk.APIError
	return !errors.As(err, &ae) || ae.Status == http.StatusBadGateway || ae.Status == http.StatusGatewayTimeout
}

// Cancel releases a hold: the money returns to its From account. It is safe
// to retry. Cancel never undoes money that moved; Transactions.Reverse does
// that.
func (s *Service) Cancel(ctx context.Context, h types.HoldID) error {
	_, err := s.k.C.Inflight.Update(ctx, string(h), blnk.UpdateInflightRequest{Status: blnk.ActionVoid, SkipQueue: true})
	if blnk.HasCode(err, blnk.CodeAlreadyVoided) {
		return nil // an earlier attempt, or expiry, already released it
	}
	return core.MapErr(err)
}
