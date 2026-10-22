// Package core is what every service shares and no caller sees: the
// Blnk client, the currency check, conversion to and from Blnk's units, and
// error mapping. It is the one place amounts cross into Blnk.
package core

import "github.com/otyang/humanblnk/blnk"

// Core is one Blnk instance and what the services know about it.
type Core struct {
	C             *blnk.Client
	AccountLedger string // the customer ledger Accounts.OpenForCustomer uses by default

	// CustomerLedgers are the other ledgers that hold only customer
	// accounts, beside AccountLedger.
	CustomerLedgers map[string]bool

	// accounts caches each account's currency and whether it is a system
	// account; neither ever changes.
	accounts *cache
}

// GeneralLedger is Blnk's General Ledger, where system accounts live.
const GeneralLedger = "general_ledger_id"

// New returns a Core on c that remembers up to DefaultCacheSize accounts.
func New(c *blnk.Client, accountLedger string) *Core {
	return NewWithCache(c, accountLedger, DefaultCacheSize)
}

// NewWithCache is New remembering up to cacheSize accounts (DefaultCacheSize
// if cacheSize <= 0).
func NewWithCache(c *blnk.Client, accountLedger string, cacheSize int) *Core {
	return &Core{C: c, AccountLedger: accountLedger, accounts: newCache(cacheSize)}
}

// IsCustomerLedger reports whether ledger holds only customer accounts.
func (k *Core) IsCustomerLedger(ledger string) bool {
	return ledger != "" && (ledger == k.AccountLedger || k.CustomerLedgers[ledger])
}
