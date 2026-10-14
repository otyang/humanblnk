package ledger

import (
	"strings"
	"unicode"

	"github.com/otyang/humanblnk/blnk"
)

// Chart of accounts for the system (GL) side of the ledger. Customer accounts
// are the sub-ledger; these are the accounts finance reconciles against.
//
// Blnk creates an indicator balance the first time it is used, so none of these
// need setting up. Blnk debits the source and credits the destination, so a
// debit-normal account (asset) runs negative and a credit-normal one
// (liability, income) runs positive.
const (
	// FeeIncomeAccount: income. Every fee leg is credited here.
	FeeIncomeAccount = "@FeeIncome"

	// DepositClearingAccount: asset (receivable). Default deposit source when no
	// provider is named. Debited on deposit, cleared when the provider settles.
	DepositClearingAccount = "@DepositClearing"

	// PayoutClearingAccount: liability (payable). Default withdrawal destination
	// when no provider is named. Credited on withdrawal, cleared when funds leave the bank.
	PayoutClearingAccount = "@PayoutClearing"

	// MerchantPayableAccount: liability. Captured card-style holds owed to merchants.
	MerchantPayableAccount = "@MerchantPayable"

	// SuspenseAccount: unidentified or unmatched funds awaiting investigation.
	SuspenseAccount = "@Suspense"

	// FXPoolAccount: the house's currency position. Exchange debits the customer
	// into the pool in one currency and pays out of the pool in the other, so
	// each currency's @FXPool balance is the open position in that currency.
	FXPoolAccount = "@FXPool"

	// AdjustmentsAccount: counterparty for manual corrections (Adjust).
	AdjustmentsAccount = "@Adjustments"

	// BankOperatingAccount: asset. Your bank account; debited when a provider
	// settles collections to you, credited when you fund payouts.
	BankOperatingAccount = "@BankOperating"

	// ProviderFeesAccount: expense. Fees providers deduct at settlement.
	ProviderFeesAccount = "@ProviderFees"
)

// AccountKind tells customer accounts (the sub-ledger) from system (GL) accounts.
type AccountKind string

const (
	CustomerAccount AccountKind = "customer" // opened with Accounts().Open
	SystemAccount   AccountKind = "system"   // "@" indicator, e.g. @FeeIncome, @PaystackClearing
	PotAccount      AccountKind = "pot"      // savings pot under a customer account (OpenPot)
	EscrowAccount   AccountKind = "escrow"   // one per escrow agreement (Escrows().Open)
)

// metaKind is the meta_data key that marks pots and escrow accounts.
const metaKind = "account_kind"

// ClearingAccount returns the per-provider clearing account:
// ClearingAccount("paystack") == "@PaystackClearing". It also accepts "@Paystack"
// and "@PaystackClearing". One clearing account per provider means each
// provider's settlement report can be reconciled against its own balance.
func ClearingAccount(provider string) string {
	name := strings.TrimPrefix(strings.TrimSpace(provider), "@")
	name = strings.TrimSuffix(name, "Clearing")
	if name == "" {
		return DepositClearingAccount
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return "@" + string(r) + "Clearing"
}

// EntryStatus is the accounting view of Blnk's transaction status.
type EntryStatus string

const (
	EntryPosted    EntryStatus = "posted"    // Blnk APPLIED: in the balances
	EntryPending   EntryStatus = "pending"   // Blnk INFLIGHT: funds reserved, not posted
	EntryVoided    EntryStatus = "voided"    // Blnk VOID: pending entry cancelled, never posted
	EntryQueued    EntryStatus = "queued"    // Blnk QUEUED: accepted, not yet processed
	EntryScheduled EntryStatus = "scheduled" // Blnk SCHEDULED: will post at scheduled_for
	EntryRejected  EntryStatus = "rejected"  // Blnk REJECTED: refused (e.g. insufficient funds)
)

var toEntryStatus = map[string]EntryStatus{
	blnk.StatusApplied:   EntryPosted,
	blnk.StatusInflight:  EntryPending,
	blnk.StatusVoid:      EntryVoided,
	blnk.StatusQueued:    EntryQueued,
	blnk.StatusScheduled: EntryScheduled,
	blnk.StatusRejected:  EntryRejected,
}

// EntryStatusOf maps a Blnk status (APPLIED, INFLIGHT, ...) to an EntryStatus.
func EntryStatusOf(blnkStatus string) EntryStatus {
	if s, ok := toEntryStatus[strings.ToUpper(blnkStatus)]; ok {
		return s
	}
	return EntryStatus(strings.ToLower(blnkStatus))
}

// blnkStatusOf accepts either vocabulary ("posted" or "APPLIED") and returns Blnk's.
func blnkStatusOf(s string) string {
	for b, e := range toEntryStatus {
		if strings.EqualFold(s, string(e)) {
			return b
		}
	}
	return strings.ToUpper(s)
}
