package humanblnk_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// The contract tests check what the unit tests only assume from reading
// Core's source. They run against the in-memory Blnk on every go test, and
// against a real Blnk Core (deploy/docker-compose.yml) with the tag:
//
//	BLNK_URL=http://localhost:5001 go test -tags integration .
//
// Passing against both is what keeps the in-memory Blnk honest.

var (
	iusd = currency.MustGet(currency.USD)
	ingn = currency.MustGet(currency.NGN)
)

type world struct {
	hb  *humanblnk.Client
	url string // the Blnk it runs against
	run string // makes this run's references unique
}

func setup(t *testing.T) (context.Context, world) {
	t.Helper()
	url := backend(t)
	ctx := context.Background()
	hb := humanblnk.New(blnk.New(url))
	run := fmt.Sprint(time.Now().UnixNano())
	book, err := hb.Ledgers.Create(ctx, "it-"+run, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, world{hb: humanblnk.New(blnk.New(url), humanblnk.WithAccountLedger(book.ID)), url: url, run: run}
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

func TestContractRecordAndErrors(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("funded balance %s", got)
	}

	// skip_queue: insufficient funds is an immediate error.
	tooMuch := humanblnk.Transaction{
		Ref: w.ref("too-much"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("500")),
		To: humanblnk.Single(humanblnk.AccountRef(bola.ID), usdT("500")), Description: "too much",
	}
	_, err := w.hb.Transactions.Record(ctx, tooMuch)
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
	// The refusal leaves the reference unused: once the money is there, the
	// same write records.
	w.fund(t, ctx, ada, "500")
	if _, err := w.hb.Transactions.Record(ctx, tooMuch); err != nil {
		t.Fatalf("the refused write, retried with the money there: %v", err)
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

func TestContractSplitIsGathered(t *testing.T) {
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

func TestContractHolds(t *testing.T) {
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

func TestContractReversals(t *testing.T) {
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

func TestContractRecordAllAndList(t *testing.T) {
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

func TestContractWatches(t *testing.T) {
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
func TestContractConcurrentReversals(t *testing.T) {
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
func TestContractReconciliation(t *testing.T) {
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

// Blnk applies a repeated partial commit again: the request carries nothing
// that marks it as a repeat. This is why Holds.Capture looks for an earlier
// capture before committing. If Blnk starts refusing the repeat, this fails:
// Capture can then rely on Blnk instead.
func TestContractBlnkRepeatsPartialCommit(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	h, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("hold-repeat"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	raw := blnk.New(w.url)
	commit30 := blnk.UpdateInflightRequest{Status: blnk.ActionCommit, PreciseAmount: blnk.NewBigInt(3000), SkipQueue: true}
	for i := range 2 { // the second is a client's retry after a timeout
		res, err := raw.Inflight.Update(ctx, string(h), commit30)
		if err != nil {
			t.Fatalf("commit %d: %v", i+1, err)
		}
		t.Logf("commit %d: %s %s %s", i+1, res.Transaction.GetID(), res.Transaction.Status, res.Transaction.PreciseAmount.String())
	}
	got, err := w.hb.Accounts.Get(ctx, ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total.String() != "40.00" {
		t.Fatalf("total after committing 30 twice: %s, want 40.00 (both applied)", got.Total)
	}
	held, err := w.hb.Transactions.Get(ctx, string(h))
	if err != nil {
		t.Fatal(err)
	}
	if held.Status != humanblnk.StatusHeld {
		t.Fatalf("the hold itself is %s; package holds assumes it stays %s", held.Status, humanblnk.StatusHeld)
	}
}

// Repeating Place and Capture with the same arguments moves nothing more.
func TestContractHoldRepeats(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	payout := humanblnk.Transaction{
		Ref: w.ref("hold-retry"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	}
	h, err := w.hb.Holds.Place(ctx, payout, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if again, err := w.hb.Holds.Place(ctx, payout, time.Time{}); err != nil || again != h {
		t.Fatalf("second Place: %q %v, want %q", again, err, h)
	}

	first, err := w.hb.Holds.Capture(ctx, h, usdT("30"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.hb.Holds.Capture(ctx, h, usdT("30"))
	if err != nil || second.ID != first.ID {
		t.Fatalf("second Capture: %+v %v, want %s", second, err, first.ID)
	}
	if _, err := w.hb.Holds.Capture(ctx, h, money.Money{}); err == nil {
		t.Fatal("capturing the rest after a partial capture succeeded")
	}
	if got := w.available(t, ctx, ada); got != "70.00" {
		t.Fatalf("available after capturing 30 twice: %s, want 70.00", got)
	}
}

// Opening the same customer account many times at once, as retries and
// double-clicks do, gives every caller the same account.
func TestContractOpenForCustomerRace(t *testing.T) {
	ctx, w := setup(t)
	owner, err := w.hb.Identities.Create(ctx, humanblnk.Meta{"customer_id": "cus_" + w.run})
	if err != nil {
		t.Fatal(err)
	}
	found, err := w.hb.Identities.FindByCustomer(ctx, "cus_"+w.run)
	if err != nil || found.ID != owner {
		t.Fatalf("FindByCustomer: %+v %v, want %s", found, err, owner)
	}
	if _, err := w.hb.Identities.UpdateMeta(ctx, owner, humanblnk.Meta{"tier": "gold"}); err != nil {
		t.Fatal(err)
	}
	if got, err := w.hb.Identities.Get(ctx, owner); err != nil || got.Meta["tier"] != "gold" || got.Meta["customer_id"] != "cus_"+w.run {
		t.Fatalf("after UpdateMeta: %+v %v", got, err)
	}
	want := humanblnk.CustomerAccount{Owner: owner, Currency: iusd}

	const n = 8
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := w.hb.Accounts.OpenForCustomer(ctx, want)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = a.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("callers got different accounts: %v", ids)
		}
	}

	// Later opens return it too, and another currency is a second account.
	again, err := w.hb.Accounts.OpenForCustomer(ctx, want)
	if err != nil || again.ID != ids[0] {
		t.Fatalf("reopen: %+v %v", again, err)
	}
	ngn, err := w.hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: ingn})
	if err != nil || ngn.ID == ids[0] {
		t.Fatalf("NGN: %+v %v", ngn, err)
	}
	all, _, err := w.hb.Accounts.List(ctx, humanblnk.AccountQuery{Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	spares := 0
	for _, a := range all {
		if a.Meta["duplicate_of"] != nil {
			spares++
		}
	}
	// One process takes turns, so the race leaves no spares.
	if len(all) != 2 || spares != 0 {
		t.Fatalf("%d accounts, %d spares; want USD and NGN only", len(all), spares)
	}
}

// A reference with "/" in it, as bank and statement references often have,
// is found again: a retry is ErrDuplicate, never "not found".
func TestContractReferenceWithSlash(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	deposit := humanblnk.Transaction{
		Ref: w.ref("INV/2026/0042"), From: humanblnk.Single(humanblnk.World, usdT("5")),
		To: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("5")), Description: "deposit",
	}
	e, err := w.hb.Transactions.Record(ctx, deposit)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := w.hb.Transactions.GetByReference(ctx, deposit.Ref); err != nil || got.ID != e.ID {
		t.Fatalf("GetByReference: %+v %v", got, err)
	}
	if _, err := w.hb.Transactions.Record(ctx, deposit); !errors.Is(err, humanblnk.ErrDuplicate) {
		t.Fatalf("retry: %v, want ErrDuplicate", err)
	}
}

// A split and a single transaction can't share a reference, whichever comes
// first, though Blnk records a split's postings as ref-1, ref-2 and would
// accept both.
func TestContractSplitAndSingleCantShareAReference(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	split := func(ref string) humanblnk.Transaction {
		return humanblnk.Transaction{
			Ref: ref, From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("11")),
			To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
			Description: "transfer with fee",
		}
	}
	single := func(ref string) humanblnk.Transaction {
		return humanblnk.Transaction{Ref: ref, From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("1")), To: humanblnk.Single(humanblnk.AccountRef(bola.ID), usdT("1")), Description: "pay"}
	}
	x := w.ref("shared-x")
	if _, err := w.hb.Transactions.Record(ctx, split(x)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Record(ctx, single(x)); !errors.Is(err, humanblnk.ErrReferenceUsed) {
		t.Fatalf("a single under a split's reference: %v", err)
	}
	y := w.ref("shared-y")
	if _, err := w.hb.Transactions.Record(ctx, single(y)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Record(ctx, split(y)); !errors.Is(err, humanblnk.ErrReferenceUsed) {
		t.Fatalf("a split under a single's reference: %v", err)
	}
	// "<z>-1" taken by a single: a split under z is refused, not "check again".
	z := w.ref("shared-z")
	if _, err := w.hb.Transactions.Record(ctx, single(z+"-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Record(ctx, split(z)); !errors.Is(err, humanblnk.ErrReferenceUsed) {
		t.Fatalf("a split whose first posting's reference is taken: %v", err)
	}
	if got := w.available(t, ctx, ada); got != "87.00" {
		t.Fatalf("ada %s: a refused write moved money", got)
	}
}

// A batch whose items are "<ref>-1" and "<ref>-2" is not an entry recorded
// under ref.
func TestContractBatchIsNotASplit(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "10")
	order := w.ref("order")
	if _, err := w.hb.Transactions.RecordAll(ctx, []humanblnk.Transaction{
		{Ref: order + "-1", From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("1")), To: humanblnk.Single(humanblnk.World, usdT("1")), Description: "a"},
		{Ref: order + "-2", From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("2")), To: humanblnk.Single(humanblnk.World, usdT("2")), Description: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if e, err := w.hb.Transactions.GetByReference(ctx, order); !errors.Is(err, humanblnk.ErrNotFound) {
		t.Fatalf("GetByReference(%s) = %+v %v, want ErrNotFound", order, e, err)
	}
}

// A transfer with a fee can be refunded after the day's fees were swept out
// of @FeeIncome: its reversal takes from a customer and from @FeeIncome,
// which may go below zero though the customer may not.
func TestContractRefundAfterFeesSwept(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("pay-fee"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("11")),
		To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("10")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		Description: "transfer with fee",
	})
	if err != nil {
		t.Fatal(err)
	}
	fees, err := w.hb.Accounts.System(ctx, humanblnk.FeeIncome, iusd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("sweep"), From: humanblnk.Single(humanblnk.FeeIncome, fees.Total), To: humanblnk.Single(humanblnk.World, fees.Total), Description: "fee sweep",
	}); err != nil {
		t.Fatal(err)
	}
	refund := humanblnk.Reversal{Ref: w.ref("refund-fee"), Entry: e.ID, Reason: "transfer cancelled"}
	r, err := w.hb.Transactions.Reverse(ctx, refund)
	if err != nil {
		t.Fatalf("refund after the sweep: %v", err)
	}
	if a, b := w.available(t, ctx, ada), w.available(t, ctx, bola); a != "100.00" || b != "0.00" {
		t.Fatalf("ada %s, bola %s after the refund", a, b)
	}
	if again, err := w.hb.Transactions.Reverse(ctx, refund); err != nil || again.ID != r.ID {
		t.Fatalf("repeat: %+v %v, want %s", again, err, r.ID)
	}
	if got, err := w.hb.Transactions.Get(ctx, r.ID); err != nil || got.Status != humanblnk.StatusApplied || len(got.From) != 2 {
		t.Fatalf("the refund read back: %+v %v", got, err)
	}
	// A customer among the sources still can't overdraw.
	_, err = w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("bola-short"), From: humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT("5")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		To: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("6")), Description: "bola can't pay",
	})
	if !errors.Is(err, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("bola overdrawn: %v", err)
	}
}

// A hold taking from a customer and a system account (a purchase with a
// promotion's subsidy) is held, captured and cancelled like any split hold.
func TestContractMixedSourceHold(t *testing.T) {
	ctx, w := setup(t)
	ada, shop := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "10")
	promo := humanblnk.SystemAccount("Promotions")
	buy := func(ref string) humanblnk.Transaction {
		return humanblnk.Transaction{
			Ref: w.ref(ref), From: humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(ada.ID), Amount: usdT("8")}, humanblnk.Posting{Account: promo, Amount: usdT("2")}),
			To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("10")), Description: "purchase with subsidy",
		}
	}
	h, err := w.hb.Holds.Place(ctx, buy("promo-buy"), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if again, err := w.hb.Holds.Place(ctx, buy("promo-buy"), time.Time{}); err != nil || again != h {
		t.Fatalf("repeat Place: %q %v, want %q", again, err, h)
	}
	if got := w.available(t, ctx, ada); got != "2.00" {
		t.Fatalf("ada while held: %s", got)
	}
	c, err := w.hb.Holds.Capture(ctx, h, money.Money{})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, shop); got != "10.00" {
		t.Fatalf("shop after capture: %s", got)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("promo-refund"), Entry: c.ID, Reason: "returned"}); err != nil {
		t.Fatalf("reversing the captured hold: %v", err)
	}
	if a, s := w.available(t, ctx, ada), w.available(t, ctx, shop); a != "10.00" || s != "0.00" {
		t.Fatalf("after the refund: ada %s, shop %s", a, s)
	}
	h2, err := w.hb.Holds.Place(ctx, buy("promo-cancel"), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.hb.Holds.Cancel(ctx, h2); err != nil {
		t.Fatal(err)
	}
	if got := w.available(t, ctx, ada); got != "10.00" {
		t.Fatalf("ada after cancel: %s", got)
	}
	if _, err := w.hb.Holds.Place(ctx, buy("promo-cancel"), time.Time{}); !errors.Is(err, humanblnk.ErrHoldSettled) {
		t.Fatalf("Place after Cancel: %v, want ErrHoldSettled", err)
	}
}

// A captured hold is reversed by the id Capture returned, or the hold's.
func TestContractReverseCapturedHolds(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")

	// A split hold: a payout and its fee, captured, then bounced.
	split, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("payout-fee"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("20")),
		To:          humanblnk.Many(humanblnk.Posting{Account: humanblnk.World, Amount: usdT("19")}, humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: usdT("1")}),
		Description: "payout with fee",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("too-soon"), Entry: string(split), Reason: "x"}); !errors.Is(err, humanblnk.ErrInvalid) {
		t.Fatalf("reversing a hold not yet captured: %v", err)
	}
	c, err := w.hb.Holds.Capture(ctx, split, money.Money{})
	if err != nil {
		t.Fatal(err)
	}
	bounced := humanblnk.Reversal{Ref: w.ref("bounced"), Entry: c.ID, Reason: "bank returned it"}
	if _, err := w.hb.Transactions.Reverse(ctx, bounced); err != nil {
		t.Fatalf("reversing the captured split hold: %v", err)
	}
	if got := w.available(t, ctx, ada); got != "100.00" {
		t.Fatalf("ada after the bounce: %s", got)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("bounced-2"), Entry: c.ID, Reason: "again"}); !errors.Is(err, humanblnk.ErrAlreadyReversed) {
		t.Fatalf("second reversal: %v", err)
	}

	// A single hold, partly captured: its id and its capture's reach the
	// same total, so the two can't add up to more than was captured.
	single, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("payout"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("50")),
		To: humanblnk.Single(humanblnk.World, usdT("50")), Description: "payout",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	cap30, err := w.hb.Holds.Capture(ctx, single, usdT("30"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("part-a"), Entry: string(single), Amount: usdT("20"), Reason: "part"}); err != nil {
		t.Fatalf("by the hold's id: %v", err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: w.ref("part-b"), Entry: cap30.ID, Amount: usdT("20"), Reason: "part"}); !errors.Is(err, humanblnk.ErrAlreadyReversed) {
		t.Fatalf("by the capture's id, past what was captured: %v", err)
	}
	if got := w.available(t, ctx, ada); got != "90.00" {
		t.Fatalf("ada: %s, want 90.00 (100 - 30 captured + 20 back)", got)
	}
}

