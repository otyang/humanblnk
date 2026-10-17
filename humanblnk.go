// Package humanblnk is a wallet and financial-operations domain layer built on
// top of Blnk, the open-source double-entry ledger.
//
// Blnk keeps balances and double-entry transactions and knows nothing about
// the business on top. humanblnk is that business layer: your app calls it in
// the domain's words (open a customer, deposit, hold, withdraw, refund, settle
// with a provider, close the books) and it applies the rules Blnk leaves to you
// (fees, limits, frozen and closed accounts, approvals, exact money) before
// turning each call into correct Blnk entries. It stores nothing itself: Blnk
// stays the record. It never calls payment providers: your app does, and tells
// humanblnk what happened.
//
// Two handles, one per domain: l.Wallet() for what customers do, and l.Ops()
// for what the business does.
//
// This package is the front door: it re-exports package ledger, where
// everything is defined and documented, so you can import the module root.
// Types are aliases, so humanblnk.Money and ledger.Money are the same type.
//
//	l := humanblnk.New("http://localhost:5001", humanblnk.Config{LedgerID: "ldg_..."},
//		humanblnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))
//
//	_, err := l.Wallet().Transfer(ctx, humanblnk.TransferInput{
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
	WithInstanceID = blnk.WithInstanceID
	WithRetries    = blnk.WithRetries
	WithUserAgent  = blnk.WithUserAgent
)

// CloudProxyURL is Blnk Cloud's proxy; use it with WithInstanceID.
const CloudProxyURL = blnk.CloudProxyURL

// Version is the humanblnk release.
const Version = blnk.Version

// Types. Each is documented in package ledger.
type (
	Account                 = ledger.Account
	AccountID               = ledger.AccountID
	AccountKind             = ledger.AccountKind
	AccountPage             = ledger.AccountPage
	AccountQuery            = ledger.AccountQuery
	AdjustInput             = ledger.AdjustInput
	AdjustmentType          = ledger.AdjustmentType
	Balances                = ledger.Balances
	BatchResult             = ledger.BatchResult
	BulkEvent               = ledger.BulkEvent
	BulkItem                = ledger.BulkItem
	BulkReceipt             = ledger.BulkReceipt
	BulkTransferInput       = ledger.BulkTransferInput
	ClaimFromSuspenseInput  = ledger.ClaimFromSuspenseInput
	CloseInput              = ledger.CloseInput
	Config                  = ledger.Config
	ConfirmItem             = ledger.ConfirmItem
	CreateLedgerInput       = ledger.CreateLedgerInput
	Customer                = ledger.Customer
	CustomerInput           = ledger.CustomerInput
	DepositInput            = ledger.DepositInput
	Direction               = ledger.Direction
	DisputeInput            = ledger.DisputeInput
	DisputeOutcome          = ledger.DisputeOutcome
	EntryStatus             = ledger.EntryStatus
	Escrow                  = ledger.Escrow
	EscrowInput             = ledger.EscrowInput
	EscrowSettleInput       = ledger.EscrowSettleInput
	Event                   = ledger.Event
	ExchangeInput           = ledger.ExchangeInput
	ExchangeQuote           = ledger.ExchangeQuote
	ExchangeReceipt         = ledger.ExchangeReceipt
	ExternalRecord          = ledger.ExternalRecord
	FeeFunc                 = ledger.FeeFunc
	FeeKind                 = ledger.FeeKind
	FeeRequest              = ledger.FeeRequest
	FeeRule                 = ledger.FeeRule
	FeeTier                 = ledger.FeeTier
	GLDef                   = ledger.GLDef
	GLType                  = ledger.GLType
	HistoryPage             = ledger.HistoryPage
	HistoryQuery            = ledger.HistoryQuery
	HoldInput               = ledger.HoldInput
	HouseAccount            = ledger.HouseAccount
	HouseRef                = ledger.HouseRef
	Interval                = ledger.Interval
	Ledger                  = ledger.Ledger
	Wallet                  = ledger.Wallet
	Ops                     = ledger.Ops
	LedgerID                = ledger.LedgerID
	LedgerInfo              = ledger.LedgerInfo
	LedgerPage              = ledger.LedgerPage
	LedgerQuery             = ledger.LedgerQuery
	Leg                     = ledger.Leg
	Limit                   = ledger.Limit
	LimitUsage              = ledger.LimitUsage
	MatchingCriterion       = ledger.MatchingCriterion
	MatchingRule            = ledger.MatchingRule
	Money                   = ledger.Money
	NormalSide              = ledger.NormalSide
	Observer                = ledger.Observer
	ObserverFunc            = ledger.ObserverFunc
	OpError                 = ledger.OpError
	OpenAccountInput        = ledger.OpenAccountInput
	PartialRefundInput      = ledger.PartialRefundInput
	Pot                     = ledger.Pot
	PotInput                = ledger.PotInput
	Quote                   = ledger.Quote
	Receipt                 = ledger.Receipt
	ReconcileInput          = ledger.ReconcileInput
	Reconciliation          = ledger.Reconciliation
	RecurringInput          = ledger.RecurringInput
	RefundInput             = ledger.RefundInput
	ReturnFromSuspenseInput = ledger.ReturnFromSuspenseInput
	Rounding                = ledger.Rounding
	Service                 = ledger.Service
	SettlementDirection     = ledger.SettlementDirection
	SettlementInput         = ledger.SettlementInput
	SettlementReceipt       = ledger.SettlementReceipt
	SplitInput              = ledger.SplitInput
	SplitPart               = ledger.SplitPart
	Statement               = ledger.Statement
	StatementLine           = ledger.StatementLine
	SuspenseInput           = ledger.SuspenseInput
	TransferInput           = ledger.TransferInput
	TrialBalance            = ledger.TrialBalance
	UnknownCurrencyPolicy   = ledger.UnknownCurrencyPolicy
	Watch                   = ledger.Watch
	WatchInput              = ledger.WatchInput
	WebhookConfig           = ledger.WebhookConfig
	WithdrawInput           = ledger.WithdrawInput
)

