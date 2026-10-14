package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
)

// fakeBlnk records requests and answers with scripted responses keyed by "METHOD /path".
type fakeBlnk struct {
	t        *testing.T
	mu       sync.Mutex
	reqs     []recorded
	handlers map[string][]reply // consumed in order; last one repeats
}

type recorded struct {
	Method, Path, Key string
	Body              map[string]any
}

type reply struct {
	status int
	body   string
}

func newFake(t *testing.T) (*fakeBlnk, *ledger.Service, *httptest.Server) {
	f := &fakeBlnk{t: t, handlers: map[string][]reply{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := blnk.New(srv.URL, blnk.WithAPIKey("secret"), blnk.WithRetries(2, time.Millisecond))
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	svc := ledger.New(c, ledger.Config{LedgerID: "ldg_1", Now: func() time.Time { return now }})
	return f, svc, srv
}

func (f *fakeBlnk) on(route string, status int, body string) {
	f.handlers[route] = append(f.handlers[route], reply{status, body})
}

func (f *fakeBlnk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	route := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.Header.Get("X-Blnk-Key"), body})
	rs := f.handlers[route]
	var rep reply
	switch {
	case len(rs) == 0:
		rep = reply{404, `{"error":"no route","error_detail":{"code":"GEN_NOT_FOUND","message":"no route"}}`}
	case len(rs) == 1:
		rep = rs[0]
	default:
		rep, f.handlers[route] = rs[0], rs[1:]
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rep.status)
	_, _ = w.Write([]byte(rep.body))
}

func (f *fakeBlnk) last() recorded { return f.reqs[len(f.reqs)-1] }

const appliedTxn = `{"transaction_id":"txn_1","reference":"ref-1","precise_amount":100000,"precision":100,
 "currency":"NGN","source":"@PaystackClearing","destination":"bln_w","status":"APPLIED","created_at":"2026-10-11T12:00:00Z"}`

func TestDepositWithFeeSplitsToFeeAccount(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 201, appliedTxn)

	r, err := svc.Payments().Deposit(context.Background(), ledger.DepositInput{
		AccountID: "bln_w", Amount: ledger.Minor(100000, "ngn"), Fee: ledger.Minor(1500, "NGN"),
		Reference: "paystack_abc", Provider: "paystack",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Settled() || !r.Posted() || r.EntryStatus != ledger.EntryPosted {
		t.Fatalf("want APPLIED/posted, got %s/%s", r.Status, r.EntryStatus)
	}
	req := f.last()
	if req.Key != "secret" {
		t.Errorf("missing X-Blnk-Key header")
	}
	b := req.Body
	if b["source"] != "@PaystackClearing" || b["allow_overdraft"] != true || b["skip_queue"] != true {
		t.Errorf("unexpected flags: %v", b)
	}
	if b["precise_amount"].(float64) != 100000 || b["precision"].(float64) != 100 || b["currency"] != "NGN" {
		t.Errorf("unexpected amount fields: %v", b)
	}
	if _, has := b["destination"]; has {
		t.Errorf("must not send destination with destinations")
	}
	dests := b["destinations"].([]any)
	fee := dests[0].(map[string]any)
	rest := dests[1].(map[string]any)
	if fee["identifier"] != "@FeeIncome" || fee["precise_distribution"] != "1500" {
		t.Errorf("fee leg wrong: %v", fee)
	}
	if rest["identifier"] != "bln_w" || rest["distribution"] != "left" {
		t.Errorf("account leg wrong: %v", rest)
	}
}

func TestWithdrawIsHeldWithExpiryAndFeeOnTop(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 201, `{"transaction_id":"txn_w","status":"INFLIGHT","precise_amount":51000,"currency":"NGN"}`)

	r, err := svc.Payments().Withdraw(context.Background(), ledger.WithdrawInput{
		AccountID: "bln_w", Amount: ledger.Minor(50000, "NGN"), Fee: ledger.Minor(1000, "NGN"), Reference: "wd-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Pending() {
		t.Fatalf("want INFLIGHT")
	}
	b := f.last().Body
	if b["inflight"] != true || b["inflight_expiry_date"] != "2026-10-12T12:00:00Z" {
		t.Errorf("hold not set: %v", b)
	}
	if b["precise_amount"].(float64) != 51000 || b["source"] != "bln_w" {
		t.Errorf("total/source wrong: %v", b)
	}
	if _, overdraft := b["allow_overdraft"]; overdraft {
		t.Errorf("customer account must not overdraft")
	}
	d := b["destinations"].([]any)
	if d[0].(map[string]any)["identifier"] != "@PayoutClearing" || d[0].(map[string]any)["precise_distribution"] != "50000" {
		t.Errorf("payout leg wrong: %v", d[0])
	}
}

func TestTransferInsufficientFundsMapsToSentinel(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 400, `{"error":"insufficient funds","error_detail":{"code":"TXN_INSUFFICIENT_FUNDS","message":"insufficient funds"}}`)

	_, err := svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "bln_a", To: "bln_b", Amount: ledger.Minor(10, "NGN"), Reference: "t-1",
	})
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
	var ae *blnk.APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("APIError should stay reachable: %v", err)
	}
	if len(f.reqs) != 1 {
		t.Fatalf("4xx must not be retried, got %d requests", len(f.reqs))
	}
}

