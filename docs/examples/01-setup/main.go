// Chapter 1: create the ledgers once, then build the Ledger your service
// uses from their ids.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger"
)

func main() {
	ctx := context.Background()
	url := os.Getenv("BLNK_URL")
	if url == "" {
		url = "http://localhost:5001"
	}
	client := blnk.New(url) // add blnk.WithAPIKey(key) when Blnk runs in secure mode

	// --- Once, at setup (a script or migration, never at start-up) ---
	// snippet:once
	setup := ledger.New(client)
	suffix := time.Now().Format("150405") // only so this example can run twice
	wallets, err := setup.Books.Create(ctx, "Customer Wallets "+suffix, nil)
	if err != nil {
		log.Fatal(err)
	}
	savings, err := setup.Books.Create(ctx, "Savings "+suffix, nil)
	if err != nil {
		log.Fatal(err)
	}
	ops, err := setup.Books.Create(ctx, "Operations "+suffix, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Put these in your config:")
	fmt.Println("  WALLETS_LEDGER =", wallets.ID)
	fmt.Println("  SAVINGS_LEDGER =", savings.ID)
	fmt.Println("  OPS_LEDGER     =", ops.ID)
	// end-snippet

	// --- At start-up, every time: one Ledger for the whole service ---
	// snippet:startup
	books := ledger.New(client,
		ledger.WithAccountLedger(wallets.ID),   // where customer wallets open
		ledger.WithCustomerLedgers(savings.ID), // other customer-only ledgers
	)

	// The services hang off it.
	_ = books.Identities     // owners of customer accounts
	_ = books.Accounts       // open, create and read accounts
	_ = books.Transactions   // record, preview, reverse and read money movements
	_ = books.Holds          // reserve money, then capture or cancel
	_ = books.Watches        // balance thresholds that fire a webhook
	_ = books.Reconciliation // match provider statements against entries
	_ = books.Books          // the ledgers themselves
	// end-snippet

	all, err := books.Books.List(ctx, 3, 0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Newest ledgers:")
	for _, b := range all {
		fmt.Printf("  %s  %s\n", b.ID, b.Name)
	}
}
