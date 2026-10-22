package money

import (
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/otyang/humanblnk/money/currency"
)

var (
	usd = currency.MustGet(currency.USD)
	eur = currency.MustGet(currency.EUR)
	jpy = currency.MustGet(currency.JPY)
	eth = currency.MustGet(currency.ETH)
)

// mustMinor is m.Minor() for a Money known to be valid.
func mustMinor(t *testing.T, m Money) *big.Int {
	t.Helper()
	minor, err := m.Minor()
	if err != nil {
		t.Fatalf("%s.Minor() error = %v", m, err)
	}
	return minor
}

func TestParse(t *testing.T) {
	for _, test := range []struct {
		value, code, want string
		err               error
	}{
		{"100.5", "USD", "100.50", nil},
		{" 100.56 ", "USD", "100.56", nil},
		{"-3", "EUR", "-3.00", nil},
		{"-0.00", "EUR", "0.00", nil},
		{"1500", "JPY", "1500", nil},
		{"1500.00", "JPY", "1500", nil},
		{"0.456", "BTC", "0.45600000", nil},
		{"0.000000000000000001", "ETH", "0.000000000000000001", nil},
		{"1.005", "USD", "", ErrTooManyDecimals},
		{"1500.5", "JPY", "", ErrTooManyDecimals},
		{"1e3", "USD", "", ErrInvalidAmount},
		{"+1", "USD", "", ErrInvalidAmount},
		{"1,000", "USD", "", ErrInvalidAmount},
		{".5", "USD", "", ErrInvalidAmount},
		{"", "USD", "", ErrInvalidAmount},
		{"NaN", "USD", "", ErrInvalidAmount},
		{strings.Repeat("9", 41), "USD", "", ErrInvalidAmount},
	} {
		got, err := Parse(test.value, currency.MustGet(test.code))
		if !errors.Is(err, test.err) {
			t.Errorf("Parse(%q, %q) error = %v, want %v", test.value, test.code, err, test.err)
			continue
		}
		if err == nil && got.String() != test.want {
			t.Errorf("Parse(%q, %q) = %s, want %s", test.value, test.code, got, test.want)
		}
	}
	if _, err := Parse("1", currency.Currency{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("Parse(no currency) error = %v, want ErrUnknownCurrency", err)
	}
}

func TestNewAndZero(t *testing.T) {
	if _, err := New(decimal.RequireFromString("1.234"), usd); !errors.Is(err, ErrTooManyDecimals) {
		t.Errorf("New(1.234 USD) error = %v, want ErrTooManyDecimals", err)
	}
	m, err := New(decimal.RequireFromString("1.23"), usd)
	if err != nil || m.String() != "1.23" || m.Code() != currency.USD {
		t.Errorf("New(1.23 USD) = %s %s, %v", m, m.Code(), err)
	}
	zero := Zero(currency.MustGet(currency.NGN))
	if !zero.IsZeroAmount() || !zero.Valid() || zero.String() != "0.00" {
		t.Errorf("Zero(NGN) = %s, valid %v", zero, zero.Valid())
	}
	if (Money{}).Valid() || Zero(currency.Currency{}).Valid() {
		t.Error("the zero Money is valid")
	}
	if _, err := New(decimal.NewFromInt(1), currency.Currency{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("New(no currency) error = %v, want ErrUnknownCurrency", err)
	}
}

func TestArithmetic(t *testing.T) {
	a, b := MustParse("10.25", usd), MustParse("0.75", usd)
	sum, err := a.Add(b)
	if err != nil || sum.String() != "11.00" {
		t.Errorf("Add = %s, %v", sum, err)
	}
	diff, err := b.Sub(a)
	if err != nil || diff.String() != "-9.50" || !diff.IsNegative() || diff.Sign() != -1 {
		t.Errorf("Sub = %s, %v", diff, err)
	}
	if diff.Neg().String() != "9.50" || diff.Abs().String() != "9.50" {
		t.Errorf("Neg/Abs = %s/%s", diff.Neg(), diff.Abs())
	}
	if cmp, err := a.Cmp(b); err != nil || cmp != 1 {
		t.Errorf("Cmp = %d, %v", cmp, err)
	}
	if !MustParse("1.5", usd).Equal(MustParse("1.50", usd)) || MustParse("1", usd).Equal(MustParse("1", eur)) {
		t.Error("Equal compares value and currency")
	}

	euros := MustParse("1", eur)
	if _, err := a.Add(euros); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add USD+EUR error = %v", err)
	}
	if _, err := a.Sub(euros); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Sub USD-EUR error = %v", err)
	}
	if _, err := a.Cmp(euros); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Cmp USD/EUR error = %v", err)
	}
}

