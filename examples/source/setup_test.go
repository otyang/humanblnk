package source_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
)

func TestSetup(t *testing.T) {
	_, core, _, _ := setup(t, ledger.Config{})
	blnkURL := core.URL() // stands in for your Core, e.g. http://localhost:5001
	apiKey := "blnk_secret_key"

	begin("setup/provision")
	// sample: setup/provision
	// Once, at provisioning time: no LedgerID yet.
	boot := humanblnk.New(blnkURL, humanblnk.Config{}, humanblnk.WithAPIKey(apiKey))
	check(boot.Check(ctx)) // Core is up; no ledger to check yet

	ng, err := boot.Ops().CreateLedger(ctx, humanblnk.CreateLedgerInput{Key: "oja-ng", Name: "Oja Nigeria"})
	check(err)
	again, err := boot.Ops().CreateLedger(ctx, humanblnk.CreateLedgerInput{Key: "oja-ng", Name: "Oja Nigeria"})
	check(err)
	fmt.Println("ledger:", ng.ID, ng.Name)
	fmt.Println("run again:", again.ID == ng.ID, "replayed:", again.Replayed)
	// end
	end()

	begin("setup/connect")
	// sample: setup/connect
	svc := humanblnk.New(blnkURL,
		humanblnk.Config{LedgerID: ng.ID}, // the id from provisioning, e.g. from BLNK_LEDGER_ID
		humanblnk.WithAPIKey(apiKey),      // sent as X-Blnk-Key
	)
	if err := svc.Check(ctx); err != nil {
		panic(err) // log.Fatal in a real app: don't start with a broken setup
	}
	fmt.Println("connected to Blnk; humanblnk", humanblnk.Version)
	// end
	end()

	begin("setup/two-step")
	// sample: setup/two-step
	client := blnk.New(blnkURL,
		blnk.WithAPIKey(apiKey),
		blnk.WithRetries(5, 100*time.Millisecond),                    // default 3 retries from 200ms
		blnk.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}), // default timeout 30s
		blnk.WithUserAgent("oja-wallet/2.4"),                         // default humanblnk-go/<version>
	)
	svc2 := ledger.New(client, ledger.Config{LedgerID: ng.ID})
	check(svc2.Check(ctx))

	h, err := client.Health(ctx) // the client is usable on its own too
	check(err)
	fmt.Println("Core status:", h.Status)
	// end
	end()

	begin("setup/handles")
	// sample: setup/handles
	var l ledger.Ledger = svc2 // what the rest of the app depends on

	// The customer-facing API gets only the wallet handle.
	type customerAPI struct{ wallet ledger.Wallet }
	// The back-office tool gets only the ops handle.
	type backOffice struct{ ops ledger.Ops }

	api := customerAPI{wallet: l.Wallet()}
	admin := backOffice{ops: l.Ops()}

	ada, err := api.wallet.OpenCustomer(ctx, ledger.CustomerInput{
		FirstName: "Ada", LastName: "Obi", Currencies: []string{currency.NGN}, ExternalID: "user_8812",
	})
	check(err)
	// api.wallet.FreezeAccount(...) would not compile: Wallet has no such method.
	check(admin.ops.FreezeAccount(ctx, ada.Accounts[currency.NGN].ID, "KYC review"))

	acct, err := api.wallet.GetAccount(ctx, ada.Accounts[currency.NGN].ID)
	check(err)
	fmt.Println(acct.ID, acct.Status)
	// end
	end()

	begin("setup/check-ledger")
	// sample: setup/check-ledger
	wrong := humanblnk.New(blnkURL, humanblnk.Config{LedgerID: "ldg_typo"}, humanblnk.WithAPIKey(apiKey))
	err = wrong.Check(ctx)
	fmt.Println(err)
	fmt.Println("ErrLedgerNotFound:", errors.Is(err, ledger.ErrLedgerNotFound), "ErrNotFound:", errors.Is(err, ledger.ErrNotFound))

	wrongKind := humanblnk.New(blnkURL, humanblnk.Config{LedgerID: "bln_000123"}) // a balance id, not a ledger id
	err = wrongKind.Check(ctx)
	fmt.Println("ErrWrongKind:", errors.Is(err, ledger.ErrWrongKind))
	// end
	end()

	begin("setup/check-client")
	// sample: setup/check-client
	noURL := humanblnk.New("", humanblnk.Config{}) // BLNK_URL not set
	err = noURL.Check(ctx)
	fmt.Println(err)

	noInstance := humanblnk.New(humanblnk.CloudProxyURL, humanblnk.Config{}, humanblnk.WithAPIKey(apiKey))
	err = noInstance.Check(ctx)
	fmt.Println(err)

	badInstance := humanblnk.New(humanblnk.CloudProxyURL, humanblnk.Config{},
		humanblnk.WithAPIKey(apiKey), humanblnk.WithInstanceID("deployment_7f3a"))
	err = badInstance.Check(ctx)
	fmt.Println(err)
	fmt.Println("ErrUnavailable:", errors.Is(err, ledger.ErrUnavailable))
	// end
	end()

	begin("setup/check-down")
	// sample: setup/check-down
	core.Close() // Core goes away
	down := ledger.New(blnk.New(blnkURL, blnk.WithRetries(0, 0)), ledger.Config{})
	err = down.Check(ctx)
	fmt.Println("ErrUnavailable:", errors.Is(err, ledger.ErrUnavailable))
	// end
	end()
}
