package ledgertest_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// Test code that uses ledger.Service without a real Blnk Core. In a test,
// pass your *testing.T.
func ExampleNew() {
	svc, core := ledgertest.New(&testing.T{}, ledger.Config{})
	ctx := context.Background()

	acct, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: acct.ID, Amount: ledger.Amount("5000", "NGN"),
		Fee: ledger.NoFee, Reference: "seed", Provider: "paystack"})
	svc.Wallet().Withdraw(ctx, ledger.WithdrawInput{AccountID: acct.ID, Amount: ledger.Amount("1000", "NGN"),
		Fee: ledger.NoFee, Reference: "wd1"}) // pending, auto-cancels after 24h

	core.Advance(25 * time.Hour) // the provider never confirmed
	a, _ := svc.Wallet().GetAccount(ctx, acct.ID)
	fmt.Println(a.Available.Display, len(core.Events()) > 0)
	// Output:
	// 5000.00 true
}
