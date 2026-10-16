package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/otyang/humanblnk/ledger"
)

func TestCustomerAccounts(t *testing.T) {
	_, _, wallet, ops := setup(t, ledger.Config{})

	begin("customer-accounts/person")
	// sample: customer-accounts/person
	ada, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{
		FirstName:  "Ada",
		LastName:   "Obi",
		OtherNames: "Chiamaka", // optional
		Email:      "ada@example.com",
		Phone:      "+2348030000000",
		Country:    "NG",
		Currencies: []string{"NGN", "USD"},          // at least one
		ExternalID: "user_8812",                     // your user id: makes the call safe to retry
		MetaData:   map[string]any{"tier": "tier2"}, // stored on the identity and on each account
	})
	check(err)

	adaNGN := ada.Accounts["NGN"] // *ledger.Account
	fmt.Println("identity:", ada.ID)
	fmt.Println("NGN account:", adaNGN.ID, adaNGN.Kind, adaNGN.Status, "tier:", adaNGN.MetaData["tier"])
	fmt.Println("USD account:", ada.Accounts["USD"].ID, "precision:", ada.Accounts["USD"].Precision)
	// end
	end()

	begin("customer-accounts/business")
	// sample: customer-accounts/business
	shop, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{
		Type:             "organization",     // default is "individual"
		OrganizationName: "Chidi Stores Ltd", // required for organizations
		Email:            "accounts@chidistores.ng",
		Country:          "NG",
		Currencies:       []string{"NGN"},
		ExternalID:       "merchant_311",
	})
	check(err)
	fmt.Println(shop.Identity.IdentityType, shop.Identity.OrganizationName, shop.Accounts["NGN"].ID)
	// end
	end()

	begin("customer-accounts/retry")
	// sample: customer-accounts/retry
	// The sign-up timed out and the app tries again, now also asking for BTC.
	again, err := wallet.OpenCustomer(ctx, ledger.CustomerInput{
		FirstName: "Ada", LastName: "Obi",
		Currencies: []string{"NGN", "USD", "BTC"},
		ExternalID: "user_8812", // same user id as before
	})
	check(err)
	fmt.Println("same identity:", again.ID == ada.ID)
	fmt.Println("same NGN account:", again.Accounts["NGN"].ID == adaNGN.ID)
	fmt.Println("accounts now:", len(again.Accounts), "BTC precision:", again.Accounts["BTC"].Precision)
	// end
	end()

	begin("customer-accounts/existing-identity")
	// sample: customer-accounts/existing-identity
	usdt, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{
		Currency:   "USDT",
		IdentityID: ada.ID,      // link it to Ada
		ExternalID: "user_8812", // retry-safe per ExternalID and currency
	})
	check(err)
	fmt.Println(usdt.ID, usdt.Currency, "precision:", usdt.Precision, "Ada's:", usdt.IdentityID == ada.ID)
	// end
	end()

	begin("customer-accounts/no-identity")
	// sample: customer-accounts/no-identity
	float, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{
		Currency:   "NGN",
		ExternalID: "agent_float_17",
		MetaData:   map[string]any{"purpose": "agent float"},
	})
	check(err)
	retry, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN", ExternalID: "agent_float_17"})
	check(err)
	fmt.Println(float.ID, "no identity:", float.IdentityID == "", "retry returns it:", retry.ID == float.ID)
	// end
	end()

	begin("customer-accounts/other-ledger")
	// sample: customer-accounts/other-ledger
	gh, err := ops.CreateLedger(ctx, ledger.CreateLedgerInput{Key: "oja-gh", Name: "Oja Ghana"})
	check(err)

	kofi, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{
		Currency:   "GHS",
		LedgerID:   gh.ID, // overrides Config.LedgerID
		ExternalID: "user_9100",
	})
	check(err)
	fmt.Println(kofi.ID, "in", kofi.LedgerID == gh.ID)

	_, err = wallet.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "GHS", LedgerID: "ldg_doesnotexist"})
	fmt.Println("missing ledger:", errors.Is(err, ledger.ErrLedgerNotFound))
	// end
	end()

	begin("customer-accounts/pot")
	// sample: customer-accounts/pot
	rent, err := wallet.OpenPot(ctx, ledger.PotInput{
		ParentID: adaNGN.ID,
		Name:     "Rent",                         // unique per parent
		Goal:     ledger.Amount("600000", "NGN"), // optional
	})
	check(err)
	fmt.Println(rent.ID, rent.Kind, rent.Currency, "goal:", rent.Goal.Display, "Ada's:", rent.IdentityID == ada.ID)

	same, err := wallet.OpenPot(ctx, ledger.PotInput{ParentID: adaNGN.ID, Name: "Rent"})
	check(err)
	fmt.Println("same name returns the same pot:", same.ID == rent.ID)
	// end
	end()

	fund(wallet, adaNGN.ID, "100000", "NGN", "seed_ada")

	begin("customer-accounts/escrow")
	// sample: customer-accounts/escrow
	esc, err := wallet.OpenEscrow(ctx, ledger.EscrowInput{
		Payer:       adaNGN.ID,
		Beneficiary: shop.Accounts["NGN"].ID,
		Amount:      ledger.Amount("80000", "NGN"),
		Reference:   "escrow_10450", // a retry returns the same escrow
	})
	check(err)
	fmt.Println(esc.ID, esc.Status, "held:", esc.Held.Display, esc.Held.Currency)
	// end
	end()

	begin("customer-accounts/read")
	// sample: customer-accounts/read
	c, err := wallet.GetCustomer(ctx, ada.ID) // main accounts by currency; no pots or escrows
	check(err)
	fmt.Println("currencies:", len(c.Accounts))

	acct, err := wallet.GetAccount(ctx, adaNGN.ID)
	check(err)
	fmt.Println("balance:", acct.Balance.Display, "available:", acct.Available.Display)

	byUser, err := wallet.FindAccounts(ctx, ledger.AccountQuery{MetaData: map[string]string{"external_id": "user_8812"}})
	check(err)
	fmt.Println("accounts for user_8812:", byUser.Total)

	pots, err := wallet.FindAccounts(ctx, ledger.AccountQuery{IdentityID: ada.ID, Kind: ledger.PotAccount})
	check(err)
	fmt.Println("Ada's pots:", len(pots.Items))
	// end
	end()

	begin("customer-accounts/errors")
	// sample: customer-accounts/errors
	_, err = wallet.OpenCustomer(ctx, ledger.CustomerInput{FirstName: "Bola"})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.OpenCustomer(ctx, ledger.CustomerInput{Type: "organization", Currencies: []string{"NGN"}})
	fmt.Println(errors.Is(err, ledger.ErrInvalidInput), err)

	_, err = wallet.OpenPot(ctx, ledger.PotInput{ParentID: ledger.AccountID(ledger.FeeIncomeAccount), Name: "Fees"})
	fmt.Println(errors.Is(err, ledger.ErrWrongKind))
	// end
	end()

	begin("customer-accounts/reserved")
	// sample: customer-accounts/reserved
	acc, err := wallet.OpenAccount(ctx, ledger.OpenAccountInput{
		Currency: "NGN",
		MetaData: map[string]any{"status": "frozen", "account_kind": "pot", "branch": "Ikeja"},
	})
	check(err)
	fmt.Println(acc.Status, acc.Kind, acc.MetaData["branch"])
	// end
	end()
}
