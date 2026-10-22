package humanblnk

import (
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/internal/accounts"
	"github.com/otyang/humanblnk/internal/core"
	"github.com/otyang/humanblnk/internal/holds"
	"github.com/otyang/humanblnk/internal/identities"
	"github.com/otyang/humanblnk/internal/ledger"
	"github.com/otyang/humanblnk/internal/reconcile"
	"github.com/otyang/humanblnk/internal/transactions"
	"github.com/otyang/humanblnk/internal/watches"
)

// Client keeps your books in one Blnk instance. Its operations are grouped
// by what they act on: hb.Accounts.OpenForCustomer,
// hb.Transactions.Record, hb.Holds.Capture.
type Client struct {
	Identities     *identities.Service
	Accounts       *accounts.Service
	Transactions   *transactions.Service
	Holds          *holds.Service
	Watches        *watches.Service
	Reconciliation *reconcile.Service
	Ledgers        *ledger.Service
}

// Option configures a Client.
type Option func(*config)

type config struct {
	accountLedger   string
	customerLedgers map[string]bool
	cacheSize       int
}

// WithAccountLedger sets the customer ledger Accounts.OpenForCustomer opens
// accounts in when CustomerAccount.Ledger is empty, such as the "Customer
// Wallets" ledger.
func WithAccountLedger(id string) Option {
	return func(c *config) { c.accountLedger = id }
}

// WithCustomerLedgers names further ledgers that hold only customer accounts,
// such as "Savings". Accounts in them are opened with OpenForCustomer, never
// CreateInternal.
func WithCustomerLedgers(ids ...string) Option {
	return func(c *config) {
		if c.customerLedgers == nil {
			c.customerLedgers = map[string]bool{}
		}
		for _, id := range ids {
			c.customerLedgers[id] = true
		}
	}
}

// WithAccountCacheSize sets how many accounts the Client remembers the
// currency of, so it needn't ask Blnk before every write: 100,000 unless
// set, about 20 MiB. The least recently used are forgotten first, so a
// long-running service's memory stays bounded however many accounts it
// touches.
func WithAccountCacheSize(n int) Option {
	return func(c *config) { c.cacheSize = n }
}

// New returns a Client on c.
func New(c *blnk.Client, opts ...Option) *Client {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	k := core.NewWithCache(c, cfg.accountLedger, cfg.cacheSize)
	k.CustomerLedgers = cfg.customerLedgers
	return &Client{
		Identities:     identities.New(k),
		Accounts:       accounts.New(k),
		Transactions:   transactions.New(k),
		Holds:          holds.New(k),
		Watches:        watches.New(k),
		Reconciliation: reconcile.New(k),
		Ledgers:        ledger.New(k),
	}
}
