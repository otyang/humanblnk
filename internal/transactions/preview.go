package transactions

import (
	"context"
	"errors"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/money"
)

// Preview checks t as Record would, funds included, without moving anything
// or using up its reference.
func (s *Service) Preview(ctx context.Context, t types.Transaction) (types.Preview, error) {
	r, err := s.k.Request(ctx, t)
	if err != nil {
		return types.Preview{}, err
	}
	reqs := []blnk.CreateTransactionRequest{r}
	if items, ok, err := s.k.PerSource(ctx, r); err != nil {
		return types.Preview{}, err
	} else if ok {
		reqs = items // as Record will send it: one transaction per source
	}
	out := types.Preview{WouldApply: true}
	changes := map[types.AccountRef]int{} // index in out.Changes
	for _, r := range reqs {
		if err := s.preview(ctx, r, &out, changes); err != nil {
			return types.Preview{}, err
		}
	}
	return out, nil
}

// preview adds what r would do to out. Each request of a per-source split is
// previewed on its own, so an account several of them touch (the split's
// destination) adds up their changes.
func (s *Service) preview(ctx context.Context, r blnk.CreateTransactionRequest, out *types.Preview, changes map[types.AccountRef]int) error {
	p, err := s.k.C.Transactions.Preview(ctx, r)
	if err != nil {
		return core.MapErr(err)
	}
	out.WouldApply = out.WouldApply && p.WouldApply
	out.Notes = append(out.Notes, p.Notes...)
	if rej := p.Rejection; rej != nil && out.Rejection == nil {
		reason := errors.New(rej.Message)
		if mapped := core.CodeErr(rej.Code); mapped != nil {
			reason = fmt.Errorf("%w: %s", mapped, rej.Message)
		}
		out.Rejection = reason
	}
	for _, b := range p.Balances {
		c := types.AccountChange{Account: types.AccountRef(b.BalanceID)}
		for _, v := range []struct {
			to   *money.Money
			from blnk.BigInt
		}{
			{&c.Before, b.CurrentBalance}, {&c.After, b.ResultingBalance},
			{&c.AvailableBefore, b.CurrentAvailable}, {&c.AvailableAfter, b.ResultingAvailable},
		} {
			if *v.to, err = core.FromBlnk(&v.from.Int, b.Currency); err != nil {
				return err
			}
		}
		i, seen := changes[c.Account]
		if !seen {
			changes[c.Account] = len(out.Changes)
			out.Changes = append(out.Changes, c)
			continue
		}
		// Touched by an earlier request too: add this one's change to it.
		prev := &out.Changes[i]
		for _, v := range []struct{ total, before, after *money.Money }{
			{&prev.After, &c.Before, &c.After}, {&prev.AvailableAfter, &c.AvailableBefore, &c.AvailableAfter},
		} {
			delta, err := v.after.Sub(*v.before)
			if err != nil {
				return err
			}
			if *v.total, err = v.total.Add(delta); err != nil {
				return err
			}
		}
	}
	return nil
}
