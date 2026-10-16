package ledger_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
)

// TestLiveCore runs against a real Blnk Core and writes test data to it.
// It is skipped unless BLNK_LIVE_URL is set:
//
//	BLNK_LIVE_URL=http://localhost:5001 go test ./ledger -run TestLiveCore -v
func TestLiveCore(t *testing.T) {
	url := os.Getenv("BLNK_LIVE_URL")
	if url == "" {
		t.Skip("set BLNK_LIVE_URL to run against a real Blnk Core")
	}
	c := blnk.New(url, blnk.WithAPIKey(os.Getenv("BLNK_LIVE_API_KEY")))
	l, err := c.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: "humanblnk live test"})
	if err != nil {
		t.Fatal(err)
	}
	svc := ledger.New(c, ledger.Config{LedgerID: ledger.LedgerID(l.LedgerID)})
	run := fmt.Sprint(time.Now().UnixNano())
	a, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	b, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})

	// Every kind of movement must replay safely when retried with the same
	// reference. In order: the deposits fund what follows.
	moves := []struct {
		name string
		move func() (*ledger.Receipt, error)
	}{
		{"deposit with fee", func() (*ledger.Receipt, error) {
			return svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
				Fee: ledger.Amount("10", ""), Reference: "dep_fee_" + run, Provider: "paystack"})
		}},
		{"deposit", func() (*ledger.Receipt, error) {
			return svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("500", "NGN"),
				Fee: ledger.NoFee, Reference: "dep_" + run, Provider: "paystack"})
		}},
		{"transfer with fee", func() (*ledger.Receipt, error) {
			return svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("100", "NGN"),
				Fee: ledger.Amount("1", ""), Reference: "tr_fee_" + run})
		}},
		{"withdrawal", func() (*ledger.Receipt, error) {
			return svc.Wallet().Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("50", "NGN"),
				Fee: ledger.NoFee, Reference: "wd_" + run, Immediate: true})
		}},
	}
	for _, m := range moves {
		name, move := m.name, m.move
		first, err := move()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		again, err := move()
		if err != nil || !again.Replayed || again.Amount.Value != first.Amount.Value {
			t.Errorf("%s: retry should replay the original, got %+v, %v", name, again, err)
		}
		got, err := svc.Wallet().GetTransaction(ctx, first.TransactionID)
		if err != nil || got.Amount.Value != first.Amount.Value {
			t.Errorf("%s: Get(%s) = %+v, %v", name, first.TransactionID, got, err)
		}
	}
	if bal, _ := svc.Wallet().GetAccount(ctx, a.ID); bal.Balance.Value != "1339" { // 990 + 500 − 101 − 50
		t.Errorf("balance after retries = %s, want 1339 (a retry posted twice?)", bal.Balance.Value)
	}

	// Limits count a confirmed withdrawal once and give a cancelled one back,
	// although Core's filter results leave parent_transaction empty.
	lim := ledger.New(c, ledger.Config{LedgerID: ledger.LedgerID(l.LedgerID), Limits: []ledger.Limit{{Tier: "*", Currency: "NGN", Daily: "100000"}}})
	if _, err := lim.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: b.ID, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee, Reference: "lim_in_" + run}); err != nil {
		t.Fatal(err)
	}
	wd := func(ref string) *ledger.Receipt {
		r, err := lim.Wallet().Withdraw(ctx, ledger.WithdrawInput{AccountID: b.ID, Amount: ledger.Amount("100", "NGN"), Fee: ledger.Amount("1", ""), Reference: ref + run})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	w1, w2 := wd("lim_w1_"), wd("lim_w2_")
	part := ledger.Amount("50", "")
	if _, err := lim.Wallet().Confirm(ctx, w1.TransactionID, &part); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("partial confirm of a withdrawal with a fee: %v", err)
	}
	if _, err := lim.Wallet().Confirm(ctx, w1.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := lim.Wallet().Cancel(ctx, w2.TransactionID); err != nil {
		t.Fatal(err)
	}
	// b has sent nothing else: only the confirmed withdrawal counts, once.
	if u, err := lim.Wallet().LimitUsage(ctx, b.ID); err != nil || u.DailyUsed.Value != "101" {
		t.Errorf("daily used = %v, %v; want 101", u, err)
	}
}

