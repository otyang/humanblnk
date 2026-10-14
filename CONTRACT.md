# Ledger service contract

Generated from the Go source by `scripts/gencontract.py` (run `make docs`); the
source is authoritative. It lists the `Ledger` interface, its namespaces, and
every exported type, function, value and constant they use. For explanations
and real-world examples, see the guide at https://otyang.github.io/humanblnk/.

## Importing

```go
import "github.com/otyang/humanblnk"         // everything below, re-exported, plus humanblnk.New
import "github.com/otyang/humanblnk/ledger"  // the same types, defined here
```

`humanblnk` re-exports package `ledger` with type aliases (`humanblnk.Money`
is `ledger.Money`), renames `ledger.New` to `humanblnk.NewService`, and adds:

```go
// New connects to the Blnk Core at blnkURL and returns the ledger service.
func New(blnkURL string, cfg Config, opts ...Option) *Service
```

## Interface

```go
// Ledger is the interface your application depends on, grouped into
// namespaces: l.Accounts().Open(...), l.Payments().Transfer(...),
// l.Pending().Confirm(...). Depend on a single namespace (e.g. ledger.Pending)
// where that is all a component needs. Service implements it on Blnk;
// Instrument wraps any Ledger with an Observer.
type Ledger interface {
	Accounts() Accounts
	Customers() Customers
	Payments() Payments
	Pending() Pending
	Transactions() Transactions
	Disputes() Disputes
	Escrows() Escrows
	Operations() Operations
	Reconciliations() Reconciliations
}

// Accounts: open, read and manage accounts, and their history and statements.
type Accounts interface {
	// Open creates a customer account (a Blnk balance) in Config.LedgerID or
	// in.LedgerID, marked active. With ExternalID it is safe to retry.
	Open(ctx context.Context, in OpenAccountInput) (*Account, error)

	// Get returns an account with its balance, available balance (balance minus
	// pending debits) and pending amounts.
	Get(ctx context.Context, accountID string) (*Account, error)

	// GetSystem returns a system account such as "@FeeIncome" or "@PaystackClearing".
	GetSystem(ctx context.Context, indicator, currency string) (*Account, error)

	// Find lists accounts matching q, oldest first.
	Find(ctx context.Context, q AccountQuery) (*AccountPage, error)

	// Freeze marks the account frozen in metadata. Enforced by this layer when
	// Config.EnforceAccountStatus is true (Blnk itself does not block on metadata).
	Freeze(ctx context.Context, accountID, reason string) error

	// Unfreeze marks a frozen account active again.
	Unfreeze(ctx context.Context, accountID string) error

	// Close moves any remaining balance to SweepTo and marks the account
	// closed. It refuses while anything is pending (INFLIGHT) or the balance is
	// negative. Closed accounts are rejected by every movement when
	// Config.EnforceAccountStatus is on. Closing a frozen account is allowed.
	Close(ctx context.Context, accountID string, in CloseInput) (*Account, error)

	// History lists an account's transactions, newest first.
	History(ctx context.Context, accountID string, q HistoryQuery) (*HistoryPage, error)

	// BalanceAt returns the account's posted balance at t, e.g. a month-end
	// balance. It uses Blnk's historical balance endpoint recomputed from
	// transactions (Core 0.10.1+). Pending (INFLIGHT) amounts aren't included.
	BalanceAt(ctx context.Context, accountID string, t time.Time) (Money, error)

	// Statement builds a statement from posted (APPLIED) entries. Opening comes
	// from Blnk's historical balance; the running balance is computed here and
	// checked against the historical balance at To, so a statement that doesn't
	// add up is an error rather than a wrong document.
	Statement(ctx context.Context, accountID string, from, to time.Time) (*Statement, error)

	// LimitUsage reports usage against the account's limit (for "you can send X
	// more today" in your app).
	LimitUsage(ctx context.Context, accountID string) (*LimitUsage, error)
}

// Customers: onboard customers and their savings pots.
type Customers interface {
	// Open creates the identity and opens its accounts.
	Open(ctx context.Context, in CustomerInput) (*Customer, error)

	// Get returns an identity and its customer accounts, by currency.
	// Pots and escrows are left out; list them with Customers().ListPots / Accounts().Find.
	Get(ctx context.Context, identityID string) (*Customer, error)

	// OpenPot opens a pot in the parent's currency, linked to the parent and its
	// identity. Opening the same name twice returns the existing pot.
	OpenPot(ctx context.Context, in PotInput) (*Pot, error)

	// ListPots lists the pots under a parent account.
	ListPots(ctx context.Context, parentID string) ([]*Pot, error)
}

// Payments: move money and quote it first.
type Payments interface {
	// Deposit records money received from outside: the provider's clearing
	// account (e.g. @PaystackClearing) is debited and the account credited, less
	// the fee, which goes to the fee income account. Pending holds it INFLIGHT
	// until Pending().Confirm.
	Deposit(ctx context.Context, in DepositInput) (*Receipt, error)

	// Withdraw sends money out: the account is debited Amount + Fee, the
	// provider's clearing account credited Amount. It is held INFLIGHT until
	// Pending().Confirm (provider success) or Cancel (failure), and auto-cancels
	// after Config.WithdrawalHoldTTL; Immediate posts it at once.
	Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error)

	// Transfer moves money between accounts. The sender pays Amount + Fee; the
	// recipient gets Amount. Pending holds it INFLIGHT; ScheduledFor posts it later.
	Transfer(ctx context.Context, in TransferInput) (*Receipt, error)

	// Split records the payment as one Blnk transaction with one destination per
	// part. Every part is converted to an exact amount here and sent as
	// precise_distribution, so what each recipient gets is decided by this code,
	// not by rounding on the server: percentage parts round down, and the dust
	// goes to the Rest part (or, without one, to the first percentage part).
	Split(ctx context.Context, in SplitInput) (*Receipt, error)

	// Exchange debits From into the FX pool in the sell currency and credits To out
	// of the pool in the buy currency, as one atomic batch: both legs post or
	// neither does. Each currency's pool balance is the house's open position.
	Exchange(ctx context.Context, in ExchangeInput) (*ExchangeReceipt, error)

	// Bulk posts up to 10,000 transfers as one Blnk batch, e.g. payroll.
	// Atomic makes them all-or-nothing; Pending holds them all INFLIGHT; Async
	// processes them in the background with the outcome sent by webhook.
	Bulk(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error)

	// Schedule creates every occurrence up front as a Blnk scheduled
	// transaction (scheduled_for), e.g. a monthly savings plan or subscription.
	// Blnk has no native recurrence, so a plan is a set of scheduled transfers
	// sharing meta_data.recurring_reference. Calling it again with the same input
	// is safe: existing occurrences come back as replays.
	//
	// Monthly dates follow time.AddDate, so a plan starting on the 31st lands on
	// the 1st of shorter months; start on the 28th or earlier to avoid that.
	Schedule(ctx context.Context, in RecurringInput) ([]*Receipt, error)

	// QuoteDeposit previews Deposit (fee and net credited).
	QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error)

	// QuoteWithdraw previews Withdraw.
	QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error)

	// QuoteTransfer previews Transfer.
	QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error)
}

// Pending: place holds and finish pending (INFLIGHT) transactions: confirm a withdrawal, capture a hold (optionally partially), approve a pending deposit, or cancel them.
type Pending interface {
	// Hold reserves money on an account without moving it, like a card
	// authorisation. Pending().Confirm captures all or part of it; Cancel releases
	// it. ExpiresAt releases it automatically, CaptureAt captures it.
	Hold(ctx context.Context, in HoldInput) (*Receipt, error)

	// Confirm settles an INFLIGHT transaction. With partial set, only that amount is
	// settled and the rest stays held (call Confirm again or Cancel the remainder).
	// A transaction with several legs (one with a fee) can only be confirmed in
	// full, or one leg at a time by the leg's transaction id.
	Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error)

	// Cancel releases whatever is still held on an INFLIGHT transaction.
	Cancel(ctx context.Context, txID string) (*Receipt, error)

	// ConfirmMany confirms up to 100 independent pending transactions in one call.
	// Each succeeds or fails on its own; check every BatchResult.
	ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error)

	// CancelMany cancels up to 100 independent pending transactions in one call.
	CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error)
}

// Transactions: look up and reverse single transactions.
type Transactions interface {
	// Get returns a transaction by its Blnk id.
	Get(ctx context.Context, txID string) (*Receipt, error)

	// GetByReference tries the reference as given, then with Blnk's "_q"
	// suffix used for records produced by the queue.
	GetByReference(ctx context.Context, reference string) (*Receipt, error)

	// Refund reverses a settled (or confirmed/cancelled pending) transaction once.
	// For split transactions created through the queue, pass the queued parent id.
	// Blnk reverses the whole amount, so a transaction that already has partial
	// refunds (RefundPartial) is refused; refund the rest with RefundPartial.
	Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error)

	// RefundPartial sends part of a posted transaction back: destination → source,
	// linked to the original with meta_data.refund_of. Refunds beyond the original
	// amount (counting the partial refunds linked to it) are rejected, as is a
	// transaction already reversed with Refund; the original's
	// meta_data.refunded_precise records the total refunded. Use Refund for
	// Blnk's one-shot full reversal.
	//
	// Two partial refunds of the same transaction racing each other can both pass
	// the remaining-amount check; serialise refunds per transaction in your app
	// (e.g. one worker or a row lock) if they can happen concurrently.
	RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error)
}

// Disputes: open and resolve chargebacks.
type Disputes interface {
	// Open puts the disputed amount on hold in the recipient's account,
	// pointed back at the original payer, so it can't be spent while the dispute
	// is open. The hold has no expiry. Close it with Disputes().Resolve.
	Open(ctx context.Context, in DisputeInput) (*Receipt, error)

	// Resolve closes a dispute opened with Disputes().Open. With DisputeUpheld,
	// partial refunds only part of the held amount; the rest stays held until you
	// resolve again or reject.
	Resolve(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error)
}

// Escrows: hold money between a payer and a beneficiary.
type Escrows interface {
	// Open opens an escrow account and moves Amount into it from the payer.
	Open(ctx context.Context, in EscrowInput) (*Escrow, error)

	// Get returns an escrow and how much of it is still held.
	Get(ctx context.Context, escrowID string) (*Escrow, error)

	// Release pays escrowed money to the beneficiary.
	Release(ctx context.Context, escrowID string, in EscrowSettleInput) (*Receipt, error)

	// Refund returns escrowed money to the payer.
	Refund(ctx context.Context, escrowID string, in EscrowSettleInput) (*Receipt, error)
}

// Operations: back-office tools: settlement, suspense, adjustments, trial balance and balance alerts.
type Operations interface {
	// RecordSettlement clears a provider clearing account against your bank, so
	// the clearing balance always equals what is still outstanding with that
	// provider and can be reconciled with its settlement report.
	//
	//	Collection: Dr Bank (Amount−Fee), Dr ProviderFees (Fee) / Cr Clearing (Amount)
	//	Payout:     Dr Clearing (Amount), Dr ProviderFees (Fee) / Cr Bank (Amount+Fee)
	//
	// The entries post atomically.
	RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error)

	// ParkInSuspense records unidentified incoming money: clearing account → @Suspense.
	ParkInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error)

	// AllocateSuspense moves parked money to its owner: @Suspense → account. It
	// can't allocate more than suspense holds.
	AllocateSuspense(ctx context.Context, in AllocateInput) (*Receipt, error)

	// Adjust posts a correction between the account and @Adjustments, recording
	// reason, requester and approver in metadata so every correction is auditable.
	Adjust(ctx context.Context, in AdjustInput) (*Receipt, error)

	// TrialBalance reads every balance in currency (100 per request).
	TrialBalance(ctx context.Context, currency string) (*TrialBalance, error)

	// WatchBalance creates a Blnk balance monitor. Alerts arrive through your
	// global webhook as EventBalanceAlert (see Service.ParseWebhook).
	WatchBalance(ctx context.Context, in WatchInput) (*Watch, error)

	// Unwatch deletes a balance monitor.
	Unwatch(ctx context.Context, watchID string) error
}

// Reconciliations: match provider records against the ledger.
type Reconciliations interface {
	// CreateRule stores a rule in Blnk and returns its id.
	CreateRule(ctx context.Context, r MatchingRule) (string, error)

	// Start starts a reconciliation. Poll Reconciliations().Get until Done.
	Start(ctx context.Context, in ReconcileInput) (*Reconciliation, error)

	// Get returns a run's status and counts.
	Get(ctx context.Context, id string) (*Reconciliation, error)
}
```

