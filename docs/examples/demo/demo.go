// Package demo is the setup the tutorial's examples share, so each one
// shows only the part it teaches. Nothing here is needed in a real service:
// there you build the ledger once at start-up from your config (chapter 1).
package demo

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
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
func Setup() (context.Context, *ledger.Ledger) {
	ctx := context.Background()
	url := os.Getenv("BLNK_URL")
	if url == "" {
		url = "http://localhost:5001"
	}
	client := blnk.New(url)
	wallets := Must(ledger.New(client).Books.Create(ctx, "Customer Wallets "+Run, nil))
	return ctx, ledger.New(client, ledger.WithAccountLedger(wallets.ID))
}

// Customer onboards a customer (chapter 2) and returns their wallet.
func Customer(ctx context.Context, books *ledger.Ledger, name string, c currency.Currency) ledger.Account {
	owner := Must(books.Identities.Create(ctx, ledger.Meta{"customer_id": name + "_" + Run}))
	return Must(books.Accounts.OpenForCustomer(ctx, ledger.CustomerAccount{Owner: owner, Currency: c}))
}

// Deposit credits a wallet as a bank transfer through Paystack would
// (chapter 4).
func Deposit(ctx context.Context, books *ledger.Ledger, wallet ledger.Account, amount string) {
	m := money.MustParse(amount, wallet.Currency)
	Must(books.Transactions.Record(ctx, ledger.Transaction{
		Ref:         ledger.Reference("deposit", "paystack", wallet.ID, amount, Run),
		From:        ledger.Single(ledger.ClearingAccount("paystack"), m),
		To:          ledger.Single(ledger.AccountRef(wallet.ID), m),
		Description: "Bank transfer",
	}))
}

// Available is what the account can spend now, as text.
func Available(ctx context.Context, books *ledger.Ledger, id string) string {
	a := Must(books.Accounts.Get(ctx, id))
	return a.Available.String() + " " + a.Available.Code()
}

// Must stops the example on an error the example doesn't expect.
func Must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}
