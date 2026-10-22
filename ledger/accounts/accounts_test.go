package accounts_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/clients/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/accounts"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/transactions"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

func TestOpenForCustomerOpensOnceInTheDefaultLedger(t *testing.T) {
	c, f := blnkfake.New(t)
	k := core.New(c, "ldg_wallets")
	s, tx := accounts.New(k), transactions.New(k) // sharing k, as ledger.New does
	f.On("POST /balances", 201, `{"balance_id":"bln_new","ledger_id":"ldg_wallets","identity_id":"idt_ada","currency":"USD"}`)
	looks := 0
	f.OnFunc("POST /balances/filter", func(blnkfake.Call) (int, string) {
		if looks++; looks == 1 {
			return 200, `[]` // nothing open yet
		}
		return 200, `[{"balance_id":"bln_new","ledger_id":"ldg_wallets","identity_id":"idt_ada","currency":"USD"}]`
	})
	a, err := s.OpenForCustomer(context.Background(), types.CustomerAccount{Owner: "idt_ada", Currency: blnkfake.USD})
	if err != nil || a.ID != "bln_new" || a.Currency != blnkfake.USD || !a.Total.IsZeroAmount() {
		t.Fatalf("%+v %v", a, err)
	}
	if got := f.Sent("POST /balances")[0].Body; got["ledger_id"] != "ldg_wallets" || got["identity_id"] != "idt_ada" || got["currency"] != "USD" {
		t.Fatalf("sent %v", got)
	}
	filter := f.Sent("POST /balances/filter")[0].Body
	if fs := filter["filters"].([]any); len(fs) != 3 || filter["sort_order"] != "asc" {
		t.Fatalf("looked with %v", filter)
	}

	// Again: the open account comes back and nothing new is made.
	again, err := s.OpenForCustomer(context.Background(), types.CustomerAccount{Owner: "idt_ada", Currency: blnkfake.USD})
	if err != nil || again.ID != "bln_new" || len(f.Sent("POST /balances")) != 1 {
		t.Fatalf("second open: %+v %v, %d creates", again, err, len(f.Sent("POST /balances")))
	}

	// The new account's currency is known without another lookup.
	f.On("POST /transactions", 201, `{"transaction_id":"t","status":"APPLIED","currency":"NGN","precise_amount":100,"precision":100}`)
	_, err = tx.Record(context.Background(), types.Transaction{
		Ref: "r", From: types.Single(types.World, money.MustParse("1", blnkfake.NGN)), To: types.Single("bln_new", money.MustParse("1", blnkfake.NGN)), Description: "d",
	})
	if !errors.Is(err, money.ErrCurrencyMismatch) || len(f.Sent("GET /balances/bln_new")) != 0 {
		t.Fatalf("got %v", err)
	}
}

func TestOpenForCustomerLosingARaceSettlesOnTheOlder(t *testing.T) {
	s, f := newService(t)
	looks := 0
	f.OnFunc("POST /balances/filter", func(blnkfake.Call) (int, string) {
		if looks++; looks == 1 {
			return 200, `[]`
		}
		return 200, `[{"balance_id":"bln_first","ledger_id":"ldg_wallets","identity_id":"idt_ada","currency":"USD"}]` // opened by the other call
	})
	f.On("POST /balances", 201, `{"balance_id":"bln_second","ledger_id":"ldg_wallets","identity_id":"idt_ada","currency":"USD"}`)
	f.On("POST /bln_second/metadata", 200, `{"meta_data":{"duplicate_of":"bln_first"}}`)
	a, err := s.OpenForCustomer(context.Background(), types.CustomerAccount{Owner: "idt_ada", Currency: blnkfake.USD})
	if err != nil || a.ID != "bln_first" {
		t.Fatalf("%+v %v", a, err)
	}
	marks := f.Sent("POST /bln_second/metadata")
	if len(marks) != 1 || marks[0].Body["meta_data"].(map[string]any)["duplicate_of"] != "bln_first" {
		t.Fatalf("marked %+v", marks)
	}
}

