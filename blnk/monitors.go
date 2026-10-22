package blnk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Create creates a balance monitor (POST /balance-monitors).
func (s *MonitorService) Create(ctx context.Context, m BalanceMonitor) (*BalanceMonitor, error) {
	var out BalanceMonitor
	return &out, s.client.doOnce(ctx, http.MethodPost, "/balance-monitors", m, &out)
}

// Get fetches a balance monitor by id.
func (s *MonitorService) Get(ctx context.Context, id string) (*BalanceMonitor, error) {
	var out BalanceMonitor
	return &out, s.client.do(ctx, http.MethodGet, "/balance-monitors/"+esc(id), nil, &out)
}

// List lists every balance monitor.
func (s *MonitorService) List(ctx context.Context) ([]BalanceMonitor, error) {
	return getList[BalanceMonitor](ctx, s.client, "/balance-monitors")
}

// ListByBalance lists the monitors on one balance.
func (s *MonitorService) ListByBalance(ctx context.Context, balanceID string) ([]BalanceMonitor, error) {
	return getList[BalanceMonitor](ctx, s.client, "/balance-monitors/balances/"+esc(balanceID))
}

// Update rewrites a monitor's balance, condition field, operator, value and
// description (PUT /balance-monitors/{id}), and clears its stored CallBackURL.
// It doesn't recompute precise_value, which is what Blnk compares against, so
// a new threshold (Value or Precision) never takes effect: use Replace.
func (s *MonitorService) Update(ctx context.Context, id string, m BalanceMonitor) error {
	return s.client.do(ctx, http.MethodPut, "/balance-monitors/"+esc(id), m, nil)
}

// Replace swaps monitor id for a new one built from m, the way to change a
// threshold (see Update). The new monitor is created before the old one is
// deleted, so the balance is never unwatched; it has a new MonitorID, and
// both may fire on a balance update in between. If the delete fails, the new
// monitor is returned with the error and the old one is still in place.
func (s *MonitorService) Replace(ctx context.Context, id string, m BalanceMonitor) (*BalanceMonitor, error) {
	m.MonitorID = ""
	created, err := s.Create(ctx, m)
	if err != nil {
		return nil, err
	}
	if err := s.Delete(ctx, id); err != nil {
		return created, fmt.Errorf("blnk: new monitor %s created but old monitor %s not deleted: %w", created.MonitorID, id, err)
	}
	return created, nil
}

// Delete deletes a balance monitor.
func (s *MonitorService) Delete(ctx context.Context, id string) error {
	return s.client.do(ctx, http.MethodDelete, "/balance-monitors/"+esc(id), nil, nil)
}

// MonitorCondition is evaluated after every update of the balance. Blnk's
// evaluator understands > < >= <= == != only: the docs' "=" for equality
// matches nothing, so a monitor using it never fires. Blnk stores
// precise_value = int64(value × precision), computed in float64: send the
// threshold in the smallest unit with Precision 1, and keep it within 2^53,
// the largest range where every whole number survives that unchanged.
type MonitorCondition struct {
	Field        string      `json:"field"`    // balance, credit_balance, debit_balance, inflight_balance, inflight_debit_balance, inflight_credit_balance
	Operator     string      `json:"operator"` // >, <, >=, <=, ==, !=
	Value        json.Number `json:"value"`
	Precision    int64       `json:"precision"`
	PreciseValue *BigInt     `json:"precise_value,omitempty"` // set on responses
}

// BalanceMonitor fires the global webhook event "balance.monitor" (data: the
// monitor) after every balance update while its condition holds.
type BalanceMonitor struct {
	MonitorID   string           `json:"monitor_id,omitempty"`
	BalanceID   string           `json:"balance_id"`
	Condition   MonitorCondition `json:"condition"`
	Description string           `json:"description,omitempty"`   // ignored by Create; set it with Update
	CallBackURL string           `json:"call_back_url,omitempty"` // stored by Create, never returned or called
	CreatedAt   Time             `json:"created_at,omitzero"`
}
