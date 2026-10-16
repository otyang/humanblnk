package source_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestObservability(t *testing.T) {
	svc, _, _, _ := setup(t, ledger.Config{})

	begin("observability/instrument")
	// sample: observability/instrument
	record := ledger.ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
		fmt.Println("start", op)
		return ctx, func(err error) {
			fmt.Println("done ", op, "err:", err != nil)
		}
	})

	var l ledger.Ledger = ledger.Instrument(svc, record)
	wallet := l.Wallet() // hand this to the rest of the app, as before

	ada, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{
		FirstName: "Ada", LastName: "Obi", Currencies: []string{currency.NGN}, ExternalID: "user_8812",
		MetaData: map[string]any{"tier": "tier2"},
	})
	check(err)
	_, err = wallet.GetAccount(ctx, "bln_999999")
	fmt.Println(errors.Is(err, ledger.ErrNotFound))
	// end
	end()

	adaNGN := ada.Accounts[currency.NGN].ID
	tunde, err := l.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN})
	check(err)

	begin("observability/log")
	// sample: observability/log
	// A text logger without time and duration, so this output is the same on every run.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == "duration" {
				return slog.Attr{}
			}
			return a
		},
	}))
	logged := ledger.Instrument(svc, ledger.LogObserver(logger))

	_, err = logged.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: adaNGN,
		Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "paystack_T8001", Provider: "paystack"})
	check(err)
	_, _ = logged.Wallet().Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tunde.ID, // more than she has
		Amount: ledger.Amount("9000", currency.NGN), Fee: ledger.NoFee, Reference: "p2p_8001"})
	_, _ = logged.Wallet().Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tunde.ID, // no reference: a bug
		Amount: ledger.Amount("100", currency.NGN), Fee: ledger.NoFee})
	check(logged.Ops().FreezeAccount(ctx, tunde.ID, "chargeback"))
	// end
	end()

	check(svc.Ops().UnfreezeAccount(ctx, tunde.ID))

	begin("observability/metrics")
	// sample: observability/metrics
	counts := map[string]int{} // a Prometheus counter vec in production
	metrics := ledger.ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
		return ctx, func(err error) {
			outcome := "ok"
			switch {
			case errors.Is(err, ledger.ErrInsufficientFunds), errors.Is(err, ledger.ErrLimitExceeded),
				errors.Is(err, ledger.ErrAccountFrozen), errors.Is(err, ledger.ErrAccountClosed):
				outcome = "refused"
			case err != nil:
				outcome = "failed"
			}
			counts[op+" "+outcome]++
		}
	})
	m := ledger.Instrument(svc, metrics).Wallet()

	for i, amount := range []string{"500", "700", "90000"} {
		_, _ = m.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tunde.ID,
			Amount: ledger.Amount(amount, currency.NGN), Fee: ledger.NoFee, Reference: fmt.Sprint("p2p_820", i)})
	}
	_, _ = m.Transfer(ctx, ledger.TransferInput{From: adaNGN, To: tunde.ID,
		Amount: ledger.Amount("100", currency.NGN), Fee: ledger.NoFee}) // no reference
	_, _ = m.GetAccount(ctx, adaNGN)

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Println(k, counts[k])
	}
	// end
	end()

	begin("observability/context")
	// sample: observability/context
	type requestID struct{}
	trace := ledger.ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
		id, _ := ctx.Value(requestID{}).(string)
		// To trace, start a span here and return the context that carries it.
		return ctx, func(err error) {
			fmt.Println("request", id, "op", op, "failed:", err != nil)
		}
	})
	traced := ledger.Instrument(svc, trace).Wallet()

	reqCtx := context.WithValue(ctx, requestID{}, "req_5521") // from your HTTP middleware
	_, err = traced.GetAccount(reqCtx, adaNGN)
	check(err)
	// end
	end()
}
