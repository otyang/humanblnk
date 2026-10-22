// Package memblnk is an in-memory Blnk Core 0.15.6: an httptest server that
// speaks the part of Blnk's HTTP API humanblnk uses, so the whole of
// humanblnk, its blnk client included, runs against it unchanged.
//
// Where humanblnk depends on how Blnk behaves, memblnk behaves the same way,
// and the contract tests at the module root run against both to keep it so:
//
//   - System accounts ("@World", "@FeeIncome", …) are balances in the
//     General Ledger, made the first time a transaction names them.
//
//   - A split is recorded a posting at a time, each its own transaction
//     referenced "<ref>-1", "<ref>-2", … and naming the split as its parent.
//     The split itself isn't a transaction: Get on its id is not found.
//
//   - When a later posting of a split, or a later item of an atomic batch,
//     fails, the earlier ones stay applied and are refunded a moment later
//     (WithRollbackDelay) under "<id>_refund". Their references stay used.
//
//   - A hold is an INFLIGHT transaction that stays INFLIGHT; each commit or
//     void is a child transaction naming it as parent. A repeated partial
//     commit is applied again, as Blnk does.
//
//   - A balance monitor stores precise_value as int64(value × precision) in
//     float64, ignores a description on create, and fires the
//     balance.monitor webhook after each update while its condition holds.
//
//   - A split's postings share one created_at, so a newest-first listing
//     keeps them in order; a reference lookup answers without precision or
//     flags, as Blnk's does.
//
// It isn't Blnk where humanblnk doesn't look: nothing is queued (skip_queue
// or not, every write applies at once); hold expiry happens on the next
// request; splits take precise_distribution only and batches can't hold one;
// batches, commits and voids can't be previewed; effective_date is ignored;
// reconciliation matches one_to_one only. Webhook delivery is best effort.
package memblnk

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// Option configures a Server.
type Option func(*Server)

// WithRollbackDelay sets how long after a failed split or batch its earlier
// parts are refunded (200ms unless set, about what Blnk's worker takes).
func WithRollbackDelay(d time.Duration) Option {
	return func(s *Server) { s.rollbackDelay = max(d, 0) }
}

// WithWebhook sends Blnk's webhooks (balance.monitor, transaction.applied,
// transaction.inflight, transaction.void) to url, signed with secret as Blnk
// signs them.
func WithWebhook(url, secret string) Option {
	return func(s *Server) { s.webhookURL, s.secret = url, secret }
}

// Server is an in-memory Blnk Core.
type Server struct {
	URL string // the base URL to give blnk.New

	srv           *httptest.Server
	rollbackDelay time.Duration
	webhookURL    string
	secret        string
	background    sync.WaitGroup // rollbacks and webhook deliveries
	done          chan struct{}  // closed by Close: pending rollbacks are dropped

	mu     sync.Mutex
	closed bool
	last   time.Time // the latest created_at handed out
	outbox [][]byte  // webhook bodies to send once mu is released

	ledgers    map[string]*blnk.Ledger
	balances   map[string]*blnk.Balance
	identities map[string]*blnk.Identity
	txns       map[string]*txn
	byRef      map[string]*txn
	monitors   map[string]*blnk.BalanceMonitor
	rules      map[string]*blnk.MatchingRule
	uploads    map[string]*upload
	runs       map[string]*blnk.Reconciliation

	// Creation order of each kind, for stable listing.
	ledgerOrder, balanceOrder, identityOrder, txnOrder, monitorOrder []string
}

// GeneralLedger is the ledger Blnk keeps system accounts in.
const GeneralLedger = "general_ledger_id"

// New starts a Server. Close it when done.
func New(opts ...Option) *Server {
	s := &Server{
		rollbackDelay: 200 * time.Millisecond,
		done:          make(chan struct{}),
		ledgers:       map[string]*blnk.Ledger{},
		balances:      map[string]*blnk.Balance{},
		identities:    map[string]*blnk.Identity{},
		txns:          map[string]*txn{},
		byRef:         map[string]*txn{},
		monitors:      map[string]*blnk.BalanceMonitor{},
		rules:         map[string]*blnk.MatchingRule{},
		uploads:       map[string]*upload{},
		runs:          map[string]*blnk.Reconciliation{},
	}
	for _, o := range opts {
		o(s)
	}
	s.addLedger(&blnk.Ledger{LedgerID: GeneralLedger, Name: "General Ledger", CreatedAt: blnk.Time{Time: s.now()}})
	s.srv = httptest.NewServer(s.routes())
	s.URL = s.srv.URL
	return s
}

// Close stops the server. Rollbacks not yet due are dropped, and webhooks
// still being delivered are cancelled: deliveries are best effort.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	s.srv.Close()
	s.background.Wait()
}

// handler is one endpoint. It runs with mu held and returns the status and
// the value to send as JSON. The value may share state with the server, so
// it is encoded before mu is released.
type handler func(r *http.Request, body []byte) (int, any)

// locked runs h with the state locked, then sends any webhooks it queued.
func (s *Server) locked(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiError(blnk.CodeMalformedRequest, "read body: %v", err))
			return
		}
		s.mu.Lock()
		s.expireHolds()
		status, out := h(r, body)
		raw, err := json.Marshal(out)
		s.mu.Unlock()
		s.flush()
		if err != nil {
			status, raw = http.StatusInternalServerError, mustJSON(apiError(blnk.CodeInternal, "encode response: %v", err))
		}
		writeRaw(w, status, raw)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) { writeRaw(w, status, mustJSON(v)) }

func writeRaw(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// now is the time to stamp a new record with: strictly later than the last,
// so sorting by created_at follows creation order.
func (s *Server) now() time.Time {
	t := time.Now().UTC()
	if !t.After(s.last) {
		t = s.last.Add(time.Microsecond)
	}
	s.last = t
	return t
}

// newID is prefix and a random UUID, as Blnk makes ids.
func newID(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return prefix + h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// later runs f with the state locked after d, unless the server closes first.
func (s *Server) later(d time.Duration, f func()) {
	s.background.Add(1) // callers hold mu, so never after Close has begun waiting
	go func() {
		defer s.background.Done()
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.done:
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		f()
		s.mu.Unlock()
		s.flush()
	}()
}
