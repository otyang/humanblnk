// Package blnkfake is a fake Blnk Core for tests: an
// httptest server that answers canned routes and records every request.
package blnkfake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money/currency"
)

// Currencies the tests use.
var (
	USD = currency.MustGet(currency.USD)
	NGN = currency.MustGet(currency.NGN)
)

// Call is one request the fake received.
type Call struct {
	Method, Path, Query string
	Body                map[string]any
}

// Fake answers like Blnk Core from canned routes ("METHOD /path"). Unknown
// routes are 404 GEN_NOT_FOUND.
type Fake struct {
	routes map[string]func(c Call) (int, string)
	calls  []Call
}

// New starts a fake with three accounts: bln_usd and bln_usd2 in USD,
// bln_ngn in NGN. It returns a client on it that never retries.
func New(t *testing.T) (*blnk.Client, *Fake) {
	t.Helper()
	f := &Fake{routes: map[string]func(Call) (int, string){}}
	f.On("GET /balances/bln_usd", 200, `{"balance_id":"bln_usd","ledger_id":"ldg_wallets","identity_id":"idt_ada","currency":"USD","balance":150025,"inflight_debit_balance":25,"queued_debit_balance":100000}`)
	f.On("GET /balances/bln_usd2", 200, `{"balance_id":"bln_usd2","currency":"USD","balance":0}`)
	f.On("GET /balances/bln_ngn", 200, `{"balance_id":"bln_ngn","currency":"NGN","balance":0}`)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return blnk.New(srv.URL, blnk.WithRetries(0, 0)), f
}

// On answers route with status and body.
func (f *Fake) On(route string, status int, body string) {
	f.routes[route] = func(Call) (int, string) { return status, body }
}

// OnFunc answers route with h.
func (f *Fake) OnFunc(route string, h func(c Call) (int, string)) { f.routes[route] = h }

// Sent returns the requests made to route, in order.
func (f *Fake) Sent(route string) []Call {
	var out []Call
	for _, c := range f.calls {
		if c.Method+" "+c.Path == route {
			out = append(out, c)
		}
	}
	return out
}

// ServeHTTP records the request and answers it.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := Call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &c.Body)
	}
	f.calls = append(f.calls, c)
	status, body := http.StatusNotFound, APIError("GEN_NOT_FOUND")
	if h, ok := f.routes[r.Method+" "+r.URL.Path]; ok {
		status, body = h(c)
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// APIError is a Blnk error body with code.
func APIError(code string) string {
	return `{"error":"x","error_detail":{"code":"` + code + `","message":"x"}}`
}
