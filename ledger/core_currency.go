package ledger

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CurrencyKind groups currencies: fiat money, crypto currencies, and
// stablecoins (crypto that tracks a fiat currency and reads like money).
type CurrencyKind string

const (
	Fiat       CurrencyKind = "fiat"
	Crypto     CurrencyKind = "crypto"
	Stablecoin CurrencyKind = "stablecoin"
)

// Currency is what humanblnk knows about a currency: its name and symbol for
// people, its precision for Blnk and how its amounts are displayed.
//
// The built-in list is Currencies. Config.Currencies changes entries or adds
// new ones; read the result with Service.Currency and Service.Currencies.
type Currency struct {
	Code   string       // "NGN", "BTC"
	Name   string       // "Nigerian Naira"
	Kind   CurrencyKind // Fiat, Crypto or Stablecoin
	Symbol string       // "₦"; the code itself when there is no common symbol

	// Precision is the smallest-unit multiplier, a power of ten: 100 for
	// NGN (kobo), 100000000 for BTC (satoshi). Keep it fixed once money has
	// moved: Blnk sums precise_amount on balances, so mixing precisions
	// corrupts the totals.
	Precision int64

	// Display says how amounts are shown to people (Money.Display).
	Display DisplayRule
}

// UnknownCurrencyPolicy says what happens to a currency that is neither built
// in nor in Config.Currencies.
type UnknownCurrencyPolicy int

const (
	// RefuseUnknown (the default) refuses unknown currencies with
	// ErrInvalidInput when opening accounts and parsing amounts, so a typo
	// such as "NGM" can't open an account.
	RefuseUnknown UnknownCurrencyPolicy = iota
	// AllowUnknown accepts any code, with Config.DefaultPrecision and every
	// decimal place of it shown, padded.
	AllowUnknown
)

// buildCurrencies merges cfg.Currencies onto the built-in list. Entries with
// mistakes are skipped and reported in the error, which Service.Check returns.
func buildCurrencies(cfg []Currency) (map[string]Currency, error) {
	out := make(map[string]Currency, len(Currencies)+len(cfg))
	for k, c := range Currencies {
		out[k] = c
	}
	var errs []error
	for i, c := range cfg {
		code := strings.ToUpper(strings.TrimSpace(c.Code))
		bad := func(format string, a ...any) {
			errs = append(errs, fmt.Errorf("Config.Currencies[%d] %s: "+format, append([]any{i, code}, a...)...))
		}
		if code == "" {
			bad("Code is required")
			continue
		}
		cur, known := out[code]
		if !known {
			cur = Currency{Code: code, Name: code, Kind: Fiat, Symbol: code}
		}
		switch {
		case c.Precision < 0:
			bad("Precision %d is negative", c.Precision)
			continue
		case c.Precision == 0 && !known:
			bad("Precision is required for a currency that isn't built in")
			continue
		case c.Precision > 0:
			if _, ok := decimalPlaces(c.Precision); !ok {
				bad("Precision %d is not a power of ten", c.Precision)
				continue
			}
		}
		switch c.Kind {
		case "", Fiat, Crypto, Stablecoin:
		default:
			bad("unknown Kind %q", c.Kind)
			continue
		}
		if c.Display.Decimals < 0 {
			bad("Display.Decimals %d is negative", c.Display.Decimals)
			continue
		}
		if c.Name != "" {
			cur.Name = c.Name
		}
		if c.Kind != "" {
			cur.Kind = c.Kind
		}
		if c.Symbol != "" {
			cur.Symbol = c.Symbol
		}
		if c.Precision > 0 {
			cur.Precision = c.Precision
		}
		switch {
		case c.Display != (DisplayRule{}):
			cur.Display = c.Display
		case !known:
			dp, _ := decimalPlaces(cur.Precision)
			cur.Display = DisplayRule{Decimals: dp}
		}
		out[code] = cur
	}
	return out, errors.Join(errs...)
}

// Currency returns the currency for code (any case): built in, or as changed
// or added by Config.Currencies. ok is false for an unknown code.
func (s *Service) Currency(code string) (Currency, bool) {
	c, ok := s.currencies[strings.ToUpper(strings.TrimSpace(code))]
	return c, ok
}

// Currencies returns the known currencies of the given kinds (all of them
// when none are given), sorted by code.
func (s *Service) Currencies(kinds ...CurrencyKind) []Currency {
	out := make([]Currency, 0, len(s.currencies))
	for _, c := range s.currencies {
		if len(kinds) == 0 || containsKind(kinds, c.Kind) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func containsKind(kinds []CurrencyKind, k CurrencyKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Precision returns the precision used for currency: its registry entry, or
// Config.DefaultPrecision for an unknown code.
func (s *Service) Precision(currency string) int64 {
	if c, ok := s.Currency(currency); ok {
		return c.Precision
	}
	return s.cfg.DefaultPrecision
}

// DisplayRule returns the rule used for currency: its registry entry, or every
// decimal place of its precision, padded, for an unknown code.
func (s *Service) DisplayRule(currency string) DisplayRule {
	if c, ok := s.Currency(currency); ok {
		return c.Display
	}
	dp, _ := decimalPlaces(s.cfg.DefaultPrecision)
	return DisplayRule{Decimals: dp}
}

// knownCurrency refuses an unknown currency unless Config.UnknownCurrency is
// AllowUnknown.
func (s *Service) knownCurrency(op, currency string) error {
	if s.cfg.UnknownCurrency == AllowUnknown {
		return nil
	}
	if _, ok := s.Currency(currency); ok {
		return nil
	}
	return invalid(op, "unknown currency %q: add it to Config.Currencies", strings.ToUpper(strings.TrimSpace(currency)))
}

// builtinRule is the display rule without a Service: the built-in entry,
// then every decimal place of the precision.
func builtinRule(currency string, precision int64) DisplayRule {
	if c, ok := Currencies[currency]; ok {
		return c.Display
	}
	dp, _ := decimalPlaces(precision)
	return DisplayRule{Decimals: dp}
}
