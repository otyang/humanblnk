package source_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestFees(t *testing.T) {
	svc, core, wallet, ops := setup(t, ledger.Config{})
	client := core.Client() // stands for your blnk.Client
	ada := customer(wallet, "user_8812", "Ada", currency.NGN, currency.USD)
	adaNGN, adaUSD := ada.Accounts[currency.NGN].ID, ada.Accounts[currency.USD].ID
	ledgerID := ada.Accounts[currency.NGN].LedgerID
	tunde := customer(wallet, "user_9034", "Tunde", currency.NGN, currency.USD)
	tundeNGN := tunde.Accounts[currency.NGN].ID
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{currency.NGN}, ExternalID: "merchant_311", MetaData: map[string]any{"tier": "tier3"}})
	check(err)
	chidiNGN := shop.Accounts[currency.NGN].ID
	fund(wallet, tundeNGN, "100000", currency.NGN, "seed_tunde")
	fund(wallet, chidiNGN, "200000", currency.NGN, "seed_chidi")
	fund(wallet, adaUSD, "100", currency.USD, "seed_ada_usd")

	begin("fees/explicit")
	// sample: fees/explicit
	// Paystack says ₦50,000 arrived for Ada; Oja keeps ₦750.
	dep, err := wallet.Deposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("50000", currency.NGN), // gross, as Paystack received it
		Fee:       ledger.Amount("750", ""),             // "" takes the amount's currency
		Reference: "paystack_T100201",
		Provider:  "paystack",
	})
	check(err)
	for _, leg := range dep.Legs { // the fee leg has a fixed amount; the rest goes to Ada
		fmt.Println("leg:", leg.Account, leg.Narration, leg.Amount.Display, leg.Distribution)
	}
	fmt.Println("fee:", dep.MetaData["fee"], "net:", dep.MetaData["net_amount"])

	// Tunde sends Ada ₦5,000 for free.
	tr, err := wallet.Transfer(ctx, ledger.TransferInput{
		From: tundeNGN, To: adaNGN, Amount: ledger.Amount("5000", currency.NGN),
		Fee:       ledger.NoFee, // deliberately free
		Reference: "p2p_20001",
	})
	check(err)
	fmt.Println("free transfer:", tr.Amount.Display, "legs:", len(tr.Legs))
	// end
	end()

	begin("fees/rule")
	// sample: fees/rule
	rules := []ledger.FeeRule{
		{Percent: "1.5", Max: "2000"},           // 1.5%, capped at ₦2,000
		{Percent: "0.5", Min: "10", Max: "100"}, // 0.5%, at least ₦10, at most ₦100
		{Percent: "1", Fixed: "100"},            // 1% plus ₦100
		{Tiers: []ledger.FeeTier{ // banded by amount
			{UpTo: "5000", Rule: ledger.FeeRule{Fixed: "10"}},
			{UpTo: "50000", Rule: ledger.FeeRule{Fixed: "25"}},
			{Rule: ledger.FeeRule{Fixed: "50"}}, // everything above
		}},
	}
	for _, amount := range []string{"1500", "20000", "250000"} {
		line := amount + " NGN:"
		for _, rule := range rules {
			fee, err := svc.ComputeFee(rule, ledger.Amount(amount, currency.NGN))
			check(err)
			line += " " + fee.Display
		}
		fmt.Println(line)
	}

	// 1.5% of ₦333.33 is ₦4.99995: the rounding mode decides the last kobo.
	modes := []ledger.Rounding{ledger.RoundHalfUp, ledger.RoundUp, ledger.RoundDown}
	for i, name := range []string{"RoundHalfUp", "RoundUp", "RoundDown"} {
		fee, err := svc.ComputeFee(ledger.FeeRule{Percent: "1.5", Rounding: modes[i]}, ledger.Amount("333.33", currency.NGN))
		check(err)
		fmt.Println(name, fee.Display)
	}
	// end
	end()

	begin("fees/convert")
	// sample: fees/convert
	// Oja charges $0.50 on every withdrawal, in the withdrawal's currency.
	amount, err := svc.Parse("20000", currency.NGN) // resolved, like FeeRequest.Amount
	check(err)
	r := ledger.FeeRequest{Kind: ledger.FeeWithdrawal, Amount: amount}

	fee, err := r.Convert(ledger.Amount("0.50", currency.USD), "1532.40", ledger.RoundUp)
	check(err)
	fmt.Println(fee.Value, fee.Currency)

	odd, err := r.Convert(ledger.Amount("0.50", currency.USD), "1532.333", ledger.RoundUp)
	check(err)
	fmt.Println(odd.Value, odd.Currency, "(766.1665 rounded up)")
	// end
	end()

	begin("fees/config")
	// sample: fees/config
	transferFee := "10" // in production, read from your pricing table
	usdRate := map[string]string{currency.NGN: "1532.40", currency.USD: "1"}

	pricing := func(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
		if r.Account != nil && r.Account.MetaData["tier"] == "tier3" {
			return ledger.NoFee, nil // merchants on tier3 pay nothing
		}
		switch {
		case r.Kind == ledger.FeeDeposit && r.Amount.Currency == currency.NGN:
			return ledger.ComputeFee(ledger.FeeRule{Percent: "1.5", Max: "2000"}, r.Amount)
		case r.Kind == ledger.FeeTransfer && r.Amount.Currency == currency.NGN:
			return ledger.ComputeFee(ledger.FeeRule{Fixed: transferFee}, r.Amount)
		case r.Kind == ledger.FeeWithdrawal && usdRate[r.Amount.Currency] != "":
			return r.Convert(ledger.Amount("0.50", currency.USD), usdRate[r.Amount.Currency], ledger.RoundUp)
		}
		return ledger.Money{}, fmt.Errorf("no price for a %s in %s", r.Kind, r.Amount.Currency)
	}

	priced := ledger.New(client, ledger.Config{LedgerID: ledgerID, Fees: pricing})
	pw := priced.Wallet()

	d, err := pw.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("20000", currency.NGN),
		Reference: "paystack_T100202", Provider: "paystack"}) // no Fee: Config.Fees prices it
	check(err)
	fmt.Println("deposit fee:", d.MetaData["fee"])

	w, err := pw.Withdraw(ctx, ledger.WithdrawInput{AccountID: adaNGN, Amount: ledger.Amount("20000", currency.NGN),
		Reference: "payout_7001", Provider: "flutterwave", Immediate: true})
	check(err)
	fmt.Println("withdrawal: debited", w.Amount.Display, "fee", w.MetaData["fee"])

	m, err := pw.Transfer(ctx, ledger.TransferInput{From: chidiNGN, To: tundeNGN,
		Amount: ledger.Amount("15000", currency.NGN), Reference: "chidi_refund_88"})
	check(err)
	fmt.Println("tier3 transfer:", m.Amount.Display, "legs:", len(m.Legs))
	// end
	end()

	begin("fees/waiver")
	// sample: fees/waiver
	// Free-transfer weekend: the app passes Fee itself, so Config.Fees isn't asked.
	promo, err := pw.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("2000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_20002",
		MetaData: map[string]any{"promo": "free-weekend"}})
	check(err)
	fmt.Println("promo:", promo.Amount.Display, "legs:", len(promo.Legs))

	// A half-price offer: price as usual, then charge less.
	q, err := pw.QuoteTransfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("2000", currency.NGN), Reference: "p2p_20003"})
	check(err)
	half, err := priced.ComputeFee(ledger.FeeRule{Percent: "50"}, ledger.Amount(q.Fee.Value, currency.NGN))
	check(err)
	offer, err := pw.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("2000", currency.NGN), Fee: half, Reference: "p2p_20003"})
	check(err)
	fmt.Println("usual fee:", q.Fee.Display, "charged:", offer.MetaData["fee"])
	// end
	end()

	begin("fees/retry")
	// sample: fees/retry
	in := ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("3000", currency.NGN), Reference: "p2p_20004"}
	first, err := pw.Transfer(ctx, in)
	check(err)
	fmt.Println("first attempt fee:", first.MetaData["fee"])

	transferFee = "15" // finance changes the price; the app retries a timed-out call
	_, err = pw.Transfer(ctx, in)
	fmt.Println("retry, re-priced:", errors.Is(err, ledger.ErrDuplicateReference))

	in.Fee = ledger.Amount(first.MetaData["fee"].(string), "") // retry with the fee first charged
	again, err := pw.Transfer(ctx, in)
	check(err)
	fmt.Println("retry with the original fee: replayed", again.Replayed)
	// end
	end()

	begin("fees/income")
	// sample: fees/income
	income, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: currency.NGN})
	check(err)
	fmt.Println(income.Indicator, income.Type, "balance:", income.Balance.Display)

	h, err := ops.HouseHistory(ctx, income.Ref(), ledger.HistoryQuery{Limit: 3})
	check(err)
	for _, it := range h.Items {
		fmt.Println(it.Reference, it.Amount.Display, it.Description)
	}
	// end
	end()

	begin("fees/errors")
	// sample: fees/errors
	// No Fee and no Config.Fees: refused before anything reaches Blnk.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("100", currency.NGN), Reference: "p2p_e1"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	// Config.Fees has no price for a USD transfer.
	_, err = pw.Transfer(ctx, ledger.TransferInput{From: adaUSD, To: tunde.Accounts[currency.USD].ID,
		Amount: ledger.Amount("10", currency.USD), Reference: "p2p_e2"})
	fmt.Println(errors.Is(err, ledger.ErrFeePricing), err)

	// A fee in another currency than the transaction.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("100", currency.NGN), Fee: ledger.Amount("0.10", currency.USD), Reference: "p2p_e3"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	// A deposit fee as large as the deposit.
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: adaNGN, Amount: ledger.Amount("100", currency.NGN),
		Fee: ledger.Amount("100", ""), Reference: "paystack_e4", Provider: "paystack"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()
}
