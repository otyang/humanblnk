package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
)

// New wires every service to one Blnk and one currency cache, and the
// re-exported names are the real ones.
func TestNewWiresTheServices(t *testing.T) {
	c, f := blnkfake.New(t)
	books := ledger.New(c, ledger.WithAccountLedger("ldg_wallets"))
	ctx := context.Background()

	f.On("POST /balances/filter", 200, `[]`)
	f.On("POST /balances", 201, `{"balance_id":"bln_new","currency":"USD"}`)
	if _, err := books.Accounts.OpenForCustomer(ctx, ledger.CustomerAccount{Owner: "idt_ada", Currency: blnkfake.USD}); err != nil {
		t.Fatal(err)
	}
	if got := f.Sent("POST /balances")[0].Body["ledger_id"]; got != "ldg_wallets" {
		t.Fatalf("ledger_id %v", got)
	}

	// Transactions sees the currency Accounts cached: no lookup, and the
	// mismatch surfaces as money's error through the ledger.
	_, err := books.Transactions.Record(ctx, ledger.Transaction{
		Ref: "r", From: ledger.Single(ledger.World, money.MustParse("1", blnkfake.NGN)),
		To: ledger.Single("bln_new", money.MustParse("1", blnkfake.NGN)), Description: "d",
	})
	if !errors.Is(err, money.ErrCurrencyMismatch) || len(f.Sent("GET /balances/bln_new")) != 0 {
		t.Fatalf("record: %v", err)
	}

	f.On("PUT /transactions/inflight/h1", 400, blnkfake.APIError("TXN_ALREADY_COMMITTED"))
	if err := books.Holds.Cancel(ctx, "h1"); !errors.Is(err, ledger.ErrHoldSettled) {
		t.Fatalf("cancel: %v", err)
	}
}
