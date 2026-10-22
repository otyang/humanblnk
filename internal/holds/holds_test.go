package holds_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk/blnkfake"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

const heldTxn = `{"transaction_id":"h1","status":"INFLIGHT","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"@World"}`

func TestHoldIsInflightWithExpiry(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 201, heldTxn)
	expires := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	h, err := s.Place(context.Background(), types.Transaction{
		Ref: "payout:po_1", From: types.Single("bln_usd", money.MustParse("100", blnkfake.USD)), To: types.Single(types.World, money.MustParse("100", blnkfake.USD)), Description: "payout",
	}, expires)
	if err != nil || h != "h1" {
		t.Fatalf("%q %v", h, err)
	}
	got := f.Sent("POST /transactions")[0].Body
	if got["inflight"] != true || got["inflight_expiry_date"] != "2026-10-24T12:00:00Z" || got["skip_queue"] != true {
		t.Fatalf("sent %v", got)
	}
}

func TestPartialCaptureReleasesTheRest(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/h1", 200, heldTxn)
	f.OnFunc("PUT /transactions/inflight/h1", func(c blnkfake.Call) (int, string) {
		if c.Body["status"] == "void" {
			return 200, `{"transaction_id":"v1","status":"VOID","currency":"USD","precise_amount":2000,"precision":100}`
		}
		return 200, `{"transaction_id":"c1","status":"APPLIED","currency":"USD","precise_amount":8000,"precision":100,"source":"bln_usd","destination":"@World"}`
	})
	e, err := s.Capture(context.Background(), "h1", money.MustParse("80", blnkfake.USD))
	if err != nil || e.Amount.String() != "80.00" {
		t.Fatalf("%+v %v", e, err)
	}
	puts := f.Sent("PUT /transactions/inflight/h1")
	if len(puts) != 2 || puts[0].Body["status"] != "commit" || puts[0].Body["precise_amount"] != float64(8000) || puts[1].Body["status"] != "void" {
		t.Fatalf("sent %+v", puts)
	}
}

func TestFullCaptureDoesntVoid(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/h1", 200, heldTxn)
	f.On("PUT /transactions/inflight/h1", 200, `{"transaction_id":"c1","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"@World"}`)
	if _, err := s.Capture(context.Background(), "h1", money.Money{}); err != nil {
		t.Fatal(err)
	}
	puts := f.Sent("PUT /transactions/inflight/h1")
	if len(puts) != 1 || puts[0].Body["precise_amount"] != nil {
		t.Fatalf("sent %+v", puts)
	}
}

func TestCaptureRefusals(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/h1", 200, heldTxn)
	if _, err := s.Capture(context.Background(), "h1", money.MustParse("100.01", blnkfake.USD)); !errors.Is(err, types.ErrInvalid) {
		t.Errorf("over capture: %v", err)
	}
	if _, err := s.Capture(context.Background(), "h1", money.MustParse("1", blnkfake.NGN)); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("wrong currency: %v", err)
	}
	f.On("GET /transactions/h1", 200, `{"transaction_id":"h1","status":"VOID","currency":"USD","precise_amount":10000,"precision":100}`)
	if _, err := s.Capture(context.Background(), "h1", money.Money{}); !errors.Is(err, types.ErrHoldSettled) {
		t.Errorf("settled: %v", err)
	}
	if n := len(f.Sent("PUT /transactions/inflight/h1")); n != 0 {
		t.Fatalf("%d captures reached Blnk", n)
	}
}

func TestCancelIsSafeToRepeat(t *testing.T) {
	s, f := newService(t)
	f.On("PUT /transactions/inflight/h1", 400, blnkfake.APIError("TXN_ALREADY_VOIDED"))
	if err := s.Cancel(context.Background(), "h1"); err != nil {
		t.Fatalf("already voided: %v", err)
	}
	f.On("PUT /transactions/inflight/h1", 400, blnkfake.APIError("TXN_ALREADY_COMMITTED"))
	if err := s.Cancel(context.Background(), "h1"); !errors.Is(err, types.ErrHoldSettled) {
		t.Fatalf("captured: %v", err)
	}
}

