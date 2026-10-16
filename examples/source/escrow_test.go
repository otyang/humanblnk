package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

// escrowBalance reads an account's balance for display.
func escrowBalance(w ledger.Wallet, id ledger.AccountID) string {
	a, err := w.GetAccount(ctx, id)
	check(err)
	return a.Balance.Display
}

func TestEscrow(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_ada", "Ada", "NGN")
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{"NGN"}, ExternalID: "merchant_311"})
	check(err)
	adaNGN, chidi := ada.Accounts["NGN"].ID, shop.Accounts["NGN"].ID
	fund(wallet, adaNGN, "300000", "NGN", "seed_ada")

	begin("escrow/open")
	// sample: escrow/open
	// Ada buys an ₦80,000 phone from Chidi Stores with buyer protection.
	esc, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{
		Payer:       adaNGN,
		Beneficiary: chidi,
		Amount:      ledger.Amount("80000", "NGN"),
		Reference:   "escrow_10450", // required; a retry returns the same escrow
		Description: "Order 10450",
		MetaData:    map[string]any{"order_id": "10450"},
	})
	check(err)
	fmt.Println(esc.ID, esc.Status, "still held:", esc.Held.Display, "of", esc.Amount.Display)
	fmt.Println("kind:", esc.Account.Kind, "Ada's identity:", esc.Account.IdentityID == ada.ID)
	fmt.Println("Ada:", escrowBalance(wallet, adaNGN), "Chidi Stores:", escrowBalance(wallet, chidi))

	retry, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("80000", "NGN"), Reference: "escrow_10450"})
	check(err)
	fmt.Println("retry returns it:", retry.ID == esc.ID)
	// end
	end()

	begin("escrow/release")
	// sample: escrow/release
	// Ada confirms delivery. A zero Amount releases everything still held.
	paid, err := wallet.ReleaseEscrow(ctx, esc.ID, ledger.EscrowSettleInput{
		Reference:   "escrow_10450_release",
		Description: "Order 10450 delivered",
	})
	check(err)
	fmt.Println(paid.Status, paid.Amount.Display, paid.MetaData["txn_type"])

	after, err := wallet.GetEscrow(ctx, esc.ID)
	check(err)
	fmt.Println(after.Status, "still held:", after.Held.Display, "Chidi Stores:", escrowBalance(wallet, chidi))
	// end
	end()

	begin("escrow/refund")
	// sample: escrow/refund
	// The order is cancelled before it ships: everything goes back to Ada.
	esc2, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("30000", "NGN"), Reference: "escrow_10451"})
	check(err)
	back, err := wallet.RefundEscrow(ctx, esc2.ID, ledger.EscrowSettleInput{Reference: "escrow_10451_refund"})
	check(err)
	fmt.Println(back.Amount.Display, back.MetaData["txn_type"], "to Ada:", back.Destination == string(adaNGN))
	// end
	end()

	begin("escrow/partial")
	// sample: escrow/partial
	// A ₦50,000 order of two items. One arrives broken.
	esc3, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("50000", "NGN"), Reference: "escrow_10452"})
	check(err)

	_, err = wallet.RefundEscrow(ctx, esc3.ID, ledger.EscrowSettleInput{
		Amount: ledger.Amount("12000", "NGN"), Reference: "escrow_10452_refund_1", Description: "Broken item",
	})
	check(err)
	e, err := wallet.GetEscrow(ctx, esc3.ID)
	check(err)
	fmt.Println(e.Status, "still held:", e.Held.Display)

	_, err = wallet.ReleaseEscrow(ctx, esc3.ID, ledger.EscrowSettleInput{
		Amount: ledger.Amount("20000", "NGN"), Reference: "escrow_10452_release_1",
	})
	check(err)
	e, err = wallet.GetEscrow(ctx, esc3.ID)
	check(err)
	fmt.Println(e.Status, "still held:", e.Held.Display)

	_, err = wallet.ReleaseEscrow(ctx, esc3.ID, ledger.EscrowSettleInput{Reference: "escrow_10452_release_2"}) // the rest
	check(err)
	e, err = wallet.GetEscrow(ctx, esc3.ID)
	check(err)
	fmt.Println(e.Status, "still held:", e.Held.Display, "original:", e.Amount.Display)
	// end
	end()

	begin("escrow/over-release")
	// sample: escrow/over-release
	esc4, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("10000", "NGN"), Reference: "escrow_10453"})
	check(err)
	_, err = wallet.ReleaseEscrow(ctx, esc4.ID, ledger.EscrowSettleInput{
		Amount: ledger.Amount("15000", "NGN"), Reference: "escrow_10453_release"})
	fmt.Println("more than held:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.RefundEscrow(ctx, esc4.ID, ledger.EscrowSettleInput{Reference: "escrow_10453_refund"})
	check(err)
	_, err = wallet.ReleaseEscrow(ctx, esc4.ID, ledger.EscrowSettleInput{Reference: "escrow_10453_release_2"})
	fmt.Println("nothing left:", errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()

	begin("escrow/house")
	// sample: escrow/house
	// Ada books an Oja delivery slot. Oja itself is paid on completion.
	booking, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{
		Payer:       adaNGN,
		Beneficiary: ledger.AccountID(ledger.FeeIncomeAccount),
		Amount:      ledger.Amount("2500", "NGN"),
		Reference:   "booking_771",
	})
	check(err)
	_, err = wallet.ReleaseEscrow(ctx, booking.ID, ledger.EscrowSettleInput{Reference: "booking_771_done"})
	check(err)
	income, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	check(err)
	fmt.Println("@FeeIncome:", income.Balance.Display)
	// end
	end()

	begin("escrow/errors")
	// sample: escrow/errors
	_, err = wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: adaNGN,
		Amount: ledger.Amount("100", "NGN"), Reference: "escrow_bad_1"})
	fmt.Println("same payer and beneficiary:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: ledger.AccountID("@PaystackClearing"), Beneficiary: chidi,
		Amount: ledger.Amount("100", "NGN"), Reference: "escrow_bad_2"})
	fmt.Println("house payer:", errors.Is(err, ledger.ErrWrongKind))

	_, err = wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("100", "NGN"), Reference: "seed_ada"})
	fmt.Println("reference of another transaction:", errors.Is(err, ledger.ErrDuplicateReference))

	_, err = wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("9000000", "NGN"), Reference: "escrow_bad_3"})
	fmt.Println("not enough money:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.ReleaseEscrow(ctx, adaNGN, ledger.EscrowSettleInput{Reference: "escrow_bad_4"})
	fmt.Println("not an escrow:", errors.Is(err, ledger.ErrInvalidInput))

	esc5, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{Payer: adaNGN, Beneficiary: chidi,
		Amount: ledger.Amount("5000", "NGN"), Reference: "escrow_10454"})
	check(err)
	check(ops.FreezeAccount(ctx, chidi, "KYC review"))
	_, err = wallet.ReleaseEscrow(ctx, esc5.ID, ledger.EscrowSettleInput{Reference: "escrow_10454_release"})
	fmt.Println("frozen beneficiary:", errors.Is(err, ledger.ErrAccountFrozen))
	// end
	end()
}
