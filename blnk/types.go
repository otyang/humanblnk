package blnk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// ---------------------------------------------------------------------------
// Scalars
// ---------------------------------------------------------------------------

// BigInt is an arbitrary-precision integer that marshals as a JSON number and
// unmarshals from a number, a quoted string, or null. Blnk stores amounts in the
// smallest unit (precise_amount), which can exceed int64 for high-precision assets.
type BigInt struct{ big.Int }

// NewBigInt returns a BigInt holding v.
func NewBigInt(v int64) *BigInt { b := &BigInt{}; b.SetInt64(v); return b }

// BigFrom wraps a *big.Int (nil stays nil).
func BigFrom(v *big.Int) *BigInt {
	if v == nil {
		return nil
	}
	b := &BigInt{}
	b.Set(v)
	return b
}

func (b BigInt) MarshalJSON() ([]byte, error) { return []byte(b.String()), nil }

func (b *BigInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		b.SetInt64(0)
		return nil
	}
	s := string(bytes.Trim(data, `"`))
	if s == "" {
		b.SetInt64(0)
		return nil
	}
	// Accept exponent / decimal forms defensively (e.g. 1e+06, 100.0).
	if _, ok := b.SetString(s, 10); ok {
		return nil
	}
	f, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	if err != nil {
		return fmt.Errorf("blnk: invalid integer %q", s)
	}
	f.Int(&b.Int)
	return nil
}

// Time tolerates Blnk's zero value ("0001-01-01T00:00:00Z"), "" and null.
type Time struct{ time.Time }

func (t *Time) UnmarshalJSON(data []byte) error {
	s := string(bytes.Trim(bytes.TrimSpace(data), `"`))
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("blnk: invalid time %q: %w", s, err)
	}
	t.Time = parsed
	return nil
}

func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time.Format(time.RFC3339Nano))
}

// MetaData is free-form metadata attached to any Blnk entity.
type MetaData map[string]any

// ---------------------------------------------------------------------------
// Ledgers
// ---------------------------------------------------------------------------

type CreateLedgerRequest struct {
	Name     string   `json:"name"`
	MetaData MetaData `json:"meta_data,omitempty"`
}

type Ledger struct {
	LedgerID  string   `json:"ledger_id"`
	Name      string   `json:"name"`
	CreatedAt Time     `json:"created_at"`
	MetaData  MetaData `json:"meta_data,omitempty"`
}

// ---------------------------------------------------------------------------
// Balances
// ---------------------------------------------------------------------------

type CreateBalanceRequest struct {
	LedgerID   string   `json:"ledger_id"`
	Currency   string   `json:"currency"`
	IdentityID string   `json:"identity_id,omitempty"`
	MetaData   MetaData `json:"meta_data,omitempty"`
}

type Balance struct {
	BalanceID             string   `json:"balance_id"`
	LedgerID              string   `json:"ledger_id"`
	IdentityID            string   `json:"identity_id"`
	Indicator             string   `json:"indicator"`
	Currency              string   `json:"currency"`
	Balance               BigInt   `json:"balance"`
	CreditBalance         BigInt   `json:"credit_balance"`
	DebitBalance          BigInt   `json:"debit_balance"`
	InflightBalance       BigInt   `json:"inflight_balance"`
	InflightCreditBalance BigInt   `json:"inflight_credit_balance"`
	InflightDebitBalance  BigInt   `json:"inflight_debit_balance"`
	QueuedCreditBalance   BigInt   `json:"queued_credit_balance"`
	QueuedDebitBalance    BigInt   `json:"queued_debit_balance"`
	Version               int64    `json:"version"`
	CreatedAt             Time     `json:"created_at"`
	InflightExpiresAt     Time     `json:"inflight_expires_at"`
	MetaData              MetaData `json:"meta_data,omitempty"`
}

