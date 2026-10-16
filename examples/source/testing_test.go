package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

func TestTesting(t *testing.T) {
	curT = t

	begin("testing/basic")
	// sample: testing/basic
	// In a _test.go file: no Docker, no network, a fresh Core per test.
	svc, core := ledgertest.New(t, ledger.Config{}) // closed when the test ends
	wallet := svc.Wallet()

	ada, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	check(err)
	landlord, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	check(err)
	_, err = wallet.Deposit(ctx, ledger.DepositInput{AccountID: ada.ID, Amount: ledger.Amount("300000", currency.NGN),
		Fee: ledger.NoFee, Reference: "seed", Provider: "paystack"})
	check(err)

	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: ada.ID, To: landlord.ID,
		Amount: ledger.Amount("250000", currency.NGN), Fee: ledger.NoFee, Reference: "rent_sept"})
	check(err)

	got, err := wallet.GetAccount(ctx, landlord.ID)
	check(err)
	if got.Balance.Value != "250000" {
		t.Fatalf("landlord has %s, want 250000", got.Balance.Value)
	}
	fmt.Println("landlord:", got.Balance.Display, "ledger:", got.LedgerID, "events so far:", len(core.Events()))
	// end
	end()
}

func TestTestingClock(t *testing.T) {
	curT = t

	begin("testing/clock")
	// sample: testing/clock
	svc, core := ledgertest.New(t, ledger.Config{})
	fmt.Println("starts at:", core.Now()) // always the same

	core.SetNow(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	acct, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	check(err)
	r, err := svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: acct.ID, Amount: ledger.Amount("1000", currency.NGN),
		Fee: ledger.NoFee, Reference: "seed", Provider: "paystack"})
	check(err)
	fmt.Println("posted at:", r.CreatedAt)

	core.Advance(36 * time.Hour)
	fmt.Println("now:", core.Now())
	// end
	end()
}

func TestTestingExpiry(t *testing.T) {
	svc, core, wallet, _ := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN).Accounts[currency.NGN].ID
	shop := customer(wallet, "merchant_311", "Chidi", currency.NGN).Accounts[currency.NGN].ID
	fund(wallet, ada, "50000", currency.NGN, "paystack_T9001")

	begin("testing/hold-expiry")
	// sample: testing/hold-expiry
	hold, err := wallet.Hold(ctx, ledger.HoldInput{
		AccountID: ada, Destination: shop, Amount: ledger.Amount("20000", currency.NGN),
		Reference: "card_auth_4410", ExpiresAt: core.Now().Add(2 * time.Hour),
	})
	check(err)
	a, err := wallet.GetAccount(ctx, ada)
	check(err)
	fmt.Println("held:", a.Balances.PendingOut.Display, "available:", a.Available.Display)

	core.Advance(3 * time.Hour) // Chidi Stores never captured it

	a, err = wallet.GetAccount(ctx, ada)
	check(err)
	fmt.Println("after 3h available:", a.Available.Display)

	evs := core.Events()
	body, err := json.Marshal(evs[len(evs)-1]) // the newest webhook
	check(err)
	void, err := svc.DecodeEvent(body)
	check(err)
	fmt.Println(void.Name, "for the hold:", void.Receipt.ParentTransaction == hold.TransactionID)

	_, err = wallet.Confirm(ctx, hold.TransactionID, nil) // too late to capture
	fmt.Println("capture now:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()

	begin("testing/withdrawal-ttl")
	// sample: testing/withdrawal-ttl
	wd, err := wallet.Withdraw(ctx, ledger.WithdrawInput{
		AccountID: ada, Amount: ledger.Amount("10000", currency.NGN), Fee: ledger.NoFee,
		Reference: "wd_9001", Provider: "flutterwave", // pending until Flutterwave answers
	})
	check(err)
	a, err = wallet.GetAccount(ctx, ada)
	check(err)
	fmt.Println("withdrawal:", wd.EntryStatus, "available:", a.Available.Display)

	core.Advance(23 * time.Hour)
	a, err = wallet.GetAccount(ctx, ada)
	check(err)
	fmt.Println("after 23h available:", a.Available.Display)

	core.Advance(2 * time.Hour) // past Config.WithdrawalHoldTTL (24h)
	a, err = wallet.GetAccount(ctx, ada)
	check(err)
	fmt.Println("after 25h available:", a.Available.Display)

	_, err = wallet.Confirm(ctx, wd.TransactionID, nil) // Flutterwave's late success webhook
	fmt.Println("confirm now:", errors.Is(err, ledger.ErrAlreadyFinalized))
	// end
	end()
}

