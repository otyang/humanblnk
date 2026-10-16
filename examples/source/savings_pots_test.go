package source_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// potBalance reads an account's balance for display.
func potBalance(w ledger.Wallet, id ledger.AccountID) string {
	a, err := w.GetAccount(ctx, id)
	check(err)
	return a.Balance.Display
}

func TestSavingsPots(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{}) // the clock starts at 1 Sep 2026, 09:00 UTC
	ada := customer(wallet, "user_ada", "Ada", currency.NGN)
	adaNGN := ada.Accounts[currency.NGN].ID
	fund(wallet, adaNGN, "100000", currency.NGN, "seed_ada")

	begin("savings-pots/open")
	// sample: savings-pots/open
	rent, err := wallet.OpenPot(ctx, ledger.PotInput{
		ParentID: adaNGN,
		Name:     "Rent", // unique per parent; opening it again returns this pot
		Goal:     ledger.Amount("600000", currency.NGN),
	})
	check(err)
	travel, err := wallet.OpenPot(ctx, ledger.PotInput{ParentID: adaNGN, Name: "Travel"}) // no goal
	check(err)
	fmt.Println(rent.ID, rent.Name, rent.Kind, rent.Currency, "goal:", rent.Goal.Display)
	fmt.Println(travel.Name, "has a goal:", !travel.Goal.IsZero())

	pots, err := wallet.ListPots(ctx, adaNGN)
	check(err)
	for _, p := range pots {
		fmt.Println("pot:", p.Name, "parent is Ada's account:", p.Parent == adaNGN)
	}
	// end
	end()

	begin("savings-pots/move")
	// sample: savings-pots/move
	// Into the pot: an ordinary transfer from Ada's main account.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: adaNGN, To: rent.ID,
		Amount: ledger.Amount("40000", currency.NGN), Fee: ledger.NoFee,
		Reference: "pot_in_0901", Description: "Save for rent",
	})
	check(err)

	// Out of the pot: the same call the other way.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{
		From: rent.ID, To: adaNGN,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee,
		Reference: "pot_out_0903",
	})
	check(err)

	p, err := wallet.GetAccount(ctx, rent.ID)
	check(err)
	fmt.Println("Rent pot:", p.Balance.Display, "of", rent.Goal.Display)
	fmt.Println("Ada's main account:", potBalance(wallet, adaNGN))
	// end
	end()

	begin("savings-pots/schedule")
	// sample: savings-pots/schedule
	lagos := time.FixedZone("WAT", 3600)
	plan, err := wallet.Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{
			From: adaNGN, To: rent.ID,
			Amount: ledger.Amount("25000", currency.NGN), Fee: ledger.NoFee,
			Reference:   "rent_plan_ada", // occurrence i gets "rent_plan_ada_i"
			Description: "Rent savings",
		},
		First: time.Date(2026, 10, 1, 7, 0, 0, 0, lagos),
		Every: ledger.Monthly,
		Count: 12, // 1 to 120
	})
	check(err)
	fmt.Println(len(plan), "occurrences")
	for _, r := range plan[:3] {
		fmt.Println(r.Reference, r.EntryStatus, r.Raw.ScheduledFor.In(lagos).Format("2 Jan 2006 15:04"), r.MetaData["occurrence"])
	}
	fmt.Println("Ada still has:", potBalance(wallet, adaNGN))
	// end
	end()

	begin("savings-pots/retry")
	// sample: savings-pots/retry
	// The app timed out and sends the same plan again.
	again, err := wallet.Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: adaNGN, To: rent.ID, Amount: ledger.Amount("25000", currency.NGN),
			Fee: ledger.NoFee, Reference: "rent_plan_ada", Description: "Rent savings"},
		First: time.Date(2026, 10, 1, 7, 0, 0, 0, lagos), Every: ledger.Monthly, Count: 12,
	})
	check(err)
	fmt.Println(len(again), "occurrences, replayed:", again[0].Replayed, "same id:", again[0].TransactionID == plan[0].TransactionID)
	// end
	end()

	begin("savings-pots/run")
	// sample: savings-pots/run
	// In tests, move the in-memory Blnk's clock. In production Blnk posts them itself.
	core.SetNow(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))

	for _, ref := range []string{"rent_plan_ada_1", "rent_plan_ada_2", "rent_plan_ada_3"} {
		r, err := wallet.GetTransactionByReference(ctx, ref)
		check(err)
		fmt.Println(ref, r.EntryStatus)
	}
	fmt.Println("Rent pot:", potBalance(wallet, rent.ID), "Ada:", potBalance(wallet, adaNGN))

	// The same plan sent again after it started still replays: nothing runs twice.
	again, err = wallet.Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: adaNGN, To: rent.ID, Amount: ledger.Amount("25000", currency.NGN),
			Fee: ledger.NoFee, Reference: "rent_plan_ada", Description: "Rent savings"},
		First: time.Date(2026, 10, 1, 7, 0, 0, 0, lagos), Every: ledger.Monthly, Count: 12,
	})
	check(err)
	fmt.Println("retry:", len(again), "occurrences, replayed:", again[0].Replayed, "Rent pot:", potBalance(wallet, rent.ID))
	// end
	end()

	begin("savings-pots/short")
	// sample: savings-pots/short
	// Ada spends most of her balance before 1 December.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: travel.ID,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "pot_in_1120"})
	check(err)
	fmt.Println("Ada before:", potBalance(wallet, adaNGN))

	core.SetNow(time.Date(2026, 12, 2, 9, 0, 0, 0, time.UTC))
	missed, err := wallet.GetTransactionByReference(ctx, "rent_plan_ada_3")
	check(err)
	fmt.Println(missed.Reference, missed.Status, missed.EntryStatus)

	next, err := wallet.GetTransactionByReference(ctx, "rent_plan_ada_4")
	check(err)
	fmt.Println(next.Reference, next.EntryStatus) // the plan carries on
	// end
	end()

	begin("savings-pots/intervals")
	// sample: savings-pots/intervals
	first := time.Date(2027, 1, 4, 8, 0, 0, 0, lagos)
	for _, every := range []ledger.Interval{ledger.Daily, ledger.Weekly, {Days: 14}, ledger.Monthly, {Months: 3}} {
		occ, err := wallet.Schedule(ctx, ledger.RecurringInput{
			Transfer: ledger.TransferInput{From: adaNGN, To: travel.ID, Amount: ledger.Amount("500", currency.NGN),
				Fee: ledger.NoFee, Reference: fmt.Sprintf("travel_%dm%dd", every.Months, every.Days)},
			First: first, Every: every, Count: 2,
		})
		check(err)
		fmt.Printf("%+v second run: %s\n", every, occ[1].Raw.ScheduledFor.In(lagos).Format("2 Jan 2006"))
	}
	// end
	end()

	begin("savings-pots/errors")
	// sample: savings-pots/errors
	base := ledger.TransferInput{From: adaNGN, To: rent.ID, Amount: ledger.Amount("1000", currency.NGN),
		Fee: ledger.NoFee, Reference: "plan_bad"}
	later := core.Now().Add(24 * time.Hour)

	_, err = wallet.Schedule(ctx, ledger.RecurringInput{Transfer: base, First: later, Every: ledger.Weekly, Count: 121})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Schedule(ctx, ledger.RecurringInput{Transfer: base, First: core.Now().Add(-time.Hour), Every: ledger.Weekly, Count: 4})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Schedule(ctx, ledger.RecurringInput{Transfer: base, First: later, Count: 4})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	pending := base
	pending.Pending = true
	_, err = wallet.Schedule(ctx, ledger.RecurringInput{Transfer: pending, First: later, Every: ledger.Weekly, Count: 4})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Cancel(ctx, next.TransactionID)
	fmt.Println("cancel a scheduled occurrence:", errors.Is(err, ledger.ErrNotPending))

	_, err = ops.CloseAccount(ctx, adaNGN, ledger.CloseInput{Reason: "customer request", RequestedBy: "ops.kemi", ApprovedBy: "ops_lead",
		SweepTo: ledger.AccountID(ledger.PayoutClearingAccount)})
	fmt.Println("close with a plan running:", errors.Is(err, ledger.ErrPendingActivity))
	// end
	end()
}
