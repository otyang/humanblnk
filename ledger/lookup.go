package ledger

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// How Blnk stores a transaction with several destinations (every movement
// with a fee is one): it answers the create call with a parent transaction,
// but stores only the legs, one per destination, referenced "<ref>-1",
// "<ref>-2", … (Core 0.15.x; "<ref>_1" in later versions), each carrying
// parent_transaction = the parent's id. Looking the parent up by id or by
// reference returns 404. Committing or voiding by the parent id acts on every leg.
//
// The lookups below rebuild such a parent from its legs, so replays,
// Transactions().Get and GetByReference work for split transactions too.

// maxLegs bounds the leg probe; a split never has more destinations than this.
const maxLegs = 100

// byReference finds a transaction by reference: as given, then with the "_q"
// suffix the queue writes, then as a split parent rebuilt from its legs.
func (s *Service) byReference(ctx context.Context, ref string) (*blnk.Transaction, error) {
	t, err := s.c.GetTransactionByReference(ctx, ref)
	if !blnk.IsNotFound(err) {
		return t, err
	}
	if !strings.HasSuffix(ref, "_q") {
		q, qerr := s.c.GetTransactionByReference(ctx, ref+"_q")
		if qerr == nil {
			return q, nil
		}
		if !blnk.IsNotFound(qerr) {
			return nil, qerr
		}
	}
	for _, sep := range []string{"-", "_"} {
		var legs []blnk.Transaction
		for i := 1; i <= maxLegs; i++ {
			leg, lerr := s.c.GetTransactionByReference(ctx, fmt.Sprintf("%s%s%d", ref, sep, i))
			if blnk.IsNotFound(lerr) {
				break
			}
			if lerr != nil {
				return nil, lerr
			}
			legs = append(legs, *leg)
		}
		if len(legs) > 0 {
			return assemble(legs, ref), nil
		}
	}
	return nil, err
}

// byID finds a transaction by id, rebuilding a split parent from its legs
// when only the legs are stored.
func (s *Service) byID(ctx context.Context, id string) (*blnk.Transaction, error) {
	t, err := s.c.GetTransaction(ctx, id)
	if !blnk.IsNotFound(err) {
		return t, err
	}
	legs, _, ferr := s.c.FilterTransactions(ctx, blnk.FilterRequest{
		Filters: []blnk.Filter{{Field: "parent_transaction", Operator: "eq", Value: id}},
		SortBy:  "created_at", SortOrder: "asc", Limit: maxLegs,
	})
	if ferr != nil || len(legs) == 0 {
		return nil, err
	}
	// Commit/void children point at a leg, not at the parent, so everything
	// here is a leg. Order them by their "-N" suffix.
	sort.SliceStable(legs, func(i, j int) bool { return legNumber(legs[i].Reference) < legNumber(legs[j].Reference) })
	ref := legs[0].Reference
	if k := strings.LastIndexAny(ref, "-_"); k > 0 {
		ref = ref[:k]
	}
	p := assemble(legs, ref)
	p.TransactionID = id
	return p, nil
}

// assemble rebuilds a split parent from its legs.
func assemble(legs []blnk.Transaction, ref string) *blnk.Transaction {
	p := legs[0]
	p.TransactionID, p.ID = legs[0].ParentTransaction, ""
	p.ParentTransaction = ""
	p.Reference = ref
	p.Destination = ""
	p.Destinations = nil
	p.PreciseAmount = blnk.BigInt{}
	for _, l := range legs {
		p.PreciseAmount.Add(&p.PreciseAmount.Int, &l.PreciseAmount.Int)
		p.Destinations = append(p.Destinations, blnk.Distribution{
			Identifier: l.Destination, PreciseDistribution: l.PreciseAmount.String(),
			TransactionID: l.GetID(), Narration: l.Description,
		})
		if l.Status != p.Status {
			p.Status = blnk.StatusInflight // legs disagree: some still pending
		}
	}
	p.AmountString, p.Amount = "", ""
	return &p
}

func legNumber(ref string) int {
	k := strings.LastIndexAny(ref, "-_")
	n := 0
	fmt.Sscan(ref[k+1:], &n)
	return n
}

// samePayload reports whether a stored transaction is the one req describes:
// same amount, currency, source, destination(s) and kind of movement. Blnk
// stores "@Name" accounts by their balance id, so those are resolved first.
func (s *Service) samePayload(ctx context.Context, t *blnk.Transaction, req blnk.CreateTransactionRequest) bool {
	if t.PreciseAmount.Cmp(&req.PreciseAmount.Int) != 0 || !strings.EqualFold(t.Currency, req.Currency) ||
		!s.sameAccount(ctx, t.Source, req.Source, req.Currency) {
		return false
	}
	if req.Destination != "" && t.Destination != "" && !s.sameAccount(ctx, t.Destination, req.Destination, req.Currency) {
		return false
	}
	if len(req.Destinations) > 0 {
		if len(t.Destinations) != len(req.Destinations) {
			return false
		}
		for i := range req.Destinations {
			if !s.sameAccount(ctx, t.Destinations[i].Identifier, req.Destinations[i].Identifier, req.Currency) {
				return false
			}
			if want := req.Destinations[i].PreciseDistribution; want != "" && want != "left" {
				if got := t.Destinations[i].PreciseDistribution; got != "" && got != want {
					return false
				}
			}
		}
	}
	if want, _ := req.MetaData["txn_type"].(string); want != "" {
		if got, _ := t.MetaData["txn_type"].(string); got != "" && got != want {
			return false
		}
	}
	return true
}

// sameAccount compares a stored identifier with a requested one, which may be
// an "@Name" indicator that Blnk stored as a balance id.
func (s *Service) sameAccount(ctx context.Context, stored, requested, currency string) bool {
	if stored == requested {
		return true
	}
	if !strings.HasPrefix(requested, "@") || strings.HasPrefix(stored, "@") {
		return false
	}
	id, err := s.indicatorID(ctx, requested, currency)
	return err == nil && id == stored
}

// indicatorID returns the balance id of an "@Name" account in currency.
func (s *Service) indicatorID(ctx context.Context, indicator, currency string) (string, error) {
	key := indicator + "|" + strings.ToUpper(currency)
	if id, ok := s.indicators.Load(key); ok {
		return id.(string), nil
	}
	b, err := s.c.GetBalanceByIndicator(ctx, indicator, strings.ToUpper(currency))
	if err != nil {
		return "", err
	}
	s.indicators.Store(key, b.BalanceID)
	return b.BalanceID, nil
}

// indicatorOf returns the "@Name" of a system balance id, or the id unchanged
// for customer accounts. names caches lookups for one report.
func (s *Service) indicatorOf(ctx context.Context, id string, names map[string]string) string {
	if id == "" || strings.HasPrefix(id, "@") {
		return id
	}
	if n, ok := names[id]; ok {
		return n
	}
	n := id
	if b, err := s.c.GetBalance(ctx, id); err == nil && strings.HasPrefix(b.Indicator, "@") {
		n = b.Indicator
	}
	names[id] = n
	return n
}
