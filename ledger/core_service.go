// Package ledger is a wallet and financial-operations domain layer built on
// top of Blnk, the open-source double-entry ledger.
//
// Blnk is the ledger of record: balances, transactions and the guarantee that
// they balance. It knows nothing about the business. This package is the
// business layer between your app and Blnk. The wallet domain covers what
// customers do: accounts, deposits, withdrawals, transfers, card holds, splits,
// currency exchange, escrow, disputes, refunds and standing orders. The
// financial-operations domain covers what the business does: ledgers, house
// accounts, freezing and closing, settlement, suspense, adjustments, trial
// balance and reconciliation. Fees, limits, account and ledger status,
// approvals and exact money are enforced here, because Blnk leaves them to you.
// Nothing is stored here: every balance, entry and status lives in Blnk.
//
// Application code depends on the Ledger interface, which has one handle per
// domain. Each handle's methods live in the files with its prefix:
//
//	wallet := l.Wallet()   // wallet_*.go: what customers do
//	  customers   OpenCustomer, GetCustomer, OpenPot, ListPots
//	  accounts    OpenAccount, GetAccount, FindAccounts, History
//	  statements  Statement, BalanceAt          limits  LimitUsage
//	  payments    Deposit, Withdraw, Transfer, Bulk, Split, Exchange, Schedule
//	  quotes      QuoteDeposit, QuoteWithdraw, QuoteTransfer, QuoteExchange
//	  pending     Hold, Confirm, Cancel, ConfirmMany, CancelMany
//	  refunds     GetTransaction, GetTransactionByReference, Refund, RefundPartial
//	  escrow      OpenEscrow, GetEscrow, ReleaseEscrow, RefundEscrow
//	  disputes    OpenDispute, ResolveDispute
//
//	ops := l.Ops()         // ops_*.go: what the business does
//	  ledgers     CreateLedger, GetLedger, ListLedgers
//	  controls    FreezeAccount, UnfreezeAccount, CloseAccount, FreezeLedger, UnfreezeLedger
//	  house       GetHouseAccount, ListHouseAccounts, HouseHistory, HouseBalanceAt, HouseStatement
//	  settlement  RecordSettlement              adjust  Adjust
//	  suspense    HoldInSuspense, ClaimFromSuspense, ReturnFromSuspense
//	  reports     TrialBalance                  alerts  WatchBalance, UnwatchBalance
//	  reconcile   CreateMatchingRule, StartReconciliation, GetReconciliation
//
// Files prefixed core_ hold what both domains share.
//
// Service implements it on a Blnk Core instance (0.15.4 or later, self-hosted
// or on Blnk Cloud); package ledgertest provides an in-memory Blnk for tests.
// Amounts are exact decimals (see Money): never floats. Every movement takes a Reference, which makes retries safe. Every
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
	"github.com/otyang/humanblnk/currency"
)

// Service implements Ledger on top of a Blnk Core instance.
type Service struct {
	c   *blnk.Client
	cfg Config

	currencies map[string]currency.Currency // built-in list merged with Config.Currencies, fixed at New
	configErr  error                        // mistakes in Config, reported by Check

	ruleMu sync.Mutex
	ruleID string // default reconciliation matching rule, created on first use

	indicators sync.Map // "@Name|CUR" → balance id; indicator balances never move
	ledgers    sync.Map // LedgerID → *ledgerEntry; ledgers are never deleted
	ledgerKeys sync.Map // CreateLedgerInput.Key → *sync.Mutex, one Create per key at a time
}

// New builds a Service. The client is shared and safe for concurrent use.
func New(c *blnk.Client, cfg Config) *Service {
	cfg.defaults()
	s := &Service{c: c, cfg: cfg}
	s.currencies, s.configErr = buildCurrencies(cfg.Currencies)
	return s
}

// Parse converts a typed decimal ("0.456") to Money with both forms filled.
func (s *Service) Parse(amount, currency string) (Money, error) {
	if strings.TrimSpace(amount) == "" {
		return Money{}, fmt.Errorf("%w: amount is required", ErrInvalidInput)
	}
	if err := s.knownCurrency("ledger.Parse", currency); err != nil {
		return Money{}, err
	}
	m, err := Amount(amount, currency).resolve(s.Precision(currency))
	if err != nil {
		return Money{}, err
	}
	m.Display = s.Display(m)
	return m, nil
}

