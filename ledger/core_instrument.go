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
// "Handle.Method", e.g. "Wallet.Transfer" or "Ops.CloseAccount".
func Instrument(l Ledger, o Observer) Ledger { return &instrumented{l: l, o: o} }

type instrumented struct {
	l Ledger
	o Observer
}

var _ Ledger = (*instrumented)(nil)

func (i *instrumented) Wallet() Wallet { return obsWallet{i.l.Wallet(), i.o} }
func (i *instrumented) Ops() Ops       { return obsOps{i.l.Ops(), i.o} }

type obsWallet struct {
	n Wallet
	o Observer
}

var _ Wallet = obsWallet{}

func (i obsWallet) OpenCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	ctx, done := i.o.Start(ctx, "Wallet.OpenCustomer")
	res, err := i.n.OpenCustomer(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) GetCustomer(ctx context.Context, identityID string) (*Customer, error) {
	ctx, done := i.o.Start(ctx, "Wallet.GetCustomer")
	res, err := i.n.GetCustomer(ctx, identityID)
	done(err)
	return res, err
}

func (i obsWallet) OpenPot(ctx context.Context, in PotInput) (*Pot, error) {
	ctx, done := i.o.Start(ctx, "Wallet.OpenPot")
	res, err := i.n.OpenPot(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) ListPots(ctx context.Context, parentID AccountID) ([]*Pot, error) {
	ctx, done := i.o.Start(ctx, "Wallet.ListPots")
	res, err := i.n.ListPots(ctx, parentID)
	done(err)
	return res, err
}

func (i obsWallet) OpenAccount(ctx context.Context, in OpenAccountInput) (*Account, error) {
	ctx, done := i.o.Start(ctx, "Wallet.OpenAccount")
	res, err := i.n.OpenAccount(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) GetAccount(ctx context.Context, id AccountID) (*Account, error) {
	ctx, done := i.o.Start(ctx, "Wallet.GetAccount")
	res, err := i.n.GetAccount(ctx, id)
	done(err)
	return res, err
}

func (i obsWallet) FindAccounts(ctx context.Context, q AccountQuery) (*AccountPage, error) {
	ctx, done := i.o.Start(ctx, "Wallet.FindAccounts")
	res, err := i.n.FindAccounts(ctx, q)
	done(err)
	return res, err
}

func (i obsWallet) History(ctx context.Context, id AccountID, q HistoryQuery) (*HistoryPage, error) {
	ctx, done := i.o.Start(ctx, "Wallet.History")
	res, err := i.n.History(ctx, id, q)
	done(err)
	return res, err
}

func (i obsWallet) Statement(ctx context.Context, id AccountID, from, to time.Time) (*Statement, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Statement")
	res, err := i.n.Statement(ctx, id, from, to)
	done(err)
	return res, err
}

func (i obsWallet) BalanceAt(ctx context.Context, id AccountID, t time.Time) (Money, error) {
	ctx, done := i.o.Start(ctx, "Wallet.BalanceAt")
	res, err := i.n.BalanceAt(ctx, id, t)
	done(err)
	return res, err
}

func (i obsWallet) LimitUsage(ctx context.Context, id AccountID) (*LimitUsage, error) {
	ctx, done := i.o.Start(ctx, "Wallet.LimitUsage")
	res, err := i.n.LimitUsage(ctx, id)
	done(err)
	return res, err
}

func (i obsWallet) Deposit(ctx context.Context, in DepositInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Deposit")
	res, err := i.n.Deposit(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Withdraw")
	res, err := i.n.Withdraw(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Transfer(ctx context.Context, in TransferInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Transfer")
	res, err := i.n.Transfer(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Bulk(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Bulk")
	res, err := i.n.Bulk(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Split(ctx context.Context, in SplitInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Split")
	res, err := i.n.Split(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Exchange(ctx context.Context, in ExchangeInput) (*ExchangeReceipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Exchange")
	res, err := i.n.Exchange(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Schedule(ctx context.Context, in RecurringInput) ([]*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Schedule")
	res, err := i.n.Schedule(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) QuoteDeposit(ctx context.Context, in DepositInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "Wallet.QuoteDeposit")
	res, err := i.n.QuoteDeposit(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) QuoteWithdraw(ctx context.Context, in WithdrawInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "Wallet.QuoteWithdraw")
	res, err := i.n.QuoteWithdraw(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) QuoteTransfer(ctx context.Context, in TransferInput) (*Quote, error) {
	ctx, done := i.o.Start(ctx, "Wallet.QuoteTransfer")
	res, err := i.n.QuoteTransfer(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) QuoteExchange(ctx context.Context, in ExchangeInput) (*ExchangeQuote, error) {
	ctx, done := i.o.Start(ctx, "Wallet.QuoteExchange")
	res, err := i.n.QuoteExchange(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Hold(ctx context.Context, in HoldInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Hold")
	res, err := i.n.Hold(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Confirm")
	res, err := i.n.Confirm(ctx, txID, partial)
	done(err)
	return res, err
}

func (i obsWallet) Cancel(ctx context.Context, txID string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Cancel")
	res, err := i.n.Cancel(ctx, txID)
	done(err)
	return res, err
}

func (i obsWallet) ConfirmMany(ctx context.Context, items []ConfirmItem) ([]BatchResult, error) {
	ctx, done := i.o.Start(ctx, "Wallet.ConfirmMany")
	res, err := i.n.ConfirmMany(ctx, items)
	done(err)
	return res, err
}

func (i obsWallet) CancelMany(ctx context.Context, txIDs []string) ([]BatchResult, error) {
	ctx, done := i.o.Start(ctx, "Wallet.CancelMany")
	res, err := i.n.CancelMany(ctx, txIDs)
	done(err)
	return res, err
}

func (i obsWallet) GetTransaction(ctx context.Context, txID string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.GetTransaction")
	res, err := i.n.GetTransaction(ctx, txID)
	done(err)
	return res, err
}

func (i obsWallet) GetTransactionByReference(ctx context.Context, reference string) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.GetTransactionByReference")
	res, err := i.n.GetTransactionByReference(ctx, reference)
	done(err)
	return res, err
}

func (i obsWallet) Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.Refund")
	res, err := i.n.Refund(ctx, txID, in)
	done(err)
	return res, err
}

func (i obsWallet) RefundPartial(ctx context.Context, txID string, in PartialRefundInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.RefundPartial")
	res, err := i.n.RefundPartial(ctx, txID, in)
	done(err)
	return res, err
}

func (i obsWallet) OpenEscrow(ctx context.Context, in EscrowInput) (*Escrow, error) {
	ctx, done := i.o.Start(ctx, "Wallet.OpenEscrow")
	res, err := i.n.OpenEscrow(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) GetEscrow(ctx context.Context, escrowID AccountID) (*Escrow, error) {
	ctx, done := i.o.Start(ctx, "Wallet.GetEscrow")
	res, err := i.n.GetEscrow(ctx, escrowID)
	done(err)
	return res, err
}

func (i obsWallet) ReleaseEscrow(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.ReleaseEscrow")
	res, err := i.n.ReleaseEscrow(ctx, escrowID, in)
	done(err)
	return res, err
}

func (i obsWallet) RefundEscrow(ctx context.Context, escrowID AccountID, in EscrowSettleInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.RefundEscrow")
	res, err := i.n.RefundEscrow(ctx, escrowID, in)
	done(err)
	return res, err
}

func (i obsWallet) OpenDispute(ctx context.Context, in DisputeInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.OpenDispute")
	res, err := i.n.OpenDispute(ctx, in)
	done(err)
	return res, err
}

func (i obsWallet) ResolveDispute(ctx context.Context, disputeID string, outcome DisputeOutcome, partial *Money) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Wallet.ResolveDispute")
	res, err := i.n.ResolveDispute(ctx, disputeID, outcome, partial)
	done(err)
	return res, err
}

type obsOps struct {
	n Ops
	o Observer
}

var _ Ops = obsOps{}

func (i obsOps) CreateLedger(ctx context.Context, in CreateLedgerInput) (*LedgerInfo, error) {
	ctx, done := i.o.Start(ctx, "Ops.CreateLedger")
	res, err := i.n.CreateLedger(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) GetLedger(ctx context.Context, id LedgerID) (*LedgerInfo, error) {
	ctx, done := i.o.Start(ctx, "Ops.GetLedger")
	res, err := i.n.GetLedger(ctx, id)
	done(err)
	return res, err
}

func (i obsOps) ListLedgers(ctx context.Context, q LedgerQuery) (*LedgerPage, error) {
	ctx, done := i.o.Start(ctx, "Ops.ListLedgers")
	res, err := i.n.ListLedgers(ctx, q)
	done(err)
	return res, err
}

func (i obsOps) FreezeAccount(ctx context.Context, id AccountID, reason string) error {
	ctx, done := i.o.Start(ctx, "Ops.FreezeAccount")
	err := i.n.FreezeAccount(ctx, id, reason)
	done(err)
	return err
}

func (i obsOps) UnfreezeAccount(ctx context.Context, id AccountID) error {
	ctx, done := i.o.Start(ctx, "Ops.UnfreezeAccount")
	err := i.n.UnfreezeAccount(ctx, id)
	done(err)
	return err
}

func (i obsOps) CloseAccount(ctx context.Context, id AccountID, in CloseInput) (*Account, error) {
	ctx, done := i.o.Start(ctx, "Ops.CloseAccount")
	res, err := i.n.CloseAccount(ctx, id, in)
	done(err)
	return res, err
}

func (i obsOps) FreezeLedger(ctx context.Context, id LedgerID, reason string) error {
	ctx, done := i.o.Start(ctx, "Ops.FreezeLedger")
	err := i.n.FreezeLedger(ctx, id, reason)
	done(err)
	return err
}

func (i obsOps) UnfreezeLedger(ctx context.Context, id LedgerID) error {
	ctx, done := i.o.Start(ctx, "Ops.UnfreezeLedger")
	err := i.n.UnfreezeLedger(ctx, id)
	done(err)
	return err
}

func (i obsOps) GetHouseAccount(ctx context.Context, ref HouseRef) (*HouseAccount, error) {
	ctx, done := i.o.Start(ctx, "Ops.GetHouseAccount")
	res, err := i.n.GetHouseAccount(ctx, ref)
	done(err)
	return res, err
}

func (i obsOps) ListHouseAccounts(ctx context.Context, currency string) ([]*HouseAccount, error) {
	ctx, done := i.o.Start(ctx, "Ops.ListHouseAccounts")
	res, err := i.n.ListHouseAccounts(ctx, currency)
	done(err)
	return res, err
}

func (i obsOps) HouseHistory(ctx context.Context, ref HouseRef, q HistoryQuery) (*HistoryPage, error) {
	ctx, done := i.o.Start(ctx, "Ops.HouseHistory")
	res, err := i.n.HouseHistory(ctx, ref, q)
	done(err)
	return res, err
}

func (i obsOps) HouseBalanceAt(ctx context.Context, ref HouseRef, t time.Time) (Money, error) {
	ctx, done := i.o.Start(ctx, "Ops.HouseBalanceAt")
	res, err := i.n.HouseBalanceAt(ctx, ref, t)
	done(err)
	return res, err
}

func (i obsOps) HouseStatement(ctx context.Context, ref HouseRef, from, to time.Time) (*Statement, error) {
	ctx, done := i.o.Start(ctx, "Ops.HouseStatement")
	res, err := i.n.HouseStatement(ctx, ref, from, to)
	done(err)
	return res, err
}

func (i obsOps) RecordSettlement(ctx context.Context, in SettlementInput) (*SettlementReceipt, error) {
	ctx, done := i.o.Start(ctx, "Ops.RecordSettlement")
	res, err := i.n.RecordSettlement(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) HoldInSuspense(ctx context.Context, in SuspenseInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Ops.HoldInSuspense")
	res, err := i.n.HoldInSuspense(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) ClaimFromSuspense(ctx context.Context, in ClaimFromSuspenseInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Ops.ClaimFromSuspense")
	res, err := i.n.ClaimFromSuspense(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) ReturnFromSuspense(ctx context.Context, in ReturnFromSuspenseInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Ops.ReturnFromSuspense")
	res, err := i.n.ReturnFromSuspense(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) Adjust(ctx context.Context, in AdjustInput) (*Receipt, error) {
	ctx, done := i.o.Start(ctx, "Ops.Adjust")
	res, err := i.n.Adjust(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) TrialBalance(ctx context.Context, currency string) (*TrialBalance, error) {
	ctx, done := i.o.Start(ctx, "Ops.TrialBalance")
	res, err := i.n.TrialBalance(ctx, currency)
	done(err)
	return res, err
}

func (i obsOps) WatchBalance(ctx context.Context, in WatchInput) (*Watch, error) {
	ctx, done := i.o.Start(ctx, "Ops.WatchBalance")
	res, err := i.n.WatchBalance(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) UnwatchBalance(ctx context.Context, watchID string) error {
	ctx, done := i.o.Start(ctx, "Ops.UnwatchBalance")
	err := i.n.UnwatchBalance(ctx, watchID)
	done(err)
	return err
}

func (i obsOps) CreateMatchingRule(ctx context.Context, r MatchingRule) (string, error) {
	ctx, done := i.o.Start(ctx, "Ops.CreateMatchingRule")
	res, err := i.n.CreateMatchingRule(ctx, r)
	done(err)
	return res, err
}

func (i obsOps) StartReconciliation(ctx context.Context, in ReconcileInput) (*Reconciliation, error) {
	ctx, done := i.o.Start(ctx, "Ops.StartReconciliation")
	res, err := i.n.StartReconciliation(ctx, in)
	done(err)
	return res, err
}

func (i obsOps) GetReconciliation(ctx context.Context, id string) (*Reconciliation, error) {
	ctx, done := i.o.Start(ctx, "Ops.GetReconciliation")
	res, err := i.n.GetReconciliation(ctx, id)
	done(err)
	return res, err
}
