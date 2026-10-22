package core

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

// GetEntry fetches an entry by id. A split isn't stored as one transaction:
// Blnk records one per posting, each naming the split as its parent, so a
// missing id is looked up as a parent and its postings gathered.
//
// An entry Blnk rolled back (a split or atomic batch whose later part
// failed) is StatusRejected, though Blnk still calls it APPLIED.
func (k *Core) GetEntry(ctx context.Context, id string) (types.Entry, error) {
	tx, err := k.C.Transactions.Get(ctx, id)
	if err == nil {
		e, err := Entry(tx)
		if err != nil {
			return types.Entry{}, err
		}
		return k.MarkRolledBack(ctx, e, id)
	}
	if !blnk.IsNotFound(err) {
		return types.Entry{}, MapErr(err)
	}
	children, _, ferr := k.C.Transactions.Filter(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{{Field: "parent_transaction", Operator: "eq", Value: id}},
		Limit:   100,
	})
	if ferr != nil {
		return types.Entry{}, MapErr(ferr)
	}
	if len(children) == 0 {
		return types.Entry{}, MapErr(err)
	}
	e, err := Gather(id, children)
	if err != nil {
		return types.Entry{}, err
	}
	ids := make([]string, len(children))
	for i := range children {
		ids[i] = children[i].GetID()
	}
	return k.MarkRolledBack(ctx, e, ids...)
}

// BaseRef is ref without the "-n" Blnk adds to each posting of a split.
func BaseRef(ref string) string { return splitSuffix.ReplaceAllString(ref, "") }

// MarkRolledBack makes e StatusRejected if Blnk rolled back any of the
// transactions ids. A rollback leaves the applied transaction in place and
// refunds it under "<id>_refund". The ledger never uses Blnk's refund
// itself (a reversal is a transaction of its own), so any such refund is a
// rollback. It copies the original's metadata, so that can't tell them apart.
func (k *Core) MarkRolledBack(ctx context.Context, e types.Entry, ids ...string) (types.Entry, error) {
	if e.Status != types.StatusApplied {
		return e, nil
	}
	for _, id := range ids {
		_, err := k.C.Transactions.GetByReference(ctx, id+"_refund")
		if blnk.IsNotFound(err) {
			continue
		}
		if err != nil {
			return types.Entry{}, MapErr(err)
		}
		e.Status = types.StatusRejected
		return e, nil
	}
	return e, nil
}

// Matches reports whether e moves the same amounts through the same
// accounts as t, in any order. System accounts in t are named "@…", but a
// recorded entry names them by balance id, so those are looked up first.
func (k *Core) Matches(ctx context.Context, e types.Entry, t types.Transaction) (bool, error) {
	from, err := k.balances(ctx, t.From)
	if err != nil {
		return false, err
	}
	to, err := k.balances(ctx, t.To)
	if err != nil {
		return false, err
	}
	return samePostings(e.From, from) && samePostings(e.To, to), nil
}

// balances is ps with each "@…" account replaced by its balance id.
func (k *Core) balances(ctx context.Context, ps []types.Posting) ([]types.Posting, error) {
	out := make([]types.Posting, len(ps))
	for i, p := range ps {
		out[i] = p
		if !p.Account.IsSystem() {
			continue
		}
		b, err := k.C.Balances.GetByIndicator(ctx, string(p.Account), p.Amount.Code())
		if err != nil {
			return nil, MapErr(err)
		}
		out[i].Account = types.AccountRef(b.BalanceID)
	}
	return out, nil
}

// samePostings reports whether a and b move the same amounts through the
// same accounts, in any order.
func samePostings(a, b []types.Posting) bool {
	ta, tb := totals(a), totals(b)
	if ta == nil || tb == nil || len(ta) != len(tb) {
		return false
	}
	for acct, m := range ta {
		if !m.Equal(tb[acct]) {
			return false
		}
	}
	return true
}

// totals adds up ps by account, or is nil if they don't add up.
func totals(ps []types.Posting) map[types.AccountRef]money.Money {
	out := make(map[types.AccountRef]money.Money, len(ps))
	for _, p := range ps {
		prev, ok := out[p.Account]
		if !ok {
			out[p.Account] = p.Amount
			continue
		}
		sum, err := prev.Add(p.Amount)
		if err != nil {
			return nil
		}
		out[p.Account] = sum
	}
	return out
}

// Existing is what to return when Blnk refuses t's reference as already
// used: t's entry with ErrDuplicate if that entry is t, applied, or
// ErrReferenceUsed if the reference holds anything else.
func (k *Core) Existing(ctx context.Context, t types.Transaction) (types.Entry, error) {
	e, err := k.GetEntryByReference(ctx, t.Ref)
	if err != nil {
		// Not ErrDuplicate: that would tell the caller the money moved.
		return types.Entry{}, fmt.Errorf("ledger: reference %s is already used, and looking it up failed (check again before retrying): %w", t.Ref, err)
	}
	same, err := k.Matches(ctx, e, t)
	if err != nil {
		return types.Entry{}, fmt.Errorf("ledger: reference %s is already used, and checking it failed (check again before retrying): %w", t.Ref, err)
	}
	if !same || e.Status != types.StatusApplied {
		return types.Entry{}, fmt.Errorf("%w: %s is %s %s, not this transaction applied", types.ErrReferenceUsed, t.Ref, e.ID, e.Status)
	}
	return e, fmt.Errorf("%w: %s is %s", types.ErrDuplicate, t.Ref, e.ID)
}

