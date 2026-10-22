// Chapter 2: onboard a customer, open their accounts and read balances.
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/docs/examples/demo"
)

func main() {
	ctx, hb := demo.Setup()
	customerID := "cus_ada_" + demo.Run // your own customer id, from your database

	// 1. The owner: find it, or create it the first time.
	// snippet:owner
	owner := ""
	found, err := hb.Identities.FindByCustomer(ctx, customerID)
	switch {
	case errors.Is(err, humanblnk.ErrNotFound):
		owner, err = hb.Identities.Create(ctx, humanblnk.Meta{"customer_id": customerID})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("created owner", owner)
	case err != nil:
		log.Fatal(err)
	default:
		owner = found.ID
	}
	// Save owner on your customer record now, before opening accounts.
	// end-snippet

	// 2. Her naira wallet. Opening it again returns the same account.
	// snippet:open
	ngn, err := hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: demo.NGN})
	if err != nil {
		log.Fatal(err)
	}
	again, err := hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: demo.NGN})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("NGN wallet", ngn.ID, "| opened again:", again.ID, "| same:", ngn.ID == again.ID)

	// 3. A dollar wallet is a second account.
	usd, err := hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: demo.USD})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("USD wallet", usd.ID)
	// end-snippet

	// 4. All her accounts, and one balance.
	// snippet:balances
	accounts, total, err := hb.Accounts.List(ctx, humanblnk.AccountQuery{Owner: owner})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d accounts:\n", total)
	for _, a := range accounts {
		fmt.Printf("  %s  %s  available %s\n", a.ID, a.Currency.Code(), a.Available)
	}
	acct, err := hb.Accounts.Get(ctx, ngn.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Get %s: Total %s, Held %s, Available %s\n", acct.ID, acct.Total, acct.Held, acct.Available)
	// end-snippet

	// 5. Tag the owner later; keys you don't send keep their values.
	// snippet:meta
	meta, err := hb.Identities.UpdateMeta(ctx, owner, humanblnk.Meta{"tier": "gold"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("owner meta:", meta)
	// end-snippet

	// 6. An account for your company itself, not a customer: CreateInternal, in a
	// ledger that isn't a customer one.
	// snippet:internal
	ops, err := hb.Ledgers.Create(ctx, "Operations "+demo.Run, nil)
	if err != nil {
		log.Fatal(err)
	}
	pool, err := hb.Accounts.CreateInternal(ctx, humanblnk.NewAccount{Ledger: ops.ID, Currency: demo.NGN, Meta: humanblnk.Meta{"purpose": "escrow"}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("escrow pool", pool.ID)
	// end-snippet

	// The rules hold both ways.
	// snippet:rules
	_, err = hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Currency: demo.NGN})
	fmt.Println("customer account without an owner:", err)
	_, err = hb.Accounts.CreateInternal(ctx, humanblnk.NewAccount{Ledger: acct.Ledger, Currency: demo.NGN})
	fmt.Println("internal account in the wallets ledger:", err)
	// end-snippet
}
