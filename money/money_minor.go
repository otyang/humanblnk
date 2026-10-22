package money

import (
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"

	"github.com/otyang/humanblnk/money/currency"
)

// Minor is the amount in its currency's smallest unit, exactly: 10056 for
// 100.56 USD, 45600000 for 0.456 BTC. A ledger stores this, with Precision.
// For the zero Money it is ErrUnknownCurrency, so a Money never set is not
// stored as 0.
func (m Money) Minor() (*big.Int, error) {
	if err := known(m.currency); err != nil {
		return nil, err
	}
	return m.amount.Shift(m.currency.Decimals()).BigInt(), nil
}

// Precision is how many smallest units make one whole unit of the
// currency: 100 for USD, 1 for JPY, 100000000 for BTC (a ledger's
// "precision").
func (m Money) Precision() *big.Int {
	return Precision(m.currency)
}

// Precision is 10 to the power of c's decimals.
func Precision(c currency.Currency) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(c.Decimals())), nil)
}

// FromMinor is the amount minor smallest units of c make: FromMinor(10056,
// USD) is 100.56 USD. For a ledger row, get c with currency.Lookup, so a
// stored code that is not exact is caught.
func FromMinor(minor *big.Int, c currency.Currency) (Money, error) {
	if err := known(c); err != nil {
		return Money{}, err
	}
	if minor == nil {
		return Money{}, fmt.Errorf("%w: no amount", ErrInvalidAmount)
	}
	amount, err := inRange(decimal.NewFromBigInt(minor, -c.Decimals()))
	if err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: c}, nil
}
