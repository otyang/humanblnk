package blnk

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Create creates a balance (POST /balances).
func (s *BalanceService) Create(ctx context.Context, r CreateBalanceRequest) (*Balance, error) {
	var out Balance
	return &out, s.client.doOnce(ctx, http.MethodPost, "/balances", r, &out)
}

// Get fetches a balance by id. Set o.WithQueued for the queued credit and
// debit balances, which Available needs; o.Include embeds the balance's
// "identity" and/or "ledger".
func (s *BalanceService) Get(ctx context.Context, id string, o GetBalanceOptions) (*Balance, error) {
	q := url.Values{}
	if o.WithQueued {
		q.Set("with_queued", "true")
	}
	for _, inc := range o.Include {
		q.Add("include", inc)
	}
	path := "/balances/" + esc(id)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out Balance
	return &out, s.client.do(ctx, http.MethodGet, path, nil, &out)
}

// GetByIndicator looks up an internal balance such as "@FeeIncome" or "@PaystackClearing".
func (s *BalanceService) GetByIndicator(ctx context.Context, indicator, currency string) (*Balance, error) {
	var out Balance
	path := fmt.Sprintf("/balances/indicator/%s/currency/%s", esc(indicator), esc(currency))
	return &out, s.client.do(ctx, http.MethodGet, path, nil, &out)
}

// GetAt returns the balance as it stood at t (Core 0.8.4+). fromSource
// recomputes from transactions instead of the latest snapshot (0.10.1+).
func (s *BalanceService) GetAt(ctx context.Context, id string, t time.Time, fromSource bool) (*HistoricalBalance, error) {
	q := url.Values{"timestamp": {t.UTC().Format(time.RFC3339Nano)}}
	if fromSource {
		q.Set("from_source", "true")
	}
	var out HistoricalBalance
	return &out, s.client.do(ctx, http.MethodGet, "/balances/"+esc(id)+"/at?"+q.Encode(), nil, &out)
}

// List lists balances (GET /balances).
func (s *BalanceService) List(ctx context.Context, o ListOptions) ([]Balance, error) {
	return getList[Balance](ctx, s.client, o.path("/balances"))
}

// Filter runs POST /balances/filter.
func (s *BalanceService) Filter(ctx context.Context, r FilterRequest) ([]Balance, int, error) {
	return filterList[Balance](ctx, s.client, "/balances/filter", r)
}

// UpdateIdentity moves a balance to another identity (PUT /balances/{id}/identity).
func (s *BalanceService) UpdateIdentity(ctx context.Context, id, identityID string) error {
	return s.client.do(ctx, http.MethodPut, "/balances/"+esc(id)+"/identity", map[string]string{"identity_id": identityID}, nil)
}

// Lineage reports where a balance's funds came from, per provider
// (GET /balances/{id}/lineage). The balance needs TrackFundLineage.
func (s *BalanceService) Lineage(ctx context.Context, id string) (*BalanceLineage, error) {
	var out BalanceLineage
	return &out, s.client.do(ctx, http.MethodGet, "/balances/"+esc(id)+"/lineage", nil, &out)
}

// TakeSnapshots starts snapshotting every balance in the background
// (POST /balances-snapshots). batchSize <= 0 leaves Blnk's default of 1000.
func (s *BalanceService) TakeSnapshots(ctx context.Context, batchSize int) error {
	path := "/balances-snapshots"
	if batchSize > 0 {
		path += "?batch_size=" + strconv.Itoa(batchSize)
	}
	return s.client.do(ctx, http.MethodPost, path, nil, nil)
}

// GetBalanceOptions are the query options of GET /balances/{id}.
type GetBalanceOptions struct {
	WithQueued bool     // include queued_credit_balance and queued_debit_balance
	Include    []string // "identity", "ledger"
}

// Fund-lineage allocation strategies: which provider's funds a debit spends first.
const (
	AllocationFIFO         = "FIFO" // Blnk's default
	AllocationLIFO         = "LIFO"
	AllocationProportional = "PROPORTIONAL"
)

// CreateBalanceRequest is the body of POST /balances.
type CreateBalanceRequest struct {
	LedgerID           string   `json:"ledger_id"`
	Currency           string   `json:"currency"`
	IdentityID         string   `json:"identity_id,omitempty"`
	Indicator          string   `json:"indicator,omitempty"` // Core 0.15.3+; "@name", only with LedgerID "general_ledger_id"
	TrackFundLineage   bool     `json:"track_fund_lineage,omitempty"`
	AllocationStrategy string   `json:"allocation_strategy,omitempty"`
	MetaData           MetaData `json:"meta_data,omitempty"`
}

// Balance is a Blnk balance. Amounts are in the smallest unit.
type Balance struct {
	BalanceID             string    `json:"balance_id"`
	LedgerID              string    `json:"ledger_id"`
	IdentityID            string    `json:"identity_id"`
	Indicator             string    `json:"indicator"`
	Currency              string    `json:"currency"`
	Balance               BigInt    `json:"balance"`
	CreditBalance         BigInt    `json:"credit_balance"`
	DebitBalance          BigInt    `json:"debit_balance"`
	InflightBalance       BigInt    `json:"inflight_balance"`
	InflightCreditBalance BigInt    `json:"inflight_credit_balance"`
	InflightDebitBalance  BigInt    `json:"inflight_debit_balance"`
	QueuedCreditBalance   BigInt    `json:"queued_credit_balance"` // only with GetBalanceOptions.WithQueued
	QueuedDebitBalance    BigInt    `json:"queued_debit_balance"`  // only with GetBalanceOptions.WithQueued
	Version               int64     `json:"version"`
	TrackFundLineage      bool      `json:"track_fund_lineage"`
	AllocationStrategy    string    `json:"allocation_strategy,omitempty"`
	CreatedAt             Time      `json:"created_at"`
	InflightExpiresAt     Time      `json:"inflight_expires_at"`
	Identity              *Identity `json:"identity,omitempty"` // with Include "identity"
	Ledger                *Ledger   `json:"ledger,omitempty"`   // with Include "ledger"
	MetaData              MetaData  `json:"meta_data,omitempty"`
}

// Available returns balance - inflight_debit_balance - queued_debit_balance,
// the funds Blnk checks before accepting a debit (see "Managing insufficient
// funds"). Fetch the balance with GetBalanceOptions.WithQueued, or queued
// debits read as 0 and this overstates what can be spent.
func (b *Balance) Available() *big.Int {
	out := new(big.Int).Sub(&b.Balance.Int, &b.InflightDebitBalance.Int)
	return out.Sub(out, &b.QueuedDebitBalance.Int)
}

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

// BalanceLineage is GET /balances/{id}/lineage.
type BalanceLineage struct {
	BalanceID          string              `json:"balance_id"`
	TotalWithLineage   BigInt              `json:"total_with_lineage"`
	AggregateBalanceID string              `json:"aggregate_balance_id"`
	Providers          []ProviderBreakdown `json:"providers"`
}

// ProviderBreakdown is how much of a balance came from one provider.
type ProviderBreakdown struct {
	Provider        string `json:"provider"`
	Amount          BigInt `json:"amount"`
	Available       BigInt `json:"available"`
	Spent           BigInt `json:"spent"`
	ShadowBalanceID string `json:"shadow_balance_id"`
}