## Money and display

```go
// Money is an amount in a currency, carried in both forms Blnk stores:
//
//	Value         "0.456"      the human decimal, exactly as typed (trailing zeros trimmed)
//	PreciseAmount 45600000     the integer in the smallest unit (what Blnk sums on balances)
//	Precision     100000000    the multiplier between them (Value × Precision = PreciseAmount)
//
// Build one from what the user typed with Amount("0.456", "BTC"); the Service
// fills PreciseAmount and Precision from the currency's configured precision.
// No floats are involved at any step.
type Money struct {
	Value         string
	Currency      string
	PreciseAmount *big.Int
	Precision     int64

	// Display is the amount for people to read, set on every Money the
	// Service returns: fiat padded to its decimals ("100.00"), crypto trimmed
	// and rounded down ("0.456789"). See DisplayRule. Never compute with it.
	Display string
}

// DisplayRule says how a currency's amounts are shown to people. Amounts are
// cut (never rounded up) to Decimals, so a displayed balance can always be
// spent in full; the exact amount stays in Money.Value.
//
// Fiat style pads to a fixed number of places ("100.00"); crypto style trims
// trailing zeros ("0.5", "0.456789").
type DisplayRule struct {
	Decimals  int  // most decimal places shown; capped at the currency's precision
	TrimZeros bool // drop trailing zeros instead of padding to Decimals
}

// Rounding decides what happens to a fraction of the smallest unit.
type Rounding int

const (
	RoundHalfUp Rounding = iota // 0.5 and above rounds up (default)
	RoundUp                     // any fraction rounds up (in the house's favour for fees)
	RoundDown                   // fractions are dropped (in the customer's favour for fees)
)
```