func TestDuplicateReferenceReplaysWhenPayloadMatches(t *testing.T) {
	f, svc, _ := newFake(t)
	dup := `{"error":"dup","error_detail":{"code":"TXN_DUPLICATE_REFERENCE","message":"dup"}}`
	f.on("POST /transactions", 409, dup)
	f.on("GET /transactions/reference/t-9", 200,
		`{"transaction_id":"txn_9","reference":"t-9","precise_amount":500,"currency":"NGN","source":"bln_a","destination":"bln_b","status":"APPLIED"}`)

	r, err := svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "bln_a", To: "bln_b", Amount: ledger.Minor(500, "NGN"), Reference: "t-9",
	})
	if err != nil || !r.Replayed || r.TransactionID != "txn_9" {
		t.Fatalf("expected replay of txn_9, got %+v err=%v", r, err)
	}

	_, err = svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "bln_a", To: "bln_b", Amount: ledger.Minor(999, "NGN"), Reference: "t-9",
	})
	if !errors.Is(err, ledger.ErrDuplicateReference) {
		t.Fatalf("different payload must conflict, got %v", err)
	}
}

func TestPartialConfirmAndCancel(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("GET /transactions/txn_h", 200, `{"transaction_id":"txn_h","status":"INFLIGHT","precise_amount":10000,"precision":100,"currency":"NGN"}`)
	f.on("PUT /transactions/inflight/txn_h", 200, `{"transaction_id":"txn_c","status":"APPLIED","precise_amount":4000,"precision":100,"currency":"NGN","parent_transaction":"txn_h"}`)
	part := ledger.Minor(4000, "NGN")
	r, err := svc.Pending().Confirm(context.Background(), "txn_h", &part)
	if err != nil || r.Status != "APPLIED" {
		t.Fatalf("confirm: %+v %v", r, err)
	}
	b := f.last().Body
	if b["status"] != "commit" || b["precise_amount"].(float64) != 4000 || b["skip_queue"] != true {
		t.Errorf("confirm (commit) body wrong: %v", b)
	}

	f.handlers = map[string][]reply{}
	f.on("PUT /transactions/inflight/txn_h", 400, `{"error":"x","error_detail":{"code":"TXN_ALREADY_COMMITTED","message":"x"}}`)
	_, err = svc.Pending().Cancel(context.Background(), "txn_h")
	if !errors.Is(err, ledger.ErrAlreadyFinalized) {
		t.Fatalf("want ErrAlreadyFinalized, got %v", err)
	}
	if f.last().Body["status"] != "void" {
		t.Errorf("cancel (void) body wrong")
	}
}

