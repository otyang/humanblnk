// Instrument mirrors every Ledger namespace; the "var _" checks below make the compiler keep it complete.

package ledger

import (
	"context"
	"log/slog"
	"time"
)

// Observer is told about every Ledger call: Start runs before it and returns
// the context to use (e.g. carrying a trace span) and a function that is
// called with the call's error when it returns. Use it for logs, metrics and
// tracing (OpenTelemetry: start a span in Start, end it in the returned func).
type Observer interface {
	Start(ctx context.Context, op string) (context.Context, func(err error))
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(ctx context.Context, op string) (context.Context, func(err error))

func (f ObserverFunc) Start(ctx context.Context, op string) (context.Context, func(err error)) {
	return f(ctx, op)
}

// LogObserver logs every call with its duration: Info on success, Warn for
// business refusals (insufficient funds, limits, frozen accounts), Error
// otherwise.
func LogObserver(log *slog.Logger) Observer {
	return ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
		start := time.Now()
		return ctx, func(err error) {
			attrs := []any{"op", op, "duration", time.Since(start)}
			switch {
			case err == nil:
				log.InfoContext(ctx, "ledger call", attrs...)
			case refusal(err):
				log.WarnContext(ctx, "ledger call refused", append(attrs, "error", err)...)
			default:
				log.ErrorContext(ctx, "ledger call failed", append(attrs, "error", err)...)
			}
		}
	})
}

// Instrument wraps l so o observes every call. Calls are named
// "Namespace.Method", e.g. "Payments.Transfer".
func Instrument(l Ledger, o Observer) Ledger { return &instrumented{l: l, o: o} }

type instrumented struct {
	l Ledger
	o Observer
}

var _ Ledger = (*instrumented)(nil)

func (i *instrumented) Ledgers() Ledgers           { return obsLedgers{i.l.Ledgers(), i.o} }
func (i *instrumented) Accounts() Accounts         { return obsAccounts{i.l.Accounts(), i.o} }
func (i *instrumented) System() System             { return obsSystem{i.l.System(), i.o} }
func (i *instrumented) Customers() Customers       { return obsCustomers{i.l.Customers(), i.o} }
func (i *instrumented) Payments() Payments         { return obsPayments{i.l.Payments(), i.o} }
func (i *instrumented) Pending() Pending           { return obsPending{i.l.Pending(), i.o} }
func (i *instrumented) Transactions() Transactions { return obsTransactions{i.l.Transactions(), i.o} }
func (i *instrumented) Disputes() Disputes         { return obsDisputes{i.l.Disputes(), i.o} }
func (i *instrumented) Escrows() Escrows           { return obsEscrows{i.l.Escrows(), i.o} }
func (i *instrumented) Operations() Operations     { return obsOperations{i.l.Operations(), i.o} }
func (i *instrumented) Reconciliations() Reconciliations {
	return obsReconciliations{i.l.Reconciliations(), i.o}
}

type obsLedgers struct {
	n Ledgers
	o Observer
}

var _ Ledgers = obsLedgers{}

func (i obsLedgers) Create(ctx context.Context, in CreateLedgerInput) (*LedgerInfo, error) {
	ctx, done := i.o.Start(ctx, "Ledgers.Create")
	res, err := i.n.Create(ctx, in)
	done(err)
	return res, err
}

func (i obsLedgers) Get(ctx context.Context, id LedgerID) (*LedgerInfo, error) {
	ctx, done := i.o.Start(ctx, "Ledgers.Get")
	res, err := i.n.Get(ctx, id)
	done(err)
	return res, err
}

func (i obsLedgers) List(ctx context.Context, q LedgerQuery) (*LedgerPage, error) {
	ctx, done := i.o.Start(ctx, "Ledgers.List")
	res, err := i.n.List(ctx, q)
	done(err)
	return res, err
}

func (i obsLedgers) Freeze(ctx context.Context, id LedgerID, reason string) error {
	ctx, done := i.o.Start(ctx, "Ledgers.Freeze")
	err := i.n.Freeze(ctx, id, reason)
	done(err)
	return err
}

func (i obsLedgers) Unfreeze(ctx context.Context, id LedgerID) error {
	ctx, done := i.o.Start(ctx, "Ledgers.Unfreeze")
	err := i.n.Unfreeze(ctx, id)
	done(err)
	return err
}

