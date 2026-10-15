package humanblnk_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/ledgertest"
)

// Import the module root and use everything through it.
func Example() {
	ctx := context.Background()

	// In your app: l := humanblnk.New("http://localhost:5001", humanblnk.Config{LedgerID: "ldg_..."})
	core := ledgertest.NewServer() // in-memory Blnk, for this example
	defer core.Close()
	ledgerID := core.CreateLedger("Wallets")
	l := humanblnk.New(core.URL(), humanblnk.Config{LedgerID: humanblnk.LedgerID(ledgerID)})

	ada, _ := l.Accounts().Open(ctx, humanblnk.OpenAccountInput{Currency: "NGN"})
	tunde, _ := l.Accounts().Open(ctx, humanblnk.OpenAccountInput{Currency: "NGN"})
	l.Payments().Deposit(ctx, humanblnk.DepositInput{
		AccountID: ada.ID, Amount: humanblnk.Amount("10000", "NGN"), Fee: humanblnk.NoFee,
		Reference: "paystack_T1", Provider: "paystack",
	})

	_, err := l.Payments().Transfer(ctx, humanblnk.TransferInput{
		From: ada.ID, To: tunde.ID, Amount: humanblnk.Amount("25000", "NGN"),
		Fee: humanblnk.NoFee, Reference: "p2p_1",
	})
	fmt.Println(errors.Is(err, humanblnk.ErrInsufficientFunds))

	_, err = l.Payments().Transfer(ctx, humanblnk.TransferInput{
		From: ada.ID, To: tunde.ID, Amount: humanblnk.Amount("2500", "NGN"),
		Fee: humanblnk.NoFee, Reference: "p2p_2",
	})
	t, _ := l.Accounts().Get(ctx, tunde.ID)
	fmt.Println(err, t.Balance.Display)
	// Output:
	// true
	// <nil> 2500.00
}
