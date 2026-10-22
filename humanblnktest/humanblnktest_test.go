package humanblnktest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/humanblnktest"
)

var ctx = context.Background()

func TestBooks(t *testing.T) {
	t.Parallel()
	hb := humanblnktest.New(t)
	ada, bola := hb.Customer(t, "USD"), hb.Customer(t, "USD")
	hb.Fund(t, ada, "100.00")
	hb.Fund(t, ada, "20.00") // each Fund is its own deposit

	fee := humanblnktest.Money(t, "1.50", "USD")
	if _, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: humanblnk.Reference("transfer", "t1"), From: humanblnk.Single(ada, humanblnktest.Money(t, "51.50", "USD")),
		To: humanblnk.Many(
			humanblnk.Posting{Account: bola, Amount: humanblnktest.Money(t, "50", "USD")},
			humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: fee},
		),
		Description: "transfer with fee",
	}); err != nil {
		t.Fatal(err)
	}
	if a, b := hb.Available(t, ada), hb.Available(t, bola); a != "68.50" || b != "50.00" {
		t.Fatalf("ada %s, bola %s", a, b)
	}
	if hb.Wallets == "" || hb.URL == "" {
		t.Fatalf("%+v", hb)
	}
}

func TestEachNewIsItsOwnBlnk(t *testing.T) {
	t.Parallel()
	a, b := humanblnktest.New(t), humanblnktest.New(t)
	ada := a.Customer(t, "NGN")
	if _, err := b.Accounts.Get(ctx, string(ada)); !errors.Is(err, humanblnk.ErrNotFound) {
		t.Fatalf("account seen by another Books: %v", err)
	}
}

// Record holds a split, so one refused partway never waits on Blnk's
// rollback: nothing moved, even with the refund an hour away.
func TestRefusedSplitMovesNothing(t *testing.T) {
	t.Parallel()
	hb := humanblnktest.New(t, humanblnktest.WithRollbackDelay(time.Hour))
	ada, bola, shop := hb.Customer(t, "USD"), hb.Customer(t, "USD"), hb.Customer(t, "USD")
	hb.Fund(t, ada, "10.00")
	_, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: "group-pay",
		From: humanblnk.Many(
			humanblnk.Posting{Account: ada, Amount: humanblnktest.Money(t, "10", "USD")},
			humanblnk.Posting{Account: bola, Amount: humanblnktest.Money(t, "10", "USD")},
		),
		To: humanblnk.Single(shop, humanblnktest.Money(t, "20", "USD")), Description: "group payment",
	})
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatal(err)
	}
	if got := hb.Available(t, ada); got != "10.00" {
		t.Fatalf("ada %s", got)
	}
	if e, err := hb.Transactions.GetByReference(ctx, "group-pay"); err != nil || e.Status != humanblnk.StatusRejected {
		t.Fatalf("the failed split reads %+v %v", e, err)
	}
}

func TestWebhooks(t *testing.T) {
	t.Parallel()
	events := make(chan humanblnk.Event, 10)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ev, err := humanblnk.VerifyWebhook("secret", r, time.Minute); err == nil {
			events <- ev
		}
	}))
	t.Cleanup(hook.Close)
	hb := humanblnktest.New(t, humanblnktest.WithWebhook(hook.URL, "secret"))
	ada := hb.Customer(t, "USD")
	watch, err := hb.Watches.Create(ctx, humanblnk.Watch{
		Account: ada, Field: humanblnk.WatchBalance, Op: humanblnk.WatchAbove, Threshold: humanblnktest.Money(t, "5", "USD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	hb.Fund(t, ada, "10.00")
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Kind == "balance.monitor" && ev.Watch == watch.ID && ev.Account == ada {
				return
			}
		case <-timeout:
			t.Fatal("no balance.monitor webhook")
		}
	}
}

func TestCustomerLedger(t *testing.T) {
	t.Parallel()
	hb := humanblnktest.New(t, humanblnktest.WithCustomerLedger("Savings"))
	savings := hb.CustomerLedgers["Savings"]
	owner, err := hb.Identities.Create(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	usd := humanblnktest.Money(t, "1", "USD").Currency()
	a, err := hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: usd, Ledger: savings})
	if err != nil || a.Ledger != savings {
		t.Fatalf("savings account %+v %v", a, err)
	}
	// Your own accounts can't be opened in a customer ledger.
	if _, err := hb.Accounts.CreateInternal(ctx, humanblnk.NewAccount{Ledger: savings, Currency: usd}); !errors.Is(err, humanblnk.ErrInvalid) {
		t.Fatalf("internal account in Savings: %v", err)
	}
}
