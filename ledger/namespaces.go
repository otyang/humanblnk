package ledger

import (
	"context"
	"time"
)

// Ledger is the interface your application depends on, grouped into
// namespaces: l.Accounts().Open(...), l.Payments().Transfer(...),
// l.Pending().Confirm(...). Depend on a single namespace (e.g. ledger.Pending)
// where that is all a component needs. Service implements it on Blnk;
// Instrument wraps any Ledger with an Observer.
//
// Ledgers, Accounts and System each work on one kind of object only: Blnk
// ledgers, customer accounts, and system ("@") accounts. Each takes its own id
// type (LedgerID, AccountID, SystemRef) and refuses ids of another kind.
type Ledger interface {
	Ledgers() Ledgers
	Accounts() Accounts
	System() System
	Customers() Customers
	Payments() Payments
	Pending() Pending
	Transactions() Transactions
	Disputes() Disputes
	Escrows() Escrows
	Operations() Operations
	Reconciliations() Reconciliations
}

// Ledgers: create, read and freeze Blnk ledgers. Accounts are opened inside a ledger.
type Ledgers interface {
	// Create creates a ledger, or returns the one already created with the same
	// Key (Replayed set), so it never makes two. The same Key with a different
	// Name is refused with ErrDuplicateReference. Create ledgers when you
	// provision, not on the request path.
	Create(ctx context.Context, in CreateLedgerInput) (*LedgerInfo, error)

	// Get returns a ledger. A ledger that doesn't exist is ErrLedgerNotFound.
	Get(ctx context.Context, id LedgerID) (*LedgerInfo, error)

	// List lists ledgers, oldest first.
	List(ctx context.Context, q LedgerQuery) (*LedgerPage, error)

	// Freeze marks the ledger frozen: no money can leave any of its customer
	// accounts while Config.EnforceAccountStatus is on. Money can still come in,
	// and system accounts are never blocked.
	Freeze(ctx context.Context, id LedgerID, reason string) error

	// Unfreeze marks a frozen ledger active again.
	Unfreeze(ctx context.Context, id LedgerID) error
}

// Accounts: open, read and manage customer accounts (and pots and escrows),
// and their history and statements. Every method refuses ledger ids and
// system accounts with ErrWrongKind; system accounts are read through System().
type Accounts interface {
	// Open creates a customer account (a Blnk balance) in Config.LedgerID or
	// in.LedgerID, marked active. The ledger must exist (ErrLedgerNotFound
	// otherwise). With ExternalID it is safe to retry.
	Open(ctx context.Context, in OpenAccountInput) (*Account, error)

	// Get returns an account with its balance, available balance (balance minus
	// pending debits) and pending amounts.
	Get(ctx context.Context, id AccountID) (*Account, error)

	// Find lists accounts matching q, oldest first. System accounts are never
	// listed.
	Find(ctx context.Context, q AccountQuery) (*AccountPage, error)

	// Freeze marks the account frozen in metadata. Enforced by this layer when
	// Config.EnforceAccountStatus is true (Blnk itself does not block on metadata).
	// A closed account can't be frozen (ErrAccountClosed).
	Freeze(ctx context.Context, id AccountID, reason string) error

	// Unfreeze marks a frozen account active again. A closed account stays
	// closed (ErrAccountClosed).
	Unfreeze(ctx context.Context, id AccountID) error

	// Close moves any remaining balance to SweepTo and marks the account
	// closed. It refuses while anything is pending (INFLIGHT) or the balance is
	// negative. Closed accounts are rejected by every movement when
	// Config.EnforceAccountStatus is on. Closing a frozen account is allowed;
	// sweeping money out of an account whose ledger is frozen is not
	// (ErrLedgerFrozen).
	Close(ctx context.Context, id AccountID, in CloseInput) (*Account, error)

	// History lists an account's transactions, newest first.
	History(ctx context.Context, id AccountID, q HistoryQuery) (*HistoryPage, error)

	// BalanceAt returns the account's posted balance at t, e.g. a month-end
	// balance. It uses Blnk's historical balance endpoint recomputed from
	// transactions (Core 0.10.1+). Pending (INFLIGHT) amounts aren't included.
	BalanceAt(ctx context.Context, id AccountID, t time.Time) (Money, error)

	// Statement builds a statement from posted (APPLIED) entries. Opening comes
	// from Blnk's historical balance; the running balance is computed here and
	// checked against the historical balance at To, so a statement that doesn't
	// add up is an error rather than a wrong document.
	Statement(ctx context.Context, id AccountID, from, to time.Time) (*Statement, error)

	// LimitUsage reports usage against the account's limit (for "you can send X
	// more today" in your app).
	LimitUsage(ctx context.Context, id AccountID) (*LimitUsage, error)
}

