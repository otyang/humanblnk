package transactions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/clients/blnk/blnkfake"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

const appliedTxn = `{"transaction_id":"txn_1","reference":"pay:1","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"bln_usd2"}`

// reversals sets up txn_1 with partial reversals totalling partial cents.
func reversals(f *blnkfake.Fake, partial string) {
	f.On("GET /transactions/txn_1", 200, appliedTxn)
	f.OnFunc("POST /transactions/filter", func(blnkfake.Call) (int, string) {
		if partial == "" {
			return 200, `[]`
		}
		return 200, `[{"transaction_id":"r1","reference":"reversal:txn_1:1","status":"APPLIED","currency":"USD","precise_amount":` + partial + `,"precision":100,"source":"bln_usd2","destination":"bln_usd","meta_data":{"reversal_of":"txn_1"}}]`
	})
}

func TestFullReversalIsANumberedReversal(t *testing.T) {
	s, f := newService(t)
	reversals(f, "")
	f.On("POST /transactions", 201, `{"transaction_id":"txn_r","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	e, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1", Entry: "txn_1", Reason: "customer refund"})
	if err != nil || e.ID != "txn_r" {
		t.Fatalf("%+v %v", e, err)
	}
	got := f.Sent("POST /transactions")[0].Body
	meta, _ := got["meta_data"].(map[string]any)
	if got["reference"] != "reversal:txn_1:1" || got["source"] != "bln_usd2" || got["destination"] != "bln_usd" ||
		got["precise_amount"] != float64(10000) || meta["reversal_ref"] != "refund:o1" || meta["reversal_reason"] != "customer refund" {
		t.Fatalf("sent %v", got)
	}
	if n := len(f.Sent("POST /refund-transaction/txn_1")); n != 0 {
		t.Fatalf("%d Blnk refunds", n)
	}
}

// A full reversal retried with its Ref gets its own reversal back.
func TestFullReversalRepeatReturnsIt(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/txn_1", 200, appliedTxn)
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"txn_r","reference":"reversal:txn_1:1","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,
		"source":"bln_usd2","destination":"bln_usd","meta_data":{"reversal_of":"txn_1","reversal_ref":"refund:o1","reversal_reason":"x"}}]`)
	f.On("GET /transactions/reference/reversal:txn_1:1", 200, `{"transaction_id":"txn_r"}`)
	f.On("GET /transactions/txn_r", 200, `{"transaction_id":"txn_r","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	e, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1", Entry: "txn_1", Reason: "x"})
	if err != nil || e.ID != "txn_r" || len(f.Sent("POST /transactions")) != 0 {
		t.Fatalf("%+v %v", e, err)
	}
	// Without the Ref, nothing is left: ErrAlreadyReversed.
	if _, err := s.Reverse(context.Background(), types.Reversal{Entry: "txn_1", Reason: "x"}); !errors.Is(err, types.ErrAlreadyReversed) {
		t.Fatalf("no ref: %v", err)
	}
}

// Reversing a split undoes every posting in one entry.
func TestFullReversalOfASplit(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/split1", 404, blnkfake.APIError("TXN_NOT_FOUND"))
	f.OnFunc("POST /transactions/filter", func(c blnkfake.Call) (int, string) {
		if c.Body["filters"].([]any)[0].(map[string]any)["field"] == "parent_transaction" {
			return 200, `[{"transaction_id":"l1","reference":"pay:2-1","parent_transaction":"split1","status":"APPLIED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd","destination":"bln_usd2"},
				{"transaction_id":"l2","reference":"pay:2-2","parent_transaction":"split1","status":"APPLIED","currency":"USD","precise_amount":500,"precision":100,"source":"bln_usd","destination":"@FeeIncome"}]`
		}
		return 200, `[]` // no reversals yet
	})
	f.On("POST /transactions", 201, `{"transaction_id":"rv","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"destination":"bln_usd","sources":[{"identifier":"bln_usd2","precise_distribution":"9500"},{"identifier":"@FeeIncome","precise_distribution":"500"}]}`)
	e, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o2", Entry: "split1", Reason: "x"})
	if err != nil || len(e.From) != 2 || e.Amount.String() != "100.00" {
		t.Fatalf("%+v %v", e, err)
	}
	got := f.Sent("POST /transactions")[0].Body
	if got["destination"] != "bln_usd" || len(got["sources"].([]any)) != 2 || got["reference"] != "reversal:split1:1" {
		t.Fatalf("sent %v", got)
	}
}

// A payout's entry names @World by balance id; reversing it may still
// take @World below zero.
func TestReversingAPayoutMayOverdrawWorld(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/txn_p", 200, `{"transaction_id":"txn_p","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"bln_world"}`)
	f.On("GET /balances/bln_world", 200, `{"balance_id":"bln_world","ledger_id":"general_ledger_id","indicator":"@World","currency":"USD","balance":-500000}`)
	f.On("POST /transactions/filter", 200, `[]`)
	f.On("POST /transactions", 201, `{"transaction_id":"rv","status":"APPLIED","currency":"USD","precise_amount":4000,"precision":100,"source":"bln_world","destination":"bln_usd"}`)
	if _, err := s.Reverse(context.Background(), types.Reversal{Ref: "bounce:1", Entry: "txn_p", Amount: money.MustParse("40", blnkfake.USD), Reason: "bounced"}); err != nil {
		t.Fatal(err)
	}
	if got := f.Sent("POST /transactions")[0].Body; got["allow_overdraft"] != true || got["source"] != "bln_world" {
		t.Fatalf("sent %v", got)
	}
}

func TestPartialReversalMovesMoneyBack(t *testing.T) {
	s, f := newService(t)
	reversals(f, "")
	f.On("POST /transactions", 201, `{"transaction_id":"txn_p","status":"APPLIED","currency":"USD","precise_amount":3000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	_, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1:1", Entry: "txn_1", Amount: money.MustParse("30", blnkfake.USD), Reason: "one item returned"})
	if err != nil {
		t.Fatal(err)
	}
	got := f.Sent("POST /transactions")[0].Body
	meta, _ := got["meta_data"].(map[string]any)
	if got["source"] != "bln_usd2" || got["destination"] != "bln_usd" || got["precise_amount"] != float64(3000) || meta["reversal_of"] != "txn_1" {
		t.Fatalf("sent %v", got)
	}
}

func TestReversalsCantExceedTheEntry(t *testing.T) {
	s, f := newService(t)
	reversals(f, "7000") // $70 of $100 already reversed
	_, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1:2", Entry: "txn_1", Amount: money.MustParse("40", blnkfake.USD), Reason: "x"})
	if !errors.Is(err, types.ErrAlreadyReversed) {
		t.Fatalf("over-reversal: %v", err)
	}

	// A full reversal after partial ones undoes only what's left, as a posting.
	f.On("POST /transactions", 201, `{"transaction_id":"txn_p","status":"APPLIED","currency":"USD","precise_amount":3000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	if _, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1:rest", Entry: "txn_1", Reason: "rest"}); err != nil {
		t.Fatal(err)
	}
	if got := f.Sent("POST /transactions")[0].Body; got["precise_amount"] != float64(3000) || len(f.Sent("POST /refund-transaction/txn_1")) != 0 {
		t.Fatalf("sent %v", got)
	}
}

// An entry Blnk rolled back (refunded under "<id>_refund") never applied.
func TestRolledBackEntryCantBeReversed(t *testing.T) {
	s, f := newService(t)
	reversals(f, "")
	// Blnk's rollback copies the original's metadata, which may well include
	// a reversal_reason; it's a rollback all the same.
	f.On("GET /transactions/reference/txn_1_refund", 200, `{"transaction_id":"txn_rb","meta_data":{"reversal_reason":"copied"}}`)
	_, err := s.Reverse(context.Background(), types.Reversal{Ref: "r", Entry: "txn_1", Amount: money.MustParse("1", blnkfake.USD), Reason: "x"})
	if !errors.Is(err, types.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

// The second review's case: a split reversal Blnk rolled back. Its refund
// and the refund's queue copy inherit the reversal's metadata, as Blnk's
// do; none of it counts, and a retry with the same Ref reverses again.
func TestRolledBackSplitReversalDoesntCount(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/split1", 404, blnkfake.APIError("TXN_NOT_FOUND"))
	meta := `"meta_data":{"reversal_of":"split1","reversal_ref":"refund:o2","reversal_reason":"x","ledger_split_postings":"2"}`
	f.OnFunc("POST /transactions/filter", func(c blnkfake.Call) (int, string) {
		if c.Body["filters"].([]any)[0].(map[string]any)["field"] == "parent_transaction" {
			return 200, `[{"transaction_id":"l1","reference":"pay:2-1","parent_transaction":"split1","status":"APPLIED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd","destination":"bln_usd2","meta_data":{"ledger_split_postings":"2"}},
				{"transaction_id":"l2","reference":"pay:2-2","parent_transaction":"split1","status":"APPLIED","currency":"USD","precise_amount":500,"precision":100,"source":"bln_usd","destination":"@FeeIncome","meta_data":{"ledger_split_postings":"2"}}]`
		}
		// Reversal 1's first posting applied, its second failed; Blnk
		// refunded the first (and queued a copy), all with its metadata.
		return 200, `[{"transaction_id":"rv1","reference":"reversal:split1:1-1","status":"APPLIED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd2","destination":"bln_usd",` + meta + `},
			{"transaction_id":"rb","reference":"rv1_refund","parent_transaction":"rv1","status":"QUEUED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd","destination":"bln_usd2",` + meta + `},
			{"transaction_id":"rbq","reference":"rv1_refund_q","parent_transaction":"rb","status":"APPLIED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd","destination":"bln_usd2",` + meta + `}]`
	})
	f.On("GET /transactions/reference/rv1_refund", 200, `{"transaction_id":"rb"}`)
	f.On("POST /transactions", 201, `{"transaction_id":"rv2","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"destination":"bln_usd","sources":[{"identifier":"bln_usd2","precise_distribution":"9500"},{"identifier":"@FeeIncome","precise_distribution":"500"}]}`)
	e, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o2", Entry: "split1", Reason: "x"})
	if err != nil || e.ID != "rv2" {
		t.Fatalf("%+v %v", e, err)
	}
	got := f.Sent("POST /transactions")[0].Body
	if got["reference"] != "reversal:split1:2" || got["precise_amount"] != float64(10000) || len(got["sources"].([]any)) != 2 {
		t.Fatalf("sent %v: want the whole split reversed as number 2", got)
	}
}

func TestConcurrentPartialReversalsTakeTurns(t *testing.T) {
	s, f := newService(t)
	reversals(f, "")
	// Another reversal claimed number 1 first: Blnk refuses the duplicate
	// reference, so this one recounts and takes number 2.
	f.On("GET /transactions/reference/reversal:txn_1:1", 200, `{"transaction_id":"r0"}`)
	f.On("GET /transactions/r0", 200, `{"transaction_id":"r0","status":"APPLIED","currency":"USD","precise_amount":500,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	f.OnFunc("POST /transactions", func(c blnkfake.Call) (int, string) {
		if c.Body["reference"] == "reversal:txn_1:1" {
			return 400, blnkfake.APIError("TXN_DUPLICATE_REFERENCE")
		}
		return 201, `{"transaction_id":"txn_p","status":"APPLIED","currency":"USD","precise_amount":1000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`
	})
	if _, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1:a", Entry: "txn_1", Amount: money.MustParse("10", blnkfake.USD), Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	posts := f.Sent("POST /transactions")
	if len(posts) != 2 || posts[1].Body["reference"] != "reversal:txn_1:2" {
		t.Fatalf("sent %v", posts)
	}
	if meta, _ := posts[1].Body["meta_data"].(map[string]any); meta["reversal_ref"] != "refund:o1:a" {
		t.Fatalf("meta %v", meta)
	}
}

func TestPartialReversalIsIdempotentOnItsRef(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/txn_1", 200, appliedTxn)
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"r1","reference":"reversal:txn_1:1","status":"APPLIED","currency":"USD","precise_amount":3000,"precision":100,
		"source":"bln_usd2","destination":"bln_usd","meta_data":{"reversal_of":"txn_1","reversal_ref":"refund:o1:1"}}]`)
	f.On("GET /transactions/reference/reversal:txn_1:1", 200, `{"transaction_id":"r1"}`)
	f.On("GET /transactions/r1", 200, `{"transaction_id":"r1","status":"APPLIED","currency":"USD","precise_amount":3000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	e, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1:1", Entry: "txn_1", Amount: money.MustParse("30", blnkfake.USD), Reason: "retry"})
	if err != nil || e.ID != "r1" || len(f.Sent("POST /transactions")) != 0 {
		t.Fatalf("%+v %v", e, err)
	}
}

// Only "reversal:<entry>:<n>" rows are reversals; anything else carrying
// the reversal_of tag (a refund's queue copy, say) is ignored.
func TestOnlyNumberedReversalsCount(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/txn_1", 200, appliedTxn)
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"q","reference":"rv_refund_q","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,
		"source":"bln_usd2","destination":"bln_usd","meta_data":{"reversal_of":"txn_1","reversal_ref":"refund:o1"}}]`)
	f.On("POST /transactions", 201, `{"transaction_id":"txn_r","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd2","destination":"bln_usd"}`)
	e, err := s.Reverse(context.Background(), types.Reversal{Ref: "refund:o1", Entry: "txn_1", Reason: "x"})
	if err != nil || e.ID != "txn_r" {
		t.Fatalf("%+v %v: the copy was taken for a reversal", e, err)
	}
}