func TestTheZeroMoneyIsRefused(t *testing.T) {
	a := MustParse("1", usd)
	if _, err := (Money{}).Add(Money{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("zero+zero error = %v, want ErrUnknownCurrency", err)
	}
	if _, err := a.Add(Money{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("USD+zero error = %v, want ErrUnknownCurrency", err)
	}
	if _, err := (Money{}).Sub(a); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("zero-USD error = %v, want ErrUnknownCurrency", err)
	}
	if _, err := (Money{}).Cmp(Money{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("Cmp(zero, zero) error = %v, want ErrUnknownCurrency", err)
	}
	if _, err := (Money{}).Percent(decimal.NewFromInt(10), Up); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("Percent of zero error = %v, want ErrUnknownCurrency", err)
	}
	if minor, err := (Money{}).Minor(); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("Minor of zero = %v, %v; want ErrUnknownCurrency", minor, err)
	}
	if (Money{}).Equal(Money{}) || (Money{}).Equal(Zero(currency.Currency{})) {
		t.Error("the zero Money is Equal to itself, though Cmp refuses it")
	}
}

func TestMoneyIsNotComparable(t *testing.T) {
	// == would compare the decimal's *big.Int pointers: two 1.50 USD
	// would differ, and map lookups would miss.
	if reflect.TypeFor[Money]().Comparable() {
		t.Error("Money can be compared with == or used as a map key")
	}
}

func TestMinor(t *testing.T) {
	for _, test := range []struct {
		value, code      string
		minor, precision string
	}{
		{"100.56", "USD", "10056", "100"},
		{"-0.01", "USD", "-1", "100"},
		{"1500", "JPY", "1500", "1"},
		{"0.456", "BTC", "45600000", "100000000"},
		{"1.5", "ETH", "1500000000000000000", "1000000000000000000"},
		{"12.345", "KWD", "12345", "1000"},
	} {
		c := currency.MustGet(test.code)
		m := MustParse(test.value, c)
		minor := mustMinor(t, m)
		if minor.String() != test.minor || m.Precision().String() != test.precision {
			t.Errorf("%s %s: minor %s precision %s, want %s %s", test.value, test.code, minor, m.Precision(), test.minor, test.precision)
		}
		back, err := FromMinor(minor, c)
		if err != nil || !back.Equal(m) {
			t.Errorf("FromMinor(%s, %s) = %s, %v, want %s", minor, test.code, back, err, m)
		}
	}
	if _, err := FromMinor(big.NewInt(1), currency.Currency{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("FromMinor(no currency) error = %v", err)
	}
	if _, err := FromMinor(nil, usd); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("FromMinor(nil) error = %v", err)
	}
}

func TestRounding(t *testing.T) {
	for _, test := range []struct {
		value string
		mode  RoundingMode
		want  string
	}{
		{"1.239", Down, "1.23"},
		{"-1.239", Down, "-1.23"},
		{"-0.001", Down, "0.00"},
		{"1.231", Up, "1.24"},
		{"-1.231", Up, "-1.24"},
		{"1.23", Up, "1.23"},
		{"1.225", HalfEven, "1.22"},
		{"1.235", HalfEven, "1.24"},
	} {
		got, err := Round(decimal.RequireFromString(test.value), usd, test.mode)
		if err != nil || got.String() != test.want {
			t.Errorf("Round(%s, %d) = %s, %v, want %s", test.value, test.mode, got, err, test.want)
		}
	}
	if _, err := Round(decimal.NewFromInt(1), currency.Currency{}, Down); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("Round(no currency) error = %v", err)
	}
}

