package blnk_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/otyang/humanblnk/clients/blnk"
)

// TestEndpoints checks the method, path, query and body each call sends,
// against the routes of Blnk Core 0.15.6 (api/api.go).
func TestEndpoints(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, method, path, query, body string
		call                            func(c *blnk.Client) error
	}{
		{"ledger update", "PUT", "/ledgers/ldg_1", "", `{"name":"Ops"}`, func(c *blnk.Client) error {
			_, err := c.Ledgers.Update(ctx, "ldg_1", "Ops")
			return err
		}},
		{"ledger list default", "GET", "/ledgers", "", "", func(c *blnk.Client) error {
			_, err := c.Ledgers.List(ctx, blnk.ListOptions{})
			return err
		}},
		{"balance get queued", "GET", "/balances/bln_1", "include=identity&with_queued=true", "", func(c *blnk.Client) error {
			_, err := c.Balances.Get(ctx, "bln_1", blnk.GetBalanceOptions{WithQueued: true, Include: []string{"identity"}})
			return err
		}},
		{"balance list filtered", "GET", "/balances", "currency_eq=USD&limit=20", "", func(c *blnk.Client) error {
			_, err := c.Balances.List(ctx, blnk.ListOptions{Limit: 20, Filters: url.Values{"currency_eq": {"USD"}}})
			return err
		}},
		{"balance identity", "PUT", "/balances/bln_1/identity", "", `{"identity_id":"idt_1"}`, func(c *blnk.Client) error {
			return c.Balances.UpdateIdentity(ctx, "bln_1", "idt_1")
		}},
		{"balance lineage", "GET", "/balances/bln_1/lineage", "", "", func(c *blnk.Client) error {
			_, err := c.Balances.Lineage(ctx, "bln_1")
			return err
		}},
		{"snapshots", "POST", "/balances-snapshots", "batch_size=500", "", func(c *blnk.Client) error {
			return c.Balances.TakeSnapshots(ctx, 500)
		}},
		{"monitor get", "GET", "/balance-monitors/mon_1", "", "", func(c *blnk.Client) error {
			_, err := c.Monitors.Get(ctx, "mon_1")
			return err
		}},
		{"monitor by balance", "GET", "/balance-monitors/balances/bln_1", "", "", func(c *blnk.Client) error {
			_, err := c.Monitors.ListByBalance(ctx, "bln_1")
			return err
		}},
		{"monitor update", "PUT", "/balance-monitors/mon_1", "", "", func(c *blnk.Client) error {
			return c.Monitors.Update(ctx, "mon_1", blnk.BalanceMonitor{BalanceID: "bln_1"})
		}},
		{"txn list", "GET", "/transactions", "limit=5&offset=10", "", func(c *blnk.Client) error {
			_, err := c.Transactions.List(ctx, blnk.ListOptions{Limit: 5, Offset: 10})
			return err
		}},
		{"txn lineage", "GET", "/transactions/txn_1/lineage", "", "", func(c *blnk.Client) error {
			_, err := c.Transactions.Lineage(ctx, "txn_1")
			return err
		}},
		{"txn recover", "POST", "/transactions/recover", "threshold=5m0s", "", func(c *blnk.Client) error {
			_, err := c.Transactions.RecoverQueued(ctx, 5*time.Minute)
			return err
		}},
		{"refund preview", "POST", "/refund-transaction/txn_1", "", `{"dry_run":true}`, func(c *blnk.Client) error {
			_, err := c.Transactions.PreviewRefund(ctx, "txn_1")
			return err
		}},
		{"inflight preview", "PUT", "/transactions/inflight/txn_1", "", `{"status":"void","dry_run":true}`, func(c *blnk.Client) error {
			_, err := c.Inflight.Preview(ctx, "txn_1", blnk.UpdateInflightRequest{Status: blnk.ActionVoid})
			return err
		}},
		{"bulk void preview", "POST", "/transactions/inflight/bulk/void", "", `{"transaction_ids":["a"],"dry_run":true}`, func(c *blnk.Client) error {
			_, err := c.Inflight.PreviewBulkVoid(ctx, blnk.BulkVoidInflightRequest{TransactionIDs: []string{"a"}})
			return err
		}},
		{"identity update", "PUT", "/identities/idt_1", "", `{"first_name":"Ada"}`, func(c *blnk.Client) error {
			return c.Identities.Update(ctx, "idt_1", blnk.Identity{FirstName: "Ada"})
		}},
		{"identity delete", "DELETE", "/identities/idt_1", "", "", func(c *blnk.Client) error {
			return c.Identities.Delete(ctx, "idt_1")
		}},
		{"identity filter", "POST", "/identities/filter", "", `{"filters":null}`, func(c *blnk.Client) error {
			_, _, err := c.Identities.Filter(ctx, blnk.FilterRequest{})
			return err
		}},
		{"tokenize", "POST", "/identities/idt_1/tokenize", "", `{"fields":["first_name","email_address"]}`, func(c *blnk.Client) error {
			return c.Identities.Tokenize(ctx, "idt_1", "first_name", "email_address")
		}},
		{"tokenize field", "POST", "/identities/idt_1/tokenize/first_name", "", "", func(c *blnk.Client) error {
			return c.Identities.TokenizeField(ctx, "idt_1", "first_name")
		}},
		{"detokenize all", "POST", "/identities/idt_1/detokenize", "", `{"fields":[]}`, func(c *blnk.Client) error {
			_, err := c.Identities.Detokenize(ctx, "idt_1")
			return err
		}},
		{"detokenize field", "GET", "/identities/idt_1/detokenize/first_name", "", "", func(c *blnk.Client) error {
			_, err := c.Identities.DetokenizeField(ctx, "idt_1", "first_name")
			return err
		}},
		{"tokenized fields", "GET", "/identities/idt_1/tokenized-fields", "", "", func(c *blnk.Client) error {
			_, err := c.Identities.TokenizedFields(ctx, "idt_1")
			return err
		}},
		{"rule update", "PUT", "/reconciliation/matching-rules/rul_1", "", "", func(c *blnk.Client) error {
			_, err := c.Reconciliation.UpdateMatchingRule(ctx, "rul_1", blnk.MatchingRule{Name: "r"})
			return err
		}},
		{"rule delete", "DELETE", "/reconciliation/matching-rules/rul_1", "", "", func(c *blnk.Client) error {
			return c.Reconciliation.DeleteMatchingRule(ctx, "rul_1")
		}},
		{"account create", "POST", "/accounts", "", `{"balance_id":"bln_1"}`, func(c *blnk.Client) error {
			_, err := c.Accounts.Create(ctx, blnk.CreateAccountRequest{BalanceID: "bln_1"})
			return err
		}},
		{"account get", "GET", "/accounts/acc_1", "include=balance&include=identity", "", func(c *blnk.Client) error {
			_, err := c.Accounts.Get(ctx, "acc_1", "balance", "identity")
			return err
		}},
		{"account filter", "POST", "/accounts/filter", "", `{"filters":null}`, func(c *blnk.Client) error {
			_, _, err := c.Accounts.Filter(ctx, blnk.FilterRequest{})
			return err
		}},
		{"hook register", "POST", "/hooks", "", "", func(c *blnk.Client) error {
			_, err := c.Hooks.Register(ctx, blnk.Hook{Name: "h", URL: "https://x", Type: blnk.HookPre})
			return err
		}},
		{"hook list", "GET", "/hooks", "type=POST_TRANSACTION", "", func(c *blnk.Client) error {
			_, err := c.Hooks.List(ctx, blnk.HookPost)
			return err
		}},
		{"hook delete", "DELETE", "/hooks/hk_1", "", "", func(c *blnk.Client) error {
			return c.Hooks.Delete(ctx, "hk_1")
		}},
		{"apikey list", "GET", "/api-keys", "owner=svc", "", func(c *blnk.Client) error {
			_, err := c.APIKeys.List(ctx, "svc")
			return err
		}},
		{"apikey revoke", "DELETE", "/api-keys/key_1", "", "", func(c *blnk.Client) error {
			return c.APIKeys.Revoke(ctx, "key_1", "")
		}},
		{"search", "POST", "/search/ledgers", "", `{"q":"*","query_by":"name"}`, func(c *blnk.Client) error {
			_, err := c.Search.Query(ctx, blnk.CollectionLedgers, blnk.SearchParams{Q: "*", QueryBy: "name"})
			return err
		}},
		{"multi search", "POST", "/multi-search", "", `{"searches":[{"collection":"balances","q":"*"}]}`, func(c *blnk.Client) error {
			_, err := c.Search.MultiQuery(ctx, []blnk.SearchParams{{Collection: blnk.CollectionBalances, Q: "*"}})
			return err
		}},
		{"reindex", "POST", "/search/reindex", "", `{}`, func(c *blnk.Client) error {
			_, err := c.Search.Reindex(ctx, 0)
			return err
		}},
		{"backup s3", "GET", "/backup-s3", "", "", func(c *blnk.Client) error {
			return c.Admin.BackupS3(ctx)
		}},
	} {
		var got capture
		// dry_run:true satisfies the previews' check that Core honoured it.
		c := server(t, 200, `{"dry_run":true}`, &got)
		if err := tc.call(c); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got.method != tc.method || got.path != tc.path || got.query != tc.query {
			t.Errorf("%s: sent %s %s?%s, want %s %s?%s", tc.name, got.method, got.path, got.query, tc.method, tc.path, tc.query)
		}
		if tc.body != "" && strings.TrimSpace(string(got.body)) != tc.body {
			t.Errorf("%s: body %s, want %s", tc.name, got.body, tc.body)
		}
	}
}

