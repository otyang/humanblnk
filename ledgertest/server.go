// Package ledgertest runs an in-memory Blnk Core emulator so applications can
// test code that uses ledger.Service without a real Blnk instance:
//
//	svc, core := ledgertest.New(t, ledger.Config{})
//	acct, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
//	svc.Wallet().Deposit(ctx, ledger.DepositInput{AccountID: acct.ID, Amount: ledger.Amount("100", "NGN"), Reference: "r1"})
//	core.Advance(25 * time.Hour) // expire holds, run scheduled transfers
//
// The real Service code runs unchanged against the emulator, so tests exercise
// the same requests production sends. It implements the Blnk behaviour this
// module relies on (balances, inflight, splits, refunds, bulk, filters,
// health, ledgers (create, get, list, filter), metadata, identities, balance
// monitors, historical balances, dry run,
// instant reconciliation and signed webhooks) and behaves like Blnk Core 0.15
// where that matters: a split is stored only as its legs ("<ref>-1", …, in
// reverse order, since Blnk doesn't keep request order), "@" accounts are
// stored by balance id, blank descriptions are rejected, and filter results
// leave parent_transaction empty. It is not a complete Blnk implementation.
package ledgertest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
)

// Server is the emulator. It is safe for concurrent use.
type Server struct {
	srv *httptest.Server

	mu         sync.Mutex
	now        time.Time
	seq        int
	ledgers    map[string]*blnk.Ledger
	balances   map[string]*balance
	indicators map[string]string // indicator|currency → balance id
	txns       map[string]*txn
	order      []string          // transaction ids in creation order
	refs       map[string]string // reference → transaction id
	identities map[string]*blnk.Identity
	monitors   map[string]*blnk.BalanceMonitor
	batches    map[string][]string // batch id → transaction ids
	splits     map[string][]string // split parent id → leg ids (parents aren't stored)
	splitRefs  map[string]bool     // references taken by split parents
	rules      map[string]*blnk.MatchingRule
	recons     map[string]*blnk.Reconciliation

	// Events records every webhook the emulator would send, oldest first.
	events []Event
	hook   func(Event)
	quiet  bool // dry runs emit nothing
}

