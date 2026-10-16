// Package currency defines the currencies humanblnk knows without
// configuration: every current ISO 4217 code, plus major crypto currencies
// and stablecoins.
//
// Each currency has a constant holding its code. The constants are plain
// strings, so they work wherever humanblnk takes a currency, and a typo is a
// compile error rather than a refused call:
//
//	ledger.Amount("5000", currency.NGN)
//	wallet.OpenCustomer(ctx, ledger.CustomerInput{
//		Currencies: []string{currency.NGN, currency.USD, currency.USDT},
//	})
//
// Get returns a currency's details. The built-in list can't be changed: Get
// returns a copy.
//
//	ngn, _ := currency.Get(currency.NGN)
//	ngn.Name       // "Nigerian Naira"
//	ngn.Symbol     // "₦"
//	ngn.Precision  // 100: amounts are kept in kobo
//	ngn.Decimals() // 2
//
// To change a currency for your service, or add your own, pass it in
// ledger.Config.Currencies; Service.Currency then returns what your service
// uses, which can differ from Get.
package currency

import (
	"math/big"
	"sort"
	"strings"
)

// Kind groups currencies: fiat money, crypto currencies, and stablecoins
// (crypto that tracks a fiat currency and reads like money).
type Kind string

const (
	Fiat       Kind = "fiat"
	Crypto     Kind = "crypto"
	Stablecoin Kind = "stablecoin"
)

// Currency is what humanblnk knows about a currency.
type Currency struct {
	Code   string `json:"code"`   // "NGN"
	Name   string `json:"name"`   // "Nigerian Naira"
	Kind   Kind   `json:"kind"`   // Fiat, Crypto or Stablecoin
	Symbol string `json:"symbol"` // "₦"; the code when there is no symbol of its own

	// Precision is the number of smallest units in one whole unit, a power
	// of ten: 100 for NGN (kobo), 1 for JPY, 100000000 for BTC (satoshi).
	// Blnk stores amounts as integers of this unit. Keep it fixed once money
	// has moved: Blnk sums precise_amount on balances, so mixing precisions
	// corrupts the totals.
	Precision int64 `json:"precision"`

	// Display says how amounts are shown to people.
	Display Display `json:"display"`
}

// Display says how amounts are shown to people. Amounts are cut (never
// rounded up) to Decimals places, so a displayed balance can always be spent
// in full.
//
// Fiat style pads to a fixed number of places ("100.00"); crypto style trims
// trailing zeros ("0.5", "0.456789").
type Display struct {
	Decimals  int  `json:"decimals"`   // most decimal places shown; capped at the precision's
	TrimZeros bool `json:"trim_zeros"` // drop trailing zeros instead of padding to Decimals
}

// New builds a currency that isn't built in. It is shown with every decimal
// place its precision has, padded for Fiat and trimmed otherwise. Change
// that with WithDisplay.
func New(code, name string, kind Kind, symbol string, precision int64) Currency {
	dp, _ := decimalPlaces(precision)
	return Currency{
		Code: strings.ToUpper(strings.TrimSpace(code)), Name: name, Kind: kind, Symbol: symbol, Precision: precision,
		Display: Display{Decimals: dp, TrimZeros: kind != Fiat},
	}
}

// Decimals is the number of decimal places in the smallest unit (ISO 4217
// "minor units"): NGN 2, JPY 0, KWD 3, BTC 8, ETH 18. It is 0 when Precision
// isn't a power of ten.
func (c Currency) Decimals() int {
	dp, _ := decimalPlaces(c.Precision)
	return dp
}

// WithName returns a copy of c with Name set.
func (c Currency) WithName(name string) Currency { c.Name = name; return c }

// WithSymbol returns a copy of c with Symbol set.
func (c Currency) WithSymbol(symbol string) Currency { c.Symbol = symbol; return c }

// WithDisplay returns a copy of c with Display set.
func (c Currency) WithDisplay(d Display) Currency { c.Display = d; return c }

// Get returns a copy of the built-in currency for code, in any case.
func Get(code string) (Currency, bool) {
	c, ok := builtin[strings.ToUpper(strings.TrimSpace(code))]
	return c, ok
}

// MustGet is Get for codes known to be built in, such as the constants in
// this package. It panics for any other code.
func MustGet(code string) Currency {
	c, ok := Get(code)
	if !ok {
		panic("currency: " + code + " is not built in")
	}
	return c
}

// All returns the built-in currencies of the given kinds (all of them when
// none are given), sorted by code.
func All(kinds ...Kind) []Currency {
	out := make([]Currency, 0, len(builtin))
	for _, c := range builtin {
		if len(kinds) == 0 || c.Kind.in(kinds) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func (k Kind) in(kinds []Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Format renders an amount of units (smallest units at precision, e.g. kobo
// at 100) by this rule: Format(1050075, 100) is "10500.75" for Decimals 2.
// A precision that isn't a power of ten shows the plain integer.
func (d Display) Format(units *big.Int, precision int64) string {
	if units == nil {
		units = new(big.Int)
	}
	dp, ok := decimalPlaces(precision)
	if !ok {
		return units.String()
	}
	keep := d.Decimals
	if keep < 0 {
		keep = 0
	}
	if keep > dp {
		keep = dp
	}
	// Cut toward zero to keep decimals: divide by 10^(dp-keep).
	cut := new(big.Int).Quo(units, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dp-keep)), nil))
	neg := cut.Sign() < 0
	digits := new(big.Int).Abs(cut).String()
	if keep > 0 {
		if len(digits) <= keep {
			digits = strings.Repeat("0", keep-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-keep] + "." + digits[len(digits)-keep:]
	}
	if d.TrimZeros && strings.Contains(digits, ".") {
		digits = strings.TrimRight(strings.TrimRight(digits, "0"), ".")
	}
	if neg && strings.Trim(digits, "0.") != "" {
		digits = "-" + digits // no "-0.00"
	}
	return digits
}

// decimalPlaces returns n for precision 10^n.
func decimalPlaces(precision int64) (int, bool) {
	if precision < 1 {
		return 0, false
	}
	n := 0
	for p := precision; p > 1; p /= 10 {
		if p%10 != 0 {
			return 0, false
		}
		n++
	}
	return n, true
}
