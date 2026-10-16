package ledger

import (
	"time"

	"github.com/otyang/humanblnk/currency"
)

// Config controls how the Service maps domain operations onto Blnk.
type Config struct {
	// LedgerID that customer accounts are created in (required for
	// Wallet().OpenAccount unless OpenAccountInput.LedgerID is set). Service.Check
	// verifies it exists.
	LedgerID LedgerID

	// Currencies changes built-in currencies (see package currency) or adds
	// your own. Each entry replaces the whole currency, so start a change
	// from the built-in one:
	//
	//	currency.MustGet(currency.ETH).WithDisplay(currency.Display{Decimals: 4, TrimZeros: true})
	//	currency.New("OJA", "Oja Points", currency.Fiat, "pts", 100)
	//
	// Mistakes are reported by Service.Check.
	Currencies []currency.Currency

	// UnknownCurrency says what happens to a code that is neither built in
	// nor in Currencies. Default RefuseUnknown.
	UnknownCurrency  UnknownCurrencyPolicy
	DefaultPrecision int64 // precision for unknown codes under AllowUnknown; default 100

	// House (GL) accounts. Defaults come from the chart of accounts in
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
	// outgoing transactions for the period (plus one GET per confirmation or
	// cancellation of a pending transaction among them).
	Limits []Limit
	// Location sets the day and month boundaries for limits. Default UTC.
	Location *time.Location

	// LedgerStatusTTL is how long a ledger's frozen/active status is cached
	// when checking movements. Default 30s; negative reads it every time.
	// Another process's Ops().FreezeLedger takes up to this long to apply here.
	LedgerStatusTTL time.Duration

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
	if c.LedgerStatusTTL == 0 {
		c.LedgerStatusTTL = 30 * time.Second
	}
	if c.WithdrawalHoldTTL == 0 {
		c.WithdrawalHoldTTL = 24 * time.Hour
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}
