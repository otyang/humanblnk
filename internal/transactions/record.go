package transactions

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

// Record moves money: t applies in full, or not at all.
//
// A split (several postings on one side) goes to Blnk as a hold, captured
// at once. Blnk moves a split a posting at a time: applied, one refused
// partway has already moved its earlier postings, and Blnk refunds them
// only later, through its queue. Held, they are released before Blnk
// answers, so a refused split never shows in the accounts. If the process
// stops between hold and capture, the hold expires after RecordHold, and a
// Record retried before then finishes it.
//
// A reference already used is checked before it is reported. ErrDuplicate
// means this transaction already happened, applied, which is what a retry
// after a timeout should treat as success; its entry comes back with it.
// ErrReferenceUsed means the reference holds something else, such as this
// split refused partway, or its hold expired: nothing moved, and only a
// new reference can record it.
func (s *Service) Record(ctx context.Context, t types.Transaction) (types.Entry, error) {
	if len(t.From) > 1 || len(t.To) > 1 {
		return s.recordSplit(ctx, t)
	}
	r, err := s.k.Request(ctx, t)
	if err != nil {
		return types.Entry{}, err
	}
	tx, err := s.k.C.Transactions.Create(ctx, r)
	if blnk.HasCode(err, blnk.CodeDuplicateReference) {
		return s.k.Existing(ctx, t)
	}
	if err != nil {
		return types.Entry{}, core.MapErr(err)
	}
	return core.Entry(tx)
}

// recordSplit records split t as a hold (see core.MetaRecorded) and
// captures it. A retry finds the hold Place made and finishes the capture.
func (s *Service) recordSplit(ctx context.Context, t types.Transaction) (types.Entry, error) {
	meta := make(types.Meta, len(t.Meta)+1)
	maps.Copy(meta, t.Meta)
	meta[core.MetaRecorded] = t.Ref
	t.Meta = meta
	h, err := s.holds.Place(ctx, t, time.Now().Add(RecordHold))
	if errors.Is(err, types.ErrReferenceUsed) || errors.Is(err, types.ErrHoldSettled) {
		return s.k.Existing(ctx, t) // applied by an earlier attempt, or the reference holds something else
	}
	if err != nil {
		return types.Entry{}, err
	}
	e, err := s.holds.Capture(ctx, h, money.Money{})
	if errors.Is(err, types.ErrHoldSettled) {
		return s.k.Existing(ctx, t) // a concurrent retry captured it, or it expired first
	}
	return e, err
}

// RecordAll records every transaction or none: if one fails, none moved.
// They go to Blnk as one batch of holds, captured at once, for the reason
// Record holds a split; a RecordAll retried before RecordHold passes
// finishes a batch whose capture was cut short. They may be in different currencies, as an exchange is (USD
// from the customer to your company, NGN from your company to the customer). Each must have
// one posting on each side.
//
// The entries come back in the order of ts. Blnk answers a batch with its id
// only, so the entries are built from ts and their ids read back afterwards.
// If that read keeps failing, the entries come back without ids and the
// error is ErrReadBack: the batch applied, so don't record it again.
//
// A reference already used is checked as Record checks it: ErrDuplicate
// with the entries if the whole batch already applied, ErrReferenceUsed if
// not (a refused batch keeps its references).
func (s *Service) RecordAll(ctx context.Context, ts []types.Transaction) ([]types.Entry, error) {
	if len(ts) == 0 {
		return nil, core.Invalid("no transactions")
	}
	reqs := make([]blnk.CreateTransactionRequest, len(ts))
	out := make([]types.Entry, len(ts))
	expires := core.Time(time.Now().Add(RecordHold))
	for i, t := range ts {
		if len(t.From) != 1 || len(t.To) != 1 {
			return nil, core.Invalid("transaction %d: RecordAll takes one posting on each side", i)
		}
		held := t
		held.Meta = make(types.Meta, len(t.Meta)+1)
		maps.Copy(held.Meta, t.Meta)
		held.Meta[core.MetaRecorded] = t.Ref
		r, err := s.k.Request(ctx, held)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		r.Inflight, r.InflightExpiryDate = true, expires
		reqs[i] = r
		out[i] = types.Entry{
			Ref: t.Ref, Status: types.StatusApplied, Amount: t.From[0].Amount,
			From: t.From, To: t.To, Description: t.Description, Meta: t.Meta,
		}
	}
	res, err := s.k.C.Transactions.CreateBulk(ctx, blnk.BulkTransactionRequest{
		Atomic: true, Inflight: true, SkipQueue: true, Transactions: reqs,
	})
	if blnk.HasCode(err, blnk.CodeDuplicateReference) {
		return s.existingAll(ctx, ts)
	}
	if err != nil {
		return nil, core.MapErr(err)
	}
	if err := s.commitAll(ctx, res.BatchID); err != nil {
		return nil, fmt.Errorf("humanblnk: batch %s is held but its capture failed (RecordAll again to finish it): %w", res.BatchID, err)
	}
	var failed error
	for i, t := range ts {
		e, err := s.readBack(ctx, t.Ref)
		if err != nil {
			failed = fmt.Errorf("%w: %s: %w", types.ErrReadBack, t.Ref, err)
			continue
		}
		out[i] = e
	}
	return out, failed
}

