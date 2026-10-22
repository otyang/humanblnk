// Chapter 5: a transfer between customers with a fee, checked first with a
// dry run.
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

// transfer builds Ada-pays-Bola with HumanBlnk's 1.5% fee on top, as one split:
// Bola is never paid without the fee being recorded, or the other way round.
// snippet:transfer
func transfer(id string, from, to humanblnk.Account, amount money.Money) humanblnk.Transaction {
	fee, _ := amount.Percent(decimal.RequireFromString("1.5"), money.Up)
	total, _ := amount.Add(fee)
	return humanblnk.Transaction{
		Ref:  humanblnk.Reference("transfer", id),
		From: humanblnk.Single(humanblnk.AccountRef(from.ID), total),
		To: humanblnk.Many(
			humanblnk.Posting{Account: humanblnk.AccountRef(to.ID), Amount: amount},
			humanblnk.Posting{Account: humanblnk.FeeIncome, Amount: fee},
		),
		Description: "Transfer to Bola",
	}
}

// end-snippet

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)
	bola := demo.Customer(ctx, hb, "bola", demo.NGN)
	demo.Deposit(ctx, hb, ada, "50000")

	t := transfer("tr_"+demo.Run, ada, bola, money.MustParse("10000", demo.NGN))

	// 1. Dry run: what would happen? Nothing moves, and the reference stays free.
	// snippet:preview
	p, err := hb.Transactions.Preview(ctx, t)
	if err != nil {
		log.Fatal(err) // the request itself is wrong: bad account, currency, …
	}
	fmt.Println("would apply:", p.WouldApply)
	for _, c := range p.Changes {
		fmt.Printf("  %s: %s -> %s\n", c.Account, c.AvailableBefore, c.AvailableAfter)
	}
	// end-snippet

	// 2. Do it, with the same transaction.
	// snippet:record
	e, err := hb.Transactions.Record(ctx, t)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("recorded %s: %s from Ada, %d postings to\n", e.ID, e.Amount, len(e.To))
	fmt.Println("Ada:", demo.Available(ctx, hb, ada.ID), "| Bola:", demo.Available(ctx, hb, bola.ID))
	// end-snippet

	// 3. Too much: the dry run says why, and Record refuses.
	// snippet:toomuch
	big := transfer("tr_big_"+demo.Run, ada, bola, money.MustParse("100000", demo.NGN))
	p, _ = hb.Transactions.Preview(ctx, big)
	fmt.Println("too much, would apply:", p.WouldApply, "| because:", p.Rejection)
	_, err = hb.Transactions.Record(ctx, big)
	fmt.Println("recording it anyway: insufficient funds?", errors.Is(err, humanblnk.ErrInsufficientFunds))
	// end-snippet

	// 4. Ada's history: money out of her wallet, newest first. (Money into
	// it is a second query with To set.)
	// snippet:history
	out, n, err := hb.Transactions.List(ctx, humanblnk.EntryQuery{From: humanblnk.AccountRef(ada.ID), Limit: 10})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d entries out of Ada's wallet:\n", n)
	for _, e := range out {
		fmt.Printf("  %s  %s  %s\n", e.Ref, e.Amount, e.Status)
	}
	// end-snippet
}
