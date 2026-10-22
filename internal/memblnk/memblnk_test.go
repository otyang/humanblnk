package memblnk_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/memblnk"
)

var ctx = context.Background()

// start returns a raw client on a fresh server, with a USD balance holding
// 100.00 funded from @World.
func start(t *testing.T, opts ...memblnk.Option) (*blnk.Client, string) {
	t.Helper()
	s := memblnk.New(opts...)
	t.Cleanup(s.Close)
	c := blnk.New(s.URL, blnk.WithRetries(0, 0))
	l, err := c.Ledgers.Create(ctx, blnk.CreateLedgerRequest{Name: "wallets"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Balances.Create(ctx, blnk.CreateBalanceRequest{LedgerID: l.LedgerID, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Transactions.Create(ctx, tx("fund", "@World", b.BalanceID, 10000, true)); err != nil {
		t.Fatal(err)
	}
	return c, b.BalanceID
}

func tx(ref, from, to string, minor int64, overdraft bool) blnk.CreateTransactionRequest {
	return blnk.CreateTransactionRequest{
		Reference: ref, Source: from, Destination: to, PreciseAmount: blnk.NewBigInt(minor), Precision: 100,
		Currency: "USD", Description: ref, AllowOverdraft: overdraft, SkipQueue: true,
	}
}

// settlesTo waits, up to a deadline generous enough for a loaded machine,
// for balance id to reach want.
func settlesTo(t *testing.T, c *blnk.Client, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := balance(t, c, id).Balance.String()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is %s, want %s", id, got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func balance(t *testing.T, c *blnk.Client, id string) *blnk.Balance {
	t.Helper()
	b, err := c.Balances.Get(ctx, id, blnk.GetBalanceOptions{WithQueued: true})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSystemAccountsAreMadeOnFirstUse(t *testing.T) {
	c, _ := start(t)
	w, err := c.Balances.GetByIndicator(ctx, "@World", "USD")
	if err != nil || w.LedgerID != memblnk.GeneralLedger || w.Balance.String() != "-10000" {
		t.Fatalf("@World: %+v %v", w, err)
	}
	if _, err := c.Balances.GetByIndicator(ctx, "@World", "EUR"); !blnk.IsNotFound(err) {
		t.Fatalf("@World in EUR before use: %v", err)
	}
}

func TestFundsAndDuplicates(t *testing.T) {
	c, ada := start(t)
	_, err := c.Transactions.Create(ctx, tx("too-much", ada, "@World", 20000, false))
	if !blnk.HasCode(err, blnk.CodeInsufficientFunds) {
		t.Fatalf("overspend: %v", err)
	}
	if _, err := c.Transactions.Create(ctx, tx("fund", "@World", ada, 1, true)); !blnk.HasCode(err, blnk.CodeDuplicateReference) {
		t.Fatalf("reused reference: %v", err)
	}
	if b := balance(t, c, ada); b.Balance.String() != "10000" || b.Version == 0 {
		t.Fatalf("balance after refusals: %+v", b)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	c, ada := start(t)
	r := tx("preview", ada, "@FeeIncome", 2500, false)
	r.Destination, r.Destinations = "", []blnk.Distribution{
		{Identifier: "@FeeIncome", PreciseDistribution: "500"},
		{Identifier: "@World", PreciseDistribution: "2000"},
	}
	p, err := c.Transactions.Preview(ctx, r)
	if err != nil || !p.WouldApply || len(p.Balances) != 3 || len(p.Legs) != 2 {
		t.Fatalf("%+v %v", p, err)
	}
	if got := p.Balances[0]; got.ResultingAvailable.String() != "7500" || got.CurrentAvailable.String() != "10000" {
		t.Fatalf("source %+v", got)
	}
	if !p.Balances[1].Virtual {
		t.Fatal("@FeeIncome doesn't exist yet, so it should be virtual")
	}
	if _, err := c.Balances.GetByIndicator(ctx, "@FeeIncome", "USD"); !blnk.IsNotFound(err) {
		t.Fatalf("preview made @FeeIncome: %v", err)
	}
	if _, err := c.Transactions.GetByReference(ctx, "preview-1"); !blnk.IsNotFound(err) {
		t.Fatalf("preview used the reference: %v", err)
	}
	r.PreciseAmount = blnk.NewBigInt(20500)
	r.Destinations[1].PreciseDistribution = "20000"
	if p, err := c.Transactions.Preview(ctx, r); err != nil || p.WouldApply || p.Rejection.Code != blnk.CodeInsufficientFunds {
		t.Fatalf("overspend preview %+v %v", p, err)
	}
}

func TestSplitIsOneTransactionPerPosting(t *testing.T) {
	c, ada := start(t)
	r := tx("split", ada, "", 3000, false)
	r.Destinations = []blnk.Distribution{
		{Identifier: "@World", PreciseDistribution: "1000"},
		{Identifier: "@FeeIncome", PreciseDistribution: "500"},
		{Identifier: "@World", PreciseDistribution: "1500"}, // merged with the first
	}
	parent, err := c.Transactions.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Transactions.Get(ctx, parent.GetID()); !blnk.IsNotFound(err) {
		t.Fatalf("the split itself is stored: %v", err)
	}
	legs, _, err := c.Transactions.Filter(ctx, blnk.FilterRequest{Filters: []blnk.Filter{{Field: "parent_transaction", Operator: "eq", Value: parent.GetID()}}})
	if err != nil || len(legs) != 2 {
		t.Fatalf("legs %+v %v", legs, err)
	}
	// Blnk stamps a split's postings with one created_at, so a newest-first
	// listing keeps them in the order they were recorded.
	if legs[0].Reference != "split-1" || !legs[0].CreatedAt.Equal(legs[1].CreatedAt.Time) {
		t.Fatalf("legs listed %s then %s, created %v and %v", legs[0].Reference, legs[1].Reference, legs[0].CreatedAt, legs[1].CreatedAt)
	}
	first, err := c.Transactions.GetByReference(ctx, "split-1")
	if err != nil || first.PreciseAmount.String() != "2500" || first.ParentTransaction != parent.GetID() {
		t.Fatalf("split-1 %+v %v", first, err)
	}
}

func TestFailedSplitIsRefundedLater(t *testing.T) {
	c, ada := start(t, memblnk.WithRollbackDelay(time.Second)) // long enough to look in between
	r := tx("split-fail", "", "@World", 15000, false)
	r.Atomic = true // as humanblnk sends every split
	r.Source, r.Sources = "", []blnk.Distribution{
		{Identifier: ada, PreciseDistribution: "6000"},
		{Identifier: ada, PreciseDistribution: "0"},
	}
	if _, err := c.Transactions.Create(ctx, r); !blnk.HasCode(err, blnk.CodeInvalidDistribution) {
		t.Fatalf("zero leg: %v", err)
	}
	bola, err := c.Balances.Create(ctx, blnk.CreateBalanceRequest{LedgerID: balance(t, c, ada).LedgerID, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	r.PreciseAmount = blnk.NewBigInt(16000)
	r.Sources = []blnk.Distribution{{Identifier: ada, PreciseDistribution: "6000"}, {Identifier: bola.BalanceID, PreciseDistribution: "10000"}}
	if _, err := c.Transactions.Create(ctx, r); !blnk.HasCode(err, blnk.CodeInsufficientFunds) {
		t.Fatalf("bola can't pay: %v", err)
	}
	if b := balance(t, c, ada); b.Balance.String() != "4000" {
		t.Fatalf("ada straight after: %s, want 4000 (her leg applied)", b.Balance.String())
	}
	settlesTo(t, c, ada, "10000") // refunded
	leg, err := c.Transactions.GetByReference(ctx, "split-fail-1")
	if err != nil || leg.Status != blnk.StatusApplied {
		t.Fatalf("the rolled-back leg %+v %v", leg, err)
	}
	if _, err := c.Transactions.GetByReference(ctx, leg.GetID()+"_refund"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if _, err := c.Transactions.Create(ctx, r); !blnk.HasCode(err, blnk.CodeDuplicateReference) {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestAtomicBatchRollsBack(t *testing.T) {
	c, ada := start(t, memblnk.WithRollbackDelay(20*time.Millisecond))
	_, err := c.Transactions.CreateBulk(ctx, blnk.BulkTransactionRequest{Atomic: true, SkipQueue: true, Transactions: []blnk.CreateTransactionRequest{
		tx("b1", ada, "@World", 6000, false),
		tx("b2", ada, "@World", 6000, false),
	}})
	if !blnk.HasCode(err, blnk.CodeInsufficientFunds) {
		t.Fatalf("batch: %v", err)
	}
	settlesTo(t, c, ada, "10000")
	res, err := c.Transactions.CreateBulk(ctx, blnk.BulkTransactionRequest{Atomic: true, SkipQueue: true, Transactions: []blnk.CreateTransactionRequest{
		tx("b3", ada, "@World", 1000, false), tx("b4", ada, "@World", 1000, false),
	}})
	if err != nil || res.TransactionCount != 2 || res.Status != "applied" {
		t.Fatalf("%+v %v", res, err)
	}
	item, err := c.Transactions.GetByReference(ctx, "b4")
	if err != nil || item.ParentTransaction != res.BatchID {
		t.Fatalf("item %+v %v", item, err)
	}
}

func TestHolds(t *testing.T) {
	c, ada := start(t)
	r := tx("hold", ada, "@World", 10000, false)
	r.Inflight = true
	h, err := c.Transactions.Create(ctx, r)
	if err != nil || h.Status != blnk.StatusInflight {
		t.Fatalf("%+v %v", h, err)
	}
	if b := balance(t, c, ada); b.Available().String() != "0" || b.Balance.String() != "10000" {
		t.Fatalf("while held: %+v", b)
	}
	commit := blnk.UpdateInflightRequest{Status: blnk.ActionCommit, PreciseAmount: blnk.NewBigInt(3000), SkipQueue: true}
	for range 2 { // a repeated partial commit applies again, as in Blnk
		if _, err := c.Inflight.Update(ctx, h.GetID(), commit); err != nil {
			t.Fatal(err)
		}
	}
	commit.PreciseAmount = blnk.NewBigInt(5000)
	if _, err := c.Inflight.Update(ctx, h.GetID(), commit); !blnk.HasCode(err, blnk.CodeCommitAmountExceeded) {
		t.Fatalf("over-commit: %v", err)
	}
	void := blnk.UpdateInflightRequest{Status: blnk.ActionVoid}
	if res, err := c.Inflight.Update(ctx, h.GetID(), void); err != nil || res.Status != blnk.StatusVoid || res.PreciseAmount.String() != "4000" {
		t.Fatalf("void %+v %v", res, err)
	}
	if _, err := c.Inflight.Update(ctx, h.GetID(), void); !blnk.HasCode(err, blnk.CodeAlreadyVoided) {
		t.Fatalf("second void: %v", err)
	}
	if b := balance(t, c, ada); b.Balance.String() != "4000" || b.Available().String() != "4000" {
		t.Fatalf("after: %+v", b)
	}
	if got, _ := c.Transactions.Get(ctx, h.GetID()); got.Status != blnk.StatusInflight {
		t.Fatalf("the hold is %s; it should stay INFLIGHT", got.Status)
	}
	if _, err := c.Inflight.Update(ctx, "fund", void); !blnk.IsNotFound(err) {
		t.Fatalf("void of an unknown id: %v", err)
	}
}

func TestHoldsExpire(t *testing.T) {
	c, ada := start(t)
	r := tx("expiring", ada, "@World", 4000, false)
	r.Inflight, r.InflightExpiryDate = true, time.Now().Add(time.Second).Format(time.RFC3339)
	h, err := c.Transactions.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if b := balance(t, c, ada); b.Available().String() != "10000" {
		t.Fatalf("after expiry: %s", b.Available())
	}
	if _, err := c.Inflight.Update(ctx, h.GetID(), blnk.UpdateInflightRequest{Status: blnk.ActionCommit}); !blnk.HasCode(err, blnk.CodeAlreadyVoided) {
		t.Fatalf("commit after expiry: %v", err)
	}
}

func TestMonitorsAndWebhooks(t *testing.T) {
	var mu sync.Mutex
	var got []humanblnk.Event
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ev, err := humanblnk.VerifyWebhook("s3cret", r, time.Minute)
		if err != nil {
			t.Errorf("webhook: %v", err)
			return
		}
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}))
	defer hook.Close()
	c, ada := start(t, memblnk.WithWebhook(hook.URL, "s3cret"))
	m, err := c.Monitors.Create(ctx, blnk.BalanceMonitor{
		BalanceID: ada, Description: "ignored",
		Condition: blnk.MonitorCondition{Field: "balance", Operator: "<", Value: "5000", Precision: 1},
	})
	if err != nil || m.Description != "" || m.Condition.PreciseValue.String() != "5000" {
		t.Fatalf("%+v %v", m, err)
	}
	// Update doesn't recompute precise_value, as in Blnk.
	m.Condition.Value, m.Description = "1", "low balance"
	if err := c.Monitors.Update(ctx, m.MonitorID, *m); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Monitors.Get(ctx, m.MonitorID); got.Condition.PreciseValue.String() != "5000" || got.Description != "low balance" {
		t.Fatalf("after update %+v", got)
	}
	if _, err := c.Transactions.Create(ctx, tx("spend", ada, "@World", 6000, false)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		var fired, applied bool
		for _, ev := range got {
			fired = fired || (ev.Kind == "balance.monitor" && ev.Watch == humanblnk.WatchID(m.MonitorID))
			applied = applied || (ev.Kind == "transaction.applied" && ev.Entry != nil && ev.Entry.Ref == "spend")
		}
		mu.Unlock()
		if fired && applied {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("webhooks %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReconciliationUpload(t *testing.T) {
	c, _ := start(t)
	csv := "id,amount,currency,reference,description,date\nps_1,100.00,USD,fund,fund," + time.Now().Format(time.RFC3339) + "\nps_2,5.00,USD,missing,x," + time.Now().Format(time.RFC3339) + "\n"
	up, err := c.Reconciliation.Upload(ctx, "paystack", "report.csv", strings.NewReader(csv))
	if err != nil || up.RecordCount != 2 {
		t.Fatalf("%+v %v", up, err)
	}
	rule, err := c.Reconciliation.CreateMatchingRule(ctx, blnk.MatchingRule{Name: "r", Criteria: []blnk.MatchingCriterion{
		{Field: "amount", Operator: "equals"}, {Field: "reference", Operator: "equals"}, {Field: "date", Operator: "equals"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.Reconciliation.Start(ctx, blnk.StartReconciliationRequest{UploadID: up.UploadID, Strategy: "one_to_one", MatchingRuleIDs: []string{rule.RuleID}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.Reconciliation.Get(ctx, id)
	if err != nil || run.Status != "completed" || run.MatchedTransactions != 1 || run.UnmatchedTransactions != 1 {
		t.Fatalf("%+v %v", run, err)
	}
	if _, err := c.Reconciliation.Start(ctx, blnk.StartReconciliationRequest{UploadID: up.UploadID, Strategy: "many_to_one", MatchingRuleIDs: []string{rule.RuleID}}); err == nil {
		t.Fatal("many_to_one should be refused")
	}
}

func TestMetadataAndFilters(t *testing.T) {
	c, ada := start(t)
	md, err := c.Metadata.Update(ctx, ada, blnk.MetaData{"tier": "gold"})
	if err != nil || md["tier"] != "gold" {
		t.Fatalf("%v %v", md, err)
	}
	md, err = c.Metadata.Update(ctx, ada, blnk.MetaData{"kyc": "done"})
	if err != nil || md["tier"] != "gold" || md["kyc"] != "done" {
		t.Fatalf("merge: %v %v", md, err)
	}
	if _, err := c.Metadata.Update(ctx, "bln_nope", blnk.MetaData{"x": 1}); !blnk.IsNotFound(err) {
		t.Fatalf("unknown entity: %v", err)
	}
	bs, n, err := c.Balances.Filter(ctx, blnk.FilterRequest{
		Filters:      []blnk.Filter{{Field: "meta_data.tier", Operator: "eq", Value: "gold"}, {Field: "balance", Operator: "gte", Value: 10000}},
		IncludeCount: true,
	})
	if err != nil || n != 1 || len(bs) != 1 || bs[0].BalanceID != ada {
		t.Fatalf("%+v %d %v", bs, n, err)
	}
	for _, f := range []blnk.Filter{
		{Field: "currency", Operator: "in", Values: []any{"EUR", "USD"}},
		{Field: "balance_id", Operator: "like", Value: "bln_%"},
		{Field: "identity_id", Operator: "isnull"},
	} {
		if bs, _, err := c.Balances.Filter(ctx, blnk.FilterRequest{Filters: []blnk.Filter{f, {Field: "balance_id", Operator: "eq", Value: ada}}}); err != nil || len(bs) != 1 {
			t.Fatalf("%s %s: %+v %v", f.Field, f.Operator, bs, err)
		}
	}
	if _, _, err := c.Balances.Filter(ctx, blnk.FilterRequest{Filters: []blnk.Filter{{Field: "x", Operator: "almost"}}}); err == nil {
		t.Fatal("unknown operator accepted")
	}
}

func TestUnknownRoutesAreNotFound(t *testing.T) {
	s := memblnk.New()
	defer s.Close()
	resp, err := http.Get(s.URL + "/mocked-account")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var e struct {
		ErrorDetail struct{ Code string } `json:"error_detail"`
	}
	if resp.StatusCode != http.StatusNotFound || json.Unmarshal(body, &e) != nil || e.ErrorDetail.Code != blnk.CodeNotFound {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}
