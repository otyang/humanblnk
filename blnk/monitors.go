package blnk

import (
	"context"
	"encoding/json"
	"net/http"
)

// CreateBalanceMonitor creates a balance monitor (POST /balance-monitors).
func (c *Client) CreateBalanceMonitor(ctx context.Context, m BalanceMonitor) (*BalanceMonitor, error) {
	var out BalanceMonitor
	return &out, c.do(ctx, http.MethodPost, "/balance-monitors", m, &out)
}

// DeleteBalanceMonitor deletes a balance monitor.
func (c *Client) DeleteBalanceMonitor(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/balance-monitors/"+esc(id), nil, nil)
}

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
