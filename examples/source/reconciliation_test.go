package source_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestReconciliation(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN)
	tunde := customer(wallet, "user_9034", "Tunde", currency.NGN)
	adaNGN, tundeNGN := ada.Accounts[currency.NGN].ID, tunde.Accounts[currency.NGN].ID
	fund(wallet, adaNGN, "50000", currency.NGN, "paystack_T100")
	fund(wallet, tundeNGN, "30000", currency.NGN, "paystack_T200")
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	begin("reconciliation/inline")
	// sample: reconciliation/inline
	// Paystack's report for 1 September, parsed from its CSV.
	report := []ledger.ExternalRecord{
		{ID: "PSK-88120", Amount: ledger.Amount("50000", currency.NGN), Reference: "paystack_T100", Date: day},
		{ID: "PSK-88121", Amount: ledger.Amount("30000", currency.NGN), Reference: "paystack_T200", Date: day},
		{ID: "PSK-88122", Amount: ledger.Amount("4300", currency.NGN), Reference: "paystack_T404", Date: day},
	}
	run, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{
		Source:  "paystack",
		Records: report, // up to 10,000 inline
	})
	check(err)

	for !run.Done() { // completed or failed
		time.Sleep(2 * time.Second)
		run, err = ops.GetReconciliation(ctx, run.ID)
		check(err)
	}
	fmt.Println(run.ID, run.Status)
	fmt.Println(run.Matched, "matched,", run.Unmatched, "to investigate")
	// end
	end()

	// Flutterwave reports what it paid, after rounding its own way.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("12345.67", currency.NGN),
		Fee: ledger.NoFee, Reference: "flw_in_5501", Provider: "flutterwave"})
	check(err)

	begin("reconciliation/rule")
	// sample: reconciliation/rule
	// Same reference and currency, amount within 0.1%.
	ruleID, err := ops.CreateMatchingRule(ctx, ledger.MatchingRule{
		Name:        "flutterwave: reference + amount within 0.1%",
		Description: "Flutterwave rounds to the naira",
		Criteria: []ledger.MatchingCriterion{
			{Field: "reference", Operator: "equals"},
			{Field: "currency", Operator: "equals"},
			{Field: "amount", Operator: "equals", AllowableDrift: 0.001},
		},
	})
	check(err)
	fmt.Println("rule:", ruleID)

	flw := []ledger.ExternalRecord{
		{ID: "FLW-3001", Amount: ledger.Amount("12346", currency.NGN), Reference: "flw_in_5501", Date: day},
	}
	strict, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "flutterwave", Records: flw})
	check(err)
	loose, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "flutterwave", Records: flw,
		RuleIDs: []string{ruleID}})
	check(err)
	fmt.Println("default rule:", strict.Matched, "matched;", "with drift:", loose.Matched, "matched")
	// end
	end()

	begin("reconciliation/dry-run")
	// sample: reconciliation/dry-run
	trial, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{
		Source: "paystack", Records: report,
		DryRun: true, // match without recording the result
	})
	check(err)
	fmt.Println("dry run:", trial.DryRun, trial.Matched, "matched,", trial.Unmatched, "unmatched")
	// end
	end()

	begin("reconciliation/fee-legs")
	// sample: reconciliation/fee-legs
	// Oja charges Ada ₦300 on a ₦20,000 Paystack deposit.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("20000", currency.NGN),
		Fee: ledger.Amount("300", ""), Reference: "paystack_T300", Provider: "paystack"})
	check(err)

	// Blnk stores it as two legs, each with its own reference.
	adaSide, err := wallet.History(ctx, adaNGN, ledger.HistoryQuery{Limit: 1})
	check(err)
	feeSide, err := ops.HouseHistory(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: currency.NGN},
		ledger.HistoryQuery{Limit: 1})
	check(err)
	fmt.Println("Ada's leg:", adaSide.Items[0].Reference, adaSide.Items[0].Amount.Display)
	fmt.Println("fee leg:  ", feeSide.Items[0].Reference, feeSide.Items[0].Amount.Display)

	// Paystack reports one line of ₦20,000 under paystack_T300.
	one, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "paystack",
		Records: []ledger.ExternalRecord{{ID: "PSK-88130", Amount: ledger.Amount("20000", currency.NGN),
			Reference: "paystack_T300", Date: day}}})
	check(err)
	fmt.Println("one_to_one:", one.Matched, "matched,", one.Unmatched, "unmatched")
	// end
	end()

	begin("reconciliation/errors")
	// sample: reconciliation/errors
	line := []ledger.ExternalRecord{{ID: "PSK-1", Amount: ledger.Amount("100", currency.NGN), Reference: "r", Date: day}}

	_, err = ops.StartReconciliation(ctx, ledger.ReconcileInput{Records: line})
	fmt.Println("no source:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "paystack"})
	fmt.Println("no records or file:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "paystack", Records: line,
		File: strings.NewReader("id,amount\n")})
	fmt.Println("both records and file:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "paystack",
		Records: []ledger.ExternalRecord{{ID: "PSK-2", Amount: ledger.Amount("100", ""), Reference: "r"}}})
	fmt.Println("record without currency:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "paystack",
		Records: make([]ledger.ExternalRecord, 10001)})
	fmt.Println("over 10,000 records:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.StartReconciliation(ctx, ledger.ReconcileInput{Source: "paystack", Records: line,
		RuleIDs: []string{"rule_missing"}})
	fmt.Println("unknown rule:", errors.Is(err, ledger.ErrNotFound))

	_, err = ops.CreateMatchingRule(ctx, ledger.MatchingRule{Name: "empty"})
	fmt.Println("rule without criteria:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.GetReconciliation(ctx, "recon_missing")
	fmt.Println("unknown run:", errors.Is(err, ledger.ErrNotFound))
	// end
	end()
}
