// Package books creates and reads books: Blnk ledgers, each a named group of
// accounts. Use it through humanblnk.Ledger.Books.
package books

import (
	"context"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
)

// Service creates and reads books.
type Service struct{ k *core.Core }

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k} }

// Create creates a book. It is not retried after a timeout (a repeat would
// create a second one): look it up with List before trying again.
func (s *Service) Create(ctx context.Context, name string, meta types.Meta) (types.Book, error) {
	if name == "" {
		return types.Book{}, core.Invalid("a book needs a name")
	}
	l, err := s.k.C.Ledgers.Create(ctx, blnk.CreateLedgerRequest{Name: name, MetaData: blnk.MetaData(meta)})
	if err != nil {
		return types.Book{}, core.MapErr(err)
	}
	return book(l), nil
}

// Get fetches a book by id.
func (s *Service) Get(ctx context.Context, id string) (types.Book, error) {
	l, err := s.k.C.Ledgers.Get(ctx, id)
	if err != nil {
		return types.Book{}, core.MapErr(err)
	}
	return book(l), nil
}

// List lists books, newest first; limit 0 is Blnk's default of 10.
func (s *Service) List(ctx context.Context, limit, offset int) ([]types.Book, error) {
	ls, err := s.k.C.Ledgers.List(ctx, blnk.ListOptions{Limit: limit, Offset: offset})
	if err != nil {
		return nil, core.MapErr(err)
	}
	out := make([]types.Book, len(ls))
	for i := range ls {
		out[i] = book(&ls[i])
	}
	return out, nil
}

// Rename changes a book's name, the only thing about it that can change.
func (s *Service) Rename(ctx context.Context, id, name string) (types.Book, error) {
	if name == "" {
		return types.Book{}, core.Invalid("a book needs a name")
	}
	l, err := s.k.C.Ledgers.Update(ctx, id, name)
	if err != nil {
		return types.Book{}, core.MapErr(err)
	}
	return book(l), nil
}

func book(l *blnk.Ledger) types.Book {
	return types.Book{ID: l.LedgerID, Name: l.Name, Meta: types.Meta(l.MetaData), CreatedAt: l.CreatedAt.Time}
}
