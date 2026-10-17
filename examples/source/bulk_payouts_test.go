package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

// bulkBalance reads an account's balance for display.
func bulkBalance(w ledger.Wallet, id ledger.AccountID) string {
	a, err := w.GetAccount(ctx, id)
	check(err)
	return a.Balance.Display
}

func TestBulkPayouts(t *testing.T) {
	svc, core, wallet, _ := setup(t, ledger.Config{
		Limits: []ledger.Limit{{Tier: "*", Currency: currency.NGN, PerTransaction: "100000"}},
	})
	ada := customer(wallet, "user_ada", "Ada", currency.NGN)
	tunde := customer(wallet, "user_tunde", "Tunde", currency.NGN)
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", OrganizationName: "Chidi Stores Ltd",
		Currencies: []string{currency.NGN}, ExternalID: "merchant_311", MetaData: map[string]any{"tier": "business"}})
	check(err)
	adaNGN, tundeNGN, chidi := ada.Accounts[currency.NGN].ID, tunde.Accounts[currency.NGN].ID, shop.Accounts[currency.NGN].ID
	fund(wallet, chidi, "600000", currency.NGN, "chidi_bank_transfer_0925")

	begin("bulk-payouts/payroll")
	// sample: bulk-payouts/payroll
	// Chidi Stores pays its staff on the 25th: all or nothing.
	staff := []struct {
		account ledger.AccountID
		salary  string
	}{{tundeNGN, "85000"}, {adaNGN, "120000"}}

	var items []ledger.TransferInput
	for i, s := range staff {
		items = append(items, ledger.TransferInput{
			From: chidi, To: s.account,
			Amount: ledger.Amount(s.salary, currency.NGN), Fee: ledger.NoFee,
			Reference:   fmt.Sprintf("payroll_2026_09_%d", i+1), // unique per item
			Description: "September salary",
		})
	}
	batch, err := wallet.Bulk(ctx, ledger.BulkTransferInput{Items: items, Atomic: true})
	check(err)
	fmt.Println(batch.BatchID, batch.Status, batch.Count)
	fmt.Println("Chidi Stores:", bulkBalance(wallet, chidi), "Tunde:", bulkBalance(wallet, tundeNGN), "Ada:", bulkBalance(wallet, adaNGN))

	one, err := wallet.GetTransactionByReference(ctx, "payroll_2026_09_2")
	check(err)
	fmt.Println(one.Reference, one.Status, one.Amount.Display)
	// end
	end()

	begin("bulk-payouts/atomic-fail")
	// sample: bulk-payouts/atomic-fail
	// October: the second salary is more than Chidi Stores has left.
	_, err = wallet.Bulk(ctx, ledger.BulkTransferInput{Atomic: true, Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("85000", currency.NGN), Fee: ledger.NoFee, Reference: "payroll_2026_10_1"},
		{From: chidi, To: adaNGN, Amount: ledger.Amount("900000", currency.NGN), Fee: ledger.NoFee, Reference: "payroll_2026_10_2"},
	}})
	fmt.Println("refused:", errors.Is(err, ledger.ErrInsufficientFunds))
	fmt.Println("Chidi Stores unchanged:", bulkBalance(wallet, chidi), "Tunde unchanged:", bulkBalance(wallet, tundeNGN))
	// end
	end()

	begin("bulk-payouts/non-atomic")
	// sample: bulk-payouts/non-atomic
	// Without Atomic, Blnk posts items in order and stops at the first it
	// can't post. The ones before it stay posted.
	bonuses := ledger.BulkTransferInput{Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("10000", currency.NGN), Fee: ledger.NoFee, Reference: "bonus_1"},
		{From: chidi, To: adaNGN, Amount: ledger.Amount("900000", currency.NGN), Fee: ledger.NoFee, Reference: "bonus_2"},
	}}
	b, err := wallet.Bulk(ctx, bonuses)
	fmt.Println("refused:", errors.Is(err, ledger.ErrInsufficientFunds))
	fmt.Println(b.Status, "count:", b.Count, "of", len(bonuses.Items))
	for _, it := range b.Items {
		fmt.Println(" ", it.Reference, "recorded:", it.Recorded, it.Status)
	}

	// Lower bonus_2 to what Chidi Stores can pay, and send the same batch
	// again: bonus_1 is replayed, not paid twice.
	bonuses.Items[1].Amount = ledger.Amount("5000", currency.NGN)
	b, err = wallet.Bulk(ctx, bonuses)
	check(err)
	fmt.Println("retry:", b.Status, "count:", b.Count, "Tunde:", bulkBalance(wallet, tundeNGN))
	// end
	end()

	begin("bulk-payouts/pending")
	// sample: bulk-payouts/pending
	// Hold the batch until the finance lead approves it.
	held, err := wallet.Bulk(ctx, ledger.BulkTransferInput{Atomic: true, Pending: true, Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("20000", currency.NGN), Fee: ledger.NoFee, Reference: "commission_q3_1"},
		{From: chidi, To: adaNGN, Amount: ledger.Amount("30000", currency.NGN), Fee: ledger.NoFee, Reference: "commission_q3_2"},
	}})
	check(err)
	acct, err := wallet.GetAccount(ctx, chidi)
	check(err)
	fmt.Println(held.Status, "Chidi Stores balance:", acct.Balance.Display, "available:", acct.Available.Display)

	_, err = wallet.Confirm(ctx, held.BatchID, nil) // approved: every item posts
	check(err)
	fmt.Println("after confirm:", bulkBalance(wallet, chidi))

	held2, err := wallet.Bulk(ctx, ledger.BulkTransferInput{Pending: true, Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("5000", currency.NGN), Fee: ledger.NoFee, Reference: "refund_run_1"},
	}})
	check(err)
	_, err = wallet.Cancel(ctx, held2.BatchID) // turned down: every item is released
	check(err)
	acct, err = wallet.GetAccount(ctx, chidi)
	check(err)
	fmt.Println("after cancel:", acct.Balance.Display, "available:", acct.Available.Display)
	// end
	end()

	begin("bulk-payouts/async")
	// sample: bulk-payouts/async
	queued, err := wallet.Bulk(ctx, ledger.BulkTransferInput{Atomic: true, Async: true, Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "tips_0930_1"},
		{From: chidi, To: adaNGN, Amount: ledger.Amount("1000", currency.NGN), Fee: ledger.NoFee, Reference: "tips_0930_2"},
	}})
	check(err)
	fmt.Println(queued.Status)

	// The outcome arrives at your webhook. Here we replay what the in-memory Blnk sent.
	events := core.Events()
	last := events[len(events)-1]
	e, err := svc.ParseWebhook(core.WebhookRequest(last, ""), ledger.WebhookConfig{})
	check(err)
	fmt.Println(e.Name, "same batch:", e.Bulk.BatchID == queued.BatchID)
	// end
	end()

	begin("bulk-payouts/limits")
	// sample: bulk-payouts/limits
	// Every account here may send at most ₦100,000 per transaction.
	_, err = wallet.Transfer(ctx, ledger.TransferInput{From: chidi, To: tundeNGN,
		Amount: ledger.Amount("150000", currency.NGN), Fee: ledger.NoFee, Reference: "big_one"})
	fmt.Println("Transfer:", errors.Is(err, ledger.ErrLimitExceeded))

	big, err := wallet.Bulk(ctx, ledger.BulkTransferInput{Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("150000", currency.NGN), Fee: ledger.NoFee, Reference: "big_one_bulk"},
	}})
	check(err)
	fmt.Println("Bulk:", big.Status) // not checked against limits
	// end
	end()

	begin("bulk-payouts/errors")
	// sample: bulk-payouts/errors
	_, err = wallet.Bulk(ctx, ledger.BulkTransferInput{})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Bulk(ctx, ledger.BulkTransferInput{Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "dup"},
		{From: chidi, To: adaNGN, Amount: ledger.Amount("10", currency.NGN), Fee: ledger.NoFee, Reference: "dup"},
	}})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Bulk(ctx, ledger.BulkTransferInput{Items: []ledger.TransferInput{
		{From: chidi, To: tundeNGN, Amount: ledger.Amount("10", currency.NGN), Reference: "no_fee"},
	}})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.Bulk(ctx, ledger.BulkTransferInput{Items: make([]ledger.TransferInput, 10001)})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()
}
