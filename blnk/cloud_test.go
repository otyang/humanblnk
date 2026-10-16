package blnk_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/otyang/humanblnk/blnk"
)

func TestCloudInstanceIDOnEveryRequest(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("X-Blnk-Key") != "cloud-key" {
			t.Errorf("missing key on %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"status":"UP","ledger_id":"ldg_1"}`))
	}))
	defer srv.Close()
	c := blnk.New(srv.URL+"/proxy", blnk.WithAPIKey("cloud-key"), blnk.WithInstanceID("instance_abc"), blnk.WithRetries(0, 0))
	ctx := context.Background()
	if _, err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListLedgers(ctx, 5, 10); err != nil {
		t.Fatal(err)
	}
	if queries[0] != "/proxy/health?instance_id=instance_abc" {
		t.Fatalf("health: %s", queries[0])
	}
	// An existing query string keeps its parameters.
	if !strings.HasPrefix(queries[1], "/proxy/ledgers?") || !strings.Contains(queries[1], "instance_id=instance_abc") ||
		!strings.Contains(queries[1], "limit=5") || !strings.Contains(queries[1], "offset=10") {
		t.Fatalf("list: %s", queries[1])
	}
}

func TestCloudConfigurationMistakes(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]*blnk.Client{
		"proxy without instance": blnk.New(blnk.CloudProxyURL, blnk.WithAPIKey("k")),
		"deployment id":          blnk.New(blnk.CloudProxyURL, blnk.WithInstanceID("deployment_1")),
		"no scheme":              blnk.New("core.example.com"),
		"empty":                  blnk.New(""),
	} {
		if _, err := c.Health(ctx); err == nil {
			t.Errorf("%s: want a configuration error", name)
		}
	}
}

func TestRedirectToAnotherHostIsRefused(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Blnk-Key") != ""
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/health", http.StatusFound)
	}))
	defer srv.Close()
	c := blnk.New(srv.URL, blnk.WithAPIKey("secret"), blnk.WithRetries(0, 0))
	if _, err := c.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("want refused redirect, got %v", err)
	}
	if leaked {
		t.Fatal("API key reached another host")
	}
	// A caller's own http.Client isn't modified.
	mine := &http.Client{}
	_ = blnk.New(srv.URL, blnk.WithHTTPClient(mine))
	if mine.CheckRedirect != nil {
		t.Fatal("caller's client was changed")
	}
}

func TestHealthReportsDown(t *testing.T) {
	var got capture
	c := server(t, http.StatusServiceUnavailable, `{"status":"DOWN","reason":"database unreachable"}`, &got)
	h, err := c.Health(context.Background())
	if err == nil || h == nil || h.Status != "DOWN" || !strings.Contains(err.Error(), "database unreachable") {
		t.Fatalf("health: %+v %v", h, err)
	}
}