// TestLiveCoreNamespaces checks the Ledgers/Accounts/House split, ledger
// freeze, closure rules and dry-run quotes against a real Core (0.15.4+).
func TestLiveCoreNamespaces(t *testing.T) {
	url := os.Getenv("BLNK_LIVE_URL")
	if url == "" {
		t.Skip("set BLNK_LIVE_URL to run against a real Blnk Core")
	}
	c := blnk.New(url, blnk.WithAPIKey(os.Getenv("BLNK_LIVE_API_KEY")))
	run := fmt.Sprint(time.Now().UnixNano())
	boot := ledger.New(c, ledger.Config{})
	if err := boot.Check(ctx); err != nil {
		t.Fatalf("check: %v", err)
	}

	// Ledgers: created once per key (POST /ledgers/filter on real Core).
	lg, err := boot.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "live-" + run, Name: "Live " + run})
	if err != nil {
		t.Fatal(err)
	}
	again, err := boot.Ops().CreateLedger(ctx, ledger.CreateLedgerInput{Key: "live-" + run, Name: "Live " + run})
	if err != nil || !again.Replayed || again.ID != lg.ID {
		t.Fatalf("create replay: %+v %v", again, err)
	}
	if p, err := boot.Ops().ListLedgers(ctx, ledger.LedgerQuery{Limit: 5}); err != nil || len(p.Items) == 0 {
		t.Fatalf("list: %+v %v", p, err)
	}
	svc := ledger.New(c, ledger.Config{LedgerID: lg.ID})
	if err := svc.Check(ctx); err != nil {
		t.Fatalf("check with ledger: %v", err)
	}
	if _, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", LedgerID: "ldg_00000000-0000-0000-0000-000000000000"}); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("open in missing ledger: %v", err)
	}

	a, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	usd, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "USD"})
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.Amount("10", ""), Reference: "ns_dep_" + run, Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: usd.ID, Amount: ledger.Amount("100", "USD"),
		Fee: ledger.NoFee, Reference: "ns_usd_" + run, Provider: "paystack"}); err != nil {
		t.Fatal(err)
	}

	// House accounts: read through Ops(), refused by Wallet().GetAccount.
	fee, err := svc.Ops().GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	if err != nil || fee.Type != ledger.GLIncome || fee.Natural.Sign() <= 0 {
		t.Fatalf("house get: %+v %v", fee, err)
	}
	if _, err := svc.Wallet().GetAccount(ctx, ledger.AccountID(fee.ID)); !errors.Is(err, ledger.ErrWrongKind) {
		t.Fatalf("house account via Accounts: %v", err)
	}
	if _, err := svc.Ops().HouseHistory(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"}, ledger.HistoryQuery{Limit: 5}); err != nil {
		t.Fatalf("house history: %v", err)
	}

	// Quotes are dry-run by Core: allowed, refused, and nothing posts.
	q, err := svc.Wallet().QuoteTransfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("100", "NGN"), Fee: ledger.NoFee, Reference: "ns_q1_" + run})
	if err != nil || !q.Allowed || !q.CheckedByLedger {
		t.Fatalf("quote: %+v %v", q, err)
	}
	q, err = svc.Wallet().QuoteTransfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("999999", "NGN"), Fee: ledger.NoFee, Reference: "ns_q2_" + run})
	if err != nil || q.Allowed || !errors.Is(q.Reason, ledger.ErrInsufficientFunds) {
		t.Fatalf("refused quote: %+v %v", q, err)
	}
	xq, err := svc.Wallet().QuoteExchange(ctx, ledger.ExchangeInput{From: usd.ID, To: a.ID, Sell: ledger.Amount("10", "USD"), Rate: "1500", Fee: ledger.NoFee, Reference: "ns_xq_" + run})
	if err != nil || !xq.Allowed || !xq.CheckedByLedger || xq.Bought.Value != "15000" {
		t.Fatalf("exchange quote: %+v %v", xq, err)
	}
	if got, _ := svc.Wallet().GetAccount(ctx, a.ID); got.Balance.Value != "990" {
		t.Fatalf("quotes must not post: balance %s", got.Balance.Value)
	}

	// Ledger freeze: nothing leaves, money still comes in.
	if err := svc.Ops().FreezeLedger(ctx, lg.ID, "live test"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("1", "NGN"), Fee: ledger.NoFee, Reference: "ns_frz_" + run}); !errors.Is(err, ledger.ErrLedgerFrozen) {
		t.Fatalf("frozen ledger transfer: %v", err)
	}
	if _, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: b.ID, Amount: ledger.Amount("5", "NGN"), Fee: ledger.NoFee, Reference: "ns_in_" + run}); err != nil {
		t.Fatalf("deposit into frozen ledger: %v", err)
	}
	if err := svc.Ops().UnfreezeLedger(ctx, lg.ID); err != nil {
		t.Fatal(err)
	}

	// Closure: approval required, then final; a full refund into it is refused.
	pay, err := svc.Wallet().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("40", "NGN"), Fee: ledger.NoFee, Reference: "ns_pay_" + run})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ops().CloseAccount(ctx, a.ID, ledger.CloseInput{Reason: "live", SweepTo: b.ID, Reference: "ns_close_" + run}); !errors.Is(err, ledger.ErrApprovalRequired) {
		t.Fatalf("close without approver: %v", err)
	}
	closed, err := svc.Ops().CloseAccount(ctx, a.ID, ledger.CloseInput{Reason: "live", RequestedBy: "ops.kemi", ApprovedBy: "ops", SweepTo: b.ID, Reference: "ns_close_" + run})
	if err != nil || closed.Status != "closed" || closed.Balance.Sign() != 0 {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if _, err := svc.Wallet().Refund(ctx, pay.TransactionID, ledger.RefundInput{}); !errors.Is(err, ledger.ErrAccountClosed) {
		t.Fatalf("refund into closed account: %v", err)
	}
	if tb, err := svc.Ops().TrialBalance(ctx, "NGN"); err != nil || !tb.Balanced {
		t.Fatalf("trial balance: %+v %v", tb, err)
	}
}