// System: read system (GL) accounts such as "@FeeIncome" or
// "@PaystackClearing". They are created by Blnk the first time a movement uses
// them and change only through the ledger's own flows, so there is nothing to
// open, freeze or close.
type System interface {
	// Get returns a system account in one currency.
	Get(ctx context.Context, ref SystemRef) (*SystemAccount, error)

	// List lists every system account in currency.
	List(ctx context.Context, currency string) ([]*SystemAccount, error)

	// History lists a system account's transactions, newest first.
	History(ctx context.Context, ref SystemRef, q HistoryQuery) (*HistoryPage, error)

	// BalanceAt returns the system account's posted balance at t, as Blnk
	// stores it (debit-normal accounts run negative).
	BalanceAt(ctx context.Context, ref SystemRef, t time.Time) (Money, error)

	// Statement builds a statement for a system account, like Accounts().Statement.
	Statement(ctx context.Context, ref SystemRef, from, to time.Time) (*Statement, error)
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
	ListPots(ctx context.Context, parentID AccountID) ([]*Pot, error)
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
	Get(ctx context.Context, escrowID AccountID) (*Escrow, error)

	// Release pays escrowed money to the beneficiary.
	Release(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error)

	// Refund returns escrowed money to the payer.
	Refund(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error)
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

	// AllocateSuspense moves parked money to its owner once known: @Suspense →
	// customer account. It can't allocate more than suspense holds.
	AllocateSuspense(ctx context.Context, in AllocateInput) (*Receipt, error)

	// ReturnSuspense sends parked money back where it came from: @Suspense →
	// the provider's clearing account, e.g. when the provider reverses it.
	ReturnSuspense(ctx context.Context, in ReturnSuspenseInput) (*Receipt, error)

	// Adjust posts a correction between a customer account and @Adjustments, recording
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

// The namespace implementations share the Service they wrap.
type (
	ledgersAPI         struct{ *Service }
	systemAPI          struct{ *Service }
	accountsAPI        struct{ *Service }
	customersAPI       struct{ *Service }
	paymentsAPI        struct{ *Service }
	pendingAPI         struct{ *Service }
	transactionsAPI    struct{ *Service }
	disputesAPI        struct{ *Service }
	escrowsAPI         struct{ *Service }
	operationsAPI      struct{ *Service }
	reconciliationsAPI struct{ *Service }
)

// Ledgers returns the Ledgers namespace.
func (s *Service) Ledgers() Ledgers { return ledgersAPI{s} }

// System returns the System namespace.
func (s *Service) System() System { return systemAPI{s} }

// Accounts returns the Accounts namespace.
func (s *Service) Accounts() Accounts { return accountsAPI{s} }

// Customers returns the Customers namespace.
func (s *Service) Customers() Customers { return customersAPI{s} }

// Payments returns the Payments namespace.
func (s *Service) Payments() Payments { return paymentsAPI{s} }

// Pending returns the Pending namespace.
func (s *Service) Pending() Pending { return pendingAPI{s} }

// Transactions returns the Transactions namespace.
func (s *Service) Transactions() Transactions { return transactionsAPI{s} }

// Disputes returns the Disputes namespace.
func (s *Service) Disputes() Disputes { return disputesAPI{s} }

// Escrows returns the Escrows namespace.
func (s *Service) Escrows() Escrows { return escrowsAPI{s} }

// Operations returns the Operations namespace.
func (s *Service) Operations() Operations { return operationsAPI{s} }

// Reconciliations returns the Reconciliations namespace.
func (s *Service) Reconciliations() Reconciliations { return reconciliationsAPI{s} }

var _ Ledger = (*Service)(nil)
