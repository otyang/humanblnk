package ledger_test

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/otyang/humanblnk/currency"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

func TestServiceCurrencies(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	if got, want := len(svc.Currencies()), len(currency.All()); got != want {
		t.Fatalf("Currencies() = %d, want %d", got, want)
	}
	if got := svc.Currencies(currency.Stablecoin); len(got) != 3 || got[0].Code != currency.DAI {
		t.Errorf("stablecoins: %+v", got)
	}
	if c, ok := svc.Currency("ngn"); !ok || c != currency.MustGet(currency.NGN) {
		t.Errorf("NGN: %+v %v", c, ok)
	}
	if err := svc.Check(ctx); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestConfigCurrencies(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Currencies: []currency.Currency{
		currency.MustGet(currency.ETH).WithDisplay(currency.Display{Decimals: 2, TrimZeros: true}),
		currency.MustGet(currency.NGN).WithName("Naira").WithDisplay(currency.Display{Decimals: 0}),
		currency.New("oja", "Oja Points", currency.Fiat, "pts", 1000),
		{Code: "GEM", Precision: 100}, // a bare literal: Name, Symbol and Kind are filled in
	}})
	eth, _ := svc.Currency(currency.ETH)
	if eth.Precision != 1e18 || eth.Name != "Ether" || eth.Display != (currency.Display{Decimals: 2, TrimZeros: true}) {
		t.Errorf("ETH: %+v", eth)
	}
	if ngn, _ := svc.Currency(currency.NGN); ngn.Name != "Naira" || ngn.Symbol != "₦" {
		t.Errorf("NGN: %+v", ngn)
	}
	if gem, ok := svc.Currency("GEM"); !ok || gem.Name != "GEM" || gem.Symbol != "GEM" || gem.Kind != currency.Fiat {
		t.Errorf("GEM: %+v %v", gem, ok)
	}
	if currency.MustGet(currency.NGN).Name != "Nigerian Naira" {
		t.Error("Config.Currencies changed the built-in list")
	}
	if m, err := svc.Parse("1250.75", currency.NGN); err != nil || m.Display != "1250" {
		t.Errorf("whole naira: %q %v", m.Display, err)
	}
	if m, err := svc.Parse("0.1234", currency.ETH); err != nil || m.Display != "0.12" {
		t.Errorf("ETH display: %q %v", m.Display, err)
	}
	acct, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "oja"})
	if err != nil || acct.Precision != 1000 || acct.Balance.Display != "0.000" {
		t.Errorf("OJA account: %+v %v", acct, err)
	}
	if err := svc.Check(ctx); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestUnknownCurrency(t *testing.T) {
	svc, core := ledgertest.New(t, ledger.Config{})
	w := svc.Wallet()
	if _, err := w.OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGM"}); !errors.Is(err, ledger.ErrInvalidInput) || !strings.Contains(err.Error(), `"NGM"`) {
		t.Errorf("OpenAccount: %v", err)
	}
	if _, err := w.OpenCustomer(ctx, ledger.CustomerInput{FirstName: "Ada", Currencies: []string{currency.NGN, "NGM"}, ExternalID: "u1"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("OpenCustomer: %v", err)
	}
	if page, err := w.FindAccounts(ctx, ledger.AccountQuery{MetaData: map[string]string{"external_id": "u1"}}); err != nil || page.Total != 0 {
		t.Errorf("OpenCustomer opened accounts before refusing: %+v %v", page, err)
	}
	if _, err := svc.Parse("1", "NGM"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("Parse: %v", err)
	}
	ada := open(t, svc, currency.NGN)
	if _, err := w.Deposit(ctx, ledger.DepositInput{AccountID: ada.ID, Amount: ledger.Amount("1", "NGM"), Fee: ledger.NoFee, Reference: "d1"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("Deposit: %v", err)
	}

	allow := ledger.New(core.Client(), ledger.Config{LedgerID: ada.LedgerID, UnknownCurrency: ledger.AllowUnknown, DefaultPrecision: 1000})
	acct, err := allow.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGM"})
	if err != nil || acct.Precision != 1000 || acct.Balance.Display != "0.000" {
		t.Errorf("AllowUnknown: %+v %v", acct, err)
	}
}

func TestCurrencyConfigMistakes(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Currencies: []currency.Currency{
		{Code: " ", Precision: 100},
		{Code: "GEM"}, // no precision
		{Code: "NGN", Precision: 250},
		{Code: "BTC", Precision: 1e8, Kind: "metal"},
		{Code: "ETH", Precision: 1e18, Display: currency.Display{Decimals: -1}},
		currency.MustGet(currency.SOL).WithDisplay(currency.Display{Decimals: 2}), // fine
		currency.MustGet(currency.SOL),                                            // twice
	}})
	err := svc.Check(ctx)
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("Check: %v", err)
	}
	for _, want := range []string{"[0]", "GEM", "NGN", "BTC", "ETH", "[6] SOL: listed twice"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check doesn't name %s: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "[5]") {
		t.Errorf("Check names a good entry: %v", err)
	}
	// Bad entries are left out; good ones apply.
	if _, ok := svc.Currency("GEM"); ok {
		t.Error("GEM was added without a precision")
	}
	if svc.Precision(currency.NGN) != 100 || svc.DisplayRule(currency.SOL) != (currency.Display{Decimals: 2}) {
		t.Errorf("NGN %d, SOL %+v", svc.Precision(currency.NGN), svc.DisplayRule(currency.SOL))
	}
}

// Precise has no Service, so its Display follows the built-in list.
func TestPreciseUsesBuiltinDisplay(t *testing.T) {
	if got := ledger.Precise(big.NewInt(150_000_000), 100_000_000, "btc").Display; got != "1.5" {
		t.Errorf("BTC: %q", got)
	}
	if got := ledger.Precise(big.NewInt(150), 100, currency.NGN).Display; got != "1.50" {
		t.Errorf("NGN: %q", got)
	}
	if got := ledger.Precise(big.NewInt(150), 100, currency.NGN).Format(currency.Display{Decimals: 1}); got != "1.5" {
		t.Errorf("Money.Format: %q", got)
	}
}
