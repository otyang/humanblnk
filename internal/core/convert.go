package core

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// ToBlnk is the only way an amount reaches Blnk: in its smallest unit, with
// its currency's precision. The zero Money is refused.
func ToBlnk(m money.Money) (*blnk.BigInt, int64, error) {
	minor, err := m.Minor()
	if err != nil {
		return nil, 0, err
	}
	return blnk.BigFrom(minor), m.Precision().Int64(), nil
}

// FromBlnk reads an amount Blnk stored; Lookup, so a malformed code is an error.
func FromBlnk(minor *big.Int, code string) (money.Money, error) {
	c, ok := currency.Lookup(code)
	if !ok {
		return money.Money{}, fmt.Errorf("humanblnk: blnk returned unknown currency %q", code)
	}
	return money.FromMinor(minor, c)
}

// CheckPrecision refuses a transaction Blnk recorded with a precision other
// than its currency's: its amounts would be in a different unit.
func CheckPrecision(id string, got json.Number, c currency.Currency) error {
	if got == "" {
		return nil
	}
	p, err := strconv.ParseFloat(got.String(), 64)
	want := money.Precision(c)
	if err != nil || !want.IsInt64() || p != float64(want.Int64()) {
		return fmt.Errorf("humanblnk: blnk recorded %s with precision %s, want %s for %s", id, got, want, c.Code())
	}
	return nil
}

// Time is t as Blnk parses dates (RFC 3339 with an offset), or "" for the
// zero time. Blnk silently reads a malformed date as no date at all.
func Time(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
