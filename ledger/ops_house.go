package ledger

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// HouseAccount is one of the business's own accounts, such as "@FeeIncome",
// in one currency: the accounts finance reconciles against. "House" is the
// business itself; everything else in the ledger belongs to customers.
type HouseAccount struct {
	ID        string   `json:"id"` // Blnk balance id; read it with Ops().GetHouseAccount, never Wallet().GetAccount
	Indicator string   `json:"indicator"`
	LedgerID  LedgerID `json:"ledger_id"`
	Currency  string   `json:"currency"`
	Precision int64    `json:"precision"`

	Type   GLType     `json:"type"`   // from Chart
	Normal NormalSide `json:"normal"` // the side its balance normally sits on

	// Balances are as Blnk stores them: a debit-normal account (an asset or
	// expense) runs negative.
	Balances
	// Natural is Balance on the account's normal side: positive when a
	// debit-normal account holds a debit balance or a credit-normal one a
	// credit balance. Equal to Balance when Normal is unknown.
	Natural Money `json:"natural"`

	CreatedAt time.Time      `json:"created_at"`
	MetaData  map[string]any `json:"meta_data,omitempty"`
}

// Ref returns the HouseRef that names the account.
func (a *HouseAccount) Ref() HouseRef {
	return HouseRef{Indicator: a.Indicator, Currency: a.Currency}
}

// GetHouseAccount returns a house account in one currency.
func (s opsAPI) GetHouseAccount(ctx context.Context, ref HouseRef) (*HouseAccount, error) {
	b, err := s.loadHouse(ctx, "ledger.Ops.GetHouseAccount", ref)
	if err != nil {
		return nil, err
	}
	return s.toHouseAccount(b), nil
}

// ListHouseAccounts lists every house account in currency.
func (s opsAPI) ListHouseAccounts(ctx context.Context, currency string) ([]*HouseAccount, error) {
	const op = "ledger.Ops.ListHouseAccounts"
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		return nil, invalid(op, "currency is required")
	}
	var out []*HouseAccount
	err := s.eachBalance(ctx, op, cur, func(b *blnk.Balance) {
		if strings.HasPrefix(b.Indicator, "@") {
			out = append(out, s.toHouseAccount(b))
		}
	})
	return out, err
}

// HouseHistory lists a house account's transactions, newest first.
func (s opsAPI) HouseHistory(ctx context.Context, ref HouseRef, q HistoryQuery) (*HistoryPage, error) {
	const op = "ledger.Ops.HouseHistory"
	b, err := s.loadHouse(ctx, op, ref)
	if err != nil {
		return nil, err
	}
	return s.history(ctx, op, b.BalanceID, q)
}

// HouseBalanceAt returns the house account's posted balance at t, as Blnk stores it.
func (s opsAPI) HouseBalanceAt(ctx context.Context, ref HouseRef, t time.Time) (Money, error) {
	const op = "ledger.Ops.HouseBalanceAt"
	b, err := s.loadHouse(ctx, op, ref)
	if err != nil {
		return Money{}, err
	}
	return s.balanceAt(ctx, op, b.BalanceID, t)
}

// HouseStatement builds a statement for a house account.
func (s opsAPI) HouseStatement(ctx context.Context, ref HouseRef, from, to time.Time) (*Statement, error) {
	const op = "ledger.Ops.HouseStatement"
	b, err := s.loadHouse(ctx, op, ref)
	if err != nil {
		return nil, err
	}
	return s.statement(ctx, op, b, from, to)
}

// glDef is what indicator is, counting the accounts Config renames.
func (s *Service) glDef(indicator string) GLDef {
	switch indicator {
	case s.cfg.FeeIncomeAccount:
		return Chart[FeeIncomeAccount]
	case s.cfg.DepositClearingAccount:
		return Chart[DepositClearingAccount]
	case s.cfg.PayoutClearingAccount:
		return Chart[PayoutClearingAccount]
	}
	return ChartEntry(indicator)
}

func (s *Service) toHouseAccount(b *blnk.Balance) *HouseAccount {
	precision := s.precisionOf(b)
	def := s.glDef(b.Indicator)
	a := &HouseAccount{
		ID: b.BalanceID, Indicator: b.Indicator, LedgerID: LedgerID(b.LedgerID),
		Currency: b.Currency, Precision: precision, Type: def.Type, Normal: def.Normal,
		Balances: s.balances(b, precision), CreatedAt: b.CreatedAt.Time, MetaData: b.MetaData,
	}
	a.Natural = a.Balance
	if def.Normal == DebitNormal {
		a.Natural = s.money(new(big.Int).Neg(&b.Balance.Int), precision, b.Currency)
	}
	return a
}

// eachBalance calls fn for every balance in currency, 100 per request.
func (s *Service) eachBalance(ctx context.Context, op, cur string, fn func(*blnk.Balance)) error {
	for offset := 0; ; offset += 100 {
		bs, _, err := s.c.FilterBalances(ctx, blnk.FilterRequest{
			Filters: []blnk.Filter{{Field: "currency", Operator: "eq", Value: cur}},
			SortBy:  "created_at", SortOrder: "asc", Limit: 100, Offset: offset,
		})
		if err != nil {
			return mapErr(op, err)
		}
		for i := range bs {
			fn(&bs[i])
		}
		if len(bs) < 100 {
			return nil
		}
	}
}
