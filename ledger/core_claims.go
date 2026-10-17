package ledger

import (
	"context"
	"math/big"

	"github.com/otyang/humanblnk/blnk"
)

// A posted payment can be given back to its payer three ways: Refund (Blnk's
// full reversal), RefundPartial, and an upheld dispute. Each checks what the
// others have already claimed, so the payer is never paid back more than
// they paid.

// claims is what has already been claimed back on one payment leg.
type claims struct {
	reversed bool     // reversed in full by Refund
	refunded *big.Int // partial refunds linked by refund_of
	disputed *big.Int // open disputes in full, upheld ones by what was committed
}

// total is everything claimed back, given the leg's amount.
func (c claims) total(amount *big.Int) *big.Int {
	if c.reversed {
		return new(big.Int).Set(amount)
	}
	return new(big.Int).Add(c.refunded, c.disputed)
}

// claimsOn reads what has been claimed back on orig. The partial refund and
// the dispute with reference except are left out, so retrying either replays
// rather than counting itself.
func (s *Service) claimsOn(ctx context.Context, orig *blnk.Transaction, except string) (claims, error) {
	var c claims
	var err error
	if c.reversed, err = s.reversed(ctx, orig); err != nil {
		return c, err
	}
	if c.refunded, err = s.refundedSoFar(ctx, orig, except); err != nil {
		return c, err
	}
	c.disputed, err = s.disputedSoFar(ctx, orig, except)
	return c, err
}

// disputedSoFar sums the disputes held against orig: each dispute hold's
// amount minus whatever was released back to the recipient (its VOID
// child). An open dispute counts in full; an upheld one by what went to the
// payer; a rejected one not at all.
func (s *Service) disputedSoFar(ctx context.Context, orig *blnk.Transaction, except string) (*big.Int, error) {
	holds, err := s.filterAll(ctx, []blnk.Filter{{Field: metaField("dispute_of"), Operator: "eq", Value: orig.GetID()}})
	if err != nil {
		return nil, err
	}
	sum := new(big.Int)
	for i := range holds {
		h := &holds[i]
		if h.Reference == except || h.Reference == except+"_q" || h.Status == blnk.StatusRejected {
			continue
		}
		if h.Status != blnk.StatusInflight {
			// A commit or void child that copied the hold's metadata, or
			// the hold itself once Blnk has settled it: only a hold has no
			// parent, and Blnk's filter leaves parent_transaction empty, so
			// read it back.
			full, err := s.c.GetTransaction(ctx, h.GetID())
			if err != nil {
				return nil, err
			}
			if full.ParentTransaction != "" || !full.Inflight {
				continue
			}
		}
		sum.Add(sum, &h.PreciseAmount.Int)
		voids, err := s.filterAll(ctx, []blnk.Filter{
			{Field: "parent_transaction", Operator: "eq", Value: h.GetID()},
			{Field: "status", Operator: "eq", Value: blnk.StatusVoid},
		})
		if err != nil {
			return nil, err
		}
		for _, v := range voids {
			sum.Sub(sum, &v.PreciseAmount.Int)
		}
	}
	if sum.Sign() < 0 {
		sum.SetInt64(0)
	}
	return sum, nil
}

// filterAll returns every transaction matching fs, oldest first.
func (s *Service) filterAll(ctx context.Context, fs []blnk.Filter) ([]blnk.Transaction, error) {
	var out []blnk.Transaction
	for offset := 0; ; offset += 100 {
		page, _, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
			Filters: fs, SortBy: "created_at", SortOrder: "asc", Limit: 100, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 100 {
			return out, nil
		}
	}
}
