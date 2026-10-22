//go:build integration

package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// These run against a real Blnk Core (deploy/docker-compose.yml):
//
//	BLNK_URL=http://localhost:5001 go test -tags integration ./ledger/
//
// They check what the unit tests only assume from reading Core's source.

var (
	iusd = currency.MustGet(currency.USD)
	ingn = currency.MustGet(currency.NGN)
)

type world struct {
	books *ledger.Ledger
	run   string // makes this run's references unique
}

func setup(t *testing.T) (context.Context, world) {
	t.Helper()
	url := os.Getenv("BLNK_URL")
	if url == "" {
		t.Skip("BLNK_URL not set")
	}
	ctx := context.Background()
	books := ledger.New(blnk.New(url))
	run := fmt.Sprint(time.Now().UnixNano())
	book, err := books.Books.Create(ctx, "it-"+run, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, world{books: ledger.New(blnk.New(url), ledger.WithAccountLedger(book.ID)), run: run}
}

func (w world) ref(kind string) string { return ledger.Reference(kind, w.run) }

func (w world) account(t *testing.T, ctx context.Context, c currency.Currency) ledger.Account {
	t.Helper()
	owner, err := w.books.Identities.Create(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.books.Accounts.OpenForCustomer(ctx, ledger.CustomerAccount{Owner: owner, Currency: c})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (w world) fund(t *testing.T, ctx context.Context, a ledger.Account, amount string) {
	t.Helper()
	m := money.MustParse(amount, a.Currency)
	funded++
	if _, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: w.ref(fmt.Sprintf("fund-%s-%d", a.ID, funded)), From: ledger.Single(ledger.World, m), To: ledger.Single(ledger.AccountRef(a.ID), m), Description: "fund",
	}); err != nil {
		t.Fatal(err)
	}
}

func (w world) available(t *testing.T, ctx context.Context, a ledger.Account) string {
	t.Helper()
	got, err := w.books.Accounts.Get(ctx, a.ID)
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
	_, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: w.ref("too-much"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("500")),
		To: ledger.Single(ledger.AccountRef(bola.ID), usdT("500")), Description: "too much",
	})
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}

	// A reused reference is ErrDuplicate.
	t1 := ledger.Transaction{
		Ref: w.ref("pay"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("10")),
		To: ledger.Single(ledger.AccountRef(bola.ID), usdT("10")), Description: "pay",
	}
	if _, err := w.books.Transactions.Record(ctx, t1); err != nil {
		t.Fatal(err)
	}
	if _, err := w.books.Transactions.Record(ctx, t1); !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}

	// Preview reports the refusal without writing.
	p, err := w.books.Transactions.Preview(ctx, ledger.Transaction{
		Ref: w.ref("preview"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("1000")),
		To: ledger.Single(ledger.AccountRef(bola.ID), usdT("1000")), Description: "preview",
	})
	if err != nil || p.WouldApply || !errors.Is(p.Rejection, ledger.ErrInsufficientFunds) {
		t.Fatalf("preview %+v %v", p, err)
	}
}

