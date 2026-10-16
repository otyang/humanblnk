package ledger

import (
	"math/big"
	"strings"
)

// DisplayRule says how a currency's amounts are shown to people. Amounts are
// cut (never rounded up) to Decimals, so a displayed balance can always be
// spent in full; the exact amount stays in Money.Value.
//
// Fiat style pads to a fixed number of places ("100.00"); crypto style trims
// trailing zeros ("0.5", "0.456789").
type DisplayRule struct {
	Decimals  int  // most decimal places shown; capped at the currency's precision
	TrimZeros bool // drop trailing zeros instead of padding to Decimals
}

// BuiltinDisplay is used for currencies not in Config.Display. Currencies in
// neither show every decimal place their precision has, padded (fiat style).
var BuiltinDisplay = map[string]DisplayRule{
	"BTC": {Decimals: 8, TrimZeros: true}, "LTC": {Decimals: 8, TrimZeros: true}, "BCH": {Decimals: 8, TrimZeros: true},
	"ETH":  {Decimals: 6, TrimZeros: true},
	"SOL":  {Decimals: 4, TrimZeros: true},
	"TRX":  {Decimals: 6, TrimZeros: true},
	"USDT": {Decimals: 2}, "USDC": {Decimals: 2}, // stablecoins read as money
}

// Format renders m by this rule. m must be resolved (PreciseAmount and
// Precision set), as every Money the Service returns is.
func (r DisplayRule) Format(m Money) string {
	if m.PreciseAmount == nil || m.Precision <= 0 {
		return m.Value
	}
	dp, ok := decimalPlaces(m.Precision)
	if !ok {
		return m.Value
	}
	keep := r.Decimals
	if keep < 0 {
		keep = 0
	}
	if keep > dp {
		keep = dp
	}
	// Cut toward zero to keep decimals: divide by 10^(dp-keep).
	cut := new(big.Int).Quo(m.PreciseAmount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dp-keep)), nil))
	scale := int64(1)
	for i := 0; i < keep; i++ {
		scale *= 10
	}
	out := fixedDecimal(cut, scale)
	if r.TrimZeros && strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	if strings.Trim(out, "-0.") == "" {
		out = strings.TrimPrefix(out, "-") // no "-0.00"
	}
	return out
}

// DisplayRule returns the rule used for currency: Config.Display, then
// BuiltinDisplay, then every decimal place of its precision, padded.
func (s *Service) DisplayRule(currency string) DisplayRule {
	cur := strings.ToUpper(currency)
	if r, ok := s.cfg.Display[cur]; ok {
		return r
	}
	if r, ok := BuiltinDisplay[cur]; ok {
		return r
	}
	dp, _ := decimalPlaces(s.Precision(cur))
	return DisplayRule{Decimals: dp}
}

// Display returns m formatted for people by its currency's rule.
func (s *Service) Display(m Money) string { return s.DisplayRule(m.Currency).Format(m) }

// money builds a resolved Money with its display string.
func (s *Service) money(precise *big.Int, precision int64, currency string) Money {
	m := Precise(precise, precision, currency)
	m.Display = s.Display(m)
	return m
}
