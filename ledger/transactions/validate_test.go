package transactions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

func TestInvalidTransactionsNeverReachBlnk(t *testing.T) {
	ten, five := money.MustParse("10", blnkfake.USD), money.MustParse("5", blnkfake.USD)
	ok := types.Transaction{Ref: "r", From: types.Single("bln_usd", ten), To: types.Single("bln_usd2", ten), Description: "d"}
	with := func(edit func(*types.Transaction)) types.Transaction { t := ok; edit(&t); return t }

	for name, tc := range map[string]struct {
		t    types.Transaction
		want error
	}{
		"no reference":   {with(func(t *types.Transaction) { t.Ref = "" }), types.ErrInvalid},
		"no description": {with(func(t *types.Transaction) { t.Description = "" }), types.ErrInvalid},
		"no postings":    {with(func(t *types.Transaction) { t.To = nil }), types.ErrInvalid},
		"unbalanced":     {with(func(t *types.Transaction) { t.To = types.Single("bln_usd2", five) }), types.ErrUnbalanced},
		"zero money":     {with(func(t *types.Transaction) { t.From = types.Single("bln_usd", money.Money{}) }), money.ErrUnknownCurrency},
		"negative": {with(func(t *types.Transaction) {
			t.From = types.Single("bln_usd", ten.Neg())
			t.To = types.Single("bln_usd2", ten.Neg())
		}), types.ErrInvalid},
		"many to many": {with(func(t *types.Transaction) {
			t.From = types.Many(types.Posting{Account: "bln_usd", Amount: five}, types.Posting{Account: "bln_usd2", Amount: five})
			t.To = types.Many(types.Posting{Account: "a", Amount: five}, types.Posting{Account: "b", Amount: five})
		}), types.ErrInvalid},
		"mixed currencies": {with(func(t *types.Transaction) {
			t.To = types.Many(types.Posting{Account: "bln_usd2", Amount: five}, types.Posting{Account: "bln_ngn", Amount: money.MustParse("5", blnkfake.NGN)})
		}), money.ErrCurrencyMismatch},
		"account in another currency": {with(func(t *types.Transaction) { t.To = types.Single("bln_ngn", ten) }), money.ErrCurrencyMismatch},
	} {
		s, f := newService(t)
		_, err := s.Record(context.Background(), tc.t)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
		if n := len(f.Sent("POST /transactions")); n != 0 {
			t.Errorf("%s: %d transactions reached Blnk", name, n)
		}
	}
}