type obsSystem struct {
	n System
	o Observer
}

var _ System = obsSystem{}

func (i obsSystem) Get(ctx context.Context, ref SystemRef) (*SystemAccount, error) {
	ctx, done := i.o.Start(ctx, "System.Get")
	res, err := i.n.Get(ctx, ref)
	done(err)
	return res, err
}

func (i obsSystem) List(ctx context.Context, currency string) ([]*SystemAccount, error) {
	ctx, done := i.o.Start(ctx, "System.List")
	res, err := i.n.List(ctx, currency)
	done(err)
	return res, err
}

func (i obsSystem) History(ctx context.Context, ref SystemRef, q HistoryQuery) (*HistoryPage, error) {
	ctx, done := i.o.Start(ctx, "System.History")
	res, err := i.n.History(ctx, ref, q)
	done(err)
	return res, err
}

func (i obsSystem) BalanceAt(ctx context.Context, ref SystemRef, t time.Time) (Money, error) {
	ctx, done := i.o.Start(ctx, "System.BalanceAt")
	res, err := i.n.BalanceAt(ctx, ref, t)
	done(err)
	return res, err
}

func (i obsSystem) Statement(ctx context.Context, ref SystemRef, from, to time.Time) (*Statement, error) {
	ctx, done := i.o.Start(ctx, "System.Statement")
	res, err := i.n.Statement(ctx, ref, from, to)
	done(err)
	return res, err
}

type obsAccounts struct {
	n Accounts
	o Observer
}

var _ Accounts = obsAccounts{}

func (i obsAccounts) Open(ctx context.Context, in OpenAccountInput) (*Account, error) {
	ctx, done := i.o.Start(ctx, "Accounts.Open")
	res, err := i.n.Open(ctx, in)
	done(err)
	return res, err
}

func (i obsAccounts) Get(ctx context.Context, accountID AccountID) (*Account, error) {
	ctx, done := i.o.Start(ctx, "Accounts.Get")
	res, err := i.n.Get(ctx, accountID)
	done(err)
	return res, err
}

func (i obsAccounts) Find(ctx context.Context, q AccountQuery) (*AccountPage, error) {
	ctx, done := i.o.Start(ctx, "Accounts.Find")
	res, err := i.n.Find(ctx, q)
	done(err)
	return res, err
}

func (i obsAccounts) Freeze(ctx context.Context, accountID AccountID, reason string) error {
	ctx, done := i.o.Start(ctx, "Accounts.Freeze")
	err := i.n.Freeze(ctx, accountID, reason)
	done(err)
	return err
}

func (i obsAccounts) Unfreeze(ctx context.Context, accountID AccountID) error {
	ctx, done := i.o.Start(ctx, "Accounts.Unfreeze")
	err := i.n.Unfreeze(ctx, accountID)
	done(err)
	return err
}

func (i obsAccounts) Close(ctx context.Context, accountID AccountID, in CloseInput) (*Account, error) {
	ctx, done := i.o.Start(ctx, "Accounts.Close")
	res, err := i.n.Close(ctx, accountID, in)
	done(err)
	return res, err
}

func (i obsAccounts) History(ctx context.Context, accountID AccountID, q HistoryQuery) (*HistoryPage, error) {
	ctx, done := i.o.Start(ctx, "Accounts.History")
	res, err := i.n.History(ctx, accountID, q)
	done(err)
	return res, err
}

func (i obsAccounts) BalanceAt(ctx context.Context, accountID AccountID, t time.Time) (Money, error) {
	ctx, done := i.o.Start(ctx, "Accounts.BalanceAt")
	res, err := i.n.BalanceAt(ctx, accountID, t)
	done(err)
	return res, err
}

func (i obsAccounts) Statement(ctx context.Context, accountID AccountID, from, to time.Time) (*Statement, error) {
	ctx, done := i.o.Start(ctx, "Accounts.Statement")
	res, err := i.n.Statement(ctx, accountID, from, to)
	done(err)
	return res, err
}

func (i obsAccounts) LimitUsage(ctx context.Context, accountID AccountID) (*LimitUsage, error) {
	ctx, done := i.o.Start(ctx, "Accounts.LimitUsage")
	res, err := i.n.LimitUsage(ctx, accountID)
	done(err)
	return res, err
}

