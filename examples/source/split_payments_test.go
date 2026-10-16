package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// splitBalance reads an account's balance for display.
func splitBalance(w ledger.Wallet, id ledger.AccountID) string {
	a, err := w.GetAccount(ctx, id)
	check(err)
	return a.Balance.Display
}

func TestSplitPayments(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_ada", "Ada", currency.NGN)
	tunde := customer(wallet, "user_tunde", "Tunde", currency.NGN)
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{currency.NGN}, ExternalID: "merchant_311"})
	check(err)
	adaNGN, tundeNGN, chidi := ada.Accounts[currency.NGN].ID, tunde.Accounts[currency.NGN].ID, shop.Accounts[currency.NGN].ID

	begin("split-payments/card-order")
	// sample: split-payments/card-order
	// Ada pays ₦25,000 by card for an order from Chidi Stores. Paystack collects it.
	order, err := wallet.Split(ctx, ledger.SplitInput{
		From:           ledger.AccountID(ledger.ClearingAccount("paystack")), // @PaystackClearing
		AllowOverdraft: true,                                                 // money arriving from outside
		Amount:         ledger.Amount("25000", currency.NGN),
		Reference:      "order_10442", // Paystack's payment id
		Description:    "Order 10442",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "5", Narration: "Oja commission"},
			{To: tundeNGN, Amount: ledger.Amount("1500", currency.NGN), Narration: "Delivery"},
			{To: chidi, Rest: true, Narration: "Order 10442"},
		},
	})
	check(err)
	fmt.Println(order.Status, order.Amount.Display, "in", len(order.Legs), "legs")
	fmt.Println("Chidi Stores:", splitBalance(wallet, chidi))
	fmt.Println("Tunde (rider):", splitBalance(wallet, tundeNGN))
	fee, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: currency.NGN})
	check(err)
	fmt.Println("@FeeIncome:", fee.Balance.Display)
	// end
	end()

	// Not shown: a retry with the same parts replays the original.
	replay, err := wallet.Split(ctx, ledger.SplitInput{
		From: ledger.AccountID(ledger.ClearingAccount("paystack")), AllowOverdraft: true,
		Amount: ledger.Amount("25000", currency.NGN), Reference: "order_10442", Description: "Order 10442",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "5", Narration: "Oja commission"},
			{To: tundeNGN, Amount: ledger.Amount("1500", currency.NGN), Narration: "Delivery"},
			{To: chidi, Rest: true, Narration: "Order 10442"},
		},
	})
	check(err)
	if !replay.Replayed || replay.TransactionID != order.TransactionID {
		t.Fatalf("split retry did not replay: %+v", replay)
	}

	begin("split-payments/legs")
	// sample: split-payments/legs
	names := map[string]string{string(chidi): "Chidi Stores", string(tundeNGN): "Tunde", ledger.FeeIncomeAccount: "@FeeIncome"}
	for _, leg := range order.Legs {
		fmt.Printf("%-13s %9s  %-15s leg id: %t\n", names[leg.Account], leg.Amount.Display, leg.Narration, leg.TransactionID != "")
	}
	// end
	end()

	begin("split-payments/lookup")
	// sample: split-payments/lookup
	again, err := wallet.GetTransaction(ctx, order.TransactionID) // rebuilt from its legs
	check(err)
	fmt.Println(again.Reference, again.Amount.Display, len(again.Legs), "legs")

	byRef, err := wallet.GetTransactionByReference(ctx, "order_10442")
	check(err)
	fmt.Println("same payment:", byRef.TransactionID == order.TransactionID)

	// The id of the leg that paid Chidi Stores, for a refund or a dispute later.
	var chidiLeg string
	for _, leg := range again.Legs {
		if leg.Account == string(chidi) {
			chidiLeg = leg.TransactionID
		}
	}
	leg, err := wallet.GetTransaction(ctx, chidiLeg)
	check(err)
	fmt.Println("Chidi's leg:", leg.Amount.Display, leg.Status, "parent:", leg.ParentTransaction == order.TransactionID)
	// end
	end()

	begin("split-payments/rounding")
	// sample: split-payments/rounding
	// ₦100.01 shared 50/50 between two sellers: 50.005 each is not a real amount.
	odd, err := wallet.Split(ctx, ledger.SplitInput{
		From: ledger.AccountID(ledger.ClearingAccount("paystack")), AllowOverdraft: true,
		Amount: ledger.Amount("100.01", currency.NGN), Reference: "order_10443",
		Parts: []ledger.SplitPart{
			{To: chidi, Percent: "50"},
			{To: tundeNGN, Percent: "50"},
		},
	})
	check(err)
	for _, leg := range odd.Legs {
		fmt.Println(names[leg.Account], leg.Amount.Display) // the first percentage part gets the 1 kobo
	}

	// With a Rest part, the Rest part gets the dust instead.
	withRest, err := wallet.Split(ctx, ledger.SplitInput{
		From: ledger.AccountID(ledger.ClearingAccount("paystack")), AllowOverdraft: true,
		Amount: ledger.Amount("100.01", currency.NGN), Reference: "order_10444",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "7.5"}, // 7.50075 → 7.50
			{To: chidi, Rest: true},
		},
	})
	check(err)
	for _, leg := range withRest.Legs {
		fmt.Println(names[leg.Account], leg.Amount.Display)
	}
	// end
	end()

	fund(wallet, adaNGN, "50000", currency.NGN, "seed_ada")

	begin("split-payments/from-customer")
	// sample: split-payments/from-customer
	// Ada pays ₦12,000 from her wallet balance. Customer accounts never go negative.
	paid, err := wallet.Split(ctx, ledger.SplitInput{
		From:      adaNGN,
		Amount:    ledger.Amount("12000", currency.NGN),
		Reference: "order_10445",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Amount: ledger.Amount("200", currency.NGN), Narration: "Service charge"},
			{To: chidi, Rest: true, Narration: "Order 10445"},
		},
	})
	check(err)
	fmt.Println(paid.Status, "Ada now:", splitBalance(wallet, adaNGN))

	_, err = wallet.Split(ctx, ledger.SplitInput{
		From: adaNGN, Amount: ledger.Amount("90000", currency.NGN), Reference: "order_10446",
		Parts: []ledger.SplitPart{{To: ledger.FeeIncomeAccount, Percent: "5"}, {To: chidi, Rest: true}},
	})
	fmt.Println("too much:", errors.Is(err, ledger.ErrInsufficientFunds))
	// end
	end()

	begin("split-payments/pending")
	// sample: split-payments/pending
	// Hold the money while Chidi Stores confirms the stock.
	held, err := wallet.Split(ctx, ledger.SplitInput{
		From: adaNGN, Amount: ledger.Amount("8000", currency.NGN), Reference: "order_10447",
		Pending: true,
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "5"},
			{To: chidi, Rest: true},
		},
	})
	check(err)
	acct, err := wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println(held.Status, "Ada balance:", acct.Balance.Display, "available:", acct.Available.Display)

	part := ledger.Amount("4000", currency.NGN)
	_, err = wallet.Confirm(ctx, held.TransactionID, &part)
	fmt.Println("partial confirm of the whole split:", errors.Is(err, ledger.ErrInvalidInput))

	done, err := wallet.Confirm(ctx, held.TransactionID, nil) // every leg at once
	check(err)
	fmt.Println(done.Status, "Ada now:", splitBalance(wallet, adaNGN))
	// end
	end()

	begin("split-payments/errors")
	// sample: split-payments/errors
	clearing := ledger.AccountID(ledger.ClearingAccount("paystack"))
	try := func(parts ...ledger.SplitPart) error {
		_, err := wallet.Split(ctx, ledger.SplitInput{From: clearing, AllowOverdraft: true,
			Amount: ledger.Amount("1000", currency.NGN), Reference: "order_bad", Parts: parts})
		return err
	}
	err = try(ledger.SplitPart{To: chidi, Amount: ledger.Amount("600", currency.NGN)},
		ledger.SplitPart{To: tundeNGN, Amount: ledger.Amount("300", currency.NGN)})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	err = try(ledger.SplitPart{To: chidi, Amount: ledger.Amount("1000", currency.NGN)},
		ledger.SplitPart{To: tundeNGN, Rest: true})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	err = try(ledger.SplitPart{To: chidi, Percent: "80", Amount: ledger.Amount("800", currency.NGN)},
		ledger.SplitPart{To: tundeNGN, Rest: true})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	err = try(ledger.SplitPart{To: chidi, Rest: true})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	check(ops.FreezeAccount(ctx, tundeNGN, "KYC review"))
	err = try(ledger.SplitPart{To: chidi, Percent: "90"}, ledger.SplitPart{To: tundeNGN, Rest: true})
	fmt.Println("frozen recipient:", errors.Is(err, ledger.ErrAccountFrozen))
	// end
	end()
}
