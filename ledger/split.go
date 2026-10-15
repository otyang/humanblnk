package ledger

import (
	"context"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// SplitInput pays one amount out to several accounts in a single transaction,
// e.g. a marketplace order: seller 90%, platform 10%, delivery a fixed 500.
type SplitInput struct {
	From      AccountID // account debited: a customer account, or e.g. AccountID("@PaystackClearing") when collecting
	Amount    Money     // total debited from From
	Parts     []SplitPart
	Reference string

	// AllowOverdraft lets From go negative. Set it when From is a provider
	// clearing account receiving money from outside, as with Deposit.
	AllowOverdraft bool
	Pending        bool // create INFLIGHT; Confirm/Cancel later
	Description    string
	MetaData       map[string]any
}

// SplitPart is one recipient. Set exactly one of Amount, Percent or Rest.
type SplitPart struct {
	To        AccountID // customer account, or a system account as AccountID("@Name")
	Amount    Money     // fixed amount
	Percent   string    // share of the total, e.g. "10" for 10%
	Rest      bool      // whatever is left after the other parts (at most one part)
	Narration string
}

// Split records the payment as one Blnk transaction with one destination per
// part. Every part is converted to an exact amount here and sent as
// precise_distribution, so what each recipient gets is decided by this code,
// not by rounding on the server: percentage parts round down, and the dust
// goes to the Rest part (or, without one, to the first percentage part).
func (s paymentsAPI) Split(ctx context.Context, in SplitInput) (*Receipt, error) {
	const op = "ledger.Payments.Split"
	total, _, err := s.prepare(op, in.From, in.Amount, Money{}, in.Reference)
	if err != nil {
		return nil, err
	}
	amounts, err := s.splitAmounts(op, total, in.Parts)
	if err != nil {
		return nil, err
	}
	to := make([]AccountID, 0, len(in.Parts))
	dests := make([]blnk.Distribution, len(in.Parts))
	for i, p := range in.Parts {
		to = append(to, p.To)
		dests[i] = blnk.Distribution{
			Identifier:          string(p.To),
			PreciseDistribution: amounts[i].String(),
			Narration:           firstNonEmpty(p.Narration, in.Description),
		}
	}
	if err := s.checkActive(ctx, op, []AccountID{in.From}, to...); err != nil {
		return nil, err
	}
	if err := s.checkLimits(ctx, op, in.From, total.PreciseAmount); err != nil {
		return nil, err
	}
	req := s.baseReq(total, in.Reference, in.Description, in.MetaData, "split")
	req.Source = string(in.From)
	req.Destinations = dests
	req.AllowOverdraft = in.AllowOverdraft
	req.Inflight = in.Pending
	return s.post(ctx, op, req)
}

func (s *Service) splitAmounts(op string, total Money, parts []SplitPart) ([]*big.Int, error) {
	if len(parts) < 2 {
		return nil, invalid(op, "a split needs at least two parts")
	}
	out := make([]*big.Int, len(parts))
	sum := new(big.Int)
	rest, firstPct := -1, -1
	for i, p := range parts {
		if strings.TrimSpace(string(p.To)) == "" {
			return nil, invalid(op, "part %d: recipient is required", i)
		}
		set := 0
		for _, b := range []bool{p.Amount.isSet(), p.Percent != "", p.Rest} {
			if b {
				set++
			}
		}
		if set != 1 {
			return nil, invalid(op, "part %d: set exactly one of Amount, Percent or Rest", i)
		}
		switch {
		case p.Rest:
			if rest >= 0 {
				return nil, invalid(op, "only one part can take the rest")
			}
			rest = i
			continue
		case p.Percent != "":
			pct, ok := new(big.Rat).SetString(strings.TrimSuffix(strings.TrimSpace(p.Percent), "%"))
			if !ok || pct.Sign() <= 0 || pct.Cmp(big.NewRat(100, 1)) > 0 {
				return nil, invalid(op, "part %d: invalid percent %q", i, p.Percent)
			}
			v := new(big.Rat).SetInt(total.PreciseAmount)
			v.Mul(v, pct).Quo(v, big.NewRat(100, 1))
			out[i] = roundRat(v, RoundDown)
			if firstPct < 0 {
				firstPct = i
			}
		default:
			if p.Amount.Currency != "" && !strings.EqualFold(p.Amount.Currency, total.Currency) {
				return nil, invalid(op, "part %d: currency %s differs from %s", i, p.Amount.Currency, total.Currency)
			}
			r, err := s.resolveAt(op, p.Amount, total.Currency, total.Precision)
			if err != nil {
				return nil, err
			}
			out[i] = r.PreciseAmount
		}
		if out[i].Sign() <= 0 {
			return nil, invalid(op, "part %d: amount must be positive", i)
		}
		sum.Add(sum, out[i])
	}

	left := new(big.Int).Sub(total.PreciseAmount, sum)
	switch {
	case left.Sign() < 0:
		return nil, invalid(op, "parts add up to more than %s", total)
	case rest >= 0:
		if left.Sign() == 0 {
			return nil, invalid(op, "nothing is left for the rest part")
		}
		out[rest] = left
	case left.Sign() > 0 && firstPct >= 0 && left.Cmp(big.NewInt(int64(len(parts)))) < 0:
		out[firstPct].Add(out[firstPct], left) // rounding dust from percentages
	case left.Sign() > 0:
		return nil, invalid(op, "parts add up to %s, not %s; add a Rest part or fix the amounts",
			formatDecimal(sum, total.Precision), total)
	}
	return out, nil
}
