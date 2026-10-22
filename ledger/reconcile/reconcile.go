// Package reconcile matches outside statements (a provider's settlement
// report, a bank statement) against ledger entries: Blnk reconciliation. Use
// it through ledger.Ledger.Reconciliation.
package reconcile

import (
	"context"
	"encoding/json"
	"io"

	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/types"
)

// maxLines is the most statement lines RunLines takes; upload a file for more.
const maxLines = 10000

// Service uploads statements, keeps matching rules and runs reconciliation.
type Service struct{ k *core.Core }

// New returns a Service on k.
func New(k *core.Core) *Service { return &Service{k} }

// Upload stores a statement: CSV or JSON with the columns id, amount,
// currency, reference, description and date, amounts in major units.
// source names where it came from, such as "paystack".
func (s *Service) Upload(ctx context.Context, source, filename string, r io.Reader) (types.StatementID, int, error) {
	if source == "" || filename == "" {
		return "", 0, core.Invalid("an upload needs a source and a file name")
	}
	up, err := s.k.C.Reconciliation.Upload(ctx, source, filename, r)
	if err != nil {
		return "", 0, core.MapErr(err)
	}
	return types.StatementID(up.UploadID), up.RecordCount, nil
}

// AddRule stores a matching rule.
func (s *Service) AddRule(ctx context.Context, r types.MatchRule) (types.MatchRule, error) {
	if r.Name == "" || len(r.Criteria) == 0 {
		return types.MatchRule{}, core.Invalid("a rule needs a name and at least one criterion")
	}
	got, err := s.k.C.Reconciliation.CreateMatchingRule(ctx, matchingRule(r))
	if err != nil {
		return types.MatchRule{}, core.MapErr(err)
	}
	return rule(got), nil
}

// ChangeRule replaces a rule's name, description and criteria.
func (s *Service) ChangeRule(ctx context.Context, id types.RuleID, r types.MatchRule) (types.MatchRule, error) {
	if r.Name == "" || len(r.Criteria) == 0 {
		return types.MatchRule{}, core.Invalid("a rule needs a name and at least one criterion")
	}
	got, err := s.k.C.Reconciliation.UpdateMatchingRule(ctx, string(id), matchingRule(r))
	if err != nil {
		return types.MatchRule{}, core.MapErr(err)
	}
	return rule(got), nil
}

// RemoveRule deletes a rule.
func (s *Service) RemoveRule(ctx context.Context, id types.RuleID) error {
	return core.MapErr(s.k.C.Reconciliation.DeleteMatchingRule(ctx, string(id)))
}

// Run reconciles an uploaded statement in the background; follow it with
// Result.
func (s *Service) Run(ctx context.Context, statement types.StatementID, run types.Run) (types.RunID, error) {
	if statement == "" {
		return "", core.Invalid("a run needs a statement")
	}
	r, err := request(run)
	if err != nil {
		return "", err
	}
	r.UploadID = string(statement)
	id, err := s.k.C.Reconciliation.Start(ctx, r)
	return types.RunID(id), core.MapErr(err)
}

// RunLines reconciles up to 10,000 statement lines sent directly, without an
// upload.
func (s *Service) RunLines(ctx context.Context, lines []types.StatementLine, run types.Run) (types.RunID, error) {
	if len(lines) == 0 || len(lines) > maxLines {
		return "", core.Invalid("RunLines takes 1 to %d lines, not %d: upload a file for more", maxLines, len(lines))
	}
	r, err := request(run)
	if err != nil {
		return "", err
	}
	for i, l := range lines {
		if !l.Amount.Valid() || l.ID == "" {
			return "", core.Invalid("line %d needs an id and an amount", i)
		}
		r.ExternalTransactions = append(r.ExternalTransactions, blnk.ExternalTransaction{
			ID: l.ID, Amount: json.Number(l.Amount.String()), Currency: l.Amount.Code(),
			Reference: l.Reference, Description: l.Description, Date: core.Time(l.Date), Source: l.Source,
		})
	}
	id, err := s.k.C.Reconciliation.StartInstant(ctx, r)
	return types.RunID(id), core.MapErr(err)
}

// Result fetches a run's progress and counts.
func (s *Service) Result(ctx context.Context, id types.RunID) (types.RunResult, error) {
	r, err := s.k.C.Reconciliation.Get(ctx, string(id))
	if err != nil {
		return types.RunResult{}, core.MapErr(err)
	}
	return types.RunResult{
		ID: types.RunID(r.ReconciliationID), Statement: types.StatementID(r.UploadID), Status: r.Status,
		Matched: r.MatchedTransactions, Unmatched: r.UnmatchedTransactions,
		StartedAt: r.StartedAt.Time, CompletedAt: r.CompletedAt.Time,
	}, nil
}

func request(run types.Run) (blnk.StartReconciliationRequest, error) {
	if len(run.Rules) == 0 {
		return blnk.StartReconciliationRequest{}, core.Invalid("a run needs at least one rule")
	}
	if run.Strategy == "" {
		run.Strategy = types.OneToOne
	}
	ids := make([]string, len(run.Rules))
	for i, id := range run.Rules {
		ids[i] = string(id)
	}
	return blnk.StartReconciliationRequest{
		Strategy: string(run.Strategy), GroupingCriteria: run.GroupBy, DryRun: run.DryRun, MatchingRuleIDs: ids,
	}, nil
}

func matchingRule(r types.MatchRule) blnk.MatchingRule {
	out := blnk.MatchingRule{Name: r.Name, Description: r.Description}
	for _, c := range r.Criteria {
		out.Criteria = append(out.Criteria, blnk.MatchingCriterion{
			Field: string(c.Field), Operator: string(c.Op), Value: c.Value, Pattern: c.Pattern, AllowableDrift: c.Drift,
		})
	}
	return out
}

func rule(r *blnk.MatchingRule) types.MatchRule {
	out := types.MatchRule{ID: types.RuleID(r.RuleID), Name: r.Name, Description: r.Description, CreatedAt: r.CreatedAt.Time}
	for _, c := range r.Criteria {
		out.Criteria = append(out.Criteria, types.Criterion{
			Field: types.MatchField(c.Field), Op: types.MatchOp(c.Operator), Value: c.Value, Pattern: c.Pattern, Drift: c.AllowableDrift,
		})
	}
	return out
}