## Accounts and customers

```go
// OpenAccountInput opens a customer account.
type OpenAccountInput struct {
	Currency   string
	IdentityID string // optional: link to a Blnk identity
	LedgerID   string // optional: overrides Config.LedgerID

	// ExternalID is your own id for this account (e.g. your user id). With it,
	// Accounts().Open is safe to retry: an account already opened with the same
	// ExternalID and currency is returned instead of a second one being created.
	ExternalID string
	MetaData   map[string]any
}

// Account is a Blnk balance as the wallet sees it, amounts in both forms.
type Account struct {
	ID         string      `json:"id"`
	LedgerID   string      `json:"ledger_id"`
	IdentityID string      `json:"identity_id,omitempty"`
	Indicator  string      `json:"indicator,omitempty"`
	Kind       AccountKind `json:"kind"` // customer, or system for "@" indicator accounts such as @FeeIncome
	Currency   string      `json:"currency"`
	Precision  int64       `json:"precision"`

	Balance    Money `json:"balance"`     // settled balance
	Available  Money `json:"available"`   // balance - pending debits (what can be spent now)
	PendingIn  Money `json:"pending_in"`  // inflight credits
	PendingOut Money `json:"pending_out"` // inflight debits
	Credits    Money `json:"credits"`     // lifetime credits
	Debits     Money `json:"debits"`      // lifetime debits

	Status    string         `json:"status"` // from meta_data.status ("active" if unset)
	CreatedAt time.Time      `json:"created_at"`
	MetaData  map[string]any `json:"meta_data,omitempty"`
}

// AccountKind tells customer accounts (the sub-ledger) from system (GL) accounts.
type AccountKind string

const (
	CustomerAccount AccountKind = "customer" // opened with Accounts().Open
	SystemAccount   AccountKind = "system"   // "@" indicator, e.g. @FeeIncome, @PaystackClearing
	PotAccount      AccountKind = "pot"      // savings pot under a customer account (OpenPot)
	EscrowAccount   AccountKind = "escrow"   // one per escrow agreement (Escrows().Open)
)

// AccountQuery filters accounts. Empty fields don't filter.
type AccountQuery struct {
	IdentityID string
	Currency   string
	Kind       AccountKind       // CustomerAccount, PotAccount, EscrowAccount (system accounts: use Accounts().GetSystem)
	Status     string            // "active", "frozen", "closed", ...
	MetaData   map[string]string // exact matches on meta_data keys, e.g. {"owner": "alice"}
	Limit      int               // default 20, max 100
	Offset     int
}

// AccountPage is one page of Accounts().Find results.
type AccountPage struct {
	Items  []*Account
	Limit  int
	Offset int
	Total  int
}

// CloseInput closes an account (Accounts().Close).
type CloseInput struct {
	// SweepTo receives any remaining balance (e.g. the customer's bank payout
	// account, or another of their accounts). Required when the balance isn't zero.
	SweepTo   string
	Reference string // REQUIRED when there is a balance to sweep
	Reason    string
}

// CustomerInput onboards a customer: a Blnk identity plus one account per currency.
type CustomerInput struct {
	Type             string // "individual" (default) or "organization"
	FirstName        string
	LastName         string
	OtherNames       string
	OrganizationName string
	Email            string
	Phone            string
	Country          string
	Currencies       []string // e.g. ["NGN", "USD", "USDT"]; at least one

	// ExternalID is your own user id. It makes Customers().Open safe to retry:
	// accounts already opened for it are reused, missing ones are opened.
	ExternalID string
	MetaData   map[string]any
}

// Customer is an identity with its customer accounts.
type Customer struct {
	ID       string // Blnk identity id
	Identity *blnk.Identity
	Accounts map[string]*Account // by currency
}

// PotInput opens a savings pot under a customer account.
type PotInput struct {
	ParentID string // the customer's main account
	Name     string // e.g. "Rent", unique per parent
	Goal     Money  // optional target
}

// Pot is a savings sub-account. Move money in and out with Transfer.
type Pot struct {
	*Account
	Name   string
	Parent string
	Goal   Money // zero when no goal was set
}
```

## Moving money

