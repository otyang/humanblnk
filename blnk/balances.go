package blnk

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"time"
)

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

// FilterBalances runs POST /balances/filter.
func (c *Client) FilterBalances(ctx context.Context, r FilterRequest) ([]Balance, int, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/balances/filter", r, &raw); err != nil {
		return nil, 0, err
	}
	return decodeList[Balance](raw)
}

// CreateBalanceRequest is the body of POST /balances.
type CreateBalanceRequest struct {
	LedgerID   string   `json:"ledger_id"`
	Currency   string   `json:"currency"`
	IdentityID string   `json:"identity_id,omitempty"`
	MetaData   MetaData `json:"meta_data,omitempty"`
}

// Balance is a Blnk balance. Amounts are in the smallest unit.
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
