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

// transfersPricing charges ₦10 on every naira transfer; anything else is free.
func transfersPricing(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
	if r.Kind == ledger.FeeTransfer && r.Amount.Currency == currency.NGN {
		return ledger.ComputeFee(ledger.FeeRule{Fixed: "10"}, r.Amount)
	}
	return ledger.NoFee, nil
}

func TestTransfers(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{
		Fees:   transfersPricing,
		Limits: []ledger.Limit{{Tier: "tier2", Currency: currency.NGN, PerTransaction: "100000", Daily: "500000"}},
	})
	adaNGN := customer(wallet, "user_8812", "Ada", currency.NGN).Accounts[currency.NGN].ID
	tundeNGN := customer(wallet, "user_9034", "Tunde", currency.NGN).Accounts[currency.NGN].ID
	fund(wallet, adaNGN, "100000", currency.NGN, "seed_ada")

	balances := func() {
		a, err := wallet.GetAccount(ctx, adaNGN)
		check(err)
		b, err := wallet.GetAccount(ctx, tundeNGN)
		check(err)
		fmt.Println("Ada:", a.Balance.Display, "available:", a.Available.Display, "| Tunde:", b.Balance.Display)
	}

	begin("transfers/basic")
	// sample: transfers/basic
	sent, err := wallet.Transfer(ctx, ledger.TransferInput{
		From:        adaNGN,
		To:          tundeNGN,
		Amount:      ledger.Amount("12500", currency.NGN), // what Tunde receives
		Fee:         ledger.NoFee,                         // free: overrides Config.Fees
		Reference:   "p2p_01J8ZK2M4Q",                     // generate once, when Ada taps "Send"
		Description: "Rent share",                         // shown on both statements
	})
	check(err)
	fmt.Println(sent.Amount.Display, sent.EntryStatus, sent.Description, sent.MetaData["txn_type"])
	balances()
	// end
	end()

	begin("transfers/fee")
	// sample: transfers/fee
	// No Fee: Config.Fees prices it at ₦10, paid by Ada on top.
	r, err := wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN,
		Amount:    ledger.Amount("5000", currency.NGN),
		Reference: "p2p_01J8ZK3A7B",
	})
	check(err)
	fmt.Println("debited:", r.Amount.Display, "fee:", r.MetaData["fee"], "to Tunde:", r.MetaData["net_amount"])
	for _, leg := range r.Legs {
		fmt.Println(" leg:", leg.Amount.Display, leg.Narration)
	}
	balances()

	fees, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: currency.NGN})
	check(err)
	fmt.Println("@FeeIncome:", fees.Balance.Display)
	// end
	end()

	begin("transfers/pending")
	// sample: transfers/pending
	// Tunde sells Ada a used phone: reserve ₦30,000 now, settle when she has it.
	p, err := wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN,
		Amount:    ledger.Amount("30000", currency.NGN),
		Fee:       ledger.NoFee,
		Reference: "p2p_01J8ZK4C9D",
		Pending:   true,
	})
	check(err)
	fmt.Println(p.EntryStatus)
	balances()

	// The phone has a cracked screen: they agree on ₦25,000.
	part := ledger.Amount("25000", "")
	_, err = wallet.Confirm(ctx, p.TransactionID, &part)
	check(err)
	_, err = wallet.Cancel(ctx, p.TransactionID) // releases the other ₦5,000
	check(err)
	balances()
	// end
	end()

	begin("transfers/scheduled")
	// sample: transfers/scheduled
	// Ada sends Tunde ₦2,000 tomorrow at 08:00.
	s, err := wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN,
		Amount:       ledger.Amount("2000", currency.NGN),
		Fee:          ledger.NoFee,
		Reference:    "p2p_01J8ZK5E1F",
		ScheduledFor: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
	})
	check(err)
	fmt.Println(s.Status, s.EntryStatus)
	balances()

	core.Advance(24 * time.Hour) // the in-memory Blnk's clock reaches the date
	got, err := wallet.GetTransaction(ctx, s.TransactionID)
	check(err)
	fmt.Println(got.Status, got.EntryStatus)
	balances()
	// end
	end()

	begin("transfers/house")
	// sample: transfers/house
	// Oja pays Ada ₦500 cashback out of its marketing budget.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From:           ledger.AccountID("@MarketingCashback"),
		To:             adaNGN,
		Amount:         ledger.Amount("500", currency.NGN),
		Fee:            ledger.NoFee,
		Reference:      "cashback_sept_8812",
		AllowOverdraft: true, // a house account starts at zero
		Description:    "September cashback",
	})
	check(err)

	// Ada buys ₦1,000 of airtime; the money goes to the house account Oja settles with the vendor.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From:        adaNGN,
		To:          ledger.AccountID("@AirtimePayable"),
		Amount:      ledger.Amount("1000", currency.NGN),
		Fee:         ledger.NoFee,
		Reference:   "airtime_77120",
		Description: "MTN airtime 0803***0000",
	})
	check(err)
	for _, name := range []string{"@MarketingCashback", "@AirtimePayable"} {
		h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: name, Currency: currency.NGN})
		check(err)
		fmt.Println(name, h.Balance.Display)
	}
	balances()
	// end
	end()

	begin("transfers/overdraft")
	// sample: transfers/overdraft
	// Tunde has an approved ₦5,000 overdraft; your app decides who gets one.
	tunde, err := wallet.GetAccount(ctx, tundeNGN)
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: tundeNGN, To: adaNGN,
		Amount:    ledger.Amount("48000", currency.NGN),
		Fee:       ledger.NoFee,
		Reference: "p2p_01J8ZK6G2H",
	})
	fmt.Println("has", tunde.Balance.Display, "- without overdraft:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: tundeNGN, To: adaNGN,
		Amount:         ledger.Amount("48000", currency.NGN),
		Fee:            ledger.NoFee,
		Reference:      "p2p_01J8ZK6G2H",
		AllowOverdraft: true,
	})
	check(err)
	tunde, err = wallet.GetAccount(ctx, tundeNGN)
	check(err)
	fmt.Println("with AllowOverdraft:", tunde.Balance.Display)
	// end
	end()

	begin("transfers/replay")
	// sample: transfers/replay
	in := ledger.TransferInput{
		From: adaNGN, To: tundeNGN, Amount: ledger.Amount("12500", currency.NGN),
		Fee: ledger.NoFee, Reference: "p2p_01J8ZK2M4Q", Description: "Rent share",
	}
	again, err := wallet.Transfer(ctx, in) // the app timed out and sends it again
	check(err)
	fmt.Println("replayed:", again.Replayed, "same transaction:", again.TransactionID == sent.TransactionID)

	in.Amount = ledger.Amount("15000", currency.NGN) // same reference, different payment
	_, err = wallet.Transfer(ctx, in)
	fmt.Println("conflict:", errors.Is(err, ledger.ErrDuplicateReference))
	// end
	end()

	begin("transfers/errors")
	// sample: transfers/errors
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: tundeNGN, To: adaNGN, Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "e1",
	})
	fmt.Println("insufficient funds:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN, Amount: ledger.Amount("150000", currency.NGN), Fee: ledger.NoFee, Reference: "e2",
	})
	fmt.Println("limit:", errors.Is(err, ledger.ErrLimitExceeded), err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: adaNGN, Amount: ledger.Amount("100", currency.NGN), Fee: ledger.NoFee, Reference: "e3",
	})
	fmt.Println("same account:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN, Amount: ledger.Amount("100", currency.NGN), Fee: ledger.NoFee, Reference: "e4",
		Pending: true, ScheduledFor: time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC),
	})
	fmt.Println("pending and scheduled:", errors.Is(err, ledger.ErrInvalidInput))

	check(ops.FreezeAccount(ctx, tundeNGN, "KYC review"))
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN, Amount: ledger.Amount("100", currency.NGN), Fee: ledger.NoFee, Reference: "e5",
	})
	fmt.Println("frozen recipient:", errors.Is(err, ledger.ErrAccountFrozen))
	// end
	end()
}
