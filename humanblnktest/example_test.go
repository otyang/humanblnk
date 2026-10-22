package humanblnktest_test

import (
	"context"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/humanblnktest"
	"github.com/otyang/humanblnk/money"
)

// Payouts is code under test: a service that needs only the holds.
type Payouts struct{ Holds *humanblnk.HoldService }

// Start reserves a payout of amount from account until the bank answers.
func (p Payouts) Start(ctx context.Context, id string, account humanblnk.AccountRef, amount money.Money) (humanblnk.HoldID, error) {
	return p.Holds.Place(ctx, humanblnk.Transaction{
		Ref:         humanblnk.Reference("payout", id),
		From:        humanblnk.Single(account, amount),
		To:          humanblnk.Single(humanblnk.World, amount),
		Description: "Payout to bank",
	}, time.Now().Add(72*time.Hour))
}

// A test of a payout service, with no Blnk to run.
func Example() {
	_ = func(t *testing.T) {
		hb := humanblnktest.New(t)
		ada := hb.Customer(t, "USD")
		hb.Fund(t, ada, "100.00")

		svc := Payouts{Holds: hb.Holds}
		amount := humanblnktest.Money(t, "30.00", "USD")
		hold, err := svc.Start(context.Background(), "po_1", ada, amount)
		if err != nil {
			t.Fatal(err)
		}
		if got := hb.Available(t, ada); got != "70.00" {
			t.Fatalf("while held, ada can spend %s; want 70.00", got)
		}
		if err := hb.Holds.Cancel(context.Background(), hold); err != nil {
			t.Fatal(err)
		}
		if got := hb.Available(t, ada); got != "100.00" {
			t.Fatalf("after the bank declined, ada can spend %s; want 100.00", got)
		}
	}
}

// The same test, run.
func TestExamplePayouts(t *testing.T) {
	hb := humanblnktest.New(t)
	ada := hb.Customer(t, "USD")
	hb.Fund(t, ada, "100.00")
	amount := humanblnktest.Money(t, "30.00", "USD")
	hold, err := Payouts{Holds: hb.Holds}.Start(context.Background(), "po_1", ada, amount)
	if err != nil || hb.Available(t, ada) != "70.00" {
		t.Fatalf("%v %s", err, hb.Available(t, ada))
	}
	if err := hb.Holds.Cancel(context.Background(), hold); err != nil || hb.Available(t, ada) != "100.00" {
		t.Fatalf("%v %s", err, hb.Available(t, ada))
	}
}
