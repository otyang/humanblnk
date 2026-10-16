package currency_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/otyang/humanblnk/currency"
)

func TestBuiltin(t *testing.T) {
	for code, want := range map[string]struct {
		precision int64
		kind      currency.Kind
		symbol    string
		shown     string // 1.23456789 cut by the display rule
	}{
		currency.NGN: {100, currency.Fiat, "₦", "1.23"}, currency.JPY: {1, currency.Fiat, "¥", "1"},
		currency.KWD: {1000, currency.Fiat, "KWD", "1.234"}, currency.XOF: {1, currency.Fiat, "F\u202fCFA", "1"},
		currency.USD: {100, currency.Fiat, "$", "1.23"}, currency.AUD: {100, currency.Fiat, "A$", "1.23"},
		currency.BTC: {1e8, currency.Crypto, "₿", "1.23456789"}, currency.ETH: {1e18, currency.Crypto, "Ξ", "1.234567"},
		currency.SOL: {1e9, currency.Crypto, "SOL", "1.2345"}, currency.USDT: {1e6, currency.Stablecoin, "USDT", "1.23"},
	} {
		c, ok := currency.Get(strings.ToLower(code))
		if !ok || c.Code != code || c.Precision != want.precision || c.Kind != want.kind || c.Symbol != want.symbol || c.Name == "" {
			t.Errorf("%s: %+v %v", code, c, ok)
			continue
		}
		units, _ := new(big.Int).SetString("1234567890000000000", 10) // 1.23456789 at 1e18
		units.Quo(units, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-c.Decimals())), nil))
		if got := c.Display.Format(units, c.Precision); got != want.shown {
			t.Errorf("%s shows %s, want %s", code, got, want.shown)
		}
	}
	if n := currency.MustGet(currency.ETH).Decimals(); n != 18 {
		t.Errorf("ETH decimals %d", n)
	}
}

func TestList(t *testing.T) {
	all := currency.All()
	if len(all) != 179 || len(currency.All(currency.Fiat)) != 165 {
		t.Fatalf("%d currencies, %d fiat", len(all), len(currency.All(currency.Fiat)))
	}
	symbols := map[string]string{}
	for i, c := range all {
		if i > 0 && all[i-1].Code >= c.Code {
			t.Fatalf("not sorted at %s", c.Code)
		}
		if other, dup := symbols[c.Symbol]; dup {
			t.Errorf("%s and %s share the symbol %s", other, c.Code, c.Symbol)
		}
		symbols[c.Symbol] = c.Code
	}
	for _, c := range currency.All(currency.Crypto, currency.Stablecoin) {
		if c.Kind == currency.Fiat {
			t.Errorf("%s in the crypto filter", c.Code)
		}
	}
}

func TestGetReturnsACopy(t *testing.T) {
	c := currency.MustGet(currency.NGN)
	c.Precision = 1000
	c.Display.Decimals = 0
	if again := currency.MustGet(currency.NGN); again.Precision != 100 || again.Display.Decimals != 2 {
		t.Fatalf("changing a copy changed the list: %+v", again)
	}
	if _, ok := currency.Get("NGM"); ok {
		t.Fatal("NGM is built in")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustGet(NGM) didn't panic")
		}
	}()
	currency.MustGet("NGM")
}

func TestNewAndWith(t *testing.T) {
	oja := currency.New(" oja ", "Oja Points", currency.Fiat, "pts", 1000)
	if oja.Code != "OJA" || oja.Display != (currency.Display{Decimals: 3}) {
		t.Errorf("fiat: %+v", oja)
	}
	gem := currency.New("GEM", "Gem", currency.Crypto, "GEM", 1e6)
	if gem.Display != (currency.Display{Decimals: 6, TrimZeros: true}) {
		t.Errorf("crypto: %+v", gem)
	}
	eth := currency.MustGet(currency.ETH).WithName("Ethereum").WithSymbol("ETH").WithDisplay(currency.Display{Decimals: 2})
	if eth.Name != "Ethereum" || eth.Symbol != "ETH" || eth.Display.Decimals != 2 || eth.Precision != 1e18 {
		t.Errorf("With: %+v", eth)
	}
}

func TestFormat(t *testing.T) {
	for _, c := range []struct {
		units     int64
		precision int64
		d         currency.Display
		want      string
	}{
		{1050075, 100, currency.Display{Decimals: 2}, "10500.75"},
		{1050075, 100, currency.Display{Decimals: 0}, "10500"},
		{1050000, 100, currency.Display{Decimals: 2, TrimZeros: true}, "10500"},
		{-5, 100, currency.Display{Decimals: 2}, "-0.05"},
		{-5, 100, currency.Display{Decimals: 1}, "0.0"}, // no "-0.0"
		{5, 1, currency.Display{Decimals: 2}, "5"},      // capped at the precision
		{5, 250, currency.Display{Decimals: 2}, "5"},    // not a power of ten
	} {
		if got := c.d.Format(big.NewInt(c.units), c.precision); got != c.want {
			t.Errorf("%d at %d by %+v = %s, want %s", c.units, c.precision, c.d, got, c.want)
		}
	}
	if got := (currency.Display{Decimals: 2}).Format(nil, 100); got != "0.00" {
		t.Errorf("nil: %s", got)
	}
}
