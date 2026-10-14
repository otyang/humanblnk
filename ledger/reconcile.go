package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// ExternalRecord is one line of a provider's settlement report or bank statement.
type ExternalRecord struct {
	ID          string // the provider's id for the line
	Amount      Money
	Reference   string // what you sent to Blnk as the transaction reference
	Description string
	Date        time.Time
}

// ReconcileInput matches a provider's records against the ledger.
type ReconcileInput struct {
	Source string // e.g. "paystack"

	// Records are sent inline (instant reconciliation, Core 0.10.0+, up to
	// 10,000). For bigger files use File instead.
	Records []ExternalRecord

	// File is a CSV or JSON export with columns id, amount, currency,
	// reference, description, date (RFC3339, amount in major units).
	File     io.Reader
	Filename string

	// RuleIDs are matching rules created with Reconciliations().CreateRule. Empty uses
	// one rule matching reference + currency + exact amount (created once
	// per Service).
	RuleIDs  []string
	Strategy string // one_to_one (default), one_to_many, many_to_one
	DryRun   bool   // match without recording the result
}

// Reconciliation is a reconciliation run. Blnk reports counts only; it has no
// endpoint listing which lines matched.
type Reconciliation struct {
	ID          string
	Status      string // started | in_progress | completed | failed
	Matched     int
	Unmatched   int
	DryRun      bool
	StartedAt   time.Time
	CompletedAt time.Time
}

// Done reports whether the run has finished (completed or failed).
func (r *Reconciliation) Done() bool { return r.Status == "completed" || r.Status == "failed" }

// MatchingRule says when an external record and a ledger transaction are the same.
type MatchingRule = blnk.MatchingRule

// MatchingCriterion is one condition of a MatchingRule.
type MatchingCriterion = blnk.MatchingCriterion

// CreateRule stores a rule in Blnk and returns its id.
func (s reconciliationsAPI) CreateRule(ctx context.Context, r MatchingRule) (string, error) {
	const op = "ledger.Reconciliations.CreateRule"
	if r.Name == "" || len(r.Criteria) == 0 {
		return "", invalid(op, "name and at least one criterion are required")
	}
	out, err := s.c.CreateMatchingRule(ctx, r)
	if err != nil {
		return "", mapErr(op, err)
	}
	return out.RuleID, nil
}

// Start starts a reconciliation. Poll Reconciliations().Get until Done.
func (s reconciliationsAPI) Start(ctx context.Context, in ReconcileInput) (*Reconciliation, error) {
	const op = "ledger.Reconciliations.Start"
	if strings.TrimSpace(in.Source) == "" {
		return nil, invalid(op, "source is required")
	}
	if (len(in.Records) == 0) == (in.File == nil) {
		return nil, invalid(op, "set either Records or File")
	}
	if len(in.Records) > 10000 {
		return nil, invalid(op, "at most 10,000 records inline; upload a File instead")
	}
	rules := in.RuleIDs
	if len(rules) == 0 {
		id, err := s.defaultRule(ctx, op)
		if err != nil {
			return nil, err
		}
		rules = []string{id}
	}
	req := blnk.StartReconciliationRequest{
		Strategy: firstNonEmpty(in.Strategy, "one_to_one"), DryRun: in.DryRun, MatchingRuleIDs: rules,
	}

	var id string
	var err error
	if in.File != nil {
		up, uerr := s.c.UploadReconciliation(ctx, in.Source, firstNonEmpty(in.Filename, in.Source+".csv"), in.File)
		if uerr != nil {
			return nil, mapErr(op, uerr)
		}
		req.UploadID = up.UploadID
		id, err = s.c.StartReconciliation(ctx, req)
	} else {
		for i, r := range in.Records {
			if r.ID == "" || !r.Amount.isSet() || r.Amount.Currency == "" {
				return nil, invalid(op, "record %d: id, amount and currency are required", i)
			}
			amt, rerr := s.resolve(op, r.Amount, "")
			if rerr != nil {
				return nil, fmt.Errorf("record %d: %w", i, rerr)
			}
			req.ExternalTransactions = append(req.ExternalTransactions, blnk.ExternalTransaction{
				ID: r.ID, Amount: json.Number(amt.Value), Currency: amt.Currency, Reference: r.Reference,
				Description: r.Description, Date: r.Date.UTC().Format(time.RFC3339), Source: in.Source,
			})
		}
		id, err = s.c.StartInstantReconciliation(ctx, req)
	}
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.Reconciliations().Get(ctx, id)
}

// Get returns a run's status and counts.
func (s reconciliationsAPI) Get(ctx context.Context, id string) (*Reconciliation, error) {
	r, err := s.c.GetReconciliation(ctx, id)
	if err != nil {
		return nil, mapErr("ledger.Reconciliations.Get", err)
	}
	return &Reconciliation{
		ID: firstNonEmpty(r.ReconciliationID, id), Status: r.Status, Matched: r.MatchedTransactions,
		Unmatched: r.UnmatchedTransactions, DryRun: r.IsDryRun,
		StartedAt: r.StartedAt.Time, CompletedAt: r.CompletedAt.Time,
	}, nil
}

func (s *Service) defaultRule(ctx context.Context, op string) (string, error) {
	s.ruleMu.Lock()
	defer s.ruleMu.Unlock()
	if s.ruleID != "" {
		return s.ruleID, nil
	}
	id, err := s.Reconciliations().CreateRule(ctx, MatchingRule{
		Name: "reference+currency+amount", Description: "created by humanblnk",
		Criteria: []MatchingCriterion{
			{Field: "reference", Operator: "equals"},
			{Field: "currency", Operator: "equals"},
			{Field: "amount", Operator: "equals"},
		},
	})
	if err != nil {
		return "", err
	}
	s.ruleID = id
	return id, nil
}
