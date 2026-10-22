//go:build integration

package humanblnk_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// These run against a real Blnk Core (deploy/docker-compose.yml):
//
//	BLNK_URL=http://localhost:5001 go test -tags integration .
//
// They check what the unit tests only assume from reading Core's source.

var (
	iusd = currency.MustGet(currency.USD)
	ingn = currency.MustGet(currency.NGN)
)

type world struct {
	hb  *humanblnk.Client
	run string // makes this run's references unique
}

func setup(t *testing.T) (context.Context, world) {
	t.Helper()
	url := os.Getenv("BLNK_URL")
	if url == "" {
		t.Skip("BLNK_URL not set")
	}
	ctx := context.Background()
	hb := humanblnk.New(blnk.New(url))
	run := fmt.Sprint(time.Now().UnixNano())
	book, err := hb.Ledgers.Create(ctx, "it-"+run, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, world{hb: humanblnk.New(blnk.New(url), humanblnk.WithAccountLedger(book.ID)), run: run}
}

func (w world) ref(kind string) string { return humanblnk.Reference(kind, w.run) }

func (w world) account(t *testing.T, ctx context.Context, c currency.Currency) humanblnk.Account {
	t.Helper()
	owner, err := w.hb.Identities.Create(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: c})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (w world) fund(t *testing.T, ctx context.Context, a humanblnk.Account, amount string) {
	t.Helper()
	m := money.MustParse(amount, a.Currency)
	funded++
	if _, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref(fmt.Sprintf("fund-%s-%d", a.ID, funded)), From: humanblnk.Single(humanblnk.World, m), To: humanblnk.Single(humanblnk.AccountRef(a.ID), m), Description: "fund",
	}); err != nil {
		t.Fatal(err)
	}
}

func (w world) available(t *testing.T, ctx context.Context, a humanblnk.Account) string {
	t.Helper()
	got, err := w.hb.Accounts.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Available.String()
}

// funded numbers fund calls, so each has its own reference.
var funded int

func usdT(s string) money.Money { return money.MustParse(s, iusd) }

func TestIntegrationRecordAndErrors(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("funded balance %s", got)
	}

	// skip_queue: insufficient funds is an immediate error.
	_, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("too-much"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("500")),
		To: humanblnk.Single(humanblnk.AccountRef(bola.ID), usdT("500")), Description: "too much",
	})
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}

	// A reused reference is ErrDuplicate.
	t1 := humanblnk.Transaction{
		Ref: w.ref("pay"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("10")),
		To: humanblnk.Single(humanblnk.AccountRef(bola.ID), usdT("10")), Description: "pay",
	}
	if _, err := w.hb.Transactions.Record(ctx, t1); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Record(ctx, t1); !errors.Is(err, humanblnk.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}

	// Preview reports the refusal without writing.
	p, err := w.hb.Transactions.Preview(ctx, humanblnk.Transaction{
		Ref: w.ref("preview"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("1000")),
		To: humanblnk.Single(humanblnk.AccountRef(bola.ID), usdT("1000")), Description: "preview",
	})
	if err != nil || p.WouldApply || !errors.Is(p.Rejection, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("preview %+v %v", p, err)
	}
}

func TestIntegrationSplitIsGathered(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "200")
	ref := w.ref("split")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: ref, From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("101.50")),
		To: humanblnk.Many(
			humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("100")},
			humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1.50")},
		),
		Description: "transfer with fee",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.hb.Transactions.Get(ctx, e.ID)
	if err != nil || got.Ref != ref || got.Amount.String() != "101.50" || len(got.To) != 2 {
		t.Fatalf("Get(split) %+v %v", got, err)
	}
	byRef, err := w.hb.Transactions.GetByReference(ctx, ref)
	if err != nil || byRef.ID != e.ID {
		t.Fatalf("GetByReference(split) %+v %v", byRef, err)
	}
	if b := w.available(t, ctx, bola); b != "100.00" {
		t.Fatalf("bola %s", b)
	}
}