type obsCustomers struct {
	n Customers
	o Observer
}

var _ Customers = obsCustomers{}

func (i obsCustomers) Open(ctx context.Context, in CustomerInput) (*Customer, error) {
	ctx, done := i.o.Start(ctx, "Customers.Open")
	res, err := i.n.Open(ctx, in)
	done(err)
	return res, err
}

func (i obsCustomers) Get(ctx context.Context, identityID string) (*Customer, error) {
	ctx, done := i.o.Start(ctx, "Customers.Get")
	res, err := i.n.Get(ctx, identityID)
	done(err)
	return res, err
}

func (i obsCustomers) OpenPot(ctx context.Context, in PotInput) (*Pot, error) {
	ctx, done := i.o.Start(ctx, "Customers.OpenPot")
	res, err := i.n.OpenPot(ctx, in)
	done(err)
	return res, err
}

func (i obsCustomers) ListPots(ctx context.Context, parentID AccountID) ([]*Pot, error) {
	ctx, done := i.o.Start(ctx, "Customers.ListPots")
	res, err := i.n.ListPots(ctx, parentID)
	done(err)
	return res, err
}

type obsPayments struct {
	n Payments
	o Observer
}

var _ Payments = obsPayments{}

func (i obsPayments) Deposit(ctx context.Context, in DepositInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Deposit")
	res, err := i.n.Deposit(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Withdraw")
	res, err := i.n.Withdraw(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) Transfer(ctx context.Context, in TransferInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Transfer")
	res, err := i.n.Transfer(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) Split(ctx context.Context, in SplitInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Split")
	res, err := i.n.Split(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) Exchange(ctx context.Context, in ExchangeInput) (*ExchangeReceipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Exchange")
	res, err := i.n.Exchange(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) Bulk(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Bulk")
	res, err := i.n.Bulk(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) Schedule(ctx context.Context, in RecurringInput) ([]*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Payments.Schedule")
	res, err := i.n.Schedule(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "Payments.QuoteDeposit")
	res, err := i.n.QuoteDeposit(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "Payments.QuoteWithdraw")
	res, err := i.n.QuoteWithdraw(ctx, in)
	done(err)
	return res, err
}

func (i obsPayments) QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "Payments.QuoteTransfer")
	res, err := i.n.QuoteTransfer(ctx, in)
	done(err)
	return res, err
}

type obsPending struct {
	n Pending
	o Observer
}

var _ Pending = obsPending{}

func (i obsPending) Hold(ctx context.Context, in HoldInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Pending.Hold")
	res, err := i.n.Hold(ctx, in)
	done(err)
	return res, err
}

func (i obsPending) Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Pending.Confirm")
	res, err := i.n.Confirm(ctx, txID, partial)
	done(err)
	return res, err
}

func (i obsPending) Cancel(ctx context.Context, txID string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Pending.Cancel")
	res, err := i.n.Cancel(ctx, txID)
	done(err)
	return res, err
}

func (i obsPending) ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error) {
	ctx, done := i.o.Start(ctx, "Pending.ConfirmMany")
	res, err := i.n.ConfirmMany(ctx, items)
	done(err)
	return res, err
}

func (i obsPending) CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error) {
	ctx, done := i.o.Start(ctx, "Pending.CancelMany")
	res, err := i.n.CancelMany(ctx, txIDs)
	done(err)
	return res, err
}

type obsTransactions struct {
	n Transactions
	o Observer
}

var _ Transactions = obsTransactions{}

func (i obsTransactions) Get(ctx context.Context, txID string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Transactions.Get")
	res, err := i.n.Get(ctx, txID)
	done(err)
	return res, err
}

func (i obsTransactions) GetByReference(ctx context.Context, reference string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Transactions.GetByReference")
	res, err := i.n.GetByReference(ctx, reference)
	done(err)
	return res, err
}

func (i obsTransactions) Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Transactions.Refund")
	res, err := i.n.Refund(ctx, txID, in)
	done(err)
	return res, err
}

func (i obsTransactions) RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Transactions.RefundPartial")
	res, err := i.n.RefundPartial(ctx, txID, in)
	done(err)
	return res, err
}

