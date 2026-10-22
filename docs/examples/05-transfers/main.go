// Chapter 5: a transfer between customers with a fee, checked first with a
// dry run.
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
	"github.com/shopspring/decimal"
)

// transfer builds Ada-pays-Bola with HumanBlnk's 1.5% fee on top, as one split:
// Bola is never paid without the fee being recorded, or the other way round.
// snippet:transfer
func transfer(id string, from, to ledger.Account, amount money.Money) ledger.Transaction {
	fee, _ := amount.Percent(decimal.RequireFromString("1.5"), money.Up)
	total, _ := amount.Add(fee)
	return ledger.Transaction{
		Ref:  ledger.Reference("transfer", id),
		From: ledger.Single(ledger.AccountRef(from.ID), total),
		To: ledger.Many(
			ledger.Posting{Account: ledger.AccountRef(to.ID), Amount: amount},
			ledger.Posting{Account: ledger.FeeIncome, Amount: fee},
		),
		Description: "Transfer to Bola",
	}
}

// end-snippet

func main() {
	ctx, books := demo.Setup()
	ada := demo.Customer(ctx, books, "ada", demo.NGN)
	bola := demo.Customer(ctx, books, "bola", demo.NGN)
	demo.Deposit(ctx, books, ada, "50000")

	t := transfer("tr_"+demo.Run, ada, bola, money.MustParse("10000", demo.NGN))

	// 1. Dry run: what would happen? Nothing moves, and the reference stays free.
	// snippet:preview
	p, err := books.Transactions.Preview(ctx, t)
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
	e, err := books.Transactions.Record(ctx, t)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("recorded %s: %s from Ada, %d postings to\n", e.ID, e.Amount, len(e.To))
	fmt.Println("Ada:", demo.Available(ctx, books, ada.ID), "| Bola:", demo.Available(ctx, books, bola.ID))
	// end-snippet

	// 3. Too much: the dry run says why, and Record refuses.
	// snippet:toomuch
	big := transfer("tr_big_"+demo.Run, ada, bola, money.MustParse("100000", demo.NGN))
	p, _ = books.Transactions.Preview(ctx, big)
	fmt.Println("too much, would apply:", p.WouldApply, "| because:", p.Rejection)
	_, err = books.Transactions.Record(ctx, big)
	fmt.Println("recording it anyway: insufficient funds?", errors.Is(err, ledger.ErrInsufficientFunds))
	// end-snippet

	// 4. Ada's history: money out of her wallet, newest first. (Money into
	// it is a second query with To set.)
	// snippet:history
	out, n, err := books.Transactions.List(ctx, ledger.EntryQuery{From: ledger.AccountRef(ada.ID), Limit: 10})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d entries out of Ada's wallet:\n", n)
	for _, e := range out {
		fmt.Printf("  %s  %s  %s\n", e.Ref, e.Amount, e.Status)
	}
	// end-snippet
}
