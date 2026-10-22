package transactions

import (
	"context"
	"errors"
	"fmt"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/types"
	"github.com/otyang/humanblnk/money"
)

// Preview checks t as Record would, funds included, without moving anything
// or using up its reference.
func (s *Service) Preview(ctx context.Context, t types.Transaction) (types.Preview, error) {
	r, err := s.k.Request(ctx, t)
	if err != nil {
		return types.Preview{}, err
	}
	p, err := s.k.C.Transactions.Preview(ctx, r)
	if err != nil {
		return types.Preview{}, core.MapErr(err)
	}
	out := types.Preview{WouldApply: p.WouldApply, Notes: p.Notes}
	if rej := p.Rejection; rej != nil {
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
				return types.Preview{}, err
			}
		}
		out.Changes = append(out.Changes, c)
	}
	return out, nil
}
