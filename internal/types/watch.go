package types

import (
	"time"

	"github.com/otyang/humanblnk/money"
)

// WatchID is a watch, to Get, Change or Delete.
type WatchID string

// Watch is a threshold on one account. While it holds, Blnk sends the
// webhook event "balance.monitor" after every update of the account.
type Watch struct {
	ID          WatchID
	Account     AccountRef
	Field       WatchField
	Op          WatchOp
	Threshold   money.Money // in the account's currency
	Description string
	CreatedAt   time.Time
}

// WatchField is the part of an account a watch compares.
type WatchField string

// Watch fields.
const (
	WatchBalance      WatchField = "balance"                 // Total
	WatchCredits      WatchField = "credit_balance"          // everything ever credited
	WatchDebits       WatchField = "debit_balance"           // everything ever debited
	WatchOutgoingHeld WatchField = "inflight_debit_balance"  // reserved by holds out of it
	WatchIncomingHeld WatchField = "inflight_credit_balance" // reserved by holds into it
	WatchHeldNet      WatchField = "inflight_balance"        // incoming less outgoing holds
)

// WatchOp is how a watch compares.
type WatchOp string

// Watch operators. Blnk understands only these; the "=" in its docs never
// matches.
const (
	WatchAbove     WatchOp = ">"
	WatchAtLeast   WatchOp = ">="
	WatchBelow     WatchOp = "<"
	WatchAtMost    WatchOp = "<="
	WatchEquals    WatchOp = "=="
	WatchNotEquals WatchOp = "!="
)
