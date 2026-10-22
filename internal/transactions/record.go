package transactions

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
)

// Record moves money: t applies in full, or not at all. A split (several
// postings on one side) is still one atomic write, but Blnk records it a
// posting at a time: if a later posting fails, the earlier ones have moved
// and Blnk refunds them through its queue a moment later (about 200ms with
// its worker running). Until then they show in the accounts.
//
// A reference already used is checked before it is reported. ErrDuplicate
// means this transaction already happened, applied, which is what a retry
// after a timeout should treat as success; its entry comes back with it.
// ErrReferenceUsed means the reference holds something else, such as this
// split rolled back after a later posting failed: nothing moved, and only a
// new reference can record it.
func (s *Service) Record(ctx context.Context, t types.Transaction) (types.Entry, error) {
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

// RecordAll records every transaction or none: if one fails, Blnk rolls the
// others back. They may be in different currencies, as an exchange is (USD
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
// not (a batch Blnk rolled back keeps its references).
func (s *Service) RecordAll(ctx context.Context, ts []types.Transaction) ([]types.Entry, error) {
	if len(ts) == 0 {
		return nil, core.Invalid("no transactions")
	}
	reqs := make([]blnk.CreateTransactionRequest, len(ts))
	out := make([]types.Entry, len(ts))
	for i, t := range ts {
		if len(t.From) != 1 || len(t.To) != 1 {
			return nil, core.Invalid("transaction %d: RecordAll takes one posting on each side", i)
		}
		r, err := s.k.Request(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		reqs[i] = r
		out[i] = types.Entry{
			Ref: t.Ref, Status: types.StatusApplied, Amount: t.From[0].Amount,
			From: t.From, To: t.To, Description: t.Description, Meta: t.Meta,
		}
	}
	_, err := s.k.C.Transactions.CreateBulk(ctx, blnk.BulkTransactionRequest{
		Atomic: true, SkipQueue: true, Transactions: reqs,
	})
	if blnk.HasCode(err, blnk.CodeDuplicateReference) {
		return s.existingAll(ctx, ts)
	}
	if err != nil {
		return nil, core.MapErr(err)
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
// batch Blnk rolled back leaves its earlier entries rolled back and its
// later ones missing, which is ErrReferenceUsed.
func (s *Service) existingAll(ctx context.Context, ts []types.Transaction) ([]types.Entry, error) {
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
