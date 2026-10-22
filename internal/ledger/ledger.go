// Package ledger creates and reads Blnk ledgers, each a named group of
// accounts. Use it through humanblnk.Client.Ledgers.
package ledger

import (
	"context"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
)

// Service creates and reads ledgers.
type Service struct{ k *core.Core }

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k} }

// Create creates a ledger. It is not retried after a timeout (a repeat would
// create a second one): look it up with List before trying again.
func (s *Service) Create(ctx context.Context, name string, meta types.Meta) (types.Ledger, error) {
	if name == "" {
		return types.Ledger{}, core.Invalid("a ledger needs a name")
	}
	l, err := s.k.C.Ledgers.Create(ctx, blnk.CreateLedgerRequest{Name: name, MetaData: blnk.MetaData(meta)})
	if err != nil {
		return types.Ledger{}, core.MapErr(err)
	}
	return fromBlnk(l), nil
}

// Get fetches a ledger by id.
func (s *Service) Get(ctx context.Context, id string) (types.Ledger, error) {
	l, err := s.k.C.Ledgers.Get(ctx, id)
	if err != nil {
		return types.Ledger{}, core.MapErr(err)
	}
	return fromBlnk(l), nil
}

// List lists ledgers, newest first; limit 0 is Blnk's default of 10.
func (s *Service) List(ctx context.Context, limit, offset int) ([]types.Ledger, error) {
	ls, err := s.k.C.Ledgers.List(ctx, blnk.ListOptions{Limit: limit, Offset: offset})
	if err != nil {
		return nil, core.MapErr(err)
	}
	out := make([]types.Ledger, len(ls))
	for i := range ls {
		out[i] = fromBlnk(&ls[i])
	}
	return out, nil
}

// Rename changes a ledger's name, the only thing about it that can change.
func (s *Service) Rename(ctx context.Context, id, name string) (types.Ledger, error) {
	if name == "" {
		return types.Ledger{}, core.Invalid("a ledger needs a name")
	}
	l, err := s.k.C.Ledgers.Update(ctx, id, name)
	if err != nil {
		return types.Ledger{}, core.MapErr(err)
	}
	return fromBlnk(l), nil
}

func fromBlnk(l *blnk.Ledger) types.Ledger {
	return types.Ledger{ID: l.LedgerID, Name: l.Name, Meta: types.Meta(l.MetaData), CreatedAt: l.CreatedAt.Time}
}