func TestRetriesOnLockContention(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 423, `{"error":"locked","error_detail":{"code":"GEN_RESOURCE_LOCKED","message":"locked"}}`)
	f.on("POST /transactions", 201, appliedTxn)

	if _, err := svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "bln_a", To: "bln_b", Amount: ledger.Minor(1, "NGN"), Reference: "r"}); err != nil {
		t.Fatal(err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(f.reqs))
	}
}

func TestHistoryUsesBalanceIDFilter(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions/filter", 200, `{"data":[`+appliedTxn+`],"total_count":7}`)
	p, err := svc.Accounts().History(context.Background(), "bln_w", ledger.HistoryQuery{Statuses: []string{"posted"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 7 || len(p.Items) != 1 {
		t.Fatalf("page wrong: %+v", p)
	}
	b := f.last().Body
	fs := b["filters"].([]any)
	first, status := fs[0].(map[string]any), fs[1].(map[string]any)
	if first["field"] != "balance_id" || first["value"] != "bln_w" || status["value"] != "APPLIED" ||
		b["limit"].(float64) != 5 || b["sort_order"] != "desc" || b["logical_operator"] != nil || len(f.reqs) != 1 {
		t.Errorf("filter body wrong: %v (requests=%d)", b, len(f.reqs))
	}

	f.reqs = nil
	_, _ = svc.Accounts().History(context.Background(), "bln_w", ledger.HistoryQuery{Direction: ledger.Outgoing})
	if fs := f.last().Body["filters"].([]any); fs[0].(map[string]any)["field"] != "source" {
		t.Errorf("outgoing must filter on source: %v", fs)
	}
}

func TestFrozenAccountIsRejected(t *testing.T) {
	f := &fakeBlnk{t: t, handlers: map[string][]reply{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	svc := ledger.New(blnk.New(srv.URL), ledger.Config{EnforceAccountStatus: true})
	f.on("GET /balances/bln_a", 200, `{"balance_id":"bln_a","currency":"NGN","meta_data":{"status":"frozen"}}`)

	_, err := svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "bln_a", To: "bln_b", Amount: ledger.Minor(1, "NGN"), Reference: "r"})
	if !errors.Is(err, ledger.ErrAccountFrozen) {
		t.Fatalf("want ErrAccountFrozen, got %v", err)
	}
	for _, r := range f.reqs {
		if r.Method == "POST" {
			t.Fatal("transaction must not be posted for a frozen account")
		}
	}
}

func TestGetAccountComputesAvailable(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("GET /balances/bln_w", 200, `{"balance_id":"bln_w","currency":"NGN","balance":100000,"inflight_debit_balance":30000,"inflight_balance":-30000}`)
	w, err := svc.Accounts().Get(context.Background(), "bln_w")
	if err != nil {
		t.Fatal(err)
	}
	if w.Available.PreciseAmount.Int64() != 70000 || w.Available.Value != "700" || w.Status != "active" {
		t.Fatalf("account wrong: %+v", w.Available)
	}
	if got := w.Available.Fixed(); got != "700.00" {
		t.Fatalf("fixed: %s", got)
	}
	if w.Kind != ledger.CustomerAccount {
		t.Fatalf("kind: %s", w.Kind)
	}

	f.on("GET /balances/indicator/@FeeIncome/currency/NGN", 200, `{"balance_id":"bln_fee","indicator":"@FeeIncome","currency":"NGN"}`)
	fee, err := svc.Accounts().GetSystem(context.Background(), "@FeeIncome", "ngn")
	if err != nil || fee.Kind != ledger.SystemAccount {
		t.Fatalf("system account: %+v %v", fee, err)
	}
}

func TestValidation(t *testing.T) {
	_, svc, _ := newFake(t)
	ctx := context.Background()
	cases := map[string]error{
		"no ref": func() error {
			_, err := svc.Payments().Deposit(ctx, ledger.DepositInput{Fee: ledger.NoFee, AccountID: "w", Amount: ledger.Minor(1, "NGN")})
			return err
		}(),
		"fee >= amount": func() error {
			_, err := svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: "w", Amount: ledger.Minor(1, "NGN"), Fee: ledger.Minor(1, "NGN"), Reference: "r"})
			return err
		}(),
		"self transfer": func() error {
			_, err := svc.Payments().Transfer(ctx, ledger.TransferInput{Fee: ledger.NoFee, From: "w", To: "w", Amount: ledger.Minor(1, "NGN"), Reference: "r"})
			return err
		}(),
		"dup ref in batch": func() error {
			it := ledger.TransferInput{Fee: ledger.NoFee, From: "a", To: "b", Amount: ledger.Minor(1, "NGN"), Reference: "same"}
			_, err := svc.Payments().Bulk(ctx, ledger.BulkTransferInput{Items: []ledger.TransferInput{it, it}})
			return err
		}(),
	}
	for name, err := range cases {
		if !errors.Is(err, ledger.ErrInvalidInput) {
			t.Errorf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
}

func TestParseDecimal(t *testing.T) {
	m, err := ledger.ParseDecimal("1,500.25", "ngn", 100)
	if err != nil || m.PreciseAmount.Int64() != 150025 || m.Currency != "NGN" || m.Value != "1500.25" {
		t.Fatalf("got %+v %v", m, err)
	}
	if _, err := ledger.ParseDecimal("1.005", "NGN", 100); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("excess decimals must be rejected")
	}
	if _, err := ledger.ParseDecimal("1e3", "NGN", 100); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("exponent must be rejected")
	}
	btc, _ := ledger.ParseDecimal("0.00000001", "BTC", 100_000_000)
	if btc.PreciseAmount.Int64() != 1 || btc.Value != "0.00000001" || btc.Fixed() != "0.00000001" {
		t.Fatalf("btc: %+v", btc)
	}
}

// ---- string amounts: what you type is what is stored --------------------------

func TestDebitTypedBTCAmount(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 201, `{"transaction_id":"txn_b","status":"INFLIGHT","precise_amount":45600000,
	  "precision":100000000,"amount":0.456,"amount_string":"0.456","currency":"BTC","source":"bln_w"}`)

	r, err := svc.Payments().Withdraw(context.Background(), ledger.WithdrawInput{
		Fee:       ledger.NoFee,
		AccountID: "bln_w", Amount: ledger.Amount("0.456", "btc"), Reference: "btc-out-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.last().Body
	// Exact integer + precision go to Blnk; no float "amount" field is sent.
	if b["precise_amount"].(float64) != 45600000 || b["precision"].(float64) != 1e8 || b["currency"] != "BTC" {
		t.Fatalf("request wrong: %v", b)
	}
	if _, sent := b["amount"]; sent {
		t.Fatalf("float amount must not be sent")
	}
	if r.Amount.Value != "0.456" || r.Amount.PreciseAmount.Int64() != 45600000 || r.Amount.Precision != 1e8 {
		t.Fatalf("receipt amount wrong: %+v", r.Amount)
	}
	if r.Amount.String() != "0.456 BTC" || r.Amount.Fixed() != "0.45600000" {
		t.Fatalf("render: %s / %s", r.Amount, r.Amount.Fixed())
	}
}

func TestCreditTypedUSDAmountWithFeeAsString(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 201, `{"transaction_id":"txn_u","status":"APPLIED","precise_amount":10056,"precision":100,"currency":"USD",
	  "destinations":[{"identifier":"@FeeIncome","precise_distribution":"56"},{"identifier":"bln_w","distribution":"left"}]}`)

	r, err := svc.Payments().Deposit(context.Background(), ledger.DepositInput{
		AccountID: "bln_w", Amount: ledger.Amount("100.56", "USD"), Fee: ledger.Amount("0.56", ""), Reference: "usd-in-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.last().Body
	if b["precise_amount"].(float64) != 10056 || b["precision"].(float64) != 100 {
		t.Fatalf("request wrong: %v", b)
	}
	md := b["meta_data"].(map[string]any)
	if md["fee"] != "0.56" || md["fee_precise"] != "56" || md["net_amount"] != "100" {
		t.Fatalf("fee metadata wrong: %v", md)
	}
	if r.Amount.Value != "100.56" || r.Legs[0].Amount.Value != "0.56" {
		t.Fatalf("receipt wrong: %+v legs=%+v", r.Amount, r.Legs)
	}
}

func TestEighteenDecimalAsset(t *testing.T) {
	m, err := ledger.ParseDecimal("1.000000000000000001", "ETH", 1_000_000_000_000_000_000)
	if err != nil || m.PreciseAmount.String() != "1000000000000000001" {
		t.Fatalf("eth: %+v %v", m, err)
	}
	big, err := ledger.ParseDecimal("123456789.123456789123456789", "ETH", 1_000_000_000_000_000_000)
	if err != nil || big.PreciseAmount.String() != "123456789123456789123456789" {
		t.Fatalf("beyond int64 must work: %+v %v", big, err)
	}
}

func TestTooManyDecimalsForCurrencyRejectedBeforeCallingBlnk(t *testing.T) {
	f, svc, _ := newFake(t)
	_, err := svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "a", To: "b", Amount: ledger.Amount("100.567", "USD"), Reference: "r",
	})
	if !errors.Is(err, ledger.ErrInvalidInput) || len(f.reqs) != 0 {
		t.Fatalf("want local rejection, got %v (requests=%d)", err, len(f.reqs))
	}
}

func TestPartialConfirmWithTypedAmountUsesHeldPrecision(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("GET /transactions/txn_h", 200, `{"transaction_id":"txn_h","status":"INFLIGHT","precise_amount":45600000,"precision":100000000,"currency":"BTC"}`)
	f.on("PUT /transactions/inflight/txn_h", 200, `{"transaction_id":"txn_c","status":"APPLIED","precise_amount":10000000,"precision":100000000,"currency":"BTC"}`)

	part := ledger.Amount("0.1", "")
	r, err := svc.Pending().Confirm(context.Background(), "txn_h", &part)
	if err != nil {
		t.Fatal(err)
	}
	if f.last().Body["precise_amount"].(float64) != 10000000 || r.Amount.Value != "0.1" {
		t.Fatalf("partial wrong: %v %+v", f.last().Body, r.Amount)
	}
	tooMuch := ledger.Amount("0.5", "BTC")
	if _, err := svc.Pending().Confirm(context.Background(), "txn_h", &tooMuch); !errors.Is(err, ledger.ErrAmountExceeded) {
		t.Fatalf("want ErrAmountExceeded, got %v", err)
	}
}

func TestMoneyJSONRoundTrip(t *testing.T) {
	// Clients may send the amount as a string or a bare number; neither goes through float64.
	for _, in := range []string{
		`{"amount":"0.456","currency":"BTC"}`,
		`{"amount":0.456,"currency":"btc"}`,
	} {
		var m ledger.Money
		if err := json.Unmarshal([]byte(in), &m); err != nil {
			t.Fatal(err)
		}
		if m.Value != "0.456" || m.Currency != "BTC" {
			t.Fatalf("%s -> %+v", in, m)
		}
	}
	out, _ := json.Marshal(ledger.Precise(bigInt("45600000"), 100_000_000, "BTC"))
	want := `{"amount":"0.456","currency":"BTC","precise_amount":"45600000","precision":100000000}`
	if string(out) != want {
		t.Fatalf("got %s", out)
	}
}

func TestMinorStillSupported(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 201, `{"transaction_id":"x","status":"APPLIED","precise_amount":10056,"precision":100,"currency":"USD"}`)
	r, err := svc.Payments().Transfer(context.Background(), ledger.TransferInput{
		Fee:  ledger.NoFee,
		From: "a", To: "b", Amount: ledger.Minor(10056, "USD"), Reference: "r"})
	if err != nil || r.Amount.Value != "100.56" {
		t.Fatalf("%+v %v", r, err)
	}
}

