package blnk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Upload uploads a CSV or JSON statement (columns id, amount,
// currency, reference, description, date) for source, e.g. "paystack".
func (s *ReconciliationService) Upload(ctx context.Context, source, filename string, file io.Reader) (*UploadResponse, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("source", source); err != nil {
		return nil, err
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("blnk: read upload: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	var out UploadResponse
	return &out, s.client.doRaw(ctx, http.MethodPost, "/reconciliation/upload", w.FormDataContentType(), buf.Bytes(), &out, false)
}

// CreateMatchingRule stores a reconciliation matching rule.
func (s *ReconciliationService) CreateMatchingRule(ctx context.Context, r MatchingRule) (*MatchingRule, error) {
	var out MatchingRule
	return &out, s.client.doOnce(ctx, http.MethodPost, "/reconciliation/matching-rules", r, &out)
}

// UpdateMatchingRule replaces a matching rule's name, description and criteria.
func (s *ReconciliationService) UpdateMatchingRule(ctx context.Context, id string, r MatchingRule) (*MatchingRule, error) {
	var out MatchingRule
	return &out, s.client.do(ctx, http.MethodPut, "/reconciliation/matching-rules/"+esc(id), r, &out)
}

// DeleteMatchingRule deletes a matching rule.
func (s *ReconciliationService) DeleteMatchingRule(ctx context.Context, id string) error {
	return s.client.do(ctx, http.MethodDelete, "/reconciliation/matching-rules/"+esc(id), nil, nil)
}

// Start reconciles an uploaded statement in the background.
func (s *ReconciliationService) Start(ctx context.Context, r StartReconciliationRequest) (string, error) {
	var out struct {
		ID string `json:"reconciliation_id"`
	}
	return out.ID, s.client.doOnce(ctx, http.MethodPost, "/reconciliation/start", r, &out)
}

// StartInstant reconciles up to 10,000 external transactions
// sent inline (Core 0.10.1+).
func (s *ReconciliationService) StartInstant(ctx context.Context, r StartReconciliationRequest) (string, error) {
	var out struct {
		ID string `json:"reconciliation_id"`
	}
	return out.ID, s.client.doOnce(ctx, http.MethodPost, "/reconciliation/start-instant", r, &out)
}

// Get fetches a reconciliation run's status and counts.
func (s *ReconciliationService) Get(ctx context.Context, id string) (*Reconciliation, error) {
	var out Reconciliation
	return &out, s.client.do(ctx, http.MethodGet, "/reconciliation/"+esc(id), nil, &out)
}

// MatchingCriterion is one condition of a MatchingRule.
type MatchingCriterion struct {
	Field          string  `json:"field"`    // amount | date | description | reference | currency
	Operator       string  `json:"operator"` // equals | greater_than | less_than | contains
	Value          string  `json:"value,omitempty"`
	Pattern        string  `json:"pattern,omitempty"`
	AllowableDrift float64 `json:"allowable_drift,omitempty"` // amount: fraction (0.01 = 1%, Core 0.15.0+); date: seconds
}

// MatchingRule says when an external record and a ledger transaction match.
type MatchingRule struct {
	RuleID      string              `json:"rule_id,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Criteria    []MatchingCriterion `json:"criteria"`
	CreatedAt   Time                `json:"created_at,omitzero"`
	UpdatedAt   Time                `json:"updated_at,omitzero"`
}

// ExternalTransaction is one line of a provider's statement. Amount is in
// major units, as Blnk expects.
type ExternalTransaction struct {
	ID          string      `json:"id"`
	Amount      json.Number `json:"amount"`
	Reference   string      `json:"reference"`
	Currency    string      `json:"currency"`
	Description string      `json:"description,omitempty"`
	Date        string      `json:"date"` // RFC3339
	Source      string      `json:"source,omitempty"`
}

// UploadResponse is the answer to a reconciliation upload.
type UploadResponse struct {
	UploadID    string `json:"upload_id"`
	RecordCount int    `json:"record_count"`
	Source      string `json:"source"`
}

// StartReconciliationRequest is the body of POST /reconciliation/start and /start-instant.
type StartReconciliationRequest struct {
	UploadID             string                `json:"upload_id,omitempty"`
	ExternalTransactions []ExternalTransaction `json:"external_transactions,omitempty"` // start-instant only
	Strategy             string                `json:"strategy"`                        // one_to_one | one_to_many | many_to_one
	GroupingCriteria     string                `json:"grouping_criteria,omitempty"`
	DryRun               bool                  `json:"dry_run,omitempty"`
	MatchingRuleIDs      []string              `json:"matching_rule_ids"`
}

// Reconciliation is a reconciliation run's status and counts.
type Reconciliation struct {
	ReconciliationID      string `json:"reconciliation_id"`
	UploadID              string `json:"upload_id"`
	Status                string `json:"status"` // started | in_progress | completed | failed
	MatchedTransactions   int    `json:"matched_transactions"`
	UnmatchedTransactions int    `json:"unmatched_transactions"`
	IsDryRun              bool   `json:"is_dry_run"` // always false: Core doesn't store the dry-run flag
	StartedAt             Time   `json:"started_at"`
	CompletedAt           Time   `json:"completed_at"`
}
