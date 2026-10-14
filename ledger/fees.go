package ledger

import (
	"fmt"
	"math/big"
	"strings"
)

// Rounding decides what happens to a fraction of the smallest unit.
type Rounding int

const (
	RoundHalfUp Rounding = iota // 0.5 and above rounds up (default)
	RoundUp                     // any fraction rounds up (in the house's favour for fees)
	RoundDown                   // fractions are dropped (in the customer's favour for fees)
)

// FeeRule describes a fee as percentage + fixed part, clamped to [Min, Max].
// Amounts are decimals in the transaction currency, so keep one rule per currency:
//
//	FeeRule{Percent: "1.5", Min: "10", Max: "2000"}           // 1.5%, at least 10, at most 2,000
//	FeeRule{Fixed: "50"}                                       // flat 50
//	FeeRule{Tiers: []FeeTier{                                  // tiered
//		{UpTo: "5000", Rule: FeeRule{Fixed: "10"}},
//		{UpTo: "50000", Rule: FeeRule{Fixed: "25"}},
//		{Rule: FeeRule{Percent: "0.1", Max: "500"}},              // everything above
//	}}
type FeeRule struct {
	Percent  string // e.g. "1.5" for 1.5%; "" = none
	Fixed    string // flat part, e.g. "100"; "" = none
	Min      string // floor on the fee; "" = none
	Max      string // cap on the fee; "" = none
	Rounding Rounding

	// Tiers, when set, replace the fields above: the first tier whose UpTo is
	// >= the amount applies. A tier with an empty UpTo has no upper bound.
	Tiers []FeeTier
}

// FeeTier is one band of a tiered FeeRule.
type FeeTier struct {
	UpTo string
	Rule FeeRule
}

// ComputeFee applies rule to amount and returns the fee in the same currency
// and precision, ready to pass as Fee on Deposit, Withdraw or Transfer.
func (s *Service) ComputeFee(rule FeeRule, amount Money) (Money, error) {
	const op = "ledger.ComputeFee"
	a, err := s.resolve(op, amount, "")
	if err != nil {
		return Money{}, err
	}
	fee, err := rule.compute(a)
	if err != nil {
		return Money{}, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	return Precise(fee, a.Precision, a.Currency), nil
}

func (r FeeRule) compute(a Money) (*big.Int, error) {
	if len(r.Tiers) > 0 {
		for i, t := range r.Tiers {
			if t.UpTo == "" {
				return t.Rule.compute(a)
			}
			limit, err := parseDecimal(t.UpTo, a.Precision)
			if err != nil {
				return nil, fmt.Errorf("tier %d: %w", i, err)
			}
			if a.PreciseAmount.Cmp(limit) <= 0 {
				return t.Rule.compute(a)
			}
		}
		return nil, fmt.Errorf("%w: no fee tier covers %s", ErrInvalidInput, a)
	}

	fee := new(big.Int)
	if r.Percent != "" {
		pct, ok := new(big.Rat).SetString(strings.TrimSuffix(strings.TrimSpace(r.Percent), "%"))
		if !ok || pct.Sign() < 0 {
			return nil, fmt.Errorf("%w: invalid percent %q", ErrInvalidInput, r.Percent)
		}
		v := new(big.Rat).SetInt(a.PreciseAmount)
		v.Mul(v, pct).Quo(v, big.NewRat(100, 1))
		fee.Add(fee, roundRat(v, r.Rounding))
	}
	for _, part := range []struct {
		name, value string
		apply       func(v *big.Int)
	}{
		{"fixed", r.Fixed, func(v *big.Int) { fee.Add(fee, v) }},
		{"min", r.Min, func(v *big.Int) {
			if fee.Cmp(v) < 0 {
				fee.Set(v)
			}
		}},
		{"max", r.Max, func(v *big.Int) {
			if fee.Cmp(v) > 0 {
				fee.Set(v)
			}
		}},
	} {
		if part.value == "" {
			continue
		}
		v, err := parseDecimal(part.value, a.Precision)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", part.name, err)
		}
		part.apply(v)
	}
	return fee, nil
}

// roundRat rounds a non-negative rational to an integer.
func roundRat(v *big.Rat, mode Rounding) *big.Int {
	q, rem := new(big.Int).QuoRem(v.Num(), v.Denom(), new(big.Int))
	if rem.Sign() == 0 {
		return q
	}
	switch mode {
	case RoundUp:
		return q.Add(q, big.NewInt(1))
	case RoundDown:
		return q
	default:
		if new(big.Int).Mul(rem, big.NewInt(2)).Cmp(v.Denom()) >= 0 {
			q.Add(q, big.NewInt(1))
		}
		return q
	}
}
