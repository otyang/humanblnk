package ledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// Money is an amount in a currency, carried in both forms Blnk stores:
//
//	Value         "0.456"      the human decimal, exactly as typed (trailing zeros trimmed)
//	PreciseAmount 45600000     the integer in the smallest unit (what Blnk sums on balances)
//	Precision     100000000    the multiplier between them (Value × Precision = PreciseAmount)
//
// Build one from what the user typed with Amount("0.456", "BTC"); the Service
// fills PreciseAmount and Precision from the currency's configured precision.
// No floats are involved at any step.
type Money struct {
	Value         string
	Currency      string
	PreciseAmount *big.Int
	Precision     int64

	// Display is the amount for people to read, set on every Money the
	// Service returns: fiat padded to its decimals ("100.00"), crypto trimmed
	// and rounded down ("0.456789"). See DisplayRule. Never compute with it.
	Display string
}

// Amount builds Money from a decimal string as the user typed it:
// Amount("0.456", "BTC"), Amount("100.56", "USD"), Amount("1,500", "NGN").
func Amount(value, currency string) Money {
	return Money{Value: strings.TrimSpace(value), Currency: strings.ToUpper(strings.TrimSpace(currency))}
}

// Minor builds Money from an integer in the smallest unit: Minor(10056, "USD") = 100.56 USD.
func Minor(precise int64, currency string) Money {
	return Money{PreciseAmount: big.NewInt(precise), Currency: strings.ToUpper(currency)}
}

// Precise builds a fully specified Money from a smallest-unit integer and precision.
func Precise(precise *big.Int, precision int64, currency string) Money {
	m := Money{PreciseAmount: new(big.Int).Set(precise), Precision: precision, Currency: strings.ToUpper(currency)}
	m.Value = formatDecimal(m.PreciseAmount, precision)
	return m
}

// ParseDecimal resolves a decimal string against an explicit precision.
func ParseDecimal(value, currency string, precision int64) (Money, error) {
	return Amount(value, currency).resolve(precision)
}

// resolve fills whichever of Value / PreciseAmount is missing (and checks they
// agree when both are set). precision is used only if m.Precision is unset.
func (m Money) resolve(precision int64) (Money, error) {
	if m.Precision > 0 {
		precision = m.Precision
	}
	if _, ok := decimalPlaces(precision); !ok {
		return Money{}, fmt.Errorf("%w: precision %d must be a power of 10", ErrInvalidInput, precision)
	}
	out := Money{Currency: strings.ToUpper(m.Currency), Precision: precision}

	switch {
	case m.Value != "":
		p, err := parseDecimal(m.Value, precision)
		if err != nil {
			return Money{}, err
		}
		if m.PreciseAmount != nil && m.PreciseAmount.Cmp(p) != 0 {
			return Money{}, fmt.Errorf("%w: amount %q does not equal precise_amount %s at precision %d",
				ErrInvalidInput, m.Value, m.PreciseAmount, precision)
		}
		out.PreciseAmount = p
	case m.PreciseAmount != nil:
		out.PreciseAmount = new(big.Int).Set(m.PreciseAmount)
	default:
		out.PreciseAmount = new(big.Int)
	}
	out.Value = formatDecimal(out.PreciseAmount, precision)
	return out, nil
}

