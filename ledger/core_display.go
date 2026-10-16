package ledger

import (
	"math/big"

	"github.com/otyang/humanblnk/currency"
)

// Format renders m by d. m must be resolved (PreciseAmount and Precision
// set), as every Money the Service returns is; otherwise Format returns
// m.Value.
func (m Money) Format(d currency.Display) string {
	if m.PreciseAmount == nil || m.Precision <= 0 {
		return m.Value
	}
	if _, ok := decimalPlaces(m.Precision); !ok {
		return m.Value
	}
	return d.Format(m.PreciseAmount, m.Precision)
}

// Display returns m formatted for people by its currency's rule.
func (s *Service) Display(m Money) string { return m.Format(s.DisplayRule(m.Currency)) }

// money builds a resolved Money with its display string.
func (s *Service) money(precise *big.Int, precision int64, currency string) Money {
	m := Precise(precise, precision, currency)
	m.Display = s.Display(m)
	return m
}
