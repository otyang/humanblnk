package source_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// withdrawalsPricing charges ₦50 on every naira withdrawal.
func withdrawalsPricing(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
	if r.Kind == ledger.FeeWithdrawal && r.Amount.Currency == currency.NGN {
		return ledger.ComputeFee(ledger.FeeRule{Fixed: "50"}, r.Amount)
	}
	return ledger.NoFee, nil
}

func TestWithdrawals(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{
		Fees:   withdrawalsPricing,
		Limits: []ledger.Limit{{Tier: "tier2", Currency: currency.NGN, PerTransaction: "100000", Daily: "500000"}},
	})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN)
	adaNGN := ada.Accounts[currency.NGN].ID
	tundeNGN := customer(wallet, "user_9034", "Tunde", currency.NGN).Accounts[currency.NGN].ID
	fund(wallet, adaNGN, "200000", currency.NGN, "seed_ada")
	fund(wallet, tundeNGN, "30000", currency.NGN, "seed_tunde")

	show := func(id ledger.AccountID, who string) {
		a, err := wallet.GetAccount(ctx, id)
		check(err)
		fmt.Println(who+":", a.Balance.Display, "available:", a.Available.Display, "pending out:", a.PendingOut.Display)
	}

	begin("withdrawals/confirm")
	// sample: withdrawals/confirm
	// Ada withdraws ₦20,000 to her GTBank account through Flutterwave.
	wd, err := wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID:   adaNGN,
		Amount:      ledger.Amount("20000", currency.NGN), // what reaches her bank
		Reference:   "wd_01J8ZM7R2X",                      // generate once, when she taps "Withdraw"
		Provider:    "flutterwave",                        // credits @FlutterwaveClearing
		Description: "Withdrawal to GTBank ****4410",
		// Fee left out: Config.Fees charges ₦50 on top
	})
	check(err)
	fmt.Println(wd.EntryStatus, "total:", wd.Amount.Display, "fee:", wd.MetaData["fee"])
	show(adaNGN, "Ada")

	// Call Flutterwave's transfer API with wd.TransactionID as your reference.
	// Its webhook later says SUCCESSFUL:
	done, err := wallet.Confirm(ctx, wd.TransactionID, nil)
	check(err)
	fmt.Println(done.EntryStatus)
	show(adaNGN, "Ada")
	for _, name := range []string{"@FlutterwaveClearing", ledger.FeeIncomeAccount} {
		h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: name, Currency: currency.NGN})
		check(err)
		fmt.Println(name, h.Balance.Display)
	}
	// end
	end()

	begin("withdrawals/cancel")
	// sample: withdrawals/cancel
	wd, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: tundeNGN,
		Amount:    ledger.Amount("10000", currency.NGN),
		Reference: "wd_01J8ZM9K4T",
		Provider:  "flutterwave",
	})
	check(err)
	show(tundeNGN, "Tunde")

	// Flutterwave says FAILED (wrong account number): give the money back.
	back, err := wallet.Cancel(ctx, wd.TransactionID)
	check(err)
	fmt.Println(back.EntryStatus)
	show(tundeNGN, "Tunde")
	// end
	end()

	begin("withdrawals/immediate")
	// sample: withdrawals/immediate
	// An agent paid Ada ₦5,000 cash over the counter: there is nothing to wait for.
	wd, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID:       adaNGN,
		Amount:          ledger.Amount("5000", currency.NGN),
		Fee:             ledger.NoFee,
		Reference:       "cashout_agent17_5521",
		ClearingAccount: "@AgentCashPayable", // exact account, overrides Provider
		Immediate:       true,
	})
	check(err)
	fmt.Println(wd.EntryStatus)
	show(adaNGN, "Ada")
	// end
	end()

	begin("withdrawals/expiry")
	// sample: withdrawals/expiry
	// Default: a withdrawal the provider never answers is cancelled after
	// Config.WithdrawalHoldTTL (24h).
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: tundeNGN, Amount: ledger.Amount("3000", currency.NGN), Fee: ledger.NoFee,
		Reference: "wd_01J8ZMB1C2", Provider: "flutterwave",
	})
	check(err)
	// ExpiresIn sets a shorter window for this one.
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: tundeNGN, Amount: ledger.Amount("2000", currency.NGN), Fee: ledger.NoFee,
		Reference: "wd_01J8ZMB3D4", Provider: "flutterwave",
		ExpiresIn: 2 * time.Hour,
	})
	check(err)
	show(tundeNGN, "Tunde")

	core.Advance(3 * time.Hour)
	show(tundeNGN, "after 3h")
	core.Advance(22 * time.Hour)
	show(tundeNGN, "after 25h")
	// end
	end()

	begin("withdrawals/batch")
	// sample: withdrawals/batch
	// Flutterwave's settlement file reports three payouts: two paid, one failed.
	var ids []string
	for i, amount := range []string{"1000", "2000", "3000"} {
		wd, err := wallet.Withdraw(ctx, ledger.WithdrawInput{
			AccountID: adaNGN, Amount: ledger.Amount(amount, currency.NGN),
			Reference: fmt.Sprintf("wd_batch_%d", i+1), Provider: "flutterwave",
		})
		check(err)
		ids = append(ids, wd.TransactionID)
	}

	confirmed, err := wallet.ConfirmMany(ctx, []ledger.ConfirmItem{
		{TransactionID: ids[0]},
		{TransactionID: ids[1]},
	})
	check(err)
	cancelled, err := wallet.CancelMany(ctx, []string{ids[2], ids[0]}) // ids[0] is already paid
	check(err)
	labels := []string{"confirm #1", "confirm #2", "cancel #3", "cancel #1"}
	for i, r := range append(confirmed, cancelled...) {
		fmt.Println(labels[i], "ok:", r.OK, "already finalized:", errors.Is(r.Err, ledger.ErrAlreadyFinalized))
	}
	show(adaNGN, "Ada")
	// end
	end()

	begin("withdrawals/errors")
	// sample: withdrawals/errors
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: tundeNGN, Amount: ledger.Amount("90000", currency.NGN), Reference: "e1", Provider: "flutterwave",
	})
	fmt.Println("insufficient funds:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: adaNGN, Amount: ledger.Amount("100000", currency.NGN), Reference: "e2", Provider: "flutterwave",
	})
	fmt.Println("limit (100,000 + 50 fee):", errors.Is(err, ledger.ErrLimitExceeded))

	_, err = wallet.Confirm(ctx, ids[0], nil)
	fmt.Println("confirmed twice:", errors.Is(err, ledger.ErrAlreadyFinalized))

	wd, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: adaNGN, Amount: ledger.Amount("4000", currency.NGN), Reference: "e3", Provider: "flutterwave",
	})
	check(err)
	part := ledger.Amount("1000", "")
	_, err = wallet.Confirm(ctx, wd.TransactionID, &part)
	fmt.Println("partial with a fee:", errors.Is(err, ledger.ErrInvalidInput))

	check(ops.FreezeLedger(ctx, ada.Accounts[currency.NGN].LedgerID, "incident 42"))
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: adaNGN, Amount: ledger.Amount("1000", currency.NGN), Reference: "e4", Provider: "flutterwave",
	})
	fmt.Println("ledger frozen:", errors.Is(err, ledger.ErrLedgerFrozen))
	// end
	end()
}