func TestSplitHoldCapturesInFull(t *testing.T) {
	s, f := newService(t)
	f.On("POST /transactions", 201, `{"transaction_id":"hp","status":"INFLIGHT","currency":"USD","precise_amount":10100,"precision":100,"source":"bln_usd",
		"destinations":[{"identifier":"@World","precise_distribution":"10000"},{"identifier":"@FeeIncome","precise_distribution":"100"}]}`)
	h, err := s.Place(context.Background(), types.Transaction{
		Ref: "payout:po_2", From: types.Single("bln_usd", money.MustParse("101", blnkfake.USD)),
		To: types.Many(
			types.Posting{Account: types.World, Amount: money.MustParse("100", blnkfake.USD)},
			types.Posting{Account: types.FeeIncome, Amount: money.MustParse("1", blnkfake.USD)},
		),
		Description: "payout with fee",
	}, time.Time{})
	if err != nil || h != "hp" {
		t.Fatalf("%q %v", h, err)
	}
	if got := f.Sent("POST /transactions")[0].Body; got["inflight"] != true || got["atomic"] != true {
		t.Fatalf("sent %v", got)
	}

	// The split isn't stored under its own id; its postings name it as parent.
	f.On("POST /transactions/filter", 200, `[
		{"transaction_id":"c1","reference":"payout:po_2-1","parent_transaction":"hp","status":"INFLIGHT","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"@World"},
		{"transaction_id":"c2","reference":"payout:po_2-2","parent_transaction":"hp","status":"INFLIGHT","currency":"USD","precise_amount":100,"precision":100,"source":"bln_usd","destination":"@FeeIncome"}]`)
	if _, err := s.Capture(context.Background(), h, money.MustParse("50", blnkfake.USD)); !errors.Is(err, types.ErrInvalid) {
		t.Fatalf("partial capture of a split: %v", err)
	}
	f.On("PUT /transactions/inflight/hp", 200, `{"transaction_id":"c1x","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100}`)
	e, err := s.Capture(context.Background(), h, money.Money{})
	if err != nil || e.Status != types.StatusApplied || e.Amount.String() != "101.00" || len(e.To) != 2 {
		t.Fatalf("%+v %v", e, err)
	}
	if puts := f.Sent("PUT /transactions/inflight/hp"); len(puts) != 1 || puts[0].Body["precise_amount"] != nil {
		t.Fatalf("sent %+v", puts)
	}
}

const captured80 = `[{"transaction_id":"c1","parent_transaction":"h1","status":"APPLIED","currency":"USD","precise_amount":8000,"precision":100,"source":"bln_usd","destination":"@World"}]`

func TestCaptureRepeatFinishesTheEarlierOne(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/h1", 200, heldTxn)
	f.On("POST /transactions/filter", 200, captured80) // committed, but the void never happened
	f.On("PUT /transactions/inflight/h1", 200, `{"transaction_id":"v1","status":"VOID","currency":"USD","precise_amount":2000,"precision":100}`)
	e, err := s.Capture(context.Background(), "h1", money.MustParse("80", blnkfake.USD))
	if err != nil || e.ID != "c1" {
		t.Fatalf("%+v %v", e, err)
	}
	puts := f.Sent("PUT /transactions/inflight/h1")
	if len(puts) != 1 || puts[0].Body["status"] != "void" {
		t.Fatalf("sent %+v, want only the void", puts)
	}
	filter := f.Sent("POST /transactions/filter")[0].Body["filters"].([]any)
	if p := filter[0].(map[string]any); p["field"] != "parent_transaction" || p["value"] != "h1" {
		t.Fatalf("filtered %v", filter)
	}
}

func TestCaptureRepeatWithAnotherAmount(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/h1", 200, heldTxn)
	f.On("POST /transactions/filter", 200, captured80)
	f.On("PUT /transactions/inflight/h1", 200, `{"transaction_id":"v1","status":"VOID","currency":"USD","precise_amount":2000,"precision":100}`)
	if _, err := s.Capture(context.Background(), "h1", money.MustParse("30", blnkfake.USD)); !errors.Is(err, types.ErrHoldSettled) {
		t.Fatalf("got %v", err)
	}
	// No second capture; the 20 the earlier one left held is released.
	puts := f.Sent("PUT /transactions/inflight/h1")
	if len(puts) != 1 || puts[0].Body["status"] != "void" {
		t.Fatalf("sent %+v, want only the void", puts)
	}
}

func TestCaptureLostAnswer(t *testing.T) {
	s, f := newService(t)
	f.On("GET /transactions/h1", 200, heldTxn)
	f.On("PUT /transactions/inflight/h1", 504, `gateway timeout`)
	if _, err := s.Capture(context.Background(), "h1", money.Money{}); err == nil {
		t.Fatal("a commit that left no capture behind succeeded")
	}

	// The commit landed: the second look finds it.
	looks := 0
	f.OnFunc("POST /transactions/filter", func(blnkfake.Call) (int, string) {
		if looks++; looks == 1 {
			return 200, `[]`
		}
		return 200, `[{"transaction_id":"c1","parent_transaction":"h1","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"@World"}]`
	})
	e, err := s.Capture(context.Background(), "h1", money.Money{})
	if err != nil || e.ID != "c1" || e.Amount.String() != "100.00" {
		t.Fatalf("%+v %v", e, err)
	}
}