func TestIntegrationHolds(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	h, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("hold"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "0.00" {
		t.Fatalf("available while held %s", got)
	}
	// Partial capture, then the rest is released.
	if _, err := w.hb.Holds.Capture(ctx, h, usdT("80")); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "20.00" {
		t.Fatalf("available after capturing 80 of 100: %s", got)
	}
	if _, err := w.hb.Holds.Capture(ctx, h, money.Money{}); !errors.Is(err, humanblnk.ErrHoldSettled) {
		t.Fatalf("second capture: %v", err)
	}

	// A split hold, captured in full.
	h2, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("hold-split"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("20")),
		To: humanblnk.Many(
			humanblnk.Posting{Account: humanblnk.World, Amount: usdT("19")},
			humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")},
		),
		Description: "payout with fee",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Holds.Capture(ctx, h2, money.Money{}); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "0.00" {
		t.Fatalf("available after split capture: %s", got)
	}

	// Cancel releases, and is safe to repeat.
	w.fund(t, ctx, ada, "5")
	h3, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("hold-cancel"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("5")),
		To: humanblnk.Single(humanblnk.World, usdT("5")), Description: "cancelled payout",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := w.hb.Holds.Cancel(ctx, h3); err != nil {
			t.Fatal(err)
		}
	}
	if got := w.available(t, ctx, ada); got != "5.00" {
		t.Fatalf("available after cancel: %s", got)
	}
}

func TestIntegrationReversals(t *testing.T) {
	ctx, w := setup(t)
	ada, shop := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("buy"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("100")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	r1 := humanblnk.Reversal{Ref: w.ref("refund-1"), Entry: e.ID, Amount: usdT("30"), Reason: "one item returned"}
	first, err := w.hb.Transactions.Reverse(ctx, r1)
	if err != nil {
		t.Fatal(err)
	}
	// Retrying the same reversal returns it, not a second refund.
	again, err := w.hb.Transactions.Reverse(ctx, r1)
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("refund-2"), Entry: e.ID, Amount: usdT("80"), Reason: "too much"}); !errors.Is(err, humanblnk.ErrAlreadyReversed) {
		t.Fatalf("over-reversal: %v", err)
	}
	// The rest, as a full reversal after a partial one.
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("refund-rest"), Entry: e.ID, Reason: "cancel order"}); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("ada after reversals: %s", got)
	}

	// A fresh entry reversed in full, once.
	e2, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("buy-2"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("10")),
		To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("10")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Entry: e2.ID, Reason: "refund"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Entry: e2.ID, Reason: "refund"}); !errors.Is(err, humanblnk.ErrAlreadyReversed) {
		t.Fatalf("second full reversal: %v", err)
	}
}

func TestIntegrationRecordAllAndList(t *testing.T) {
	ctx, w := setup(t)
	usdAcct, ngnAcct := w.account(t, ctx, iusd), w.account(t, ctx, ingn)
	w.fund(t, ctx, usdAcct, "10")
	fx := humanblnk.SystemAccount("FX")
	es, err := w.hb.Transactions.RecordAll(ctx, []humanblnk.Transaction{
		{Ref: w.ref("fx-usd"), From: humanblnk.Single(humanblnk.AccountRef(usdAcct.ID), usdT("10")), To: humanblnk.Single(fx, usdT("10")), Description: "sell USD"},
		{Ref: w.ref("fx-ngn"), From: humanblnk.Single(fx, money.MustParse("16000", ingn)), To: humanblnk.Single(humanblnk.AccountRef(ngnAcct.ID), money.MustParse("16000", ingn)), Description: "buy NGN"},
	})
	if err != nil || len(es) != 2 || es[0].ID == "" || es[1].ID == "" {
		t.Fatalf("%+v %v", es, err)
	}
	if got := w.available(t, ctx, ngnAcct); got != "16000.00" {
		t.Fatalf("ngn %s", got)
	}
	list, total, err := w.hb.Transactions.List(ctx, humanblnk.EntryQuery{To: humanblnk.AccountRef(ngnAcct.ID)})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list %+v %d %v", list, total, err)
	}
	accts, _, err := w.hb.Accounts.List(ctx, humanblnk.AccountQuery{Currency: ingn})
	if err != nil || len(accts) == 0 {
		t.Fatalf("accounts %v %v", accts, err)
	}
}