func bigInt(s string) *big.Int { v, _ := new(big.Int).SetString(s, 10); return v }

func TestChartOfAccounts(t *testing.T) {
	for in, want := range map[string]string{
		"paystack": "@PaystackClearing", "@Paystack": "@PaystackClearing",
		"@PaystackClearing": "@PaystackClearing", "flutterwave": "@FlutterwaveClearing", "": "@DepositClearing",
	} {
		if got := ledger.ClearingAccount(in); got != want {
			t.Errorf("ClearingAccount(%q) = %q, want %q", in, got, want)
		}
	}
	for blnkSt, want := range map[string]ledger.EntryStatus{
		"APPLIED": ledger.EntryPosted, "INFLIGHT": ledger.EntryPending, "VOID": ledger.EntryVoided,
		"QUEUED": ledger.EntryQueued, "REJECTED": ledger.EntryRejected, "SCHEDULED": ledger.EntryScheduled,
	} {
		if got := ledger.EntryStatusOf(blnkSt); got != want {
			t.Errorf("EntryStatusOf(%s) = %s", blnkSt, got)
		}
	}
}

func TestWithdrawToProviderClearingAndOverrides(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /transactions", 201, `{"transaction_id":"x","status":"INFLIGHT","precise_amount":100,"precision":100,"currency":"NGN"}`)
	ctx := context.Background()

	r, err := svc.Payments().Withdraw(ctx, ledger.WithdrawInput{Fee: ledger.NoFee, AccountID: "w", Amount: ledger.Amount("1", "NGN"), Reference: "a", Provider: "flutterwave"})
	if err != nil || f.last().Body["destination"] != "@FlutterwaveClearing" || r.EntryStatus != ledger.EntryPending {
		t.Fatalf("provider clearing: %v %v %+v", err, f.last().Body["destination"], r)
	}
	if _, err := svc.Payments().Withdraw(ctx, ledger.WithdrawInput{Fee: ledger.NoFee, AccountID: "w", Amount: ledger.Amount("1", "NGN"), Reference: "b", Provider: "x", ClearingAccount: "@Legacy"}); err != nil ||
		f.last().Body["destination"] != "@Legacy" {
		t.Fatalf("explicit account must win: %v", f.last().Body["destination"])
	}

	// Existing data on old indicators: point config at them.
	f2, _, srv := newFake(t)
	f2.on("POST /transactions", 201, `{"transaction_id":"y","status":"APPLIED"}`)
	legacy := ledger.New(blnk.New(srv.URL), ledger.Config{FeeIncomeAccount: "@Fees", DepositClearingAccount: "@World"})
	if _, err := legacy.Payments().Deposit(ctx, ledger.DepositInput{AccountID: "w", Amount: ledger.Amount("10", "NGN"), Fee: ledger.Amount("1", ""), Reference: "c"}); err != nil {
		t.Fatal(err)
	}
	b := f2.last().Body
	if b["source"] != "@World" || b["destinations"].([]any)[0].(map[string]any)["identifier"] != "@Fees" {
		t.Fatalf("legacy config ignored: %v", b)
	}
}

