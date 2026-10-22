//go:build integration

package humanblnk_test

import (
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
)

// Blnk applies a repeated partial commit again: the request carries nothing
// that marks it as a repeat. This is why Holds.Capture looks for an earlier
// capture before committing. If Blnk starts refusing the repeat, this fails:
// Capture can then rely on Blnk instead.
func TestIntegrationBlnkRepeatsPartialCommit(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	h, err := w.books.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("hold-repeat"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	raw := blnk.New(os.Getenv("BLNK_URL"))
	commit30 := blnk.UpdateInflightRequest{Status: blnk.ActionCommit, PreciseAmount: blnk.NewBigInt(3000), SkipQueue: true}
	for i := range 2 { // the second is a client's retry after a timeout
		res, err := raw.Inflight.Update(ctx, string(h), commit30)
		if err != nil {
			t.Fatalf("commit %d: %v", i+1, err)
		}
		t.Logf("commit %d: %s %s %s", i+1, res.Transaction.GetID(), res.Transaction.Status, res.Transaction.PreciseAmount.String())
	}
	got, err := w.books.Accounts.Get(ctx, ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total.String() != "40.00" {
		t.Fatalf("total after committing 30 twice: %s, want 40.00 (both applied)", got.Total)
	}
	held, err := w.books.Transactions.Get(ctx, string(h))
	if err != nil {
		t.Fatal(err)
	}
	if held.Status != humanblnk.StatusHeld {
		t.Fatalf("the hold itself is %s; package holds assumes it stays %s", held.Status, humanblnk.StatusHeld)
	}
}

// Repeating Place and Capture with the same arguments moves nothing more.
func TestIntegrationHoldRepeats(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	payout := humanblnk.Transaction{
		Ref: w.ref("hold-retry"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	}
	h, err := w.books.Holds.Place(ctx, payout, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if again, err := w.books.Holds.Place(ctx, payout, time.Time{}); err != nil || again != h {
		t.Fatalf("second Place: %q %v, want %q", again, err, h)
	}

	first, err := w.books.Holds.Capture(ctx, h, usdT("30"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.books.Holds.Capture(ctx, h, usdT("30"))
	if err != nil || second.ID != first.ID {
		t.Fatalf("second Capture: %+v %v, want %s", second, err, first.ID)
	}
	if _, err := w.books.Holds.Capture(ctx, h, money.Money{}); err == nil {
		t.Fatal("capturing the rest after a partial capture succeeded")
	}
	if got := w.available(t, ctx, ada); got != "70.00" {
		t.Fatalf("available after capturing 30 twice: %s, want 70.00", got)
	}
}