func TestUnknownRoundingModes(t *testing.T) {
	var unset RoundingMode
	for _, mode := range []RoundingMode{unset, HalfEven + 1, -1} {
		if _, err := Round(decimal.RequireFromString("1.231"), usd, mode); !errors.Is(err, ErrUnknownRoundingMode) {
			t.Errorf("Round(mode %d) error = %v, want ErrUnknownRoundingMode", mode, err)
		}
		if _, err := MustParse("1", usd).Percent(decimal.NewFromInt(1), mode); !errors.Is(err, ErrUnknownRoundingMode) {
			t.Errorf("Percent(mode %d) error = %v, want ErrUnknownRoundingMode", mode, err)
		}
	}
}

func TestPercent(t *testing.T) {
	for _, test := range []struct {
		amount  Money
		percent string
		mode    RoundingMode
		want    string
	}{
		// 1.5% of 333.33 is 4.99995.
		{MustParse("333.33", usd), "1.5", Up, "5.00"},
		{MustParse("333.33", usd), "1.5", Down, "4.99"},
		{MustParse("1000", jpy), "0.25", Up, "3"},
	} {
		got, err := test.amount.Percent(decimal.RequireFromString(test.percent), test.mode)
		if err != nil || got.String() != test.want {
			t.Errorf("%s%% of %s %s by %d = %s, %v, want %s", test.percent, test.amount, test.amount.Code(), test.mode, got, err, test.want)
		}
	}
}

func TestRange(t *testing.T) {
	largest := strings.Repeat("9", 40)
	for name, amount := range map[string]decimal.Decimal{
		"1e100000000":  decimal.New(1, 100_000_000),
		"1e-100000000": decimal.New(1, -100_000_000),
		"1e40":         decimal.New(1, 40),
		"41 digits":    decimal.RequireFromString("1" + largest),
	} {
		start := time.Now()
		if _, err := New(amount, usd); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("New(%s) error = %v, want ErrOutOfRange", name, err)
		}
		if _, err := Round(amount, usd, Down); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("Round(%s) error = %v, want ErrOutOfRange", name, err)
		}
		if _, err := MustParse("1", usd).Percent(amount, Up); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("Percent(%s) error = %v, want ErrOutOfRange", name, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("refusing %s took %s", name, elapsed)
		}
	}
	if m, err := New(decimal.New(0, -100_000_000), usd); err != nil || m.String() != "0.00" {
		t.Errorf("New(0e-100000000) = %s, %v, want 0.00", m, err)
	}

	max := MustParse(largest+".99", usd)
	if _, err := max.Add(MustParse("0.01", usd)); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("Add past the largest amount error = %v, want ErrOutOfRange", err)
	}
	if _, err := max.Neg().Sub(MustParse("0.01", usd)); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("Sub past the smallest amount error = %v, want ErrOutOfRange", err)
	}
	if _, err := Round(decimal.RequireFromString(largest+".999"), usd, Up); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("Round up past the largest amount error = %v, want ErrOutOfRange", err)
	}
	if _, err := max.Percent(decimal.NewFromInt(200), Down); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("200%% of the largest amount error = %v, want ErrOutOfRange", err)
	}

	// The largest amount of an 18-decimal token marshals and parses back.
	wei, _ := new(big.Int).SetString(largest+strings.Repeat("9", 18), 10)
	tokens, err := FromMinor(wei, eth)
	if err != nil {
		t.Fatalf("FromMinor(largest wei) error = %v", err)
	}
	data, _ := json.Marshal(tokens)
	var back Money
	if err := json.Unmarshal(data, &back); err != nil || !back.Equal(tokens) {
		t.Errorf("round trip of %s = %s, %v", data, back, err)
	}
	if _, err := FromMinor(new(big.Int).Add(wei, big.NewInt(1)), eth); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("FromMinor(largest wei + 1) error = %v, want ErrOutOfRange", err)
	}
}