func TestCreateNeverSendsDryRun(t *testing.T) {
	var got capture
	c := server(t, 201, `{"transaction_id":"txn_1"}`, &got)
	if _, err := c.Transactions.Create(context.Background(), blnk.CreateTransactionRequest{Reference: "r", Atomic: true}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(got.body, &body)
	if _, ok := body["dry_run"]; ok || body["atomic"] != true {
		t.Fatalf("body: %s", got.body)
	}
}

func TestAvailableSubtractsQueuedDebits(t *testing.T) {
	var got capture
	c := server(t, 200, `{"balance":1000,"inflight_debit_balance":100,"queued_debit_balance":250}`, &got)
	b, err := c.Balances.Get(context.Background(), "bln_1", blnk.GetBalanceOptions{WithQueued: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Available().Int64() != 650 {
		t.Fatalf("available = %s, want 650", b.Available())
	}
}

func TestDetokenizeAndPreviewDecode(t *testing.T) {
	var got capture
	c := server(t, 200, `{"fields":{"first_name":"Ada"}}`, &got)
	f, err := c.Identities.Detokenize(context.Background(), "idt_1", "first_name")
	if err != nil || f["first_name"] != "Ada" {
		t.Fatalf("%v %v", f, err)
	}

	c = server(t, 200, `{"dry_run":true,"would_apply":true,"precise_amount":"1000","balances":[{"balance_id":"b","resulting_available":"-5"}]}`, &got)
	p, err := c.Transactions.PreviewRefund(context.Background(), "txn_1")
	if err != nil || p.PreciseAmount.Int64() != 1000 || p.Balances[0].ResultingAvailable.Int64() != -5 {
		t.Fatalf("%+v %v", p, err)
	}
}
