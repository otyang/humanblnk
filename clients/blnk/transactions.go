package blnk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Create records a transaction (POST /transactions). A reference
// that was already used fails with TXN_DUPLICATE_REFERENCE; after a retried
// network error or gateway timeout that can mean the first attempt went
// through, so look it up with GetByReference.
func (s *TransactionService) Create(ctx context.Context, r CreateTransactionRequest) (*Transaction, error) {
	var out Transaction
	return &out, s.client.do(ctx, http.MethodPost, "/transactions", r, &out)
}

// Preview sends r with dry_run set (Core 0.15.3+): Blnk checks it,
// including funds, and reports what would happen without writing anything.
// On older Core versions dry_run is ignored and the transaction POSTS, so only
// call this against 0.15.3 or later.
func (s *TransactionService) Preview(ctx context.Context, r CreateTransactionRequest) (*TransactionPreview, error) {
	body := struct {
		CreateTransactionRequest
		DryRun bool `json:"dry_run"`
	}{r, true}
	var out TransactionPreview
	if err := s.client.do(ctx, http.MethodPost, "/transactions", body, &out); err != nil {
		return nil, err
	}
	if err := previewed(out.DryRun, "transaction "+r.Reference); err != nil {
		return nil, err
	}
	return &out, nil
}

// previewed reports an answer to a dry run that Core didn't treat as one:
// what was meant as a preview may have been applied.
func previewed(dryRun bool, what string) error {
	if dryRun {
		return nil
	}
	return fmt.Errorf("blnk: dry_run not honoured by this Core version (%s may have been applied)", what)
}

// Get fetches a transaction by id.
func (s *TransactionService) Get(ctx context.Context, id string) (*Transaction, error) {
	var out Transaction
	return &out, s.client.do(ctx, http.MethodGet, "/transactions/"+esc(id), nil, &out)
}

// GetByReference fetches by your reference. Queued transactions that
// have been processed carry the reference with a "_q" suffix. Blnk returns
// only some fields here (ids, reference, parties, amounts, currency,
// description, status, created_at, effective_date, meta_data); use Get for
// the full record.
func (s *TransactionService) GetByReference(ctx context.Context, ref string) (*Transaction, error) {
	var out Transaction
	return &out, s.client.do(ctx, http.MethodGet, "/transactions/reference/"+esc(ref), nil, &out)
}

// List lists transactions (GET /transactions).
func (s *TransactionService) List(ctx context.Context, o ListOptions) ([]Transaction, error) {
	return getList[Transaction](ctx, s.client, o.path("/transactions"))
}

// Filter runs POST /transactions/filter. It handles both response
// shapes (bare array, or {"data": [...], "total_count": n} with include_count).
func (s *TransactionService) Filter(ctx context.Context, r FilterRequest) ([]Transaction, int, error) {
	return filterList[Transaction](ctx, s.client, "/transactions/filter", r)
}

// Refund reverses a transaction. Each original can be refunded once
// (a second attempt returns TXN_ALREADY_REFUNDED).
func (s *TransactionService) Refund(ctx context.Context, id string, r *RefundRequest) (*Transaction, error) {
	var out Transaction
	var body any
	if r != nil {
		body = r
	}
	return &out, s.client.do(ctx, http.MethodPost, "/refund-transaction/"+esc(id), body, &out)
}