```go
// DepositInput records money received from outside through a provider.
type DepositInput struct {
	AccountID string
	Amount    Money  // gross amount received from the provider
	Fee       Money  // deducted from Amount, credited to the fee income account; required unless Config.Fees is set (NoFee for free)
	Reference string // REQUIRED idempotency key, e.g. the provider's payment id

	// Provider that collected the money, e.g. "paystack" → debits @PaystackClearing.
	// Empty uses Config.DepositClearingAccount. ClearingAccount overrides with an exact indicator.
	Provider        string
	ClearingAccount string

	// Pending creates the deposit INFLIGHT so you can run checks before Confirm/Cancel.
	Pending     bool
	Description string
	MetaData    map[string]any
}

// WithdrawInput sends money out through a provider.
type WithdrawInput struct {
	AccountID string
	Amount    Money  // amount the customer receives
	Fee       Money  // charged on top of Amount; required unless Config.Fees is set (NoFee for free)
	Reference string // REQUIRED idempotency key
	// Provider that pays out, e.g. "flutterwave" → credits @FlutterwaveClearing.
	// Empty uses Config.PayoutClearingAccount. ClearingAccount overrides with an exact indicator.
	Provider        string
	ClearingAccount string

	// Immediate settles straight away. By default withdrawals are held INFLIGHT
	// until you Confirm (provider success) or Cancel (provider failure).
	Immediate   bool
	ExpiresIn   time.Duration // overrides Config.WithdrawalHoldTTL
	Description string
	MetaData    map[string]any
}

// TransferInput moves money between accounts.
type TransferInput struct {
	From           string // account / balance id or indicator
	To             string
	Amount         Money // amount the recipient receives
	Fee            Money // charged to sender on top of Amount; required unless Config.Fees is set (NoFee for free)
	Reference      string
	AllowOverdraft bool
	Pending        bool // create INFLIGHT; Confirm/Cancel later
	ScheduledFor   time.Time
	Description    string
	MetaData       map[string]any
}

// SplitInput pays one amount out to several accounts in a single transaction,
// e.g. a marketplace order: seller 90%, platform 10%, delivery a fixed 500.
type SplitInput struct {
	From      string // account debited (a customer account, or e.g. "@PaystackClearing" when collecting)
	Amount    Money  // total debited from From
	Parts     []SplitPart
	Reference string

	// AllowOverdraft lets From go negative. Set it when From is a provider
	// clearing account receiving money from outside, as with Deposit.
	AllowOverdraft bool
	Pending        bool // create INFLIGHT; Confirm/Cancel later
	Description    string
	MetaData       map[string]any
}

// SplitPart is one recipient. Set exactly one of Amount, Percent or Rest.
type SplitPart struct {
	To        string
	Amount    Money  // fixed amount
	Percent   string // share of the total, e.g. "10" for 10%
	Rest      bool   // whatever is left after the other parts (at most one part)
	Narration string
}

// ExchangeInput converts money between two accounts in different currencies,
// e.g. a customer's USD account to their NGN account.
type ExchangeInput struct {
	From string // account debited, in the sell currency
	To   string // account credited, in the buy currency

	// Set Sell (how much leaves From) or Buy (how much must arrive in To).
	Sell Money
	Buy  Money

	// Rate is units of the buy currency per 1 unit of the sell currency,
	// e.g. "1550.25" for USD→NGN. The buy amount rounds down and a computed
	// sell amount rounds up, so rounding never costs the house.
	Rate string

	Fee         Money  // in the sell currency, charged on top to the fee income account; required unless Config.Fees is set (NoFee for free)
	Reference   string // the two legs are recorded as Reference+"_sell" and Reference+"_buy"
	Pool        string // default FXPoolAccount
	Description string
	MetaData    map[string]any
}

// ExchangeReceipt is both legs of an exchange.
type ExchangeReceipt struct {
	Sold     Money  // debited from From (excluding Fee)
	Bought   Money  // credited to To
	Fee      Money  // fee charged in the sell currency
	Rate     string // as given
	Debit    *Receipt
	Credit   *Receipt
	Replayed bool
}

// HoldInput reserves money on an account (Pending().Hold).
type HoldInput struct {
	AccountID   string
	Destination string // where captured funds go, e.g. merchant account or "@CardSettlement"
	Amount      Money
	Reference   string
	ExpiresAt   time.Time // optional auto-release
	CaptureAt   time.Time // optional auto-capture (requires Blnk inflight commit queue)
	Description string
	MetaData    map[string]any
}

// BulkTransferInput is a batch of transfers (Payments().Bulk).
type BulkTransferInput struct {
	Items []TransferInput
	// Atomic: all succeed or all fail. Set it for payroll-style batches (the zero value is false).
	Atomic  bool
	Pending bool // all items INFLIGHT; Confirm/Cancel the batch with BatchID
	Async   bool // process in background; outcome arrives via bulk_transaction.* webhook
}

// BulkReceipt is the outcome of Payments().Bulk.
type BulkReceipt struct {
	BatchID string
	Status  string // applied | inflight | processing
	Count   int
}

// RecurringInput schedules the same transfer Count times.
type RecurringInput struct {
	Transfer TransferInput // Reference is the base; occurrence i uses Reference+"_"+i
	First    time.Time     // when the first occurrence runs
	Every    Interval
	Count    int // 1–120
}

// Interval is the gap between occurrences of a recurring transfer.
type Interval struct {
	Months int
	Days   int
}

// ConfirmItem is one pending transaction to confirm, optionally partially.
type ConfirmItem struct {
	TransactionID string
	Partial       *Money
}

// BatchResult is the outcome for one transaction in ConfirmMany / CancelMany.
type BatchResult struct {
	TransactionID string
	OK            bool
	Queued        bool
	Err           error // a ledger sentinel (errors.Is) when OK is false
}
```

## Refunds, disputes, escrow

```go
// RefundInput describes a full refund (Transactions().Refund).
type RefundInput struct {
	Reason      string
	Description string
	MetaData    map[string]any
}

// PartialRefundInput refunds part of a posted transaction.
type PartialRefundInput struct {
	Amount      Money
	Reference   string // REQUIRED, one per partial refund
	Reason      string
	Description string
	MetaData    map[string]any
}

// DisputeInput opens a dispute (chargeback) on a posted transaction.
type DisputeInput struct {
	TransactionID string // the disputed transaction (for a split, the leg's transaction id)
	Amount        Money  // zero = the full amount
	Reference     string // REQUIRED
	Reason        string
	MetaData      map[string]any
}

// DisputeOutcome is how a dispute ends.
type DisputeOutcome int

const (
	// DisputeUpheld: the payer wins and the held money goes back to them.
	DisputeUpheld DisputeOutcome = iota + 1
	// DisputeRejected: the recipient wins and the hold is released to them.
	DisputeRejected
)

// EscrowInput opens an escrow.
type EscrowInput struct {
	Payer       string // account the money comes from
	Beneficiary string // account it goes to on release
	Amount      Money
	Reference   string // REQUIRED; retrying with the same reference returns the same escrow
	Description string
	MetaData    map[string]any
}

// EscrowSettleInput releases or refunds escrowed money.
type EscrowSettleInput struct {
	Amount      Money  // zero = everything still held
	Reference   string // REQUIRED
	Description string
	MetaData    map[string]any
}

// Escrow is money taken from a payer and held until it is released to the
// beneficiary or refunded to the payer, in whole or in parts.
//
// Each escrow is its own Blnk balance, so what is still held is simply that
// balance, and Blnk itself refuses to release or refund more than is left.
type Escrow struct {
	ID          string // the escrow account id; pass it to Escrows().Release / Refund
	Payer       string
	Beneficiary string
	Amount      Money  // amount originally put in escrow
	Held        Money  // still in escrow
	Status      string // "held" while anything remains, then "settled"
	Reference   string // reference of the funding transaction
	Account     *Account
}
```

