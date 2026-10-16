package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// suspenseHouse prints a house account's balance on its normal side.
func suspenseHouse(ops ledger.Ops, indicator string) {
	h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: indicator, Currency: currency.NGN})
	check(err)
	fmt.Println(" ", h.Indicator, h.Natural.Display, "("+string(h.Type)+")")
}

func TestSuspense(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	adaNGN := customer(wallet, "user_ada", "Ada", currency.NGN).Accounts[currency.NGN]
	tundeNGN := customer(wallet, "user_tunde", "Tunde", currency.NGN).Accounts[currency.NGN]

	begin("suspense/hold")
	// sample: suspense/hold
	// Paystack reports ₦15,000 paid into Oja's collection account. The narration is blank
	// and the sender name matches no customer.
	r, err := ops.HoldInSuspense(ctx, ledger.SuspenseInput{
		Amount:    ledger.Amount("15000", currency.NGN),
		Provider:  "paystack", // it arrived through @PaystackClearing
		Reference: "paystack_T7700011",
		Note:      "sender OKAFOR J., narration blank",
		MetaData:  map[string]any{"sender_name": "OKAFOR J.", "sender_bank": "Access"},
	})
	check(err)
	fmt.Println(r.Status, "|", r.Description, "|", r.Amount.Display)
	fmt.Println("metadata:", r.MetaData["txn_type"], "/", r.MetaData["suspense_note"], "/", r.MetaData["sender_name"])
	suspenseHouse(ops, ledger.SuspenseAccount)
	suspenseHouse(ops, "@PaystackClearing")
	// end
	end()

	begin("suspense/claim")
	// sample: suspense/claim
	// Tunde writes in: "I sent ₦15,000 from my Access account, it hasn't shown up."
	r, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{
		AccountID:         tundeNGN.ID,
		Amount:            ledger.Amount("15000", currency.NGN),
		Reference:         "claim_7700011",
		SuspenseReference: "paystack_T7700011", // links the claim to what was held
		Note:              "identified by support ticket 4410",
	})
	check(err)
	fmt.Println(r.Status, "|", r.Description, "|", r.MetaData["txn_type"], "for", r.MetaData["suspense_reference"])

	acct, err := wallet.GetAccount(ctx, tundeNGN.ID)
	check(err)
	fmt.Println("Tunde:", acct.Balance.Display)
	suspenseHouse(ops, ledger.SuspenseAccount)
	// end
	end()

	begin("suspense/partial")
	// sample: suspense/partial
	// One ₦30,000 bank transfer, narration "ADA AND TUNDE RENT SHARE".
	_, err = ops.HoldInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("30000", currency.NGN),
		Provider: "paystack", Reference: "paystack_T7700020", Note: "narration: ADA AND TUNDE RENT SHARE"})
	check(err)

	// Ada's share is confirmed first.
	_, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: adaNGN.ID,
		Amount: ledger.Amount("18000", currency.NGN), Reference: "claim_7700020_ada", SuspenseReference: "paystack_T7700020"})
	check(err)
	fmt.Println("after Ada's share:")
	suspenseHouse(ops, ledger.SuspenseAccount)

	// Tunde's the next day.
	_, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: tundeNGN.ID,
		Amount: ledger.Amount("12000", currency.NGN), Reference: "claim_7700020_tunde", SuspenseReference: "paystack_T7700020"})
	check(err)
	fmt.Println("after Tunde's share:")
	suspenseHouse(ops, ledger.SuspenseAccount)
	// end
	end()

	begin("suspense/return")
	// sample: suspense/return
	_, err = ops.HoldInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("8000", currency.NGN),
		Provider: "paystack", Reference: "paystack_T7700031", Note: "sender NWOSU K., no match"})
	check(err)
	fmt.Println("held:")
	suspenseHouse(ops, ledger.SuspenseAccount)
	suspenseHouse(ops, "@PaystackClearing")

	// Nobody claims it in 14 days, so Oja asks Paystack to reverse it to the sender.
	r, err = ops.ReturnFromSuspense(ctx, ledger.ReturnFromSuspenseInput{
		Provider:          "paystack", // back to @PaystackClearing
		Amount:            ledger.Amount("8000", currency.NGN),
		Reference:         "return_7700031",
		SuspenseReference: "paystack_T7700031",
		Note:              "unclaimed after 14 days, reversed to sender",
	})
	check(err)
	fmt.Println("returned:", r.Status, r.MetaData["txn_type"])
	suspenseHouse(ops, ledger.SuspenseAccount)
	suspenseHouse(ops, "@PaystackClearing")
	// end
	end()

	// Chidi Stores closed its old account last month and opened a new one.
	oldShop, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, ExternalID: "merchant_311_old"})
	check(err)
	_, err = ops.CloseAccount(ctx, oldShop.ID, ledger.CloseInput{Reason: "merchant moved to new account",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi"})
	check(err)
	newShop := customer(wallet, "merchant_311", "Chidi", currency.NGN).Accounts[currency.NGN]

	begin("suspense/closed")
	// sample: suspense/closed
	// A customer's Paystack payment to Chidi Stores' old account comes in late.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: oldShop.ID, Amount: ledger.Amount("6500", currency.NGN),
		Fee: ledger.NoFee, Reference: "paystack_T7700042", Provider: "paystack"})
	fmt.Println("deposit:", errors.Is(err, ledger.ErrAccountClosed))

	_, err = ops.HoldInSuspense(ctx, ledger.SuspenseInput{
		Amount: ledger.Amount("6500", currency.NGN), Provider: "paystack", Reference: "paystack_T7700042",
		Note:     "payment for closed account " + string(oldShop.ID),
		MetaData: map[string]any{"intended_account": string(oldShop.ID)},
	})
	check(err)

	r, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{
		AccountID: newShop.ID, Amount: ledger.Amount("6500", currency.NGN),
		Reference: "claim_7700042", SuspenseReference: "paystack_T7700042",
		Note: "moved from closed account",
	})
	check(err)
	acct, err = wallet.GetAccount(ctx, newShop.ID)
	check(err)
	fmt.Println("claimed:", r.Status, "new account:", acct.Balance.Display)
	// end
	end()

	begin("suspense/errors")
	// sample: suspense/errors
	_, err = ops.HoldInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("5000", currency.NGN),
		Provider: "paystack", Reference: "paystack_T7700050"})
	check(err)

	_, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: adaNGN.ID,
		Amount: ledger.Amount("9000", currency.NGN), Reference: "claim_x1"})
	fmt.Println("more than suspense holds:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: ledger.AccountID(ledger.FeeIncomeAccount),
		Amount: ledger.Amount("5000", currency.NGN), Reference: "claim_x2"})
	fmt.Println("claim to a house account:", errors.Is(err, ledger.ErrWrongKind))

	check(ops.FreezeAccount(ctx, adaNGN.ID, "review"))
	_, err = ops.ClaimFromSuspense(ctx, ledger.ClaimFromSuspenseInput{AccountID: adaNGN.ID,
		Amount: ledger.Amount("5000", currency.NGN), Reference: "claim_x3"})
	fmt.Println("claim to a frozen account:", errors.Is(err, ledger.ErrAccountFrozen))

	_, err = ops.ReturnFromSuspense(ctx, ledger.ReturnFromSuspenseInput{ClearingAccount: string(tundeNGN.ID),
		Amount: ledger.Amount("5000", currency.NGN), Reference: "return_x4"})
	fmt.Println("return to a customer account:", errors.Is(err, ledger.ErrWrongKind))

	_, err = ops.HoldInSuspense(ctx, ledger.SuspenseInput{ClearingAccount: string(tundeNGN.ID),
		Amount: ledger.Amount("5000", currency.NGN), Reference: "suspense_x5"})
	fmt.Println("hold from a customer account:", errors.Is(err, ledger.ErrWrongKind))

	_, err = ops.HoldInSuspense(ctx, ledger.SuspenseInput{Amount: ledger.Amount("5000", currency.NGN), Provider: "paystack"})
	fmt.Println("no reference:", errors.Is(err, ledger.ErrInvalidInput))
	// end
	end()
}
