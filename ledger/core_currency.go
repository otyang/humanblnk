package ledger

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/currency"
)

// UnknownCurrencyPolicy says what happens to a currency that is neither built
// in (see package currency) nor in Config.Currencies.
type UnknownCurrencyPolicy int

const (
	// RefuseUnknown (the default) refuses unknown currencies with
	// ErrInvalidInput when opening accounts, parsing amounts and moving
	// money, so a typo such as "NGM" can't open an account.
	RefuseUnknown UnknownCurrencyPolicy = iota
	// AllowUnknown accepts any code, with Config.DefaultPrecision and every
	// decimal place of it shown, padded.
	AllowUnknown
)

// buildCurrencies puts cfg.Currencies over the built-in list; each entry
// replaces the whole currency. A code whose entry has a mistake is broken:
// it is refused everywhere (not quietly run on the built-in entry, whose
// precision may be the very thing the entry meant to change) until the
// entry is fixed. Every mistake is reported in the error, which
// Service.Check returns.
func buildCurrencies(cfg []currency.Currency) (map[string]currency.Currency, map[string]error, error) {
	all := currency.All()
	out := make(map[string]currency.Currency, len(all)+len(cfg))
	for _, c := range all {
		out[c.Code] = c
	}
	var errs []error
	broken := map[string]error{}
	seen := map[string]bool{}
	for i, c := range cfg {
		c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
		bad := func(format string, a ...any) {
			err := fmt.Errorf("Config.Currencies[%d] %s: "+format, append([]any{i, c.Code}, a...)...)
			errs = append(errs, err)
			if c.Code != "" {
				broken[c.Code] = err
				delete(out, c.Code)
			}
		}
		switch _, pow10 := decimalPlaces(c.Precision); {
		case c.Code == "":
			bad("Code is required")
			continue
		case seen[c.Code]:
			bad("listed twice")
			continue
		case c.Precision <= 0:
			bad("Precision is required (100 for 2 decimal places)")
			continue
		case !pow10:
			bad("Precision %d is not a power of ten", c.Precision)
			continue
		case c.Display.Decimals < 0:
			bad("Display.Decimals %d is negative", c.Display.Decimals)
			continue
		}
		c.Kind = currency.Kind(strings.ToLower(strings.TrimSpace(string(c.Kind))))
		switch c.Kind {
		case "":
			c.Kind = currency.Fiat
		case currency.Fiat, currency.Crypto, currency.Stablecoin:
		default:
			bad("unknown Kind %q", c.Kind)
			continue
		}
		if c.Name == "" {
			c.Name = c.Code
		}
		if c.Symbol == "" {
			c.Symbol = c.Code
		}
		seen[c.Code] = true
		if broken[c.Code] == nil {
			out[c.Code] = c
		}
	}
	return out, broken, errors.Join(errs...)
}

// Currency returns the currency for code (any case): built in, or as changed
// or added by Config.Currencies. ok is false for an unknown code.
func (s *Service) Currency(code string) (currency.Currency, bool) {
	c, ok := s.currencies[strings.ToUpper(strings.TrimSpace(code))]
	return c, ok
}

// Currencies returns the currencies this Service knows, of the given kinds
// (all of them when none are given), sorted by code.
func (s *Service) Currencies(kinds ...currency.Kind) []currency.Currency {
	out := make([]currency.Currency, 0, len(s.currencies))
	for _, c := range s.currencies {
		if len(kinds) == 0 || containsKind(kinds, c.Kind) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func containsKind(kinds []currency.Kind, k currency.Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Precision returns the precision used for currency: its registry entry, or
// Config.DefaultPrecision for an unknown code.
func (s *Service) Precision(code string) int64 {
	if c, ok := s.Currency(code); ok {
		return c.Precision
	}
	return s.cfg.DefaultPrecision
}

// DisplayRule returns the rule used for currency: its registry entry, or every
// decimal place of its precision, padded, for an unknown code.
func (s *Service) DisplayRule(code string) currency.Display {
	if c, ok := s.Currency(code); ok {
		return c.Display
	}
	dp, _ := decimalPlaces(s.cfg.DefaultPrecision)
	return currency.Display{Decimals: dp}
}

// knownCurrency refuses a currency whose Config.Currencies entry has a
// mistake, and an unknown currency unless Config.UnknownCurrency is
// AllowUnknown.
func (s *Service) knownCurrency(op, code string) error {
	if err := s.broken[strings.ToUpper(strings.TrimSpace(code))]; err != nil {
		return &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	if s.cfg.UnknownCurrency == AllowUnknown {
		return nil
	}
	if _, ok := s.Currency(code); ok {
		return nil
	}
	return invalid(op, "unknown currency %q: add it to Config.Currencies", strings.ToUpper(strings.TrimSpace(code)))
}

// builtinRule is the display rule without a Service: the built-in one,
// else every decimal place of the precision, padded.
func builtinRule(code string, precision int64) currency.Display {
	if c, ok := currency.Get(code); ok {
		return c.Display
	}
	dp, _ := decimalPlaces(precision)
	return currency.Display{Decimals: dp}
}

// metaPrecision records on each account the precision it was opened with.
const metaPrecision = "precision"

// precisionOf is the precision b's amounts are held at: the one recorded
// when it was opened, else (house accounts, accounts from before the
// record) the currency's.
func (s *Service) precisionOf(b *blnk.Balance) int64 {
	if p, err := strconv.ParseInt(str(b.MetaData[metaPrecision]), 10, 64); err == nil && p > 0 {
		return p
	}
	return s.Precision(b.Currency)
}

// samePrecision refuses to move money on an account opened at a different
// precision from the one this Service now uses for its currency: Blnk sums
// precise amounts whatever their precision, so mixing them corrupts the
// balance.
func (s *Service) samePrecision(op string, b *blnk.Balance) error {
	have, now := s.precisionOf(b), s.Precision(b.Currency)
	if have == now {
		return nil
	}
	return &OpError{Op: op, Kind: ErrInvalidInput, Cause: fmt.Errorf(
		"account %s holds %s at precision %d but this service uses %d; set its precision back to %d in Config.Currencies",
		b.BalanceID, strings.ToUpper(b.Currency), have, now, have)}
}
