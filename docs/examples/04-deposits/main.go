// Chapter 4: a deposit through a payment provider, the retried webhook, and
// the provider settling.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
)

// deposit is what your Paystack webhook handler does with a successful
// charge. paystackRef is Paystack's own id for it, so a retried webhook
// carries the same one.
// snippet:deposit
func deposit(ctx context.Context, books *ledger.Ledger, wallet ledger.Account, paystackRef, amount string) (ledger.Entry, error) {
	m, err := money.Parse(amount, wallet.Currency) // the amount the webhook carried
	if err != nil {
		return ledger.Entry{}, err
	}
	return books.Transactions.Record(ctx, ledger.Transaction{
		Ref:         ledger.Reference("deposit", "paystack", paystackRef),
		From:        ledger.Single(ledger.ClearingAccount("paystack"), m), // Paystack owes HumanBlnk this
		To:          ledger.Single(ledger.AccountRef(wallet.ID), m),
		Description: "Bank transfer",
		Meta:        ledger.Meta{"provider": "paystack", "provider_ref": paystackRef},
	})
}

// end-snippet

func main() {
	ctx, books := demo.Setup()
	ada := demo.Customer(ctx, books, "ada", demo.NGN)
	psRef := "ps_" + demo.Run

	// The webhook arrives.
	e, err := deposit(ctx, books, ada, psRef, "25000")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("recorded", e.ID, e.Amount, e.Status)
	fmt.Println("Ada can spend:", demo.Available(ctx, books, ada.ID))

	// Paystack sends the same webhook again (it does, on timeouts).
	// snippet:retry
	again, err := deposit(ctx, books, ada, psRef, "25000")
	switch {
	case errors.Is(err, ledger.ErrDuplicate):
		fmt.Println("retried webhook: already recorded as", again.ID, "- answer 200 OK and move on")
	case err != nil:
		log.Fatal(err)
	}
	fmt.Println("Ada can still spend:", demo.Available(ctx, books, ada.ID))
	// end-snippet

	// A forged or buggy webhook reusing the reference for another amount.
	// snippet:forged
	_, err = deposit(ctx, books, ada, psRef, "99999")
	fmt.Println("same reference, different amount:", errors.Is(err, ledger.ErrReferenceUsed))
	// end-snippet

	// Look a deposit up by your reference, e.g. from a support tool.
	// snippet:lookup
	found, err := books.Transactions.GetByReference(ctx, ledger.Reference("deposit", "paystack", psRef))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("found by reference:", found.ID, found.Amount, found.Meta["provider_ref"])
	// end-snippet

	// Days later Paystack settles: the money reaches HumanBlnk's bank account,
	// and Paystack no longer owes it. The clearing account returns to zero.
	// snippet:settle
	settled := money.MustParse("25000", demo.NGN)
	if _, err := books.Transactions.Record(ctx, ledger.Transaction{
		Ref:         ledger.Reference("settlement", "paystack", "stl_"+demo.Run),
		From:        ledger.Single(ledger.World, settled),
		To:          ledger.Single(ledger.ClearingAccount("paystack"), settled),
		Description: "Paystack settlement",
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("settlement recorded")
	// end-snippet
}
