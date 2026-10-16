package ledgertest

import (
	"math/big"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// reconcile runs an instant one-to-one reconciliation: an external record
// matches an unmatched APPLIED transaction when every criterion of any rule
// holds (equals on reference / currency / amount with drift, contains on
// description).
func (s *Server) reconcile(in blnk.StartReconciliationRequest) (any, int, error) {
	if len(in.MatchingRuleIDs) == 0 || in.Strategy == "" {
		return nil, 0, fail(400, blnk.CodeValidation, "strategy and matching_rule_ids are required")
	}
	if in.Strategy != "one_to_one" {
		return nil, 0, fail(400, blnk.CodeValidation, "emulator: only one_to_one is supported")
	}
	var rules []*blnk.MatchingRule
	for _, id := range in.MatchingRuleIDs {
		r, ok := s.rules[id]
		if !ok {
			return nil, 0, fail(404, blnk.CodeNotFound, "matching rule %s not found", id)
		}
		rules = append(rules, r)
	}
	used := map[string]bool{}
	rec := &blnk.Reconciliation{ReconciliationID: s.id("recon"), Status: "completed", IsDryRun: in.DryRun,
		StartedAt: blnk.Time{Time: s.now}, CompletedAt: blnk.Time{Time: s.now}}
	for _, ext := range in.ExternalTransactions {
		matched := false
		for _, id := range s.order {
			t := s.txns[id]
			if used[id] || t.Status != blnk.StatusApplied {
				continue
			}
			for _, r := range rules {
				if ruleMatches(r, ext, t) {
					matched = true
					break
				}
			}
			if matched {
				used[id] = true
				break
			}
		}
		if matched {
			rec.MatchedTransactions++
		} else {
			rec.UnmatchedTransactions++
		}
	}
	s.recons[rec.ReconciliationID] = rec
	return map[string]any{"reconciliation_id": rec.ReconciliationID}, 200, nil
}

func ruleMatches(r *blnk.MatchingRule, ext blnk.ExternalTransaction, t *txn) bool {
	for _, c := range r.Criteria {
		switch c.Field + "/" + c.Operator {
		case "reference/equals":
			if ext.Reference != t.Reference {
				return false
			}
		case "currency/equals":
			if !strings.EqualFold(ext.Currency, t.Currency) {
				return false
			}
		case "description/contains":
			if !strings.Contains(strings.ToLower(t.Description), strings.ToLower(ext.Description)) {
				return false
			}
		case "amount/equals":
			ea, ok := new(big.Rat).SetString(ext.Amount.String())
			if !ok {
				return false
			}
			ta, _ := new(big.Rat).SetString(t.AmountString)
			if ta == nil {
				return false
			}
			diff := new(big.Rat).Sub(ea, ta)
			diff.Abs(diff)
			tol := new(big.Rat).Mul(ta, new(big.Rat).SetFloat64(c.AllowableDrift))
			if diff.Cmp(tol.Abs(tol)) > 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