## Fees, quotes, limits

```go
// FeeFunc returns the fee for a transaction, in r.Amount's currency (an empty
// currency means r.Amount's). Return NoFee for a free transaction. An error
// refuses the transaction with ErrFeePricing.
type FeeFunc func(ctx context.Context, r FeeRequest) (Money, error)

// FeeRequest is what Config.Fees is asked to price.
type FeeRequest struct {
	Kind FeeKind

	// Account is the customer account the fee is charged to: the receiving
	// account for a deposit (the fee is deducted), the sending account
	// otherwise. Nil when that side is a system account ("@...").
	Account *Account

	// Amount is the transaction amount, resolved: currency and precision are
	// set. For a deposit it is the gross amount received; otherwise what the
	// recipient gets. For an exchange it is the amount sold.
	Amount Money

	Provider    string // deposits and withdrawals, e.g. "paystack"
	BuyCurrency string // exchanges: the currency bought
}

// FeeKind is the kind of transaction a fee is for.
type FeeKind string

const (
	FeeDeposit    FeeKind = "deposit"
	FeeWithdrawal FeeKind = "withdrawal"
	FeeTransfer   FeeKind = "transfer"
	FeeExchange   FeeKind = "exchange"
)

// FeeRule describes a fee as percentage + fixed part, clamped to [Min, Max].
// Amounts are decimals in the transaction currency, so keep one rule per currency:
//
//	FeeRule{Percent: "1.5", Min: "10", Max: "2000"}           // 1.5%, at least 10, at most 2,000
//	FeeRule{Fixed: "50"}                                       // flat 50
//	FeeRule{Tiers: []FeeTier{                                  // tiered
//		{UpTo: "5000", Rule: FeeRule{Fixed: "10"}},
//		{UpTo: "50000", Rule: FeeRule{Fixed: "25"}},
//		{Rule: FeeRule{Percent: "0.1", Max: "500"}},              // everything above
//	}}
type FeeRule struct {
	Percent  string // e.g. "1.5" for 1.5%; "" = none
	Fixed    string // flat part, e.g. "100"; "" = none
	Min      string // floor on the fee; "" = none
	Max      string // cap on the fee; "" = none
	Rounding Rounding

	// Tiers, when set, replace the fields above: the first tier whose UpTo is
	// >= the amount applies. A tier with an empty UpTo has no upper bound.
	Tiers []FeeTier
}

// FeeTier is one band of a tiered FeeRule.
type FeeTier struct {
	UpTo string
	Rule FeeRule
}

// Quote previews a movement before the customer confirms it: what is
// charged, what arrives, and the paying account's available balance after.
type Quote struct {
	Amount Money // the amount as entered (deposit: gross received; withdraw/transfer: what the recipient gets)
	Fee    Money
	Debit  Money // total leaving the paying account
	Credit Money // total reaching the customer account (deposit: net of fee)

	Account         string // the customer account the quote is about
	AvailableBefore Money
	AvailableAfter  Money
	Allowed         bool
	Reason          error // why not, when !Allowed (ErrInsufficientFunds, ErrLimitExceeded, ErrAccountFrozen, ...)
	CheckedByLedger bool  // true when Blnk itself dry-ran it (Config.CoreDryRun)
}

// Limit caps what a customer account can send. Amounts are decimals in
// Currency; "" means no cap. Configure them in Config.Limits, e.g. by KYC tier:
//
//	Limits: []ledger.Limit{
//		{Tier: "tier1", Currency: "NGN", PerTransaction: "50000", Daily: "200000"},
//		{Tier: "tier2", Currency: "NGN", PerTransaction: "1000000", Daily: "5000000", Monthly: "50000000"},
//		{Currency: "NGN", PerTransaction: "10000", Daily: "30000"}, // accounts with no tier
//	}
//
// An account's tier is its meta_data.tier. Limits apply to money leaving
// customer accounts (Withdraw, Transfer, Schedule, Hold, Split, Exchange and
// Escrows().Open); system accounts, pots and escrows are not limited, and
// Bulk batches are not checked. Usage is read before posting, so two
// payments racing each other can both pass the check.
type Limit struct {
	Tier           string // "" matches accounts without a tier; "*" matches any tier
	Currency       string
	PerTransaction string
	Daily          string
	Monthly        string
}

// LimitUsage is how much of its limits an account has used.
type LimitUsage struct {
	Limit       *Limit // nil when no limit applies
	DailyUsed   Money
	MonthlyUsed Money
	DailyLeft   *Money // nil when there is no daily cap
	MonthlyLeft *Money
}
```

## Results and reads

```go
// Receipt is the domain view of a Blnk transaction.
type Receipt struct {
	TransactionID     string         `json:"transaction_id"`
	ParentTransaction string         `json:"parent_transaction,omitempty"`
	Reference         string         `json:"reference"`
	Status            string         `json:"status"`       // Blnk's: QUEUED | APPLIED | INFLIGHT | VOID | REJECTED | SCHEDULED
	EntryStatus       EntryStatus    `json:"entry_status"` // accounting view: posted | pending | voided | queued | scheduled | rejected
	Amount            Money          `json:"amount"`       // {"amount":"0.456","currency":"BTC","precise_amount":"45600000","precision":100000000}
	Source            string         `json:"source"`
	Destination       string         `json:"destination,omitempty"`
	Legs              []Leg          `json:"legs,omitempty"` // populated for split (fee) transactions
	Description       string         `json:"description,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	MetaData          map[string]any `json:"meta_data,omitempty"`

	Queued   bool `json:"queued,omitempty"`   // accepted by the queue; final status arrives later (webhook / poll)
	Replayed bool `json:"replayed,omitempty"` // reference already existed with the same payload; this is the original

	Raw *blnk.Transaction `json:"-"`
}

// Leg is one destination of a transaction with several, e.g. a payment and its fee.
type Leg struct {
	Account       string `json:"account"`
	Amount        Money  `json:"amount"`       // set for fixed legs (fee, payout); zero for "left"/"%"
	Distribution  string `json:"distribution"` // as sent: "left", "20%", or the precise integer
	TransactionID string `json:"transaction_id,omitempty"`
	Narration     string `json:"narration,omitempty"`
}

