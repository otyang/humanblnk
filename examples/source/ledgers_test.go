package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestLedgers(t *testing.T) {
	_, core, wallet, ops := setup(t, ledger.Config{})
	client := core.Client() // stands for your blnk.Client

	begin("ledgers/create")
	// sample: ledgers/create
	// The provisioning job for Nigeria.
	ng, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{
		Key:      "oja-ng",      // required: your stable name; makes the call safe to repeat
		Name:     "Oja Nigeria", // default: Key
		MetaData: map[string]any{"country": "NG", "regulator": "CBN"},
	})
	check(err)
	fmt.Println(ng.ID, ng.Key, ng.Name, ng.Status, ng.MetaData["country"], "replayed:", ng.Replayed)

	// The job runs again on the next deploy.
	again, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-ng", Name: "Oja Nigeria"})
	check(err)
	fmt.Println(again.ID, "same ledger:", again.ID == ng.ID, "replayed:", again.Replayed)
	// end
	end()

	begin("ledgers/markets")
	// sample: ledgers/markets
	gh, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-gh", Name: "Oja Ghana",
		MetaData: map[string]any{"country": "GH"}})
	check(err)

	ada, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.NGN, LedgerID: ng.ID, ExternalID: "user_8812"})
	check(err)
	kofi, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.GHS, LedgerID: gh.ID, ExternalID: "user_9100"})
	check(err)
	fmt.Println("Ada's naira account in", ada.LedgerID, "| Kofi's cedi account in", kofi.LedgerID)
	// end
	end()

	begin("ledgers/read")
	// sample: ledgers/read
	l, err := ops.GetLedger(ctx, gh.ID)
	check(err)
	fmt.Println(l.ID, l.Name, l.Status, l.CreatedAt.Format("2006-01-02"))

	page, err := ops.ListLedgers(ctx, ledger.LedgerQuery{Limit: 10}) // oldest first
	check(err)
	for _, l := range page.Items {
		fmt.Printf("%s %q key=%q %s\n", l.ID, l.Name, l.Key, l.Status)
	}
	// end
	end()

	begin("ledgers/check")
	// sample: ledgers/check
	// At start-up, with the ledger id from configuration (e.g. BLNK_LEDGER_ID).
	svc := ledger.New(client, ledger.Config{LedgerID: ng.ID})
	fmt.Println("good id:", svc.Check(ctx))

	typo := ledger.New(client, ledger.Config{LedgerID: "ldg_0000_typo"})
	err = typo.Check(ctx)
	fmt.Println("bad id:", errors.Is(err, ledger.ErrLedgerNotFound), err)
	// end
	end()

	begin("ledgers/twins")
	// sample: ledgers/twins
	// Two provisioning jobs raced and both created "oja-ke" (shown here by
	// creating the twins straight in Blnk).
	for i := 0; i < 2; i++ {
		_, err := client.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: "Oja Kenya",
			MetaData: blnk.MetaData{"ledger_key": "oja-ke", "status": "active"}})
		check(err)
	}

	ke, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-ke", Name: "Oja Kenya"})
	check(err)
	fmt.Println("everyone gets", ke.ID, "replayed:", ke.Replayed)

	all, err := ops.ListLedgers(ctx, ledger.LedgerQuery{Offset: 4})
	check(err)
	for _, l := range all.Items {
		fmt.Println(l.ID, l.Key, l.Status, l.DuplicateOf)
	}
	twin := all.Items[len(all.Items)-1]
	_, err = wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: currency.KES, LedgerID: twin.ID})
	fmt.Println("open in the twin:", errors.Is(err, ledger.ErrInvalidInput), err)
	// end
	end()

	begin("ledgers/errors")
	// sample: ledgers/errors
	_, err = ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-ng", Name: "Oja Naija"})
	fmt.Println(errors.Is(err, ledger.ErrDuplicateReference), err)

	_, err = ops.CreateLedger(ctx, ledger.CreateLedgerInput{Name: "No key"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = ops.GetLedger(ctx, "ldg_0000_typo")
	fmt.Println(errors.Is(err, ledger.ErrLedgerNotFound), errors.Is(err, ledger.ErrNotFound))

	_, err = ops.GetLedger(ctx, ledger.LedgerID(ada.ID)) // an account id
	fmt.Println(errors.Is(err, ledger.ErrWrongKind), err)
	// end
	end()
}
