// Package humanblnk runs wallet operations on Blnk, the open-source
// double-entry ledger: accounts and customers, deposits, withdrawals,
// transfers, card holds, marketplace splits, currency exchange, escrow,
// disputes, refunds, standing orders, payroll, fees, limits, statements,
// settlement and reconciliation.
//
// This package is the front door: it re-exports package ledger, where
// everything is defined and documented, so you can import the module root.
// Types are aliases, so humanblnk.Money and ledger.Money are the same type.
//
//	l := humanblnk.New("http://localhost:5001", humanblnk.Config{LedgerID: "ldg_..."},
//		humanblnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))
//
//	_, err := l.Payments().Transfer(ctx, humanblnk.TransferInput{
//		From: ada, To: tunde, Amount: humanblnk.Amount("5000", "NGN"),
//		Fee: humanblnk.NoFee, Reference: "p2p_01J8ZK2M4Q",
//	})
//	if errors.Is(err, humanblnk.ErrInsufficientFunds) { ... }
//
// Guide with real-world examples: https://otyang.github.io/humanblnk/
package humanblnk

import (
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
)

// New connects to the Blnk Core at blnkURL and returns the ledger service.
// For full control over the HTTP client, build one with NewClient and pass it
// to NewService.
func New(blnkURL string, cfg Config, opts ...Option) *Service {
	return ledger.New(blnk.New(blnkURL, opts...), cfg)
}

// NewService returns the ledger service over an existing Blnk client.
var NewService = ledger.New

// Blnk client construction.
type (
	Client = blnk.Client
	Option = blnk.Option
)

var (
	NewClient      = blnk.New
	WithAPIKey     = blnk.WithAPIKey
	WithHTTPClient = blnk.WithHTTPClient
	WithRetries    = blnk.WithRetries
	WithUserAgent  = blnk.WithUserAgent
)

// Version is the humanblnk release.
const Version = blnk.Version

// Types. Each is documented in package ledger.
type (
	Account             = ledger.Account
	AccountKind         = ledger.AccountKind
	AccountPage         = ledger.AccountPage
	AccountQuery        = ledger.AccountQuery
	Accounts            = ledger.Accounts
	AdjustInput         = ledger.AdjustInput
	AdjustmentType      = ledger.AdjustmentType
	AllocateInput       = ledger.AllocateInput
	BatchResult         = ledger.BatchResult
	BulkEvent           = ledger.BulkEvent
	BulkReceipt         = ledger.BulkReceipt
	BulkTransferInput   = ledger.BulkTransferInput
	CloseInput          = ledger.CloseInput
	Config              = ledger.Config
	ConfirmItem         = ledger.ConfirmItem
	Customer            = ledger.Customer
	CustomerInput       = ledger.CustomerInput
	Customers           = ledger.Customers
	DepositInput        = ledger.DepositInput
	Direction           = ledger.Direction
	DisplayRule         = ledger.DisplayRule
	DisputeInput        = ledger.DisputeInput
	DisputeOutcome      = ledger.DisputeOutcome
	Disputes            = ledger.Disputes
	EntryStatus         = ledger.EntryStatus
	Escrow              = ledger.Escrow
	EscrowInput         = ledger.EscrowInput
	EscrowSettleInput   = ledger.EscrowSettleInput
	Escrows             = ledger.Escrows
	Event               = ledger.Event
	ExchangeInput       = ledger.ExchangeInput
	ExchangeReceipt     = ledger.ExchangeReceipt
	ExternalRecord      = ledger.ExternalRecord
	FeeFunc             = ledger.FeeFunc
	FeeKind             = ledger.FeeKind
	FeeRequest          = ledger.FeeRequest
	FeeRule             = ledger.FeeRule
	FeeTier             = ledger.FeeTier
	HistoryPage         = ledger.HistoryPage
	HistoryQuery        = ledger.HistoryQuery
	HoldInput           = ledger.HoldInput
	Interval            = ledger.Interval
	Ledger              = ledger.Ledger
	Leg                 = ledger.Leg
	Limit               = ledger.Limit
	LimitUsage          = ledger.LimitUsage
	MatchingCriterion   = ledger.MatchingCriterion
	MatchingRule        = ledger.MatchingRule
	Money               = ledger.Money
	Observer            = ledger.Observer
	ObserverFunc        = ledger.ObserverFunc
	OpError             = ledger.OpError
	OpenAccountInput    = ledger.OpenAccountInput
	Operations          = ledger.Operations
	PartialRefundInput  = ledger.PartialRefundInput
	Payments            = ledger.Payments
	Pending             = ledger.Pending
	Pot                 = ledger.Pot
	PotInput            = ledger.PotInput
	Quote               = ledger.Quote
	Receipt             = ledger.Receipt
	ReconcileInput      = ledger.ReconcileInput
	Reconciliation      = ledger.Reconciliation
	Reconciliations     = ledger.Reconciliations
	RecurringInput      = ledger.RecurringInput
	RefundInput         = ledger.RefundInput
	Rounding            = ledger.Rounding
	Service             = ledger.Service
	SettlementDirection = ledger.SettlementDirection
	SettlementInput     = ledger.SettlementInput
	SettlementReceipt   = ledger.SettlementReceipt
	SplitInput          = ledger.SplitInput
	SplitPart           = ledger.SplitPart
	Statement           = ledger.Statement
	StatementLine       = ledger.StatementLine
	SuspenseInput       = ledger.SuspenseInput
	Transactions        = ledger.Transactions
	TransferInput       = ledger.TransferInput
	TrialBalance        = ledger.TrialBalance
	Watch               = ledger.Watch
	WatchInput          = ledger.WatchInput
	WebhookConfig       = ledger.WebhookConfig
	WithdrawInput       = ledger.WithdrawInput
)