func TestTestingScheduled(t *testing.T) {
	_, core, wallet, _ := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN)
	adaNGN := ada.Accounts[currency.NGN].ID
	pot, err := wallet.OpenPot(ctx, ledger.PotInput{ParentID: adaNGN, Name: "Rent"})
	check(err)
	fund(wallet, adaNGN, "25000", currency.NGN, "paystack_T9101")

	begin("testing/scheduled")
	// sample: testing/scheduled
	plan, err := wallet.Schedule(ctx, ledger.RecurringInput{
		Transfer: ledger.TransferInput{From: adaNGN, To: pot.ID, Amount: ledger.Amount("10000", currency.NGN),
			Fee: ledger.NoFee, Reference: "rent_plan_ada"},
		First: core.Now().Add(24 * time.Hour), Every: ledger.Monthly, Count: 3,
	})
	check(err)
	fmt.Println("scheduled:", len(plan), plan[0].EntryStatus)

	for _, day := range []time.Time{
		time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC),  // first payment due 2 Sep, 09:00
		time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC), // then the 2nd of each month
		time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
	} {
		core.SetNow(day)
		p, err := wallet.GetAccount(ctx, pot.ID)
		check(err)
		fmt.Println(day.Format("2 Jan"), "pot:", p.Balance.Display)
	}

	last, err := wallet.GetTransactionByReference(ctx, "rent_plan_ada_3")
	check(err)
	fmt.Println("third payment:", last.EntryStatus) // Ada had ₦5,000 left
	// end
	end()
}

func TestTestingEvents(t *testing.T) {
	svc, core, wallet, _ := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN).Accounts[currency.NGN].ID
	tunde := customer(wallet, "user_9034", "Tunde", currency.NGN).Accounts[currency.NGN].ID

	begin("testing/events")
	// sample: testing/events
	var names []string
	core.OnEvent(func(e ledgertest.Event) { names = append(names, e.Event) }) // as each is sent

	fund(wallet, ada, "5000", currency.NGN, "paystack_T9201")
	hold, err := wallet.Hold(ctx, ledger.HoldInput{AccountID: ada, Destination: tunde,
		Amount: ledger.Amount("1000", currency.NGN), Reference: "card_auth_9201"})
	check(err)
	_, err = wallet.Cancel(ctx, hold.TransactionID)
	check(err)

	fmt.Println(names)
	for _, e := range core.Events() { // everything sent so far, oldest first
		fmt.Println(e.Event, len(e.Data) > 0)
	}
	// end
	end()

	begin("testing/handler")
	// sample: testing/handler
	// Test your own webhook handler with the request Blnk would send.
	var cancelled string
	handler := svc.WebhookHandler(ledger.WebhookConfig{Secret: "test-secret"},
		func(ctx context.Context, e *ledger.Event) error {
			if e.Name == ledger.EventTransactionVoid {
				cancelled = e.Receipt.ParentTransaction // the hold that was cancelled
			}
			return nil
		})

	evs := core.Events()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, core.WebhookRequest(evs[len(evs)-1], "test-secret"))
	if rec.Code != 200 || cancelled != hold.TransactionID {
		t.Fatalf("handler answered %d for %q", rec.Code, cancelled)
	}
	fmt.Println(rec.Code, cancelled == hold.TransactionID)
	// end
	end()
}

func TestTestingServer(t *testing.T) {
	curT = t

	begin("testing/server")
	// sample: testing/server
	core := ledgertest.NewServer()
	defer core.Close()

	ng := ledger.LedgerID(core.CreateLedger("Oja Nigeria"))
	gh := ledger.LedgerID(core.CreateLedger("Oja Ghana"))

	// Two services on one Core, as two processes would be in production.
	var calls []string
	spy := ledger.ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
		calls = append(calls, op)
		return ctx, func(error) {}
	})
	api := ledger.Instrument(ledger.New(core.Client(), ledger.Config{LedgerID: ng, Now: core.Now}), spy)
	backOffice := ledger.New(core.Client(), ledger.Config{LedgerID: gh, Now: core.Now})

	acct, err := api.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	check(err)
	check(backOffice.Ops().FreezeAccount(ctx, acct.ID, "test"))
	a, err := api.Wallet().GetAccount(ctx, acct.ID)
	check(err)

	fmt.Println(a.LedgerID == ng, a.Status, calls)
	fmt.Println("URL for blnk.New:", core.URL() != "")
	// end
	end()
}
