# Ledger service contract

The `ledger.Ledger` interface, its namespaces, and every parameter and result type they use, generated from the source in `ledger/` (the source is authoritative). `Config` is documented in `ledger/ledger.go`.

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
	Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error)

	// RefundPartial sends part of a posted transaction back: destination → source,
	// linked to the original with meta_data.refund_of. The original's
	// meta_data.refunded_precise tracks the total refunded, and refunds beyond the
	// original amount are rejected. Use Refund for Blnk's one-shot full reversal.
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

## Money

```go
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

type DisplayRule struct {
	Decimals  int  // most decimal places shown; capped at the currency's precision
	TrimZeros bool // drop trailing zeros instead of padding to Decimals
}
```

## Accounts and customers

```go
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

type AccountKind string

const (
	CustomerAccount AccountKind = "customer" // opened with Accounts().Open
	SystemAccount   AccountKind = "system"   // "@" indicator, e.g. @FeeIncome, @PaystackClearing
	PotAccount      AccountKind = "pot"      // savings pot under a customer account (OpenPot)
	EscrowAccount   AccountKind = "escrow"   // one per escrow agreement (Escrows().Open)
)

type AccountQuery struct {
	IdentityID string
	Currency   string
	Kind       AccountKind       // CustomerAccount, PotAccount, EscrowAccount (system accounts: use Accounts().GetSystem)
	Status     string            // "active", "frozen", "closed", ...
	MetaData   map[string]string // exact matches on meta_data keys, e.g. {"owner": "alice"}
	Limit      int               // default 20, max 100
	Offset     int
}

type AccountPage struct {
	Items  []*Account
	Limit  int
	Offset int
	Total  int
}

type CloseInput struct {
	// SweepTo receives any remaining balance (e.g. the customer's bank payout
	// account, or another of their accounts). Required when the balance isn't zero.
	SweepTo   string
	Reference string // REQUIRED when there is a balance to sweep
	Reason    string
}

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

type Customer struct {
	ID       string // Blnk identity id
	Identity *blnk.Identity
	Accounts map[string]*Account // by currency
}

type PotInput struct {
	ParentID string // the customer's main account
	Name     string // e.g. "Rent", unique per parent
	Goal     Money  // optional target
}

type Pot struct {
	*Account
	Name   string
	Parent string
	Goal   Money // zero when no goal was set
}
```

## Moving money

```go
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

type SplitPart struct {
	To        string
	Amount    Money  // fixed amount
	Percent   string // share of the total, e.g. "10" for 10%
	Rest      bool   // whatever is left after the other parts (at most one part)
	Narration string
}

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

type ExchangeReceipt struct {
	Sold     Money  // debited from From (excluding Fee)
	Bought   Money  // credited to To
	Fee      Money  // fee charged in the sell currency
	Rate     string // as given
	Debit    *Receipt
	Credit   *Receipt
	Replayed bool
}

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

type BulkTransferInput struct {
	Items []TransferInput
	// Atomic: all succeed or all fail. Set it for payroll-style batches (the zero value is false).
	Atomic  bool
	Pending bool // all items INFLIGHT; Confirm/Cancel the batch with BatchID
	Async   bool // process in background; outcome arrives via bulk_transaction.* webhook
}

type BulkReceipt struct {
	BatchID string
	Status  string // applied | inflight | processing
	Count   int
}

type RecurringInput struct {
	Transfer TransferInput // Reference is the base; occurrence i uses Reference+"_"+i
	First    time.Time     // when the first occurrence runs
	Every    Interval
	Count    int // 1–120
}

type Interval struct {
	Months int
	Days   int
}

type ConfirmItem struct {
	TransactionID string
	Partial       *Money
}

type BatchResult struct {
	TransactionID string
	OK            bool
	Queued        bool
	Err           error // a ledger sentinel (errors.Is) when OK is false
}
```

## Refunds, disputes, escrow

