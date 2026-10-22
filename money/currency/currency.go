// Package currency is every currency HumanBlnk can hold: the ISO 4217
// currencies in circulation and the crypto assets HumanBlnk supports, each with
// the number of decimal places its smallest unit has.
//
// The list is generated (list.go, from currencies.csv): a code that is not
// on it is refused, never given a guessed precision. Add puts a currency of
// your own on it at run time, for the life of the process. A currency's
// decimals must never change once money has moved in it, since the ledger
// stores amounts in its smallest unit.
//
// Each code is a constant (currency.USD), so a misspelt code does not
// compile.
package currency

//go:generate go run ./gen

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Kind says whether a currency is a government currency or a crypto asset.
type Kind string

const (
	// Fiat is an ISO 4217 currency.
	Fiat Kind = "fiat"
	// Crypto is a crypto asset or stablecoin.
	Crypto Kind = "crypto"
)

// Currency is one currency of the list. Its fields are unexported, so the
// only Currency there is comes from Add, Lookup, Get, MustGet or All (or is
// the zero Currency, which is not Valid): one with made-up decimals cannot
// be built.
type Currency struct {
	code     string
	name     string
	kind     Kind
	decimals int32
}

// Code is the currency's code, as stored and sent ("USD").
func (c Currency) Code() string { return c.code }

// Name is the currency's name ("US Dollar").
func (c Currency) Name() string { return c.name }

// Kind says whether the currency is Fiat or Crypto.
func (c Currency) Kind() Kind { return c.kind }

// Decimals is how many decimal places the currency's smallest unit has:
// 2 for USD, 0 for JPY, 8 for BTC.
func (c Currency) Decimals() int32 { return c.decimals }

// byCode indexes the list, and the currencies given to Add, by code. mu
// guards it, since Add may run while it is read.
var (
	mu     sync.RWMutex
	byCode = func() map[string]Currency {
		index := make(map[string]Currency, len(list))
		for _, c := range list {
			index[c.code] = c
		}
		return index
	}()
)

// codePattern is the shape of a code, as gen/main.go enforces for the CSV.
var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// ErrExists is returned by Add for a code already on the list.
var ErrExists = errors.New("currency: code already exists")

// Spec describes a currency to Add. It is only a request: it becomes a
// Currency once Add has checked it.
type Spec struct {
	Code     string
	Name     string
	Kind     Kind
	Decimals int32
}

// Add puts a currency of your own on the list, so that Lookup, Get, MustGet
// and All return it, and returns it. It lasts only as long as the process:
// call it at startup, with currencies kept somewhere that outlives it, such
// as the database, and add the same ones on every start.
//
// The code must be an upper-case letter then 1 to 9 upper-case letters or
// digits ("HUMANBLNK"), and decimals 0 to 18. A code already on the list is
// refused with ErrExists, never replaced: replacing it could change the
// decimals of a currency money has moved in.
func Add(s Spec) (Currency, error) {
	switch {
	case !codePattern.MatchString(s.Code):
		return Currency{}, fmt.Errorf("currency: bad code %q", s.Code)
	case strings.TrimSpace(s.Name) == "":
		return Currency{}, fmt.Errorf("currency: %s has no name", s.Code)
	case s.Kind != Fiat && s.Kind != Crypto:
		return Currency{}, fmt.Errorf("currency: %s has bad kind %q", s.Code, s.Kind)
	case s.Decimals < 0 || s.Decimals > 18:
		return Currency{}, fmt.Errorf("currency: %s has bad decimals %d", s.Code, s.Decimals)
	}
	c := Currency{code: s.Code, name: s.Name, kind: s.Kind, decimals: s.Decimals}

	mu.Lock()
	defer mu.Unlock()
	if _, ok := byCode[c.code]; ok {
		return Currency{}, fmt.Errorf("%w: %s", ErrExists, c.code)
	}
	byCode[c.code] = c
	return c, nil
}

// Lookup returns the currency whose code is exactly code ("USD"), and
// whether there is one. Use it for codes read from storage or other
// services, which must already be exact: "usd" or "USD " there is a bug
// upstream, and is refused rather than quietly accepted.
func Lookup(code string) (Currency, bool) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := byCode[code]
	return c, ok
}

// Get is Lookup for user input: it ignores case and surrounding space.
// Store and send the Code of the currency it returns, never the input.
func Get(code string) (Currency, bool) {
	return Lookup(strings.ToUpper(strings.TrimSpace(code)))
}

// MustGet is Lookup for a code known to exist, such as a constant of this
// package; it panics otherwise.
func MustGet(code string) Currency {
	c, ok := Lookup(code)
	if !ok {
		panic("currency: unknown code " + code)
	}
	return c
}

// All returns the currencies of the given kinds (every currency when none
// is given), ordered by code.
func All(kinds ...Kind) []Currency {
	mu.RLock()
	defer mu.RUnlock()
	var out []Currency
	for _, c := range byCode {
		if len(kinds) == 0 || slices.Contains(kinds, c.kind) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Currency) int { return strings.Compare(a.code, b.code) })
	return out
}

// Valid reports whether c is a known currency (not the zero Currency).
func (c Currency) Valid() bool {
	known, ok := Lookup(c.code)
	return ok && known == c
}

// String is the currency's code.
func (c Currency) String() string { return c.code }