// EntryStatus is the accounting view of Blnk's transaction status.
type EntryStatus string

const (
	EntryPosted    EntryStatus = "posted"    // Blnk APPLIED: in the balances
	EntryPending   EntryStatus = "pending"   // Blnk INFLIGHT: funds reserved, not posted
	EntryVoided    EntryStatus = "voided"    // Blnk VOID: pending entry cancelled, never posted
	EntryQueued    EntryStatus = "queued"    // Blnk QUEUED: accepted, not yet processed
	EntryScheduled EntryStatus = "scheduled" // Blnk SCHEDULED: will post at scheduled_for
	EntryRejected  EntryStatus = "rejected"  // Blnk REJECTED: refused (e.g. insufficient funds)
)

// HistoryQuery filters and pages Accounts().History.
type HistoryQuery struct {
	Direction Direction
	Statuses  []string // "posted"/"pending"/... or Blnk's "APPLIED"/"INFLIGHT"/...; empty = all
	Since     time.Time
	Until     time.Time
	Limit     int // default 20, max 100
	Offset    int
}

// Direction filters History by which side the account is on.
type Direction int

const (
	All      Direction = iota // money in and out
	Incoming                  // the account is the destination
	Outgoing                  // the account is the source
)

// HistoryPage is one page of Accounts().History.
type HistoryPage struct {
	Items  []Receipt
	Limit  int
	Offset int
	Total  int
}

// Statement is an account statement for [From, To): opening balance, every
// posted entry with the running balance after it, and closing balance.
type Statement struct {
	AccountID string
	Currency  string
	From, To  time.Time
	Opening   Money
	Closing   Money
	Credits   Money // total in
	Debits    Money // total out
	Lines     []StatementLine
}

// StatementLine is one posted entry on a Statement.
type StatementLine struct {
	Date        time.Time
	Reference   string
	Description string
	Credit      Money  // money in (zero for debits)
	Debit       Money  // money out (zero for credits)
	Balance     Money  // running balance after this line
	Counterpart string // the other account: an id, or "@Name" for system accounts
	Receipt     Receipt
}

// TrialBalance is every balance in one currency, summed. In double entry the
// total is always zero; anything else means entries were lost or posted
// outside this ledger, and is worth an alert.
type TrialBalance struct {
	Currency string

	// System lists every system account (fee income, clearing, suspense, ...)
	// with its balance: the figures finance reconciles.
	System []*Account

	// CustomerFunds is what customer accounts (including pots and escrows)
	// hold in total: the money you owe your customers.
	CustomerFunds    Money
	CustomerAccounts int

	Net      Money // sum of everything; zero when balanced
	Balanced bool
}
```

## Back office

```go
// SettlementInput records a provider settlement (Operations().RecordSettlement).
type SettlementInput struct {
	Provider        string // e.g. "paystack" → @PaystackClearing
	ClearingAccount string // or an exact clearing account
	Direction       SettlementDirection
	Amount          Money  // amount cleared from the clearing account
	Fee             Money  // optional provider fee, booked to the fee expense account
	Reference       string // REQUIRED, e.g. the provider's settlement id
	BankAccount     string // default BankOperatingAccount
	FeeAccount      string // default ProviderFeesAccount
	Description     string
}

// SettlementDirection says which way a provider settlement moves money.
type SettlementDirection int

const (
	// SettlementCollection: the provider pays you the money it collected
	// (deposits). Clears the provider's receivable.
	SettlementCollection SettlementDirection = iota
	// SettlementPayout: your bank funds the payouts the provider made for you
	// (withdrawals). Clears the provider's payable.
	SettlementPayout
)

// SettlementReceipt holds the settlement's entries.
type SettlementReceipt struct {
	Entries  []*Receipt // main entry, then the fee entry if any
	Replayed bool
}

// SuspenseInput parks money that arrived but can't be matched to a customer yet.
type SuspenseInput struct {
	Amount          Money
	Provider        string // provider it arrived through, e.g. "paystack"
	ClearingAccount string // or an exact clearing account; default Config.DepositClearingAccount
	Reference       string // REQUIRED, e.g. the provider's payment id
	Note            string // what is known about it (sender name, narration, ...)
	MetaData        map[string]any
}

// AllocateInput moves money out of suspense once its owner is known.
type AllocateInput struct {
	AccountID string // the customer account, or a clearing account to send it back
	Amount    Money
	Reference string // REQUIRED
	// SuspenseReference links the allocation to the ParkInSuspense reference.
	SuspenseReference string
	Note              string
}

// AdjustInput is a manual correction, with who asked for it and who approved it.
type AdjustInput struct {
	AccountID   string
	Type        AdjustmentType
	Amount      Money
	Reason      string // REQUIRED
	RequestedBy string
	ApprovedBy  string // REQUIRED; must differ from RequestedBy when both are set
	Reference   string // REQUIRED
	// Counterparty is the other side of the entry; default @Adjustments.
	Counterparty string
	// AllowOverdraft lets a debit take the account below zero.
	AllowOverdraft bool
}

// AdjustmentType is the direction of a manual correction.
type AdjustmentType int

const (
	AdjustCredit AdjustmentType = iota + 1 // add money to the account
	AdjustDebit                            // take money from the account
)

// WatchInput sets up an alert on an account's balance, e.g. "tell me when
// @PayoutClearing's float drops below 1,000,000".
type WatchInput struct {
	AccountID string // balance id (for a system account, use Accounts().GetSystem(...).ID)
	Field     string // balance (default), credit_balance, debit_balance, inflight_balance, inflight_debit_balance, inflight_credit_balance
	Operator  string // <, <=, >, >=, ==, !=  ("=" is accepted and sent as "==")
	Threshold Money
	MetaData  map[string]any
}

// Watch is a balance monitor. When its condition holds, Blnk sends the
// "balance.monitor" webhook after every update of the balance (not only the
// first time), so debounce alerts on your side.
type Watch struct {
	ID        string
	AccountID string
	Field     string
	Operator  string
	Threshold Money
}

