package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// ----- Balance alerts --------------------------------------------------------------

// WatchInput sets up an alert on an account's balance, e.g. "tell me when
// @PayoutClearing's float drops below 1,000,000".
type WatchInput struct {
	// Set exactly one: the customer account, or the system account, to watch.
	AccountID AccountID
	System    SystemRef
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
	AccountID string // Blnk balance id of the watched account (customer or system)
	Field     string
	Operator  string
	Threshold Money
}

// WatchBalance creates a Blnk balance monitor. Alerts arrive through your
// global webhook as EventBalanceAlert (see Service.ParseWebhook).
func (s operationsAPI) WatchBalance(ctx context.Context, in WatchInput) (*Watch, error) {
	const op = "ledger.Operations.WatchBalance"
	var b *blnk.Balance
	var err error
	switch {
	case in.AccountID != "" && in.System != (SystemRef{}):
		return nil, invalid(op, "set AccountID or System, not both")
	case in.AccountID != "":
		b, err = s.loadAccount(ctx, op, in.AccountID)
	case in.System != (SystemRef{}):
		b, err = s.loadSystem(ctx, op, in.System)
	default:
		return nil, invalid(op, "account id or system account is required")
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

// Unwatch deletes a balance monitor.
func (s operationsAPI) Unwatch(ctx context.Context, watchID string) error {
	if watchID == "" {
		return invalid("ledger.Operations.Unwatch", "watch id is required")
	}
	return mapErr("ledger.Operations.Unwatch", s.c.DeleteBalanceMonitor(ctx, watchID))
}

// ----- Webhooks ------------------------------------------------------------------

// Event names Blnk sends to the global webhook.
const (
	EventTransactionQueued    = "transaction.queued"
	EventTransactionApplied   = "transaction.applied"
	EventTransactionScheduled = "transaction.scheduled"
	EventTransactionInflight  = "transaction.inflight"
	EventTransactionVoid      = "transaction.void" // also sent when a hold expires
	EventTransactionRejected  = "transaction.rejected"
	EventBulkApplied          = "bulk_transaction.applied"
	EventBulkInflight         = "bulk_transaction.inflight"
	EventBulkFailed           = "bulk_transaction.failed"
	EventBalanceAlert         = "balance.monitor"
	EventBalanceCreated       = "balance.created"
	EventIdentityCreated      = "identity.created"
	EventLedgerCreated        = "ledger.created"
	EventSystemError          = "system.error"
)

// Event is a decoded Blnk webhook. Exactly one of the typed fields is set for
// known events; Data always holds the raw payload.
type Event struct {
	Name string

	Receipt *Receipt       // transaction.* events
	Bulk    *BulkEvent     // bulk_transaction.*
	Alert   *Watch         // balance.monitor
	Account *Account       // balance.created, for a customer account (or pot or escrow)
	System  *SystemAccount // balance.created, for a system ("@") account

	Data json.RawMessage
}

// BulkEvent is the payload of bulk_transaction.* events.
type BulkEvent struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
	Count   int    `json:"transaction_count"` // non-failed transactions
	Error   string `json:"error,omitempty"`
}

// Webhook verification settings.
type WebhookConfig struct {
	// Secret is Blnk's server.secret_key. When set, unsigned or mis-signed
	// deliveries are rejected with ErrSignature. When empty, nothing is
	// verified: leave it empty only if your Blnk doesn't sign webhooks.
	Secret string
	// Tolerance rejects deliveries whose X-Blnk-Timestamp is further than this
	// from now (replay protection). Default 5 minutes; negative disables it.
	Tolerance time.Duration
}

// maxWebhookBody bounds what ParseWebhook reads.
const maxWebhookBody = 5 << 20

// ParseWebhook verifies (when cfg.Secret is set) and decodes a Blnk webhook
// request. Blnk delivers at least once and retries on non-2xx, so make your
// handling idempotent, e.g. keyed on Receipt.TransactionID + Name.
func (s *Service) ParseWebhook(r *http.Request, cfg WebhookConfig) (*Event, error) {
	const op = "ledger.ParseWebhook"
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		return nil, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	if len(body) > maxWebhookBody {
		return nil, invalid(op, "webhook body larger than %d bytes", maxWebhookBody)
	}
	if cfg.Secret != "" {
		tol := cfg.Tolerance
		if tol == 0 {
			tol = 5 * time.Minute
		}
		if err := blnk.VerifySignature(cfg.Secret, r.Header.Get(blnk.HeaderSignature),
			r.Header.Get(blnk.HeaderTimestamp), body, s.cfg.Now(), tol); err != nil {
			return nil, &OpError{Op: op, Kind: ErrSignature, Cause: err}
		}
	}
	e, err := s.DecodeEvent(body)
	if err != nil {
		return nil, err
	}
	if e.Alert != nil && e.Alert.Threshold.PreciseAmount != nil && e.Alert.Threshold.Currency == "" {
		// Blnk's monitor payload has no currency; read it from the account.
		b, err := s.c.GetBalance(r.Context(), e.Alert.AccountID)
		if err != nil {
			return nil, mapErr(op, err)
		}
		e.Alert.Threshold = s.money(e.Alert.Threshold.PreciseAmount, s.Precision(b.Currency), b.Currency)
	}
	return e, nil
}

// DecodeEvent decodes a webhook body that has already been verified.
func (s *Service) DecodeEvent(body []byte) (*Event, error) {
	const op = "ledger.DecodeEvent"
	var env struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Event == "" {
		return nil, invalid(op, "not a Blnk webhook: %v", err)
	}
	e := &Event{Name: env.Event, Data: env.Data}
	switch {
	case strings.HasPrefix(env.Event, "transaction.") && !strings.HasSuffix(env.Event, ".metadata.updated"):
		var t blnk.Transaction
		if err := json.Unmarshal(env.Data, &t); err != nil {
			return nil, invalid(op, "%s payload: %v", env.Event, err)
		}
		e.Receipt = s.toReceipt(&t)
	case strings.HasPrefix(env.Event, "bulk_transaction."):
		e.Bulk = &BulkEvent{}
		if err := json.Unmarshal(env.Data, e.Bulk); err != nil {
			return nil, invalid(op, "%s payload: %v", env.Event, err)
		}
	case env.Event == EventBalanceAlert:
		var m blnk.BalanceMonitor
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, invalid(op, "%s payload: %v", env.Event, err)
		}
		w := &Watch{ID: m.MonitorID, AccountID: m.BalanceID, Field: m.Condition.Field, Operator: m.Condition.Operator}
		if m.Condition.PreciseValue != nil {
			// The payload has no currency: the threshold is the raw smallest-unit
			// number. ParseWebhook (and WebhookHandler) fill in currency and value.
			w.Threshold = Money{PreciseAmount: new(big.Int).Set(&m.Condition.PreciseValue.Int)}
		}
		e.Alert = w
	case env.Event == EventBalanceCreated:
		var b blnk.Balance
		if err := json.Unmarshal(env.Data, &b); err != nil {
			return nil, invalid(op, "%s payload: %v", env.Event, err)
		}
		if strings.HasPrefix(b.Indicator, "@") {
			e.System = s.toSystemAccount(&b)
		} else {
			e.Account = s.toAccount(&b)
		}
	}
	return e, nil
}

// WebhookHandler is an http.Handler for Blnk's global webhook. It verifies and
// decodes each delivery and calls fn. A nil error answers 200; an error
// answers 500 so Blnk retries, except for bad signatures and malformed bodies,
// which answer 401 and 400 (retrying won't help).
func (s *Service) WebhookHandler(cfg WebhookConfig, fn func(ctx context.Context, e *Event) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		e, err := s.ParseWebhook(r, cfg)
		switch {
		case errors.Is(err, ErrSignature):
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := fn(r.Context(), e); err != nil {
			http.Error(w, fmt.Sprintf("handler: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
