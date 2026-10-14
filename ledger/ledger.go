// Package ledger runs wallet operations on Blnk, the open-source double-entry
// ledger: accounts and customers, deposits, withdrawals, transfers, card holds,
// marketplace splits, currency exchange, escrow, disputes, refunds, standing
// orders, payroll, fees, limits, statements, settlement and reconciliation.
//
// Application code depends on the Ledger interface, grouped into namespaces:
//
//	l.Accounts()        Open, Get, GetSystem, Find, Freeze, Unfreeze, Close, History, BalanceAt, Statement, LimitUsage
//	l.Customers()       Open, Get, OpenPot, ListPots
//	l.Payments()        Deposit, Withdraw, Transfer, Split, Exchange, Bulk, Schedule, QuoteDeposit, QuoteWithdraw, QuoteTransfer
//	l.Pending()         Hold, Confirm, Cancel, ConfirmMany, CancelMany
//	l.Transactions()    Get, GetByReference, Refund, RefundPartial
//	l.Disputes()        Open, Resolve
//	l.Escrows()         Open, Get, Release, Refund
//	l.Operations()      RecordSettlement, ParkInSuspense, AllocateSuspense, Adjust, TrialBalance, WatchBalance, Unwatch
//	l.Reconciliations() CreateRule, Start, Get
//
// Service implements it on a Blnk Core instance; package ledgertest provides an
// in-memory Blnk for tests. Amounts are exact decimals (see Money): never
// floats. Every movement takes a Reference, which makes retries safe. Every
// chargeable movement states its fee, either explicitly (NoFee for free) or
// through Config.Fees.
//
// Money flows (all accounts configurable in Config):
//
//	Deposit     @PaystackClearing ──► account            (+ fee leg to @FeeIncome)
//	Withdraw    account ──► @FlutterwaveClearing         (+ fee leg to @FeeIncome, pending until confirmed)
//	Transfer    account ──► account                       (+ fee leg to @FeeIncome)
//	Hold        account ──► destination, pending         (Confirm = capture, Cancel = release)
//
// A full guide with real-world examples is at https://otyang.github.io/humanblnk/.
package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/otyang/humanblnk/blnk"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

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
	// outgoing transactions for the period.
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

