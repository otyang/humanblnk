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
	a, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	b, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})

	// Every kind of movement must replay safely when retried with the same
	// reference. In order: the deposits fund what follows.
	moves := []struct {
		name string
		move func() (*ledger.Receipt, error)
	}{
		{"deposit with fee", func() (*ledger.Receipt, error) {
			return svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
				Fee: ledger.Amount("10", ""), Reference: "dep_fee_" + run, Provider: "paystack"})
		}},
		{"deposit", func() (*ledger.Receipt, error) {
			return svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("500", "NGN"),
				Fee: ledger.NoFee, Reference: "dep_" + run, Provider: "paystack"})
		}},
		{"transfer with fee", func() (*ledger.Receipt, error) {
			return svc.Payments().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("100", "NGN"),
				Fee: ledger.Amount("1", ""), Reference: "tr_fee_" + run})
		}},
		{"withdrawal", func() (*ledger.Receipt, error) {
			return svc.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("50", "NGN"),
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
		got, err := svc.Transactions().Get(ctx, first.TransactionID)
		if err != nil || got.Amount.Value != first.Amount.Value {
			t.Errorf("%s: Get(%s) = %+v, %v", name, first.TransactionID, got, err)
		}
	}
	if bal, _ := svc.Accounts().Get(ctx, a.ID); bal.Balance.Value != "1339" { // 990 + 500 − 101 − 50
		t.Errorf("balance after retries = %s, want 1339 (a retry posted twice?)", bal.Balance.Value)
	}

	// Limits count a confirmed withdrawal once and give a cancelled one back,
	// although Core's filter results leave parent_transaction empty.
	lim := ledger.New(c, ledger.Config{LedgerID: ledger.LedgerID(l.LedgerID), Limits: []ledger.Limit{{Tier: "*", Currency: "NGN", Daily: "100000"}}})
	if _, err := lim.Payments().Deposit(ctx, ledger.DepositInput{AccountID: b.ID, Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee, Reference: "lim_in_" + run}); err != nil {
		t.Fatal(err)
	}
	wd := func(ref string) *ledger.Receipt {
		r, err := lim.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: b.ID, Amount: ledger.Amount("100", "NGN"), Fee: ledger.Amount("1", ""), Reference: ref + run})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	w1, w2 := wd("lim_w1_"), wd("lim_w2_")
	part := ledger.Amount("50", "")
	if _, err := lim.Pending().Confirm(ctx, w1.TransactionID, &part); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("partial confirm of a withdrawal with a fee: %v", err)
	}
	if _, err := lim.Pending().Confirm(ctx, w1.TransactionID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := lim.Pending().Cancel(ctx, w2.TransactionID); err != nil {
		t.Fatal(err)
	}
	// b has sent nothing else: only the confirmed withdrawal counts, once.
	if u, err := lim.Accounts().LimitUsage(ctx, b.ID); err != nil || u.DailyUsed.Value != "101" {
		t.Errorf("daily used = %v, %v; want 101", u, err)
	}
}
