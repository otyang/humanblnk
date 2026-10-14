package ledger_test

import (
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
	svc := ledger.New(c, ledger.Config{LedgerID: l.LedgerID})
	run := fmt.Sprint(time.Now().UnixNano())
	a, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	b, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})

	// Every kind of movement must replay safely when retried with the same reference.
	moves := map[string]func() (*ledger.Receipt, error){
		"deposit with fee": func() (*ledger.Receipt, error) {
			return svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("1000", "NGN"),
				Fee: ledger.Amount("10", ""), Reference: "dep_fee_" + run, Provider: "paystack"})
		},
		"deposit": func() (*ledger.Receipt, error) {
			return svc.Payments().Deposit(ctx, ledger.DepositInput{AccountID: a.ID, Amount: ledger.Amount("500", "NGN"),
				Fee: ledger.NoFee, Reference: "dep_" + run, Provider: "paystack"})
		},
		"transfer with fee": func() (*ledger.Receipt, error) {
			return svc.Payments().Transfer(ctx, ledger.TransferInput{From: a.ID, To: b.ID, Amount: ledger.Amount("100", "NGN"),
				Fee: ledger.Amount("1", ""), Reference: "tr_fee_" + run})
		},
		"withdrawal": func() (*ledger.Receipt, error) {
			return svc.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: a.ID, Amount: ledger.Amount("50", "NGN"),
				Fee: ledger.NoFee, Reference: "wd_" + run, Immediate: true})
		},
	}
	for name, move := range moves {
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
}
