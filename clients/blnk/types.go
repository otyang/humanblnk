package blnk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// BigInt is an arbitrary-precision integer that marshals as a JSON number and
// unmarshals from a number, a quoted string, or null. Blnk stores amounts in the
// smallest unit (precise_amount), which can exceed int64 for high-precision assets.
type BigInt struct{ big.Int }

// NewBigInt returns a BigInt holding v.
func NewBigInt(v int64) *BigInt { b := &BigInt{}; b.SetInt64(v); return b }

// BigFrom wraps a *big.Int (nil stays nil).
func BigFrom(v *big.Int) *BigInt {
	if v == nil {
		return nil
	}
	b := &BigInt{}
	b.Set(v)
	return b
}

// MarshalJSON writes the integer as a bare JSON number.
func (b BigInt) MarshalJSON() ([]byte, error) { return []byte(b.String()), nil }

// UnmarshalJSON reads a number, a quoted string or null (as zero).
func (b *BigInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		b.SetInt64(0)
		return nil
	}
	s := string(bytes.Trim(data, `"`))
	if s == "" {
		b.SetInt64(0)
		return nil
	}
	// Accept exponent / decimal forms defensively (e.g. 1e+06, 100.0).
	if _, ok := b.SetString(s, 10); ok {
		return nil
	}
	f, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	if err != nil {
		return fmt.Errorf("blnk: invalid integer %q", s)
	}
	f.Int(&b.Int)
	return nil
}

// Time tolerates Blnk's zero value ("0001-01-01T00:00:00Z"), "" and null.
type Time struct{ time.Time }

// UnmarshalJSON reads RFC 3339 text; "", null and Blnk's zero time give the zero Time.
func (t *Time) UnmarshalJSON(data []byte) error {
	s := string(bytes.Trim(bytes.TrimSpace(data), `"`))
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("blnk: invalid time %q: %w", s, err)
	}
	t.Time = parsed
	return nil
}

// MarshalJSON writes RFC 3339 text, or null for the zero Time.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time.Format(time.RFC3339Nano))
}
