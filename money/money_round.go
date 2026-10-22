package money

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/otyang/humanblnk/money/currency"
)

// ErrUnknownRoundingMode is a RoundingMode that is not Down, Up or
// HalfEven, such as an unset one.
var ErrUnknownRoundingMode = errors.New("unknown rounding mode")

// RoundingMode says how an amount more precise than its currency becomes
// one that is not. Pick it for whom the rounding favours:
//
//   - a fee you charge rounds Up, so you never charge less than its
//     price;
//   - an amount paid out or credited rounds Down, so nobody is paid more
//     than what was held for them.
//
// The zero RoundingMode is none of them, and is ErrUnknownRoundingMode:
// a mode left unset must not quietly round one way.
type RoundingMode int

const (
	// Down rounds toward zero: 1.239 → 1.23, -1.239 → -1.23.
	Down RoundingMode = iota + 1
	// Up rounds away from zero: 1.231 → 1.24, -1.231 → -1.24.
	Up
	// HalfEven rounds to the nearest, a half to the even digit (banker's
	// rounding): 1.225 → 1.22, 1.235 → 1.24. For reports, not for money
	// that moves.
	HalfEven
)

// Round returns amount in c, rounded to its decimals by mode.
func Round(amount decimal.Decimal, c currency.Currency, mode RoundingMode) (Money, error) {
	if err := known(c); err != nil {
		return Money{}, err
	}
	amount, err := inRange(amount)
	if err != nil {
		return Money{}, err
	}
	return Zero(c).rounded(amount, mode)
}

// Percent is percent per cent of m (Percent(1.5, Up) is 1.5% of it),
// rounded to the currency by mode. For the zero Money it is
// ErrUnknownCurrency.
func (m Money) Percent(percent decimal.Decimal, mode RoundingMode) (Money, error) {
	if err := known(m.currency); err != nil {
		return Money{}, err
	}
	percent, err := inRange(percent)
	if err != nil {
		return Money{}, err
	}
	return m.rounded(m.amount.Mul(percent).Shift(-2), mode)
}

// rounded is amount, already inRange or the product of two that are,
// rounded to m's currency by mode.
func (m Money) rounded(amount decimal.Decimal, mode RoundingMode) (Money, error) {
	places := m.currency.Decimals()
	switch mode {
	case Down:
		amount = amount.RoundDown(places)
	case Up:
		amount = amount.RoundUp(places)
	case HalfEven:
		amount = amount.RoundBank(places)
	default:
		return Money{}, fmt.Errorf("%w: %d", ErrUnknownRoundingMode, mode)
	}
	return m.with(amount)
}