// resolve fills PreciseAmount/Precision (or Value) using the currency's precision.
// currency is used when m.Currency is empty (e.g. a fee written as Amount("1.50", "")).
func (s *Service) resolve(op string, m Money, currency string) (Money, error) {
	cur := strings.ToUpper(firstNonEmpty(m.Currency, currency))
	if cur != "" {
		if err := s.knownCurrency(op, cur); err != nil {
			return Money{}, err
		}
	}
	return s.resolveAt(op, m, cur, s.Precision(cur))
}

// resolveAt resolves m at precision, the precision it will be posted with.
// Blnk sums precise amounts on a balance whatever precision each transaction
// names, so a Money carrying another Precision (built with Precise, or decoded
// from a client's JSON) is converted with its value kept, or rejected when it
// has more decimal places than precision allows. It is never posted as is.
func (s *Service) resolveAt(op string, m Money, currency string, precision int64) (Money, error) {
	if m.Currency == "" {
		m.Currency = strings.ToUpper(currency)
	}
	if m.Currency == "" {
		return Money{}, invalid(op, "currency is required")
	}
	if m.Precision > 0 && m.Precision != precision {
		own, err := m.resolve(m.Precision) // checks Value and PreciseAmount agree
		if err != nil {
			return Money{}, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
		}
		m = Money{Value: own.Value, Currency: own.Currency}
	}
	m.Precision = 0
	r, err := m.resolve(precision)
	if err != nil {
		return Money{}, &OpError{Op: op, Kind: ErrInvalidInput, Cause: err}
	}
	r.Display = s.Display(r)
	return r, nil
}

// prepare validates a movement and resolves amount and fee to the same precision.
func (s *Service) prepare(op string, account AccountID, amount, fee Money, ref string) (Money, Money, error) {
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
		if f, err = s.resolveAt(op, fee, a.Currency, a.Precision); err != nil {
			return Money{}, Money{}, err
		}
		if f.Sign() < 0 {
			return Money{}, Money{}, invalid(op, "fee cannot be negative")
		}
	}
	return a, f, nil
}

func (s *Service) validateMove(op string, account AccountID, ref string) error {
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
	meta := blnk.MetaData{}
	for k, v := range md {
		meta[k] = v
	}
	// Set after the caller's metadata: limits and refunds rely on the kind.
	meta["txn_type"] = kind
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
	req.Source = string(in.From)
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
			{Identifier: string(in.To), PreciseDistribution: amount.PreciseAmount.String(), Narration: firstNonEmpty(in.Description, "Transfer")},
			{Identifier: s.cfg.FeeIncomeAccount, PreciseDistribution: fee.PreciseAmount.String(), Narration: "Transfer fee"},
		}
		feeMeta(req.MetaData, fee, amount)
	} else {
		req.Destination = string(in.To)
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

func (s *Service) toAccount(b *blnk.Balance) *Account {
	cur := b.Currency
	precision := s.Precision(cur)
	w := &Account{
		ID: AccountID(b.BalanceID), LedgerID: LedgerID(b.LedgerID), IdentityID: b.IdentityID,
		Currency: cur, Precision: precision,
		Balances:  s.balances(b, precision),
		Available: s.money(b.Available(), precision, cur),
		Status:    "active",
		Kind:      CustomerAccount,
		CreatedAt: b.CreatedAt.Time,
		MetaData:  b.MetaData,
	}
	if st, ok := b.MetaData["status"].(string); ok && st != "" {
		w.Status = st
	}
	if k := AccountKind(str(b.MetaData[metaKind])); k == PotAccount || k == EscrowAccount {
		w.Kind = k
	}
	return w
}

// balances reads a Blnk balance's figures at precision.
func (s *Service) balances(b *blnk.Balance, precision int64) Balances {
	m := func(v *big.Int) Money { return s.money(v, precision, b.Currency) }
	return Balances{
		Balance:    m(&b.Balance.Int),
		PendingIn:  m(&b.InflightCreditBalance.Int),
		PendingOut: m(&b.InflightDebitBalance.Int),
		Credits:    m(&b.CreditBalance.Int),
		Debits:     m(&b.DebitBalance.Int),
	}
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
