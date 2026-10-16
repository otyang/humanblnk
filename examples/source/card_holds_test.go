package source_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/ledger"
)

func TestCardHolds(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{
		Limits: []ledger.Limit{{Tier: "tier2", Currency: "NGN", PerTransaction: "100000", Daily: "150000"}},
	})
	adaNGN := customer(wallet, "user_8812", "Ada", "NGN").Accounts["NGN"].ID
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{"NGN"}, ExternalID: "merchant_311"})
	check(err)
	chidiNGN := shop.Accounts["NGN"].ID
	fund(wallet, adaNGN, "100000", "NGN", "seed_ada")

	show := func(who string) {
		a, err := wallet.GetAccount(ctx, adaNGN)
		check(err)
		fmt.Println(who+":", a.Balance.Display, "available:", a.Available.Display, "pending out:", a.PendingOut.Display)
	}
	payable := func() {
		h, err := ops.GetHouseAccount(ctx, ledger.HouseRef{Indicator: ledger.MerchantPayableAccount, Currency: "NGN"})
		check(err)
		fmt.Println("@MerchantPayable:", h.Balance.Display, "pending in:", h.PendingIn.Display)
	}
	now := core.Now() // 2026-09-01 09:00 UTC in these examples

	begin("card-holds/authorise")
	// sample: card-holds/authorise
	// Ada's virtual card is used at a restaurant: the network asks to authorise ₦18,500.
	auth, err := wallet.Hold(ctx, ledger.HoldInput{
		AccountID:   adaNGN,
		Destination: ledger.MerchantPayableAccount, // owed to the card network once captured
		Amount:      ledger.Amount("18500", "NGN"),
		Reference:   "card_auth_449201", // the network's authorisation id
		ExpiresAt:   now.Add(7 * 24 * time.Hour),
		Description: "Mama Nkechi's Kitchen",
	})
	check(err)
	fmt.Println(auth.EntryStatus, auth.Amount.Display, auth.MetaData["txn_type"])
	show("Ada")
	payable()
	// end
	end()

	begin("card-holds/capture")
	// sample: card-holds/capture
	// The restaurant settles the full bill.
	c, err := wallet.Confirm(ctx, auth.TransactionID, nil)
	check(err)
	fmt.Println(c.EntryStatus, c.Amount.Display)
	show("Ada")
	payable()
	// end
	end()

	begin("card-holds/partial")
	// sample: card-holds/partial
	// A fuel station authorises ₦30,000, then pumps ₦12,000 and ₦6,250.50 on two captures.
	fuel, err := wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("30000", "NGN"), Reference: "card_auth_449305",
	})
	check(err)
	first := ledger.Amount("12000", "") // currency taken from the hold
	_, err = wallet.Confirm(ctx, fuel.TransactionID, &first)
	check(err)
	second := ledger.Amount("6250.50", "")
	_, err = wallet.Confirm(ctx, fuel.TransactionID, &second)
	check(err)
	show("after two captures")

	rest, err := wallet.Cancel(ctx, fuel.TransactionID) // release what is still held
	check(err)
	fmt.Println("released:", rest.Amount.Display, rest.EntryStatus)
	show("after release")
	// end
	end()

	begin("card-holds/merchant")
	// sample: card-holds/merchant
	// Chidi Stores takes a ₦10,000 deposit hold for a delivery, paid to its own account on capture.
	h, err := wallet.Hold(ctx, ledger.HoldInput{
		AccountID:   adaNGN,
		Destination: chidiNGN, // a customer account works too
		Amount:      ledger.Amount("10000", "NGN"),
		Reference:   "hold_order_10450",
	})
	check(err)
	shopAcct, err := wallet.GetAccount(ctx, chidiNGN)
	check(err)
	fmt.Println("Chidi Stores pending in:", shopAcct.PendingIn.Display, "balance:", shopAcct.Balance.Display)

	_, err = wallet.Confirm(ctx, h.TransactionID, nil)
	check(err)
	shopAcct, err = wallet.GetAccount(ctx, chidiNGN)
	check(err)
	fmt.Println("Chidi Stores pending in:", shopAcct.PendingIn.Display, "balance:", shopAcct.Balance.Display)
	// end
	end()

	begin("card-holds/timers")
	// sample: card-holds/timers
	// A hotel holds ₦5,000; if it never captures, the hold lapses after 3 days.
	_, err = wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("5000", "NGN"), Reference: "card_auth_449410",
		ExpiresAt: now.Add(72 * time.Hour),
	})
	check(err)
	// A subscription authorised now and captured automatically tomorrow.
	_, err = wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("2500", "NGN"), Reference: "card_auth_449411",
		CaptureAt: now.Add(24 * time.Hour),
		ExpiresAt: now.Add(7 * 24 * time.Hour), // CaptureAt must come before ExpiresAt
	})
	check(err)
	show("now")

	core.Advance(25 * time.Hour)
	show("after 25h")
	core.Advance(48 * time.Hour)
	show("after 73h")
	events := core.Events()
	fmt.Println("last webhook:", events[len(events)-1].Event)
	// end
	end()

	begin("card-holds/limits")
	// sample: card-holds/limits
	// Holds count against limits as soon as they are placed (here ₦150,000 a day).
	// The clock is now three days on, so today starts at zero.
	u, err := wallet.LimitUsage(ctx, adaNGN)
	check(err)
	fmt.Println("used today:", u.DailyUsed.Display, "left:", u.DailyLeft.Display)

	atm, err := wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("40000", "NGN"), Reference: "card_auth_449500",
	})
	check(err)
	u, err = wallet.LimitUsage(ctx, adaNGN)
	check(err)
	fmt.Println("with the hold:", u.DailyUsed.Display, "left:", u.DailyLeft.Display)

	_, err = wallet.Cancel(ctx, atm.TransactionID) // a released hold gives the room back
	check(err)
	u, err = wallet.LimitUsage(ctx, adaNGN)
	check(err)
	fmt.Println("after release:", u.DailyUsed.Display, "left:", u.DailyLeft.Display)
	// end
	end()

	begin("card-holds/errors")
	// sample: card-holds/errors
	_, err = wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("90000", "NGN"), Reference: "e1",
	})
	fmt.Println("insufficient funds:", errors.Is(err, ledger.ErrInsufficientFunds))

	_, err = wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("1000", "NGN"), Reference: "e2",
		CaptureAt: now.Add(10 * 24 * time.Hour), ExpiresAt: now.Add(7 * 24 * time.Hour),
	})
	fmt.Println("CaptureAt after ExpiresAt:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN, Amount: ledger.Amount("1000", "NGN"), Reference: "e3"})
	fmt.Println("no destination:", errors.Is(err, ledger.ErrInvalidInput))

	small, err := wallet.Hold(ctx, ledger.HoldInput{
		AccountID: adaNGN, Destination: ledger.MerchantPayableAccount,
		Amount: ledger.Amount("1000", "NGN"), Reference: "e4",
	})
	check(err)
	tooMuch := ledger.Amount("1500", "")
	_, err = wallet.Confirm(ctx, small.TransactionID, &tooMuch)
	fmt.Println("capture more than held:", errors.Is(err, ledger.ErrAmountExceeded))

	_, err = wallet.Cancel(ctx, small.TransactionID)
	check(err)
	_, err = wallet.Confirm(ctx, small.TransactionID, nil)
	fmt.Println("capture after release:", errors.Is(err, ledger.ErrAlreadyFinalized))

	_, err = wallet.Confirm(ctx, c.TransactionID, nil) // the capture record, not a hold
	fmt.Println("not a hold:", errors.Is(err, ledger.ErrNotPending))

	// A merchant under review: no new hold pays it, and an open one can't be captured into it.
	held, err := wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN, Destination: chidiNGN,
		Amount: ledger.Amount("2000", "NGN"), Reference: "e5"})
	check(err)
	check(ops.FreezeAccount(ctx, chidiNGN, "merchant review"))
	_, err = wallet.Hold(ctx, ledger.HoldInput{AccountID: adaNGN, Destination: chidiNGN,
		Amount: ledger.Amount("1000", "NGN"), Reference: "e6"})
	fmt.Println("hold paying a frozen merchant:", errors.Is(err, ledger.ErrAccountFrozen))
	_, err = wallet.Confirm(ctx, held.TransactionID, nil)
	fmt.Println("capture into a frozen merchant:", errors.Is(err, ledger.ErrAccountFrozen))
	_, err = wallet.Cancel(ctx, held.TransactionID) // releasing it gives Ada her money back
	fmt.Println("release:", err == nil)
	// end
	end()
}
