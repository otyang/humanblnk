package source_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

func TestAdjustments(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", "NGN")
	tunde := customer(wallet, "user_9034", "Tunde", "NGN")
	adaNGN, tundeNGN := ada.Accounts["NGN"].ID, tunde.Accounts["NGN"].ID
	fund(wallet, adaNGN, "20000", "NGN", "paystack_T100")
	fund(wallet, tundeNGN, "5000", "NGN", "paystack_T200")

	begin("adjustments/cashback")
	// sample: adjustments/cashback
	rc, err := ops.Adjust(ctx, ledger.AdjustInput{
		AccountID:   adaNGN,
		Type:        ledger.AdjustCredit, // money in
		Amount:      ledger.Amount("500", "NGN"),
		Reason:      "Cashback: September promo",
		RequestedBy: "ops.kemi",
		ApprovedBy:  "lead.femi", // must not be the requester
		Reference:   "adj_cashback_ada_09",
	})
	check(err)
	fmt.Println(rc.Status, rc.Amount.Display, rc.Amount.Currency)
	fmt.Println("description:", rc.Description)

	keys := make([]string, 0, len(rc.MetaData))
	for k := range rc.MetaData {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s = %v\n", k, rc.MetaData[k])
	}

	acct, err := wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println("Ada's balance:", acct.Balance.Display)
	// end
	end()

	// A Paystack webhook was recorded twice under two references.
	fund(wallet, tundeNGN, "3000", "NGN", "paystack_T300")
	fund(wallet, tundeNGN, "3000", "NGN", "paystack_T300_dup")

	begin("adjustments/correction")
	// sample: adjustments/correction
	// Tunde's ₦3,000 deposit was posted twice. Take the extra one back.
	rc, err = ops.Adjust(ctx, ledger.AdjustInput{
		AccountID:   tundeNGN,
		Type:        ledger.AdjustDebit, // money out
		Amount:      ledger.Amount("3000", "NGN"),
		Reason:      "Duplicate deposit paystack_T300_dup",
		RequestedBy: "ops.kemi",
		ApprovedBy:  "lead.femi",
		Reference:   "adj_dup_paystack_T300",
	})
	check(err)
	fmt.Println(rc.Status, rc.MetaData["txn_type"], rc.Amount.Display)

	acct, err = wallet.GetAccount(ctx, tundeNGN)
	check(err)
	fmt.Println("Tunde's balance:", acct.Balance.Display)
	// end
	end()

	begin("adjustments/retry")
	// sample: adjustments/retry
	// The ops tool timed out and sent the same correction again.
	again, err := ops.Adjust(ctx, ledger.AdjustInput{
		AccountID: tundeNGN, Type: ledger.AdjustDebit, Amount: ledger.Amount("3000", "NGN"),
		Reason:      "Duplicate deposit paystack_T300_dup",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi",
		Reference: "adj_dup_paystack_T300", // same reference
	})
	check(err)
	fmt.Println("replayed:", again.Replayed, "same transaction:", again.TransactionID == rc.TransactionID)

	acct, err = wallet.GetAccount(ctx, tundeNGN)
	check(err)
	fmt.Println("Tunde's balance:", acct.Balance.Display) // taken once only
	// end
	end()

	begin("adjustments/counterparty")
	// sample: adjustments/counterparty
	// A goodwill payment after a failed transfer, paid from the marketing budget.
	_, err = ops.Adjust(ctx, ledger.AdjustInput{
		AccountID:    tundeNGN,
		Type:         ledger.AdjustCredit,
		Amount:       ledger.Amount("1000", "NGN"),
		Reason:       "Goodwill: failed transfer on 30 Aug",
		RequestedBy:  "ops.kemi",
		ApprovedBy:   "lead.femi",
		Reference:    "adj_goodwill_tunde_0830",
		Counterparty: "@Marketing", // any house account; default @Adjustments
	})
	check(err)

	mkt, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@Marketing", Currency: "NGN"})
	check(err)
	fmt.Println(mkt.Indicator, "balance:", mkt.Balance.Display)
	// end
	end()

	begin("adjustments/overdraft")
	// sample: adjustments/overdraft
	// Ada spent a ₦25,000 reversal before it was clawed back. Her balance is ₦20,500.
	in := ledger.AdjustInput{
		AccountID: adaNGN, Type: ledger.AdjustDebit, Amount: ledger.Amount("25000", "NGN"),
		Reason:      "Claw back reversed card payment",
		RequestedBy: "ops.kemi", ApprovedBy: "lead.femi",
		Reference: "adj_clawback_ada_01",
	}
	_, err = ops.Adjust(ctx, in)
	fmt.Println("without AllowOverdraft:", errors.Is(err, ledger.ErrInsufficientFunds))

	in.AllowOverdraft = true // take her below zero; she owes the difference
	_, err = ops.Adjust(ctx, in)
	check(err)
	acct, err = wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println("Ada's balance:", acct.Balance.Display)
	// end
	end()

	begin("adjustments/house")
	// sample: adjustments/house
	adj, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.AdjustmentsAccount, Currency: "NGN"})
	check(err)
	fmt.Println(adj.Indicator, adj.Type, adj.Normal)
	fmt.Println("credits:", adj.Credits.Display, "debits:", adj.Debits.Display, "balance:", adj.Balance.Display)
	// end
	end()

	begin("adjustments/errors")
	// sample: adjustments/errors
	base := ledger.AdjustInput{
		AccountID: tundeNGN, Type: ledger.AdjustCredit, Amount: ledger.Amount("100", "NGN"),
		Reason: "Test", RequestedBy: "ops.kemi", ApprovedBy: "lead.femi", Reference: "adj_err_1",
	}

	self := base
	self.ApprovedBy = "OPS.KEMI" // the same person, in capitals
	_, err = ops.Adjust(ctx, self)
	fmt.Println("self-approval:", errors.Is(err, ledger.ErrApprovalRequired))

	noApprover := base
	noApprover.ApprovedBy = ""
	_, err = ops.Adjust(ctx, noApprover)
	fmt.Println("no approver:", errors.Is(err, ledger.ErrApprovalRequired))

	noReason := base
	noReason.Reason = ""
	_, err = ops.Adjust(ctx, noReason)
	fmt.Println("no reason:", errors.Is(err, ledger.ErrInvalidInput))

	badOther := base
	badOther.Counterparty = string(adaNGN) // a customer account, not "@…"
	_, err = ops.Adjust(ctx, badOther)
	fmt.Println("customer counterparty:", errors.Is(err, ledger.ErrInvalidInput))

	house := base
	house.AccountID = ledger.AccountID(ledger.FeeIncomeAccount)
	_, err = ops.Adjust(ctx, house)
	fmt.Println("house account corrected:", errors.Is(err, ledger.ErrWrongKind))

	noType := base
	noType.Type = 0
	_, err = ops.Adjust(ctx, noType)
	fmt.Println("no type:", errors.Is(err, ledger.ErrInvalidInput))

	check(ops.FreezeAccount(ctx, tundeNGN, "fraud review"))
	_, err = ops.Adjust(ctx, base)
	fmt.Println("frozen:", errors.Is(err, ledger.ErrAccountFrozen))

	changed := base
	changed.Reference = "adj_cashback_ada_09" // used above for a different correction
	check(ops.UnfreezeAccount(ctx, tundeNGN))
	_, err = ops.Adjust(ctx, changed)
	fmt.Println("reused reference:", errors.Is(err, ledger.ErrDuplicateReference))
	// end
	end()
}
