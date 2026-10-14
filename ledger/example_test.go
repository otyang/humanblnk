package ledger_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// newExample starts an in-memory Blnk for the examples below. In your app,
// use ledger.New(blnk.New(url), cfg) against a real Blnk Core.
func newExample() *ledger.Service {
	svc, _ := ledgertest.New(&testing.T{}, ledger.Config{})
	return svc
}

// A customer tops up through Paystack and sends money to a friend.
func Example() {
	ctx := context.Background()
	var l ledger.Ledger = newExample()

	ada, _ := l.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	tunde, _ := l.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})

	// Paystack confirmed ₦50,000; their reference makes a repeated webhook harmless.
	_, err := l.Payments().Deposit(ctx, ledger.DepositInput{
		AccountID: ada.ID, Amount: ledger.Amount("50000", "NGN"), Fee: ledger.Amount("750", ""),
		Reference: "paystack_T629384756", Provider: "paystack",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = l.Payments().Transfer(ctx, ledger.TransferInput{
		From: ada.ID, To: tunde.ID, Amount: ledger.Amount("12500", "NGN"), Fee: ledger.Amount("10", ""),
		Reference: "p2p_01J8ZK2M4Q",
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	a, _ := l.Accounts().Get(ctx, ada.ID)
	t, _ := l.Accounts().Get(ctx, tunde.ID)
	fmt.Println("Ada:  ", a.Balance.Display)
	fmt.Println("Tunde:", t.Balance.Display)
	// Output:
	// Ada:   36740.00
	// Tunde: 12500.00
}

// Card authorisation: capture part of a hold, release the rest.
func ExamplePending() {
	ctx := context.Background()
	l := newExample()
	acct, _ := l.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
	l.Payments().Deposit(ctx, ledger.DepositInput{AccountID: acct.ID, Amount: ledger.Amount("20000", "NGN"),
		Fee: ledger.NoFee, Reference: "seed", Provider: "paystack"})

	auth, _ := l.Pending().Hold(ctx, ledger.HoldInput{AccountID: acct.ID, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("18500", "NGN"), Reference: "card_auth_449201"})
	a, _ := l.Accounts().Get(ctx, acct.ID)
	fmt.Println("held:     ", a.PendingOut.Display, "available", a.Available.Display)

	captured := ledger.Amount("17250.50", "")
	l.Pending().Confirm(ctx, auth.TransactionID, &captured)
	l.Pending().Cancel(ctx, auth.TransactionID)
	a, _ = l.Accounts().Get(ctx, acct.ID)
	fmt.Println("captured: ", a.Balance.Display, "available", a.Available.Display)
	// Output:
	// held:      18500.00 available 1500.00
	// captured:  2749.50 available 2749.50
}

// Exact decimals: what the user typed, the integer Blnk stores, and what to show.
func ExampleParseDecimal() {
	m, _ := ledger.ParseDecimal("0.456", "BTC", 100_000_000)
	fmt.Println(m.Value, m.PreciseAmount, m.Fixed())

	_, err := ledger.ParseDecimal("100.567", "NGN", 100)
	fmt.Println(err)
	// Output:
	// 0.456 45600000 0.45600000
	// ledger: invalid input: amount "100.567" has more than 2 decimal places
}

// A percentage fee with a floor and a cap.
func ExampleComputeFee() {
	rule := ledger.FeeRule{Percent: "1.5", Min: "10", Max: "2000"}
	for _, typed := range []string{"500", "50000", "500000"} {
		amount, _ := ledger.ParseDecimal(typed, "NGN", 100)
		fee, _ := ledger.ComputeFee(rule, amount)
		fmt.Printf("₦%s → fee ₦%s\n", typed, fee.Value)
	}
	// Output:
	// ₦500 → fee ₦10
	// ₦50000 → fee ₦750
	// ₦500000 → fee ₦2000
}

// Display: fiat padded, crypto trimmed and cut so a balance can always be spent.
func ExampleDisplayRule_Format() {
	ngn, _ := ledger.ParseDecimal("100.5", "NGN", 100)
	eth, _ := ledger.ParseDecimal("0.4567896", "ETH", 1_000_000_000_000_000_000)
	fmt.Println(ledger.DisplayRule{Decimals: 2}.Format(ngn))
	fmt.Println(ledger.BuiltinDisplay["ETH"].Format(eth))
	// Output:
	// 100.50
	// 0.456789
}
