package core_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// Every currency's precision fits Blnk's int64, and the largest amount Money
// allows survives the trip to minor units and back.
func TestEveryCurrencyRoundTrips(t *testing.T) {
	largest, _ := new(big.Int).SetString(strings.Repeat("9", 40), 10)
	for _, c := range currency.All() {
		if !money.Precision(c).IsInt64() {
			t.Errorf("%s: precision %s overflows int64", c, money.Precision(c))
		}
		m, err := money.FromMinor(largest, c)
		if err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if back, err := m.Minor(); err != nil || back.Cmp(largest) != 0 {
			t.Errorf("%s: %s came back as %s (%v)", c, largest, back, err)
		}
	}
}
