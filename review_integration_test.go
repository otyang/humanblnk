//go:build integration

package humanblnk_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
)

// A split whose second posting fails is rolled back by Blnk, but its first
// posting's reference stays used. A retry must say so, not "already done".
func TestIntegrationRetryAfterRolledBackSplit(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	shop := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	w.fund(t, ctx, bola, "5")
	// Many to one: Ada's 95 posts first, then Bola's 10, which she can't cover.
	payment := humanblnk.Transaction{
		Ref: w.ref("split-rollback"),
		From: humanblnk.Many(
			humanblnk.Posting{Account: humanblnk.AccountRef(ada.ID), Amount: usdT("95")},
			humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")},
		),
		To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("105")), Description: "group payment",
	}
	_, err := w.hb.Transactions.Record(ctx, payment)
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("first attempt: %v", err)
	}
	// Blnk refunds the applied posting through its queue, a moment later.
	start := time.Now()
	for w.available(t, ctx, ada) != "100.00" {
		if time.Since(start) > 10*time.Second {
			t.Fatalf("ada after the rollback: %s", w.available(t, ctx, ada))
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("rollback refund landed after %v", time.Since(start).Round(time.Millisecond))
	w.fund(t, ctx, bola, "10")
	if _, err := w.hb.Transactions.Record(ctx, payment); !errors.Is(err, humanblnk.ErrReferenceUsed) {
		t.Fatalf("retry: %v, want ErrReferenceUsed", err)
	}
	if e, err := w.hb.Transactions.GetByReference(ctx, payment.Ref); err == nil && e.Status == humanblnk.StatusApplied {
		t.Fatalf("GetByReference calls the rolled-back split %s", e.Status)
	}
	// A new reference records it.
	payment.Ref = w.ref("split-rollback-2")
	if _, err := w.hb.Transactions.Record(ctx, payment); err != nil {
		t.Fatal(err)
	}
}

// Reversing part of a payout takes it back from @World, which is below zero.
func TestIntegrationPartialReversalOfAPayout(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100") // @World goes below zero here
	payout, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("payout"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("bounce-part"), Entry: payout.ID, Amount: usdT("40"), Reason: "partly bounced"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("bounce-rest"), Entry: payout.ID, Reason: "rest bounced"}); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("ada after the payout bounced: %s", got)
	}
}

// Full and partial reversals racing never undo more than the entry moved.
func TestIntegrationFullAndPartialReversalsRace(t *testing.T) {
	ctx, w := setup(t)
	ada, shop := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("buy-mixed"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("100")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 6)
	for i := range 6 {
		go func() {
			r := humanblnk.Reversal{Ref: w.ref(fmt.Sprintf("mixed-%d", i)), Entry: e.ID, Reason: "race"}
			if i%2 == 1 {
				r.Amount = usdT("40")
			}
			_, err := w.hb.Transactions.Reverse(ctx, r)
			done <- err
		}()
	}
	for range 6 {
		if err := <-done; err != nil && !errors.Is(err, humanblnk.ErrAlreadyReversed) {
			t.Logf("other outcome: %v", err)
		}
	}
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("ada has %s: reversals undid more or less than the $100 entry", got)
	}
	if got := w.available(t, ctx, shop); got != "0.00" {
		t.Fatalf("shop has %s, want 0.00", got)
	}
}

// The second review's case, live: a split reversal fails partway because
// one recipient spent their share, and Blnk rolls it back. A retry with the
// same Ref must reverse it properly, not report the failed one as done.
func TestIntegrationRolledBackSplitReversal(t *testing.T) {
	ctx, w := setup(t)
	ada, bola, cara := w.account(t, ctx, iusd), w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("pay-two"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Many(
			humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("50")},
			humanblnk.Posting{Account: humanblnk.AccountRef(cara.ID), Amount: usdT("50")},
		),
		Description: "split payment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := w.hb.Transactions.Get(ctx, e.ID); err != nil || got.Status != humanblnk.StatusApplied || got.Amount.String() != "100.00" {
		t.Fatalf("the split, read back: %+v %v", got, err)
	}
	// Cara spends hers, so the reversal can't take it back.
	if _, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("cara-spends"), From: humanblnk.Single(humanblnk.AccountRef(cara.ID), usdT("50")),
		To: humanblnk.Single(humanblnk.World, usdT("50")), Description: "spent",
	}); err != nil {
		t.Fatal(err)
	}
	refund := humanblnk.Reversal{Ref: w.ref("refund-split"), Entry: e.ID, Reason: "order cancelled"}
	if _, err := w.hb.Transactions.Reverse(ctx, refund); !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("first attempt: %v", err)
	}
	// Whatever part applied is rolled back a moment later.
	t.Logf("bola straight after the failed reversal: %s (0.00 means her posting applied and is being rolled back)", w.available(t, ctx, bola))
	start := time.Now()
	for w.available(t, ctx, bola) != "50.00" {
		if time.Since(start) > 10*time.Second {
			t.Fatalf("bola after the rollback: %s", w.available(t, ctx, bola))
		}
		time.Sleep(100 * time.Millisecond)
	}

	w.fund(t, ctx, cara, "50")
	if _, err := w.hb.Transactions.Reverse(ctx, refund); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("ada after the refund: %s, want all 100.00 back", got)
	}
	if b, c := w.available(t, ctx, bola), w.available(t, ctx, cara); b != "0.00" || c != "0.00" {
		t.Fatalf("bola %s, cara %s after the refund; want 0.00 each", b, c)
	}
	// Once more with the same Ref: the same reversal, nothing moves.
	if _, err := w.hb.Transactions.Reverse(ctx, refund); err != nil || w.available(t, ctx, ada) != "100.00" {
		t.Fatalf("repeat: %v, ada %s", err, w.available(t, ctx, ada))
	}
}