func TestIntegrationWatches(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	watch, err := w.hb.Watches.Create(ctx, humanblnk.Watch{
		Account: humanblnk.AccountRef(ada.ID), Field: humanblnk.WatchBalance, Op: humanblnk.WatchBelow,
		Threshold: usdT("10"), Description: "low balance",
	})
	if err != nil || watch.Threshold.String() != "10.00" {
		t.Fatalf("%+v %v", watch, err)
	}
	got, err := w.hb.Watches.Get(ctx, watch.ID)
	if err != nil || got.Threshold.String() != "10.00" || got.Description != "low balance" {
		t.Fatalf("get %+v %v", got, err)
	}
	changed, err := w.hb.Watches.Change(ctx, watch.ID, humanblnk.Watch{
		Account: humanblnk.AccountRef(ada.ID), Field: humanblnk.WatchBalance, Op: humanblnk.WatchBelow, Threshold: usdT("5"),
	})
	if err != nil || changed.Threshold.String() != "5.00" {
		t.Fatalf("change %+v %v", changed, err)
	}
	if _, err := w.hb.Watches.Get(ctx, watch.ID); !errors.Is(err, humanblnk.ErrNotFound) {
		t.Fatalf("old watch still there: %v", err)
	}
	if err := w.hb.Watches.Delete(ctx, changed.ID); err != nil {
		t.Fatal(err)
	}
}

// Concurrent partial reversals take turns: ten $30 refunds of a $100
// purchase at once, and exactly three go through.
func TestIntegrationConcurrentReversals(t *testing.T) {
	ctx, w := setup(t)
	ada, shop := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("buy-race"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("100")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 10)
	for i := range 10 {
		go func() {
			_, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{
				Ref: w.ref(fmt.Sprintf("race-%d", i)), Entry: e.ID, Amount: usdT("30"), Reason: "race",
			})
			results <- err
		}()
	}
	ok, over := 0, 0
	for range 10 {
		switch err := <-results; {
		case err == nil:
			ok++
		case errors.Is(err, humanblnk.ErrAlreadyReversed):
			over++
		default:
			t.Logf("other outcome: %v", err)
		}
	}
	if ok != 3 {
		t.Fatalf("%d reversals went through (%d refused); want exactly 3", ok, over)
	}
	if got := w.available(t, ctx, ada); got != "90.00" {
		t.Fatalf("ada has %s after three $30 refunds of $100; want 90.00", got)
	}
}

// A deposit and the provider's statement line for it reconcile.
func TestIntegrationReconciliation(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	ref := w.ref("deposit-ps")
	if _, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: ref, From: humanblnk.Single(humanblnk.ClearingAccount("paystack"), usdT("25")),
		To: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("25")), Description: "deposit",
	}); err != nil {
		t.Fatal(err)
	}
	rule, err := w.hb.Reconciliation.AddRule(ctx, humanblnk.MatchRule{
		Name: "amount and reference " + w.run,
		Criteria: []humanblnk.Criterion{
			{Field: humanblnk.MatchAmount, Op: humanblnk.MatchEquals},
			{Field: humanblnk.MatchReference, Op: humanblnk.MatchEquals},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := w.hb.Reconciliation.RunLines(ctx, []humanblnk.StatementLine{{
		ID: "ps_" + w.run, Amount: usdT("25"), Reference: ref, Date: time.Now(), Source: "paystack",
	}}, humanblnk.Run{Rules: []humanblnk.RuleID{rule.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var res humanblnk.RunResult
	for range 50 {
		if res, err = w.hb.Reconciliation.Result(ctx, id); err == nil && (res.Status == "completed" || res.Status == "failed") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || res.Status != "completed" || res.Matched != 1 {
		t.Fatalf("result %+v %v", res, err)
	}
	if err := w.hb.Reconciliation.RemoveRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
}
