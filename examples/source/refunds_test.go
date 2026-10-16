package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

// refundBalance reads an account's balance for display.
func refundBalance(w ledger.Wallet, id ledger.AccountID) string {
	a, err := w.GetAccount(ctx, id)
	check(err)
	return a.Balance.Display
}

func TestRefunds(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_ada", "Ada", "NGN")
	tunde := customer(wallet, "user_tunde", "Tunde", "NGN")
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{"NGN"}, ExternalID: "merchant_311"})
	check(err)
	adaNGN, tundeNGN, chidi := ada.Accounts["NGN"].ID, tunde.Accounts["NGN"].ID, shop.Accounts["NGN"].ID
	fund(wallet, adaNGN, "100000", "NGN", "seed_ada")

	pay, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("15000", "NGN"), Fee: ledger.NoFee, Reference: "pay_10460"})
	check(err)

	begin("refunds/full")
	// sample: refunds/full
	// Chidi Stores cancels the whole ₦15,000 order.
	back, err := wallet.Refund(ctx, pay.TransactionID, ledger.RefundInput{
		Reason:      "order cancelled",
		Description: "Refund of order 10460",
	})
	check(err)
	fmt.Println(back.Status, back.Amount.Display, "reference:", back.Reference == pay.TransactionID+"_refund")
	fmt.Println("reason:", back.MetaData["refund_reason"])
	fmt.Println("Ada:", refundBalance(wallet, adaNGN), "Chidi Stores:", refundBalance(wallet, chidi))

	_, err = wallet.Refund(ctx, pay.TransactionID, ledger.RefundInput{})
	fmt.Println("second refund:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()

	pay2, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("6000", "NGN"), Fee: ledger.NoFee, Reference: "pay_10470"})
	check(err)

	begin("refunds/partial")
	// sample: refunds/partial
	// Two of three items in a ₦6,000 order were out of stock, refunded one at a time.
	r1, err := wallet.RefundPartial(ctx, pay2.TransactionID, ledger.PartialRefundInput{
		Amount:    ledger.Amount("2000", "NGN"),
		Reference: "refund_10470_1", // one per refund; a retry replays it
		Reason:    "item out of stock",
	})
	check(err)
	r2, err := wallet.RefundPartial(ctx, pay2.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("2500", "NGN"), Reference: "refund_10470_2", Reason: "item out of stock",
	})
	check(err)
	fmt.Println(r1.Amount.Display, r2.Amount.Display, "linked:", r2.MetaData["refund_of"] == pay2.TransactionID)

	orig, err := wallet.GetTransaction(ctx, pay2.TransactionID)
	check(err)
	fmt.Println("refunded so far (kobo):", orig.MetaData["refunded_precise"])

	_, err = wallet.RefundPartial(ctx, pay2.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("2000", "NGN"), Reference: "refund_10470_3",
	})
	fmt.Println(errors.Is(err, ledger.ErrAmountExceeded), err)

	_, err = wallet.Refund(ctx, pay2.TransactionID, ledger.RefundInput{})
	fmt.Println("full refund after partials:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()

	begin("refunds/lookup")
	// sample: refunds/lookup
	// Your support tool has only the refund's reference.
	ref, err := wallet.GetTransactionByReference(ctx, "refund_10470_1")
	check(err)
	fmt.Println(ref.Amount.Display, ref.MetaData["txn_type"], ref.MetaData["refund_reason"])

	// refund_of leads back to the original payment.
	o, err := wallet.GetTransaction(ctx, ref.MetaData["refund_of"].(string))
	check(err)
	fmt.Println(o.Reference, o.Amount.Display, "refunded (kobo):", o.MetaData["refunded_precise"])

	// A full refund's reference is the original id plus "_refund".
	full, err := wallet.GetTransactionByReference(ctx, pay.TransactionID+"_refund")
	check(err)
	fmt.Println(full.Amount.Display, full.Status)
	// end
	end()

	begin("refunds/with-fee")
	// sample: refunds/with-fee
	// Ada paid Chidi Stores ₦10,000 plus a ₦50 Oja fee. The order is cancelled.
	feePay, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("10000", "NGN"), Fee: ledger.Amount("50", "NGN"), Reference: "pay_10480"})
	check(err)
	fmt.Println("paid in legs:", len(feePay.Legs), "Ada:", refundBalance(wallet, adaNGN))

	// Refund by the payment id: every leg is reversed, the fee included.
	undo, err := wallet.Refund(ctx, feePay.TransactionID, ledger.RefundInput{Reason: "order cancelled"})
	check(err)
	feeIncome, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: "NGN"})
	check(err)
	names := map[string]string{string(chidi): "Chidi Stores", feeIncome.ID: "@FeeIncome"}
	for _, leg := range undo.Legs {
		fmt.Println("  reversed from", names[leg.Account], leg.Amount.Display)
	}
	fmt.Println("Ada:", refundBalance(wallet, adaNGN), "Chidi Stores:", refundBalance(wallet, chidi))

	_, err = wallet.Refund(ctx, feePay.TransactionID, ledger.RefundInput{})
	fmt.Println("second refund:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()

	begin("refunds/keep-fee")
	// sample: refunds/keep-fee
	// Another ₦10,000 + ₦50 order. Chidi Stores gives the ₦10,000 back; Oja keeps its fee.
	feePay2, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("10000", "NGN"), Fee: ledger.Amount("50", "NGN"), Reference: "pay_10481"})
	check(err)
	rf, err := wallet.RefundPartial(ctx, feePay2.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("10000", "NGN"), Reference: "refund_10481",
	})
	check(err)
	fmt.Println(rf.Amount.Display, "from Chidi Stores:", rf.Source == string(chidi))
	fmt.Println("refund_of is the leg:", rf.MetaData["refund_of"] != feePay2.TransactionID)
	// end
	end()

	begin("refunds/split-full")
	// sample: refunds/split-full
	// A ₦10,000 card order: Oja's 5%, ₦1,500 to Tunde for delivery, the rest to Chidi Stores.
	cancelled, err := wallet.Split(ctx, ledger.SplitInput{
		From: ledger.AccountID(ledger.ClearingAccount("paystack")), AllowOverdraft: true,
		Amount: ledger.Amount("10000", "NGN"), Reference: "order_10491",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "5"},
			{To: tundeNGN, Amount: ledger.Amount("1500", "NGN")},
			{To: chidi, Rest: true},
		},
	})
	check(err)
	fmt.Println("Tunde:", refundBalance(wallet, tundeNGN), "Chidi Stores:", refundBalance(wallet, chidi))

	// The whole order is cancelled: one call reverses all three legs.
	whole, err := wallet.Refund(ctx, cancelled.TransactionID, ledger.RefundInput{Reason: "order cancelled"})
	check(err)
	fmt.Println("legs reversed:", len(whole.Legs))
	fmt.Println("Tunde:", refundBalance(wallet, tundeNGN), "Chidi Stores:", refundBalance(wallet, chidi))
	// end
	end()

	begin("refunds/split-leg")
	// sample: refunds/split-leg
	// Another card order, split the same way. Chidi Stores refunds ₦3,800 of its share.
	order, err := wallet.Split(ctx, ledger.SplitInput{
		From: ledger.AccountID(ledger.ClearingAccount("paystack")), AllowOverdraft: true,
		Amount: ledger.Amount("20000", "NGN"), Reference: "order_10490",
		Parts: []ledger.SplitPart{
			{To: ledger.FeeIncomeAccount, Percent: "5"},
			{To: tundeNGN, Amount: ledger.Amount("1500", "NGN")},
			{To: chidi, Rest: true},
		},
	})
	check(err)

	_, err = wallet.RefundPartial(ctx, order.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("1000", "NGN"), Reference: "refund_10490_x",
	})
	fmt.Println("whole split:", errors.Is(err, ledger.ErrInvalidInput))

	var chidiLeg string
	for _, leg := range order.Legs {
		if leg.Account == string(chidi) {
			chidiLeg = leg.TransactionID
		}
	}
	rs, err := wallet.RefundPartial(ctx, chidiLeg, ledger.PartialRefundInput{
		Amount: ledger.Amount("3800", "NGN"), Reference: "refund_10490_1", Reason: "returned item",
	})
	check(err)
	clearing, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@PaystackClearing", Currency: "NGN"})
	check(err)
	fmt.Println(rs.Amount.Display, "back to @PaystackClearing:", rs.Destination == clearing.ID)
	fmt.Println("Chidi Stores balance:", refundBalance(wallet, chidi))
	// end
	end()

	begin("refunds/spent")
	// sample: refunds/spent
	// Tunde got ₦2,000 from Ada, then cashed out everything he had (₦3,500).
	p2p, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("2000", "NGN"), Fee: ledger.NoFee, Reference: "p2p_502"})
	check(err)
	_, err = wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: tundeNGN, Amount: ledger.Amount("3500", "NGN"),
		Fee: ledger.NoFee, Reference: "cashout_77", Provider: "flutterwave", Immediate: true})
	check(err)

	_, err = wallet.Refund(ctx, p2p.TransactionID, ledger.RefundInput{Reason: "sent by mistake"})
	fmt.Println(errors.Is(err, ledger.ErrInsufficientFunds), err)
	_, err = wallet.RefundPartial(ctx, p2p.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("2000", "NGN"), Reference: "refund_502_1"})
	fmt.Println("partial too:", errors.Is(err, ledger.ErrInsufficientFunds))
	fmt.Println("nothing moved, Tunde:", refundBalance(wallet, tundeNGN))

	// Tunde tops up again. Now the refund posts.
	fund(wallet, tundeNGN, "2000", "NGN", "paystack_T502")
	_, err = wallet.Refund(ctx, p2p.TransactionID, ledger.RefundInput{Reason: "sent by mistake"})
	check(err)
	fmt.Println("refunded, Tunde:", refundBalance(wallet, tundeNGN))
	// end
	end()

	begin("refunds/retry")
	// sample: refunds/retry
	// A refund of a ₦5,000 + ₦50 payment stopped after its first leg, here the fee.
	pay4, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("5000", "NGN"), Fee: ledger.Amount("50", "NGN"), Reference: "pay_10495"})
	check(err)
	for _, leg := range pay4.Legs {
		if leg.Account == ledger.FeeIncomeAccount {
			_, err = wallet.Refund(ctx, leg.TransactionID, ledger.RefundInput{Reason: "order cancelled"})
			check(err)
		}
	}

	// Calling Refund again with the payment id reverses only what is left.
	rest, err := wallet.Refund(ctx, pay4.TransactionID, ledger.RefundInput{Reason: "order cancelled"})
	check(err)
	fmt.Println("legs reversed now:", len(rest.Legs), rest.Legs[0].Amount.Display, "from Chidi Stores:", rest.Legs[0].Account == string(chidi))

	_, err = wallet.Refund(ctx, pay4.TransactionID, ledger.RefundInput{})
	fmt.Println("nothing left:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()

	begin("refunds/frozen")
	// sample: refunds/frozen
	pay3, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("3000", "NGN"), Fee: ledger.NoFee, Reference: "p2p_501"})
	check(err)
	check(ops.FreezeAccount(ctx, adaNGN, "fraud check"))

	_, err = wallet.Refund(ctx, pay3.TransactionID, ledger.RefundInput{})
	fmt.Println("payer frozen:", errors.Is(err, ledger.ErrAccountFrozen))
	_, err = wallet.RefundPartial(ctx, pay3.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("1000", "NGN"), Reference: "refund_501_1"})
	fmt.Println("partial too:", errors.Is(err, ledger.ErrAccountFrozen))

	check(ops.UnfreezeAccount(ctx, adaNGN))
	ok, err := wallet.Refund(ctx, pay3.TransactionID, ledger.RefundInput{})
	check(err)
	fmt.Println("after unfreeze:", ok.Status)
	// end
	end()

	begin("refunds/errors")
	// sample: refunds/errors
	held, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("1000", "NGN"), Fee: ledger.NoFee, Reference: "pay_10500", Pending: true})
	check(err)
	_, err = wallet.RefundPartial(ctx, held.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("500", "NGN"), Reference: "refund_10500_1"})
	fmt.Println("pending:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.RefundPartial(ctx, "txn_unknown", ledger.PartialRefundInput{
		Amount: ledger.Amount("500", "NGN"), Reference: "refund_x"})
	fmt.Println("unknown:", errors.Is(err, ledger.ErrNotFound))

	_, err = wallet.RefundPartial(ctx, pay.TransactionID, ledger.PartialRefundInput{
		Amount: ledger.Amount("500", "NGN"), Reference: "refund_10460_1"})
	fmt.Println("already fully refunded:", errors.Is(err, ledger.ErrAlreadyFinalized))

	_, err = wallet.Refund(ctx, "txn_unknown", ledger.RefundInput{})
	fmt.Println("full refund, unknown:", errors.Is(err, ledger.ErrNotFound))

	_, err = wallet.Refund(ctx, "", ledger.RefundInput{})
	fmt.Println("no id:", errors.Is(err, ledger.ErrInvalidInput))
	// end
	end()
}
