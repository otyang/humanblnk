package types

import (
	"encoding/json"
	"time"

	"github.com/otyang/humanblnk/money"
)

// Transaction is money moving From some accounts To others, as one atomic
// write. Either side may have several postings, but not both: Blnk moves
// one account to many, or many to one.
type Transaction struct {
	Ref         string // required; the same on every retry (see Reference)
	From, To    []Posting
	Description string // required by Blnk
	Meta        Meta
}

// Posting is one account's part in a transaction. Amounts are positive:
// From and To say the direction.
type Posting struct {
	Account AccountRef
	Amount  money.Money
}

// Meta is free-form data stored with an account or entry.
type Meta map[string]any

// Single is one posting: From: humanblnk.Single(wallet, amount).
func Single(a AccountRef, m money.Money) []Posting {
	return []Posting{{Account: a, Amount: m}}
}

// Many is several postings, for one side of a split.
func Many(ps ...Posting) []Posting { return ps }

// Entry statuses.
const (
	StatusApplied  = "APPLIED"  // money moved
	StatusHeld     = "INFLIGHT" // a hold; it stays so after capture or cancel (see package holds)
	StatusVoid     = "VOID"     // the release of a cancelled or expired hold
	StatusRejected = "REJECTED" // refused, or rolled back with the rest of its split or batch
	StatusQueued   = "QUEUED"
)

// Entry is what the ledger recorded.
type Entry struct {
	ID          string
	Ref         string
	Status      string
	Amount      money.Money
	From, To    []Posting
	Description string
	Meta        Meta
	CreatedAt   time.Time
}

// EntryQuery selects entries for Transactions.List; empty fields match all.
// Blnk can't combine "from or to" with other conditions, so an account's
// full history is two queries: one with From set, one with To.
type EntryQuery struct {
	From, To     AccountRef
	Status       string
	Since, Until time.Time
	Limit        int // 1-100; 0 for 20
	Offset       int
}

// Reversal undoes all or part of an entry.
type Reversal struct {
	Ref    string      // required for a partial reversal; the same on every retry
	Entry  string      // the entry to undo
	Amount money.Money // the zero Money undoes all of it (or all that's left)
	Reason string      // required: why, for the audit trail
}

// Preview is what recording a transaction would do. Nothing was written.
type Preview struct {
	WouldApply bool
	Rejection  error // why not, when WouldApply is false: ErrInsufficientFunds, …
	Changes    []AccountChange
	Notes      []string // Blnk's caveats, such as a balance that doesn't exist yet
}

// AccountChange is one account before and after.
type AccountChange struct {
	Account                         AccountRef
	Before, After                   money.Money
	AvailableBefore, AvailableAfter money.Money
}

// HoldID is a hold, to Capture or Cancel.
type HoldID string

// Event is a webhook from Blnk, such as "transaction.applied" or
// "balance.monitor".
type Event struct {
	Kind    string
	Entry   *Entry          // for "transaction.*" events
	Watch   WatchID         // for "balance.monitor": the watch that fired
	Account AccountRef      // for "balance.monitor": the account it watches
	Data    json.RawMessage // the event's data as Blnk sent it
}
