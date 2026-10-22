// Chapter 8: refunds, corrections and bounced payouts, all with Reverse.
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
	"github.com/otyang/humanblnk/money"
)

func main() {
	ctx, hb := demo.Setup()
	ada := demo.Customer(ctx, hb, "ada", demo.NGN)
	shop := demo.Customer(ctx, hb, "mama_put_store", demo.NGN) // a merchant is a customer too
	demo.Deposit(ctx, hb, ada, "30000")
	ngn := func(v string) money.Money { return money.MustParse(v, demo.NGN) }

	// Ada buys three items for 15,000.
	// snippet:order
	order, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:  humanblnk.Reference("order", "ord_1_"+demo.Run),
		From: humanblnk.Single(humanblnk.AccountRef(ada.ID), ngn("15000")), To: humanblnk.Single(humanblnk.AccountRef(shop.ID), ngn("15000")),
		Description: "Order #1",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("paid | Ada:", demo.Available(ctx, hb, ada.ID), "| shop:", demo.Available(ctx, hb, shop.ID))
	// end-snippet

	// One item comes back: a partial refund. Ref is your id for this refund.
	// snippet:partial
	oneItem := humanblnk.Reversal{Ref: "refund:ord_1:item_2_" + demo.Run, Entry: order.ID, Amount: ngn("5000"), Reason: "item returned"}
	r1, err := hb.Transactions.Reverse(ctx, oneItem)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("refunded", r1.Amount, "| Ada:", demo.Available(ctx, hb, ada.ID))
	// end-snippet

	// The support agent clicks twice: same Ref, same refund, no money moves.
	// snippet:twice
	r1again, err := hb.Transactions.Reverse(ctx, oneItem)
	fmt.Println("clicked again, same refund:", r1again.ID == r1.ID, err)
	// end-snippet

	// Refunding more than is left is refused.
	// snippet:toomuch
	_, err = hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: "refund:ord_1:too_much_" + demo.Run, Entry: order.ID, Amount: ngn("20000"), Reason: "mistake"})
	fmt.Println("more than is left:", errors.Is(err, humanblnk.ErrAlreadyReversed))
	// end-snippet

	// The order is cancelled: a full reversal undoes what's left (10,000).
	// snippet:rest
	rest, err := hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: "refund:ord_1:cancel_" + demo.Run, Entry: order.ID, Reason: "order cancelled"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("cancelled, refunded", rest.Amount, "| Ada:", demo.Available(ctx, hb, ada.ID), "| shop:", demo.Available(ctx, hb, shop.ID))
	_, err = hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: "refund:ord_1:again_" + demo.Run, Entry: order.ID, Reason: "again"})
	fmt.Println("nothing left to refund:", errors.Is(err, humanblnk.ErrAlreadyReversed))
	// end-snippet

	// A payout that bounced: the bank returns the money days later.
	// snippet:bounce
	payout, err := hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:  humanblnk.Reference("payout", "po_"+demo.Run),
		From: humanblnk.Single(humanblnk.AccountRef(ada.ID), ngn("8000")), To: humanblnk.Single(humanblnk.World, ngn("8000")),
		Description: "Withdrawal",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("paid out | Ada:", demo.Available(ctx, hb, ada.ID))
	if _, err := hb.Transactions.Reverse(ctx, humanblnk.Reversal{Ref: "bounce:po_" + demo.Run, Entry: payout.ID, Reason: "account closed at the bank"}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("bounced back | Ada:", demo.Available(ctx, hb, ada.ID))
	// end-snippet

	// A refund takes money back from where it went. If the shop had
	// already spent it, the refund would be ErrInsufficientFunds: a customer
	// is never pushed below zero.
}
