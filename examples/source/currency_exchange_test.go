package source_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// exchangePricing charges 1% of the dollars sold. Selling naira is free.
func exchangePricing(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
	if r.Kind == ledger.FeeExchange && r.Amount.Currency == currency.USD {
		return ledger.ComputeFee(ledger.FeeRule{Percent: "1"}, r.Amount)
	}
	return ledger.NoFee, nil
}

func TestCurrencyExchange(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{Fees: exchangePricing})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN, currency.USD)
	adaNGN, adaUSD := ada.Accounts[currency.NGN].ID, ada.Accounts[currency.USD].ID
	tunde := customer(wallet, "user_9034", "Tunde", currency.NGN, currency.USD)
	tundeNGN, tundeUSD := tunde.Accounts[currency.NGN].ID, tunde.Accounts[currency.USD].ID
	fund(wallet, adaUSD, "300", currency.USD, "seed_ada_usd")
	fund(wallet, tundeNGN, "200000", currency.NGN, "seed_tunde")

	pool := func() {
		for _, cur := range []string{currency.USD, currency.NGN} {
			h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FXPoolAccount, Currency: cur})
			check(err)
			fmt.Println("@FXPool", cur, h.Balance.Display)
		}
	}

	begin("currency-exchange/sell")
	// sample: currency-exchange/sell
	// Ada converts $100 to naira at the rate Oja quoted: ₦1,532.40 per $1.
	fx := ledger.ExchangeInput{
		From:      adaUSD,
		To:        adaNGN,
		Sell:      ledger.Amount("100", currency.USD), // exactly this leaves her USD account
		Rate:      "1532.40",                          // NGN per 1 USD
		Reference: "fx_01J8ZQ0T5C",
		// Fee left out: Config.Fees charges 1% of the dollars, on top
	}
	x, err := wallet.Exchange(ctx, fx)
	check(err)
	fmt.Println("Sold:", x.Sold.Display, x.Sold.Currency, "| Fee:", x.Fee.Display, "| Bought:", x.Bought.Display, x.Bought.Currency, "| Rate:", x.Rate)
	fmt.Println("Debit leg:", x.Debit.Reference, x.Debit.Amount.Display, x.Debit.Amount.Currency, x.Debit.EntryStatus)
	fmt.Println("Credit leg:", x.Credit.Reference, x.Credit.Amount.Display, x.Credit.Amount.Currency, x.Credit.EntryStatus)
	fmt.Println("pair:", x.Debit.MetaData["fx_pair"], "rate:", x.Debit.MetaData["fx_rate"])

	usd, err := wallet.GetAccount(ctx, adaUSD)
	check(err)
	ngn, err := wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println("Ada USD:", usd.Balance.Display, "| Ada NGN:", ngn.Balance.Display)
	// end
	end()

	begin("currency-exchange/buy")
	// sample: currency-exchange/buy
	// Tunde needs exactly $25.00 for a subscription: Buy fixes what arrives.
	y, err := wallet.Exchange(ctx, ledger.ExchangeInput{
		From:      tundeNGN,
		To:        tundeUSD,
		Buy:       ledger.Amount("25", currency.USD),
		Rate:      "0.000652", // USD per 1 NGN
		Reference: "fx_01J8ZQ1A2B",
	})
	check(err)
	fmt.Println("Sold:", y.Sold.Display, y.Sold.Currency, "| Fee:", y.Fee.Display, "| Bought:", y.Bought.Display, y.Bought.Currency)
	// end
	end()

	begin("currency-exchange/rounding")
	// sample: currency-exchange/rounding
	// Selling: the amount bought rounds down. 3.33 × 1532.47 = 5103.1251 naira.
	q, err := wallet.QuoteExchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Sell: ledger.Amount("3.33", currency.USD), Rate: "1532.47",
		Fee: ledger.NoFee, Reference: "fx_round_1",
	})
	check(err)
	fmt.Println("sell 3.33 USD -> buy", q.Bought.Display, "NGN")

	// Buying: the amount sold rounds up. 1000 / 1532.47 = 0.65254... dollars.
	q, err = wallet.QuoteExchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Buy: ledger.Amount("1000", currency.NGN), Rate: "1532.47",
		Fee: ledger.NoFee, Reference: "fx_round_2",
	})
	check(err)
	fmt.Println("buy 1000 NGN -> sell", q.Sold.Display, "USD")
	// end
	end()

	begin("currency-exchange/pool")
	// sample: currency-exchange/pool
	// Oja took in $101 from Ada and paid out naira; Tunde did the opposite.
	pool()
	fees, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: currency.USD})
	check(err)
	fmt.Println("@FeeIncome USD", fees.Balance.Display)
	// end
	end()

	begin("currency-exchange/replay")
	// sample: currency-exchange/replay
	again, err := wallet.Exchange(ctx, fx) // the app timed out and sends it again
	check(err)
	fmt.Println("replayed:", again.Replayed, "same legs:", again.Debit.TransactionID == x.Debit.TransactionID)

	changed := fx
	changed.Rate = "1540.00" // a new rate needs a new reference
	_, err = wallet.Exchange(ctx, changed)
	fmt.Println("conflict:", errors.Is(err, ledger.ErrDuplicateReference))
	pool()
	// end
	end()

	begin("currency-exchange/errors")
	// sample: currency-exchange/errors
	_, err = wallet.Exchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Sell: ledger.Amount("500", currency.USD), Rate: "1532.40", Reference: "e1",
	})
	fmt.Println("insufficient funds:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.Exchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Sell: ledger.Amount("10", currency.USD), Buy: ledger.Amount("15000", currency.NGN),
		Rate: "1532.40", Reference: "e2",
	})
	fmt.Println("Sell and Buy:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Exchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: tundeUSD, Sell: ledger.Amount("10", currency.USD), Rate: "1", Reference: "e3",
	})
	fmt.Println("same currency:", err)

	_, err = wallet.Exchange(ctx, ledger.ExchangeInput{
		From: tundeNGN, To: tundeUSD, Sell: ledger.Amount("0.01", currency.NGN), Rate: "0.000652", Reference: "e4",
	})
	fmt.Println("too small:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Exchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: ledger.AccountID(ledger.FXPoolAccount), Sell: ledger.Amount("10", currency.USD),
		Rate: "1532.40", Reference: "e5",
	})
	fmt.Println("house account:", errors.Is(err, ledger.ErrWrongKind))

	check(ops.FreezeAccount(ctx, adaNGN, "KYC review"))
	_, err = wallet.Exchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Sell: ledger.Amount("10", currency.USD), Rate: "1532.40", Reference: "e6",
	})
	fmt.Println("frozen destination:", errors.Is(err, ledger.ErrAccountFrozen))
	// end
	end()
}
