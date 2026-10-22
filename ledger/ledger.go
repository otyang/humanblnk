package ledger

import (
	"github.com/otyang/humanblnk/clients/blnk"
	"github.com/otyang/humanblnk/ledger/accounts"
	"github.com/otyang/humanblnk/ledger/books"
	"github.com/otyang/humanblnk/ledger/holds"
	"github.com/otyang/humanblnk/ledger/identities"
	"github.com/otyang/humanblnk/ledger/internal/core"
	"github.com/otyang/humanblnk/ledger/reconcile"
	"github.com/otyang/humanblnk/ledger/transactions"
	"github.com/otyang/humanblnk/ledger/watches"
)

// Ledger keeps HumanBlnk's books in one Blnk instance. Its operations are grouped
// by what they act on: books.Accounts.OpenForCustomer,
// books.Transactions.Record, books.Holds.Capture.
type Ledger struct {
	Identities     *identities.Service
	Accounts       *accounts.Service
	Transactions   *transactions.Service
	Holds          *holds.Service
	Watches        *watches.Service
	Reconciliation *reconcile.Service
	Books          *books.Service
}

// Option configures a Ledger.
type Option func(*config)

type config struct {
	accountLedger   string
	customerLedgers map[string]bool
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

// New returns a Ledger on c.
func New(c *blnk.Client, opts ...Option) *Ledger {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	k := core.New(c, cfg.accountLedger)
	k.CustomerLedgers = cfg.customerLedgers
	return &Ledger{
		Identities:     identities.New(k),
		Accounts:       accounts.New(k),
		Transactions:   transactions.New(k),
		Holds:          holds.New(k),
		Watches:        watches.New(k),
		Reconciliation: reconcile.New(k),
		Books:          books.New(k),
	}
}
