package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

// webhooksLast returns the newest transaction.applied event the emulator emitted.
func webhooksLast(core *ledgertest.Server) ledgertest.Event {
	ev := core.Events()
	for i := len(ev) - 1; i >= 0; i-- {
		if ev[i].Event == ledger.EventTransactionApplied {
			return ev[i]
		}
	}
	panic("no transaction.applied event")
}

func TestWebhooks(t *testing.T) {
	svc, core, wallet, ops := setup(t, ledger.Config{})
	ada := customer(wallet, "user_8812", "Ada", "NGN").Accounts["NGN"].ID
	secret := "blnk_secret_key" // Blnk's server.secret_key

	begin("webhooks/handler")
	// sample: webhooks/handler
	handler := svc.WebhookHandler(ledger.WebhookConfig{Secret: secret},
		func(ctx context.Context, e *ledger.Event) error {
			switch e.Name {
			case ledger.EventTransactionApplied:
				fmt.Println("posted:", e.Receipt.Reference, e.Receipt.Amount.Display, e.Receipt.Amount.Currency)
			case ledger.EventTransactionVoid:
				fmt.Println("cancelled or expired:", e.Receipt.ParentTransaction) // the pending transaction
			default:
				fmt.Println("ignored:", e.Name)
			}
			return nil // 200: Blnk won't send it again
		})
	// In main: http.Handle("/webhooks/blnk", handler)

	// Paystack confirms Ada's top-up, and Oja records it.
	fund(wallet, ada, "50000", "NGN", "paystack_T7001")

	// Blnk then POSTs the event to the handler. The emulator builds that request.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, core.WebhookRequest(webhooksLast(core), secret))
	fmt.Println("answered:", rec.Code)
	// end
	end()

	begin("webhooks/answers")
	// sample: webhooks/answers
	send := func(h http.Handler, r *http.Request) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	e := webhooksLast(core)

	fmt.Println("wrong secret:", send(handler, core.WebhookRequest(e, "guessed")))
	fmt.Println("unsigned:    ", send(handler, core.WebhookRequest(e, "")))

	get := httptest.NewRequest(http.MethodGet, "/webhooks/blnk", nil)
	fmt.Println("GET:         ", send(handler, get))

	busy := svc.WebhookHandler(ledger.WebhookConfig{Secret: secret},
		func(ctx context.Context, e *ledger.Event) error { return errors.New("database down") })
	fmt.Println("handler err: ", send(busy, core.WebhookRequest(e, secret)))

	// Without a Secret nothing is verified: a forged request gets through.
	open := svc.WebhookHandler(ledger.WebhookConfig{},
		func(ctx context.Context, e *ledger.Event) error { return nil })
	fmt.Println("no Secret:   ", send(open, core.WebhookRequest(e, "")))
	bad := httptest.NewRequest(http.MethodPost, "/webhooks/blnk", strings.NewReader(`{"hello":"world"}`))
	fmt.Println("not Blnk:    ", send(open, bad))

	// end
	end()

	begin("webhooks/parse")
	// sample: webhooks/parse
	// Your own handler, e.g. inside a router you already use.
	mine := func(w http.ResponseWriter, r *http.Request) {
		e, err := svc.ParseWebhook(r, ledger.WebhookConfig{Secret: secret})
		if errors.Is(err, ledger.ErrSignature) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Println(e.Name, e.Receipt.TransactionID, e.Receipt.EntryStatus, e.Receipt.Destination == string(ada))
		w.WriteHeader(http.StatusOK)
	}
	rec = httptest.NewRecorder()
	mine(rec, core.WebhookRequest(webhooksLast(core), secret))
	fmt.Println("answered:", rec.Code)
	// end
	end()

	begin("webhooks/stale")
	// sample: webhooks/stale
	old := core.WebhookRequest(webhooksLast(core), secret) // signed now
	core.Advance(6 * time.Minute)                          // replayed 6 minutes later

	_, err := svc.ParseWebhook(old, ledger.WebhookConfig{Secret: secret})
	fmt.Println("ErrSignature:", errors.Is(err, ledger.ErrSignature), "stale:", errors.Is(err, blnk.ErrStale))

	old = core.WebhookRequest(webhooksLast(core), secret)
	core.Advance(20 * time.Minute)
	_, err = svc.ParseWebhook(old, ledger.WebhookConfig{Secret: secret, Tolerance: 30 * time.Minute})
	fmt.Println("with a 30 minute window:", err)
	// end
	end()

	begin("webhooks/alert")
	// sample: webhooks/alert
	_, err = ops.WatchBalance(ctx, ledger.WatchInput{
		AccountID: ada, Operator: ">", Threshold: ledger.Amount("100000", "NGN"),
	})
	check(err)
	fund(wallet, ada, "60000", "NGN", "paystack_T7002") // Ada now has ₦110,000

	for _, ev := range core.Events() {
		if ev.Event != ledger.EventBalanceAlert {
			continue
		}
		e, err := svc.ParseWebhook(core.WebhookRequest(ev, secret), ledger.WebhookConfig{Secret: secret})
		check(err)
		fmt.Println(e.Name, e.Alert.AccountID, e.Alert.Field, e.Alert.Operator, e.Alert.Threshold.Display, e.Alert.Threshold.Currency)
	}
	// end
	end()

	begin("webhooks/decode")
	// sample: webhooks/decode
	// A body you already verified, e.g. replayed from your own queue.
	for _, ev := range core.Events() {
		body, err := json.Marshal(ev)
		check(err)
		e, err := svc.DecodeEvent(body)
		check(err)
		switch {
		case e.Receipt != nil:
			fmt.Println(e.Name, "Receipt", e.Receipt.Reference)
		case e.Alert != nil:
			fmt.Println(e.Name, "Alert", e.Alert.ID, "threshold in kobo:", e.Alert.Threshold.PreciseAmount)
		}
	}
	// end
	end()

	begin("webhooks/idempotent")
	// sample: webhooks/idempotent
	seen := map[string]bool{} // your database in production, with a unique key
	credited := 0
	notify := svc.WebhookHandler(ledger.WebhookConfig{Secret: secret},
		func(ctx context.Context, e *ledger.Event) error {
			if e.Name != ledger.EventTransactionApplied {
				return nil
			}
			key := e.Name + "/" + e.Receipt.TransactionID
			if seen[key] {
				return nil // a repeat delivery: already handled
			}
			seen[key] = true
			credited++ // e.g. send Ada a push notification
			return nil
		})

	tx := webhooksLast(core) // Blnk delivers it, then delivers it again
	for range 3 {
		rec := httptest.NewRecorder()
		notify.ServeHTTP(rec, core.WebhookRequest(tx, secret))
		fmt.Println("answered:", rec.Code)
	}
	fmt.Println("notifications sent:", credited)
	// end
	end()
}