// ReconcileInput matches a provider's records against the ledger.
type ReconcileInput struct {
	Source string // e.g. "paystack"

	// Records are sent inline (instant reconciliation, Core 0.10.0+, up to
	// 10,000). For bigger files use File instead.
	Records []ExternalRecord

	// File is a CSV or JSON export with columns id, amount, currency,
	// reference, description, date (RFC3339, amount in major units).
	File     io.Reader
	Filename string

	// RuleIDs are matching rules created with Reconciliations().CreateRule. Empty uses
	// one rule matching reference + currency + exact amount (created once
	// per Service).
	RuleIDs  []string
	Strategy string // one_to_one (default), one_to_many, many_to_one
	// GroupingCriteria groups ledger entries for one_to_many / many_to_one,
	// e.g. "parent_transaction" to match one provider line against the legs
	// of a transaction that carried a fee.
	GroupingCriteria string
	DryRun           bool // match without recording the result
}

// ExternalRecord is one line of a provider's settlement report or bank statement.
type ExternalRecord struct {
	ID          string // the provider's id for the line
	Amount      Money
	Reference   string // what you sent to Blnk as the transaction reference
	Description string
	Date        time.Time
}

// Reconciliation is a reconciliation run. Blnk reports counts only; it has no
// endpoint listing which lines matched.
type Reconciliation struct {
	ID          string
	Status      string // started | in_progress | completed | failed
	Matched     int
	Unmatched   int
	DryRun      bool
	StartedAt   time.Time
	CompletedAt time.Time
}

// MatchingRule says when an external record and a ledger transaction are the same.
type MatchingRule = blnk.MatchingRule

// MatchingCriterion is one condition of a MatchingRule.
type MatchingCriterion = blnk.MatchingCriterion
```

## Webhooks and observability

```go
// Event is a decoded Blnk webhook. Exactly one of the typed fields is set for
// known events; Data always holds the raw payload.
type Event struct {
	Name string

	Receipt *Receipt   // transaction.* events
	Bulk    *BulkEvent // bulk_transaction.*
	Alert   *Watch     // balance.monitor
	Account *Account   // balance.created

	Data json.RawMessage
}

// BulkEvent is the payload of bulk_transaction.* events.
type BulkEvent struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
	Count   int    `json:"transaction_count"` // non-failed transactions
	Error   string `json:"error,omitempty"`
}

// Webhook verification settings.
type WebhookConfig struct {
	// Secret is Blnk's server.secret_key. When set, unsigned or mis-signed
	// deliveries are rejected with ErrSignature. When empty, nothing is
	// verified: leave it empty only if your Blnk doesn't sign webhooks.
	Secret string
	// Tolerance rejects deliveries whose X-Blnk-Timestamp is further than this
	// from now (replay protection). Default 5 minutes; negative disables it.
	Tolerance time.Duration
}

