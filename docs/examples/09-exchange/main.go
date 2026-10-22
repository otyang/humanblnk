// Chapter 9: a currency exchange, two transactions that apply together or
// not at all.
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
	"github.com/shopspring/decimal"
)

// fxDesk is HumanBlnk's own account for currency it buys and sells: a system
// account, kept in a variable so a typo can't create another.
// snippet:desk
var fxDesk = humanblnk.SystemAccount("FXDesk")

// end-snippet

func main() {
	ctx, hb := demo.Setup()
	usdWallet := demo.Customer(ctx, hb, "ada", demo.USD)
	owner := usdWallet.Owner
	ngnWallet, err := hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: demo.NGN})
	if err != nil {
		log.Fatal(err)
	}
	demo.Deposit(ctx, hb, usdWallet, "200")

	// Ada sells $100 at ₦1,550.25 to the dollar.
	// snippet:exchange
	sell := money.MustParse("100", demo.USD)
	rate := decimal.RequireFromString("1550.25")
	buy, err := money.Round(sell.Amount().Mul(rate), demo.NGN, money.Down) // credit rounds down
	if err != nil {
		log.Fatal(err)
	}
	id := "fx_" + demo.Run
	exchange := []humanblnk.Transaction{
		{
			Ref:  humanblnk.Reference("fx", id, "sell"),
			From: humanblnk.Single(humanblnk.AccountRef(usdWallet.ID), sell), To: humanblnk.Single(fxDesk, sell),
			Description: "Sell USD", Meta: humanblnk.Meta{"rate": rate.String()},
		},
		{
			Ref:  humanblnk.Reference("fx", id, "buy"),
			From: humanblnk.Single(fxDesk, buy), To: humanblnk.Single(humanblnk.AccountRef(ngnWallet.ID), buy),
			Description: "Buy NGN", Meta: humanblnk.Meta{"rate": rate.String()},
		},
	}
	entries, err := hb.Transactions.RecordAll(ctx, exchange)
	if err != nil {
		log.Fatal(err) // ErrReadBack: it applied, but reading it back failed; look it up, don't redo it
	}
	for _, e := range entries {
		fmt.Printf("%s  %s %s\n", e.Ref, e.Amount, e.Amount.Code())
	}
	fmt.Println("USD:", demo.Available(ctx, hb, usdWallet.ID), "| NGN:", demo.Available(ctx, hb, ngnWallet.ID))
	// end-snippet

	// The app retries after a timeout: nothing moves twice.
	// snippet:retry
	_, err = hb.Transactions.RecordAll(ctx, exchange)
	fmt.Println("retried:", errors.Is(err, humanblnk.ErrDuplicate))
	// end-snippet

	// Selling more than she has: neither half applies.
	tooMuch := money.MustParse("500", demo.USD)
	_, err = hb.Transactions.RecordAll(ctx, []humanblnk.Transaction{
		{Ref: humanblnk.Reference("fx", id+"_big", "sell"), From: humanblnk.Single(humanblnk.AccountRef(usdWallet.ID), tooMuch), To: humanblnk.Single(fxDesk, tooMuch), Description: "Sell USD"},
		{Ref: humanblnk.Reference("fx", id+"_big", "buy"), From: humanblnk.Single(fxDesk, money.MustParse("775125", demo.NGN)), To: humanblnk.Single(humanblnk.AccountRef(ngnWallet.ID), money.MustParse("775125", demo.NGN)), Description: "Buy NGN"},
	})
	fmt.Println("too much:", errors.Is(err, humanblnk.ErrInsufficientFunds))
	fmt.Println("USD:", demo.Available(ctx, hb, usdWallet.ID), "| NGN:", demo.Available(ctx, hb, ngnWallet.ID))
}
