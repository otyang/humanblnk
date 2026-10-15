package blnk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// ----- Ledgers --------------------------------------------------------------

// CreateLedger creates a ledger (POST /ledgers).
func (c *Client) CreateLedger(ctx context.Context, r CreateLedgerRequest) (*Ledger, error) {
	var out Ledger
	return &out, c.do(ctx, http.MethodPost, "/ledgers", r, &out)
}

// GetLedger fetches a ledger by id.
func (c *Client) GetLedger(ctx context.Context, id string) (*Ledger, error) {
	var out Ledger
	return &out, c.do(ctx, http.MethodGet, "/ledgers/"+esc(id), nil, &out)
}

// ListLedgers lists ledgers, oldest first (GET /ledgers?limit=&offset=).
func (c *Client) ListLedgers(ctx context.Context, limit, offset int) ([]Ledger, error) {
	q := url.Values{"limit": {fmt.Sprint(limit)}, "offset": {fmt.Sprint(offset)}}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/ledgers?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	out, _, err := decodeList[Ledger](raw)
	return out, err
}

// FilterLedgers runs POST /ledgers/filter.
func (c *Client) FilterLedgers(ctx context.Context, r FilterRequest) ([]Ledger, int, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/ledgers/filter", r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[Ledger](raw)
}

// ----- Balances -------------------------------------------------------------

// CreateBalance creates a balance (POST /balances).
func (c *Client) CreateBalance(ctx context.Context, r CreateBalanceRequest) (*Balance, error) {
	var out Balance
	return &out, c.do(ctx, http.MethodPost, "/balances", r, &out)
}

// GetBalance fetches a balance by id.
func (c *Client) GetBalance(ctx context.Context, id string) (*Balance, error) {
	var out Balance
	return &out, c.do(ctx, http.MethodGet, "/balances/"+esc(id), nil, &out)
}

// GetBalanceByIndicator looks up an internal balance such as "@FeeIncome" or "@PaystackClearing".
func (c *Client) GetBalanceByIndicator(ctx context.Context, indicator, currency string) (*Balance, error) {
	var out Balance
	path := fmt.Sprintf("/balances/indicator/%s/currency/%s", esc(indicator), esc(currency))
	return &out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// CreateBalanceMonitor creates a balance monitor (POST /balance-monitors).
func (c *Client) CreateBalanceMonitor(ctx context.Context, m BalanceMonitor) (*BalanceMonitor, error) {
	var out BalanceMonitor
	return &out, c.do(ctx, http.MethodPost, "/balance-monitors", m, &out)
}

// DeleteBalanceMonitor deletes a balance monitor.
func (c *Client) DeleteBalanceMonitor(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/balance-monitors/"+esc(id), nil, nil)
}

// GetBalanceAt returns the balance as it stood at t (Core 0.8.4+). fromSource
// recomputes from transactions instead of the latest snapshot (0.10.1+).
func (c *Client) GetBalanceAt(ctx context.Context, id string, t time.Time, fromSource bool) (*HistoricalBalance, error) {
	q := url.Values{"timestamp": {t.UTC().Format(time.RFC3339Nano)}}
	if fromSource {
		q.Set("from_source", "true")
	}
	var out HistoricalBalance
	return &out, c.do(ctx, http.MethodGet, "/balances/"+esc(id)+"/at?"+q.Encode(), nil, &out)
}

// ----- Transactions ---------------------------------------------------------

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

// UpdateInflight commits (fully or partially) or voids an INFLIGHT transaction,
// or every transaction in an inflight bulk batch when id is a batch_id. It is
// not retried after a network error or gateway timeout (see WithRetries).
func (c *Client) UpdateInflight(ctx context.Context, id string, r UpdateInflightRequest) (*UpdateInflightResponse, error) {
	var out UpdateInflightResponse
	return &out, c.doOnce(ctx, http.MethodPut, "/transactions/inflight/"+esc(id), r, &out)
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

// BulkCommitInflight commits up to 100 independent inflight transactions.
// Like UpdateInflight, it is not retried when it may have reached Blnk.
func (c *Client) BulkCommitInflight(ctx context.Context, r BulkCommitInflightRequest) (*BulkInflightResponse, error) {
	var out BulkInflightResponse
	return &out, c.doOnce(ctx, http.MethodPost, "/transactions/inflight/bulk/commit", r, &out)
}

// BulkVoidInflight voids up to 100 independent inflight transactions.
func (c *Client) BulkVoidInflight(ctx context.Context, r BulkVoidInflightRequest) (*BulkInflightResponse, error) {
	var out BulkInflightResponse
	return &out, c.do(ctx, http.MethodPost, "/transactions/inflight/bulk/void", r, &out)
}

// ----- Filter / metadata ----------------------------------------------------

// FilterTransactions runs POST /transactions/filter. It handles both response
// shapes (bare array, or {"data": [...], "total_count": n} with include_count).
func (c *Client) FilterTransactions(ctx context.Context, r FilterRequest) ([]Transaction, int, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/transactions/filter", r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[Transaction](raw)
}

// FilterBalances runs POST /balances/filter.
func (c *Client) FilterBalances(ctx context.Context, r FilterRequest) ([]Balance, int, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/balances/filter", r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[Balance](raw)
}

func decodeList[T any](raw json.RawMessage) ([]T, int, error) {
	var arr []T
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, len(arr), nil
	}
	var wrapped struct {
		Data       []T `json:"data"`
		TotalCount int `json:"total_count"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, 0, fmt.Errorf("blnk: decode filter response: %w", err)
	}
	if wrapped.TotalCount == 0 {
		wrapped.TotalCount = len(wrapped.Data)
	}
	return wrapped.Data, wrapped.TotalCount, nil
}

// UpdateMetadata merges keys into the metadata of a ledger, balance,
// transaction or identity (POST /{entity_id}/metadata).
func (c *Client) UpdateMetadata(ctx context.Context, entityID string, md MetaData) (MetaData, error) {
	var out struct {
		MetaData MetaData `json:"meta_data"`
	}
	err := c.do(ctx, http.MethodPost, "/"+esc(entityID)+"/metadata", map[string]any{"meta_data": md}, &out)
	return out.MetaData, err
}

// ----- Identities -----------------------------------------------------------

// CreateIdentity creates an identity (POST /identities).
func (c *Client) CreateIdentity(ctx context.Context, id Identity) (*Identity, error) {
	var out Identity
	return &out, c.do(ctx, http.MethodPost, "/identities", id, &out)
}

// GetIdentity fetches an identity by id.
func (c *Client) GetIdentity(ctx context.Context, id string) (*Identity, error) {
	var out Identity
	return &out, c.do(ctx, http.MethodGet, "/identities/"+esc(id), nil, &out)
}

// ----- Reconciliation -------------------------------------------------------

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
