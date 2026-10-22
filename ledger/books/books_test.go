package books_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/ledger/types"
)

func TestBooks(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	f.On("POST /ledgers", 201, `{"ledger_id":"ldg_1","name":"Customer Wallets"}`)
	b, err := s.Create(ctx, "Customer Wallets", types.Meta{"purpose": "wallets"})
	if err != nil || b.ID != "ldg_1" || b.Name != "Customer Wallets" {
		t.Fatalf("%+v %v", b, err)
	}
	if got := f.Sent("POST /ledgers")[0].Body; got["name"] != "Customer Wallets" {
		t.Fatalf("sent %v", got)
	}
	f.On("PUT /ledgers/ldg_1", 200, `{"ledger_id":"ldg_1","name":"Wallets"}`)
	if b, err = s.Rename(ctx, "ldg_1", "Wallets"); err != nil || b.Name != "Wallets" {
		t.Fatalf("%+v %v", b, err)
	}
	if _, err := s.Create(ctx, "", nil); !errors.Is(err, types.ErrInvalid) {
		t.Fatalf("no name: %v", err)
	}
	if _, err := s.Get(ctx, "ldg_missing"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	f.On("GET /ledgers", 200, `[{"ledger_id":"ldg_1","name":"Wallets"}]`)
	if bs, err := s.List(ctx, 5, 0); err != nil || len(bs) != 1 || f.Sent("GET /ledgers")[0].Query != "limit=5" {
		t.Fatalf("%+v %v", bs, err)
	}
}
