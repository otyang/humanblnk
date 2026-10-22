package transactions

import (
	"context"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/types"
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
// in all. Each posting of a split is listed as its own entry.
func (s *Service) List(ctx context.Context, q types.EntryQuery) ([]types.Entry, int, error) {
	r := blnk.FilterRequest{Limit: q.Limit, Offset: q.Offset, IncludeCount: true, SortBy: "created_at", SortOrder: "desc"}
	add := func(field, op string, v any) {
		r.Filters = append(r.Filters, blnk.Filter{Field: field, Operator: op, Value: v})
	}
	if q.From != "" {
		add("source", "eq", string(q.From))
	}
	if q.To != "" {
		add("destination", "eq", string(q.To))
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
	out := make([]types.Entry, len(txs))
	for i := range txs {
		if out[i], err = core.Entry(&txs[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}
