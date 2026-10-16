package blnk_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

type capture struct {
	method, path, query, ctype string
	body                       []byte
}

func server(t *testing.T, status int, resp string, got *capture) *blnk.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query, got.ctype = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return blnk.New(srv.URL, blnk.WithRetries(0, 0))
}

func TestSignatureMatchesBlnk(t *testing.T) {
	body := []byte(`{"event":"transaction.applied","data":{}}`)
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write([]byte("1700000000." + string(body)))
	want := hex.EncodeToString(mac.Sum(nil))
	if got := blnk.Sign("key", "1700000000", body); got != want {
		t.Fatalf("sign = %s, want %s", got, want)
	}
	now := time.Unix(1700000100, 0)
	if err := blnk.VerifySignature("key", want, "1700000000", body, now, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := blnk.VerifySignature("key", want, "1700000000", append(body, ' '), now, 0); !errors.Is(err, blnk.ErrBadSignature) {
		t.Fatalf("tampered body: %v", err)
	}
	if err := blnk.VerifySignature("key", want, "1700000000", body, now.Add(time.Hour), 5*time.Minute); !errors.Is(err, blnk.ErrStale) {
		t.Fatalf("stale: %v", err)
	}
}

func TestPreviewRefusesWhenDryRunIgnored(t *testing.T) {
	var got capture
	// An old Core answers with a normal (posted) transaction.
	c := server(t, 201, `{"transaction_id":"txn_1","status":"APPLIED"}`, &got)
	_, err := c.PreviewTransaction(context.Background(), blnk.CreateTransactionRequest{Reference: "r", PreciseAmount: blnk.NewBigInt(1)})
	if err == nil || !strings.Contains(err.Error(), "not honoured") {
		t.Fatalf("want dry_run-not-honoured error, got %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(got.body, &body)
	if body["dry_run"] != true {
		t.Fatalf("dry_run not sent: %s", got.body)
	}
}

func TestHistoricalBalanceQuery(t *testing.T) {
	var got capture
	c := server(t, 200, `{"balance":{"balance_id":"bln_1","currency":"NGN","balance":150025},"timestamp":"2026-01-31T23:59:59Z","from_source":true}`, &got)
	h, err := c.GetBalanceAt(context.Background(), "bln_1", time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC), true)
	if err != nil || h.Balance.Balance.Int64() != 150025 {
		t.Fatalf("%+v %v", h, err)
	}
	if got.path != "/balances/bln_1/at" || got.query != "from_source=true&timestamp=2026-01-31T23%3A59%3A59Z" {
		t.Fatalf("request: %s?%s", got.path, got.query)
	}
}

func TestUploadIsMultipart(t *testing.T) {
	var got capture
	c := server(t, 200, `{"upload_id":"up_1","record_count":2,"source":"paystack"}`, &got)
	up, err := c.UploadReconciliation(context.Background(), "paystack", "june.csv", strings.NewReader("id,amount\n1,10\n"))
	if err != nil || up.UploadID != "up_1" {
		t.Fatalf("%+v %v", up, err)
	}
	if !strings.HasPrefix(got.ctype, "multipart/form-data; boundary=") ||
		!strings.Contains(string(got.body), `name="source"`) || !strings.Contains(string(got.body), `filename="june.csv"`) {
		t.Fatalf("upload body: %s %s", got.ctype, got.body)
	}
}

func TestVerifySignatureRejectsEmptySecret(t *testing.T) {
	body := []byte(`{"event":"transaction.applied","data":{}}`)
	forged := blnk.Sign("", "1700000000", body) // anyone can compute this
	err := blnk.VerifySignature("", forged, "1700000000", body, time.Unix(1700000000, 0), 0)
	if !errors.Is(err, blnk.ErrBadSignature) {
		t.Fatalf("an empty secret must not verify anything, got %v", err)
	}
	upper := strings.ToUpper(blnk.Sign("key", "1700000000", body))
	if err := blnk.VerifySignature("key", upper, "1700000000", body, time.Unix(1700000000, 0), 0); err != nil {
		t.Fatalf("hex case should not matter: %v", err)
	}
}

func TestCommitIsNotRetriedWhenItMayHaveApplied(t *testing.T) {
	for _, tc := range []struct {
		status   int
		attempts int
	}{
		{http.StatusGatewayTimeout, 1}, // may have committed: retrying could capture twice
		{http.StatusBadGateway, 1},
		{http.StatusLocked, 3}, // refused by Blnk: safe to retry
	} {
		n := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n++
			w.WriteHeader(tc.status)
		}))
		c := blnk.New(srv.URL, blnk.WithRetries(2, time.Millisecond))
		_, err := c.UpdateInflight(context.Background(), "txn_1", blnk.UpdateInflightRequest{Status: blnk.ActionCommit, PreciseAmount: blnk.NewBigInt(500)})
		srv.Close()
		if err == nil || n != tc.attempts {
			t.Errorf("status %d: %d attempts (want %d), err %v", tc.status, n, tc.attempts, err)
		}
	}
	// Reads and reference-keyed writes are still retried on a gateway timeout.
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"transaction_id":"txn_1"}`))
	}))
	defer srv.Close()
	c := blnk.New(srv.URL, blnk.WithRetries(2, time.Millisecond))
	if _, err := c.CreateTransaction(context.Background(), blnk.CreateTransactionRequest{Reference: "r"}); err != nil || n != 2 {
		t.Fatalf("create: %d attempts, %v", n, err)
	}
}
