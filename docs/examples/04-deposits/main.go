// Chapter 4: a deposit through a payment provider, the retried webhook, and
// the provider settling.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
)

// deposit is what your Paystack webhook handler does with a successful
// charge. paystackRef is Paystack's own id for it, so a retried webhook
// carries the same one.
// snippet:deposit
func deposit(ctx context.Context, hb *humanblnk.Client, wallet humanblnk.Account, paystackRef, amount string) (humanblnk.Entry, error) {
	m, err := money.Parse(amount, wallet.Currency) // the amount the webhook carried
	if err != nil {
		return humanblnk.Entry{}, err
	}
	return hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:         humanblnk.Reference("deposit", "paystack", paystackRef),
		From:        humanblnk.Single(humanblnk.ClearingAccount("paystack"), m), // Paystack owes you this
		To:          humanblnk.Single(humanblnk.AccountRef(wallet.ID), m),
		Description: "Bank transfer",
		Meta:        humanblnk.Meta{"provider": "paystack", "provider_ref": paystackRef},
	})
}

// end-snippet

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)
	psRef := "ps_" + demo.Run

	// The webhook arrives.
	e, err := deposit(ctx, hb, ada, psRef, "25000")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("recorded", e.ID, e.Amount, e.Status)
	fmt.Println("Ada can spend:", demo.Available(ctx, hb, ada.ID))

	// Paystack sends the same webhook again (it does, on timeouts).
	// snippet:retry
	again, err := deposit(ctx, hb, ada, psRef, "25000")
	switch {
	case errors.Is(err, humanblnk.ErrDuplicate):
		fmt.Println("retried webhook: already recorded as", again.ID, "- answer 200 OK and move on")
	case err != nil:
		log.Fatal(err)
	}
	fmt.Println("Ada can still spend:", demo.Available(ctx, hb, ada.ID))
	// end-snippet

	// A forged or buggy webhook reusing the reference for another amount.
	// snippet:forged
	_, err = deposit(ctx, hb, ada, psRef, "99999")
	fmt.Println("same reference, different amount:", errors.Is(err, humanblnk.ErrReferenceUsed))
	// end-snippet

	// Look a deposit up by your reference, e.g. from a support tool.
	// snippet:lookup
	found, err := hb.Transactions.GetByReference(ctx, humanblnk.Reference("deposit", "paystack", psRef))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("found by reference:", found.ID, found.Amount, found.Meta["provider_ref"])
	// end-snippet

	// Days later Paystack settles: the money reaches your bank account,
	// and Paystack no longer owes it. The clearing account returns to zero.
	// snippet:settle
	settled := money.MustParse("25000", demo.NGN)
	if _, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:         humanblnk.Reference("settlement", "paystack", "stl_"+demo.Run),
		From:        humanblnk.Single(humanblnk.World, settled),
		To:          humanblnk.Single(humanblnk.ClearingAccount("paystack"), settled),
		Description: "Paystack settlement",
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("settlement recorded")
	// end-snippet
}
