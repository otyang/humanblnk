package humanblnk_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
)

// A split whose second posting fails moves nothing, not even for a moment:
// Record holds it, and Blnk releases the first posting's hold before it
// answers. Its reference stays used, and a retry must say so, not "already
// done".
func TestContractRetryAfterRolledBackSplit(t *testing.T) {
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
	if a, b, c := w.available(t, ctx, ada), w.available(t, ctx, bola), w.available(t, ctx, shop); a != "100.00" || b != "5.00" || c != "0.00" {
		t.Fatalf("straight after the refusal: ada %s, bola %s, shop %s; want 100.00, 5.00, 0.00", a, b, c)
	}
	if got, err := w.hb.Accounts.Get(ctx, ada.ID); err != nil || got.Total.String() != "100.00" {
		t.Fatalf("ada's total straight after the refusal: %+v %v", got, err)
	}
	w.fund(t, ctx, bola, "10")
	if _, err := w.hb.Transactions.Record(ctx, payment); !errors.Is(err, humanblnk.ErrReferenceUsed) {
		t.Fatalf("retry: %v, want ErrReferenceUsed", err)
	}
	if e, err := w.hb.Transactions.GetByReference(ctx, payment.Ref); err != nil || e.Status != humanblnk.StatusRejected {
		t.Fatalf("GetByReference of the refused split: %+v %v", e, err)
	}
	// A new reference records it.
	payment.Ref = w.ref("split-rollback-2")
	if _, err := w.hb.Transactions.Record(ctx, payment); err != nil {
		t.Fatal(err)
	}
}

