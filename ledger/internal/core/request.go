package core

import (
	"context"
	"fmt"
	"strconv"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

// Validate checks t on its own, before any account is looked up, and
// returns its total.
func Validate(t types.Transaction) (money.Money, error) {
	switch {
	case t.Ref == "":
		return money.Money{}, Invalid("a reference is required")
	case t.Description == "":
		return money.Money{}, Invalid("a description is required")
	case len(t.From) == 0 || len(t.To) == 0:
		return money.Money{}, Invalid("a transaction needs From and To postings")
	case len(t.From) > 1 && len(t.To) > 1:
		return money.Money{}, Invalid("Blnk moves one account to many or many to one, not many to many")
	}
	from, err := sum(t.From)
	if err != nil {
		return money.Money{}, err
	}
	to, err := sum(t.To)
	if err != nil {
		return money.Money{}, err
	}
	if cmp, err := from.Cmp(to); err != nil {
		return money.Money{}, err
	} else if cmp != 0 {
		return money.Money{}, fmt.Errorf("%w: from %s %s, to %s %s", types.ErrUnbalanced, from, from.Code(), to, to.Code())
	}
	return from, nil
}

// sum adds up postings, which must be positive and in one currency.
func sum(ps []types.Posting) (money.Money, error) {
	var total money.Money
	for i, p := range ps {
		if p.Account == "" {
			return money.Money{}, Invalid("posting %d has no account", i)
		}
		if !p.Amount.Valid() {
			return money.Money{}, fmt.Errorf("posting %d: %w", i, money.ErrUnknownCurrency)
		}
		if !p.Amount.IsPositive() {
			return money.Money{}, Invalid("posting %d: amount %s must be positive", i, p.Amount)
		}
		if i == 0 {
			total = p.Amount
			continue
		}
		var err error
		if total, err = total.Add(p.Amount); err != nil {
			return money.Money{}, fmt.Errorf("posting %d: %w", i, err)
		}
	}
	return total, nil
}

// Request turns t into Blnk's request, after checking each account holds
// t's currency.
func (k *Core) Request(ctx context.Context, t types.Transaction) (blnk.CreateTransactionRequest, error) {
	total, err := Validate(t)
	if err != nil {
		return blnk.CreateTransactionRequest{}, err
	}
	for _, p := range append(append([]types.Posting{}, t.From...), t.To...) {
		if err := k.SameCurrency(ctx, p.Account, total); err != nil {
			return blnk.CreateTransactionRequest{}, err
		}
	}
	overdraft, err := k.fromSystemOnly(ctx, t)
	if err != nil {
		return blnk.CreateTransactionRequest{}, err
	}
	minor, precision, err := ToBlnk(total)
	if err != nil {
		return blnk.CreateTransactionRequest{}, err
	}
	r := blnk.CreateTransactionRequest{
		Reference:      t.Ref,
		PreciseAmount:  minor,
		Precision:      precision,
		Currency:       total.Code(),
		Description:    t.Description,
		SkipQueue:      true,
		AllowOverdraft: overdraft,
		MetaData:       blnk.MetaData(t.Meta),
	}
	if split := max(len(t.From), len(t.To)); split > 1 {
		// Blnk copies this onto every posting; Gather counts them against it.
		md := make(blnk.MetaData, len(t.Meta)+1)
		for k, v := range t.Meta {
			md[k] = v
		}
		md[MetaSplitPostings] = strconv.Itoa(postings(t.From, t.To))
		r.MetaData = md
	}
	switch {
	case len(t.To) > 1:
		r.Source = string(t.From[0].Account)
		r.Destinations, err = distributions(t.To)
		r.Atomic = true
	case len(t.From) > 1:
		r.Destination = string(t.To[0].Account)
		r.Sources, err = distributions(t.From)
		r.Atomic = true
	default:
		r.Source, r.Destination = string(t.From[0].Account), string(t.To[0].Account)
	}
	return r, err
}

// fromSystemOnly reports whether every account t takes money from is a
// system account. Those go below zero by design (@World is what HumanBlnk has
// taken in from outside, a clearing account what a provider owes), and Blnk
// refuses to let any balance do so unless the transaction allows it. A
// customer account never may: if one is a source, so is the funds check.
// A system account named by its balance id, as a recorded entry names it,
// counts too, so reversing a payout can take from @World.
func (k *Core) fromSystemOnly(ctx context.Context, t types.Transaction) (bool, error) {
	for _, p := range t.From {
		sys, err := k.IsSystemAccount(ctx, p.Account)
		if err != nil || !sys {
			return false, err
		}
	}
	return true, nil
}

// postings is how many postings Blnk makes of a split: one per account on
// its split side, since it merges an account named twice.
func postings(from, to []types.Posting) int {
	side := to
	if len(from) > 1 {
		side = from
	}
	seen := map[types.AccountRef]bool{}
	for _, p := range side {
		seen[p.Account] = true
	}
	return len(seen)
}

// distributions is the split side of a request, each an exact amount.
func distributions(ps []types.Posting) ([]blnk.Distribution, error) {
	out := make([]blnk.Distribution, len(ps))
	for i, p := range ps {
		minor, _, err := ToBlnk(p.Amount)
		if err != nil {
			return nil, err
		}
		out[i] = blnk.Distribution{Identifier: string(p.Account), PreciseDistribution: minor.String()}
	}
	return out, nil
}
