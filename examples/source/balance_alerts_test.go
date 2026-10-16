package source_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestBalanceAlerts(t *testing.T) {
	svc, core, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", currency.NGN)
	chidi := customer(wallet, "merchant_311", "Chidi Stores", currency.NGN)
	adaNGN, chidiNGN := ada.Accounts[currency.NGN].ID, chidi.Accounts[currency.NGN].ID
	fund(wallet, adaNGN, "5000", currency.NGN, "paystack_T100")

	begin("balance-alerts/account")
	// sample: balance-alerts/account
	// Tell Ada when her balance drops below ₦1,000.
	low, err := ops.WatchBalance(ctx, ledger.WatchInput{
		AccountID: adaNGN,
		Field:     "balance", // the default
		Operator:  "<",
		Threshold: ledger.Amount("1000", ""), // currency from the account
		MetaData:  map[string]any{"purpose": "low balance push"},
	})
	check(err)
	fmt.Println(low.ID, "on", low.AccountID, low.Field, low.Operator, low.Threshold.Display, low.Threshold.Currency)
	// end
	end()

	begin("balance-alerts/house")
	// sample: balance-alerts/house
	// Alert finance when Paystack owes Oja more than ₦100,000 not yet settled.
	// @PaystackClearing is an asset: Blnk stores it negative, so "owes more" is "below -100,000".
	psk, err := ops.WatchBalance(ctx, ledger.WatchInput{
		House:     ledger.HouseRef{Indicator: ledger.ClearingAccount("paystack"), Currency: currency.NGN},
		Operator:  "<",
		Threshold: ledger.Amount("-100000", currency.NGN),
	})
	check(err)
	fmt.Println(psk.ID, "on", psk.AccountID, psk.Field, psk.Operator, psk.Threshold.Display)
	// end
	end()

	begin("balance-alerts/receive")
	// sample: balance-alerts/receive
	// Ada pays Chidi Stores twice: ₦4,500, then ₦200.
	for i, amt := range []string{"4500", "200"} {
		_, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidiNGN,
			Amount: ledger.Amount(amt, currency.NGN), Fee: ledger.NoFee, Reference: fmt.Sprintf("pay_%d", i)})
		check(err)
	}

	// The emulator records the webhooks Blnk would send. Feed them to your handler.
	for _, raw := range core.Events() {
		if raw.Event != ledger.EventBalanceAlert {
			continue
		}
		e, err := svc.ParseWebhook(core.WebhookRequest(raw, ""), ledger.WebhookConfig{})
		check(err)
		fmt.Println(e.Name, e.Alert.ID, e.Alert.AccountID, e.Alert.Operator, e.Alert.Threshold.Display, e.Alert.Threshold.Currency)
	}
	// end
	end()

	begin("balance-alerts/debounce")
	// sample: balance-alerts/debounce
	// Blnk sends balance.monitor after every update while the condition holds.
	// Notify once, and again only after the balance has recovered.
	notified := map[string]bool{} // in production: a row in your database
	handler := svc.WebhookHandler(ledger.WebhookConfig{}, func(ctx context.Context, e *ledger.Event) error {
		switch e.Name {
		case ledger.EventBalanceAlert:
			if e.Alert.ID != low.ID || notified[e.Alert.ID] {
				return nil // another watch, or Ada was already told
			}
			notified[e.Alert.ID] = true
			fmt.Println("push to Ada: your balance is below", e.Alert.Threshold.Display)
		case ledger.EventTransactionApplied:
			acct, err := wallet.GetAccount(ctx, adaNGN)
			if err != nil {
				return err // answers 500, Blnk retries
			}
			if acct.Balance.PreciseAmount.Cmp(low.Threshold.PreciseAmount) >= 0 {
				delete(notified, low.ID) // recovered: the next drop alerts again
			}
		}
		return nil
	})

	seen := len(core.Events())
	deliver := func(step string) { // send every new webhook to the handler, as Blnk would
		alerts := 0
		for _, raw := range core.Events()[seen:] {
			if raw.Event == ledger.EventBalanceAlert {
				alerts++
			}
			handler.ServeHTTP(httptest.NewRecorder(), core.WebhookRequest(raw, ""))
		}
		seen = len(core.Events())
		acct, err := wallet.GetAccount(ctx, adaNGN)
		check(err)
		fmt.Printf("%-24s balance %8s, alerts received %d\n", step, acct.Balance.Display, alerts)
	}

	for i := 0; i < 3; i++ { // three small payments while below ₦1,000
		_, err := wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidiNGN,
			Amount: ledger.Amount("50", currency.NGN), Fee: ledger.NoFee, Reference: fmt.Sprintf("small_%d", i)})
		check(err)
		deliver(fmt.Sprintf("pays ₦50 (small_%d)", i))
	}
	fund(wallet, adaNGN, "3000", currency.NGN, "paystack_T101")
	deliver("tops up ₦3,000")
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: chidiNGN,
		Amount: ledger.Amount("2500", currency.NGN), Fee: ledger.NoFee, Reference: "pay_big"})
	check(err)
	deliver("pays ₦2,500")
	// end
	end()

	begin("balance-alerts/fields")
	// sample: balance-alerts/fields
	// Warn when a pending payout over ₦1,000 is waiting on Chidi Stores' account.
	pend, err := ops.WatchBalance(ctx, ledger.WatchInput{
		AccountID: chidiNGN,
		Field:     "inflight_debit_balance",
		Operator:  ">",
		Threshold: ledger.Amount("1000", currency.NGN),
	})
	check(err)
	fmt.Println(pend.Field, pend.Operator, pend.Threshold.Display)

	// "=" is accepted and sent as "==", the only equality Blnk's evaluator matches.
	exact, err := ops.WatchBalance(ctx, ledger.WatchInput{AccountID: chidiNGN, Operator: "=", Threshold: ledger.Amount("0", "")})
	check(err)
	fmt.Println(exact.Field, exact.Operator, exact.Threshold.Display)
	// end
	end()

	begin("balance-alerts/unwatch")
	// sample: balance-alerts/unwatch
	check(ops.UnwatchBalance(ctx, pend.ID))
	check(ops.UnwatchBalance(ctx, exact.ID))

	before := len(core.Events())
	w, err := wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: chidiNGN, Amount: ledger.Amount("2000", currency.NGN),
		Fee: ledger.NoFee, Reference: "payout_7001", Provider: "flutterwave"})
	check(err)
	alerts := 0
	for _, raw := range core.Events()[before:] {
		if raw.Event == ledger.EventBalanceAlert {
			alerts++
		}
	}
	fmt.Println(w.Status, "alerts after unwatching:", alerts)

	err = ops.UnwatchBalance(ctx, pend.ID)
	fmt.Println("unwatch twice:", errors.Is(err, ledger.ErrNotFound))
	// end
	end()

	begin("balance-alerts/errors")
	// sample: balance-alerts/errors
	fees := ledger.HouseRef{Indicator: ledger.FeeIncomeAccount, Currency: currency.NGN}
	th := ledger.Amount("1000", "")

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{AccountID: adaNGN, House: fees, Operator: ">", Threshold: th})
	fmt.Println("both set:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{Operator: ">", Threshold: th})
	fmt.Println("neither set:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{AccountID: adaNGN, Operator: "=>", Threshold: th})
	fmt.Println("bad operator:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{AccountID: adaNGN, Field: "available", Operator: "<", Threshold: th})
	fmt.Println("bad field:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{AccountID: adaNGN, Operator: "<", Threshold: ledger.Amount("10", currency.USD)})
	fmt.Println("wrong currency:", errors.Is(err, ledger.ErrInvalidInput))

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{House: fees, Operator: ">", Threshold: th})
	fmt.Println("house account not used yet:", errors.Is(err, ledger.ErrNotFound))

	_, err = ops.WatchBalance(ctx, ledger.WatchInput{AccountID: ledger.AccountID(ledger.FeeIncomeAccount), Operator: ">", Threshold: th})
	fmt.Println("house account as AccountID:", errors.Is(err, ledger.ErrWrongKind))
	// end
	end()
}
