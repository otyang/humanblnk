// Chapter 13: the airtime service from chapter 7, tested against an
// in-memory Blnk. Run it with go test ./docs/examples/13-testing: no Blnk,
// Docker or network needed.
package airtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/07-airtime/airtime"
	"github.com/otyang/humanblnk/humanblnktest"
	"github.com/otyang/humanblnk/money"
)

var ctx = context.Background()

// provider is a fake airtime provider: it answers with err, and counts the
// calls so a test can check nothing was sent twice.
// snippet:provider
type provider struct {
	err   error
	calls int
}

func (p *provider) Buy(context.Context, string, string, money.Money) error {
	p.calls++
	return p.err
}

// end-snippet

// snippet:outcomes
func TestBuy(t *testing.T) {
	for _, c := range []struct {
		name      string
		answer    error
		wantErr   error
		available string // what Ada can spend afterwards
	}{
		{"delivered", nil, nil, "4000.00"},
		{"declined", airtime.ErrDeclined, airtime.ErrDeclined, "5000.00"},
		{"no answer", errors.New("timeout"), airtime.ErrPending, "4000.00"}, // still held
	} {
		t.Run(c.name, func(t *testing.T) {
			hb := humanblnktest.New(t) // a fresh, empty Blnk for each case
			ada := hb.Customer(t, "NGN")
			hb.Fund(t, ada, "5000")

			air := airtime.New(hb.Holds, &provider{err: c.answer}, "vtpass")
			_, err := air.Buy(ctx, airtime.Purchase{
				ID: "air_1", Wallet: ada, Phone: "08031234567", Amount: humanblnktest.Money(t, "1000", "NGN"),
			})
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Buy: %v, want %v", err, c.wantErr)
			}
			if got := hb.Available(t, ada); got != c.available {
				t.Fatalf("Ada can spend %s, want %s", got, c.available)
			}
		})
	}
}

// end-snippet

// A retried Buy charges once: Place returns the same hold, and a full
// capture can't move the money twice.
// snippet:retry
func TestBuyIsSafeToRetry(t *testing.T) {
	hb := humanblnktest.New(t)
	ada := hb.Customer(t, "NGN")
	hb.Fund(t, ada, "5000")
	air := airtime.New(hb.Holds, &provider{}, "vtpass")
	p := airtime.Purchase{ID: "air_1", Wallet: ada, Phone: "08031234567", Amount: humanblnktest.Money(t, "1000", "NGN")}

	first, err := air.Buy(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	again, err := air.Buy(ctx, p) // the same purchase, as a retry sends it
	if err != nil || again != first {
		t.Fatalf("retry: %q %v, want the same hold %q", again, err, first)
	}
	if got := hb.Available(t, ada); got != "4000.00" {
		t.Fatalf("Ada can spend %s after a retried Buy, want 4000.00", got)
	}
}

// end-snippet

// A customer who can't pay gets humanblnk's error, and nothing is sent.
// snippet:funds
func TestBuyWithoutFunds(t *testing.T) {
	hb := humanblnktest.New(t)
	ada := hb.Customer(t, "NGN") // never funded
	p := &provider{}
	air := airtime.New(hb.Holds, p, "vtpass")

	_, err := air.Buy(ctx, airtime.Purchase{ID: "air_1", Wallet: ada, Phone: "08031234567", Amount: humanblnktest.Money(t, "1000", "NGN")})
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("Buy: %v, want ErrInsufficientFunds", err)
	}
	if p.calls != 0 {
		t.Fatalf("the provider was called %d times for an unpaid purchase", p.calls)
	}
}

// end-snippet