// A reversal's Ref names one reversal: reusing it for another amount is
// ErrReferenceUsed, not the first reversal handed back as if it were this.
func TestContractReversalRefIsOneReversal(t *testing.T) {
	ctx, w := setup(t)
	ada, shop := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "10")
	e, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: w.ref("buy"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("10")),
		To: humanblnk.Single(humanblnk.AccountRef(shop.ID), usdT("10")), Description: "purchase",
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := w.ref("rf1")
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: ref, Entry: e.ID, Amount: usdT("1"), Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: ref, Entry: e.ID, Amount: usdT("2"), Reason: "x"}); !errors.Is(err, humanblnk.ErrReferenceUsed) {
		t.Fatalf("the same Ref for 2.00: %v", err)
	}
	if got := w.available(t, ctx, ada); got != "1.00" {
		t.Fatalf("ada %s", got)
	}
}

// Captures of one hold for different amounts, at once: exactly one wins,
// and only its amount moves.
func TestContractConcurrentCaptures(t *testing.T) {
	ctx, w := setup(t)
	ada := w.account(t, ctx, iusd)
	w.fund(t, ctx, ada, "100")
	h, err := w.hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref: w.ref("hold-race"), From: humanblnk.Single(humanblnk.AccountRef(ada.ID), usdT("100")),
		To: humanblnk.Single(humanblnk.World, usdT("100")), Description: "payout",
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	amounts := []string{"30", "40", "50"}
	won := make(chan string, len(amounts))
	var wg sync.WaitGroup
	for _, a := range amounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.hb.Holds.Capture(ctx, h, usdT(a))
			switch {
			case err == nil:
				won <- a
			case !errors.Is(err, humanblnk.ErrHoldSettled):
				t.Errorf("capture %s: %v", a, err)
			}
		}()
	}
	wg.Wait()
	close(won)
	var winners []string
	for a := range won {
		winners = append(winners, a)
	}
	if len(winners) != 1 {
		t.Fatalf("captures that went through: %v, want exactly one", winners)
	}
	want, _ := usdT("100").Sub(usdT(winners[0]))
	if got := w.available(t, ctx, ada); got != want.String() {
		t.Fatalf("ada %s after capturing %s of 100, want %s", got, winners[0], want)
	}
}