```go
type RefundInput struct {
	Reason      string
	Description string
	MetaData    map[string]any
}

type PartialRefundInput struct {
	Amount      Money
	Reference   string // REQUIRED, one per partial refund
	Reason      string
	Description string
	MetaData    map[string]any
}

type DisputeInput struct {
	TransactionID string // the disputed transaction (for a split, the leg's transaction id)
	Amount        Money  // zero = the full amount
	Reference     string // REQUIRED
	Reason        string
	MetaData      map[string]any
}

type DisputeOutcome int

const (
	// DisputeUpheld: the payer wins and the held money goes back to them.
	DisputeUpheld DisputeOutcome = iota + 1
	// DisputeRejected: the recipient wins and the hold is released to them.
	DisputeRejected
)

type EscrowInput struct {
	Payer       string // account the money comes from
	Beneficiary string // account it goes to on release
	Amount      Money
	Reference   string // REQUIRED; retrying with the same reference returns the same escrow
	Description string
	MetaData    map[string]any
}

type EscrowSettleInput struct {
	Amount      Money  // zero = everything still held
	Reference   string // REQUIRED
	Description string
	MetaData    map[string]any
}

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
// NoFee states that a transaction is deliberately free. Chargeable calls
// (Deposit, Withdraw, Transfer, Exchange, Schedule, each Bulk item and the
// quotes) must say what they charge: set Fee, or set Config.Fees to price
// them. Leaving both out is an error, so a fee can't be forgotten.
var NoFee = Amount("0", "")

// FeeKind is the kind of transaction a fee is for.
type FeeKind string

const (
	FeeDeposit    FeeKind = "deposit"
	FeeWithdrawal FeeKind = "withdrawal"
	FeeTransfer   FeeKind = "transfer"
	FeeExchange   FeeKind = "exchange"
)

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

// FeeFunc returns the fee for a transaction, in r.Amount's currency (an empty
// currency means r.Amount's). Return NoFee for a free transaction. An error
// refuses the transaction with ErrFeePricing.
type FeeFunc func(ctx context.Context, r FeeRequest) (Money, error)

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

type FeeTier struct {
	UpTo string
	Rule FeeRule
}

type Rounding int

const (
	RoundHalfUp Rounding = iota // 0.5 and above rounds up (default)
	RoundUp                     // any fraction rounds up (in the house's favour for fees)
	RoundDown                   // fractions are dropped (in the customer's favour for fees)
)

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

type Limit struct {
	Tier           string // "" matches accounts without a tier; "*" matches any tier
	Currency       string
	PerTransaction string
	Daily          string
	Monthly        string
}

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

type Leg struct {
	Account       string `json:"account"`
	Amount        Money  `json:"amount"`       // set for fixed legs (fee, payout); zero for "left"/"%"
	Distribution  string `json:"distribution"` // as sent: "left", "20%", or the precise integer
	TransactionID string `json:"transaction_id,omitempty"`
	Narration     string `json:"narration,omitempty"`
}

type EntryStatus string

const (
	EntryPosted    EntryStatus = "posted"    // Blnk APPLIED: in the balances
	EntryPending   EntryStatus = "pending"   // Blnk INFLIGHT: funds reserved, not posted
	EntryVoided    EntryStatus = "voided"    // Blnk VOID: pending entry cancelled, never posted
	EntryQueued    EntryStatus = "queued"    // Blnk QUEUED: accepted, not yet processed
	EntryScheduled EntryStatus = "scheduled" // Blnk SCHEDULED: will post at scheduled_for
	EntryRejected  EntryStatus = "rejected"  // Blnk REJECTED: refused (e.g. insufficient funds)
)

type HistoryQuery struct {
	Direction Direction
	Statuses  []string // "posted"/"pending"/... or Blnk's "APPLIED"/"INFLIGHT"/...; empty = all
	Since     time.Time
	Until     time.Time
	Limit     int // default 20, max 100
	Offset    int
}

type Direction int

const (
	All Direction = iota
	Incoming
	Outgoing
)

type HistoryPage struct {
	Items  []Receipt
	Limit  int
	Offset int
	Total  int
}

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

type StatementLine struct {
	Date        time.Time
	Reference   string
	Description string
	Credit      Money // money in (zero for debits)
	Debit       Money // money out (zero for credits)
	Balance     Money // running balance after this line
	Counterpart string
	Receipt     Receipt
}

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

type SettlementDirection int

const (
	// SettlementCollection: the provider pays you the money it collected
	// (deposits). Clears the provider's receivable.
	SettlementCollection SettlementDirection = iota
	// SettlementPayout: your bank funds the payouts the provider made for you
	// (withdrawals). Clears the provider's payable.
	SettlementPayout
)

type SettlementReceipt struct {
	Entries  []*Receipt // main entry, then the fee entry if any
	Replayed bool
}

type SuspenseInput struct {
	Amount          Money
	Provider        string // provider it arrived through, e.g. "paystack"
	ClearingAccount string // or an exact clearing account; default Config.DepositClearingAccount
	Reference       string // REQUIRED, e.g. the provider's payment id
	Note            string // what is known about it (sender name, narration, ...)
	MetaData        map[string]any
}

type AllocateInput struct {
	AccountID string // the customer account, or a clearing account to send it back
	Amount    Money
	Reference string // REQUIRED
	// SuspenseReference links the allocation to the ParkInSuspense reference.
	SuspenseReference string
	Note              string
}

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

type AdjustmentType int

const (
	AdjustCredit AdjustmentType = iota + 1 // add money to the account
	AdjustDebit                            // take money from the account
)

type WatchInput struct {
	AccountID string // balance id (for a system account, use Accounts().GetSystem(...).ID)
	Field     string // balance (default), credit_balance, debit_balance, inflight_balance, inflight_debit_balance, inflight_credit_balance
	Operator  string // <, <=, >, >=, ==, !=  ("=" is accepted and sent as "==")
	Threshold Money
	MetaData  map[string]any
}

type Watch struct {
	ID        string
	AccountID string
	Field     string
	Operator  string
	Threshold Money
}

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
	DryRun   bool   // match without recording the result
}

type ExternalRecord struct {
	ID          string // the provider's id for the line
	Amount      Money
	Reference   string // what you sent to Blnk as the transaction reference
	Description string
	Date        time.Time
}

type Reconciliation struct {
	ID          string
	Status      string // started | in_progress | completed | failed
	Matched     int
	Unmatched   int
	DryRun      bool
	StartedAt   time.Time
	CompletedAt time.Time
}

type MatchingRule = blnk.MatchingRule

type MatchingCriterion = blnk.MatchingCriterion

// in package blnk:
type MatchingCriterion struct {
	Field          string  `json:"field"`    // amount | date | description | reference | currency
	Operator       string  `json:"operator"` // equals | greater_than | less_than | contains
	Value          string  `json:"value,omitempty"`
	Pattern        string  `json:"pattern,omitempty"`
	AllowableDrift float64 `json:"allowable_drift,omitempty"` // amount: fraction (0.01 = 1%, Core 0.15.0+); date: seconds
}

type MatchingRule struct {
	RuleID      string              `json:"rule_id,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Criteria    []MatchingCriterion `json:"criteria"`
	CreatedAt   Time                `json:"created_at,omitempty"`
}
```

## Webhooks and observability

```go
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

type Event struct {
	Name string

	Receipt *Receipt   // transaction.* events
	Bulk    *BulkEvent // bulk_transaction.*
	Alert   *Watch     // balance.monitor
	Account *Account   // balance.created

	Data json.RawMessage
}

type BulkEvent struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
	Count   int    `json:"transaction_count"` // non-failed transactions
	Error   string `json:"error,omitempty"`
}

type WebhookConfig struct {
	// Secret is Blnk's server.secret_key. When set, unsigned or mis-signed
	// deliveries are rejected with ErrSignature.
	Secret string
	// Tolerance rejects deliveries whose X-Blnk-Timestamp is further than this
	// from now (replay protection). Default 5 minutes; negative disables it.
	Tolerance time.Duration
}

type Observer interface {
	Start(ctx context.Context, op string) (context.Context, func(err error))
}
```

Every money-movement method requires `Reference`. Resending the same reference returns the original with `Replayed: true` instead of posting twice.
