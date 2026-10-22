package memblnk

import (
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// child records what became of hold h: a commit (APPLIED) or a release
// (VOID) of amount, naming h as its parent. The hold itself stays INFLIGHT.
func (s *Server) child(h *txn, ref, status string, amount *big.Int) *txn {
	c := &txn{Transaction: h.Transaction, amount: new(big.Int).Set(amount), committed: new(big.Int)}
	c.TransactionID = newID("txn_")
	c.ParentTransaction = h.TransactionID
	c.Reference = ref
	c.Status = status
	c.Inflight = false
	c.PreciseAmount = blnk.BigInt{Int: *new(big.Int).Set(amount)}
	c.Amount, c.AmountString = major(amount, h.Precision)
	c.CreatedAt = blnk.Time{Time: s.now()}
	c.EffectiveDate = c.CreatedAt
	c.InflightExpiryDate = blnk.Time{}
	c.MetaData = maps.Clone(h.MetaData)
	s.txns[c.TransactionID] = c
	s.byRef[ref] = c
	s.txnOrder = append(s.txnOrder, c.TransactionID)
	s.emit("transaction."+lower(status), c.Transaction)
	return c
}

// commit moves amount of hold h (all that is left if nil) to its
// destination.
func (s *Server) commit(h *txn, amount *big.Int) (*txn, *failure) {
	if err := settled(h); err != nil {
		return nil, err
	}
	left := h.remaining()
	if amount == nil {
		amount = left
	}
	switch {
	case amount.Sign() <= 0:
		return nil, invalid(blnk.CodeInvalidAmount, "commit amount must be positive")
	case amount.Cmp(left) > 0:
		return nil, invalid(blnk.CodeCommitAmountExceeded, "commit of %s exceeds the %s left on %s", amount, left, h.TransactionID)
	}
	src, dst := s.balances[h.Source], s.balances[h.Destination]
	s.release(src, dst, amount)
	s.move(src, dst, amount, false)
	h.committed.Add(h.committed, amount)
	return s.child(h, newID("ref_"), blnk.StatusApplied, amount), nil
}

// void releases what is left of hold h.
func (s *Server) void(h *txn) (*txn, *failure) {
	if err := settled(h); err != nil {
		return nil, err
	}
	left := h.remaining()
	s.release(s.balances[h.Source], s.balances[h.Destination], left)
	h.voided = true
	return s.child(h, newID("ref_"), blnk.StatusVoid, left), nil
}

// settled refuses a hold with nothing left to commit or void.
func settled(h *txn) *failure {
	switch {
	case !h.Inflight || h.Status != blnk.StatusInflight:
		return invalid(blnk.CodeNotInflight, "transaction %s is not inflight", h.TransactionID)
	case h.voided:
		return conflict(blnk.CodeAlreadyVoided, "transaction %s has already been voided", h.TransactionID)
	case h.remaining().Sign() == 0:
		return conflict(blnk.CodeAlreadyCommitted, "transaction %s has already been committed", h.TransactionID)
	}
	return nil
}

// updateInflight commits or voids a hold, or every hold under a split or
// batch id (Blnk holds a split as one INFLIGHT transaction per posting).
func (s *Server) updateInflight(r *http.Request, body []byte) (int, any) {
	var in struct {
		blnk.UpdateInflightRequest
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return respond(0, nil, invalid(blnk.CodeMalformedRequest, "%v", err))
	}
	if in.DryRun {
		return respond(0, nil, invalid(blnk.CodeBadRequest, "memblnk doesn't preview commits or voids"))
	}
	if in.Status != blnk.ActionCommit && in.Status != blnk.ActionVoid {
		return respond(0, nil, invalid(blnk.CodeInvalidStatusAction, "status must be commit or void, not %q", in.Status))
	}
	id := r.PathValue("id")
	holds := []*txn{s.txns[id]}
	if holds[0] == nil {
		holds = s.holdsUnder(id)
		if len(holds) == 0 {
			return respond(0, nil, notFound(blnk.CodeTxnNotFound, "transaction", id))
		}
		if in.PreciseAmount != nil {
			return respond(0, nil, invalid(blnk.CodeTxnValidation, "%s holds several transactions: commit them in full", id))
		}
	}
	// Blnk settles every hold still open, then answers with the error of
	// one settled before: ALREADY_COMMITTED if any was committed, even with
	// another voided. So a split committed partway is finished by committing
	// it again, and one with a posting voided is committed for the rest.
	var refusal *failure
	for _, h := range holds {
		if err := settled(h); err != nil && (refusal == nil || h.committed.Sign() > 0) {
			refusal = err
			if h.committed.Sign() > 0 {
				refusal = conflict(blnk.CodeAlreadyCommitted, "transaction %s has already been committed", h.TransactionID)
			}
		}
	}
	var last *txn
	for _, h := range holds {
		if len(holds) > 1 && settled(h) != nil {
			continue
		}
		var amount *big.Int
		if in.PreciseAmount != nil {
			amount = &in.PreciseAmount.Int
		}
		var err *failure
		if in.Status == blnk.ActionCommit {
			last, err = s.commit(h, amount)
		} else {
			last, err = s.void(h)
		}
		if err != nil {
			return respond(0, nil, err)
		}
	}
	if refusal != nil {
		return respond(0, nil, refusal)
	}
	return http.StatusOK, blnk.UpdateInflightResponse{Transaction: last.Transaction}
}

// holdsUnder are the holds naming parent as their parent.
func (s *Server) holdsUnder(parent string) []*txn {
	var out []*txn
	for _, id := range s.txnOrder {
		if t := s.txns[id]; t.ParentTransaction == parent && t.Inflight && t.Status == blnk.StatusInflight {
			out = append(out, t)
		}
	}
	return out
}

// expireHolds voids every hold whose expiry has passed, as Blnk does.
func (s *Server) expireHolds() {
	now := time.Now()
	for _, id := range s.txnOrder {
		h := s.txns[id]
		if h.Inflight && h.Status == blnk.StatusInflight && !h.expires.IsZero() && now.After(h.expires) && settled(h) == nil {
			_, _ = s.void(h)
		}
	}
}
