package source_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// limitsShow prints what LimitUsage reports, for the limits page.
func limitsShow(label string, u *ledger.LimitUsage) {
	left := func(m *ledger.Money) string {
		if m == nil {
			return "no cap"
		}
		return m.Display
	}
	fmt.Println(label, "used today", u.DailyUsed.Display, "left today", left(u.DailyLeft),
		"| used this month", u.MonthlyUsed.Display, "left", left(u.MonthlyLeft))
}

func TestLimits(t *testing.T) {
	curT = t // setup runs inside the first sample
	begin("limits/config")
	// sample: limits/config
	lagos := time.FixedZone("WAT", 3600) // Lagos, UTC+1
	cfg := ledger.Config{
		Limits: []ledger.Limit{
			{Tier: "tier1", Currency: currency.NGN, PerTransaction: "50000", Daily: "200000", Monthly: "1000000"},
			{Tier: "tier2", Currency: currency.NGN, PerTransaction: "100000", Daily: "500000", Monthly: "5000000"},
			{Tier: "tier3", Currency: currency.NGN, PerTransaction: "5000000", Daily: "25000000"},
			{Currency: currency.NGN, PerTransaction: "10000", Daily: "30000"}, // accounts with no tier
			{Tier: "*", Currency: currency.USD, Daily: "1000"},                // every tier, in dollars
		},
		Location: lagos, // a day starts at midnight in Lagos
	}
	svc, core, wallet, ops := setup(t, cfg) // in your app: ledger.New(client, cfg)

	tunde, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{
		FirstName: "Tunde", LastName: "Bakare", Currencies: []string{currency.NGN, currency.USD},
		ExternalID: "user_9034",
		MetaData:   map[string]any{"tier": "tier1"}, // the tier is read from here
	})
	check(err)
	tundeNGN := tunde.Accounts[currency.NGN].ID

	u, err := wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	fmt.Printf("%s %s: per transaction %s, daily %s, monthly %s\n",
		u.Limit.Tier, u.Limit.Currency, u.Limit.PerTransaction, u.Limit.Daily, u.Limit.Monthly)
	usd, err := wallet.LimitUsage(ctx, tunde.Accounts[currency.USD].ID)
	check(err)
	fmt.Printf("USD: tier %q, daily %s\n", usd.Limit.Tier, usd.Limit.Daily)
	// end
	end()

	_ = svc
	ada := customer(wallet, "user_8812", "Ada", currency.NGN) // tier2
	adaNGN := ada.Accounts[currency.NGN].ID
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{currency.NGN}, ExternalID: "merchant_311"})
	check(err)
	chidiNGN := shop.Accounts[currency.NGN].ID
	fund(wallet, tundeNGN, "300000", currency.NGN, "seed_tunde")
	fund(wallet, adaNGN, "2000000", currency.NGN, "seed_ada")

	begin("limits/per-transaction")
	// sample: limits/per-transaction
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: adaNGN,
		Amount: ledger.Amount("60000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_31001"})
	fmt.Println(errors.Is(err, ledger.ErrLimitExceeded), err)

	// The fee counts: ₦49,990 + ₦50 is over ₦50,000.
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: tundeNGN, Amount: ledger.Amount("49990", currency.NGN),
		Fee: ledger.Amount("50", ""), Reference: "payout_31002", Provider: "flutterwave"})
	fmt.Println(errors.Is(err, ledger.ErrLimitExceeded), err)
	// end
	end()

	begin("limits/daily")
	// sample: limits/daily
	for i, amount := range []string{"50000", "50000", "50000", "30000"} {
		_, err := wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: chidiNGN,
			Amount: ledger.Amount(amount, currency.NGN), Fee: ledger.NoFee, Reference: fmt.Sprintf("order_%d", 5101+i)}) // order_5101 … order_5104
		check(err)
	}
	u, err = wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	limitsShow("Tunde:", u)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: chidiNGN,
		Amount: ledger.Amount("25000", currency.NGN), Fee: ledger.NoFee, Reference: "order_5105"})
	fmt.Println(errors.Is(err, ledger.ErrLimitExceeded), err)
	// end
	end()

	begin("limits/holds")
	// sample: limits/holds
	// Ada's card is authorised for ₦80,000 at Chidi Stores.
	hold, err := wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN, Destination: chidiNGN,
		Amount: ledger.Amount("80000", currency.NGN), Reference: "card_auth_771"})
	check(err)
	u, err = wallet.LimitUsage(ctx, adaNGN)
	check(err)
	limitsShow("held:", u)

	_, err = wallet.Cancel(ctx, hold.TransactionID) // the order is cancelled
	check(err)
	u, err = wallet.LimitUsage(ctx, adaNGN)
	check(err)
	limitsShow("released:", u)

	// A partial capture counts only what was captured.
	hold, err = wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN, Destination: chidiNGN,
		Amount: ledger.Amount("80000", currency.NGN), Reference: "card_auth_772"})
	check(err)
	part := ledger.Amount("30000", currency.NGN)
	_, err = wallet.Confirm(ctx, hold.TransactionID, &part)
	check(err)
	_, err = wallet.Cancel(ctx, hold.TransactionID)
	check(err)
	u, err = wallet.LimitUsage(ctx, adaNGN)
	check(err)
	limitsShow("captured:", u)
	// end
	end()

	begin("limits/scope")
	// sample: limits/scope
	// Money coming in is never limited.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: tundeNGN, Amount: ledger.Amount("900000", currency.NGN),
		Fee: ledger.NoFee, Reference: "paystack_T31003", Provider: "paystack"})
	fmt.Println("deposit:", err == nil)

	// Moving into a pot is a transfer from Tunde's account, so it counts.
	pot, err := wallet.OpenPot(ctx, ledger.PotInput{ParentID: tundeNGN, Name: "School fees"})
	check(err)
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: pot.ID,
		Amount: ledger.Amount("60000", currency.NGN), Fee: ledger.NoFee, Reference: "save_31004"})
	fmt.Println("into the pot:", errors.Is(err, ledger.ErrLimitExceeded))

	// Bulk items are checked like transfers: each one, and their total.
	_, err = wallet.Bulk(ctx, ledger.BulkTransferInput{Atomic: true, Items: []ledger.TransferInput{
		{From: tundeNGN, To: adaNGN, Amount: ledger.Amount("60000", currency.NGN), Fee: ledger.NoFee, Reference: "bulk_31005_1"},
	}})
	fmt.Println("bulk over the limit:", errors.Is(err, ledger.ErrLimitExceeded))
	u, err = wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	limitsShow("Tunde:", u)

	// Back-office flows don't use up an allowance: a correction that debits Tunde,
	_, err = ops.Adjust(ctx, ledger.AdjustInput{AccountID: tundeNGN, Type: ledger.AdjustDebit,
		Amount: ledger.Amount("5000", currency.NGN), Reason: "duplicate top-up", RequestedBy: "ops.kemi",
		ApprovedBy: "lead.femi", Reference: "adj_31006"})
	check(err)
	u, err = wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	limitsShow("after a correction:", u)

	// or a ₦20,000 refund Chidi Stores pays, though its own cap is ₦10,000 at a time.
	order, err := wallet.GetTransactionByReference(ctx, "order_5101")
	check(err)
	_, err = wallet.RefundPartial(ctx, order.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("20000", currency.NGN), Reference: "refund_5101_1"})
	check(err)
	u, err = wallet.LimitUsage(ctx, chidiNGN)
	check(err)
	limitsShow("Chidi Stores:", u)
	// end
	end()

	client := core.Client() // stands for your blnk.Client

	begin("limits/upgrade")
	// sample: limits/upgrade
	// Tunde verifies his BVN. Your KYC system moves him to tier2 on every account.
	for _, acct := range tunde.Accounts {
		_, err := client.UpdateMetadata(ctx, string(acct.ID), blnk.MetaData{"tier": "tier2"})
		check(err)
	}
	u, err = wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	fmt.Println("now:", u.Limit.Tier, "daily", u.Limit.Daily, "left today", u.DailyLeft.Display)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: chidiNGN,
		Amount: ledger.Amount("90000", currency.NGN), Fee: ledger.NoFee, Reference: "order_5106"})
	fmt.Println("₦90,000 transfer:", err == nil)
	// end
	end()

	begin("limits/next-day")
	// sample: limits/next-day
	// Tunde schedules ₦20,000 for 00:15 on 2 September in Lagos.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: adaNGN,
		Amount: ledger.Amount("20000", currency.NGN), Fee: ledger.NoFee, Reference: "so_31007",
		ScheduledFor: time.Date(2026, 9, 2, 0, 15, 0, 0, lagos)})
	check(err)
	u, err = wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	limitsShow("while it waits:", u)

	core.SetNow(time.Date(2026, 9, 1, 23, 30, 0, 0, time.UTC)) // 00:30 on 2 September in Lagos
	u, err = wallet.LimitUsage(ctx, tundeNGN)
	check(err)
	limitsShow("next day:", u)
	// end
	end()

	begin("limits/quote")
	// sample: limits/quote
	q, err := wallet.QuoteTransfer(ctx, ledger.TransferInput{From: adaNGN, To: chidiNGN,
		Amount: ledger.Amount("150000", currency.NGN), Fee: ledger.NoFee, Reference: "order_5107"})
	check(err)
	fmt.Println("allowed:", q.Allowed, "limit:", errors.Is(q.Reason, ledger.ErrLimitExceeded))
	// end
	end()
}