// Available returns balance - inflight_debit_balance, which is exactly what Blnk
// checks before accepting a debit (see "Managing insufficient funds").
func (b *Balance) Available() *big.Int {
	return new(big.Int).Sub(&b.Balance.Int, &b.InflightDebitBalance.Int)
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

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

// Inflight update actions.
const (
	ActionCommit = "commit"
	ActionVoid   = "void"
)

// UpdateInflightRequest is the body of PUT /transactions/inflight/{id}.
// PreciseAmount (commit only) performs a partial commit.
type UpdateInflightRequest struct {
	Status        string  `json:"status"`
	PreciseAmount *BigInt `json:"precise_amount,omitempty"`
	SkipQueue     bool    `json:"skip_queue,omitempty"`
}

// UpdateInflightResponse covers both queued ({"queued":true,...}) and
// synchronous (child APPLIED/VOID transaction) responses.
type UpdateInflightResponse struct {
	Transaction
	Queued bool `json:"queued"`
}

// RefundRequest is the optional body of POST /refund-transaction/{id}.
type RefundRequest struct {
	SkipQueue   bool     `json:"skip_queue,omitempty"`
	Description string   `json:"description,omitempty"` // Core 0.15.3+
	MetaData    MetaData `json:"meta_data,omitempty"`   // merged onto inherited metadata
}

// ---------------------------------------------------------------------------
// Bulk
// ---------------------------------------------------------------------------

type BulkTransactionRequest struct {
	Atomic       bool                       `json:"atomic"`
	Inflight     bool                       `json:"inflight"`
	RunAsync     bool                       `json:"run_async"`
	SkipQueue    bool                       `json:"skip_queue"`
	DryRun       bool                       `json:"dry_run,omitempty"`
	Transactions []CreateTransactionRequest `json:"transactions"`
}

type BulkTransactionResponse struct {
	BatchID          string `json:"batch_id"`
	Status           string `json:"status"` // applied | inflight | processing
	TransactionCount int    `json:"transaction_count"`
	Message          string `json:"message,omitempty"`
}

type BulkCommitItem struct {
	TransactionID string  `json:"transaction_id"`
	PreciseAmount *BigInt `json:"precise_amount,omitempty"`
}

type BulkCommitInflightRequest struct {
	SkipQueue    bool             `json:"skip_queue,omitempty"`
	Transactions []BulkCommitItem `json:"transactions"`
}

type BulkVoidInflightRequest struct {
	SkipQueue      bool     `json:"skip_queue,omitempty"`
	TransactionIDs []string `json:"transaction_ids"`
}

type BulkInflightResult struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"` // succeeded | failed | queued
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

type BulkInflightResponse struct {
	Succeeded int                  `json:"succeeded"`
	Failed    int                  `json:"failed"`
	Results   []BulkInflightResult `json:"results"`
}

// ---------------------------------------------------------------------------
// Filter (POST /{collection}/filter, Core 0.13.2+)
// ---------------------------------------------------------------------------

type Filter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"` // eq ne gt gte lt lte in between like ilike isnull isnotnull
	Value    any    `json:"value,omitempty"`
	Values   []any  `json:"values,omitempty"`
}

type FilterRequest struct {
	Filters         []Filter `json:"filters"`
	LogicalOperator string   `json:"logical_operator,omitempty"` // and | or
	SortBy          string   `json:"sort_by,omitempty"`
	SortOrder       string   `json:"sort_order,omitempty"` // asc | desc
	IncludeCount    bool     `json:"include_count,omitempty"`
	Limit           int      `json:"limit,omitempty"` // max 100
	Offset          int      `json:"offset,omitempty"`
}

// ---------------------------------------------------------------------------
// Identities
// ---------------------------------------------------------------------------

type Identity struct {
	IdentityID       string   `json:"identity_id,omitempty"`
	IdentityType     string   `json:"identity_type"` // individual | organization
	OrganizationName string   `json:"organization_name,omitempty"`
	Category         string   `json:"category,omitempty"`
	FirstName        string   `json:"first_name,omitempty"`
	LastName         string   `json:"last_name,omitempty"`
	OtherNames       string   `json:"other_names,omitempty"`
	Gender           string   `json:"gender,omitempty"`
	EmailAddress     string   `json:"email_address,omitempty"`
	PhoneNumber      string   `json:"phone_number,omitempty"`
	Nationality      string   `json:"nationality,omitempty"`
	Street           string   `json:"street,omitempty"`
	Country          string   `json:"country,omitempty"`
	State            string   `json:"state,omitempty"`
	PostCode         string   `json:"post_code,omitempty"`
	City             string   `json:"city,omitempty"`
	MetaData         MetaData `json:"meta_data,omitempty"`
}

