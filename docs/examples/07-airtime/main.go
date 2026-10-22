// Chapter 7: a business service on top of the ledger. airtime/ is the
// service; this program plays a provider that delivers, declines, or
// doesn't answer.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/07-airtime/airtime"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
)

// provider answers by phone number, to show each outcome.
// snippet:provider
type provider struct{}

func (provider) Buy(_ context.Context, id, phone string, _ money.Money) error {
	switch phone {
	case "08000000000":
		return airtime.ErrDeclined
	case "08099999999":
		return errors.New("timeout talking to VTpass")
	}
	return nil
}

// end-snippet

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)
	demo.Deposit(ctx, hb, ada, "5000")
	// snippet:wire
	air := airtime.New(hb.Holds, provider{}, "vtpass")

	buy := func(id, phone string) (humanblnk.HoldID, error) {
		return air.Buy(ctx, airtime.Purchase{
			ID: id + "_" + demo.Run, Wallet: humanblnk.AccountRef(ada.ID), Phone: phone,
			Amount: money.MustParse("1000", demo.NGN),
		})
	}

	_, err := buy("air_1", "08031234567")
	fmt.Println("delivered:", err, "| Ada:", demo.Available(ctx, hb, ada.ID))

	_, err = buy("air_2", "08000000000")
	fmt.Println("declined:", errors.Is(err, airtime.ErrDeclined), "| Ada:", demo.Available(ctx, hb, ada.ID))

	pending, err := buy("air_3", "08099999999")
	fmt.Println("no answer:", errors.Is(err, airtime.ErrPending), "| Ada:", demo.Available(ctx, hb, ada.ID), "(1000 still held)")

	// Later the provider's webhook says it went through.
	if err := air.Settle(ctx, pending, true); err != nil {
		fmt.Println(err)
	}
	fmt.Println("settled after the webhook | Ada:", demo.Available(ctx, hb, ada.ID))
	// end-snippet

	// More than she has.
	_, err = air.Buy(ctx, airtime.Purchase{
		ID: "air_4_" + demo.Run, Wallet: humanblnk.AccountRef(ada.ID), Phone: "08031234567",
		Amount: money.MustParse("50000", demo.NGN),
	})
	fmt.Println("too much:", errors.Is(err, humanblnk.ErrInsufficientFunds))
}
