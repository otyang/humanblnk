// Package demo is the setup the tutorial's examples share, so each one
// shows only the part it teaches. Nothing here is needed in a real service:
// there you build the Client once at start-up from your config (chapter 1).
package demo

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// Currencies the examples use.
var (
	NGN = currency.MustGet(currency.NGN)
	USD = currency.MustGet(currency.USD)
)

// Run makes this run's ids unique, so an example can be run again and
// again against the same Blnk without its references colliding.
var Run = fmt.Sprint(time.Now().UnixNano())

// Setup connects to the Blnk at $BLNK_URL (default http://localhost:5001)
// and creates a fresh "Customer Wallets" ledger for this run.
func Setup() (context.Context, *humanblnk.Client) {
	ctx := context.Background()
	url := os.Getenv("BLNK_URL")
	if url == "" {
		url = "http://localhost:5001"
	}
	client := blnk.New(url)
	wallets := Must(humanblnk.New(client).Ledgers.Create(ctx, "Customer Wallets "+Run, nil))
	return ctx, humanblnk.New(client, humanblnk.WithAccountLedger(wallets.ID))
}

// Customer onboards a customer (chapter 2) and returns their wallet.
func Customer(ctx context.Context, hb *humanblnk.Client, name string, c currency.Currency) humanblnk.Account {
	owner := Must(hb.Identities.Create(ctx, humanblnk.Meta{"customer_id": name + "_" + Run}))
	return Must(hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: c}))
}

// Deposit credits a wallet as a bank transfer through Paystack would
// (chapter 4).
func Deposit(ctx context.Context, hb *humanblnk.Client, wallet humanblnk.Account, amount string) {
	m := money.MustParse(amount, wallet.Currency)
	Must(hb.Transactions.Record(ctx, humanblnk.Transaction{
		Ref:         humanblnk.Reference("deposit", "paystack", wallet.ID, amount, Run),
		From:        humanblnk.Single(humanblnk.ClearingAccount("paystack"), m),
		To:          humanblnk.Single(humanblnk.AccountRef(wallet.ID), m),
		Description: "Bank transfer",
	}))
}

// Available is what the account can spend now, as text.
func Available(ctx context.Context, hb *humanblnk.Client, id string) string {
	a := Must(hb.Accounts.Get(ctx, id))
	return a.Available.String() + " " + a.Available.Code()
}

// Must stops the example on an error the example doesn't expect.
func Must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}
