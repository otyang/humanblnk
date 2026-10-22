package transactions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

func TestRecordSendsMinorUnitsSynchronously(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 201, `{"transaction_id":"txn_1","reference":"deposit:ps_1","status":"APPLIED","currency":"USD","precise_amount":150025,"precision":100,"source":"@World","destination":"bln_usd"}`)
	e, err := s.Record(context.Background(), types.Transaction{
		Ref: types.Reference("deposit", "ps_1"), From: types.Single(types.World, money.MustParse("1500.25", blnkfake.USD)),
		To: types.Single("bln_usd", money.MustParse("1500.25", blnkfake.USD)), Description: "top-up",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.Sent("POST /transactions")[0].Body
	if got["precise_amount"] != float64(150025) || got["precision"] != float64(100) || got["currency"] != "USD" ||
		got["source"] != "@World" || got["destination"] != "bln_usd" || got["skip_queue"] != true || got["reference"] != "deposit:ps_1" {
		t.Fatalf("sent %v", got)
	}
	if e.ID != "txn_1" || e.Amount.String() != "1500.25" || e.To[0].Account != "bln_usd" || e.Status != types.StatusApplied {
		t.Fatalf("entry %+v", e)
	}
}

func TestRecordSplitIsAtomic(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 201, `{"transaction_id":"txn_p","reference":"transfer:t1","status":"APPLIED","currency":"USD","precise_amount":10150,"precision":100,"source":"bln_usd",
		"destinations":[{"identifier":"bln_usd2","precise_distribution":"10000"},{"identifier":"@FeeIncome","precise_distribution":"150"}]}`)
	e, err := s.Record(context.Background(), types.Transaction{
		Ref:  "transfer:t1",
		From: types.Single("bln_usd", money.MustParse("101.50", blnkfake.USD)),
		To: types.Many(
			types.Posting{Account: "bln_usd2", Amount: money.MustParse("100", blnkfake.USD)},
			types.Posting{Account: types.FeeIncome, Amount: money.MustParse("1.50", blnkfake.USD)},
		),
		Description: "transfer with fee",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.Sent("POST /transactions")[0].Body
	ds, _ := got["destinations"].([]any)
	if got["atomic"] != true || got["source"] != "bln_usd" || got["precise_amount"] != float64(10150) || len(ds) != 2 ||
		ds[1].(map[string]any)["precise_distribution"] != "150" {
		t.Fatalf("sent %v", got)
	}
	if len(e.To) != 2 || e.To[1].Account != types.FeeIncome || e.To[1].Amount.String() != "1.50" {
		t.Fatalf("entry %+v", e)
	}
}

func TestRecordErrorsAreHumanBlnks(t *testing.T) {
	for code, want := range map[string]error{
		"TXN_INSUFFICIENT_FUNDS": types.ErrInsufficientFunds,
		"BAL_NOT_FOUND":          types.ErrNotFound,
	} {
		s, f := newService(t)
		f.On("POST /transactions", 400, blnkfake.APIError(code))
		_, err := s.Record(context.Background(), types.Transaction{
			Ref: "r", From: types.Single("bln_usd", money.MustParse("1", blnkfake.USD)), To: types.Single("bln_usd2", money.MustParse("1", blnkfake.USD)), Description: "d",
		})
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v", code, err)
		}
	}
}

// one is 1.00 USD from bln_usd to bln_usd2 under "r".
var one = types.Transaction{
	Ref: "r", From: types.Single("bln_usd", money.MustParse("1", blnkfake.USD)), To: types.Single("bln_usd2", money.MustParse("1", blnkfake.USD)), Description: "d",
}

const oneApplied = `{"transaction_id":"txn_1","reference":"r","status":"APPLIED","currency":"USD","precise_amount":100,"precision":100,"source":"bln_usd","destination":"bln_usd2"}`

// A used reference is ErrDuplicate only when it holds this transaction,
// applied.
func TestRecordDuplicateIsChecked(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 400, blnkfake.APIError("TXN_DUPLICATE_REFERENCE"))
	f.On("GET /transactions/reference/r", 200, `{"transaction_id":"txn_1"}`)
	f.On("GET /transactions/txn_1", 200, oneApplied)
	e, err := s.Record(context.Background(), one)
	if !errors.Is(err, types.ErrDuplicate) || e.ID != "txn_1" {
		t.Fatalf("same: %+v %v", e, err)
	}

	other := one
	other.To = types.Single("bln_usd2", money.MustParse("2", blnkfake.USD))
	other.From = types.Single("bln_usd", money.MustParse("2", blnkfake.USD))
	if _, err := s.Record(context.Background(), other); !errors.Is(err, types.ErrReferenceUsed) || errors.Is(err, types.ErrDuplicate) {
		t.Fatalf("different: %v", err)
	}

	// Rolled back by Blnk: refunded without a reversal reason.
	f.On("GET /transactions/reference/txn_1_refund", 200, `{"transaction_id":"txn_rb","meta_data":{}}`)
	if _, err := s.Record(context.Background(), one); !errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("rolled back: %v", err)
	}

	// Can't look it up: neither, so nobody takes it as done.
	f.On("GET /transactions/reference/r", 503, blnkfake.APIError("GEN_INTERNAL"))
	if _, err := s.Record(context.Background(), one); err == nil || errors.Is(err, types.ErrDuplicate) || errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("lookup failed: %v", err)
	}
}

