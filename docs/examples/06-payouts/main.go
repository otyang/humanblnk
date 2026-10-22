// Chapter 6: holds. Reserve money now, settle it when the outside world
// answers.
package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
)

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)
	demo.Deposit(ctx, hb, ada, "50000")
	ngn := func(v string) money.Money { return money.MustParse(v, demo.NGN) }

	// --- A withdrawal to Ada's bank account ---
	// snippet:place
	payout := humanblnk.Transaction{
		Ref:         humanblnk.Reference("payout", "po_1_"+demo.Run),
		From:        humanblnk.Single(humanblnk.AccountRef(ada.ID), ngn("20000")),
		To:          humanblnk.Single(humanblnk.World, ngn("20000")), // leaving HumanBlnk
		Description: "Withdrawal to GTBank ****1234",
	}
	hold, err := hb.Holds.Place(ctx, payout, time.Now().Add(72*time.Hour))
	if err != nil {
		log.Fatal(err) // humanblnk.ErrInsufficientFunds: tell Ada
	}
	acct, _ := hb.Accounts.Get(ctx, ada.ID)
	fmt.Printf("held %s: Total %s, Held %s, Available %s\n", hold, acct.Total, acct.Held, acct.Available)

	// Placing it again (a retry) gives back the same hold.
	same, _ := hb.Holds.Place(ctx, payout, time.Now().Add(72*time.Hour))
	fmt.Println("placed again, same hold:", same == hold)
	// end-snippet

	// The bank confirms: capture all of it.
	// snippet:capture
	e, err := hb.Holds.Capture(ctx, hold, money.Money{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("captured", e.Amount, "| Ada:", demo.Available(ctx, hb, ada.ID))

	// Capturing again (a retried callback) finishes nothing twice.
	again, err := hb.Holds.Capture(ctx, hold, money.Money{})
	fmt.Println("captured again:", again.Amount, err)
	// end-snippet

	// --- A withdrawal the bank rejects ---
	// snippet:rejected
	h2, err := hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref:  humanblnk.Reference("payout", "po_2_"+demo.Run),
		From: humanblnk.Single(humanblnk.AccountRef(ada.ID), ngn("5000")), To: humanblnk.Single(humanblnk.World, ngn("5000")),
		Description: "Withdrawal to a closed account",
	}, time.Time{}) // no expiry: only the bank's answer settles it
	if err != nil {
		log.Fatal(err)
	}
	if err := hb.Holds.Cancel(ctx, h2); err != nil {
		log.Fatal(err)
	}
	fmt.Println("bank rejected, cancelled | Ada:", demo.Available(ctx, hb, ada.ID))
	fmt.Println("cancel again:", hb.Holds.Cancel(ctx, h2)) // safe to repeat
	_, err = hb.Holds.Capture(ctx, h2, money.Money{})
	fmt.Println("capture after cancel is settled:", errors.Is(err, humanblnk.ErrHoldSettled))
	// end-snippet

	// --- A partial capture: a ride, estimated then charged ---
	// snippet:partial
	h3, err := hb.Holds.Place(ctx, humanblnk.Transaction{
		Ref:  humanblnk.Reference("ride", "rd_"+demo.Run),
		From: humanblnk.Single(humanblnk.AccountRef(ada.ID), ngn("6000")), To: humanblnk.Single(humanblnk.SystemAccount("RidesClearing"), ngn("6000")),
		Description: "Ride estimate",
	}, time.Now().Add(2*time.Hour))
	if err != nil {
		log.Fatal(err)
	}
	fare, err := hb.Holds.Capture(ctx, h3, ngn("4200")) // the actual fare; the rest goes back
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ride charged", fare.Amount, "| Ada:", demo.Available(ctx, hb, ada.ID))
	// end-snippet
}
