// Instrument mirrors every Ledger method; "var _ Ledger" below makes the compiler keep it complete.

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

// Instrument wraps l so o observes every call.
func Instrument(l Ledger, o Observer) Ledger { return &instrumented{l: l, o: o} }

type instrumented struct {
	l Ledger
	o Observer
}

var _ Ledger = (*instrumented)(nil)

func (i *instrumented) OpenAccount(ctx context.Context, in OpenAccountInput) (*Account, error) {
	ctx, done := i.o.Start(ctx, "OpenAccount")
	res, err := i.l.OpenAccount(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	ctx, done := i.o.Start(ctx, "GetAccount")
	res, err := i.l.GetAccount(ctx, accountID)
	done(err)
	return res, err
}

func (i *instrumented) GetSystemAccount(ctx context.Context, indicator, currency string) (*Account, error) {
	ctx, done := i.o.Start(ctx, "GetSystemAccount")
	res, err := i.l.GetSystemAccount(ctx, indicator, currency)
	done(err)
	return res, err
}

func (i *instrumented) FindAccounts(ctx context.Context, q AccountQuery) (*AccountPage, error) {
	ctx, done := i.o.Start(ctx, "FindAccounts")
	res, err := i.l.FindAccounts(ctx, q)
	done(err)
	return res, err
}

func (i *instrumented) FreezeAccount(ctx context.Context, accountID, reason string) error {
	ctx, done := i.o.Start(ctx, "FreezeAccount")
	err := i.l.FreezeAccount(ctx, accountID, reason)
	done(err)
	return err
}

func (i *instrumented) UnfreezeAccount(ctx context.Context, accountID string) error {
	ctx, done := i.o.Start(ctx, "UnfreezeAccount")
	err := i.l.UnfreezeAccount(ctx, accountID)
	done(err)
	return err
}

func (i *instrumented) CloseAccount(ctx context.Context, accountID string, in CloseInput) (*Account, error) {
	ctx, done := i.o.Start(ctx, "CloseAccount")
	res, err := i.l.CloseAccount(ctx, accountID, in)
	done(err)
	return res, err
}

func (i *instrumented) OpenCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	ctx, done := i.o.Start(ctx, "OpenCustomer")
	res, err := i.l.OpenCustomer(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) GetCustomer(ctx context.Context, identityID string) (*Customer, error) {
	ctx, done := i.o.Start(ctx, "GetCustomer")
	res, err := i.l.GetCustomer(ctx, identityID)
	done(err)
	return res, err
}

func (i *instrumented) OpenPot(ctx context.Context, in PotInput) (*Pot, error) {
	ctx, done := i.o.Start(ctx, "OpenPot")
	res, err := i.l.OpenPot(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) ListPots(ctx context.Context, parentID string) ([]*Pot, error) {
	ctx, done := i.o.Start(ctx, "ListPots")
	res, err := i.l.ListPots(ctx, parentID)
	done(err)
	return res, err
}

func (i *instrumented) Deposit(ctx context.Context, in DepositInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Deposit")
	res, err := i.l.Deposit(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Withdraw")
	res, err := i.l.Withdraw(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Transfer(ctx context.Context, in TransferInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Transfer")
	res, err := i.l.Transfer(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Split(ctx context.Context, in SplitInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Split")
	res, err := i.l.Split(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Exchange(ctx context.Context, in ExchangeInput) (*ExchangeReceipt, error) {
	ctx, done := i.o.Start(ctx, "Exchange")
	res, err := i.l.Exchange(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Hold(ctx context.Context, in HoldInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Hold")
	res, err := i.l.Hold(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) BulkTransfer(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error) {
	ctx, done := i.o.Start(ctx, "BulkTransfer")
	res, err := i.l.BulkTransfer(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) ScheduleRecurring(ctx context.Context, in RecurringInput) ([]*Receipt, error) {
	ctx, done := i.o.Start(ctx, "ScheduleRecurring")
	res, err := i.l.ScheduleRecurring(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Confirm")
	res, err := i.l.Confirm(ctx, txID, partial)
	done(err)
	return res, err
}

func (i *instrumented) Cancel(ctx context.Context, txID string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Cancel")
	res, err := i.l.Cancel(ctx, txID)
	done(err)
	return res, err
}

func (i *instrumented) ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error) {
	ctx, done := i.o.Start(ctx, "ConfirmMany")
	res, err := i.l.ConfirmMany(ctx, items)
	done(err)
	return res, err
}

func (i *instrumented) CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error) {
	ctx, done := i.o.Start(ctx, "CancelMany")
	res, err := i.l.CancelMany(ctx, txIDs)
	done(err)
	return res, err
}

func (i *instrumented) Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Refund")
	res, err := i.l.Refund(ctx, txID, in)
	done(err)
	return res, err
}

func (i *instrumented) RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "RefundPartial")
	res, err := i.l.RefundPartial(ctx, txID, in)
	done(err)
	return res, err
}

func (i *instrumented) OpenDispute(ctx context.Context, in DisputeInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "OpenDispute")
	res, err := i.l.OpenDispute(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) ResolveDispute(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "ResolveDispute")
	res, err := i.l.ResolveDispute(ctx, disputeID, outcome, partial)
	done(err)
	return res, err
}

func (i *instrumented) OpenEscrow(ctx context.Context, in EscrowInput) (*Escrow, error) {
	ctx, done := i.o.Start(ctx, "OpenEscrow")
	res, err := i.l.OpenEscrow(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) GetEscrow(ctx context.Context, escrowID string) (*Escrow, error) {
	ctx, done := i.o.Start(ctx, "GetEscrow")
	res, err := i.l.GetEscrow(ctx, escrowID)
	done(err)
	return res, err
}

func (i *instrumented) ReleaseEscrow(ctx context.Context, escrowID string, in EscrowSettleInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "ReleaseEscrow")
	res, err := i.l.ReleaseEscrow(ctx, escrowID, in)
	done(err)
	return res, err
}

func (i *instrumented) RefundEscrow(ctx context.Context, escrowID string, in EscrowSettleInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "RefundEscrow")
	res, err := i.l.RefundEscrow(ctx, escrowID, in)
	done(err)
	return res, err
}

func (i *instrumented) QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "QuoteDeposit")
	res, err := i.l.QuoteDeposit(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "QuoteWithdraw")
	res, err := i.l.QuoteWithdraw(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "QuoteTransfer")
	res, err := i.l.QuoteTransfer(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) LimitUsage(ctx context.Context, accountID string) (*LimitUsage, error) {
	ctx, done := i.o.Start(ctx, "LimitUsage")
	res, err := i.l.LimitUsage(ctx, accountID)
	done(err)
	return res, err
}

func (i *instrumented) GetTransaction(ctx context.Context, txID string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "GetTransaction")
	res, err := i.l.GetTransaction(ctx, txID)
	done(err)
	return res, err
}

func (i *instrumented) GetTransactionByReference(ctx context.Context, reference string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "GetTransactionByReference")
	res, err := i.l.GetTransactionByReference(ctx, reference)
	done(err)
	return res, err
}

func (i *instrumented) History(ctx context.Context, accountID string, q HistoryQuery) (*HistoryPage, error) {
	ctx, done := i.o.Start(ctx, "History")
	res, err := i.l.History(ctx, accountID, q)
	done(err)
	return res, err
}

func (i *instrumented) BalanceAt(ctx context.Context, accountID string, t time.Time) (Money, error) {
	ctx, done := i.o.Start(ctx, "BalanceAt")
	res, err := i.l.BalanceAt(ctx, accountID, t)
	done(err)
	return res, err
}

func (i *instrumented) Statement(ctx context.Context, accountID string, from, to time.Time) (*Statement, error) {
	ctx, done := i.o.Start(ctx, "Statement")
	res, err := i.l.Statement(ctx, accountID, from, to)
	done(err)
	return res, err
}

func (i *instrumented) RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error) {
	ctx, done := i.o.Start(ctx, "RecordSettlement")
	res, err := i.l.RecordSettlement(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) ParkInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "ParkInSuspense")
	res, err := i.l.ParkInSuspense(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) AllocateSuspense(ctx context.Context, in AllocateInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "AllocateSuspense")
	res, err := i.l.AllocateSuspense(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Adjust(ctx context.Context, in AdjustInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Adjust")
	res, err := i.l.Adjust(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) TrialBalance(ctx context.Context, currency string) (*TrialBalance, error) {
	ctx, done := i.o.Start(ctx, "TrialBalance")
	res, err := i.l.TrialBalance(ctx, currency)
	done(err)
	return res, err
}

func (i *instrumented) WatchBalance(ctx context.Context, in WatchInput) (*Watch, error) {
	ctx, done := i.o.Start(ctx, "WatchBalance")
	res, err := i.l.WatchBalance(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) Unwatch(ctx context.Context, watchID string) error {
	ctx, done := i.o.Start(ctx, "Unwatch")
	err := i.l.Unwatch(ctx, watchID)
	done(err)
	return err
}

func (i *instrumented) CreateMatchingRule(ctx context.Context, r MatchingRule) (string, error) {
	ctx, done := i.o.Start(ctx, "CreateMatchingRule")
	res, err := i.l.CreateMatchingRule(ctx, r)
	done(err)
	return res, err
}

func (i *instrumented) Reconcile(ctx context.Context, in ReconcileInput) (*Reconciliation, error) {
	ctx, done := i.o.Start(ctx, "Reconcile")
	res, err := i.l.Reconcile(ctx, in)
	done(err)
	return res, err
}

func (i *instrumented) GetReconciliation(ctx context.Context, id string) (*Reconciliation, error) {
	ctx, done := i.o.Start(ctx, "GetReconciliation")
	res, err := i.l.GetReconciliation(ctx, id)
	done(err)
	return res, err
}