// The review's case: a split whose fee posting failed. Blnk never records
// the failed posting, so the split doesn't match and a retry isn't "done".
func TestRecordRetryAfterASplitMissingAPosting(t *testing.T) {
	s, f := newService(t)
	usd := func(v string) money.Money { return money.MustParse(v, blnkfake.USD) }
	payout := types.Transaction{
		Ref: "payout:1", From: types.Single("bln_usd", usd("105")),
		To: types.Many(types.Posting{Account: "bln_usd2", Amount: usd("95")}, types.Posting{Account: "bln_ngn", Amount: usd("10")}), Description: "d",
	}
	f.On("GET /balances/bln_ngn", 200, `{"balance_id":"bln_ngn","currency":"USD","balance":0}`) // a USD account, for this test
	f.On("POST /transactions", 400, blnkfake.APIError("TXN_DUPLICATE_REFERENCE"))
	f.On("GET /transactions/reference/payout:1-1", 200, `{"transaction_id":"leg1","parent_transaction":"split1"}`)
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"leg1","reference":"payout:1-1","parent_transaction":"split1","status":"APPLIED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd","destination":"bln_usd2"}]`)
	f.On("GET /transactions/reference/leg1_refund", 200, `{"transaction_id":"leg1r"}`)
	if _, err := s.Record(context.Background(), payout); !errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("got %v", err)
	}
}

func TestRecordAllRetryAfterARolledBackBatch(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions/bulk", 400, blnkfake.APIError("TXN_DUPLICATE_REFERENCE"))
	f.On("GET /transactions/reference/r", 200, `{"transaction_id":"txn_1"}`)
	f.On("GET /transactions/txn_1", 200, oneApplied)
	f.On("GET /transactions/reference/txn_1_refund", 200, `{"transaction_id":"txn_rb"}`) // rolled back
	second := one
	second.Ref = "r2" // never recorded: the batch stopped before it
	if _, err := s.RecordAll(context.Background(), []types.Transaction{one, second}); !errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("got %v", err)
	}
}

// A split is stamped with its posting count: Blnk copies it onto each
// posting, and a split missing one reads as not applied, even before Blnk's
// rollback refund lands.
func TestSplitMissingAPostingIsNotApplied(t *testing.T) {
	s, f := newService(t)
	usd := func(v string) money.Money { return money.MustParse(v, blnkfake.USD) }
	f.On("POST /transactions", 201, `{"transaction_id":"split1","status":"APPLIED","currency":"USD","precise_amount":10500,"precision":100,"source":"bln_usd","destinations":[{"identifier":"bln_usd2","precise_distribution":"9500"},{"identifier":"@FeeIncome","precise_distribution":"1000"}]}`)
	if _, err := s.Record(context.Background(), types.Transaction{
		Ref: "pay:9", From: types.Single("bln_usd", usd("105")),
		To: types.Many(types.Posting{Account: "bln_usd2", Amount: usd("95")}, types.Posting{Account: types.FeeIncome, Amount: usd("10")}), Description: "d",
	}); err != nil {
		t.Fatal(err)
	}
	if meta := f.Sent("POST /transactions")[0].Body["meta_data"].(map[string]any); meta["ledger_split_postings"] != "2" {
		t.Fatalf("stamped %v", meta)
	}

	f.On("GET /transactions/split9", 404, blnkfake.APIError("TXN_NOT_FOUND"))
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"l1","reference":"pay:9-1","parent_transaction":"split9","status":"APPLIED","currency":"USD","precise_amount":9500,"precision":100,"source":"bln_usd","destination":"bln_usd2","meta_data":{"ledger_split_postings":"2"}}]`)
	e, err := s.Get(context.Background(), "split9")
	if err != nil || e.Status != types.StatusRejected {
		t.Fatalf("%+v %v", e, err)
	}
}