type obsDisputes struct {
	n Disputes
	o Observer
}

var _ Disputes = obsDisputes{}

func (i obsDisputes) Open(ctx context.Context, in DisputeInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Disputes.Open")
	res, err := i.n.Open(ctx, in)
	done(err)
	return res, err
}

func (i obsDisputes) Resolve(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Disputes.Resolve")
	res, err := i.n.Resolve(ctx, disputeID, outcome, partial)
	done(err)
	return res, err
}

type obsEscrows struct {
	n Escrows
	o Observer
}

var _ Escrows = obsEscrows{}

func (i obsEscrows) Open(ctx context.Context, in EscrowInput) (*Escrow, error) {
	ctx, done := i.o.Start(ctx, "Escrows.Open")
	res, err := i.n.Open(ctx, in)
	done(err)
	return res, err
}

func (i obsEscrows) Get(ctx context.Context, escrowID AccountID) (*Escrow, error) {
	ctx, done := i.o.Start(ctx, "Escrows.Get")
	res, err := i.n.Get(ctx, escrowID)
	done(err)
	return res, err
}

func (i obsEscrows) Release(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Escrows.Release")
	res, err := i.n.Release(ctx, escrowID, in)
	done(err)
	return res, err
}

func (i obsEscrows) Refund(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Escrows.Refund")
	res, err := i.n.Refund(ctx, escrowID, in)
	done(err)
	return res, err
}

type obsOperations struct {
	n Operations
	o Observer
}

var _ Operations = obsOperations{}

func (i obsOperations) RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error) {
	ctx, done := i.o.Start(ctx, "Operations.RecordSettlement")
	res, err := i.n.RecordSettlement(ctx, in)
	done(err)
	return res, err
}

func (i obsOperations) ParkInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Operations.ParkInSuspense")
	res, err := i.n.ParkInSuspense(ctx, in)
	done(err)
	return res, err
}

func (i obsOperations) AllocateSuspense(ctx context.Context, in AllocateInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Operations.AllocateSuspense")
	res, err := i.n.AllocateSuspense(ctx, in)
	done(err)
	return res, err
}

func (i obsOperations) ReturnSuspense(ctx context.Context, in ReturnSuspenseInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Operations.ReturnSuspense")
	res, err := i.n.ReturnSuspense(ctx, in)
	done(err)
	return res, err
}

func (i obsOperations) Adjust(ctx context.Context, in AdjustInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Operations.Adjust")
	res, err := i.n.Adjust(ctx, in)
	done(err)
	return res, err
}

func (i obsOperations) TrialBalance(ctx context.Context, currency string) (*TrialBalance, error) {
	ctx, done := i.o.Start(ctx, "Operations.TrialBalance")
	res, err := i.n.TrialBalance(ctx, currency)
	done(err)
	return res, err
}

func (i obsOperations) WatchBalance(ctx context.Context, in WatchInput) (*Watch, error) {
	ctx, done := i.o.Start(ctx, "Operations.WatchBalance")
	res, err := i.n.WatchBalance(ctx, in)
	done(err)
	return res, err
}

func (i obsOperations) Unwatch(ctx context.Context, watchID string) error {
	ctx, done := i.o.Start(ctx, "Operations.Unwatch")
	err := i.n.Unwatch(ctx, watchID)
	done(err)
	return err
}

type obsReconciliations struct {
	n Reconciliations
	o Observer
}

var _ Reconciliations = obsReconciliations{}

func (i obsReconciliations) CreateRule(ctx context.Context, r MatchingRule) (string, error) {
	ctx, done := i.o.Start(ctx, "Reconciliations.CreateRule")
	res, err := i.n.CreateRule(ctx, r)
	done(err)
	return res, err
}

func (i obsReconciliations) Start(ctx context.Context, in ReconcileInput) (*Reconciliation, error) {
	ctx, done := i.o.Start(ctx, "Reconciliations.Start")
	res, err := i.n.Start(ctx, in)
	done(err)
	return res, err
}

func (i obsReconciliations) Get(ctx context.Context, id string) (*Reconciliation, error) {
	ctx, done := i.o.Start(ctx, "Reconciliations.Get")
	res, err := i.n.Get(ctx, id)
	done(err)
	return res, err
}