// ---------------------------------------------------------------------------
// Balance monitors
// ---------------------------------------------------------------------------

// MonitorCondition is evaluated after every update of the balance. Blnk's
// evaluator understands > < >= <= == != (not "="), and stores
// precise_value = int64(value × precision); send the threshold in the smallest
// unit with Precision 1 so no float rounding is involved.
type MonitorCondition struct {
	Field        string      `json:"field"`    // balance, credit_balance, debit_balance, inflight_balance, inflight_debit_balance, inflight_credit_balance
	Operator     string      `json:"operator"` // >, <, >=, <=, ==, !=
	Value        json.Number `json:"value"`
	Precision    int64       `json:"precision"`
	PreciseValue *BigInt     `json:"precise_value,omitempty"` // set on responses
}

// BalanceMonitor fires the global webhook event "balance.monitor" (data: the
// monitor) after every balance update while its condition holds. Blnk ignores
// description and call_back_url on create.
type BalanceMonitor struct {
	MonitorID   string           `json:"monitor_id,omitempty"`
	BalanceID   string           `json:"balance_id"`
	Condition   MonitorCondition `json:"condition"`
	Description string           `json:"description,omitempty"`
	MetaData    MetaData         `json:"meta_data,omitempty"`
	CreatedAt   Time             `json:"created_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Historical balances
// ---------------------------------------------------------------------------

// HistoricalBalance is GET /balances/{id}/at. It counts APPLIED transactions
// by COALESCE(effective_date, created_at) <= timestamp; there are no inflight fields.
type HistoricalBalance struct {
	Balance struct {
		BalanceID     string `json:"balance_id"`
		Currency      string `json:"currency"`
		Balance       BigInt `json:"balance"`
		CreditBalance BigInt `json:"credit_balance"`
		DebitBalance  BigInt `json:"debit_balance"`
	} `json:"balance"`
	Timestamp  Time `json:"timestamp"`
	FromSource bool `json:"from_source"`
}

// ---------------------------------------------------------------------------
// Dry run (Core 0.15.3+)
// ---------------------------------------------------------------------------

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

type PreviewBalance struct {
	BalanceID        string `json:"balance_id"`
	Role             string `json:"role"` // source | destination
	Currency         string `json:"currency"`
	CurrentBalance   BigInt `json:"current_balance"`
	CurrentAvailable BigInt `json:"current_available"`
	ResultingBalance BigInt `json:"resulting_balance"`
	ResultingAvail   BigInt `json:"resulting_available"`
}

// ---------------------------------------------------------------------------
// Reconciliation
// ---------------------------------------------------------------------------

type MatchingCriterion struct {
	Field          string  `json:"field"`    // amount | date | description | reference | currency
	Operator       string  `json:"operator"` // equals | greater_than | less_than | contains
	Value          string  `json:"value,omitempty"`
	Pattern        string  `json:"pattern,omitempty"`
	AllowableDrift float64 `json:"allowable_drift,omitempty"` // amount: fraction (0.01 = 1%, Core 0.15.0+); date: seconds
}

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

type UploadResponse struct {
	UploadID    string `json:"upload_id"`
	RecordCount int    `json:"record_count"`
	Source      string `json:"source"`
}

type StartReconciliationRequest struct {
	UploadID             string                `json:"upload_id,omitempty"`
	ExternalTransactions []ExternalTransaction `json:"external_transactions,omitempty"` // start-instant only
	Strategy             string                `json:"strategy"`                        // one_to_one | one_to_many | many_to_one
	GroupingCriteria     string                `json:"grouping_criteria,omitempty"`
	DryRun               bool                  `json:"dry_run,omitempty"`
	MatchingRuleIDs      []string              `json:"matching_rule_ids"`
}

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
