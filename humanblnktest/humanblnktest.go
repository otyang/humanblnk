// Package humanblnktest runs humanblnk against an in-memory Blnk, so code
// built on humanblnk can be tested with no Blnk, Docker or network:
//
//	func TestPayout(t *testing.T) {
//		hb := humanblnktest.New(t)
//		ada := hb.Customer(t, "USD")
//		hb.Fund(t, ada, "100.00")
//
//		svc := payouts.New(hb.Holds) // the code under test
//		...
//		if got := hb.Available(t, ada); got != "70.00" { ... }
//	}
//
// Books embeds the *humanblnk.Client: pass hb.Holds and the like to the code
// under test, or hb.Client where a whole Client is wanted.
//
// The in-memory Blnk copies the Blnk Core 0.15.6 behaviour humanblnk relies
// on: system accounts made on first use, splits held a posting at a time
// and released at once when a later posting fails, holds whose captures
// and releases are transactions of their own, reused references refused. humanblnk's contract tests run against it and a real Blnk alike.
// It isn't Blnk: nothing is queued, and reconciliation matches one_to_one
// only (see the tutorial's chapter 13 for the rest). Each New is a fresh,
// empty Blnk of its own, so tests can run in parallel.
package humanblnktest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/memblnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// Books is a humanblnk.Client on an in-memory Blnk, with a "Customer
// Wallets" ledger set up as its account ledger, and helpers for tests.
type Books struct {
	*humanblnk.Client

	// URL is the in-memory Blnk's address, for a blnk.Client of your own.
	URL string
	// Wallets is the id of the "Customer Wallets" ledger.
	Wallets string
	// CustomerLedgers are the ids of the ledgers WithCustomerLedger made,
	// by name.
	CustomerLedgers map[string]string

	funded atomic.Int64
}

// Option configures New.
type Option func(*config)

type config struct {
	server          []memblnk.Option
	client          []humanblnk.Option
	customerLedgers []string
}

// WithRollbackDelay sets how long after a split or batch Blnk applied
// fails partway its applied part is refunded (200ms unless set, about what
// Blnk takes). humanblnk holds splits and batches, which a failure releases
// at once, so it only matters to code calling Blnk directly.
func WithRollbackDelay(d time.Duration) Option {
	return func(c *config) { c.server = append(c.server, memblnk.WithRollbackDelay(d)) }
}

// WithWebhook sends Blnk's webhooks (balance.monitor, transaction.applied,
// transaction.inflight, transaction.void) to url, signed with secret, so a
// handler using humanblnk.VerifyWebhook can be tested end to end.
func WithWebhook(url, secret string) Option {
	return func(c *config) { c.server = append(c.server, memblnk.WithWebhook(url, secret)) }
}

// WithCustomerLedger also makes a customer ledger named name, such as
// "Savings", and gives it to humanblnk.WithCustomerLedgers. Its id is
// Books.CustomerLedgers[name].
func WithCustomerLedger(name string) Option {
	return func(c *config) { c.customerLedgers = append(c.customerLedgers, name) }
}

// WithClientOptions passes options to humanblnk.New, such as
// humanblnk.WithAccountCacheSize.
func WithClientOptions(opts ...humanblnk.Option) Option {
	return func(c *config) { c.client = append(c.client, opts...) }
}

// New starts an in-memory Blnk for the test, stopped when it ends, and
// returns Books on it.
func New(t testing.TB, opts ...Option) *Books {
	t.Helper()
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	s := memblnk.New(cfg.server...)
	t.Cleanup(s.Close)
	c := blnk.New(s.URL)
	setup := humanblnk.New(c)
	ledger := func(name string) string {
		l, err := setup.Ledgers.Create(context.Background(), name, nil)
		if err != nil {
			t.Fatalf("humanblnktest: %v", err)
		}
		return l.ID
	}
	b := &Books{URL: s.URL, Wallets: ledger("Customer Wallets"), CustomerLedgers: map[string]string{}}
	clientOpts := []humanblnk.Option{humanblnk.WithAccountLedger(b.Wallets)}
	if len(cfg.customerLedgers) > 0 {
		var ids []string
		for _, name := range cfg.customerLedgers {
			b.CustomerLedgers[name] = ledger(name)
			ids = append(ids, b.CustomerLedgers[name])
		}
		clientOpts = append(clientOpts, humanblnk.WithCustomerLedgers(ids...))
	}
	b.Client = humanblnk.New(c, append(clientOpts, cfg.client...)...)
	return b
}

// Customer makes an owner and their wallet in currency (a code such as
// "USD"), and returns the wallet's account id.
func (b *Books) Customer(t testing.TB, code string) humanblnk.AccountRef {
	t.Helper()
	ctx := context.Background()
	c := mustCurrency(t, code)
	owner, err := b.Identities.Create(ctx, nil)
	if err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
	a, err := b.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: c})
	if err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
	return humanblnk.AccountRef(a.ID)
}

// Fund moves amount (such as "100.00", in the account's currency) into
// account from @World, as a deposit would.
func (b *Books) Fund(t testing.TB, account humanblnk.AccountRef, amount string) {
	t.Helper()
	ctx := context.Background()
	a, err := b.Accounts.Get(ctx, string(account))
	if err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
	m, err := money.Parse(amount, a.Currency)
	if err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
	ref := humanblnk.Reference("humanblnktest-fund", string(account), fmt.Sprint(b.funded.Add(1)))
	if _, err := b.Transactions.Record(ctx, humanblnk.Transaction{
		Ref: ref, From: humanblnk.Single(humanblnk.World, m), To: humanblnk.Single(account, m), Description: "test funding",
	}); err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
}

// Available is what account can spend now, as a decimal such as "70.00".
func (b *Books) Available(t testing.TB, account humanblnk.AccountRef) string {
	t.Helper()
	a, err := b.Accounts.Get(context.Background(), string(account))
	if err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
	return a.Available.String()
}

// Money parses amount in currency code, failing the test if it can't.
func Money(t testing.TB, amount, code string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, mustCurrency(t, code))
	if err != nil {
		t.Fatalf("humanblnktest: %v", err)
	}
	return m
}

func mustCurrency(t testing.TB, code string) currency.Currency {
	t.Helper()
	c, ok := currency.Lookup(code)
	if !ok {
		t.Fatalf("humanblnktest: unknown currency %q", code)
	}
	return c
}