// Functions.
var (
	Amount          = ledger.Amount
	ClearingAccount = ledger.ClearingAccount
	ComputeFee      = ledger.ComputeFee
	EntryStatusOf   = ledger.EntryStatusOf
	Instrument      = ledger.Instrument
	LogObserver     = ledger.LogObserver
	Minor           = ledger.Minor
	ParseDecimal    = ledger.ParseDecimal
	Precise         = ledger.Precise
)

// Errors: check with errors.Is.
var (
	ErrAccountClosed      = ledger.ErrAccountClosed
	ErrAccountFrozen      = ledger.ErrAccountFrozen
	ErrAlreadyFinalized   = ledger.ErrAlreadyFinalized
	ErrAmountExceeded     = ledger.ErrAmountExceeded
	ErrApprovalRequired   = ledger.ErrApprovalRequired
	ErrConflict           = ledger.ErrConflict
	ErrDuplicateReference = ledger.ErrDuplicateReference
	ErrFeePricing         = ledger.ErrFeePricing
	ErrInsufficientFunds  = ledger.ErrInsufficientFunds
	ErrInvalidInput       = ledger.ErrInvalidInput
	ErrLimitExceeded      = ledger.ErrLimitExceeded
	ErrNotFound           = ledger.ErrNotFound
	ErrNotPending         = ledger.ErrNotPending
	ErrPendingActivity    = ledger.ErrPendingActivity
	ErrSignature          = ledger.ErrSignature
	ErrUnauthorized       = ledger.ErrUnauthorized
	ErrUnavailable        = ledger.ErrUnavailable
)

// Values.
var (
	BuiltinDisplay    = ledger.BuiltinDisplay
	BuiltinPrecisions = ledger.BuiltinPrecisions
	Daily             = ledger.Daily
	Monthly           = ledger.Monthly
	NoFee             = ledger.NoFee
	Weekly            = ledger.Weekly
)

// Constants: system accounts, kinds, statuses, events and options.
const (
	AdjustCredit              = ledger.AdjustCredit
	AdjustDebit               = ledger.AdjustDebit
	AdjustmentsAccount        = ledger.AdjustmentsAccount
	All                       = ledger.All
	BankOperatingAccount      = ledger.BankOperatingAccount
	CustomerAccount           = ledger.CustomerAccount
	DepositClearingAccount    = ledger.DepositClearingAccount
	DisputeRejected           = ledger.DisputeRejected
	DisputeUpheld             = ledger.DisputeUpheld
	EntryPending              = ledger.EntryPending
	EntryPosted               = ledger.EntryPosted
	EntryQueued               = ledger.EntryQueued
	EntryRejected             = ledger.EntryRejected
	EntryScheduled            = ledger.EntryScheduled
	EntryVoided               = ledger.EntryVoided
	EscrowAccount             = ledger.EscrowAccount
	EventBalanceAlert         = ledger.EventBalanceAlert
	EventBalanceCreated       = ledger.EventBalanceCreated
	EventBulkApplied          = ledger.EventBulkApplied
	EventBulkFailed           = ledger.EventBulkFailed
	EventBulkInflight         = ledger.EventBulkInflight
	EventIdentityCreated      = ledger.EventIdentityCreated
	EventLedgerCreated        = ledger.EventLedgerCreated
	EventSystemError          = ledger.EventSystemError
	EventTransactionApplied   = ledger.EventTransactionApplied
	EventTransactionInflight  = ledger.EventTransactionInflight
	EventTransactionQueued    = ledger.EventTransactionQueued
	EventTransactionRejected  = ledger.EventTransactionRejected
	EventTransactionScheduled = ledger.EventTransactionScheduled
	EventTransactionVoid      = ledger.EventTransactionVoid
	FXPoolAccount             = ledger.FXPoolAccount
	FeeDeposit                = ledger.FeeDeposit
	FeeExchange               = ledger.FeeExchange
	FeeIncomeAccount          = ledger.FeeIncomeAccount
	FeeTransfer               = ledger.FeeTransfer
	FeeWithdrawal             = ledger.FeeWithdrawal
	Incoming                  = ledger.Incoming
	MerchantPayableAccount    = ledger.MerchantPayableAccount
	Outgoing                  = ledger.Outgoing
	PayoutClearingAccount     = ledger.PayoutClearingAccount
	PotAccount                = ledger.PotAccount
	ProviderFeesAccount       = ledger.ProviderFeesAccount
	RoundDown                 = ledger.RoundDown
	RoundHalfUp               = ledger.RoundHalfUp
	RoundUp                   = ledger.RoundUp
	SettlementCollection      = ledger.SettlementCollection
	SettlementPayout          = ledger.SettlementPayout
	SuspenseAccount           = ledger.SuspenseAccount
	SystemAccount             = ledger.SystemAccount
)
