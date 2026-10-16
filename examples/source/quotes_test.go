package source_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

// quotesPricing is Oja's fee table for the quotes page.
func quotesPricing(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
	rules := map[ledger.FeeKind]map[string]ledger.FeeRule{
		ledger.FeeDeposit:    {"NGN": {Percent: "1.5", Max: "2000"}},
		ledger.FeeTransfer:   {"NGN": {Fixed: "10"}},
		ledger.FeeWithdrawal: {"NGN": {Fixed: "50"}},
		ledger.FeeExchange:   {"USD": {Percent: "1"}},
	}
	rule, ok := rules[r.Kind][r.Amount.Currency]
	if !ok {
		return ledger.NoFee, nil
	}
	return ledger.ComputeFee(rule, r.Amount)
}

func TestQuotes(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{
		Fees:   quotesPricing,
		Limits: []ledger.Limit{{Tier: "tier2", Currency: "NGN", PerTransaction: "100000", Daily: "500000"}},
	})
	ada := customer(wallet, "user_8812", "Ada", "NGN", "USD")
	adaNGN, adaUSD := ada.Accounts["NGN"].ID, ada.Accounts["USD"].ID
	tundeNGN := customer(wallet, "user_9034", "Tunde", "NGN").Accounts["NGN"].ID
	fund(wallet, adaNGN, "60000", "NGN", "seed_ada")
	fund(wallet, adaUSD, "300", "USD", "seed_ada_usd")

	begin("quotes/transfer")
	// sample: quotes/transfer
	send := ledger.TransferInput{
		From: adaNGN, To: tundeNGN,
		Amount:      ledger.Amount("12500", "NGN"),
		Reference:   "p2p_01J8ZK2M4Q", // generated when Ada opens the confirm screen
		Description: "Rent share",
	}
	q, err := wallet.QuoteTransfer(ctx, send)
	check(err)
	fmt.Println("Amount:", q.Amount.Display, "| Fee:", q.Fee.Display, "| Debit:", q.Debit.Display, "| Credit:", q.Credit.Display)
	fmt.Println("Available:", q.AvailableBefore.Display, "->", q.AvailableAfter.Display)
	fmt.Println("Allowed:", q.Allowed, "| CheckedByLedger:", q.CheckedByLedger, "| Reason:", q.Reason)
	fmt.Println(q)

	// Ada taps Confirm: send the same input.
	r, err := wallet.Transfer(ctx, send)
	check(err)
	fmt.Println("debited:", r.Amount.Display, "matches the quote:", r.Amount.Value == q.Debit.Value)
	// end
	end()

	begin("quotes/deposit")
	// sample: quotes/deposit
	// "Top up ₦50,000 with your card": show the fee before sending her to Paystack.
	q, err = wallet.QuoteDeposit(ctx, ledger.DepositInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("50000", "NGN"),
		Reference: "paystack_T629384756",
		Provider:  "paystack",
	})
	check(err)
	fmt.Println("Amount:", q.Amount.Display, "| Fee:", q.Fee.Display, "| Debit:", q.Debit.Display, "| Credit:", q.Credit.Display)
	fmt.Println("Available:", q.AvailableBefore.Display, "->", q.AvailableAfter.Display, "| Allowed:", q.Allowed)
	// end
	end()

	begin("quotes/withdraw")
	// sample: quotes/withdraw
	q, err = wallet.QuoteWithdraw(ctx, ledger.WithdrawInput{
		AccountID: adaNGN,
		Amount:    ledger.Amount("20000", "NGN"),
		Reference: "wd_01J8ZM7R2X",
		Provider:  "flutterwave",
	})
	check(err)
	fmt.Printf("You receive ₦%s, fee ₦%s, total ₦%s\n", q.Amount.Display, q.Fee.Display, q.Debit.Display)
	fmt.Println("Available:", q.AvailableBefore.Display, "->", q.AvailableAfter.Display, "| Allowed:", q.Allowed)
	// end
	end()

	begin("quotes/exchange")
	// sample: quotes/exchange
	xq, err := wallet.QuoteExchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN,
		Sell:      ledger.Amount("100", "USD"),
		Rate:      "1532.40",
		Reference: "fx_01J8ZQ0T5C",
	})
	check(err)
	fmt.Println("Sold:", xq.Sold.Display, xq.Sold.Currency, "| Fee:", xq.Fee.Display, "| Debit:", xq.Debit.Display, xq.Debit.Currency)
	fmt.Println("Bought:", xq.Bought.Display, xq.Bought.Currency, "at", xq.Rate)
	fmt.Println("Available:", xq.AvailableBefore.Display, "->", xq.AvailableAfter.Display)
	fmt.Println("Allowed:", xq.Allowed, "| CheckedByLedger:", xq.CheckedByLedger)
	// end
	end()

	begin("quotes/refused")
	// sample: quotes/refused
	tooMuch := send
	tooMuch.Amount, tooMuch.Reference = ledger.Amount("55000", "NGN"), "p2p_01J8ZKA001"
	q, err = wallet.QuoteTransfer(ctx, tooMuch)
	check(err) // a refusal is not an error
	fmt.Println(q.Allowed, errors.Is(q.Reason, ledger.ErrInsufficientFunds), "after:", q.AvailableAfter.Display)

	overLimit := send
	overLimit.Amount, overLimit.Reference = ledger.Amount("150000", "NGN"), "p2p_01J8ZKA002"
	q, err = wallet.QuoteTransfer(ctx, overLimit)
	check(err)
	fmt.Println(q.Allowed, errors.Is(q.Reason, ledger.ErrLimitExceeded))

	check(ops.FreezeAccount(ctx, tundeNGN, "KYC review"))
	q, err = wallet.QuoteTransfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN, Amount: ledger.Amount("100", "NGN"), Reference: "p2p_01J8ZKA003",
	})
	check(err)
	fmt.Println(q.Allowed, errors.Is(q.Reason, ledger.ErrAccountFrozen), "CheckedByLedger:", q.CheckedByLedger)
	check(ops.UnfreezeAccount(ctx, tundeNGN))

	xq, err = wallet.QuoteExchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Sell: ledger.Amount("400", "USD"), Rate: "1532.40", Reference: "fx_01J8ZQ0T6D",
	})
	check(err)
	fmt.Println(xq.Allowed, errors.Is(xq.Reason, ledger.ErrInsufficientFunds))
	// end
	end()

	begin("quotes/errors")
	// sample: quotes/errors
	_, err = wallet.QuoteTransfer(ctx, ledger.TransferInput{
		From: adaNGN, To: tundeNGN, Amount: ledger.Amount("12.345", "NGN"), Reference: "p2p_x1",
	})
	fmt.Println("3 decimals in naira:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.QuoteWithdraw(ctx, ledger.WithdrawInput{AccountID: adaNGN, Amount: ledger.Amount("100", "NGN")})
	fmt.Println("no reference:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.QuoteExchange(ctx, ledger.ExchangeInput{
		From: adaUSD, To: adaNGN, Sell: ledger.Amount("10", "USD"), Rate: "-1", Reference: "fx_x1",
	})
	fmt.Println("bad rate:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.QuoteTransfer(ctx, send) // its reference was used when Ada confirmed
	fmt.Println("reference already used:", errors.Is(err, ledger.ErrDuplicateReference))
	// end
	end()
}