// GetEntryByReference fetches the entry recorded under ref.
func (k *Core) GetEntryByReference(ctx context.Context, ref string) (types.Entry, error) {
	tx, err := k.C.Transactions.GetByReference(ctx, ref)
	if err == nil {
		return k.GetEntry(ctx, tx.GetID()) // the reference lookup returns only some fields
	}
	if !blnk.IsNotFound(err) {
		return types.Entry{}, MapErr(err)
	}
	// A split's postings carry ref-1, ref-2, … and name the split as parent.
	// A batch's items name the batch as parent too, so check that what is
	// gathered is the split recorded under ref, not a batch holding "ref-1".
	first, ferr := k.C.Transactions.GetByReference(ctx, ref+"-1")
	if ferr != nil || first.ParentTransaction == "" {
		return types.Entry{}, MapErr(err)
	}
	e, gerr := k.GetEntry(ctx, first.ParentTransaction)
	if errors.Is(gerr, errNotASplit) || (gerr == nil && e.Ref != ref) {
		return types.Entry{}, MapErr(err)
	}
	return e, gerr
}

// errNotASplit is transactions gathered under one parent that aren't the
// postings of one split, such as the items of a batch.
var errNotASplit = errors.New("ledger: not the postings of one split")

// MetaSplitPostings is the metadata key Request stamps a split with: how
// many postings it has, so a split missing some reads as not applied.
const MetaSplitPostings = "ledger_split_postings"

// Entry converts one Blnk transaction.
func Entry(tx *blnk.Transaction) (types.Entry, error) {
	amount, err := FromBlnk(&tx.PreciseAmount.Int, tx.Currency)
	if err != nil {
		return types.Entry{}, err
	}
	if err := CheckPrecision(tx.GetID(), tx.Precision, amount.Currency()); err != nil {
		return types.Entry{}, err
	}
	e := types.Entry{
		ID: tx.GetID(), Ref: tx.Reference, Status: tx.Status, Amount: amount,
		Description: tx.Description, Meta: types.Meta(tx.MetaData), CreatedAt: tx.CreatedAt.Time,
	}
	if e.From, err = side(tx.Sources, tx.Source, amount); err != nil {
		return types.Entry{}, err
	}
	if e.To, err = side(tx.Destinations, tx.Destination, amount); err != nil {
		return types.Entry{}, err
	}
	return e, nil
}

// side is one side of a transaction: its split postings, or the one account.
func side(ds []blnk.Distribution, account string, amount money.Money) ([]types.Posting, error) {
	if len(ds) == 0 {
		return types.Single(types.AccountRef(account), amount), nil
	}
	out := make([]types.Posting, len(ds))
	for i, d := range ds {
		minor, ok := new(big.Int).SetString(d.PreciseDistribution, 10)
		if !ok {
			return nil, errors.New("ledger: blnk returned a split posting without an exact amount")
		}
		m, err := money.FromMinor(minor, amount.Currency())
		if err != nil {
			return nil, err
		}
		out[i] = types.Posting{Account: types.AccountRef(d.Identifier), Amount: m}
	}
	return out, nil
}

// splitSuffix is the "-n" Blnk adds to each posting's reference in a split.
var splitSuffix = regexp.MustCompile(`-[0-9]+$`)

// Gather rebuilds a split from the transactions Blnk recorded for its postings.
func Gather(id string, children []blnk.Transaction) (types.Entry, error) {
	e := types.Entry{ID: id}
	from, to := map[types.AccountRef]money.Money{}, map[types.AccountRef]money.Money{}
	var order struct{ from, to []types.AccountRef }
	for i := range children {
		c, err := Entry(&children[i])
		if err != nil {
			return types.Entry{}, err
		}
		if i == 0 {
			e.Ref = splitSuffix.ReplaceAllString(c.Ref, "")
			e.Status, e.Description, e.Meta, e.CreatedAt = c.Status, c.Description, c.Meta, c.CreatedAt
			e.Amount = c.Amount
		} else if e.Amount, err = e.Amount.Add(c.Amount); err != nil {
			return types.Entry{}, err
		}
		if BaseRef(c.Ref) != e.Ref {
			return types.Entry{}, fmt.Errorf("%w: %s holds %s and %s", errNotASplit, id, e.Ref, BaseRef(c.Ref))
		}
		if c.Status != e.Status {
			e.Status = "" // mixed: look at the postings' own entries
		}
		if err := addTo(from, &order.from, c.From[0]); err != nil {
			return types.Entry{}, err
		}
		if err := addTo(to, &order.to, c.To[0]); err != nil {
			return types.Entry{}, err
		}
	}
	for _, a := range order.from {
		e.From = append(e.From, types.Posting{Account: a, Amount: from[a]})
	}
	for _, a := range order.to {
		e.To = append(e.To, types.Posting{Account: a, Amount: to[a]})
	}
	// When a later posting fails, Blnk never records it and rolls back the
	// earlier ones a moment later; until then they look applied.
	if want, _ := e.Meta[MetaSplitPostings].(string); want != "" && want != strconv.Itoa(len(children)) && e.Status == types.StatusApplied {
		e.Status = types.StatusRejected
	}
	return e, nil
}

func addTo(totals map[types.AccountRef]money.Money, order *[]types.AccountRef, p types.Posting) error {
	prev, ok := totals[p.Account]
	if !ok {
		totals[p.Account] = p.Amount
		*order = append(*order, p.Account)
		return nil
	}
	sum, err := prev.Add(p.Amount)
	totals[p.Account] = sum
	return err
}