// splitHold answers like Blnk for split hold s1, whose postings l1 and l2
// have been captured as captured says.
func splitHold(f *blnkfake.Fake, captured map[string]bool) {
	f.OnFunc("POST /transactions/filter", func(c blnkfake.Call) (int, string) {
		switch parent := c.Body["filters"].([]any)[0].(map[string]any)["value"]; {
		case parent == "s1":
			return 200, `[
				{"transaction_id":"l1","parent_transaction":"s1","reference":"payout:po_1-1","status":"INFLIGHT","currency":"USD","precise_amount":9900,"precision":100,"source":"bln_usd","destination":"@World"},
				{"transaction_id":"l2","parent_transaction":"s1","reference":"payout:po_1-2","status":"INFLIGHT","currency":"USD","precise_amount":100,"precision":100,"source":"bln_usd","destination":"@FeeIncome"}]`
		case captured[parent.(string)]:
			return 200, `[{"transaction_id":"c_` + parent.(string) + `","parent_transaction":"` + parent.(string) + `","status":"APPLIED","currency":"USD","precise_amount":1,"precision":100}]`
		}
		return 200, `[]`
	})
}

func TestSplitCaptureRepeat(t *testing.T) {
	s, f := newService(t)
	splitHold(f, map[string]bool{"l1": true, "l2": true})
	f.On("PUT /transactions/inflight/s1", 400, blnkfake.APIError("TXN_ALREADY_COMMITTED"))
	e, err := s.Capture(context.Background(), "s1", money.Money{})
	if err != nil || e.Status != types.StatusApplied || e.Amount.String() != "100.00" {
		t.Fatalf("%+v %v", e, err)
	}
}

// Blnk says ALREADY_COMMITTED when any posting was, even if the fee posting
// was voided meanwhile: that isn't a full capture.
func TestSplitCapturePartlyVoided(t *testing.T) {
	s, f := newService(t)
	splitHold(f, map[string]bool{"l1": true}) // l2 voided
	f.On("PUT /transactions/inflight/s1", 400, blnkfake.APIError("TXN_ALREADY_COMMITTED"))
	if _, err := s.Capture(context.Background(), "s1", money.Money{}); !errors.Is(err, types.ErrHoldSettled) {
		t.Fatalf("got %v", err)
	}
}

// A split hold Blnk rolled back keeps its references with a posting
// missing; placing it again isn't "already placed".
func TestPlaceAfterARolledBackSplitHold(t *testing.T) {
	s, f := newService(t)
	usd := func(v string) money.Money { return money.MustParse(v, blnkfake.USD) }
	f.On("POST /transactions", 400, blnkfake.APIError("TXN_DUPLICATE_REFERENCE"))
	f.On("GET /transactions/reference/payout:po_1-1", 200, `{"transaction_id":"l1","parent_transaction":"s1"}`)
	f.On("POST /transactions/filter", 200, `[{"transaction_id":"l1","parent_transaction":"s1","reference":"payout:po_1-1","status":"INFLIGHT","currency":"USD","precise_amount":9900,"precision":100,"source":"bln_usd","destination":"bln_usd2"}]`)
	_, err := s.Place(context.Background(), types.Transaction{
		Ref: "payout:po_1", From: types.Single("bln_usd", usd("100")),
		To: types.Many(types.Posting{Account: "bln_usd2", Amount: usd("99")}, types.Posting{Account: "bln_usd", Amount: usd("1")}), Description: "payout",
	}, time.Time{})
	if !errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("got %v", err)
	}
}

func TestPlaceRepeatReturnsTheHold(t *testing.T) {
	s, f := newService(t)
	usd := money.MustParse("100", blnkfake.USD)
	hold := types.Transaction{Ref: "payout:po_1", From: types.Single("bln_usd", usd), To: types.Single(types.World, usd), Description: "payout"}
	f.On("POST /transactions", 400, blnkfake.APIError("TXN_DUPLICATE_REFERENCE"))
	f.On("GET /transactions/reference/payout:po_1", 200, `{"transaction_id":"h1"}`)
	f.On("GET /balances/indicator/@World/currency/USD", 200, `{"balance_id":"bln_world","indicator":"@World","currency":"USD"}`)
	f.On("GET /transactions/h1", 200, `{"transaction_id":"h1","status":"INFLIGHT","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"bln_world"}`) // Blnk names system accounts by balance
	if h, err := s.Place(context.Background(), hold, time.Time{}); err != nil || h != "h1" {
		t.Fatalf("%q %v", h, err)
	}

	other := hold
	other.To = types.Single("bln_usd2", usd)
	if _, err := s.Place(context.Background(), other, time.Time{}); !errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("other postings: %v", err)
	}
	f.On("GET /transactions/h1", 200, `{"transaction_id":"h1","status":"APPLIED","currency":"USD","precise_amount":10000,"precision":100,"source":"bln_usd","destination":"bln_world"}`)
	if _, err := s.Place(context.Background(), hold, time.Time{}); !errors.Is(err, types.ErrReferenceUsed) {
		t.Fatalf("not a hold: %v", err)
	}
}