func TestIntegrationSplitIsGathered(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "200")
	ref := w.ref("split")
	e, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: ref, From: ledger.Single(ledger.AccountRef(ada.ID), usdT("101.50")),
		To: ledger.Many(
			ledger.Posting{Account: ledger.AccountRef(bola.ID), Amount: usdT("100")},
			ledger.Posting{Account: ledger.FeeIncome, Amount: usdT("1.50")},
		),
		Description: "transfer with fee",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.books.Transactions.Get(ctx, e.ID)
	if err != nil || got.Ref != ref || got.Amount.String() != "101.50" || len(got.To) != 2 {
		t.Fatalf("Get(split) %+v %v", got, err)
	}
	byRef, err := w.books.Transactions.GetByReference(ctx, ref)
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
	h, err := w.books.Holds.Place(ctx, ledger.Transaction{
		Ref: w.ref("hold"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("100")),
		To: ledger.Single(ledger.World, usdT("100")), Description: "payout",
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "0.00" {
		t.Fatalf("available while held %s", got)
	}
	// Partial capture, then the rest is released.
	if _, err := w.books.Holds.Capture(ctx, h, usdT("80")); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "20.00" {
		t.Fatalf("available after capturing 80 of 100: %s", got)
	}
	if _, err := w.books.Holds.Capture(ctx, h, money.Money{}); !errors.Is(err, ledger.ErrHoldSettled) {
		t.Fatalf("second capture: %v", err)
	}

	// A split hold, captured in full.
	h2, err := w.books.Holds.Place(ctx, ledger.Transaction{
		Ref: w.ref("hold-split"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("20")),
		To: ledger.Many(
			ledger.Posting{Account: ledger.World, Amount: usdT("19")},
			ledger.Posting{Account: ledger.FeeIncome, Amount: usdT("1")},
		),
		Description: "payout with fee",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.books.Holds.Capture(ctx, h2, money.Money{}); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "0.00" {
		t.Fatalf("available after split capture: %s", got)
	}

	// Cancel releases, and is safe to repeat.
	w.fund(t, ctx, ada, "5")
	h3, err := w.books.Holds.Place(ctx, ledger.Transaction{
		Ref: w.ref("hold-cancel"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("5")),
		To: ledger.Single(ledger.World, usdT("5")), Description: "cancelled payout",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := w.books.Holds.Cancel(ctx, h3); err != nil {
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
	e, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: w.ref("buy"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("100")),
		To: ledger.Single(ledger.AccountRef(shop.ID), usdT("100")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	r1 := ledger.Reversal{Ref: w.ref("refund-1"), Entry: e.ID, Amount: usdT("30"), Reason: "one item returned"}
	first, err := w.books.Transactions.Reverse(ctx, r1)
	if err != nil {
		t.Fatal(err)
	}
	// Retrying the same reversal returns it, not a second refund.
	again, err := w.books.Transactions.Reverse(ctx, r1)
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if _, err := w.books.Transactions.Reverse(ctx, ledger.Reversal{Ref: w.ref("refund-2"), Entry: e.ID, Amount: usdT("80"), Reason: "too much"}); !errors.Is(err, ledger.ErrAlreadyReversed) {
		t.Fatalf("over-reversal: %v", err)
	}
	// The rest, as a full reversal after a partial one.
	if _, err := w.books.Transactions.Reverse(ctx, ledger.Reversal{Ref: w.ref("refund-rest"), Entry: e.ID, Reason: "cancel order"}); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("ada after reversals: %s", got)
	}

	// A fresh entry reversed in full, once.
	e2, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: w.ref("buy-2"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("10")),
		To: ledger.Single(ledger.AccountRef(shop.ID), usdT("10")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.books.Transactions.Reverse(ctx, ledger.Reversal{Entry: e2.ID, Reason: "refund"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.books.Transactions.Reverse(ctx, ledger.Reversal{Entry: e2.ID, Reason: "refund"}); !errors.Is(err, ledger.ErrAlreadyReversed) {
		t.Fatalf("second full reversal: %v", err)
	}
}

func TestIntegrationRecordAllAndList(t *testing.T) {
	ctx, w := setup(t)
	usdAcct, ngnAcct := w.account(t, ctx, iusd), w.account(t, ctx, ingn)
	w.fund(t, ctx, usdAcct, "10")
	fx := ledger.SystemAccount("FX")
	es, err := w.books.Transactions.RecordAll(ctx, []ledger.Transaction{
		{Ref: w.ref("fx-usd"), From: ledger.Single(ledger.AccountRef(usdAcct.ID), usdT("10")), To: ledger.Single(fx, usdT("10")), Description: "sell USD"},
		{Ref: w.ref("fx-ngn"), From: ledger.Single(fx, money.MustParse("16000", ingn)), To: ledger.Single(ledger.AccountRef(ngnAcct.ID), money.MustParse("16000", ingn)), Description: "buy NGN"},
	})
	if err != nil || len(es) != 2 || es[0].ID == "" || es[1].ID == "" {
		t.Fatalf("%+v %v", es, err)
	}
	if got := w.available(t, ctx, ngnAcct); got != "16000.00" {
		t.Fatalf("ngn %s", got)
	}
	list, total, err := w.books.Transactions.List(ctx, ledger.EntryQuery{To: ledger.AccountRef(ngnAcct.ID)})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list %+v %d %v", list, total, err)
	}
	accts, _, err := w.books.Accounts.List(ctx, ledger.AccountQuery{Currency: ingn})
	if err != nil || len(accts) == 0 {
		t.Fatalf("accounts %v %v", accts, err)
	}
}

func TestIntegrationWatches(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	watch, err := w.books.Watches.Create(ctx, ledger.Watch{
		Account: ledger.AccountRef(ada.ID), Field: ledger.WatchBalance, Op: ledger.WatchBelow,
		Threshold: usdT("10"), Description: "low balance",
	})
	if err != nil || watch.Threshold.String() != "10.00" {
		t.Fatalf("%+v %v", watch, err)
	}
	got, err := w.books.Watches.Get(ctx, watch.ID)
	if err != nil || got.Threshold.String() != "10.00" || got.Description != "low balance" {
		t.Fatalf("get %+v %v", got, err)
	}
	changed, err := w.books.Watches.Change(ctx, watch.ID, ledger.Watch{
		Account: ledger.AccountRef(ada.ID), Field: ledger.WatchBalance, Op: ledger.WatchBelow, Threshold: usdT("5"),
	})
	if err != nil || changed.Threshold.String() != "5.00" {
		t.Fatalf("change %+v %v", changed, err)
	}
	if _, err := w.books.Watches.Get(ctx, watch.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("old watch still there: %v", err)
	}
	if err := w.books.Watches.Delete(ctx, changed.ID); err != nil {
		t.Fatal(err)
	}
}

// Concurrent partial reversals take turns: ten $30 refunds of a $100
// purchase at once, and exactly three go through.
func TestIntegrationConcurrentReversals(t *testing.T) {
	ctx, w := setup(t)
	ada, shop := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	e, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: w.ref("buy-race"), From: ledger.Single(ledger.AccountRef(ada.ID), usdT("100")),
		To: ledger.Single(ledger.AccountRef(shop.ID), usdT("100")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 10)
	for i := range 10 {
		go func() {
			_, err := w.books.Transactions.Reverse(ctx, ledger.Reversal{
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
		case errors.Is(err, ledger.ErrAlreadyReversed):
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
	if _, err := w.books.Transactions.Record(ctx, ledger.Transaction{
		Ref: ref, From: ledger.Single(ledger.ClearingAccount("paystack"), usdT("25")),
		To: ledger.Single(ledger.AccountRef(ada.ID), usdT("25")), Description: "deposit",
	}); err != nil {
		t.Fatal(err)
	}
	rule, err := w.books.Reconciliation.AddRule(ctx, ledger.MatchRule{
		Name: "amount and reference " + w.run,
		Criteria: []ledger.Criterion{
			{Field: ledger.MatchAmount, Op: ledger.MatchEquals},
			{Field: ledger.MatchReference, Op: ledger.MatchEquals},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := w.books.Reconciliation.RunLines(ctx, []ledger.StatementLine{{
		ID: "ps_" + w.run, Amount: usdT("25"), Reference: ref, Date: time.Now(), Source: "paystack",
	}}, ledger.Run{Rules: []ledger.RuleID{rule.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var res ledger.RunResult
	for range 50 {
		if res, err = w.books.Reconciliation.Result(ctx, id); err == nil && (res.Status == "completed" || res.Status == "failed") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || res.Status != "completed" || res.Matched != 1 {
		t.Fatalf("result %+v %v", res, err)
	}
	if err := w.books.Reconciliation.RemoveRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
}