// Event is a webhook notification as Blnk would send it.
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// NewServer starts an emulator. Close it when done (New does that for you).
func NewServer() *Server {
	s := &Server{
		now:        time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		ledgers:    map[string]*blnk.Ledger{},
		balances:   map[string]*balance{},
		indicators: map[string]string{},
		txns:       map[string]*txn{},
		refs:       map[string]string{},
		identities: map[string]*blnk.Identity{},
		monitors:   map[string]*blnk.BalanceMonitor{},
		batches:    map[string][]string{},
		splits:     map[string][]string{},
		splitRefs:  map[string]bool{},
		rules:      map[string]*blnk.MatchingRule{},
		recons:     map[string]*blnk.Reconciliation{},
	}
	s.ledgers["general_ledger_id"] = &blnk.Ledger{LedgerID: "general_ledger_id", Name: "General Ledger"}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// New starts an emulator, registers its cleanup on t, and returns a Service
// wired to it. cfg.LedgerID defaults to a ledger created for the test, and
// cfg.Now defaults to the emulator's clock.
func New(t testing.TB, cfg ledger.Config) (*ledger.Service, *Server) {
	t.Helper()
	s := NewServer()
	t.Cleanup(s.Close)
	if cfg.LedgerID == "" {
		cfg.LedgerID = ledger.LedgerID(s.CreateLedger("Test Ledger"))
	}
	if cfg.Now == nil {
		cfg.Now = s.Now
	}
	return ledger.New(s.Client(), cfg), s
}

// URL is the emulator's base URL, to pass to blnk.New.
func (s *Server) URL() string { return s.srv.URL }

// Close shuts the emulator down.
func (s *Server) Close() { s.srv.Close() }

// Client returns a blnk.Client pointed at the emulator, with retries off.
func (s *Server) Client(opts ...blnk.Option) *blnk.Client {
	return blnk.New(s.srv.URL, append([]blnk.Option{blnk.WithRetries(0, 0)}, opts...)...)
}

// Now is the emulator's clock.
func (s *Server) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// SetNow moves the clock to t and runs whatever became due (see Advance).
func (s *Server) SetNow(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = t
	s.runDue()
}

// Advance moves the clock forward by d and runs whatever became due:
// scheduled transactions post, inflight transactions past their expiry are
// voided, and those past their commit date are committed.
func (s *Server) Advance(d time.Duration) { s.SetNow(s.Now().Add(d)) }

// Events returns the webhooks emitted so far.
func (s *Server) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

// OnEvent calls fn for every webhook as it is emitted (under the server lock;
// don't call back into the server from fn).
func (s *Server) OnEvent(fn func(Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hook = fn
}

// WebhookRequest builds the HTTP request Blnk would POST for e, signed with
// secret the way Blnk signs it (empty secret = unsigned). Feed it to your
// webhook handler in tests.
func (s *Server) WebhookRequest(e Event, secret string) *http.Request {
	body, _ := json.Marshal(e)
	r := httptest.NewRequest(http.MethodPost, "/webhooks/blnk", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if secret != "" {
		ts := fmt.Sprint(s.Now().Unix())
		r.Header.Set(blnk.HeaderTimestamp, ts)
		r.Header.Set(blnk.HeaderSignature, blnk.Sign(secret, ts, body))
	}
	return r
}

func (s *Server) emit(name string, data any) {
	if s.quiet {
		return
	}
	raw, _ := json.Marshal(data)
	e := Event{Event: name, Data: raw}
	s.events = append(s.events, e)
	if s.hook != nil {
		s.hook(e)
	}
}

func (s *Server) id(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s_%06d", prefix, s.seq)
}

type apiErr struct {
	status int
	code   string
	msg    string
}

func (e *apiErr) Error() string { return e.msg }

func fail(status int, code, format string, args ...any) *apiErr {
	return &apiErr{status, code, fmt.Sprintf(format, args...)}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	out, status, err := s.route(r.Method, r.URL.Path, r.URL.Query(), body)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		ae, ok := err.(*apiErr)
		if !ok {
			ae = fail(400, blnk.CodeMalformedRequest, "%v", err)
		}
		w.WriteHeader(ae.status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":        ae.msg,
			"error_detail": map[string]any{"code": ae.code, "message": ae.msg},
		})
		return
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) route(method, path string, q map[string][]string, body []byte) (any, int, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	seg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	n := len(parts)
	decode := func(v any) error {
		if len(body) == 0 {
			return nil
		}
		if err := json.Unmarshal(body, v); err != nil {
			return fail(400, blnk.CodeMalformedRequest, "malformed JSON: %v", err)
		}
		return nil
	}

	switch {
	case method == "GET" && path == "/health":
		return map[string]any{"status": "UP"}, 200, nil

	// ledgers
	case method == "POST" && path == "/ledgers":
		var in blnk.CreateLedgerRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		id := s.id("ldg")
		l := &blnk.Ledger{LedgerID: id, Name: in.Name, MetaData: in.MetaData, CreatedAt: blnk.Time{Time: s.now}}
		s.ledgers[id] = l
		return l, 201, nil
	case method == "GET" && path == "/ledgers":
		return s.listLedgers(q), 200, nil
	case method == "POST" && path == "/ledgers/filter":
		rows := make([]any, 0, len(s.ledgers))
		for _, l := range s.sortedLedgers() {
			rows = append(rows, *l)
		}
		var in blnk.FilterRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		return applyFilter(in, rows)
	case method == "GET" && seg(0) == "ledgers" && n == 2:
		l, ok := s.ledgers[seg(1)]
		if !ok {
			return nil, 0, fail(404, blnk.CodeLedgerNotFound, "ledger %s not found", seg(1))
		}
		return l, 200, nil

	// balances
	case method == "POST" && path == "/balances":
		var in blnk.CreateBalanceRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		b, err := s.createBalance(in)
		if err != nil {
			return nil, 0, err
		}
		return b.view(), 201, nil
	case method == "POST" && path == "/balances/filter":
		return s.filterBalances(body)
	case method == "GET" && seg(0) == "balances" && seg(1) == "indicator" && n == 5:
		id, ok := s.indicators[seg(2)+"|"+strings.ToUpper(seg(4))]
		if !ok {
			return nil, 0, fail(404, blnk.CodeBalanceNotFound, "balance %s/%s not found", seg(2), seg(4))
		}
		return s.balances[id].view(), 200, nil
	case method == "GET" && seg(0) == "balances" && n == 3 && seg(2) == "at":
		return s.balanceAt(seg(1), q)
	case method == "GET" && seg(0) == "balances" && n == 2:
		b, ok := s.balances[seg(1)]
		if !ok {
			return nil, 0, fail(404, blnk.CodeBalanceNotFound, "balance %s not found", seg(1))
		}
		return b.view(), 200, nil
	case method == "POST" && path == "/balance-monitors":
		var m blnk.BalanceMonitor
		if err := decode(&m); err != nil {
			return nil, 0, err
		}
		if _, ok := s.balances[m.BalanceID]; !ok {
			return nil, 0, fail(404, blnk.CodeBalanceNotFound, "balance %s not found", m.BalanceID)
		}
		// precise_value = int64(value × precision), as Blnk computes it.
		v, ok := new(big.Rat).SetString(m.Condition.Value.String())
		if !ok || m.Condition.Field == "" || m.Condition.Operator == "" {
			return nil, 0, fail(400, blnk.CodeValidation, "condition field, operator and value are required")
		}
		v.Mul(v, new(big.Rat).SetInt64(m.Condition.Precision))
		pv := blnk.BigInt{}
		pv.Quo(v.Num(), v.Denom())
		m.Condition.PreciseValue = &pv
		m.MonitorID = s.id("mon")
		m.Description = "" // not part of Blnk's create request
		m.CreatedAt = blnk.Time{Time: s.now}
		s.monitors[m.MonitorID] = &m
		return m, 201, nil
	case method == "DELETE" && seg(0) == "balance-monitors" && n == 2:
		if _, ok := s.monitors[seg(1)]; !ok {
			return nil, 0, fail(404, blnk.CodeNotFound, "monitor %s not found", seg(1))
		}
		delete(s.monitors, seg(1))
		return map[string]any{"message": "deleted"}, 200, nil

	// identities
	case method == "POST" && path == "/identities":
		var in blnk.Identity
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		in.IdentityID = s.id("idt")
		s.identities[in.IdentityID] = &in
		return in, 201, nil
	case method == "GET" && seg(0) == "identities" && n == 2:
		i, ok := s.identities[seg(1)]
		if !ok {
			return nil, 0, fail(404, blnk.CodeNotFound, "identity %s not found", seg(1))
		}
		return i, 200, nil

	// transactions
	case method == "POST" && path == "/transactions":
		var in blnk.CreateTransactionRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		if in.DryRun {
			return s.preview(in)
		}
		t, err := s.createTxn(in, "")
		if err != nil {
			return nil, 0, err
		}
		return t.view(), 201, nil
	case method == "POST" && path == "/transactions/filter":
		return s.filterTxns(body)
	case method == "POST" && path == "/transactions/bulk":
		var in blnk.BulkTransactionRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		return s.bulk(in)
	case method == "POST" && path == "/transactions/inflight/bulk/commit":
		var in blnk.BulkCommitInflightRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		return s.bulkInflight(in.Transactions, blnk.ActionCommit), 200, nil
	case method == "POST" && path == "/transactions/inflight/bulk/void":
		var in blnk.BulkVoidInflightRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		items := make([]blnk.BulkCommitItem, len(in.TransactionIDs))
		for i, id := range in.TransactionIDs {
			items[i].TransactionID = id
		}
		return s.bulkInflight(items, blnk.ActionVoid), 200, nil
	case method == "PUT" && seg(0) == "transactions" && seg(1) == "inflight" && n == 3:
		var in blnk.UpdateInflightRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		t, err := s.updateInflight(seg(2), in)
		if err != nil {
			return nil, 0, err
		}
		return t.view(), 200, nil
	case method == "GET" && seg(0) == "transactions" && seg(1) == "reference" && n == 3:
		id, ok := s.refs[seg(2)]
		if !ok {
			return nil, 0, fail(404, blnk.CodeTxnNotFound, "transaction with reference %s not found", seg(2))
		}
		return s.txns[id].stored(), 200, nil
	case method == "GET" && seg(0) == "transactions" && n == 2:
		t, ok := s.txns[seg(1)]
		if !ok {
			return nil, 0, fail(404, blnk.CodeTxnNotFound, "transaction %s not found", seg(1))
		}
		return t.stored(), 200, nil
	case method == "POST" && seg(0) == "refund-transaction" && n == 2:
		var in blnk.RefundRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		t, err := s.refund(seg(1), in)
		if err != nil {
			return nil, 0, err
		}
		return t.view(), 201, nil

	// reconciliation
	case method == "POST" && path == "/reconciliation/matching-rules":
		var r blnk.MatchingRule
		if err := decode(&r); err != nil {
			return nil, 0, err
		}
		if r.Name == "" || len(r.Criteria) == 0 {
			return nil, 0, fail(400, blnk.CodeValidation, "name and criteria are required")
		}
		r.RuleID = s.id("rule")
		r.CreatedAt = blnk.Time{Time: s.now}
		s.rules[r.RuleID] = &r
		return r, 201, nil
	case method == "POST" && path == "/reconciliation/start-instant":
		var in blnk.StartReconciliationRequest
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		return s.reconcile(in)
	case method == "GET" && seg(0) == "reconciliation" && n == 2:
		r, ok := s.recons[seg(1)]
		if !ok {
			return nil, 0, fail(404, blnk.CodeNotFound, "reconciliation %s not found", seg(1))
		}
		return r, 200, nil

	// metadata
	case method == "POST" && n == 2 && seg(1) == "metadata":
		var in struct {
			MetaData blnk.MetaData `json:"meta_data"`
		}
		if err := decode(&in); err != nil {
			return nil, 0, err
		}
		md, err := s.updateMetadata(seg(0), in.MetaData)
		if err != nil {
			return nil, 0, err
		}
		return map[string]any{"meta_data": md}, 200, nil
	}
	return nil, 0, fail(404, blnk.CodeNotFound, "emulator: no route for %s %s", method, path)
}
