package blnk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CreateTransaction records a transaction (POST /transactions). A reference
// that was already used fails with TXN_DUPLICATE_REFERENCE.
func (c *Client) CreateTransaction(ctx context.Context, r CreateTransactionRequest) (*Transaction, error) {
	var out Transaction
	return &out, c.do(ctx, http.MethodPost, "/transactions", r, &out)
}

// PreviewTransaction sends r with dry_run set (Core 0.15.3+): Blnk checks it,
// including funds, and reports what would happen without writing anything.
// On older Core versions dry_run is ignored and the transaction POSTS, so only
// call this against 0.15.3 or later.
func (c *Client) PreviewTransaction(ctx context.Context, r CreateTransactionRequest) (*TransactionPreview, error) {
	r.DryRun = true
	var out TransactionPreview
	if err := c.do(ctx, http.MethodPost, "/transactions", r, &out); err != nil {
		return nil, err
	}
	if !out.DryRun {
		return nil, fmt.Errorf("blnk: dry_run not honoured by this Core version (transaction may have posted: reference %s)", r.Reference)
	}
	return &out, nil
}

// GetTransaction fetches a transaction by id.
func (c *Client) GetTransaction(ctx context.Context, id string) (*Transaction, error) {
	var out Transaction
	return &out, c.do(ctx, http.MethodGet, "/transactions/"+esc(id), nil, &out)
}

// GetTransactionByReference fetches by your reference. Queued transactions that
// have been processed carry the reference with a "_q" suffix.
func (c *Client) GetTransactionByReference(ctx context.Context, ref string) (*Transaction, error) {
	var out Transaction
	return &out, c.do(ctx, http.MethodGet, "/transactions/reference/"+esc(ref), nil, &out)
}

// Refund reverses a transaction. Each original can be refunded once
// (a second attempt returns TXN_ALREADY_REFUNDED).
func (c *Client) Refund(ctx context.Context, id string, r *RefundRequest) (*Transaction, error) {
	var out Transaction
	var body any
	if r != nil {
		body = r
	}
	return &out, c.do(ctx, http.MethodPost, "/refund-transaction/"+esc(id), body, &out)
}

// CreateBulkTransactions posts a batch (POST /transactions/bulk).
func (c *Client) CreateBulkTransactions(ctx context.Context, r BulkTransactionRequest) (*BulkTransactionResponse, error) {
	var out BulkTransactionResponse
	return &out, c.do(ctx, http.MethodPost, "/transactions/bulk", r, &out)
}

// FilterTransactions runs POST /transactions/filter. It handles both response
// shapes (bare array, or {"data": [...], "total_count": n} with include_count).
func (c *Client) FilterTransactions(ctx context.Context, r FilterRequest) ([]Transaction, int, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/transactions/filter", r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[Transaction](raw)
}

// Transaction statuses.
const (
	StatusQueued    = "QUEUED"
	StatusApplied   = "APPLIED"
	StatusInflight  = "INFLIGHT"
	StatusVoid      = "VOID"
	StatusRejected  = "REJECTED"
	StatusScheduled = "SCHEDULED"
)

// Distribution is one leg of a multi-source / multi-destination transaction.
// Set exactly one of Distribution ("20%", "left") or PreciseDistribution
// (an exact amount in the smallest unit; requires PreciseAmount on the parent).
type Distribution struct {
	Identifier          string `json:"identifier"`
	Distribution        string `json:"distribution,omitempty"`
	PreciseDistribution string `json:"precise_distribution,omitempty"`
	Narration           string `json:"narration,omitempty"`
	TransactionID       string `json:"transaction_id,omitempty"` // set on responses
}

// CreateTransactionRequest is the body of POST /transactions.
// Use Destination OR Destinations, Source OR Sources, and never both
// Sources and Destinations together (rejected with TXN_VALIDATION_ERROR).
type CreateTransactionRequest struct {
	PreciseAmount      *BigInt        `json:"precise_amount"`
	Precision          int64          `json:"precision,omitempty"`
	Reference          string         `json:"reference"`
	Currency           string         `json:"currency"`
	Source             string         `json:"source,omitempty"`
	Destination        string         `json:"destination,omitempty"`
	Sources            []Distribution `json:"sources,omitempty"`
	Destinations       []Distribution `json:"destinations,omitempty"`
	Description        string         `json:"description,omitempty"`
	AllowOverdraft     bool           `json:"allow_overdraft,omitempty"`
	Inflight           bool           `json:"inflight,omitempty"`
	InflightExpiryDate string         `json:"inflight_expiry_date,omitempty"` // RFC3339 with offset
	InflightCommitDate string         `json:"inflight_commit_date,omitempty"` // RFC3339 with offset
	ScheduledFor       string         `json:"scheduled_for,omitempty"`
	EffectiveDate      string         `json:"effective_date,omitempty"` // backdated transactions
	SkipQueue          bool           `json:"skip_queue,omitempty"`
	DryRun             bool           `json:"dry_run,omitempty"`
	MetaData           MetaData       `json:"meta_data,omitempty"`
}

