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

	Receipt *Receipt      // transaction.* events
	Bulk    *BulkEvent    // bulk_transaction.*
	Alert   *Watch        // balance.monitor
	Account *Account      // balance.created, for a customer account (or pot or escrow)
	House   *HouseAccount // balance.created, for a house ("@") account

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
		e.Alert.Threshold = s.money(e.Alert.Threshold.PreciseAmount, s.precisionOf(b), b.Currency)
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
			e.House = s.toHouseAccount(&b)
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
