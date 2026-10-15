package ledger

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// SystemAccount is a system (GL) account such as "@FeeIncome" in one
// currency: the accounts finance reconciles against.
type SystemAccount struct {
	ID        string   `json:"id"` // Blnk balance id; read it with System(), never Accounts()
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

// Ref returns the SystemRef that names the account.
func (a *SystemAccount) Ref() SystemRef {
	return SystemRef{Indicator: a.Indicator, Currency: a.Currency}
}

// Get returns a system account in one currency.
func (s systemAPI) Get(ctx context.Context, ref SystemRef) (*SystemAccount, error) {
	b, err := s.loadSystem(ctx, "ledger.System.Get", ref)
	if err != nil {
		return nil, err
	}
	return s.toSystemAccount(b), nil
}

// List lists every system account in currency.
func (s systemAPI) List(ctx context.Context, currency string) ([]*SystemAccount, error) {
	const op = "ledger.System.List"
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		return nil, invalid(op, "currency is required")
	}
	var out []*SystemAccount
	err := s.eachBalance(ctx, op, cur, func(b *blnk.Balance) {
		if strings.HasPrefix(b.Indicator, "@") {
			out = append(out, s.toSystemAccount(b))
		}
	})
	return out, err
}

// History lists a system account's transactions, newest first.
func (s systemAPI) History(ctx context.Context, ref SystemRef, q HistoryQuery) (*HistoryPage, error) {
	const op = "ledger.System.History"
	b, err := s.loadSystem(ctx, op, ref)
	if err != nil {
		return nil, err
	}
	return s.history(ctx, op, b.BalanceID, q)
}

// BalanceAt returns the system account's posted balance at t, as Blnk stores it.
func (s systemAPI) BalanceAt(ctx context.Context, ref SystemRef, t time.Time) (Money, error) {
	const op = "ledger.System.BalanceAt"
	b, err := s.loadSystem(ctx, op, ref)
	if err != nil {
		return Money{}, err
	}
	return s.balanceAt(ctx, op, b.BalanceID, t)
}

// Statement builds a statement for a system account.
func (s systemAPI) Statement(ctx context.Context, ref SystemRef, from, to time.Time) (*Statement, error) {
	const op = "ledger.System.Statement"
	b, err := s.loadSystem(ctx, op, ref)
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

func (s *Service) toSystemAccount(b *blnk.Balance) *SystemAccount {
	precision := s.Precision(b.Currency)
	def := s.glDef(b.Indicator)
	a := &SystemAccount{
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
