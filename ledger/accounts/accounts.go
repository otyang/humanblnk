// Package accounts creates and reads ledger accounts: Blnk balances, each
// money in one currency. Use it through ledger.Ledger.Accounts.
package accounts

import (
	"context"
	"hash/maphash"
	"sync"

	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

// Service creates and reads accounts.
type Service struct {
	k *core.Core

	// opening serialises OpenForCustomer calls for the same owner, ledger
	// and currency within this process, so they don't each open one.
	opening [64]sync.Mutex
	seed    maphash.Seed
}

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k: k, seed: maphash.MakeSeed()} }

// OpenForCustomer returns the customer's account in a.Currency, opening it
// the first time. A customer has at most one account per customer ledger and
// currency, so this is safe to repeat: every call for the same owner, ledger
// and currency returns the same account.
//
// Blnk can't enforce that by itself (balances take no reference). Calls in
// one process take turns; calls racing from different processes can each
// open one, and then all return the oldest. The others are left empty and
// marked with meta_data.duplicate_of.
func (s *Service) OpenForCustomer(ctx context.Context, a types.CustomerAccount) (types.Account, error) {
	if a.Ledger == "" {
		a.Ledger = s.k.AccountLedger
	}
	switch {
	case a.Owner == "":
		return types.Account{}, core.Invalid("a customer account needs an owner")
	case a.Ledger == "":
		return types.Account{}, core.Invalid("no ledger: set CustomerAccount.Ledger or WithAccountLedger")
	case !s.k.IsCustomerLedger(a.Ledger):
		return types.Account{}, core.Invalid("%s isn't a customer ledger (see WithCustomerLedgers); use CreateInternal", a.Ledger)
	case !a.Currency.Valid():
		return types.Account{}, money.ErrUnknownCurrency
	}
	mu := &s.opening[maphash.String(s.seed, a.Ledger+"\x00"+a.Owner+"\x00"+a.Currency.Code())%uint64(len(s.opening))]
	mu.Lock()
	defer mu.Unlock()

	if open, ok, err := s.oldest(ctx, a); err != nil || ok {
		return open, err
	}
	b, err := s.k.C.Balances.Create(ctx, blnk.CreateBalanceRequest{
		LedgerID: a.Ledger, IdentityID: a.Owner, Currency: a.Currency.Code(),
	})
	if err != nil {
		// The answer may be lost with the account made: look before failing.
		if open, ok, lerr := s.oldest(ctx, a); lerr == nil && ok {
			return open, nil
		}
		return types.Account{}, core.MapErr(err)
	}
	open, ok, err := s.oldest(ctx, a)
	if err != nil || !ok || open.ID == b.BalanceID {
		return s.k.Account(b)
	}
	// Another call opened one first: settle on that, and mark ours. Marking
	// is only a label, so a failure doesn't fail the open.
	_, _ = s.k.C.Metadata.Update(ctx, b.BalanceID, blnk.MetaData{"duplicate_of": open.ID})
	return open, nil
}

// oldest is the customer's first account matching a, if any.
func (s *Service) oldest(ctx context.Context, a types.CustomerAccount) (types.Account, bool, error) {
	bs, _, err := s.k.C.Balances.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{
			{Field: "ledger_id", Operator: "eq", Value: a.Ledger},
			{Field: "identity_id", Operator: "eq", Value: a.Owner},
			{Field: "currency", Operator: "eq", Value: a.Currency.Code()},
		},
		SortBy: "created_at", SortOrder: "asc", Limit: 1,
	})
	if err != nil {
		return types.Account{}, false, core.MapErr(err)
	}
	if len(bs) == 0 {
		return types.Account{}, false, nil
	}
	acct, err := s.k.Account(&bs[0])
	return acct, err == nil, err
}

// CreateInternal creates one of HumanBlnk's own accounts, such as an escrow pool,
// in a ledger that isn't a customer ledger; customer accounts are opened
// with OpenForCustomer. The owner is optional.
//
// It is not retried after a timeout (a repeat would create a second one):
// look the account up with List before trying again.
func (s *Service) CreateInternal(ctx context.Context, a types.NewAccount) (types.Account, error) {
	switch {
	case a.Ledger == "":
		return types.Account{}, core.Invalid("an internal account needs a ledger")
	case s.k.IsCustomerLedger(a.Ledger):
		return types.Account{}, core.Invalid("%s holds customer accounts: use OpenForCustomer", a.Ledger)
	case a.Ledger == core.GeneralLedger:
		return types.Account{}, core.Invalid("the General Ledger holds system accounts, which Blnk makes itself")
	case !a.Currency.Valid():
		return types.Account{}, money.ErrUnknownCurrency
	}
	b, err := s.k.C.Balances.Create(ctx, blnk.CreateBalanceRequest{
		LedgerID:   a.Ledger,
		IdentityID: a.Owner,
		Currency:   a.Currency.Code(),
		MetaData:   blnk.MetaData(a.Meta),
	})
	if err != nil {
		return types.Account{}, core.MapErr(err)
	}
	return s.k.Account(b)
}

// Get fetches an account, with its queued debits so Available is exact.
func (s *Service) Get(ctx context.Context, id string) (types.Account, error) {
	b, err := s.k.C.Balances.Get(ctx, id, blnk.GetBalanceOptions{WithQueued: true})
	if err != nil {
		return types.Account{}, core.MapErr(err)
	}
	return s.k.Account(b)
}

// List returns the accounts q selects and how many there are in all. Their
// Available doesn't count queued debits; use Get for that.
func (s *Service) List(ctx context.Context, q types.AccountQuery) ([]types.Account, int, error) {
	r := blnk.FilterRequest{Limit: q.Limit, Offset: q.Offset, IncludeCount: true, SortBy: "created_at", SortOrder: "desc"}
	if q.Ledger != "" {
		r.Filters = append(r.Filters, blnk.Filter{Field: "ledger_id", Operator: "eq", Value: q.Ledger})
	}
	if q.Owner != "" {
		r.Filters = append(r.Filters, blnk.Filter{Field: "identity_id", Operator: "eq", Value: q.Owner})
	}
	if q.Currency.Valid() {
		r.Filters = append(r.Filters, blnk.Filter{Field: "currency", Operator: "eq", Value: q.Currency.Code()})
	}
	bs, total, err := s.k.C.Balances.Filter(ctx, r)
	if err != nil {
		return nil, 0, core.MapErr(err)
	}
	out := make([]types.Account, len(bs))
	for i := range bs {
		if out[i], err = s.k.Account(&bs[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}
