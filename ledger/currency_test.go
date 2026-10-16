package ledger_test

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/ledgertest"
)

func TestBuiltinCurrencies(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{})
	for code, want := range map[string]struct {
		precision int64
		kind      ledger.CurrencyKind
		shown     string // 1.23456789 cut by the display rule
	}{
		"NGN": {100, ledger.Fiat, "1.23"}, "JPY": {1, ledger.Fiat, "1"}, "KWD": {1000, ledger.Fiat, "1.234"},
		"IDR": {100, ledger.Fiat, "1.23"}, "XOF": {1, ledger.Fiat, "1"},
		"BTC": {1e8, ledger.Crypto, "1.23456789"}, "ETH": {1e18, ledger.Crypto, "1.234567"},
		"SOL": {1e9, ledger.Crypto, "1.2345"}, "USDT": {1e6, ledger.Stablecoin, "1.23"},
	} {
		c, ok := svc.Currency(strings.ToLower(code))
		if !ok || c.Code != code || c.Precision != want.precision || c.Kind != want.kind || c.Name == "" || c.Symbol == "" {
			t.Errorf("%s: %+v %v", code, c, ok)
			continue
		}
		m, err := ledger.ParseDecimal("1.23456789", code, 1e18)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Display.Format(m); got != want.shown {
			t.Errorf("%s shows %s, want %s", code, got, want.shown)
		}
	}
	all := svc.Currencies()
	if len(all) != len(ledger.Currencies) {
		t.Fatalf("Currencies() = %d, want %d", len(all), len(ledger.Currencies))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Code >= all[i].Code {
			t.Fatalf("not sorted at %s", all[i].Code)
		}
	}
	for _, c := range svc.Currencies(ledger.Crypto, ledger.Stablecoin) {
		if c.Kind == ledger.Fiat {
			t.Errorf("%s in the crypto filter", c.Code)
		}
	}
	if err := svc.Check(ctx); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestConfigCurrencies(t *testing.T) {
	svc, _ := ledgertest.New(t, ledger.Config{Currencies: []ledger.Currency{
		{Code: "eth", Display: ledger.DisplayRule{Decimals: 2, TrimZeros: true}},
		{Code: "NGN", Name: "Naira"},
		{Code: "OJA", Precision: 1000, Symbol: "pts"},
		{Code: "JPY", Display: ledger.DisplayRule{Decimals: 0, TrimZeros: true}},
	}})
	eth, _ := svc.Currency("ETH")
	if eth.Precision != 1e18 || eth.Name != "Ether" || eth.Display != (ledger.DisplayRule{Decimals: 2, TrimZeros: true}) {
		t.Errorf("ETH override changed more than Display: %+v", eth)
	}
	ngn, _ := svc.Currency("NGN")
	if ngn.Name != "Naira" || ngn.Symbol != "₦" || ngn.Display != (ledger.DisplayRule{Decimals: 2}) {
		t.Errorf("NGN: %+v", ngn)
	}
	oja, ok := svc.Currency("OJA")
	if !ok || oja.Kind != ledger.Fiat || oja.Name != "OJA" || oja.Symbol != "pts" || oja.Display != (ledger.DisplayRule{Decimals: 3}) {
		t.Errorf("OJA: %+v %v", oja, ok)
	}
	if ledger.Currencies["NGN"].Name != "Nigerian Naira" || ledger.Currencies["OJA"].Code != "" {
		t.Error("Config.Currencies changed the built-in list")
	}
	m, err := svc.Parse("0.1234", "ETH")
	if err != nil || m.Display != "0.12" {
		t.Errorf("ETH display: %q %v", m.Display, err)
	}
	if acct, err := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "oja"}); err != nil || acct.Precision != 1000 || acct.Balance.Display != "0.000" {
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
	if _, err := w.OpenCustomer(ctx, ledger.CustomerInput{FirstName: "Ada", Currencies: []string{"NGN", "NGM"}, ExternalID: "u1"}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("OpenCustomer: %v", err)
	}
	if page, err := w.FindAccounts(ctx, ledger.AccountQuery{MetaData: map[string]string{"external_id": "u1"}}); err != nil || page.Total != 0 {
		t.Errorf("OpenCustomer opened accounts before refusing: %+v %v", page, err)
	}
	if _, err := svc.Parse("1", "NGM"); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("Parse: %v", err)
	}
	ada := open(t, svc, "NGN")
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
	svc, _ := ledgertest.New(t, ledger.Config{Currencies: []ledger.Currency{
		{Code: " "},
		{Code: "GEM"},
		{Code: "NGN", Precision: 250},
		{Code: "USD", Precision: -100},
		{Code: "BTC", Kind: "metal"},
		{Code: "ETH", Display: ledger.DisplayRule{Decimals: -1}},
		{Code: "SOL", Display: ledger.DisplayRule{Decimals: 2}}, // fine
	}})
	err := svc.Check(ctx)
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("Check: %v", err)
	}
	for _, want := range []string{"[0]", "GEM", "NGN", "USD", "BTC", "ETH"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check doesn't name %s: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "SOL") {
		t.Errorf("Check names a good entry: %v", err)
	}
	// Bad entries are left out; good ones apply.
	if _, ok := svc.Currency("GEM"); ok {
		t.Error("GEM was added without a precision")
	}
	if svc.Precision("NGN") != 100 || svc.DisplayRule("SOL") != (ledger.DisplayRule{Decimals: 2}) {
		t.Errorf("NGN %d, SOL %+v", svc.Precision("NGN"), svc.DisplayRule("SOL"))
	}
}

// Precise has no Service, so its Display follows the built-in list.
func TestPreciseUsesBuiltinDisplay(t *testing.T) {
	if got := ledger.Precise(big.NewInt(150_000_000), 100_000_000, "btc").Display; got != "1.5" {
		t.Errorf("BTC: %q", got)
	}
	if got := ledger.Precise(big.NewInt(150), 100, "NGN").Display; got != "1.50" {
		t.Errorf("NGN: %q", got)
	}
}
