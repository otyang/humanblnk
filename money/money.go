// Package money is an exact amount in one currency (money/currency): parsed
// from the decimal a person or client wrote, never a float, and refused
// when it has more decimal places than its currency allows.
//
// Every constructor takes a currency.Currency, not a code: the caller looks
// the code up once, with currency.Get for what people type or
// currency.Lookup for codes from storage and other services.
//
// Arithmetic only combines amounts of the same currency; anything else is
// ErrCurrencyMismatch. Amounts convert exactly to and from the integer
// smallest unit a ledger stores (Minor, FromMinor), and round only where
// asked, with a stated RoundingMode.
package money

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/otyang/humanblnk/money/currency"
)

var (
	// ErrUnknownCurrency is a currency that is not on money/currency's
	// list: the zero currency.Currency (so the zero Money too), or a code
	// in JSON that Lookup refuses.
	ErrUnknownCurrency = errors.New("unknown currency")
	// ErrInvalidAmount is an amount that is not a plain decimal number
	// (exponents, separators and the like are refused), or JSON that is
	// not {"amount": "…", "currency": "…"}.
	ErrInvalidAmount = errors.New("invalid amount")
	// ErrTooManyDecimals is an amount more precise than its currency's
	// smallest unit, such as 1.005 USD.
	ErrTooManyDecimals = errors.New("more decimal places than the currency has")
	// ErrOutOfRange is an amount, or the result of arithmetic on one, with
	// more than maxDigits digits before or after the point.
	ErrOutOfRange = errors.New("amount out of range")
	// ErrCurrencyMismatch is arithmetic or a comparison between two
	// currencies.
	ErrCurrencyMismatch = errors.New("currencies differ")
)

// Money is an exact amount in one currency. The zero Money has no
// currency and is not valid (Valid): arithmetic on it is
// ErrUnknownCurrency. Make one with Parse, New, Round, FromMinor or Zero.
//
// Compare amounts with Equal or Cmp: == and map keys do not compile, since
// they would compare the decimal's internals (1.50 USD would not == 1.50
// USD). An omitzero JSON tag leaves out only the zero Money, never a valid
// amount of 0.
type Money struct {
	_        [0]func()
	amount   decimal.Decimal
	currency currency.Currency
}

// maxDigits is how many digits an amount may have before the point, and
// how many it may be written with after it: 40, enough for any uint256
// count of an 18-decimal token. Parse, New and the arithmetic all keep to
// it, so whatever Money there is marshals and parses back.
const maxDigits = 40

// amountPattern is a plain decimal: an optional minus sign, digits, and
// optionally a point and more digits. No exponent, no plus sign, no
// thousands separators.
var amountPattern = regexp.MustCompile(`^-?[0-9]{1,40}(\.[0-9]{1,40})?$`)

// powersOf10 holds 10^0 to 10^(2*maxDigits), the bounds inRange compares
// coefficients with.
var powersOf10 = func() (powers [2*maxDigits + 1]*big.Int) {
	for i := range powers {
		powers[i] = new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(i)), nil)
	}
	return powers
}()

// inRange returns d, or ErrOutOfRange when it has more than maxDigits
// digits before the point or is written with more than maxDigits after it.
// It looks only at the exponent and coefficient, so it is quick for a
// decimal such as 1e100000000 that would take seconds to rescale, and runs
// before anything else touches an amount from outside.
func inRange(d decimal.Decimal) (decimal.Decimal, error) {
	coefficient := d.Coefficient()
	if coefficient.Sign() == 0 {
		return decimal.Zero, nil
	}
	exponent := d.Exponent()
	if exponent < -maxDigits || exponent > maxDigits || coefficient.CmpAbs(powersOf10[maxDigits-exponent]) >= 0 {
		return decimal.Decimal{}, fmt.Errorf("%w: more than %d digits", ErrOutOfRange, maxDigits)
	}
	return d, nil
}

// Parse reads value, such as "100.50", as an amount of c. Surrounding
// space is ignored; trailing zeros past the currency's decimals are fine
// ("100.00" JPY), other digits there are ErrTooManyDecimals.
func Parse(value string, c currency.Currency) (Money, error) {
	return parse(strings.TrimSpace(value), c)
}

// MustParse is Parse for values known to be valid, such as constants in
// tests; it panics otherwise.
func MustParse(value string, c currency.Currency) Money {
	m, err := Parse(value, c)
	if err != nil {
		panic(err)
	}
	return m
}

func parse(value string, c currency.Currency) (Money, error) {
	if !amountPattern.MatchString(value) {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidAmount, value)
	}
	amount, err := decimal.NewFromString(value)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidAmount, value)
	}
	return New(amount, c)
}

// New returns amount in c, or ErrTooManyDecimals when it is more precise
// than the currency: round it first (Round) when it may be.
func New(amount decimal.Decimal, c currency.Currency) (Money, error) {
	if err := known(c); err != nil {
		return Money{}, err
	}
	amount, err := inRange(amount)
	if err != nil {
		return Money{}, err
	}
	if !amount.Equal(amount.Truncate(c.Decimals())) {
		return Money{}, fmt.Errorf("%w: %s has %d for %s", ErrTooManyDecimals, amount, c.Decimals(), c.Code())
	}
	return Money{amount: amount, currency: c}, nil
}

// Zero is nothing in c. For the zero currency.Currency it is the zero
// Money, which is not Valid.
func Zero(c currency.Currency) Money {
	return Money{amount: decimal.Zero, currency: c}
}

func known(c currency.Currency) error {
	if !c.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownCurrency, c.Code())
	}
	return nil
}

