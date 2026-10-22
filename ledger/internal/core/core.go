// Package core is what every ledger service shares and no caller sees: the
// Blnk client, the currency check, conversion to and from Blnk's units, and
// error mapping. It is the one place amounts cross into Blnk.
package core

import (
	"sync"

	"github.com/otyang/humanblnk/clients/blnk"
)

// Core is one Blnk instance and what the services know about it.
type Core struct {
	C             *blnk.Client
	AccountLedger string // the customer ledger Accounts.OpenForCustomer uses by default

	// CustomerLedgers are the other ledgers that hold only customer
	// accounts, beside AccountLedger.
	CustomerLedgers map[string]bool

	// currencies caches each account's currency, and system whether it is a
	// system account; neither ever changes.
	currencies sync.Map // account id -> currency.Currency
	system     sync.Map // account id -> bool
}

// GeneralLedger is Blnk's General Ledger, where system accounts live.
const GeneralLedger = "general_ledger_id"

// New returns a Core on c.
func New(c *blnk.Client, accountLedger string) *Core {
	return &Core{C: c, AccountLedger: accountLedger}
}

// IsCustomerLedger reports whether ledger holds only customer accounts.
func (k *Core) IsCustomerLedger(ledger string) bool {
	return ledger != "" && (ledger == k.AccountLedger || k.CustomerLedgers[ledger])
}
