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

	// House lists every house account (fee income, clearing, suspense, ...)
	// with its balance and its type from Chart: the figures finance reconciles.
	House []*HouseAccount

	// CustomerFunds is what customer accounts (including pots and escrows)
	// hold in total: the money you owe your customers.
	CustomerFunds    Money
	CustomerAccounts int

	Net      Money // sum of everything; zero when balanced
	Balanced bool
}

// TrialBalance reads every balance in currency (100 per request).
func (s opsAPI) TrialBalance(ctx context.Context, currency string) (*TrialBalance, error) {
	const op = "ledger.Ops.TrialBalance"
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		return nil, invalid(op, "currency is required")
	}
	prec := s.Precision(cur)
	customers, net := new(big.Int), new(big.Int)
	tb := &TrialBalance{Currency: cur}
	err := s.eachBalance(ctx, op, cur, func(b *blnk.Balance) {
		net.Add(net, &b.Balance.Int)
		if strings.HasPrefix(b.Indicator, "@") {
			tb.House = append(tb.House, s.toHouseAccount(b))
			return
		}
		customers.Add(customers, &b.Balance.Int)
		tb.CustomerAccounts++
	})
	if err != nil {
		return nil, err
	}
	tb.CustomerFunds = s.money(customers, prec, cur)
	tb.Net = s.money(net, prec, cur)
	tb.Balanced = net.Sign() == 0
	return tb, nil
}
