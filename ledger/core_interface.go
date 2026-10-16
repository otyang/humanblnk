package ledger

import (
	"context"
	"time"
)

// Ledger is the domain interface your application depends on. It has one
// handle per domain:
//
//	wallet, ops := l.Wallet(), l.Ops()
//	wallet.Deposit(ctx, …)          // what customers do      (wallet_*.go)
//	ops.RecordSettlement(ctx, …)    // what the business does (ops_*.go)
//
// Hand each part of your application only the handle it needs: a
// customer-facing server gets a Wallet, so it can't freeze or close accounts,
// adjust balances or record settlements. Service implements Ledger on Blnk;
// Instrument wraps any Ledger with an Observer.
//
// Ledgers, customer accounts and house ("@") accounts are separate kinds of
// object. Each takes its own id type (LedgerID, AccountID, HouseRef), and a
// method refuses ids of another kind with ErrWrongKind.
type Ledger interface {
	Wallet() Wallet
	Ops() Ops
}

// Wallet is the wallet domain: what customers do. Customers and their
// accounts, money in and out, holds, quotes, refunds, escrow and disputes.
// Every account it takes is a customer account (or a pot or escrow under one);
// house accounts are read through Ops.
type Wallet interface {
	// ---- Customers and savings pots (wallet_customers.go)

	// OpenCustomer creates the identity and opens its accounts.
	OpenCustomer(ctx context.Context, in CustomerInput) (*Customer, error)

	// GetCustomer returns an identity and its customer accounts, by currency.
	// Pots and escrows are left out; list them with ListPots / FindAccounts.
	GetCustomer(ctx context.Context, identityID string) (*Customer, error)

	// OpenPot opens a pot in the parent's currency, linked to the parent and its
	// identity. Opening the same name twice returns the existing pot.
	OpenPot(ctx context.Context, in PotInput) (*Pot, error)

	// ListPots lists the pots under a parent account.
	ListPots(ctx context.Context, parentID AccountID) ([]*Pot, error)

	// ---- Customer accounts (wallet_accounts.go)

	// OpenAccount creates a customer account (a Blnk balance) in Config.LedgerID or
	// in.LedgerID, marked active. The ledger must exist (ErrLedgerNotFound
	// otherwise). With ExternalID it is safe to retry.
	OpenAccount(ctx context.Context, in OpenAccountInput) (*Account, error)

	// GetAccount returns an account with its balance, available balance (balance minus
	// pending debits) and pending amounts.
	GetAccount(ctx context.Context, id AccountID) (*Account, error)

	// FindAccounts lists accounts matching q, oldest first. House accounts are never
	// listed.
	FindAccounts(ctx context.Context, q AccountQuery) (*AccountPage, error)

	// History lists an account's transactions, newest first.
	History(ctx context.Context, id AccountID, q HistoryQuery) (*HistoryPage, error)

	// ---- Statements (wallet_statements.go)

	// Statement builds a statement from posted (APPLIED) entries. Opening comes
	// from Blnk's historical balance; the running balance is computed here and
	// checked against the historical balance at To, so a statement that doesn't
	// add up is an error rather than a wrong document.
	Statement(ctx context.Context, id AccountID, from, to time.Time) (*Statement, error)

	// BalanceAt returns the account's posted balance at t, e.g. a month-end
	// balance. It uses Blnk's historical balance endpoint recomputed from
	// transactions (Core 0.10.1+). Pending (INFLIGHT) amounts aren't included.
	BalanceAt(ctx context.Context, id AccountID, t time.Time) (Money, error)

	// ---- Limits (wallet_limits.go)

	// LimitUsage reports usage against the account's limit (for "you can send X
	// more today" in your app). Only movements limits apply to count (transfers,
	// withdrawals, holds, splits, exchanges, escrow, and bulk items), each on
	// the day it took effect; what's left never goes below zero.
	LimitUsage(ctx context.Context, id AccountID) (*LimitUsage, error)

	// ---- Moving money (wallet_payments.go)

	// Deposit records money received from outside: the provider's clearing
	// account (e.g. @PaystackClearing) is debited and the account credited, less
	// the fee, which goes to the fee income account. Pending holds it INFLIGHT
	// until Wallet().Confirm.
	Deposit(ctx context.Context, in DepositInput) (*Receipt, error)

	// Withdraw sends money out: the account is debited Amount + Fee, the
	// provider's clearing account credited Amount. It is held INFLIGHT until
	// Wallet().Confirm (provider success) or Cancel (failure), and auto-cancels
	// after Config.WithdrawalHoldTTL; Immediate posts it at once.
	Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error)

	// Transfer moves money between accounts. The sender pays Amount + Fee; the
	// recipient gets Amount. Pending holds it INFLIGHT; ScheduledFor posts it later.
	Transfer(ctx context.Context, in TransferInput) (*Receipt, error)

	// Bulk posts up to 10,000 transfers as one Blnk batch, e.g. payroll.
	// Atomic makes them all-or-nothing; Pending holds them all INFLIGHT; Async
	// processes them in the background with the outcome sent by webhook.
	Bulk(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error)

	// ---- Split payments (wallet_split.go)

	// Split records the payment as one Blnk transaction with one destination per
	// part. Every part is converted to an exact amount here and sent as
	// precise_distribution, so what each recipient gets is decided by this code,
	// not by rounding on the server: percentage parts round down, and the dust
	// goes to the Rest part (or, without one, to the first percentage part).
	Split(ctx context.Context, in SplitInput) (*Receipt, error)

	// ---- Currency exchange (wallet_fx.go)

	// Exchange debits From into the FX pool in the sell currency and credits To out
	// of the pool in the buy currency, as one atomic batch: both legs post or
	// neither does. Each currency's pool balance is the house's open position.
	Exchange(ctx context.Context, in ExchangeInput) (*ExchangeReceipt, error)

	// ---- Standing orders (wallet_recurring.go)

	// Schedule creates every occurrence up front as a Blnk scheduled
	// transaction (scheduled_for), e.g. a monthly savings plan or subscription.
	// Blnk has no native recurrence, so a plan is a set of scheduled transfers
	// sharing meta_data.recurring_reference. Calling it again with the same input
	// is safe: existing occurrences come back as replays.
	//
	// Monthly dates follow time.AddDate, so a plan starting on the 31st lands on
	// the 1st of shorter months; start on the 28th or earlier to avoid that.
	Schedule(ctx context.Context, in RecurringInput) ([]*Receipt, error)

	// ---- Quotes (wallet_quote.go)

	// QuoteDeposit previews Deposit (fee and net credited).
	QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error)

	// QuoteWithdraw previews Withdraw.
	QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error)

	// QuoteTransfer previews Transfer.
	QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error)

	// QuoteExchange previews Exchange: amounts, fee, the balance after, and
	// whether it would go through (Blnk dry-runs both legs).
	QuoteExchange(ctx context.Context, in ExchangeInput) (*ExchangeQuote, error)

	// ---- Holds and pending money (wallet_pending.go)

	// Hold reserves money on an account without moving it, like a card
	// authorisation. Wallet().Confirm captures all or part of it; Cancel releases
	// it. ExpiresAt releases it automatically, CaptureAt captures it.
	Hold(ctx context.Context, in HoldInput) (*Receipt, error)

	// Confirm settles an INFLIGHT transaction. With partial set, only that amount is
	// settled and the rest stays held (call Confirm again or Cancel the remainder).
	// A transaction with several legs (one with a fee) can only be confirmed in
	// full, or one leg at a time by the leg's transaction id. A customer account
	// that would receive the money must not be frozen or closed
	// (ErrAccountFrozen, ErrAccountClosed); Cancel it instead.
	Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error)

	// Cancel releases whatever is still held on an INFLIGHT transaction.
	Cancel(ctx context.Context, txID string) (*Receipt, error)

	// ConfirmMany confirms up to 100 independent pending transactions in one call.
	// Each succeeds or fails on its own; check every BatchResult.
	ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error)

	// CancelMany cancels up to 100 independent pending transactions in one call.
	CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error)

	// ---- Transactions and refunds (wallet_transactions.go)

	// GetTransaction returns a transaction by its Blnk id.
	GetTransaction(ctx context.Context, txID string) (*Receipt, error)

	// GetTransactionByReference tries the reference as given, then with Blnk's "_q"
	// suffix used for records produced by the queue.
	GetTransactionByReference(ctx context.Context, reference string) (*Receipt, error)

	// Refund reverses a settled transaction in full, once. A payment with a fee,
	// or a split, is reversed leg by leg (fee included): the receipt's Amount is
	// the total and Legs lists each reversal, and if one leg fails, calling
	// Refund again finishes the rest. Each account that received money must
	// still hold it, checked just before reversing: otherwise the refund is
	// refused with ErrInsufficientFunds and nothing is reversed. For a pending
	// payment that was confirmed, pass the id Confirm returned; if it carried a
	// fee, Confirm returned one leg's id, so reverse each leg by its own id or
	// use RefundPartial.
	// A transaction that already has partial refunds (RefundPartial) is
	// refused; refund the rest with RefundPartial. For split transactions
	// created through the queue, pass the queued parent id.
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

	// ---- Escrow (wallet_escrow.go)

	// OpenEscrow opens an escrow account and moves Amount into it from the payer.
	// Status, limits and the payer's available balance are checked before the
	// account is opened, and a retry with the same Reference reuses it.
	OpenEscrow(ctx context.Context, in EscrowInput) (*Escrow, error)

	// GetEscrow returns an escrow and how much of it is still held.
	GetEscrow(ctx context.Context, escrowID AccountID) (*Escrow, error)

	// ReleaseEscrow pays escrowed money to the beneficiary.
	ReleaseEscrow(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error)

	// RefundEscrow returns escrowed money to the payer.
	RefundEscrow(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error)

	// ---- Disputes (wallet_dispute.go)

	// OpenDispute puts the disputed amount on hold in the recipient's account,
	// pointed back at the original payer, so it can't be spent while the dispute
	// is open. The hold has no expiry. Close it with Wallet().ResolveDispute.
	OpenDispute(ctx context.Context, in DisputeInput) (*Receipt, error)

	// ResolveDispute closes a dispute opened with Wallet().OpenDispute. With DisputeUpheld,
	// partial refunds only part of the held amount; the rest stays held until you
	// resolve again or reject.
	ResolveDispute(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error)
}