// PreviewRefund reports what refunding id would do, without writing anything.
func (s *TransactionService) PreviewRefund(ctx context.Context, id string) (*TransactionPreview, error) {
	var out TransactionPreview
	if err := s.client.do(ctx, http.MethodPost, "/refund-transaction/"+esc(id), map[string]bool{"dry_run": true}, &out); err != nil {
		return nil, err
	}
	if err := previewed(out.DryRun, "refund of "+id); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateBulk posts a batch (POST /transactions/bulk).
func (s *TransactionService) CreateBulk(ctx context.Context, r BulkTransactionRequest) (*BulkTransactionResponse, error) {
	var out BulkTransactionResponse
	return &out, s.client.do(ctx, http.MethodPost, "/transactions/bulk", r, &out)
}

// PreviewBulk reports what a batch would do, without writing anything.
func (s *TransactionService) PreviewBulk(ctx context.Context, r BulkTransactionRequest) (*BulkPreview, error) {
	body := struct {
		BulkTransactionRequest
		DryRun bool `json:"dry_run"`
	}{r, true}
	var out BulkPreview
	if err := s.client.do(ctx, http.MethodPost, "/transactions/bulk", body, &out); err != nil {
		return nil, err
	}
	if err := previewed(out.DryRun, "batch"); err != nil {
		return nil, err
	}
	return &out, nil
}

// Lineage reports which providers' funds a transaction spent
// (GET /transactions/{id}/lineage).
func (s *TransactionService) Lineage(ctx context.Context, id string) (*TransactionLineage, error) {
	var out TransactionLineage
	return &out, s.client.do(ctx, http.MethodGet, "/transactions/"+esc(id)+"/lineage", nil, &out)
}

// RecoverQueued re-queues transactions stuck in QUEUED for longer than
// threshold (POST /transactions/recover) and returns how many it recovered.
// threshold <= 0 leaves Blnk's default of 2 minutes, which is also the
// minimum: Blnk raises anything shorter to it.
func (s *TransactionService) RecoverQueued(ctx context.Context, threshold time.Duration) (int, error) {
	path := "/transactions/recover"
	if threshold > 0 {
		path += "?" + url.Values{"threshold": {threshold.String()}}.Encode()
	}
	var out struct {
		Recovered int `json:"recovered"`
	}
	return out.Recovered, s.client.do(ctx, http.MethodPost, path, nil, &out)
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
// Set exactly one of Distribution ("20%", a major-unit amount like "100",
// or "left") or PreciseDistribution
// (an exact amount in the smallest unit; requires PreciseAmount on the parent).
type Distribution struct {
	Identifier          string `json:"identifier"`
	Distribution        string `json:"distribution,omitempty"`
	PreciseDistribution string `json:"precise_distribution,omitempty"`
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
	Atomic             bool           `json:"atomic,omitempty"`      // split: all legs apply or none (default: each leg on its own)
	Description        string         `json:"description,omitempty"` // required by Blnk
	AllowOverdraft     bool           `json:"allow_overdraft,omitempty"`
	OverdraftLimit     json.Number    `json:"overdraft_limit,omitempty"` // major units, scaled by Precision: how far below 0 the source may go; AllowOverdraft with no limit is unlimited
	Inflight           bool           `json:"inflight,omitempty"`
	InflightExpiryDate string         `json:"inflight_expiry_date,omitempty"` // RFC3339 with offset
	InflightCommitDate string         `json:"inflight_commit_date,omitempty"` // RFC3339 with offset
	ScheduledFor       string         `json:"scheduled_for,omitempty"`
	EffectiveDate      string         `json:"effective_date,omitempty"` // backdated transactions
	SkipQueue          bool           `json:"skip_queue,omitempty"`
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
	OverdraftLimit     json.Number    `json:"overdraft_limit,omitempty"`
	Atomic             bool           `json:"atomic"`
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
	Transactions []CreateTransactionRequest `json:"transactions"`
}

// BulkTransactionResponse is the answer to a bulk request.
type BulkTransactionResponse struct {
	BatchID          string `json:"batch_id"`
	Status           string `json:"status"` // applied | inflight | processing
	TransactionCount int    `json:"transaction_count"`
	Message          string `json:"message,omitempty"` // run_async only
}

// TransactionPreview is the response to a dry run of a transaction, refund
// or inflight commit/void. Nothing is written and the reference is not consumed.
type TransactionPreview struct {
	DryRun        bool              `json:"dry_run"`
	WouldApply    bool              `json:"would_apply"`
	Rejection     *PreviewRejection `json:"rejection,omitempty"`
	Operation     string            `json:"operation,omitempty"` // e.g. commit, void, refund
	Status        string            `json:"status,omitempty"`
	Reference     string            `json:"reference"`
	Currency      string            `json:"currency"`
	Amount        json.Number       `json:"amount"` // display only
	PreciseAmount BigInt            `json:"precise_amount"`
	Precision     json.Number       `json:"precision"`
	Balances      []PreviewBalance  `json:"balances"`
	Legs          []PreviewLeg      `json:"legs,omitempty"` // splits
	Notes         []string          `json:"notes,omitempty"`
}

// PreviewRejection is why a dry run would be rejected.
type PreviewRejection struct {
	Code    string `json:"code"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// PreviewBalance is one balance a dry run would change, before and after.
type PreviewBalance struct {
	BalanceID                      string `json:"balance_id"`
	Role                           string `json:"role"` // source | destination
	Currency                       string `json:"currency"`
	Virtual                        bool   `json:"virtual,omitempty"` // would be created by the transaction
	CurrentBalance                 BigInt `json:"current_balance"`
	CurrentAvailable               BigInt `json:"current_available"`
	CurrentCreditBalance           BigInt `json:"current_credit_balance"`
	CurrentDebitBalance            BigInt `json:"current_debit_balance"`
	CurrentInflightDebitBalance    BigInt `json:"current_inflight_debit_balance"`
	CurrentInflightCreditBalance   BigInt `json:"current_inflight_credit_balance"`
	ResultingBalance               BigInt `json:"resulting_balance"`
	ResultingAvailable             BigInt `json:"resulting_available"`
	ResultingCreditBalance         BigInt `json:"resulting_credit_balance"`
	ResultingDebitBalance          BigInt `json:"resulting_debit_balance"`
	ResultingInflightDebitBalance  BigInt `json:"resulting_inflight_debit_balance"`
	ResultingInflightCreditBalance BigInt `json:"resulting_inflight_credit_balance"`
}

// PreviewLeg is one leg of a split dry run.
type PreviewLeg struct {
	Identifier    string      `json:"identifier"`
	Role          string      `json:"role"`
	PreciseAmount BigInt      `json:"precise_amount"`
	Amount        json.Number `json:"amount"` // display only
}

// BulkPreview is the response to a dry run of a bulk transaction or a bulk
// inflight commit/void.
type BulkPreview struct {
	DryRun     bool                 `json:"dry_run"`
	WouldApply bool                 `json:"would_apply"`
	Cumulative bool                 `json:"cumulative"` // items projected on top of each other
	Atomic     bool                 `json:"atomic"`
	Results    []TransactionPreview `json:"results"`
	Balances   []PreviewBalance     `json:"balances,omitempty"`
	Notes      []string             `json:"notes,omitempty"`
}

// TransactionLineage is GET /transactions/{id}/lineage.
type TransactionLineage struct {
	TransactionID      string           `json:"transaction_id"`
	FundAllocation     []map[string]any `json:"fund_allocation,omitempty"`
	ShadowTransactions []Transaction    `json:"shadow_transactions"`
}