func parseDecimal(s string, precision int64) (*big.Int, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	clean = strings.ReplaceAll(clean, "_", "")
	if clean == "" {
		return nil, fmt.Errorf("%w: empty amount", ErrInvalidInput)
	}
	if strings.ContainsAny(clean, "eE") {
		return nil, fmt.Errorf("%w: amount %q: exponent notation not allowed", ErrInvalidInput, s)
	}
	r, ok := new(big.Rat).SetString(clean)
	if !ok {
		return nil, fmt.Errorf("%w: invalid amount %q", ErrInvalidInput, s)
	}
	r.Mul(r, new(big.Rat).SetInt64(precision))
	if !r.IsInt() {
		dp, _ := decimalPlaces(precision)
		return nil, fmt.Errorf("%w: amount %q has more than %d decimal places", ErrInvalidInput, s, dp)
	}
	return new(big.Int).Set(r.Num()), nil
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

// formatDecimal renders precise/precision without floats, trailing zeros trimmed.
func formatDecimal(precise *big.Int, precision int64) string {
	s := fixedDecimal(precise, precision)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

func fixedDecimal(precise *big.Int, precision int64) string {
	dp, _ := decimalPlaces(precision)
	if precise == nil {
		precise = new(big.Int)
	}
	neg := precise.Sign() < 0
	digits := new(big.Int).Abs(precise).String()
	if dp > 0 {
		if len(digits) <= dp {
			digits = strings.Repeat("0", dp-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-dp] + "." + digits[len(digits)-dp:]
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}

// ----- helpers ---------------------------------------------------------------

// Sign returns -1, 0 or +1 (resolved Money only).
func (m Money) Sign() int {
	if m.PreciseAmount == nil {
		return 0
	}
	return m.PreciseAmount.Sign()
}

// IsZero reports whether m is zero or not set at all. An unresolved amount
// is read from its Value, so NoFee and Amount("0.00", "USD") are zero.
func (m Money) IsZero() bool {
	if m.PreciseAmount == nil && m.Value != "" {
		v, err := ratOf(Money{Value: m.Value})
		return err == nil && v.Sign() == 0
	}
	return m.Sign() == 0
}

// IsPositive reports whether m is greater than zero (resolved Money only).
func (m Money) IsPositive() bool { return m.Sign() > 0 }

// isSet reports whether the caller supplied any amount at all.
func (m Money) isSet() bool { return m.Value != "" || m.PreciseAmount != nil }

func (m Money) add(o Money) Money {
	sum := new(big.Int).Add(m.PreciseAmount, o.PreciseAmount)
	return Precise(sum, m.Precision, m.Currency)
}

func (m Money) cmp(o Money) int { return m.PreciseAmount.Cmp(o.PreciseAmount) }

// Fixed renders with every decimal place the precision allows: "0.45600000".
func (m Money) Fixed() string { return fixedDecimal(m.PreciseAmount, m.Precision) }

// String renders "0.456 BTC".
func (m Money) String() string {
	v := m.Value
	if v == "" && m.PreciseAmount != nil && m.Precision > 0 {
		v = formatDecimal(m.PreciseAmount, m.Precision)
	}
	return strings.TrimSpace(v + " " + m.Currency)
}

// ----- JSON -----------------------------------------------------------------

// MarshalJSON emits both forms, plus display when set. precise_amount is a
// string so JavaScript clients don't lose digits on 18-decimal assets:
//
//	{"amount":"0.456","display":"0.456","currency":"BTC","precise_amount":"45600000","precision":100000000}
func (m Money) MarshalJSON() ([]byte, error) {
	type wire struct {
		Amount        string `json:"amount"`
		Display       string `json:"display,omitempty"`
		Currency      string `json:"currency"`
		PreciseAmount string `json:"precise_amount,omitempty"`
		Precision     int64  `json:"precision,omitempty"`
	}
	w := wire{Amount: m.Value, Display: m.Display, Currency: m.Currency, Precision: m.Precision}
	if m.PreciseAmount != nil {
		w.PreciseAmount = m.PreciseAmount.String()
	}
	return json.Marshal(w)
}

// UnmarshalJSON accepts what an API client sends: amount as a string ("0.456")
// or a bare JSON number (0.456, read from its literal text — never via float64),
// plus optional precise_amount / precision.
func (m *Money) UnmarshalJSON(data []byte) error {
	var w struct {
		Amount        json.RawMessage `json:"amount"`
		Currency      string          `json:"currency"`
		PreciseAmount json.RawMessage `json:"precise_amount"`
		Precision     int64           `json:"precision"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*m = Money{Currency: strings.ToUpper(w.Currency), Precision: w.Precision, Value: rawScalar(w.Amount)}
	if p := rawScalar(w.PreciseAmount); p != "" {
		v, ok := new(big.Int).SetString(p, 10)
		if !ok {
			return fmt.Errorf("%w: invalid precise_amount %q", ErrInvalidInput, p)
		}
		m.PreciseAmount = v
	}
	return nil
}

func rawScalar(r json.RawMessage) string {
	r = bytes.TrimSpace(r)
	if len(r) == 0 || bytes.Equal(r, []byte("null")) {
		return ""
	}
	return string(bytes.Trim(r, `"`))
}

func newInt(v int64) *big.Int { return big.NewInt(v) }
