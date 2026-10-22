package blnk_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/clients/blnk"
)

func TestRetryAfterIsHonoured(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ledger_id":"ldg_1"}`))
	}))
	defer srv.Close()
	c := blnk.New(srv.URL, blnk.WithRetries(1, time.Millisecond))
	start := time.Now()
	if _, err := c.Ledgers.Get(context.Background(), "ldg_1"); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); n != 2 || waited < 900*time.Millisecond {
		t.Fatalf("%d attempts after %s; want 2 after ~1s", n, waited)
	}
}

func TestVoidIsRetriedAfterGatewayTimeout(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"transaction_id":"txn_2","status":"VOID"}`))
	}))
	defer srv.Close()
	c := blnk.New(srv.URL, blnk.WithRetries(2, time.Millisecond))
	if _, err := c.Inflight.Update(context.Background(), "txn_1", blnk.UpdateInflightRequest{Status: blnk.ActionVoid}); err != nil || n != 2 {
		t.Fatalf("void: %d attempts, %v", n, err)
	}
}

func TestFilterLimitOverMaxIsRefused(t *testing.T) {
	var got capture
	c := server(t, 200, `[]`, &got)
	_, _, err := c.Transactions.Filter(context.Background(), blnk.FilterRequest{Limit: 101})
	if err == nil || got.method != "" {
		t.Fatalf("want a client-side error and no request, got %v (sent %s)", err, got.method)
	}
}

func TestIdentityUpdateKeepsTokenizedFields(t *testing.T) {
	var put map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"identity_id":"idt_1","meta_data":{"old":"x","tokenized_fields":{"first_name":true}}}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &put)
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	}))
	defer srv.Close()
	c := blnk.New(srv.URL, blnk.WithRetries(0, 0))
	if err := c.Identities.Update(context.Background(), "idt_1", blnk.Identity{MetaData: blnk.MetaData{"tier": "gold"}}); err != nil {
		t.Fatal(err)
	}
	md, _ := put["meta_data"].(map[string]any)
	tf, _ := md["tokenized_fields"].(map[string]any)
	if md["tier"] != "gold" || tf["first_name"] != true || md["old"] != nil {
		t.Fatalf("sent meta_data %v", md)
	}
}

func TestMonitorReplaceCreatesBeforeDeleting(t *testing.T) {
	var calls []string
	failDelete := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"monitor_id":"mon_new"}`))
		case failDelete:
			w.WriteHeader(http.StatusNotFound)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	c := blnk.New(srv.URL, blnk.WithRetries(0, 0))
	m, err := c.Monitors.Replace(context.Background(), "mon_old", blnk.BalanceMonitor{MonitorID: "mon_old", BalanceID: "bln_1"})
	if err != nil || m.MonitorID != "mon_new" || strings.Join(calls, ",") != "POST /balance-monitors,DELETE /balance-monitors/mon_old" {
		t.Fatalf("%+v %v %v", m, err, calls)
	}

	failDelete = true
	m, err = c.Monitors.Replace(context.Background(), "mon_old", blnk.BalanceMonitor{BalanceID: "bln_1"})
	if err == nil || m == nil || m.MonitorID != "mon_new" {
		t.Fatalf("failed delete: %+v %v", m, err)
	}
}