// Ops is the financial-operations domain: what the business does. Ledgers,
// house accounts, freezing and closing, settlement with providers,
// unidentified money, approved corrections, reports, balance alerts and
// reconciliation.
type Ops interface {
	// ---- Ledgers (ops_ledgers.go)

	// CreateLedger creates a ledger, or returns the one already created with the same
	// Key (Replayed set), so it never makes two. The same Key with a different
	// Name is refused with ErrDuplicateReference. Create ledgers when you
	// provision, not on the request path.
	CreateLedger(ctx context.Context, in CreateLedgerInput) (*LedgerInfo, error)

	// GetLedger returns a ledger. A ledger that doesn't exist is ErrLedgerNotFound.
	GetLedger(ctx context.Context, id LedgerID) (*LedgerInfo, error)

	// ListLedgers lists ledgers, oldest first.
	ListLedgers(ctx context.Context, q LedgerQuery) (*LedgerPage, error)

	// ---- Controls: freeze and close (ops_controls.go)

	// FreezeAccount marks the account frozen: every movement touching it is refused
	// (ErrAccountFrozen), and a pending transaction can't be confirmed into it;
	// one can still be cancelled. Movements Blnk posts by itself (scheduled
	// transfers, auto-capture, hold expiry) aren't checked. A closed account
	// can't be frozen (ErrAccountClosed).
	FreezeAccount(ctx context.Context, id AccountID, reason string) error

	// UnfreezeAccount marks a frozen account active again. A closed account stays
	// closed (ErrAccountClosed).
	UnfreezeAccount(ctx context.Context, id AccountID) error

	// CloseAccount moves any remaining balance to SweepTo and marks the account
	// closed, for good; it needs a Reason, a RequestedBy and an ApprovedBy who
	// is someone else (ErrApprovalRequired otherwise). It refuses while anything is pending (INFLIGHT) or scheduled,
	// while a pot under it is open or an escrow on it still holds money, and
	// when the balance is negative. Every movement touching a closed account
	// is refused (ErrAccountClosed). Closing a frozen account is allowed;
	// sweeping money out of an account whose ledger is frozen is not
	// (ErrLedgerFrozen).
	CloseAccount(ctx context.Context, id AccountID, in CloseInput) (*Account, error)

	// FreezeLedger marks the ledger frozen: no money can leave any of its customer
	// accounts (ErrLedgerFrozen). Money can still come in, and house accounts
	// are never blocked.
	FreezeLedger(ctx context.Context, id LedgerID, reason string) error

	// UnfreezeLedger marks a frozen ledger active again.
	UnfreezeLedger(ctx context.Context, id LedgerID) error

	// ---- House accounts (ops_house.go)

	// GetHouseAccount returns a house account in one currency.
	GetHouseAccount(ctx context.Context, ref HouseRef) (*HouseAccount, error)

	// ListHouseAccounts lists every house account in currency.
	ListHouseAccounts(ctx context.Context, currency string) ([]*HouseAccount, error)

	// HouseHistory lists a house account's transactions, newest first.
	HouseHistory(ctx context.Context, ref HouseRef, q HistoryQuery) (*HistoryPage, error)

	// HouseBalanceAt returns the house account's posted balance at t, as Blnk
	// stores it (debit-normal accounts run negative).
	HouseBalanceAt(ctx context.Context, ref HouseRef, t time.Time) (Money, error)

	// HouseStatement builds a statement for a house account, like Wallet().Statement.
	HouseStatement(ctx context.Context, ref HouseRef, from, to time.Time) (*Statement, error)

	// ---- Settlement (ops_settlement.go)

	// RecordSettlement clears a provider clearing account against your bank, so
	// the clearing balance always equals what is still outstanding with that
	// provider and can be reconciled with its settlement report.
	//
	//	Collection: Dr Bank (Amount−Fee), Dr ProviderFees (Fee) / Cr Clearing (Amount)
	//	Payout:     Dr Clearing (Amount), Dr ProviderFees (Fee) / Cr Bank (Amount+Fee)
	//
	// The entries post atomically. ClearingAccount, BankAccount and FeeAccount
	// must be house accounts ("@Name"); anything else is ErrWrongKind.
	RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error)

	// ---- Suspense (ops_suspense.go)

	// HoldInSuspense records unidentified incoming money: clearing account → @Suspense.
	// The clearing account must be a house account ("@Name"); anything else is
	// ErrWrongKind.
	HoldInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error)

	// ClaimFromSuspense moves held money to its owner once known: @Suspense →
	// customer account. It can't claim more than suspense holds.
	ClaimFromSuspense(ctx context.Context, in ClaimFromSuspenseInput) (*Receipt, error)

	// ReturnFromSuspense sends held money back where it came from: @Suspense →
	// the provider's clearing account, e.g. when the provider reverses it.
	ReturnFromSuspense(ctx context.Context, in ReturnFromSuspenseInput) (*Receipt, error)

	// ---- Adjustments (ops_adjust.go)

	// Adjust posts a correction between a customer account and @Adjustments, recording
	// reason, requester and approver in metadata so every correction is auditable.
	// RequestedBy and ApprovedBy are both required and must be different people
	// (ErrApprovalRequired otherwise).
	Adjust(ctx context.Context, in AdjustInput) (*Receipt, error)

	// ---- Reports (ops_reports.go)

	// TrialBalance reads every balance in currency (100 per request).
	TrialBalance(ctx context.Context, currency string) (*TrialBalance, error)

	// ---- Balance alerts (ops_watch.go)

	// WatchBalance creates a Blnk balance monitor. Alerts arrive through your
	// global webhook as EventBalanceAlert (see Service.ParseWebhook).
	WatchBalance(ctx context.Context, in WatchInput) (*Watch, error)

	// UnwatchBalance deletes a balance monitor.
	UnwatchBalance(ctx context.Context, watchID string) error

	// ---- Reconciliation (ops_reconcile.go)

	// CreateMatchingRule stores a rule in Blnk and returns its id.
	CreateMatchingRule(ctx context.Context, r MatchingRule) (string, error)

	// StartReconciliation starts a reconciliation. Poll Ops().GetReconciliation until Done.
	StartReconciliation(ctx context.Context, in ReconcileInput) (*Reconciliation, error)

	// GetReconciliation returns a run's status and counts.
	GetReconciliation(ctx context.Context, id string) (*Reconciliation, error)
}

type (
	walletAPI struct{ *Service }
	opsAPI    struct{ *Service }
)

// Wallet returns the wallet domain.
func (s *Service) Wallet() Wallet { return walletAPI{s} }

// Ops returns the financial-operations domain.
func (s *Service) Ops() Ops { return opsAPI{s} }

var _ Ledger = (*Service)(nil)
