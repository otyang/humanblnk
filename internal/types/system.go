package types

import (
	"strings"
	"unicode"
)

// AccountRef names an account: a Blnk balance id, or a system account.
type AccountRef string

// IsSystem reports whether a is a system account, whose per-currency
// balance Blnk picks from the transaction's currency.
func (a AccountRef) IsSystem() bool { return strings.HasPrefix(string(a), "@") }

// System accounts. Blnk keeps them in its General Ledger and creates one per
// currency the first time a transaction uses it.
var (
	// World is money outside your books: where deposits come from and payouts go.
	World = SystemAccount("World")
	// FeeIncome is the fees you have earned.
	FeeIncome = SystemAccount("FeeIncome")
	// Adjustments is where staff corrections are posted from and to.
	Adjustments = SystemAccount("Adjustments")
)

// SystemAccount is one of your company's own accounts, by name ("Rewards" is
// "@Rewards"). Prefer the variables above, so a misspelt name doesn't
// silently create a new, empty account.
func SystemAccount(name string) AccountRef {
	return AccountRef("@" + name)
}

// ClearingAccount is money a payment provider owes you, or you owe it:
// ClearingAccount("paystack") is "@PaystackClearing". It returns to zero once
// the provider settles, so a balance left in it is money still in transit.
func ClearingAccount(provider string) AccountRef {
	r := []rune(strings.TrimSpace(provider))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return SystemAccount(string(r) + "Clearing")
}

// Reference builds a transaction reference from what the write is for and
// the ids that make it unique: Reference("payout", "po_123") is
// "payout:po_123". Build it from your own ids, never a random value, so a
// retry produces the same reference and Blnk refuses to apply it twice.
//
// The ":" separator keeps it apart from the suffixes Blnk adds itself ("-1"
// on each posting of a split, "_refund" on a rollback). Parts are joined as
// they are, so keep ":" out of them, or ("a:b", "c") and ("a", "b:c") are
// one reference. References starting "reversal:" are humanblnk's own, for
// Transactions.Reverse: don't build those yourself.
func Reference(kind string, parts ...string) string {
	return strings.Join(append([]string{kind}, parts...), ":")
}
