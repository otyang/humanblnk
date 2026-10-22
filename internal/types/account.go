package types

import (
	"time"

	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

// Account is a Blnk balance: money in one currency, owned by one identity.
type Account struct {
	ID           string
	Ledger       string // the Blnk ledger it lives in
	Owner        string // the Blnk identity it belongs to
	Currency     currency.Currency
	Total        money.Money // everything recorded
	Available    money.Money // Total less outgoing holds and queued debits: what can be spent now
	OutgoingHeld money.Money // reserved by holds out of it, not yet captured or cancelled
	IncomingHeld money.Money // reserved by holds into it; not in Total or Available until captured
	Meta         Meta
	CreatedAt    time.Time
}

// NewAccount is what Accounts.CreateInternal needs: one of your company's own
// accounts, outside the customer ledgers.
type NewAccount struct {
	Ledger   string // Blnk ledger; required, and not a customer ledger
	Owner    string // Blnk identity id; optional
	Currency currency.Currency
	Meta     Meta
}

// CustomerAccount is what Accounts.OpenForCustomer needs. A customer has at
// most one account per ledger and currency.
type CustomerAccount struct {
	Owner    string // Blnk identity id; required
	Currency currency.Currency
	Ledger   string // a customer ledger; empty for the one set with WithAccountLedger
}

// AccountQuery selects accounts for Accounts.List; empty fields match all.
type AccountQuery struct {
	Ledger   string
	Owner    string
	Currency currency.Currency
	Limit    int // 1-100; 0 for 20
	Offset   int
}