func (c *Config) defaults() {
	if c.DefaultPrecision <= 0 {
		c.DefaultPrecision = 100
	}
	if c.FeeIncomeAccount == "" {
		c.FeeIncomeAccount = FeeIncomeAccount
	}
	if c.DepositClearingAccount == "" {
		c.DepositClearingAccount = DepositClearingAccount
	}
	if c.PayoutClearingAccount == "" {
		c.PayoutClearingAccount = PayoutClearingAccount
	}
	if c.WithdrawalHoldTTL == 0 {
		c.WithdrawalHoldTTL = 24 * time.Hour
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// ---------------------------------------------------------------------------
// Inputs / outputs
// ---------------------------------------------------------------------------

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

// Common fields for money movement.
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

type RefundInput struct {
	Reason      string
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

// Pending reports whether funds are reserved but not yet settled.
func (r *Receipt) Pending() bool { return r.Status == blnk.StatusInflight }

// Settled reports whether funds have moved.
func (r *Receipt) Settled() bool { return r.Status == blnk.StatusApplied }

// Posted reports whether the entry is in the balances (same as Settled).
func (r *Receipt) Posted() bool { return r.EntryStatus == EntryPosted }

type Leg struct {
	Account       string `json:"account"`
	Amount        Money  `json:"amount"`       // set for fixed legs (fee, payout); zero for "left"/"%"
	Distribution  string `json:"distribution"` // as sent: "left", "20%", or the precise integer
	TransactionID string `json:"transaction_id,omitempty"`
	Narration     string `json:"narration,omitempty"`
}

type BulkReceipt struct {
	BatchID string
	Status  string // applied | inflight | processing
	Count   int
}

type Direction int

const (
	All Direction = iota
	Incoming
	Outgoing
)

type HistoryQuery struct {
	Direction Direction
	Statuses  []string // "posted"/"pending"/... or Blnk's "APPLIED"/"INFLIGHT"/...; empty = all
	Since     time.Time
	Until     time.Time
	Limit     int // default 20, max 100
	Offset    int
}

type HistoryPage struct {
	Items  []Receipt
	Limit  int
	Offset int
	Total  int
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service implements Ledger on top of a Blnk Core instance.
type Service struct {
	c   *blnk.Client
	cfg Config

	ruleMu sync.Mutex
	ruleID string // default reconciliation matching rule, created on first use

	indicators sync.Map // "@Name|CUR" → balance id; indicator balances never move
}

// New builds a Service. The client is shared and safe for concurrent use.
func New(c *blnk.Client, cfg Config) *Service {
	cfg.defaults()
	return &Service{c: c, cfg: cfg}
}

// BuiltinPrecisions are used when Config.Precisions doesn't list a currency.
// Fiat not listed here falls back to Config.DefaultPrecision (100 = 2 dp).
//
// Keep a currency's precision fixed once money has moved: Blnk sums
// precise_amount on balances, so mixing precisions corrupts the totals.
var BuiltinPrecisions = map[string]int64{
	"BTC": 100_000_000, "LTC": 100_000_000, "BCH": 100_000_000,
	"ETH":  1_000_000_000_000_000_000,
	"SOL":  1_000_000_000,
	"USDT": 1_000_000, "USDC": 1_000_000, "TRX": 1_000_000,
	"JPY": 1, "KRW": 1, "XOF": 1, "XAF": 1, "UGX": 1, "RWF": 1,
	"KWD": 1000, "BHD": 1000, "OMR": 1000, "TND": 1000,
}

// Precision returns the precision used for currency.
func (s *Service) Precision(currency string) int64 {
	cur := strings.ToUpper(currency)
	if p, ok := s.cfg.Precisions[cur]; ok && p > 0 {
		return p
	}
	if p, ok := BuiltinPrecisions[cur]; ok {
		return p
	}
	return s.cfg.DefaultPrecision
}

// Parse converts a typed decimal ("0.456") to Money with both forms filled.
func (s *Service) Parse(amount, currency string) (Money, error) {
	return Amount(amount, currency).resolve(s.Precision(currency))
}

// resolve fills PreciseAmount/Precision (or Value) using the currency's precision.
// currency is used when m.Currency is empty (e.g. a fee written as Amount("1.50", "")).
func (s *Service) resolve(op string, m Money, currency string) (Money, error) {
	if m.Currency == "" {
		m.Currency = strings.ToUpper(currency)
	}
	if m.Currency == "" {
		return Money{}, invalid(op, "currency is required")
	}
	r, err := m.resolve(s.Precision(m.Currency))
	if err != nil {
		return Money{}, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	r.Display = s.Display(r)
	return r, nil
}

// prepare validates a movement and resolves amount and fee to the same precision.
func (s *Service) prepare(op, account string, amount, fee Money, ref string) (Money, Money, error) {
	if err := s.validateMove(op, account, ref); err != nil {
		return Money{}, Money{}, err
	}
	if !amount.isSet() {
		return Money{}, Money{}, invalid(op, "amount is required")
	}
	a, err := s.resolve(op, amount, "")
	if err != nil {
		return Money{}, Money{}, err
	}
	if !a.IsPositive() {
		return Money{}, Money{}, invalid(op, "amount must be positive")
	}
	f := s.money(new(big.Int), a.Precision, a.Currency)
	if fee.isSet() {
		if fee.Currency != "" && !strings.EqualFold(fee.Currency, a.Currency) {
			return Money{}, Money{}, invalid(op, "fee currency %s differs from amount currency %s", fee.Currency, a.Currency)
		}
		fee.Precision = a.Precision
		if f, err = s.resolve(op, fee, a.Currency); err != nil {
			return Money{}, Money{}, err
		}
		if f.Sign() < 0 {
			return Money{}, Money{}, invalid(op, "fee cannot be negative")
		}
	}
	return a, f, nil
}

// ----- Accounts --------------------------------------------------------------

// Open creates a customer account (a Blnk balance) in Config.LedgerID or
// in.LedgerID, marked active. With ExternalID it is safe to retry.
func (s accountsAPI) Open(ctx context.Context, in OpenAccountInput) (*Account, error) {
	const op = "ledger.Accounts.Open"
	ledgerID := in.LedgerID
	if ledgerID == "" {
		ledgerID = s.cfg.LedgerID
	}
	if ledgerID == "" {
		return nil, invalid(op, "ledger id is required (set Config.LedgerID)")
	}
	if strings.TrimSpace(in.Currency) == "" {
		return nil, invalid(op, "currency is required")
	}
	if in.ExternalID != "" {
		existing, err := s.findOne(ctx, AccountQuery{Currency: in.Currency, MetaData: map[string]string{"external_id": in.ExternalID}})
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	md := blnk.MetaData{}
	for k, v := range in.MetaData {
		md[k] = v
	}
	if _, ok := md["status"]; !ok {
		md["status"] = "active"
	}
	if in.ExternalID != "" {
		md["external_id"] = in.ExternalID
	}
	b, err := s.c.CreateBalance(ctx, blnk.CreateBalanceRequest{
		LedgerID:   ledgerID,
		Currency:   strings.ToUpper(in.Currency),
		IdentityID: in.IdentityID,
		MetaData:   md,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toAccount(b), nil
}

// Get returns an account with its balance, available balance (balance minus
// pending debits) and pending amounts.
func (s accountsAPI) Get(ctx context.Context, accountID string) (*Account, error) {
	const op = "ledger.Accounts.Get"
	if accountID == "" {
		return nil, invalid(op, "account id is required")
	}
	b, err := s.c.GetBalance(ctx, accountID)
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toAccount(b), nil
}

// GetSystem returns a system account such as "@FeeIncome" or "@PaystackClearing".
func (s accountsAPI) GetSystem(ctx context.Context, indicator, currency string) (*Account, error) {
	const op = "ledger.Accounts.GetSystem"
	if !strings.HasPrefix(indicator, "@") {
		return nil, invalid(op, "indicator must start with @")
	}
	b, err := s.c.GetBalanceByIndicator(ctx, indicator, strings.ToUpper(currency))
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toAccount(b), nil
}

// Freeze marks the account frozen in metadata. Enforced by this layer when
// Config.EnforceAccountStatus is true (Blnk itself does not block on metadata).
func (s accountsAPI) Freeze(ctx context.Context, accountID, reason string) error {
	_, err := s.c.UpdateMetadata(ctx, accountID, blnk.MetaData{
		"status": "frozen", "status_reason": reason,
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	})
	return mapErr("ledger.Accounts.Freeze", err)
}

// Unfreeze marks a frozen account active again.
func (s accountsAPI) Unfreeze(ctx context.Context, accountID string) error {
	_, err := s.c.UpdateMetadata(ctx, accountID, blnk.MetaData{
		"status": "active", "status_reason": "",
		"status_changed_at": s.cfg.Now().UTC().Format(time.RFC3339),
	})
	return mapErr("ledger.Accounts.Unfreeze", err)
}

// ----- Money movement -------------------------------------------------------

// Deposit records money received from outside: the provider's clearing
// account (e.g. @PaystackClearing) is debited and the account credited, less
// the fee, which goes to the fee income account. Pending holds it INFLIGHT
// until Pending().Confirm.
func (s paymentsAPI) Deposit(ctx context.Context, in DepositInput) (*Receipt, error) {
	const op = "ledger.Payments.Deposit"
	req, err := s.depositReq(ctx, op, in)
	if err != nil {
		return nil, err
	}
	return s.post(ctx, op, req)
}

func (s *Service) depositReq(ctx context.Context, op string, in DepositInput) (blnk.CreateTransactionRequest, error) {
	var none blnk.CreateTransactionRequest
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeDeposit, Amount: in.Amount, Provider: in.Provider}, in.AccountID, nil)
	if err != nil {
		return none, err
	}
	amount, fee, err := s.prepare(op, in.AccountID, in.Amount, fee, in.Reference)
	if err != nil {
		return none, err
	}
	if fee.cmp(amount) >= 0 {
		return none, invalid(op, "fee must be less than the deposit amount")
	}
	if err := s.checkActive(ctx, op, in.AccountID); err != nil {
		return none, err
	}

	req := s.baseReq(amount, in.Reference, in.Description, in.MetaData, "deposit")
	req.Source = s.clearing(in.ClearingAccount, in.Provider, s.cfg.DepositClearingAccount)
	req.AllowOverdraft = true // external source mirrors money received from outside
	req.Inflight = in.Pending
	if fee.IsPositive() {
		// Gross amount leaves the clearing account; fee goes to fee income, the rest to the account.
		req.Destinations = []blnk.Distribution{
			{Identifier: s.cfg.FeeIncomeAccount, PreciseDistribution: fee.PreciseAmount.String(), Narration: "Deposit fee"},
			{Identifier: in.AccountID, Distribution: "left", Narration: firstNonEmpty(in.Description, "Deposit")},
		}
		feeMeta(req.MetaData, fee, amount.add(neg(fee)))
	} else {
		req.Destination = in.AccountID
	}
	return req, nil
}

// Withdraw sends money out: the account is debited Amount + Fee, the
// provider's clearing account credited Amount. It is held INFLIGHT until
// Pending().Confirm (provider success) or Cancel (failure), and auto-cancels
// after Config.WithdrawalHoldTTL; Immediate posts it at once.
func (s paymentsAPI) Withdraw(ctx context.Context, in WithdrawInput) (*Receipt, error) {
	const op = "ledger.Payments.Withdraw"
	req, err := s.withdrawReq(ctx, op, in)
	if err != nil {
		return nil, err
	}
	return s.post(ctx, op, req)
}

func (s *Service) withdrawReq(ctx context.Context, op string, in WithdrawInput) (blnk.CreateTransactionRequest, error) {
	var none blnk.CreateTransactionRequest
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeWithdrawal, Amount: in.Amount, Provider: in.Provider}, in.AccountID, nil)
	if err != nil {
		return none, err
	}
	amount, fee, err := s.prepare(op, in.AccountID, in.Amount, fee, in.Reference)
	if err != nil {
		return none, err
	}
	if err := s.checkActive(ctx, op, in.AccountID); err != nil {
		return none, err
	}
	if err := s.checkLimits(ctx, op, in.AccountID, amount.add(fee).PreciseAmount); err != nil {
		return none, err
	}
	sink := s.clearing(in.ClearingAccount, in.Provider, s.cfg.PayoutClearingAccount)

	req := s.baseReq(amount.add(fee), in.Reference, in.Description, in.MetaData, "withdrawal")
	req.Source = in.AccountID
	if fee.IsPositive() {
		req.Destinations = []blnk.Distribution{
			{Identifier: sink, PreciseDistribution: amount.PreciseAmount.String(), Narration: firstNonEmpty(in.Description, "Withdrawal")},
			{Identifier: s.cfg.FeeIncomeAccount, PreciseDistribution: fee.PreciseAmount.String(), Narration: "Withdrawal fee"},
		}
		feeMeta(req.MetaData, fee, amount)
	} else {
		req.Destination = sink
	}
	if !in.Immediate {
		req.Inflight = true
		ttl := in.ExpiresIn
		if ttl == 0 {
			ttl = s.cfg.WithdrawalHoldTTL
		}
		if ttl > 0 {
			req.InflightExpiryDate = s.cfg.Now().Add(ttl).UTC().Format(time.RFC3339)
		}
	}
	return req, nil
}

// Transfer moves money between accounts. The sender pays Amount + Fee; the
// recipient gets Amount. Pending holds it INFLIGHT; ScheduledFor posts it later.
func (s paymentsAPI) Transfer(ctx context.Context, in TransferInput) (*Receipt, error) {
	const op = "ledger.Payments.Transfer"
	in, err := s.withTransferFee(ctx, op, in, nil)
	if err != nil {
		return nil, err
	}
	return s.move(ctx, op, in, "transfer")
}

// withTransferFee fills in a customer transfer's fee (see feeFor). Internal
// flows build transfers without it: they never charge a fee.
func (s *Service) withTransferFee(ctx context.Context, op string, in TransferInput, from *blnk.Balance) (TransferInput, error) {
	fee, err := s.feeFor(ctx, op, in.Fee, FeeRequest{Kind: FeeTransfer, Amount: in.Amount}, in.From, from)
	in.Fee = fee
	return in, err
}

// move is Transfer with an explicit operation name and txn_type, used by the
// higher-level flows (escrow, suspense, settlement, adjustments, ...).
func (s *Service) move(ctx context.Context, op string, in TransferInput, kind string) (*Receipt, error) {
	req, err := s.moveReq(ctx, op, in, kind)
	if err != nil {
		return nil, err
	}
	return s.post(ctx, op, req)
}

// moveReq validates a transfer and builds its request without sending it.
func (s *Service) moveReq(ctx context.Context, op string, in TransferInput, kind string) (blnk.CreateTransactionRequest, error) {
	var none blnk.CreateTransactionRequest
	amount, fee, err := s.prepare(op, in.From, in.Amount, in.Fee, in.Reference)
	if err != nil {
		return none, err
	}
	if in.To == "" {
		return none, invalid(op, "destination account is required")
	}
	if in.From == in.To {
		return none, invalid(op, "cannot transfer to the same account")
	}
	if err := s.checkActive(ctx, op, in.From, in.To); err != nil {
		return none, err
	}
	if limited(kind) {
		if err := s.checkLimits(ctx, op, in.From, amount.add(fee).PreciseAmount); err != nil {
			return none, err
		}
	}
	req, err := s.transferReq(in, amount, fee, kind)
	if err != nil {
		return none, invalid(op, "%v", err)
	}
	return req, nil
}

// Hold reserves money on an account without moving it, like a card
// authorisation. Pending().Confirm captures all or part of it; Cancel releases
// it. ExpiresAt releases it automatically, CaptureAt captures it.
func (s pendingAPI) Hold(ctx context.Context, in HoldInput) (*Receipt, error) {
	return s.hold(ctx, "ledger.Pending.Hold", in, "hold")
}

func (s *Service) hold(ctx context.Context, op string, in HoldInput, kind string) (*Receipt, error) {
	amount, _, err := s.prepare(op, in.AccountID, in.Amount, Money{}, in.Reference)
	if err != nil {
		return nil, err
	}
	if in.Destination == "" {
		return nil, invalid(op, "destination is required")
	}
	if !in.CaptureAt.IsZero() && !in.ExpiresAt.IsZero() && !in.CaptureAt.Before(in.ExpiresAt) {
		return nil, invalid(op, "CaptureAt must be before ExpiresAt or the hold is released first")
	}
	if err := s.checkActive(ctx, op, in.AccountID); err != nil {
		return nil, err
	}
	if limited(kind) {
		if err := s.checkLimits(ctx, op, in.AccountID, amount.PreciseAmount); err != nil {
			return nil, err
		}
	}
	req := s.baseReq(amount, in.Reference, in.Description, in.MetaData, kind)
	req.Source = in.AccountID
	req.Destination = in.Destination
	req.Inflight = true
	if !in.ExpiresAt.IsZero() {
		req.InflightExpiryDate = in.ExpiresAt.Format(time.RFC3339)
	}
	if !in.CaptureAt.IsZero() {
		req.InflightCommitDate = in.CaptureAt.Format(time.RFC3339)
	}
	return s.post(ctx, op, req)
}

// Confirm settles an INFLIGHT transaction. With partial set, only that amount is
// settled and the rest stays held (call Confirm again or Cancel the remainder).
func (s pendingAPI) Confirm(ctx context.Context, txID string, partial *Money) (*Receipt, error) {
	const op = "ledger.Pending.Confirm"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	req := blnk.UpdateInflightRequest{Status: blnk.ActionCommit, SkipQueue: !s.cfg.UseQueue}
	if partial != nil && partial.isSet() {
		amt, err := s.partialAmount(ctx, op, txID, *partial)
		if err != nil {
			return nil, err
		}
		req.PreciseAmount = blnk.BigFrom(amt.PreciseAmount)
	}
	resp, err := s.c.UpdateInflight(ctx, txID, req)
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.inflightReceipt(txID, resp), nil
}

// Cancel releases whatever is still held on an INFLIGHT transaction.
func (s pendingAPI) Cancel(ctx context.Context, txID string) (*Receipt, error) {
	const op = "ledger.Pending.Cancel"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	resp, err := s.c.UpdateInflight(ctx, txID, blnk.UpdateInflightRequest{
		Status: blnk.ActionVoid, SkipQueue: !s.cfg.UseQueue,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.inflightReceipt(txID, resp), nil
}

// Refund reverses a settled (or confirmed/cancelled pending) transaction once.
// For split transactions created through the queue, pass the queued parent id.
func (s transactionsAPI) Refund(ctx context.Context, txID string, in RefundInput) (*Receipt, error) {
	const op = "ledger.Transactions.Refund"
	if txID == "" {
		return nil, invalid(op, "transaction id is required")
	}
	md := blnk.MetaData{}
	for k, v := range in.MetaData {
		md[k] = v
	}
	if in.Reason != "" {
		md["refund_reason"] = in.Reason
	}
	t, err := s.c.Refund(ctx, txID, &blnk.RefundRequest{
		SkipQueue:   !s.cfg.UseQueue,
		Description: in.Description,
		MetaData:    nilIfEmpty(md),
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toReceipt(t), nil
}

// Bulk posts up to 10,000 transfers as one Blnk batch, e.g. payroll.
// Atomic makes them all-or-nothing; Pending holds them all INFLIGHT; Async
// processes them in the background with the outcome sent by webhook.
func (s paymentsAPI) Bulk(ctx context.Context, in BulkTransferInput) (*BulkReceipt, error) {
	const op = "ledger.Payments.Bulk"
	if len(in.Items) == 0 {
		return nil, invalid(op, "at least one transfer is required")
	}
	if len(in.Items) > 10000 {
		return nil, invalid(op, "a batch holds at most 10,000 transfers")
	}
	seen := make(map[string]struct{}, len(in.Items))
	reqs := make([]blnk.CreateTransactionRequest, 0, len(in.Items))
	ids := make([]string, 0, 2*len(in.Items))
	senders := map[string]*blnk.Balance{} // one lookup per sender when Config.Fees prices items
	for i, it := range in.Items {
		if !it.Fee.isSet() && s.cfg.Fees != nil && it.From != "" && !strings.HasPrefix(it.From, "@") && senders[it.From] == nil {
			b, err := s.c.GetBalance(ctx, it.From)
			if err != nil {
				return nil, mapErr(op, err)
			}
			senders[it.From] = b
		}
		var err error
		if it, err = s.withTransferFee(ctx, op, it, senders[it.From]); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		amount, fee, err := s.prepare(op, it.From, it.Amount, it.Fee, it.Reference)
		if err != nil {
			return nil, invalid(op, "item %d: %v", i, err)
		}
		if it.To == "" || it.From == it.To {
			return nil, invalid(op, "item %d: invalid destination", i)
		}
		if _, dup := seen[it.Reference]; dup {
			return nil, invalid(op, "item %d: duplicate reference %q in batch", i, it.Reference)
		}
		seen[it.Reference] = struct{}{}
		r, err := s.transferReq(it, amount, fee, "transfer")
		if err != nil {
			return nil, invalid(op, "item %d: %v", i, err)
		}
		r.SkipQueue, r.Inflight = false, false // controlled at batch level
		reqs = append(reqs, r)
		ids = append(ids, it.From, it.To)
	}
	if err := s.checkActive(ctx, op, ids...); err != nil {
		return nil, err
	}
	resp, err := s.c.CreateBulkTransactions(ctx, blnk.BulkTransactionRequest{
		Atomic: in.Atomic, Inflight: in.Pending, RunAsync: in.Async,
		SkipQueue: !s.cfg.UseQueue, Transactions: reqs,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return &BulkReceipt{BatchID: resp.BatchID, Status: resp.Status, Count: resp.TransactionCount}, nil
}

// ----- Reads ----------------------------------------------------------------

// Get returns a transaction by its Blnk id.
func (s transactionsAPI) Get(ctx context.Context, txID string) (*Receipt, error) {
	t, err := s.byID(ctx, txID)
	if err != nil {
		return nil, mapErr("ledger.Transactions.Get", err)
	}
	return s.toReceipt(t), nil
}

// GetByReference tries the reference as given, then with Blnk's "_q"
// suffix used for records produced by the queue.
func (s transactionsAPI) GetByReference(ctx context.Context, reference string) (*Receipt, error) {
	const op = "ledger.Transactions.GetByReference"
	t, err := s.byReference(ctx, reference)
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.toReceipt(t), nil
}

// History lists an account's transactions, newest first.
func (s accountsAPI) History(ctx context.Context, accountID string, q HistoryQuery) (*HistoryPage, error) {
	const op = "ledger.Accounts.History"
	if accountID == "" {
		return nil, invalid(op, "account id is required")
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	// balance_id is Blnk's virtual "source OR destination" field, so every
	// direction is one exactly-paginated query.
	field := "balance_id"
	switch q.Direction {
	case Incoming:
		field = "destination"
	case Outgoing:
		field = "source"
	}
	fs := append([]blnk.Filter{{Field: field, Operator: "eq", Value: accountID}}, s.historyFilters(q)...)
	txs, total, err := s.c.FilterTransactions(ctx, blnk.FilterRequest{
		Filters: fs, SortBy: "created_at", SortOrder: "desc",
		IncludeCount: true, Limit: q.Limit, Offset: q.Offset,
	})
	if err != nil {
		return nil, mapErr(op, err)
	}
	return s.page(txs, q, total), nil
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

func (s *Service) validateMove(op, account, ref string) error {
	switch {
	case account == "":
		return invalid(op, "account id is required")
	case strings.TrimSpace(ref) == "":
		return invalid(op, "reference (idempotency key) is required")
	}
	return nil
}

// baseReq sends the exact integer (precise_amount) plus its precision. Blnk
// derives and stores amount / amount_string from these, so the typed "0.456"
// round-trips exactly — Blnk's float `amount` request field is never used.
func (s *Service) baseReq(amount Money, ref, desc string, md map[string]any, kind string) blnk.CreateTransactionRequest {
	meta := blnk.MetaData{"txn_type": kind}
	for k, v := range md {
		meta[k] = v
	}
	return blnk.CreateTransactionRequest{
		PreciseAmount: blnk.BigFrom(amount.PreciseAmount),
		Precision:     amount.Precision,
		Reference:     ref,
		Currency:      amount.Currency,
		Description:   firstNonEmpty(desc, defaultDescription(kind)),
		SkipQueue:     !s.cfg.UseQueue,
		MetaData:      meta,
	}
}

// defaultDescription names a movement when the caller gave no description;
// Blnk rejects a transaction with a blank description.
func defaultDescription(kind string) string {
	if kind == "" {
		return "Transaction"
	}
	words := strings.Split(kind, "_")
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ") // "escrow_release" → "Escrow release"
}

// feeMeta records the fee and the net leg in both forms for reporting.
func feeMeta(md blnk.MetaData, fee, net Money) {
	md["fee"] = fee.Value
	md["fee_precise"] = fee.PreciseAmount.String()
	md["net_amount"] = net.Value
	md["net_precise"] = net.PreciseAmount.String()
}

func neg(m Money) Money {
	return Precise(new(big.Int).Neg(m.PreciseAmount), m.Precision, m.Currency)
}

func (s *Service) transferReq(in TransferInput, amount, fee Money, kind string) (blnk.CreateTransactionRequest, error) {
	req := s.baseReq(amount.add(fee), in.Reference, in.Description, in.MetaData, kind)
	req.Source = in.From
	req.AllowOverdraft = in.AllowOverdraft
	req.Inflight = in.Pending
	if !in.ScheduledFor.IsZero() {
		if in.Pending {
			return req, fmt.Errorf("a scheduled transfer cannot also be pending")
		}
		req.ScheduledFor = in.ScheduledFor.Format(time.RFC3339)
	}
	if fee.IsPositive() {
		req.Destinations = []blnk.Distribution{
			{Identifier: in.To, PreciseDistribution: amount.PreciseAmount.String(), Narration: firstNonEmpty(in.Description, "Transfer")},
			{Identifier: s.cfg.FeeIncomeAccount, PreciseDistribution: fee.PreciseAmount.String(), Narration: "Transfer fee"},
		}
		feeMeta(req.MetaData, fee, amount)
	} else {
		req.Destination = in.To
	}
	return req, nil
}

// post records a transaction and turns a duplicate reference into an idempotent
// replay when the stored transaction matches what was requested.
func (s *Service) post(ctx context.Context, op string, req blnk.CreateTransactionRequest) (*Receipt, error) {
	t, err := s.c.CreateTransaction(ctx, req)
	if err == nil {
		return s.toReceipt(t), nil
	}
	if !blnk.HasCode(err, blnk.CodeDuplicateReference) {
		return nil, mapErr(op, err)
	}
	existing, lookupErr := s.byReference(ctx, req.Reference)
	if lookupErr != nil {
		return nil, mapErr(op, err)
	}
	if !s.samePayload(ctx, existing, req) {
		return nil, mapErr(op, err) // same reference, different payload: a real conflict
	}
	r := s.toReceipt(existing)
	r.Replayed = true
	return r, nil
}

func (s *Service) checkActive(ctx context.Context, op string, ids ...string) error {
	if !s.cfg.EnforceAccountStatus {
		return nil
	}
	checked := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, "@") || checked[id] {
			continue // system accounts, or already checked
		}
		checked[id] = true
		b, err := s.c.GetBalance(ctx, id)
		if err != nil {
			return mapErr(op, err)
		}
		switch st, _ := b.MetaData["status"].(string); st {
		case "frozen", "blocked":
			return &OpError{Op: op, Kind: ErrAccountFrozen, Cause: fmt.Errorf("account %s is %s", id, st)}
		case "closed":
			return &OpError{Op: op, Kind: ErrAccountClosed, Cause: fmt.Errorf("account %s is closed", id)}
		}
	}
	return nil
}

func (s *Service) historyFilters(q HistoryQuery) []blnk.Filter {
	var fs []blnk.Filter
	if len(q.Statuses) == 1 {
		fs = append(fs, blnk.Filter{Field: "status", Operator: "eq", Value: blnkStatusOf(q.Statuses[0])})
	} else if len(q.Statuses) > 1 {
		vals := make([]any, len(q.Statuses))
		for i, st := range q.Statuses {
			vals[i] = blnkStatusOf(st)
		}
		fs = append(fs, blnk.Filter{Field: "status", Operator: "in", Values: vals})
	}
	if !q.Since.IsZero() {
		fs = append(fs, blnk.Filter{Field: "created_at", Operator: "gte", Value: q.Since.UTC().Format(time.RFC3339)})
	}
	if !q.Until.IsZero() {
		fs = append(fs, blnk.Filter{Field: "created_at", Operator: "lt", Value: q.Until.UTC().Format(time.RFC3339)})
	}
	return fs
}

func (s *Service) page(txs []blnk.Transaction, q HistoryQuery, total int) *HistoryPage {
	p := &HistoryPage{Limit: q.Limit, Offset: q.Offset, Total: total}
	for i := range txs {
		p.Items = append(p.Items, *s.toReceipt(&txs[i]))
	}
	return p
}

func (s *Service) inflightReceipt(txID string, r *blnk.UpdateInflightResponse) *Receipt {
	rc := s.toReceipt(&r.Transaction)
	if rc.TransactionID == "" {
		rc.TransactionID = txID
	}
	if r.Queued {
		rc.Queued = true
	}
	return rc
}

// txPrecision is the precision the transaction was recorded with (falls back to config).
func (s *Service) txPrecision(t *blnk.Transaction) int64 {
	if p, err := t.Precision.Int64(); err == nil && p > 0 {
		return p
	}
	return s.Precision(t.Currency)
}

// txMoney builds the amount in both forms from what Blnk stored.
func (s *Service) txMoney(t *blnk.Transaction) Money {
	return s.money(&t.PreciseAmount.Int, s.txPrecision(t), t.Currency)
}

func (s *Service) toReceipt(t *blnk.Transaction) *Receipt {
	precision := s.txPrecision(t)
	r := &Receipt{
		TransactionID:     t.GetID(),
		ParentTransaction: t.ParentTransaction,
		Reference:         t.Reference,
		Status:            t.Status,
		EntryStatus:       EntryStatusOf(t.Status),
		Amount:            s.txMoney(t),
		Source:            t.Source,
		Destination:       t.Destination,
		Description:       t.Description,
		CreatedAt:         t.CreatedAt.Time,
		MetaData:          t.MetaData,
		Queued:            t.Status == blnk.StatusQueued,
		Raw:               t,
	}
	for _, legs := range [][]blnk.Distribution{t.Sources, t.Destinations} {
		for _, d := range legs {
			leg := Leg{Account: d.Identifier, Distribution: d.Distribution, TransactionID: d.TransactionID, Narration: d.Narration}
			if d.PreciseDistribution != "" {
				if v, ok := new(big.Int).SetString(d.PreciseDistribution, 10); ok {
					leg.Amount = s.money(v, precision, t.Currency)
				}
				if leg.Distribution == "" {
					leg.Distribution = d.PreciseDistribution
				}
			}
			r.Legs = append(r.Legs, leg)
		}
	}
	return r
}

func (s *Service) toAccount(b *blnk.Balance) *Account {
	cur := b.Currency
	precision := s.Precision(cur)
	m := func(v *big.Int) Money { return s.money(v, precision, cur) }
	w := &Account{
		ID: b.BalanceID, LedgerID: b.LedgerID, IdentityID: b.IdentityID,
		Indicator: b.Indicator, Currency: cur, Precision: precision,
		Balance:    m(&b.Balance.Int),
		Available:  m(b.Available()),
		PendingIn:  m(&b.InflightCreditBalance.Int),
		PendingOut: m(&b.InflightDebitBalance.Int),
		Credits:    m(&b.CreditBalance.Int),
		Debits:     m(&b.DebitBalance.Int),
		Status:     "active",
		Kind:       CustomerAccount,
		CreatedAt:  b.CreatedAt.Time,
		MetaData:   b.MetaData,
	}
	if st, ok := b.MetaData["status"].(string); ok && st != "" {
		w.Status = st
	}
	if k, _ := b.MetaData[metaKind].(string); k != "" {
		w.Kind = AccountKind(k)
	}
	if strings.HasPrefix(b.Indicator, "@") {
		w.Kind = SystemAccount
	}
	return w
}

// clearing resolves the GL account for a deposit/withdrawal: explicit account,
// else the provider's clearing account, else the default.
func (s *Service) clearing(account, provider, fallback string) string {
	switch {
	case account != "":
		return account
	case provider != "":
		return ClearingAccount(provider)
	}
	return fallback
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func nilIfEmpty(m blnk.MetaData) blnk.MetaData {
	if len(m) == 0 {
		return nil
	}
	return m
}
