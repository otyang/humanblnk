package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

// settlementHouse prints a house account's balance as Blnk stores it and on its normal side.
func settlementHouse(ops ledger.Ops, indicator string) {
	h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: indicator, Currency: "NGN"})
	check(err)
	fmt.Printf("  %-22s balance %12s  natural %12s  (%s)\n", h.Indicator, h.Balance.Display, h.Natural.Display, h.Type)
}

func TestSettlement(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	adaNGN := customer(wallet, "user_ada", "Ada", "NGN").Accounts["NGN"]
	tundeNGN := customer(wallet, "user_tunde", "Tunde", "NGN").Accounts["NGN"]

	// Yesterday Paystack collected three top-ups for Oja.
	fund(wallet, adaNGN.ID, "50000", "NGN", "paystack_T1001")
	fund(wallet, tundeNGN.ID, "30000", "NGN", "paystack_T1002")
	fund(wallet, adaNGN.ID, "20000", "NGN", "paystack_T1003")

	begin("settlement/collection")
	// sample: settlement/collection
	fmt.Println("before:")
	settlementHouse(ops, "@PaystackClearing")

	// Paystack's report: ₦100,000 collected, ₦1,500 fees kept, ₦98,500 paid to the bank.
	st, err := ops.RecordSettlement(ctx, ledger.SettlementInput{
		Provider:  "paystack", // → @PaystackClearing
		Direction: ledger.SettlementCollection,
		Amount:    ledger.Amount("100000", "NGN"), // the gross amount it clears
		Fee:       ledger.Amount("1500", "NGN"),
		Reference: "paystack_stl_2026_09_01",
	})
	check(err)
	for _, e := range st.Entries {
		fmt.Println(e.Reference, "|", e.Description, "|", e.Amount.Display, e.Status)
	}

	fmt.Println("after:")
	settlementHouse(ops, "@PaystackClearing")
	settlementHouse(ops, "@BankOperating")
	settlementHouse(ops, "@ProviderFees")
	// end
	end()

	begin("settlement/payout")
	// sample: settlement/payout
	// Monday: Oja's bank sends ₦60,000 to its Flutterwave balance. Flutterwave charges ₦250 for the transfer.
	st, err = ops.RecordSettlement(ctx, ledger.SettlementInput{
		Provider:  "flutterwave", // → @FlutterwaveClearing
		Direction: ledger.SettlementPayout,
		Amount:    ledger.Amount("60000", "NGN"),
		Fee:       ledger.Amount("250", "NGN"),
		Reference: "flw_topup_2026_09_01",
	})
	check(err)
	for _, e := range st.Entries {
		fmt.Println(e.Reference, "|", e.Description, "|", e.Amount.Display)
	}
	fmt.Println("prefunded:")
	settlementHouse(ops, "@FlutterwaveClearing")

	// During the week Flutterwave pays out ₦45,000 of withdrawals from that balance.
	w, err := wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: adaNGN.ID, Amount: ledger.Amount("45000", "NGN"),
		Fee: ledger.NoFee, Reference: "wd_ada_301", Provider: "flutterwave"})
	check(err)
	_, err = wallet.Confirm(ctx, w.TransactionID, nil)
	check(err)
	fmt.Println("after payouts:")
	settlementHouse(ops, "@FlutterwaveClearing")
	settlementHouse(ops, "@BankOperating")
	// end
	end()

	begin("settlement/no-fee")
	// sample: settlement/no-fee
	fund(wallet, tundeNGN.ID, "12000", "NGN", "paystack_T1004")
	st, err = ops.RecordSettlement(ctx, ledger.SettlementInput{
		Provider:    "paystack",
		Amount:      ledger.Amount("12000", "NGN"), // Direction defaults to SettlementCollection
		Reference:   "paystack_stl_2026_09_02",
		Description: "Paystack settlement 2 Sep",
	})
	check(err)
	fmt.Println("entries:", len(st.Entries), "-", st.Entries[0].Description)
	fmt.Println("metadata:", st.Entries[0].MetaData["txn_type"], st.Entries[0].MetaData["settlement_reference"], st.Entries[0].MetaData["provider"])
	settlementHouse(ops, "@PaystackClearing")
	// end
	end()

	begin("settlement/retry")
	// sample: settlement/retry
	// The settlement job crashed after posting and runs again with the same report.
	settlementHouse(ops, "@BankOperating")
	again, err := ops.RecordSettlement(ctx, ledger.SettlementInput{
		Provider:  "paystack",
		Amount:    ledger.Amount("100000", "NGN"),
		Fee:       ledger.Amount("1500", "NGN"),
		Reference: "paystack_stl_2026_09_01",
	})
	check(err)
	fmt.Println("replayed:", again.Replayed, "entries:", len(again.Entries))
	settlementHouse(ops, "@BankOperating")

	// The same reference with a different amount is a conflict, not a replay.
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{
		Provider:  "paystack",
		Amount:    ledger.Amount("110000", "NGN"),
		Fee:       ledger.Amount("1500", "NGN"),
		Reference: "paystack_stl_2026_09_01",
	})
	fmt.Println("different amount:", errors.Is(err, ledger.ErrDuplicateReference))
	// end
	end()

	// Customers who pay by bank transfer into their Providus virtual account
	// are credited with no provider, so the money sits in @DepositClearing.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN.ID, Amount: ledger.Amount("25000", "NGN"),
		Fee: ledger.NoFee, Reference: "providus_va_8812_01"})
	check(err)

	begin("settlement/overrides")
	// sample: settlement/overrides
	// At start-up: tell the chart what the extra accounts are.
	ledger.Chart["@BankProvidus"] = ledger.GLDef{Type: ledger.GLAsset, Normal: ledger.DebitNormal}
	ledger.Chart["@BankCharges"] = ledger.GLDef{Type: ledger.GLExpense, Normal: ledger.DebitNormal}

	// Providus sweeps the virtual-account collections into Oja's Providus account, less ₦50 in charges.
	st, err = ops.RecordSettlement(ctx, ledger.SettlementInput{
		ClearingAccount: ledger.DepositClearingAccount, // "@DepositClearing", instead of Provider
		BankAccount:     "@BankProvidus",               // default "@BankOperating"
		FeeAccount:      "@BankCharges",                // default "@ProviderFees"
		Amount:          ledger.Amount("25000", "NGN"),
		Fee:             ledger.Amount("50", "NGN"),
		Reference:       "providus_sweep_2026_09_01",
	})
	check(err)
	for _, e := range st.Entries {
		fmt.Println(e.Reference, "|", e.Description, "|", e.Amount.Display)
	}
	settlementHouse(ops, "@DepositClearing")
	settlementHouse(ops, "@BankProvidus")
	settlementHouse(ops, "@BankCharges")
	// end
	end()

	begin("settlement/errors")
	// sample: settlement/errors
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Amount: ledger.Amount("1000", "NGN"), Reference: "stl_x1"})
	fmt.Println("no provider:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", Amount: ledger.Amount("1000", "NGN")})
	fmt.Println("no reference:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack",
		Amount: ledger.Amount("1000", "NGN"), Fee: ledger.Amount("1000", "NGN"), Reference: "stl_x2"})
	fmt.Println("fee not below amount:", errors.Is(err, ledger.ErrInvalidInput))
	fmt.Println(err)

	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack",
		Amount: ledger.Amount("1000", "NGN"), Fee: ledger.Amount("10", "USD"), Reference: "stl_x3"})
	fmt.Println("fee in another currency:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack",
		Amount: ledger.Amount("0", "NGN"), Reference: "stl_x4"})
	fmt.Println("zero amount:", errors.Is(err, ledger.ErrInvalidInput))

	// Only house accounts: a customer account here could be taken below zero.
	_, err = ops.RecordSettlement(ctx, ledger.SettlementInput{Provider: "paystack", BankAccount: string(adaNGN.ID),
		Amount: ledger.Amount("1000", "NGN"), Reference: "stl_x5"})
	fmt.Println("customer account as the bank:", errors.Is(err, ledger.ErrWrongKind))
	fmt.Println(err)
	// end
	end()
}
