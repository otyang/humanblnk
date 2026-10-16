package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// disputeShow prints an account's balance and what can be spent now.
func disputeShow(w ledger.Wallet, name string, id ledger.AccountID) {
	a, err := w.GetAccount(ctx, id)
	check(err)
	fmt.Println(name+":", "balance", a.Balance.Display, "available", a.Available.Display)
}

func TestDisputes(t *testing.T) {
	_, _, wallet, _ := setup(t, ledger.Config{})
	ada := customer(wallet, "user_ada", "Ada", currency.NGN)
	tunde := customer(wallet, "user_tunde", "Tunde", currency.NGN)
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{currency.NGN}, ExternalID: "merchant_311"})
	check(err)
	adaNGN, tundeNGN, chidi := ada.Accounts[currency.NGN].ID, tunde.Accounts[currency.NGN].ID, shop.Accounts[currency.NGN].ID
	fund(wallet, adaNGN, "100000", currency.NGN, "seed_ada")

	begin("disputes/open")
	// sample: disputes/open
	pay, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("9800", currency.NGN), Fee: ledger.NoFee, Reference: "pay_10461"})
	check(err)

	// Ada says the item never arrived. Hold that money at Chidi Stores.
	d, err := wallet.OpenDispute(ctx, ledger.DisputeInput{
		TransactionID: pay.TransactionID,
		Reference:     "dispute_2231", // required
		Reason:        "item not received",
		MetaData:      map[string]any{"ticket": "CS-2231"},
	})
	check(err)
	fmt.Println(d.TransactionID != "", d.Status, d.Amount.Display, d.MetaData["txn_type"])
	fmt.Println("dispute_of:", d.MetaData["dispute_of"] == pay.TransactionID)
	disputeShow(wallet, "Chidi Stores", chidi)
	adaAcct, err := wallet.GetAccount(ctx, adaNGN)
	check(err)
	fmt.Println("Ada: balance", adaAcct.Balance.Display, "pending in", adaAcct.PendingIn.Display)
	// end
	end()

	begin("disputes/upheld")
	// sample: disputes/upheld
	// The investigation finds for Ada: the held money goes back to her.
	won, err := wallet.ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, nil)
	check(err)
	fmt.Println(won.Status, won.Amount.Display)
	disputeShow(wallet, "Chidi Stores", chidi)
	disputeShow(wallet, "Ada", adaNGN)
	// end
	end()

	begin("disputes/rejected")
	// sample: disputes/rejected
	pay2, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "pay_10462"})
	check(err)
	d2, err := wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: pay2.TransactionID,
		Reference: "dispute_2232", Reason: "wrong colour"})
	check(err)

	// The courier's photo shows the right item delivered: Chidi Stores wins.
	lost, err := wallet.ResolveDispute(ctx, d2.TransactionID, ledger.DisputeRejected, nil)
	check(err)
	fmt.Println(lost.Status, lost.Amount.Display)
	disputeShow(wallet, "Chidi Stores", chidi)
	// end
	end()

	begin("disputes/partial")
	// sample: disputes/partial
	pay3, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("12000", currency.NGN), Fee: ledger.NoFee, Reference: "pay_10463"})
	check(err)

	// Ada disputes one ₦4,000 item of a ₦12,000 order.
	d3, err := wallet.OpenDispute(ctx, ledger.DisputeInput{
		TransactionID: pay3.TransactionID,
		Amount:        ledger.Amount("4000", currency.NGN), // zero = the whole payment
		Reference:     "dispute_2233",
		Reason:        "one item damaged",
	})
	check(err)
	disputeShow(wallet, "Chidi Stores", chidi)

	// Oja finds half of it justified: ₦2,000 back to Ada, the rest stays held.
	half := ledger.Amount("2000", currency.NGN)
	r, err := wallet.ResolveDispute(ctx, d3.TransactionID, ledger.DisputeUpheld, &half)
	check(err)
	fmt.Println("to Ada:", r.Amount.Display)
	disputeShow(wallet, "Chidi Stores", chidi)

	// Close it: whatever is still held goes back to Chidi Stores.
	r, err = wallet.ResolveDispute(ctx, d3.TransactionID, ledger.DisputeRejected, nil)
	check(err)
	fmt.Println(r.Status, "released:", r.Amount.Display)
	disputeShow(wallet, "Chidi Stores", chidi)
	// end
	end()

	begin("disputes/legs")
	// sample: disputes/legs
	// A payment with a fee: the dispute holds the ₦7,000 Chidi Stores got, not the ₦50 fee.
	feePay, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("7000", currency.NGN), Fee: ledger.Amount("50", currency.NGN), Reference: "pay_10464"})
	check(err)
	d4, err := wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: feePay.TransactionID, Reference: "dispute_2234"})
	check(err)
	fmt.Println("held:", d4.Amount.Display)

	// A split paying two recipients: name the leg.
	order, err := wallet.Split(ctx, ledger.SplitInput{
		From: ledger.AccountID(ledger.ClearingAccount("paystack")), AllowOverdraft: true,
		Amount: ledger.Amount("20000", currency.NGN), Reference: "order_10465",
		Parts: []ledger.SplitPart{
			{To: tundeNGN, Amount: ledger.Amount("1500", currency.NGN), Narration: "Delivery"},
			{To: chidi, Rest: true},
		},
	})
	check(err)
	_, err = wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: order.TransactionID, Reference: "dispute_2235"})
	fmt.Println("whole split:", errors.Is(err, ledger.ErrInvalidInput))

	for _, leg := range order.Legs {
		if leg.Account == string(chidi) {
			d5, err := wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: leg.TransactionID, Reference: "dispute_2235"})
			check(err)
			fmt.Println("Chidi's leg held:", d5.Amount.Display, "pointed at the card side:", d5.Destination != string(adaNGN))
		}
	}
	// end
	end()

	begin("disputes/errors")
	// sample: disputes/errors
	pay6, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tundeNGN,
		Amount: ledger.Amount("3000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_601"})
	check(err)

	_, err = wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: pay6.TransactionID,
		Amount: ledger.Amount("3500", currency.NGN), Reference: "dispute_2240"})
	fmt.Println("more than paid:", errors.Is(err, ledger.ErrAmountExceeded))

	// Tunde has already spent part of the money.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: tundeNGN, To: chidi,
		Amount: ledger.Amount("2000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_602"})
	check(err)
	_, err = wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: pay6.TransactionID, Reference: "dispute_2241"})
	fmt.Println("recipient short:", errors.Is(err, ledger.ErrInsufficientFunds))

	held, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidi,
		Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "pay_10466", Pending: true})
	check(err)
	_, err = wallet.OpenDispute(ctx, ledger.DisputeInput{TransactionID: held.TransactionID, Reference: "dispute_2242"})
	fmt.Println("still pending:", errors.Is(err, ledger.ErrInvalidInput))

	part := ledger.Amount("100", currency.NGN)
	_, err = wallet.ResolveDispute(ctx, d4.TransactionID, ledger.DisputeRejected, &part)
	fmt.Println("partial reject:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.ResolveDispute(ctx, d.TransactionID, ledger.DisputeUpheld, nil)
	fmt.Println("already resolved:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()
}
