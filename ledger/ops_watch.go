package ledger

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/otyang/humanblnk/blnk"
)

// WatchInput sets up an alert on an account's balance, e.g. "tell me when
// @PayoutClearing's float drops below 1,000,000".
type WatchInput struct {
	// Set exactly one: the customer account, or the house account, to watch.
	AccountID AccountID
	House     HouseRef
	Field     string // balance (default), credit_balance, debit_balance, inflight_balance, inflight_debit_balance, inflight_credit_balance
	Operator  string // <, <=, >, >=, ==, !=  ("=" is accepted and sent as "==")
	Threshold Money
	MetaData  map[string]any
}

// Watch is a balance monitor. When its condition holds, Blnk sends the
// "balance.monitor" webhook after every update of the balance (not only the
// first time), so debounce alerts on your side.
type Watch struct {
	ID        string
	AccountID string // Blnk balance id of the watched account (customer or house)
	Field     string
	Operator  string
	Threshold Money
}

// WatchBalance creates a Blnk balance monitor. Alerts arrive through your
// global webhook as EventBalanceAlert (see Service.ParseWebhook).
func (s opsAPI) WatchBalance(ctx context.Context, in WatchInput) (*Watch, error) {
	const op = "ledger.Ops.WatchBalance"
	var b *blnk.Balance
	var err error
	switch {
	case in.AccountID != "" && in.House != (HouseRef{}):
		return nil, invalid(op, "set AccountID or House, not both")
	case in.AccountID != "":
		b, err = s.loadAccount(ctx, op, in.AccountID)
	case in.House != (HouseRef{}):
		b, err = s.loadHouse(ctx, op, in.House)
	default:
		return nil, invalid(op, "account id or house account is required")
	}
	if err != nil {
		return nil, err
	}
	field := firstNonEmpty(in.Field, "balance")
	switch field {
	case "balance", "credit_balance", "debit_balance", "inflight_balance", "inflight_debit_balance", "inflight_credit_balance":
	default:
		return nil, invalid(op, "unknown field %q", field)
	}
	operator := strings.TrimSpace(in.Operator)
	if operator == "=" {
		operator = "==" // Blnk accepts "=" but its evaluator never matches it
	}
	switch operator {
	case "<", "<=", ">", ">=", "==", "!=":
	default:
		return nil, invalid(op, "operator must be one of < <= > >= == !=")
	}
	th := in.Threshold
	if th.Currency != "" && !strings.EqualFold(th.Currency, b.Currency) {
		return nil, invalid(op, "threshold currency %s differs from account currency %s", th.Currency, b.Currency)
	}
	if th, err = s.resolve(op, th, b.Currency); err != nil {
		return nil, err
	}
	// Blnk stores int64(value × precision) via float math; sending the
	// smallest-unit integer with precision 1 keeps the threshold exact.
	m, err := s.c.CreateBalanceMonitor(ctx, blnk.BalanceMonitor{
		BalanceID: b.BalanceID, MetaData: in.MetaData,
		Condition: blnk.MonitorCondition{
			Field: field, Operator: operator, Value: json.Number(th.PreciseAmount.String()), Precision: 1,
		},
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return &Watch{ID: m.MonitorID, AccountID: b.BalanceID, Field: field, Operator: operator, Threshold: th}, nil
}

// UnwatchBalance deletes a balance monitor.
func (s opsAPI) UnwatchBalance(ctx context.Context, watchID string) error {
	if watchID == "" {
		return invalid("ledger.Ops.UnwatchBalance", "watch id is required")
	}
	return mapErr("ledger.Ops.UnwatchBalance", s.c.DeleteBalanceMonitor(ctx, watchID))
}