// existingAll is what to return when Blnk refuses a batch for a reference
// already used: every entry with ErrDuplicate if the whole batch already
// applied, or ErrReferenceUsed if any reference holds something else. A
// batch refused partway leaves its earlier holds released and its later
// ones missing, which is ErrReferenceUsed. A batch held but not captured
// is captured first.
func (s *Service) existingAll(ctx context.Context, ts []types.Transaction) ([]types.Entry, error) {
	if batch, ok, err := s.pendingBatch(ctx, ts); err != nil {
		return nil, err
	} else if ok {
		if err := s.commitAll(ctx, batch); err != nil {
			return nil, fmt.Errorf("humanblnk: batch %s is held but its capture failed (RecordAll again to finish it): %w", batch, err)
		}
	}
	out := make([]types.Entry, len(ts))
	var used, other error
	for i, t := range ts {
		e, err := s.k.Existing(ctx, t)
		switch {
		case errors.Is(err, types.ErrDuplicate):
			out[i] = e
		case errors.Is(err, types.ErrReferenceUsed):
			used = cmp.Or(used, fmt.Errorf("transaction %d: %w", i, err))
		default:
			other = cmp.Or(other, fmt.Errorf("transaction %d: %w", i, err))
		}
	}
	if err := cmp.Or(used, other); err != nil {
		return nil, err
	}
	return out, fmt.Errorf("%w: every transaction in the batch", types.ErrDuplicate)
}

// commitAll captures every hold of batch, a RecordAll's, in full. Blnk
// answers ALREADY_COMMITTED if an earlier attempt did; the read-back after
// it shows what applied.
func (s *Service) commitAll(ctx context.Context, batch string) error {
	_, err := s.k.C.Inflight.Update(ctx, batch, blnk.UpdateInflightRequest{Status: blnk.ActionCommit, SkipQueue: true})
	if err != nil && !blnk.HasCode(err, blnk.CodeAlreadyCommitted) {
		return core.MapErr(err)
	}
	return nil
}

// pendingBatch is the batch an earlier RecordAll of ts held but didn't
// capture, if it stopped between the two: each of ts held, as it is, under
// its reference, all in one batch.
func (s *Service) pendingBatch(ctx context.Context, ts []types.Transaction) (string, bool, error) {
	var batch string
	for _, t := range ts {
		tx, found, err := s.k.ByReference(ctx, t.Ref)
		if err != nil || !found {
			return "", false, err
		}
		if ref, _ := tx.MetaData[core.MetaRecorded].(string); ref != t.Ref || tx.ParentTransaction == "" || (batch != "" && tx.ParentTransaction != batch) {
			return "", false, nil
		}
		batch = tx.ParentTransaction
		e, err := s.k.GetEntry(ctx, tx.GetID())
		if err != nil {
			return "", false, err
		}
		if same, err := s.k.Matches(ctx, e, t); err != nil || !same || e.Status != types.StatusHeld {
			return "", false, err
		}
	}
	return batch, true, nil
}

// readBack fetches a just-recorded entry, retrying briefly in case Blnk
// hasn't finished answering lookups for it.
func (s *Service) readBack(ctx context.Context, ref string) (types.Entry, error) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return types.Entry{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}
		var e types.Entry
		if e, err = s.GetByReference(ctx, ref); err == nil {
			return e, nil
		}
	}
	return types.Entry{}, err
}
