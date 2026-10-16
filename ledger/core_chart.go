package ledger

import (
	"strings"
	"unicode"
)

// Chart of accounts for the house (GL) side of the ledger. Customer accounts
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

// AccountKind tells customer accounts from the sub-accounts opened under them.
// House (GL) accounts are a separate type, HouseAccount, read through Ops().GetHouseAccount and ListHouseAccounts.
type AccountKind string

const (
	CustomerAccount AccountKind = "customer" // opened with Wallet().OpenAccount
	PotAccount      AccountKind = "pot"      // savings pot under a customer account (OpenPot)
	EscrowAccount   AccountKind = "escrow"   // one per escrow agreement (Wallet().OpenEscrow)
)

// metaKind is the meta_data key that marks pots and escrow accounts. Callers
// can't set it: Wallet().OpenAccount drops it from their metadata.
const metaKind = "account_kind"

// GLType is the accounting type of a house account.
type GLType string

const (
	GLAsset     GLType = "asset"
	GLLiability GLType = "liability"
	GLEquity    GLType = "equity"
	GLIncome    GLType = "income"
	GLExpense   GLType = "expense"
	GLUnknown   GLType = "" // an indicator not in the chart
)

// NormalSide is the side a house account normally carries its balance on.
// Blnk debits the source and credits the destination, so a debit-normal
// account runs negative in Blnk and a credit-normal one positive.
type NormalSide string

const (
	DebitNormal  NormalSide = "debit"
	CreditNormal NormalSide = "credit"
)

// GLDef describes a house account in the chart.
type GLDef struct {
	Type   GLType
	Normal NormalSide
}

// Chart is the chart of accounts: what each house account is. Provider
// clearing accounts ("@<Provider>Clearing") are assets (money the provider
// owes you) unless listed here. Add your own indicators to it at start-up.
var Chart = map[string]GLDef{
	FeeIncomeAccount:       {GLIncome, CreditNormal},
	DepositClearingAccount: {GLAsset, DebitNormal},
	PayoutClearingAccount:  {GLLiability, CreditNormal},
	MerchantPayableAccount: {GLLiability, CreditNormal},
	SuspenseAccount:        {GLLiability, CreditNormal},
	FXPoolAccount:          {GLAsset, DebitNormal},
	AdjustmentsAccount:     {GLEquity, CreditNormal},
	BankOperatingAccount:   {GLAsset, DebitNormal},
	ProviderFeesAccount:    {GLExpense, DebitNormal},
}

// ChartEntry returns what indicator is: its Chart entry, else an asset for a
// provider clearing account, else GLUnknown.
func ChartEntry(indicator string) GLDef {
	if d, ok := Chart[indicator]; ok {
		return d
	}
	if strings.HasPrefix(indicator, "@") && strings.HasSuffix(indicator, "Clearing") {
		return GLDef{GLAsset, DebitNormal}
	}
	return GLDef{}
}

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