// Functions.
var (
	Amount          = ledger.Amount
	ChartEntry      = ledger.ChartEntry
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
	ErrLedgerFrozen       = ledger.ErrLedgerFrozen
	ErrLedgerNotFound     = ledger.ErrLedgerNotFound
	ErrLimitExceeded      = ledger.ErrLimitExceeded
	ErrNotFound           = ledger.ErrNotFound
	ErrNotPending         = ledger.ErrNotPending
	ErrPendingActivity    = ledger.ErrPendingActivity
	ErrSignature          = ledger.ErrSignature
	ErrUnauthorized       = ledger.ErrUnauthorized
	ErrUnavailable        = ledger.ErrUnavailable
	ErrWrongKind          = ledger.ErrWrongKind
)

// Values.
var (
	Chart   = ledger.Chart
	Daily   = ledger.Daily
	Monthly = ledger.Monthly
	NoFee   = ledger.NoFee
	Weekly  = ledger.Weekly
)

// Constants: house accounts, kinds, statuses, events and options.
const (
	AdjustCredit              = ledger.AdjustCredit
	AdjustDebit               = ledger.AdjustDebit
	AdjustmentsAccount        = ledger.AdjustmentsAccount
	All                       = ledger.All
	AllowUnknown              = ledger.AllowUnknown
	BankOperatingAccount      = ledger.BankOperatingAccount
	CreditNormal              = ledger.CreditNormal
	CustomerAccount           = ledger.CustomerAccount
	DebitNormal               = ledger.DebitNormal
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
	FrozenFundsAccount        = ledger.FrozenFundsAccount
	FeeDeposit                = ledger.FeeDeposit
	FeeExchange               = ledger.FeeExchange
	FeeIncomeAccount          = ledger.FeeIncomeAccount
	FeeTransfer               = ledger.FeeTransfer
	FeeWithdrawal             = ledger.FeeWithdrawal
	GLAsset                   = ledger.GLAsset
	GLEquity                  = ledger.GLEquity
	GLExpense                 = ledger.GLExpense
	GLIncome                  = ledger.GLIncome
	GLLiability               = ledger.GLLiability
	GLUnknown                 = ledger.GLUnknown
	GeneralLedgerID           = ledger.GeneralLedgerID
	Incoming                  = ledger.Incoming
	MerchantPayableAccount    = ledger.MerchantPayableAccount
	Outgoing                  = ledger.Outgoing
	PayoutClearingAccount     = ledger.PayoutClearingAccount
	PotAccount                = ledger.PotAccount
	ProviderFeesAccount       = ledger.ProviderFeesAccount
	RefuseUnknown             = ledger.RefuseUnknown
	RoundDown                 = ledger.RoundDown
	RoundHalfUp               = ledger.RoundHalfUp
	RoundUp                   = ledger.RoundUp
	SettlementCollection      = ledger.SettlementCollection
	SettlementPayout          = ledger.SettlementPayout
	SuspenseAccount           = ledger.SuspenseAccount
)