func TestOpenForCustomerLostAnswer(t *testing.T) {
	s, f := newService(t)
	looks := 0
	f.OnFunc("POST /balances/filter", func(blnkfake.Call) (int, string) {
		if looks++; looks == 1 {
			return 200, `[]`
		}
		return 200, `[{"balance_id":"bln_made","ledger_id":"ldg_wallets","identity_id":"idt_ada","currency":"USD"}]`
	})
	f.On("POST /balances", 504, `gateway timeout`) // made, but the answer was lost
	a, err := s.OpenForCustomer(context.Background(), types.CustomerAccount{Owner: "idt_ada", Currency: blnkfake.USD})
	if err != nil || a.ID != "bln_made" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestCustomerAndInternalAccountsStayApart(t *testing.T) {
	c, f := blnkfake.New(t)
	k := core.New(c, "ldg_wallets")
	k.CustomerLedgers = map[string]bool{"ldg_savings": true}
	s := accounts.New(k)
	ctx := context.Background()
	refused := []struct {
		name string
		err  error
	}{
		{"open without owner", errOf(s.OpenForCustomer(ctx, types.CustomerAccount{Currency: blnkfake.USD}))},
		{"open in an internal ledger", errOf(s.OpenForCustomer(ctx, types.CustomerAccount{Owner: "idt_ada", Ledger: "ldg_ops", Currency: blnkfake.USD}))},
		{"internal in the wallets", errOf(s.CreateInternal(ctx, types.NewAccount{Ledger: "ldg_wallets", Currency: blnkfake.USD}))},
		{"internal in savings", errOf(s.CreateInternal(ctx, types.NewAccount{Ledger: "ldg_savings", Owner: "idt_ada", Currency: blnkfake.USD}))},
		{"internal without ledger", errOf(s.CreateInternal(ctx, types.NewAccount{Currency: blnkfake.USD}))},
		{"internal in the general ledger", errOf(s.CreateInternal(ctx, types.NewAccount{Ledger: "general_ledger_id", Currency: blnkfake.USD}))},
	}
	for _, r := range refused {
		if !errors.Is(r.err, types.ErrInvalid) {
			t.Errorf("%s: %v", r.name, r.err)
		}
	}
	if n := len(f.Sent("POST /balances")); n != 0 {
		t.Fatalf("%d accounts reached Blnk", n)
	}

	// Allowed: a customer account in a declared customer ledger, and an
	// internal account with or without an owner.
	f.On("POST /balances/filter", 200, `[]`)
	f.On("POST /balances", 201, `{"balance_id":"bln_x","currency":"USD"}`)
	if _, err := s.OpenForCustomer(ctx, types.CustomerAccount{Owner: "idt_ada", Ledger: "ldg_savings", Currency: blnkfake.USD}); err != nil {
		t.Fatalf("savings: %v", err)
	}
	if _, err := s.CreateInternal(ctx, types.NewAccount{Ledger: "ldg_ops", Currency: blnkfake.USD}); err != nil {
		t.Fatalf("internal: %v", err)
	}
	if _, err := s.CreateInternal(ctx, types.NewAccount{Ledger: "ldg_ops", Owner: "idt_merchant", Currency: blnkfake.USD}); err != nil {
		t.Fatalf("internal with owner: %v", err)
	}
}

func errOf(_ types.Account, err error) error { return err }

func TestGetAccountIsMoney(t *testing.T) {
	s, _ := newService(t)
	a, err := s.Get(context.Background(), "bln_usd")
	if err != nil {
		t.Fatal(err)
	}
	if a.Total.String() != "1500.25" || a.Available.String() != "500.00" || a.Held.String() != "0.25" || a.Owner != "idt_ada" {
		t.Fatalf("%+v", a)
	}
	if _, err := s.Get(context.Background(), "bln_missing"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestListAccountsFilters(t *testing.T) {
	s, f := newService(t)
	f.On("POST /balances/filter", 200, `{"data":[{"balance_id":"bln_usd","currency":"USD","balance":5}],"total_count":7}`)
	as, total, err := s.List(context.Background(), types.AccountQuery{Owner: "idt_ada", Currency: blnkfake.USD})
	if err != nil || len(as) != 1 || total != 7 {
		t.Fatalf("%+v %d %v", as, total, err)
	}
	if fs := f.Sent("POST /balances/filter")[0].Body["filters"].([]any); len(fs) != 2 {
		t.Fatalf("filters %v", fs)
	}
}