func TestJSON(t *testing.T) {
	data, err := json.Marshal(MustParse("100.5", usd))
	if err != nil || string(data) != `{"amount":"100.50","currency":"USD"}` {
		t.Errorf("Marshal = %s, %v", data, err)
	}
	if data, _ := json.Marshal(Money{}); string(data) != "null" {
		t.Errorf("Marshal(zero) = %s, want null", data)
	}

	var m Money
	if err := json.Unmarshal([]byte(`{"currency":"BTC", "amount":"0.456"}`), &m); err != nil || m.String() != "0.45600000" || m.Code() != "BTC" {
		t.Errorf("Unmarshal = %s %s, %v", m, m.Code(), err)
	}
	for body, want := range map[string]error{
		`{"amount":100.5,"currency":"USD"}`:                      ErrInvalidAmount,
		`{"amount":"1.005","currency":"USD"}`:                    ErrTooManyDecimals,
		`{"amount":"1","currency":"XYZ"}`:                        ErrUnknownCurrency,
		`{"amount":"1","currency":"usd"}`:                        ErrUnknownCurrency,
		`{"amount":"1","currency":"USD "}`:                       ErrUnknownCurrency,
		`{"amount":"1","currency":5}`:                            ErrUnknownCurrency,
		`{"amount":" 1 ","currency":"USD"}`:                      ErrInvalidAmount,
		`{"amount":{},"currency":"USD"}`:                         ErrInvalidAmount,
		`{"amount":"1"}`:                                         ErrUnknownCurrency,
		`{"currency":"USD"}`:                                     ErrInvalidAmount,
		`{}`:                                                     ErrInvalidAmount,
		`"100.50"`:                                               ErrInvalidAmount,
		`[]`:                                                     ErrInvalidAmount,
		`123`:                                                    ErrInvalidAmount,
		`{"AMOUNT":"1","currency":"USD"}`:                        ErrInvalidAmount,
		`{"amount":"1","Currency":"USD"}`:                        ErrInvalidAmount,
		`{"amount":"1","currency":"USD","extra":true}`:           ErrInvalidAmount,
		`{"amount":"1","amount":"2","currency":"USD"}`:           ErrInvalidAmount,
		`{"amount":"1","currency":"USD","currency":"EUR"}`:       ErrInvalidAmount,
		`{"amount":"1","currency":"USD","amount":"2","x":"EUR"}`: ErrInvalidAmount,
		`{"amount":"1","amount":"2","currency":"USD","currency":"EUR"}`: ErrInvalidAmount,
	} {
		before := MustParse("7", usd)
		m = before
		if err := json.Unmarshal([]byte(body), &m); !errors.Is(err, want) {
			t.Errorf("Unmarshal(%s) error = %v, want %v", body, err, want)
		}
		if !m.Equal(before) {
			t.Errorf("Unmarshal(%s) changed the Money to %s", body, m)
		}
	}
}

func TestJSONNullIsTheZeroMoney(t *testing.T) {
	var transfer struct {
		Fee Money `json:"fee"`
	}
	transfer.Fee = MustParse("1", usd)
	if err := json.Unmarshal([]byte(`{"fee":null}`), &transfer); err != nil || transfer.Fee.Valid() {
		t.Errorf("Unmarshal(null) = %s valid %v, %v; want the zero Money", transfer.Fee, transfer.Fee.Valid(), err)
	}
	data, _ := json.Marshal(transfer)
	if err := json.Unmarshal(data, &transfer); err != nil || string(data) != `{"fee":null}` {
		t.Errorf("zero Money round trip = %s, %v", data, err)
	}
}

func TestOmitZeroKeepsAZeroAmount(t *testing.T) {
	type account struct {
		Balance Money `json:"balance,omitzero"`
	}
	if data, _ := json.Marshal(account{Balance: Zero(usd)}); string(data) != `{"balance":{"amount":"0.00","currency":"USD"}}` {
		t.Errorf("Marshal(0.00 USD, omitzero) = %s; the balance was left out", data)
	}
	if data, _ := json.Marshal(account{}); string(data) != `{}` {
		t.Errorf("Marshal(zero Money, omitzero) = %s, want {}", data)
	}
}
