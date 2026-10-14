package ledger

import (
	"context"
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// TrialBalance is every balance in one currency, summed. In double entry the
// total is always zero; anything else means entries were lost or posted
// outside this ledger, and is worth an alert.
type TrialBalance struct {
	Currency string

	// System lists every system account (fee income, clearing, suspense, ...)
	// with its balance: the figures finance reconciles.
	System []*Account

	// CustomerFunds is what customer accounts (including pots and escrows)
	// hold in total: the money you owe your customers.
	CustomerFunds    Money
	CustomerAccounts int

	Net      Money // sum of everything; zero when balanced
	Balanced bool
}

// TrialBalance reads every balance in currency (100 per request).
func (s *Service) TrialBalance(ctx context.Context, currency string) (*TrialBalance, error) {
	const op = "ledger.TrialBalance"
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		return nil, invalid(op, "currency is required")
	}
	prec := s.Precision(cur)
	customers, net := new(big.Int), new(big.Int)
	tb := &TrialBalance{Currency: cur}
	for offset := 0; ; offset += 100 {
		bs, _, err := s.c.FilterBalances(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{{Field: "currency", Operator: "eq", Value: cur}},
			SortBy:  "created_at", SortOrder: "asc", Limit: 100, Offset: offset,
		})
		if err != nil {
			return nil, mapErr(op, err)
		}
		for i := range bs {
			b := &bs[i]
			net.Add(net, &b.Balance.Int)
			if strings.HasPrefix(b.Indicator, "@") {
				tb.System = append(tb.System, s.toAccount(b))
				continue
			}
			customers.Add(customers, &b.Balance.Int)
			tb.CustomerAccounts++
		}
		if len(bs) < 100 {
			break
		}
	}
	tb.CustomerFunds = Precise(customers, prec, cur)
	tb.Net = Precise(net, prec, cur)
	tb.Balanced = net.Sign() == 0
	return tb, nil
}