func TestWatchBalanceSendsExactThreshold(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("GET /balances/bln_w", 200, `{"balance_id":"bln_w","currency":"USD"}`)
	f.on("POST /balance-monitors", 201, `{"monitor_id":"mon_1","balance_id":"bln_w","condition":{"field":"balance","operator":"==","value":29,"precision":1,"precise_value":29}}`)
	// 0.29 × 100 in float64 is 28.999…; sending 29 with precision 1 avoids that.
	w, err := svc.Operations().WatchBalance(context.Background(), ledger.WatchInput{AccountID: "bln_w", Operator: "=", Threshold: ledger.Amount("0.29", "")})
	if err != nil || w.ID != "mon_1" || w.Operator != "==" {
		t.Fatalf("%+v %v", w, err)
	}
	c := f.last().Body["condition"].(map[string]any)
	if c["value"].(float64) != 29 || c["precision"].(float64) != 1 || c["operator"] != "==" {
		t.Fatalf("condition sent: %v", c)
	}
}

func TestInstantReconciliationBody(t *testing.T) {
	f, svc, _ := newFake(t)
	f.on("POST /reconciliation/matching-rules", 201, `{"rule_id":"rule_1"}`)
	f.on("POST /reconciliation/start-instant", 200, `{"reconciliation_id":"rec_1"}`)
	f.on("GET /reconciliation/rec_1", 200, `{"reconciliation_id":"rec_1","status":"in_progress"}`)
	r, err := svc.Reconciliations().Start(context.Background(), ledger.ReconcileInput{Source: "paystack", Records: []ledger.ExternalRecord{
		{ID: "x1", Amount: ledger.Amount("0.456", "BTC"), Reference: "r1", Date: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	}})
	if err != nil || r.ID != "rec_1" || r.Done() {
		t.Fatalf("%+v %v", r, err)
	}
	var start recorded
	for _, q := range f.reqs {
		if q.Path == "/reconciliation/start-instant" {
			start = q
		}
	}
	ext := start.Body["external_transactions"].([]any)[0].(map[string]any)
	if ext["amount"].(float64) != 0.456 || ext["currency"] != "BTC" || ext["date"] != "2026-06-01T00:00:00Z" ||
		start.Body["strategy"] != "one_to_one" || start.Body["matching_rule_ids"].([]any)[0] != "rule_1" {
		t.Fatalf("start body: %v", start.Body)
	}
}

func TestMissingFeeSendsNothing(t *testing.T) {
	f, svc, _ := newFake(t)
	for name, call := range map[string]func() error{
		"deposit": func() error {
			_, err := svc.Payments().Deposit(context.Background(), ledger.DepositInput{AccountID: "w", Amount: ledger.Minor(1, "NGN"), Reference: "r"})
			return err
		},
		"withdraw": func() error {
			_, err := svc.Payments().Withdraw(context.Background(), ledger.WithdrawInput{AccountID: "w", Amount: ledger.Minor(1, "NGN"), Reference: "r"})
			return err
		},
		"bulk item": func() error {
			_, err := svc.Payments().Bulk(context.Background(), ledger.BulkTransferInput{Items: []ledger.TransferInput{{From: "a", To: "b", Amount: ledger.Minor(1, "NGN"), Reference: "r"}}})
			return err
		},
	} {
		if err := call(); !errors.Is(err, ledger.ErrInvalidInput) {
			t.Errorf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
	if len(f.reqs) != 0 {
		t.Fatalf("nothing may reach Blnk, got %d requests", len(f.reqs))
	}
}

func TestBulkPricesWithOneLookupPerSender(t *testing.T) {
	f := &fakeBlnk{t: t, handlers: map[string][]reply{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	svc := ledger.New(blnk.New(srv.URL), ledger.Config{Fees: func(_ context.Context, r ledger.FeeRequest) (ledger.Money, error) {
		return ledger.Amount("1", ""), nil
	}})
	f.on("GET /balances/bln_a", 200, `{"balance_id":"bln_a","currency":"NGN"}`)
	f.on("POST /transactions/bulk", 201, `{"batch_id":"b1","status":"applied","transaction_count":3}`)
	items := []ledger.TransferInput{
		{From: "bln_a", To: "bln_b", Amount: ledger.Amount("10", "NGN"), Reference: "p1"},
		{From: "bln_a", To: "bln_c", Amount: ledger.Amount("10", "NGN"), Reference: "p2"},
		{From: "bln_a", To: "bln_d", Amount: ledger.Amount("10", "NGN"), Reference: "p3"},
	}
	if _, err := svc.Payments().Bulk(context.Background(), ledger.BulkTransferInput{Items: items}); err != nil {
		t.Fatal(err)
	}
	gets := 0
	for _, r := range f.reqs {
		if r.Method == "GET" {
			gets++
		}
	}
	tx := f.last().Body["transactions"].([]any)[0].(map[string]any)
	if gets != 1 || tx["precise_amount"].(float64) != 1100 {
		t.Fatalf("gets=%d first item=%v", gets, tx)
	}
}
