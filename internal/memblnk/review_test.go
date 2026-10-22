package memblnk_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/memblnk"
)

// Readers and writers at once: responses are encoded under the lock, so
// -race finds nothing, and metadata changes never race a read.
func TestConcurrentReadsAndWrites(t *testing.T) {
	c, ada := start(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			_, _ = c.Transactions.Create(ctx, tx(fmt.Sprintf("w%d", i), "@World", ada, 1, true))
		}()
		go func() { defer wg.Done(); _, _ = c.Balances.Get(ctx, ada, blnk.GetBalanceOptions{}) }()
		go func() { defer wg.Done(); _, _, _ = c.Balances.Filter(ctx, blnk.FilterRequest{}) }()
		go func() {
			defer wg.Done()
			_, _ = c.Metadata.Update(ctx, ada, blnk.MetaData{"n": i})
			_, _ = c.Transactions.GetByReference(ctx, "fund")
		}()
	}
	wg.Wait()
	if b := balance(t, c, ada); b.Balance.String() != "10020" {
		t.Fatalf("balance %s", b.Balance.String())
	}
}

// Text compares as text: customer "007" is not customer "7".
func TestFiltersCompareTextAsText(t *testing.T) {
	c, _ := start(t)
	for _, id := range []string{"007", "7"} {
		if _, err := c.Identities.Create(ctx, blnk.Identity{MetaData: blnk.MetaData{"customer_id": id}}); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := c.Identities.Filter(ctx, blnk.FilterRequest{Filters: []blnk.Filter{{Field: "meta_data.customer_id", Operator: "eq", Value: "7"}}})
	if err != nil || len(got) != 1 || got[0].MetaData["customer_id"] != "7" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestReferenceLookupIsShort(t *testing.T) {
	c, _ := start(t)
	short, err := c.Transactions.GetByReference(ctx, "fund")
	if err != nil || short.Precision.String() != "0" || short.AllowOverdraft {
		t.Fatalf("by reference: %+v %v", short, err)
	}
	full, err := c.Transactions.Get(ctx, short.GetID())
	if err != nil || full.Precision.String() != "100" || !full.AllowOverdraft {
		t.Fatalf("by id: %+v %v", full, err)
	}
}

func TestSplitAtomicity(t *testing.T) {
	c, ada := start(t, memblnk.WithRollbackDelay(10*time.Millisecond))
	l := balance(t, c, ada).LedgerID
	bola, _ := c.Balances.Create(ctx, blnk.CreateBalanceRequest{LedgerID: l, Currency: "USD"})
	split := func(ref string, atomic bool) error {
		r := tx(ref, "", "@World", 16000, false)
		r.Source, r.Atomic = "", atomic
		r.Sources = []blnk.Distribution{{Identifier: ada, PreciseDistribution: "6000"}, {Identifier: bola.BalanceID, PreciseDistribution: "10000"}}
		_, err := c.Transactions.Create(ctx, r)
		return err
	}
	if err := split("atomic", true); !blnk.HasCode(err, blnk.CodeInsufficientFunds) {
		t.Fatal(err)
	}
	settlesTo(t, c, ada, "10000") // an atomic split's applied leg is refunded
	leg, _ := c.Transactions.Get(ctx, mustRef(t, c, "atomic-1"))
	if leg.Atomic || leg.MetaData["atomic"] != true {
		t.Fatalf("an atomic split's posting, as Blnk stores it: atomic %v, meta %v", leg.Atomic, leg.MetaData)
	}
	if err := split("not-atomic", false); !blnk.HasCode(err, blnk.CodeInsufficientFunds) {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // twenty rollback delays: nothing is coming
	if b := balance(t, c, ada); b.Balance.String() != "4000" {
		t.Fatalf("a non-atomic split's applied leg stands: ada %s", b.Balance.String())
	}
}

func mustRef(t *testing.T, c *blnk.Client, ref string) string {
	t.Helper()
	tx, err := c.Transactions.GetByReference(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	return tx.GetID()
}

// One posting of a split hold voided, another committed: Blnk answers
// ALREADY_COMMITTED.
func TestSplitHoldMixedSettlement(t *testing.T) {
	c, ada := start(t)
	r := tx("split-hold", ada, "", 3000, false)
	r.Inflight, r.Atomic = true, true
	r.Destinations = []blnk.Distribution{{Identifier: "@World", PreciseDistribution: "2000"}, {Identifier: "@FeeIncome", PreciseDistribution: "1000"}}
	h, err := c.Transactions.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inflight.Update(ctx, mustRef(t, c, "split-hold-1"), blnk.UpdateInflightRequest{Status: blnk.ActionVoid}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inflight.Update(ctx, mustRef(t, c, "split-hold-2"), blnk.UpdateInflightRequest{Status: blnk.ActionCommit}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inflight.Update(ctx, h.GetID(), blnk.UpdateInflightRequest{Status: blnk.ActionCommit}); !blnk.HasCode(err, blnk.CodeAlreadyCommitted) {
		t.Fatalf("commit of the split: %v", err)
	}
}

// A malformed item fails the batch before anything moves.
func TestBatchIsCheckedFirst(t *testing.T) {
	c, ada := start(t)
	bad := tx("b2", ada, "@World", 100, false)
	bad.Description = ""
	_, err := c.Transactions.CreateBulk(ctx, blnk.BulkTransactionRequest{Atomic: true, SkipQueue: true, Transactions: []blnk.CreateTransactionRequest{tx("b1", ada, "@World", 100, false), bad}})
	if !blnk.HasCode(err, blnk.CodeTxnValidation) {
		t.Fatal(err)
	}
	if _, err := c.Transactions.GetByReference(ctx, "b1"); !blnk.IsNotFound(err) {
		t.Fatalf("b1 was applied: %v", err)
	}
}

// A webhook receiver that never answers doesn't stop Close.
func TestCloseCancelsWebhooks(t *testing.T) {
	block := make(chan struct{})
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer hook.Close()
	defer close(block)
	s := memblnk.New(memblnk.WithWebhook(hook.URL, "s"))
	c := blnk.New(s.URL, blnk.WithRetries(0, 0))
	l, _ := c.Ledgers.Create(ctx, blnk.CreateLedgerRequest{Name: "w"})
	b, _ := c.Balances.Create(ctx, blnk.CreateBalanceRequest{LedgerID: l.LedgerID, Currency: "USD"})
	if _, err := c.Transactions.Create(ctx, tx("fund", "@World", b.BalanceID, 100, true)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // the delivery is now blocked
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close waited on a blocked webhook receiver")
	}
}

// Reconciliation matches text as Blnk does: equals ignores case, contains
// is fuzzy within the drift.
func TestReconciliationTextMatching(t *testing.T) {
	c, _ := start(t) // "fund" is recorded, 100.00 USD
	rule := func(op string, drift float64) string {
		r, err := c.Reconciliation.CreateMatchingRule(ctx, blnk.MatchingRule{Name: op, Criteria: []blnk.MatchingCriterion{{Field: "reference", Operator: op, AllowableDrift: drift}}})
		if err != nil {
			t.Fatal(err)
		}
		return r.RuleID
	}
	matched := func(ref, rule string) int {
		id, err := c.Reconciliation.StartInstant(ctx, blnk.StartReconciliationRequest{Strategy: "one_to_one", MatchingRuleIDs: []string{rule},
			ExternalTransactions: []blnk.ExternalTransaction{{ID: "x", Amount: "100", Currency: "USD", Reference: ref, Date: time.Now().Format(time.RFC3339)}}})
		if err != nil {
			t.Fatal(err)
		}
		run, _ := c.Reconciliation.Get(ctx, id)
		return run.MatchedTransactions
	}
	equals, fuzzy := rule("equals", 0), rule("contains", 25)
	for _, tc := range []struct {
		ref, rule string
		want      int
	}{{"FUND", equals, 1}, {"fun", equals, 0}, {"fun", fuzzy, 1}, {"fumd", fuzzy, 1}, {"xyzw", fuzzy, 0}} {
		if got := matched(tc.ref, tc.rule); got != tc.want {
			t.Errorf("%q: matched %d, want %d", tc.ref, got, tc.want)
		}
	}
}
