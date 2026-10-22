package core

import (
	"context"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// Account converts a Blnk balance, remembering its currency.
func (k *Core) Account(b *blnk.Balance) (types.Account, error) {
	c, ok := currency.Lookup(b.Currency)
	if !ok {
		return types.Account{}, fmt.Errorf("humanblnk: account %s has unknown currency %q", b.BalanceID, b.Currency)
	}
	k.accounts.put(b.BalanceID, accountInfo{currency: c, system: b.Indicator != ""})
	total, err := money.FromMinor(&b.Balance.Int, c)
	if err != nil {
		return types.Account{}, err
	}
	available, err := money.FromMinor(b.Available(), c)
	if err != nil {
		return types.Account{}, err
	}
	held, err := money.FromMinor(&b.InflightDebitBalance.Int, c)
	if err != nil {
		return types.Account{}, err
	}
	return types.Account{
		ID: b.BalanceID, Ledger: b.LedgerID, Owner: b.IdentityID, Currency: c,
		Total: total, Available: available, Held: held,
		Meta: types.Meta(b.MetaData), CreatedAt: b.CreatedAt.Time,
	}, nil
}

// SameCurrency refuses m unless account a holds m's currency. System
// accounts always do: Blnk keeps one per currency.
func (k *Core) SameCurrency(ctx context.Context, a types.AccountRef, m money.Money) error {
	if a.IsSystem() {
		return nil
	}
	c, err := k.CurrencyOf(ctx, string(a))
	if err != nil {
		return err
	}
	if c != m.Currency() {
		return fmt.Errorf("%w: account %s holds %s, amount is %s", money.ErrCurrencyMismatch, a, c.Code(), m.Code())
	}
	return nil
}

// IsSystemAccount reports whether a is a system account: named "@…", or a
// balance with an "@…" indicator, which is how a recorded entry names one
// ("bln_…" for @World).
func (k *Core) IsSystemAccount(ctx context.Context, a types.AccountRef) (bool, error) {
	if a.IsSystem() {
		return true, nil
	}
	if info, ok := k.accounts.get(string(a)); ok {
		return info.system, nil
	}
	b, err := k.C.Balances.Get(ctx, string(a), blnk.GetBalanceOptions{})
	if err != nil {
		return false, MapErr(err)
	}
	if _, err := k.Account(b); err != nil { // caches it
		return false, err
	}
	return b.Indicator != "", nil
}

// CurrencyOf is the currency account id holds, from the cache or Blnk.
func (k *Core) CurrencyOf(ctx context.Context, id string) (currency.Currency, error) {
	if info, ok := k.accounts.get(id); ok {
		return info.currency, nil
	}
	b, err := k.C.Balances.Get(ctx, id, blnk.GetBalanceOptions{})
	if err != nil {
		return currency.Currency{}, MapErr(err)
	}
	a, err := k.Account(b)
	return a.Currency, err
}