// Reversing part of a payout takes it back from @World, which is below zero.
func TestContractPartialReversalOfAPayout(t *testing.T) {
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
func TestContractFullAndPartialReversalsRace(t *testing.T) {
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
// one recipient spent their share, so nothing moves. A retry with the same
// Ref must reverse it properly, not report the failed one as done.
func TestContractRolledBackSplitReversal(t *testing.T) {
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
	if a, b := w.available(t, ctx, ada), w.available(t, ctx, bola); a != "0.00" || b != "50.00" {
		t.Fatalf("straight after the failed reversal: ada %s, bola %s; want 0.00, 50.00", a, b)
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

// Record holds a split and captures it, but it reads back as the one
// applied entry it is: by its id, by its reference, retried, and in List.
// Its postings and captures are parts of it, not entries to reverse alone.
func TestContractRecordedSplitReadsAsOneEntry(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	transfer := humanblnk.Transaction{
		Ref: w.ref("transfer-fee"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("11")),
		To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		Description: "transfer with fee",
	}
	e, err := w.hb.Transactions.Record(ctx, transfer)
	if err != nil || e.Status != humanblnk.StatusApplied || e.Ref != transfer.Ref || e.Amount.String() != "11.00" || len(e.To) != 2 {
		t.Fatalf("record: %+v %v", e, err)
	}
	if a, b := w.available(t, ctx, ada), w.available(t, ctx, bola); a != "89.00" || b != "10.00" {
		t.Fatalf("ada %s, bola %s", a, b)
	}
	for name, read := range map[string]func() (humanblnk.Entry, error){
		"Get":            func() (humanblnk.Entry, error) { return w.hb.Transactions.Get(ctx, e.ID) },
		"GetByReference": func() (humanblnk.Entry, error) { return w.hb.Transactions.GetByReference(ctx, transfer.Ref) },
	} {
		got, err := read()
		if err != nil || got.ID != e.ID || got.Ref != transfer.Ref || got.Status != humanblnk.StatusApplied || got.Amount.String() != "11.00" || len(got.To) != 2 {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
	}
	if again, err := w.hb.Transactions.Record(ctx, transfer); !errors.Is(err, humanblnk.ErrDuplicate) || again.ID != e.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if got := w.available(t, ctx, ada); got != "89.00" {
		t.Fatalf("ada %s after the retry", got)
	}

	// What moved into Bola's account is the capture, under the transfer's
	// reference; neither it nor the held posting can be reversed alone.
	list, _, err := w.hb.Transactions.List(ctx, humanblnk.EntryQuery{To: humanblnk.AccountRef(bola.ID), Status: humanblnk.StatusApplied})
	if err != nil || len(list) != 1 || list[0].Ref != transfer.Ref || list[0].Amount.String() != "10.00" {
		t.Fatalf("list %+v %v", list, err)
	}
	held, _, err := w.hb.Transactions.List(ctx, humanblnk.EntryQuery{To: humanblnk.AccountRef(bola.ID), Status: humanblnk.StatusHeld})
	if err != nil || len(held) != 1 {
		t.Fatalf("held %+v %v", held, err)
	}
	for _, part := range []string{list[0].ID, held[0].ID} {
		if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Entry: part, Reason: "refund"}); !errors.Is(err, humanblnk.ErrInvalid) {
			t.Fatalf("reversing part %s: %v", part, err)
		}
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("refund-fee"), Entry: e.ID, Reason: "refund"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Entry: e.ID, Reason: "refund"}); !errors.Is(err, humanblnk.ErrAlreadyReversed) {
		t.Fatalf("second reversal: %v", err)
	}
	if a, b := w.available(t, ctx, ada), w.available(t, ctx, bola); a != "100.00" || b != "0.00" {
		t.Fatalf("after the refund: ada %s, bola %s", a, b)
	}
}

// A RecordAll batch refused on its last item moves nothing, not even for a
// moment.
func TestContractRefusedRecordAllMovesNothing(t *testing.T) {
	ctx, w := setup(t)
	usdAcct, ngnAcct := w.account(t, ctx, iusd), w.account(t, ctx, ingn)
	w.fund(t, ctx, usdAcct, "10")
	fx := humanblnk.SystemAccount("FX")
	_, err := w.hb.Transactions.RecordAll(ctx, []humanblnk.Transaction{
		{Ref: w.ref("fx-usd-in"), From: humanblnk.Single(humanblnk.World, usdT("5")), To: humanblnk.Single(humanblnk.AccountRef(usdAcct.ID), usdT("5")), Description: "top up"},
		{Ref: w.ref("fx-usd-out"), From: humanblnk.Single(humanblnk.AccountRef(ngnAcct.ID), money.MustParse("1", ingn)), To: humanblnk.Single(fx, money.MustParse("1", ingn)), Description: "more than it has"},
	})
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("got %v", err)
	}
	if got := w.available(t, ctx, usdAcct); got != "10.00" {
		t.Fatalf("usd %s straight after the refusal, want 10.00", got)
	}
	if got, err := w.hb.Accounts.Get(ctx, usdAcct.ID); err != nil || got.Total.String() != "10.00" {
		t.Fatalf("usd total %+v %v", got, err)
	}
}

// A Record whose process stopped between holding a split and capturing it
// leaves the money reserved, not moved; retried, it captures the hold.
func TestContractRecordRetryFinishesAnUncapturedSplit(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	transfer := humanblnk.Transaction{
		Ref: w.ref("cut-short"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("11")),
		To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		Description: "transfer with fee",
	}
	// What Record's first step leaves when the process stops after it.
	cut := transfer
	cut.Meta = humanblnk.Meta{"ledger_recorded": transfer.Ref}
	if _, err := w.hb.Holds.Place(ctx, cut, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e, err := w.hb.Transactions.GetByReference(ctx, transfer.Ref); err != nil || e.Status != humanblnk.StatusHeld {
		t.Fatalf("cut short: %+v %v", e, err)
	}
	e, err := w.hb.Transactions.Record(ctx, transfer)
	if err != nil || e.Status != humanblnk.StatusApplied {
		t.Fatalf("retry: %+v %v", e, err)
	}
	if a, b := w.available(t, ctx, ada), w.available(t, ctx, bola); a != "89.00" || b != "10.00" {
		t.Fatalf("ada %s, bola %s", a, b)
	}
	if _, err := w.hb.Transactions.Record(ctx, transfer); !errors.Is(err, humanblnk.ErrDuplicate) {
		t.Fatalf("once more: %v", err)
	}
}

// Blnk commits a split hold a posting at a time. If it stops partway (here,
// the first posting committed on its own), committing the hold again
// commits the rest and answers ALREADY_COMMITTED; Capture finishes it.
func TestContractCaptureFinishesAPartlyCommittedSplit(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	h, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("half-committed"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("11")),
		To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		Description: "transfer with fee",
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c := blnk.New(w.url)
	legs, _, err := c.Transactions.Filter(ctx, blnk.FilterRequest{Filters: []blnk.Filter{{Field: "parent_transaction", Operator: "eq", Value: string(h)}}, Limit: 10})
	if err != nil || len(legs) != 2 {
		t.Fatalf("legs %+v %v", legs, err)
	}
	first := legs[0]
	for _, l := range legs {
		if strings.HasSuffix(l.Reference, "-1") {
			first = l
		}
	}
	if _, err := c.Inflight.Update(ctx, first.GetID(), blnk.UpdateInflightRequest{Status: blnk.ActionCommit, SkipQueue: true}); err != nil {
		t.Fatal(err)
	}
	e, err := w.hb.Holds.Capture(ctx, h, money.Money{})
	if err != nil || e.Status != humanblnk.StatusApplied || e.Amount.String() != "11.00" {
		t.Fatalf("capture: %+v %v", e, err)
	}
	if a, b := w.available(t, ctx, ada), w.available(t, ctx, bola); a != "89.00" || b != "10.00" {
		t.Fatalf("ada %s, bola %s; want 89.00, 10.00", a, b)
	}
	if got, err := w.hb.Accounts.Get(ctx, ada.ID); err != nil || got.Total.String() != "89.00" || got.Available.String() != "89.00" {
		t.Fatalf("ada %+v %v: nothing should be left held", got, err)
	}
}

// A split hold with one posting released (here voided on its own, as an
// expiry racing a capture could) can't be captured: committing it would move
// the other postings without it, so Capture refuses and moves nothing.
func TestContractCaptureRefusesAPartlyReleasedSplit(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	h, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("half-released"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("11")),
		To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		Description: "transfer with fee",
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c := blnk.New(w.url)
	legs, _, err := c.Transactions.Filter(ctx, blnk.FilterRequest{Filters: []blnk.Filter{{Field: "parent_transaction", Operator: "eq", Value: string(h)}}, Limit: 10})
	if err != nil || len(legs) != 2 {
		t.Fatalf("legs %+v %v", legs, err)
	}
	for _, l := range legs {
		if strings.HasSuffix(l.Reference, "-2") { // the fee
			if _, err := c.Inflight.Update(ctx, l.GetID(), blnk.UpdateInflightRequest{Status: blnk.ActionVoid, SkipQueue: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := w.hb.Holds.Capture(ctx, h, money.Money{}); !errors.Is(err, humanblnk.ErrHoldSettled) {
		t.Fatalf("capture: %v", err)
	}
	if b := w.available(t, ctx, bola); b != "0.00" {
		t.Fatalf("bola %s: part of the split moved", b)
	}
	if err := w.hb.Holds.Cancel(ctx, h); err != nil && !errors.Is(err, humanblnk.ErrHoldSettled) {
		t.Fatal(err)
	}
	if a := w.available(t, ctx, ada); a != "100.00" {
		t.Fatalf("ada %s after cancelling the rest, want 100.00", a)
	}
}