// Transaction is a transaction record as returned by Blnk.
type Transaction struct {
	TransactionID      string         `json:"transaction_id"`
	ID                 string         `json:"id"` // older Core versions use "id"
	ParentTransaction  string         `json:"parent_transaction"`
	Reference          string         `json:"reference"`
	Amount             json.Number    `json:"amount"`        // float as stored; display only
	AmountString       string         `json:"amount_string"` // exact decimal, newer Core versions
	PreciseAmount      BigInt         `json:"precise_amount"`
	Precision          json.Number    `json:"precision"`
	Currency           string         `json:"currency"`
	Source             string         `json:"source"`
	Destination        string         `json:"destination"`
	Sources            []Distribution `json:"sources,omitempty"`
	Destinations       []Distribution `json:"destinations,omitempty"`
	Description        string         `json:"description"`
	Status             string         `json:"status"`
	AllowOverdraft     bool           `json:"allow_overdraft"`
	Inflight           bool           `json:"inflight"`
	SkipQueue          bool           `json:"skip_queue"`
	Hash               string         `json:"hash,omitempty"`
	CreatedAt          Time           `json:"created_at"`
	EffectiveDate      Time           `json:"effective_date"` // when it counts in balances; defaults to created_at
	ScheduledFor       Time           `json:"scheduled_for"`
	InflightExpiryDate Time           `json:"inflight_expiry_date"`
	InflightCommitDate Time           `json:"inflight_commit_date"`
	MetaData           MetaData       `json:"meta_data,omitempty"`
}

// GetID returns the transaction id regardless of which field the server used.
func (t *Transaction) GetID() string {
	if t.TransactionID != "" {
		return t.TransactionID
	}
	return t.ID
}

// QueuedParent returns meta_data.QUEUED_PARENT_TRANSACTION if present.
func (t *Transaction) QueuedParent() string {
	if t.MetaData == nil {
		return ""
	}
	s, _ := t.MetaData["QUEUED_PARENT_TRANSACTION"].(string)
	return s
}

// RefundRequest is the optional body of POST /refund-transaction/{id}.
type RefundRequest struct {
	SkipQueue   bool     `json:"skip_queue,omitempty"`
	Description string   `json:"description,omitempty"` // Core 0.15.3+
	MetaData    MetaData `json:"meta_data,omitempty"`   // merged onto inherited metadata
}

// BulkTransactionRequest is the body of POST /transactions/bulk.
type BulkTransactionRequest struct {
	Atomic       bool                       `json:"atomic"`
	Inflight     bool                       `json:"inflight"`
	RunAsync     bool                       `json:"run_async"`
	SkipQueue    bool                       `json:"skip_queue"`
	DryRun       bool                       `json:"dry_run,omitempty"`
	Transactions []CreateTransactionRequest `json:"transactions"`
}

// BulkTransactionResponse is the answer to a bulk request.
type BulkTransactionResponse struct {
	BatchID          string `json:"batch_id"`
	Status           string `json:"status"` // applied | inflight | processing
	TransactionCount int    `json:"transaction_count"`
	Message          string `json:"message,omitempty"`
}

// TransactionPreview is the response to a dry_run transaction. Nothing is
// written and the reference is not consumed.
type TransactionPreview struct {
	DryRun     bool `json:"dry_run"`
	WouldApply bool `json:"would_apply"`
	Rejection  *struct {
		Code    string `json:"code"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"rejection,omitempty"`
	Status        string           `json:"status,omitempty"`
	Reference     string           `json:"reference"`
	Currency      string           `json:"currency"`
	PreciseAmount BigInt           `json:"precise_amount"`
	Precision     json.Number      `json:"precision"`
	Balances      []PreviewBalance `json:"balances"`
	Notes         []string         `json:"notes,omitempty"`
}

// PreviewBalance is one balance a dry run would change, before and after.
type PreviewBalance struct {
	BalanceID        string `json:"balance_id"`
	Role             string `json:"role"` // source | destination
	Currency         string `json:"currency"`
	CurrentBalance   BigInt `json:"current_balance"`
	CurrentAvailable BigInt `json:"current_available"`
	ResultingBalance BigInt `json:"resulting_balance"`
	ResultingAvail   BigInt `json:"resulting_available"`
}