// Amount is the amount as a decimal.
func (m Money) Amount() decimal.Decimal { return m.amount }

// Currency is the amount's currency.
func (m Money) Currency() currency.Currency { return m.currency }

// Code is the amount's currency code, such as "USD".
func (m Money) Code() string { return m.currency.Code() }

// Valid reports whether m has a known currency: false for the zero Money.
func (m Money) Valid() bool { return m.currency.Valid() }

// IsZeroAmount reports whether the amount is zero. It is not called IsZero
// because encoding/json's omitzero would then leave out a balance of 0.00.
func (m Money) IsZeroAmount() bool { return m.amount.IsZero() }

// Sign is -1, 0 or 1 as the amount is negative, zero or positive.
func (m Money) Sign() int { return m.amount.Sign() }

// IsPositive reports whether the amount is above zero.
func (m Money) IsPositive() bool { return m.amount.Sign() > 0 }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.amount.Sign() < 0 }

// Neg is the amount with its sign flipped.
func (m Money) Neg() Money { return Money{amount: m.amount.Neg(), currency: m.currency} }

// Abs is the amount without its sign.
func (m Money) Abs() Money { return Money{amount: m.amount.Abs(), currency: m.currency} }

// Add returns m + other, or ErrCurrencyMismatch, or ErrOutOfRange.
func (m Money) Add(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	return m.with(m.amount.Add(other.amount))
}

// Sub returns m - other, or ErrCurrencyMismatch, or ErrOutOfRange.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	return m.with(m.amount.Sub(other.amount))
}

// with is amount in m's currency, or ErrOutOfRange.
func (m Money) with(amount decimal.Decimal) (Money, error) {
	amount, err := inRange(amount)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: m.currency}, nil
}

// Cmp is -1, 0 or 1 as m is less than, equal to or more than other, or
// ErrCurrencyMismatch.
func (m Money) Cmp(other Money) (int, error) {
	if err := m.sameCurrency(other); err != nil {
		return 0, err
	}
	return m.amount.Cmp(other.amount), nil
}

// Equal reports whether m and other are the same amount of the same known
// currency (1.50 and 1.5 USD are equal). Like Cmp, it holds for no zero
// Money, not even two of them.
func (m Money) Equal(other Money) bool {
	return m.Valid() && m.currency == other.currency && m.amount.Equal(other.amount)
}

// sameCurrency refuses the zero Money, so a Money never set does not pass
// through arithmetic unnoticed, and two different currencies.
func (m Money) sameCurrency(other Money) error {
	if err := known(m.currency); err != nil {
		return err
	}
	if err := known(other.currency); err != nil {
		return err
	}
	if m.currency != other.currency {
		return fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.currency.Code(), other.currency.Code())
	}
	return nil
}

// String is the amount with exactly its currency's decimals, such as
// "100.50" for USD or "1500" for JPY, without the code.
func (m Money) String() string { return m.amount.StringFixed(m.currency.Decimals()) }

// jsonMoney is Money's JSON form: the amount as a string, so no client
// reads it as a float.
type jsonMoney struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON writes {"amount": "100.50", "currency": "USD"}, or null for
// the zero Money.
func (m Money) MarshalJSON() ([]byte, error) {
	if !m.Valid() {
		return []byte("null"), nil
	}
	return json.Marshal(jsonMoney{Amount: m.String(), Currency: m.currency.Code()})
}

// UnmarshalJSON reads {"amount": "100.50", "currency": "USD"}, and null as
// the zero Money. JSON is written by programs, so it must be exact: the
// keys exactly "amount" and "currency", each once and nothing else, the
// amount a string with no surrounding space, the code as currency.Lookup
// takes it ("usd" is ErrUnknownCurrency).
func (m *Money) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*m = Money{}
		return nil
	}
	fields, err := jsonFields(data)
	if err != nil {
		return err
	}
	amount, ok := fields["amount"]
	if !ok {
		return fmt.Errorf("%w: no amount", ErrInvalidAmount)
	}
	code, ok := fields["currency"]
	if !ok {
		return fmt.Errorf("%w: no currency", ErrUnknownCurrency)
	}
	c, ok := currency.Lookup(code)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCurrency, code)
	}
	parsed, err := parse(amount, c)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// errNotMoneyJSON is JSON that is not an object.
var errNotMoneyJSON = fmt.Errorf("%w: want {\"amount\": \"…\", \"currency\": \"…\"}", ErrInvalidAmount)

// jsonFields reads Money's JSON object into its fields. It is stricter
// than encoding/json, which matches keys in any case and keeps the last of
// a repeated key: here each key must be exactly "amount" or "currency",
// appear once and have a string value, so {"amount": "1", "amount": "2"}
// cannot mean one amount to a gateway and another to the ledger.
func jsonFields(data []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errNotMoneyJSON
	}
	fields := make(map[string]string, 2)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, errNotMoneyJSON
		}
		key, _ := token.(string)
		if key != "amount" && key != "currency" {
			return nil, fmt.Errorf("%w: unknown field %q", ErrInvalidAmount, key)
		}
		if _, seen := fields[key]; seen {
			return nil, fmt.Errorf("%w: %q given twice", ErrInvalidAmount, key)
		}
		token, err = decoder.Token()
		value, isString := token.(string)
		if err != nil || !isString {
			if key == "currency" {
				return nil, fmt.Errorf("%w: the currency must be a string", ErrUnknownCurrency)
			}
			return nil, fmt.Errorf("%w: the amount must be a string", ErrInvalidAmount)
		}
		fields[key] = value
	}
	return fields, nil
}
