package transactions

import (
	"context"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
)

// Get fetches an entry by id. For a split, id is the id Record returned,
// and the entry gathers every posting Blnk recorded for it.
func (s *Service) Get(ctx context.Context, id string) (types.Entry, error) {
	return s.k.GetEntry(ctx, id)
}

// GetByReference fetches the entry recorded under ref: the way to ask
// whether a write already happened.
func (s *Service) GetByReference(ctx context.Context, ref string) (types.Entry, error) {
	return s.k.GetEntryByReference(ctx, ref)
}

// List returns the entries q selects, newest first, and how many there are
// in all. Each posting of a split is listed as its own entry. A system
// account in From or To (such as humanblnk.ClearingAccount("paystack")) means
// its balance in every currency.
func (s *Service) List(ctx context.Context, q types.EntryQuery) ([]types.Entry, int, error) {
	r := blnk.FilterRequest{Limit: q.Limit, Offset: q.Offset, IncludeCount: true, SortBy: "created_at", SortOrder: "desc"}
	add := func(field, op string, v any) {
		r.Filters = append(r.Filters, blnk.Filter{Field: field, Operator: op, Value: v})
	}
	for _, side := range []struct {
		field   string
		account types.AccountRef
	}{{"source", q.From}, {"destination", q.To}} {
		switch {
		case side.account == "":
		case side.account.IsSystem():
			// Blnk records a system account by its balance id, one per currency.
			ids, err := s.systemBalances(ctx, side.account)
			if err != nil {
				return nil, 0, err
			}
			if len(ids) == 0 {
				return nil, 0, nil // never used, so nothing moved through it
			}
			r.Filters = append(r.Filters, blnk.Filter{Field: side.field, Operator: "in", Values: ids})
		default:
			add(side.field, "eq", string(side.account))
		}
	}
	if q.Status != "" {
		add("status", "eq", q.Status)
	}
	if !q.Since.IsZero() {
		add("created_at", "gte", core.Time(q.Since))
	}
	if !q.Until.IsZero() {
		add("created_at", "lte", core.Time(q.Until))
	}
	txs, total, err := s.k.C.Transactions.Filter(ctx, r)
	if err != nil {
		return nil, 0, core.MapErr(err)
	}
	return s.entries(txs, total)
}

// systemBalances are the ids of system account a's balances, one per
// currency it has been used in.
func (s *Service) systemBalances(ctx context.Context, a types.AccountRef) ([]any, error) {
	bs, _, err := s.k.C.Balances.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{{Field: "indicator", Operator: "eq", Value: string(a)}},
		Limit:   100,
	})
	if err != nil {
		return nil, core.MapErr(err)
	}
	ids := make([]any, len(bs))
	for i := range bs {
		ids[i] = bs[i].BalanceID
	}
	return ids, nil
}

func (s *Service) entries(txs []blnk.Transaction, total int) ([]types.Entry, int, error) {
	var err error
	out := make([]types.Entry, len(txs))
	for i := range txs {
		if out[i], err = core.Entry(&txs[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}
