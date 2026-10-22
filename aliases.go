package humanblnk

import (
	"net/http"
	"time"

	"github.com/otyang/humanblnk/internal/accounts"
	"github.com/otyang/humanblnk/internal/events"
	"github.com/otyang/humanblnk/internal/holds"
	"github.com/otyang/humanblnk/internal/identities"
	"github.com/otyang/humanblnk/internal/ledger"
	"github.com/otyang/humanblnk/internal/reconcile"
	"github.com/otyang/humanblnk/internal/transactions"
	"github.com/otyang/humanblnk/internal/types"
	"github.com/otyang/humanblnk/internal/watches"
	"github.com/otyang/humanblnk/money"
)

// HumanBlnk's vocabulary, defined in package types and re-exported here so
// callers only import humanblnk. See types for each one's documentation.

type (
	AccountRef      = types.AccountRef
	Transaction     = types.Transaction
	Posting         = types.Posting
	Meta            = types.Meta
	Entry           = types.Entry
	EntryQuery      = types.EntryQuery
	Reversal        = types.Reversal
	Preview         = types.Preview
	AccountChange   = types.AccountChange
	HoldID          = types.HoldID
	Account         = types.Account
	NewAccount      = types.NewAccount
	CustomerAccount = types.CustomerAccount
	AccountQuery    = types.AccountQuery
	Event           = types.Event
	Ledger          = types.Ledger
	Identity        = types.Identity
	WatchID         = types.WatchID
	Watch           = types.Watch
	WatchField      = types.WatchField
	WatchOp         = types.WatchOp
	StatementID     = types.StatementID
	RuleID          = types.RuleID
	RunID           = types.RunID
	StatementLine   = types.StatementLine
	MatchRule       = types.MatchRule
	Criterion       = types.Criterion
	MatchField      = types.MatchField
	MatchOp         = types.MatchOp
	Strategy        = types.Strategy
	Run             = types.Run
	RunResult       = types.RunResult
)

// The services a Client holds, named so callers can keep or pass just one:
// a handler that only places holds can take a *HoldService.
type (
	IdentityService       = identities.Service
	AccountService        = accounts.Service
	TransactionService    = transactions.Service
	HoldService           = holds.Service
	WatchService          = watches.Service
	ReconciliationService = reconcile.Service
	LedgerService         = ledger.Service
)

// Watch fields and operators.
const (
	WatchBalance      = types.WatchBalance
	WatchCredits      = types.WatchCredits
	WatchDebits       = types.WatchDebits
	WatchHeld         = types.WatchHeld
	WatchIncomingHeld = types.WatchIncomingHeld
	WatchHeldNet      = types.WatchHeldNet

	WatchAbove     = types.WatchAbove
	WatchAtLeast   = types.WatchAtLeast
	WatchBelow     = types.WatchBelow
	WatchAtMost    = types.WatchAtMost
	WatchEquals    = types.WatchEquals
	WatchNotEquals = types.WatchNotEquals
)

// Reconciliation fields, operators and strategies.
const (
	MatchAmount      = types.MatchAmount
	MatchDate        = types.MatchDate
	MatchReference   = types.MatchReference
	MatchDescription = types.MatchDescription
	MatchCurrency    = types.MatchCurrency

	MatchEquals      = types.MatchEquals
	MatchGreaterThan = types.MatchGreaterThan
	MatchLessThan    = types.MatchLessThan
	MatchContains    = types.MatchContains

	OneToOne  = types.OneToOne
	OneToMany = types.OneToMany
	ManyToOne = types.ManyToOne
)

// Entry statuses.
const (
	StatusApplied  = types.StatusApplied
	StatusHeld     = types.StatusHeld
	StatusVoid     = types.StatusVoid
	StatusRejected = types.StatusRejected
	StatusQueued   = types.StatusQueued
)

// Errors. Currency problems are money.ErrCurrencyMismatch and
// money.ErrUnknownCurrency.
var (
	ErrInsufficientFunds = types.ErrInsufficientFunds
	ErrDuplicate         = types.ErrDuplicate
	ErrReferenceUsed     = types.ErrReferenceUsed
	ErrNotFound          = types.ErrNotFound
	ErrUnbalanced        = types.ErrUnbalanced
	ErrAlreadyReversed   = types.ErrAlreadyReversed
	ErrHoldSettled       = types.ErrHoldSettled
	ErrInvalid           = types.ErrInvalid
	ErrReadBack          = types.ErrReadBack
)

// System accounts.
var (
	World       = types.World
	FeeIncome   = types.FeeIncome
	Adjustments = types.Adjustments
)

// SystemAccount is one of HumanBlnk's own accounts, by name.
func SystemAccount(name string) AccountRef { return types.SystemAccount(name) }

// ClearingAccount is money a payment provider owes HumanBlnk, or HumanBlnk owes it.
func ClearingAccount(provider string) AccountRef { return types.ClearingAccount(provider) }

// Reference builds a transaction reference: Reference("payout", "po_123").
func Reference(kind string, parts ...string) string { return types.Reference(kind, parts...) }

// Single is one posting: From: humanblnk.Single(wallet, amount).
func Single(a AccountRef, m money.Money) []Posting { return types.Single(a, m) }

// Many is several postings, for one side of a split.
func Many(ps ...Posting) []Posting { return types.Many(ps...) }

// VerifyWebhook checks a Blnk webhook's signature and age, then decodes it.
func VerifyWebhook(secret string, r *http.Request, tolerance time.Duration) (Event, error) {
	return events.VerifyWebhook(secret, r, tolerance)
}
