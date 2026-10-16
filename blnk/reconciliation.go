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

// UploadReconciliation uploads a CSV or JSON statement (columns id, amount,
// currency, reference, description, date) for source, e.g. "paystack".
func (c *Client) UploadReconciliation(ctx context.Context, source, filename string, file io.Reader) (*UploadResponse, error) {
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
	return &out, c.doRaw(ctx, http.MethodPost, "/reconciliation/upload", w.FormDataContentType(), buf.Bytes(), &out, true)
}

// CreateMatchingRule stores a reconciliation matching rule.
func (c *Client) CreateMatchingRule(ctx context.Context, r MatchingRule) (*MatchingRule, error) {
	var out MatchingRule
	return &out, c.do(ctx, http.MethodPost, "/reconciliation/matching-rules", r, &out)
}

// StartReconciliation reconciles an uploaded statement in the background.
func (c *Client) StartReconciliation(ctx context.Context, r StartReconciliationRequest) (string, error) {
	var out struct {
		ID string `json:"reconciliation_id"`
	}
	return out.ID, c.do(ctx, http.MethodPost, "/reconciliation/start", r, &out)
}

// StartInstantReconciliation reconciles up to 10,000 external transactions
// sent inline (Core 0.10.0+).
func (c *Client) StartInstantReconciliation(ctx context.Context, r StartReconciliationRequest) (string, error) {
	var out struct {
		ID string `json:"reconciliation_id"`
	}
	return out.ID, c.do(ctx, http.MethodPost, "/reconciliation/start-instant", r, &out)
}

// GetReconciliation fetches a reconciliation run's status and counts.
func (c *Client) GetReconciliation(ctx context.Context, id string) (*Reconciliation, error) {
	var out Reconciliation
	return &out, c.do(ctx, http.MethodGet, "/reconciliation/"+esc(id), nil, &out)
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
	CreatedAt   Time                `json:"created_at,omitempty"`
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
	IsDryRun              bool   `json:"is_dry_run"`
	StartedAt             Time   `json:"started_at"`
	CompletedAt           Time   `json:"completed_at"`
}