// Entries are listed by a system account's name, in every currency it was
// used in, and the account is read by name.
func TestContractListBySystemAccount(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, ingn)
	ps := humanblnk.ClearingAccount("ps" + w.run)
	for _, d := range []struct {
		to humanblnk.Account
		m  money.Money
	}{{ada, usdT("5")}, {bola, money.MustParse("700", ingn)}} {
		if _, err := w.hb.Transactions.Record(ctx, humanblnk.Transaction{
			Ref: w.ref("dep-" + d.m.Code()), From: humanblnk.Single(ps, d.m), To: humanblnk.Single(humanblnk.AccountRef(d.to.ID), d.m), Description: "deposit",
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, total, err := w.hb.Transactions.List(ctx, humanblnk.EntryQuery{From: ps})
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("from %s: %d %+v %v", ps, total, list, err)
	}
	if list, _, err := w.hb.Transactions.List(ctx, humanblnk.EntryQuery{From: humanblnk.SystemAccount("Never" + w.run)}); err != nil || len(list) != 0 {
		t.Fatalf("an unused system account: %+v %v", list, err)
	}
	owed, err := w.hb.Accounts.System(ctx, ps, ingn)
	if err != nil || owed.Total.String() != "-700.00" {
		t.Fatalf("%s in NGN: %+v %v", ps, owed, err)
	}
	if _, err := w.hb.Accounts.System(ctx, ps, currency.MustGet(currency.EUR)); !errors.Is(err, humanblnk.ErrNotFound) {
		t.Fatalf("in a currency it was never used in: %v", err)
	}
}

// Preview of a split from a customer and a system account tells what Record
// will do: the system account may go short, the customer may not.
func TestContractPreviewMixedSources(t *testing.T) {
	ctx, w := setup(t)
	ada, bola := w.account(t, ctx, iusd), w.account(t, ctx, iusd)
	w.fund(t, ctx, bola, "10")
	pay := func(fromBola string) humanblnk.Transaction {
		total, _ := usdT(fromBola).Add(usdT("1"))
		return humanblnk.Transaction{
			Ref:         w.ref("preview-" + fromBola),
			From:        humanblnk.Many(humanblnk.Posting{Account: humanblnk.AccountRef(bola.ID), Amount: usdT(fromBola)}, humanblnk.Posting{Account: humanblnk.SystemAccount("Promo" + w.run), Amount: usdT("1")}),
			To:          humanblnk.Single(humanblnk.AccountRef(ada.ID), total),
			Description: "with a subsidy",
		}
	}
	p, err := w.hb.Transactions.Preview(ctx, pay("5"))
	if err != nil || !p.WouldApply || p.Rejection != nil {
		t.Fatalf("preview %+v %v", p, err)
	}
	var adaAfter string
	for _, c := range p.Changes {
		if c.Account == humanblnk.AccountRef(ada.ID) {
			adaAfter = c.AvailableAfter.String()
		}
	}
	if adaAfter != "6.00" {
		t.Fatalf("ada would have %q, want 6.00: %+v", adaAfter, p.Changes)
	}
	if p, err := w.hb.Transactions.Preview(ctx, pay("50")); err != nil || p.WouldApply || !errors.Is(p.Rejection, humanblnk.ErrInsufficientFunds) {
		t.Fatalf("bola can't cover 50: %+v %v", p, err)
	}
	if got := w.available(t, ctx, ada); got != "0.00" {
		t.Fatalf("a preview moved money: ada %s", got)
	}
}