// Items of a batch name the batch as parent, like a split's postings; a
// reference that only matches "<ref>-1" of a batch item isn't a split.
func TestGetByReferenceIgnoresABatch(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/reference/x-1", 200, `{"transaction_id":"i1","parent_transaction":"bulk_1"}`)
	f.On("GET /transactions/bulk_1", 404, blnkfake.APIError("TXN_NOT_FOUND"))
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"i1","reference":"x-1","parent_transaction":"bulk_1","status":"APPLIED","currency":"USD","precise_amount":100,"precision":100,"source":"bln_usd","destination":"bln_usd2"},
		{"transaction_id":"i2","reference":"other","parent_transaction":"bulk_1","status":"APPLIED","currency":"USD","precise_amount":100,"precision":100,"source":"bln_usd","destination":"bln_usd2"}]`)
	if _, err := s.GetByReference(context.Background(), "x"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestRecordRejectsAnotherPrecision(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 201, `{"transaction_id":"txn_1","status":"APPLIED","currency":"USD","precise_amount":100,"precision":1000,"source":"bln_usd","destination":"bln_usd2"}`)
	_, err := s.Record(context.Background(), types.Transaction{
		Ref: "r", From: types.Single("bln_usd", money.MustParse("1", blnkfake.USD)), To: types.Single("bln_usd2", money.MustParse("1", blnkfake.USD)), Description: "d",
	})
	if err == nil {
		t.Fatal("a USD entry at precision 1000 must be an error")
	}
}

func TestRecordAllIsOneAtomicBatch(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions/bulk", 201, `{"batch_id":"bulk_1","status":"applied","transaction_count":2}`)
	f.On("GET /transactions/reference/fx:1:blnkfake.USD", 200, `{"transaction_id":"txn_a"}`)
	f.On("GET /transactions/reference/fx:1:blnkfake.NGN", 200, `{"transaction_id":"txn_b"}`)
	f.On("GET /transactions/txn_a", 200, `{"transaction_id":"txn_a","reference":"fx:1:blnkfake.USD","status":"APPLIED","currency":"USD","precise_amount":1000,"precision":100,"source":"bln_usd","destination":"@FX"}`)
	f.On("GET /transactions/txn_b", 200, `{"transaction_id":"txn_b","reference":"fx:1:blnkfake.NGN","status":"APPLIED","currency":"NGN","precise_amount":1600000,"precision":100,"source":"@FX","destination":"bln_ngn"}`)
	fx := types.SystemAccount("FX")
	es, err := s.RecordAll(context.Background(), []types.Transaction{
		{Ref: "fx:1:blnkfake.USD", From: types.Single("bln_usd", money.MustParse("10", blnkfake.USD)), To: types.Single(fx, money.MustParse("10", blnkfake.USD)), Description: "sell USD"},
		{Ref: "fx:1:blnkfake.NGN", From: types.Single(fx, money.MustParse("16000", blnkfake.NGN)), To: types.Single("bln_ngn", money.MustParse("16000", blnkfake.NGN)), Description: "buy NGN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.Sent("POST /transactions/bulk")[0].Body
	if got["atomic"] != true || got["skip_queue"] != true || len(got["transactions"].([]any)) != 2 {
		t.Fatalf("sent %v", got)
	}
	if len(es) != 2 || es[0].ID != "txn_a" || es[1].Amount.String() != "16000.00" {
		t.Fatalf("entries %+v", es)
	}
}

func TestRecordAllReportsAFailedReadBack(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions/bulk", 201, `{"batch_id":"bulk_1","status":"applied","transaction_count":1}`)
	es, err := s.RecordAll(context.Background(), []types.Transaction{
		{Ref: "fx:2:usd", From: types.Single("bln_usd", money.MustParse("10", blnkfake.USD)), To: types.Single("bln_usd2", money.MustParse("10", blnkfake.USD)), Description: "d"},
	})
	if !errors.Is(err, types.ErrReadBack) {
		t.Fatalf("got %v", err)
	}
	if len(es) != 1 || es[0].Ref != "fx:2:usd" || es[0].ID != "" || es[0].Amount.String() != "10.00" {
		t.Fatalf("entries %+v", es)
	}
}

// Only system accounts may go below zero: Blnk needs allow_overdraft to
// debit @World, and must never get it when a customer pays.
func TestOverdraftOnlyFromSystemAccounts(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 201, `{"transaction_id":"t","status":"APPLIED","currency":"USD","precise_amount":100,"precision":100}`)
	one := money.MustParse("1", blnkfake.USD)
	for _, tc := range []struct {
		from types.AccountRef
		want any
	}{
		{types.World, true},
		{types.ClearingAccount("paystack"), true},
		{"bln_usd", nil}, // omitted: false
	} {
		if _, err := s.Record(context.Background(), types.Transaction{
			Ref: "r-" + string(tc.from), From: types.Single(tc.from, one), To: types.Single("bln_usd2", one), Description: "d",
		}); err != nil {
			t.Fatal(err)
		}
		sent := f.Sent("POST /transactions")
		if got := sent[len(sent)-1].Body["allow_overdraft"]; got != tc.want {
			t.Errorf("from %s: allow_overdraft %v, want %v", tc.from, got, tc.want)
		}
	}
}

// A system account is one with an "@" indicator. A plain balance in the
// General Ledger, as a recorded entry can't make @World, doesn't overdraw.
func TestOverdraftNeedsAnIndicator(t *testing.T) {
	s, f := newService(t)
	f.On("GET /balances/bln_gl", 200, `{"balance_id":"bln_gl","ledger_id":"general_ledger_id","currency":"USD","balance":0}`)
	f.On("GET /balances/bln_world", 200, `{"balance_id":"bln_world","ledger_id":"general_ledger_id","indicator":"@World","currency":"USD","balance":0}`)
	f.On("POST /transactions", 201, `{"transaction_id":"t","status":"APPLIED","currency":"USD","precise_amount":100,"precision":100,"source":"x","destination":"bln_usd"}`)
	for from, want := range map[types.AccountRef]bool{"bln_gl": false, "bln_world": true} {
		if _, err := s.Record(context.Background(), types.Transaction{
			Ref: "r-" + string(from), From: types.Single(from, money.MustParse("1", blnkfake.USD)), To: types.Single("bln_usd", money.MustParse("1", blnkfake.USD)), Description: "d",
		}); err != nil {
			t.Fatal(err)
		}
		sent := f.Sent("POST /transactions")
		if got := sent[len(sent)-1].Body["allow_overdraft"]; (got == true) != want {
			t.Errorf("%s: allow_overdraft %v, want %v", from, got, want)
		}
	}
}