// Observer is told about every Ledger call: Start runs before it and returns
// the context to use (e.g. carrying a trace span) and a function that is
// called with the call's error when it returns. Use it for logs, metrics and
// tracing (OpenTelemetry: start a span in Start, end it in the returned func).
type Observer interface {
	Start(ctx context.Context, op string) (context.Context, func(err error))
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(ctx context.Context, op string) (context.Context, func(err error))
```

## Service and configuration

```go
// Service implements Ledger on top of a Blnk Core instance.
type Service struct {
	c   *blnk.Client
	cfg Config

	ruleMu sync.Mutex
	ruleID string // default reconciliation matching rule, created on first use

	indicators sync.Map // "@Name|CUR" → balance id; indicator balances never move
}

// Config controls how the Service maps domain operations onto Blnk.
type Config struct {
	// LedgerID that customer accounts are created in (required for Accounts().Open).
	LedgerID string

	// Display sets how amounts are shown to people per currency (Money.Display),
	// e.g. {"ETH": {Decimals: 4, TrimZeros: true}}. Unlisted currencies use
	// BuiltinDisplay, else every decimal place of their precision, padded.
	Display map[string]DisplayRule

	// Precisions maps currency → smallest-unit multiplier. Unlisted currencies use DefaultPrecision.
	Precisions       map[string]int64
	DefaultPrecision int64 // default 100

	// System (GL) accounts. Defaults come from the chart of accounts in
	// accounts.go. If you already have money on older indicators (e.g. "@Fees"),
	// set them here so balances aren't split across two accounts.
	FeeIncomeAccount       string // default "@FeeIncome"
	DepositClearingAccount string // default "@DepositClearing" (used when DepositInput.Provider is empty)
	PayoutClearingAccount  string // default "@PayoutClearing" (used when WithdrawInput.Provider is empty)

	// Fees prices every chargeable transaction (Deposit, Withdraw, Transfer,
	// Exchange, Schedule, each Bulk item and the quotes) that has no explicit
	// Fee. It runs on every such call, so prices can change at runtime. Without
	// it, those calls must set Fee (ledger.NoFee for a free transaction).
	Fees FeeFunc

	// UseQueue routes writes through Blnk's queue (status QUEUED, final result via
	// webhook). Default false = skip_queue:true, so you get APPLIED/INFLIGHT or a
	// definite error (e.g. insufficient funds) in the response.
	UseQueue bool

	// WithdrawalHoldTTL auto-cancels pending withdrawals the provider never
	// confirms. Default 24h. Negative disables expiry.
	WithdrawalHoldTTL time.Duration

	// Limits caps what customer accounts can send, per tier and currency
	// (see Limit). Checking a daily or monthly limit reads the account's
	// outgoing transactions for the period (plus one GET per confirmation or
	// cancellation of a pending transaction among them).
	Limits []Limit
	// Location sets the day and month boundaries for limits. Default UTC.
	Location *time.Location

	// CoreDryRun makes quotes also ask Blnk to dry-run the transaction
	// (dry_run, Core 0.15.3+), which checks everything Blnk would check.
	// Leave it off on older Core: they ignore dry_run and would post.
	CoreDryRun bool

	// EnforceAccountStatus rejects movements touching accounts whose
	// meta_data.status is "frozen" or "blocked" (one extra GET per account).
	EnforceAccountStatus bool

	Now func() time.Time
}

// OpError carries the operation name, a domain sentinel, and the cause.
type OpError struct {
	Op    string
	Kind  error
	Cause error
}
```

## Constants

```go
// Chart of accounts for the system (GL) side of the ledger. Customer accounts
// are the sub-ledger; these are the accounts finance reconciles against.
//
// Blnk creates an indicator balance the first time it is used, so none of these
// need setting up. Blnk debits the source and credits the destination, so a
// debit-normal account (asset) runs negative and a credit-normal one
// (liability, income) runs positive.
const (
	// FeeIncomeAccount: income. Every fee leg is credited here.
	FeeIncomeAccount = "@FeeIncome"

	// DepositClearingAccount: asset (receivable). Default deposit source when no
	// provider is named. Debited on deposit, cleared when the provider settles.
	DepositClearingAccount = "@DepositClearing"

	// PayoutClearingAccount: liability (payable). Default withdrawal destination
	// when no provider is named. Credited on withdrawal, cleared when funds leave the bank.
	PayoutClearingAccount = "@PayoutClearing"

	// MerchantPayableAccount: liability. Captured card-style holds owed to merchants.
	MerchantPayableAccount = "@MerchantPayable"

	// SuspenseAccount: unidentified or unmatched funds awaiting investigation.
	SuspenseAccount = "@Suspense"

	// FXPoolAccount: the house's currency position. Exchange debits the customer
	// into the pool in one currency and pays out of the pool in the other, so
	// each currency's @FXPool balance is the open position in that currency.
	FXPoolAccount = "@FXPool"

	// AdjustmentsAccount: counterparty for manual corrections (Adjust).
	AdjustmentsAccount = "@Adjustments"

	// BankOperatingAccount: asset. Your bank account; debited when a provider
	// settles collections to you, credited when you fund payouts.
	BankOperatingAccount = "@BankOperating"

	// ProviderFeesAccount: expense. Fees providers deduct at settlement.
	ProviderFeesAccount = "@ProviderFees"
)

// Event names Blnk sends to the global webhook.
const (
	EventTransactionQueued    = "transaction.queued"
	EventTransactionApplied   = "transaction.applied"
	EventTransactionScheduled = "transaction.scheduled"
	EventTransactionInflight  = "transaction.inflight"
	EventTransactionVoid      = "transaction.void" // also sent when a hold expires
	EventTransactionRejected  = "transaction.rejected"
	EventBulkApplied          = "bulk_transaction.applied"
	EventBulkInflight         = "bulk_transaction.inflight"
	EventBulkFailed           = "bulk_transaction.failed"
	EventBalanceAlert         = "balance.monitor"
	EventBalanceCreated       = "balance.created"
	EventIdentityCreated      = "identity.created"
	EventLedgerCreated        = "ledger.created"
	EventSystemError          = "system.error"
)
```

## Values and errors

```go
// Domain errors. Check with errors.Is; the underlying *blnk.APIError is still
// reachable with errors.As for logging (status, code, details).
var (
	ErrInvalidInput       = errors.New("ledger: invalid input")
	ErrInsufficientFunds  = errors.New("ledger: insufficient funds")
	ErrNotFound           = errors.New("ledger: not found")
	ErrDuplicateReference = errors.New("ledger: reference already used for a different transaction")
	ErrAlreadyFinalized   = errors.New("ledger: transaction already confirmed, cancelled or refunded")
	ErrNotPending         = errors.New("ledger: transaction is not pending (INFLIGHT)")
	ErrAmountExceeded     = errors.New("ledger: amount exceeds remaining held amount")
	ErrAccountFrozen      = errors.New("ledger: account is frozen or blocked")
	ErrAccountClosed      = errors.New("ledger: account is closed")
	ErrPendingActivity    = errors.New("ledger: account has pending (INFLIGHT) activity")
	ErrLimitExceeded      = errors.New("ledger: transaction limit exceeded")
	ErrApprovalRequired   = errors.New("ledger: approval required")
	ErrSignature          = errors.New("ledger: invalid webhook signature")
	ErrFeePricing         = errors.New("ledger: fee could not be priced")
	ErrConflict           = errors.New("ledger: conflicting operation in progress")
	ErrUnavailable        = errors.New("ledger: ledger temporarily unavailable")
	ErrUnauthorized       = errors.New("ledger: unauthorized")
)

var (
	Daily   = Interval{Days: 1}
	Weekly  = Interval{Days: 7}
	Monthly = Interval{Months: 1}
)

// BuiltinDisplay is used for currencies not in Config.Display. Currencies in
// neither show every decimal place their precision has, padded (fiat style).
var BuiltinDisplay = map[string]DisplayRule{

// NoFee states that a transaction is deliberately free. Chargeable calls
// (Deposit, Withdraw, Transfer, Exchange, Schedule, each Bulk item and the
// quotes) must say what they charge: set Fee, or set Config.Fees to price
// them. Leaving both out is an error, so a fee can't be forgotten.
var NoFee = Amount("0", "")

// BuiltinPrecisions are used when Config.Precisions doesn't list a currency.
// Fiat not listed here falls back to Config.DefaultPrecision (100 = 2 dp).
//
// Keep a currency's precision fixed once money has moved: Blnk sums
// precise_amount on balances, so mixing precisions corrupts the totals.
var BuiltinPrecisions = map[string]int64{
```

## Functions

```go
// ClearingAccount returns the per-provider clearing account:
// ClearingAccount("paystack") == "@PaystackClearing". It also accepts "@Paystack"
// and "@PaystackClearing". One clearing account per provider means each
// provider's settlement report can be reconciled against its own balance.
func ClearingAccount(provider string) string

// EntryStatusOf maps a Blnk status (APPLIED, INFLIGHT, ...) to an EntryStatus.
func EntryStatusOf(blnkStatus string) EntryStatus

// ComputeFee applies rule to a resolved amount (one with Precision set, such
// as FeeRequest.Amount) and returns the fee in the same currency and
// precision. Use Service.ComputeFee for an amount as typed.
func ComputeFee(rule FeeRule, amount Money) (Money, error)

// LogObserver logs every call with its duration: Info on success, Warn for
// business refusals (insufficient funds, limits, frozen accounts), Error
// otherwise.
func LogObserver(log *slog.Logger) Observer

// Instrument wraps l so o observes every call. Calls are named
// "Namespace.Method", e.g. "Payments.Transfer".
func Instrument(l Ledger, o Observer) Ledger

// New builds a Service. The client is shared and safe for concurrent use.
func New(c *blnk.Client, cfg Config) *Service

// Amount builds Money from a decimal string as the user typed it:
// Amount("0.456", "BTC"), Amount("100.56", "USD"), Amount("1,500", "NGN").
func Amount(value, currency string) Money

// Minor builds Money from an integer in the smallest unit: Minor(10056, "USD") = 100.56 USD.
func Minor(precise int64, currency string) Money

// Precise builds a fully specified Money from a smallest-unit integer and precision.
func Precise(precise *big.Int, precision int64, currency string) Money

// ParseDecimal resolves a decimal string against an explicit precision.
func ParseDecimal(value, currency string, precision int64) (Money, error)
```

Every money-movement method requires `Reference`. Resending the same reference returns the original with `Replayed: true` instead of posting twice.
