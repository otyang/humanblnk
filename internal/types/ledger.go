package types

import "time"

// Ledger is a Blnk ledger: a named group of accounts, such as "Customer
// Wallets". Blnk's own General Ledger holds the system accounts.
type Ledger struct {
	ID        string
	Name      string
	Meta      Meta
	CreatedAt time.Time
}
