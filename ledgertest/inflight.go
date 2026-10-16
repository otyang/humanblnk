package ledgertest

import (
	"math/big"

	"github.com/otyang/humanblnk/blnk"
)

func (s *Server) updateInflight(id string, in blnk.UpdateInflightRequest) (*txn, error) {
	if ids, ok := s.batches[id]; ok {
		var last *txn
		for _, tid := range ids {
			t, err := s.inflightAction(tid, in.Status, nil)
			if err != nil {
				return nil, err
			}
			last = t
		}
		return last, nil
	}
	var partial *big.Int
	if in.PreciseAmount != nil {
		partial = &in.PreciseAmount.Int
	}
	return s.inflightAction(id, in.Status, partial)
}

// inflightAction commits (optionally partially) or voids an inflight
// transaction, recording the outcome as a child transaction.
func (s *Server) inflightAction(id, action string, partial *big.Int) (*txn, error) {
	if legIDs, isSplit := s.splits[id]; isSplit {
		// Like Blnk 0.15, a partial amount is committed on each leg in turn;
		// a leg smaller than it fails the call and leaves the earlier legs
		// committed.
		var first *txn
		for _, lid := range legIDs {
			c, err := s.inflightAction(lid, action, partial)
			if err != nil {
				return nil, err
			}
			if first == nil {
				first = c // Blnk answers with the first leg's outcome
			}
		}
		return first, nil
	}
	t, ok := s.txns[id]
	if !ok {
		return nil, fail(404, blnk.CodeTxnNotFound, "transaction %s not found", id)
	}
	if t.Status != blnk.StatusInflight {
		return nil, fail(400, blnk.CodeNotInflight, "transaction %s is not inflight", id)
	}
	if t.voided {
		return nil, fail(400, blnk.CodeAlreadyVoided, "transaction %s has already been voided", id)
	}
	rem := t.remaining()
	if rem.Sign() == 0 {
		return nil, fail(400, blnk.CodeAlreadyCommitted, "transaction %s has already been committed", id)
	}
	legs := s.legsOf(t)

	switch action {
	case blnk.ActionCommit:
		amt := rem
		if partial != nil {
			if partial.Sign() <= 0 {
				return nil, fail(400, blnk.CodeInvalidAmount, "amount must be positive")
			}
			if partial.Cmp(rem) > 0 {
				return nil, fail(400, blnk.CodeCommitAmountExceeded, "commit amount exceeds the remaining inflight amount")
			}
			amt = new(big.Int).Set(partial)
		}
		legs[0].amt = amt
		for _, l := range legs {
			reserve(l.src, l.dst, l.amt, -1)
			post(l.src, l.dst, l.amt)
		}
		t.committed.Add(&t.committed, amt)
		return s.child(t, blnk.StatusApplied, amt, legs), nil
	case blnk.ActionVoid:
		legs[0].amt = rem
		for _, l := range legs {
			reserve(l.src, l.dst, l.amt, -1)
		}
		t.voided = true
		return s.child(t, blnk.StatusVoid, rem, legs), nil
	}
	return nil, fail(400, blnk.CodeInvalidStatusAction, "status must be commit or void")
}

func (s *Server) bulkInflight(items []blnk.BulkCommitItem, action string) blnk.BulkInflightResponse {
	var out blnk.BulkInflightResponse
	for _, it := range items {
		var partial *big.Int
		if it.PreciseAmount != nil {
			partial = &it.PreciseAmount.Int
		}
		r := blnk.BulkInflightResult{TransactionID: it.TransactionID, Status: "succeeded"}
		if _, err := s.inflightAction(it.TransactionID, action, partial); err != nil {
			ae := err.(*apiErr)
			r.Status, r.Code, r.Message = "failed", ae.code, ae.msg
			out.Failed++
		} else {
			out.Succeeded++
		}
		out.Results = append(out.Results, r)
	}
	return out
}
