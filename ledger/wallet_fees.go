package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// NoFee states that a transaction is deliberately free. Chargeable calls
// (Deposit, Withdraw, Transfer, Exchange, Schedule, each Bulk item and the
// quotes) must say what they charge: set Fee, or set Config.Fees to price
// them. Leaving both out is an error, so a fee can't be forgotten.
var NoFee = Amount("0", "")

// FeeKind is the kind of transaction a fee is for.
type FeeKind string

const (
	FeeDeposit    FeeKind = "deposit"
	FeeWithdrawal FeeKind = "withdrawal"
	FeeTransfer   FeeKind = "transfer"
	FeeExchange   FeeKind = "exchange"
)

// FeeRequest is what Config.Fees is asked to price.
type FeeRequest struct {
	Kind FeeKind

	// Account is the customer account the fee is charged to: the receiving
	// account for a deposit (the fee is deducted), the sending account
	// otherwise. Nil when that side is a house account ("@...").
	Account *Account

	// Amount is the transaction amount, resolved: currency and precision are
	// set. For a deposit it is the gross amount received; otherwise what the
	// recipient gets. For an exchange it is the amount sold.
	Amount Money

	Provider    string // deposits and withdrawals, e.g. "paystack"
	BuyCurrency string // exchanges: the currency bought
}

// FeeFunc returns the fee for a transaction, in r.Amount's currency (an empty
// currency means r.Amount's). Return NoFee for a free transaction. An error
// refuses the transaction with ErrFeePricing.
type FeeFunc func(ctx context.Context, r FeeRequest) (Money, error)

// Convert converts m into r.Amount's currency at rate (units of r.Amount's
// currency per 1 unit of m's), rounded to that currency's precision. Use it to
// price a fee set in another currency, e.g. "$0.50 everywhere":
//
//	r.Convert(ledger.Amount("0.50", "USD"), "1532.40", ledger.RoundUp) // ₦766.20
func (r FeeRequest) Convert(m Money, rate string, mode Rounding) (Money, error) {
	if r.Amount.Precision <= 0 {
		return Money{}, fmt.Errorf("%w: FeeRequest.Amount is not resolved", ErrInvalidInput)
	}
	v, err := ratOf(m)
	if err != nil {
		return Money{}, err
	}
	x, ok := new(big.Rat).SetString(strings.TrimSpace(rate))
	if !ok || x.Sign() <= 0 {
		return Money{}, fmt.Errorf("%w: rate %q must be a positive decimal", ErrInvalidInput, rate)
	}
	v.Mul(v, x).Mul(v, new(big.Rat).SetInt64(r.Amount.Precision))
	if v.Sign() < 0 {
		return Money{}, fmt.Errorf("%w: cannot convert a negative amount", ErrInvalidInput)
	}
	return Precise(roundRat(v, mode), r.Amount.Precision, r.Amount.Currency), nil
}

// ratOf is m as an exact rational, from its decimal Value or from
// PreciseAmount / Precision.
func ratOf(m Money) (*big.Rat, error) {
	if m.Value != "" {
		clean := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(m.Value), ",", ""), "_", "")
		if strings.ContainsAny(clean, "eE") {
			return nil, fmt.Errorf("%w: amount %q: exponent notation not allowed", ErrInvalidInput, m.Value)
		}
		v, ok := new(big.Rat).SetString(clean)
		if !ok {
			return nil, fmt.Errorf("%w: invalid amount %q", ErrInvalidInput, m.Value)
		}
		return v, nil
	}
	if m.PreciseAmount != nil && m.Precision > 0 {
		return new(big.Rat).SetFrac(m.PreciseAmount, big.NewInt(m.Precision)), nil
	}
	return nil, fmt.Errorf("%w: amount has neither a value nor a precise amount with precision", ErrInvalidInput)
}

// feeFor decides a chargeable transaction's fee: the explicit fee if set,
// else Config.Fees, else an error. account is the customer account charged
// (an "@" indicator gives a nil FeeRequest.Account); acct, when already
// fetched, saves a request.
func (s *Service) feeFor(ctx context.Context, op string, fee Money, r FeeRequest, account AccountID, acct *blnk.Balance) (Money, error) {
	if fee.isSet() {
		return fee, nil
	}
	if s.cfg.Fees == nil {
		return Money{}, invalid(op, "fee is required; pass ledger.NoFee for a free transaction, or set Config.Fees")
	}
	if !r.Amount.isSet() {
		return Money{}, invalid(op, "amount is required")
	}
	a, err := s.resolve(op, r.Amount, "")
	if err != nil {
		return Money{}, err
	}
	r.Amount = a
	if account != "" && !account.isIndicator() {
		if acct == nil {
			if acct, err = s.c.GetBalance(ctx, string(account)); err != nil {
				return Money{}, mapErr(op, err)
			}
		}
		if !strings.HasPrefix(acct.Indicator, "@") {
			r.Account = s.toAccount(acct)
		}
	}
	f, err := s.cfg.Fees(ctx, r)
	if err != nil {
		return Money{}, &OpError{Op: op, Kind: ErrFeePricing, Cause: err}
	}
	if !f.isSet() {
		return Money{}, &OpError{Op: op, Kind: ErrFeePricing, Cause: fmt.Errorf("Config.Fees returned no fee; return ledger.NoFee for a free transaction")}
	}
	if f.Currency != "" && !strings.EqualFold(f.Currency, a.Currency) {
		return Money{}, &OpError{Op: op, Kind: ErrFeePricing,
			Cause: fmt.Errorf("Config.Fees returned a %s fee for a %s transaction", strings.ToUpper(f.Currency), a.Currency)}
	}
	return f, nil
}

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

// ComputeFee applies rule to a resolved amount (one with Precision set, such
// as FeeRequest.Amount) and returns the fee in the same currency and
// precision. Use Service.ComputeFee for an amount as typed.
func ComputeFee(rule FeeRule, amount Money) (Money, error) {
	const op = "ledger.ComputeFee"
	if amount.Precision <= 0 {
		return Money{}, invalid(op, "amount has no precision; use FeeRequest.Amount or Service.ComputeFee")
	}
	a, err := amount.resolve(amount.Precision)
	if err != nil {
		return Money{}, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	fee, err := rule.compute(a)
	if err != nil {
		return Money{}, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	return Precise(fee, a.Precision, a.Currency), nil
}

// ComputeFee applies rule to amount as typed, using the currency's configured
// precision, and returns the fee ready to pass as Fee.
func (s *Service) ComputeFee(rule FeeRule, amount Money) (Money, error) {
	a, err := s.resolve("ledger.ComputeFee", amount, "")
	if err != nil {
		return Money{}, err
	}
	fee, err := ComputeFee(rule, a)
	if err == nil {
		fee.Display = s.Display(fee)
	}
	return fee, err
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
