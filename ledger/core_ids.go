package ledger

import "strings"

// Each namespace takes its own kind of id, so one can't be passed where
// another is expected:
//
//	l.Ops().FreezeLedger(ctx, ledgerID, reason)   // LedgerID
//	l.Ops().FreezeAccount(ctx, accountID, reason) // AccountID
//	l.Ops().GetHouseAccount(ctx, ledger.HouseRef{Indicator: "@FeeIncome", Currency: "NGN"})
//
// A plain string still converts (AccountID(s)), so every namespace also checks
// at run time that the id names the kind of object it works on.

// AccountID is the balance id of a customer account, pot or escrow ("bln_…").
//
// Movement inputs that also accept a house account say so in their field
// comments; there an AccountID may hold an "@" indicator, e.g.
// AccountID(ledger.MerchantPayableAccount). Customer-account methods refuse house accounts.
type AccountID string

// LedgerID is the id of a Blnk ledger ("ldg_…").
type LedgerID string

// HouseRef names a house account: an "@" indicator in one currency.
// A house account exists once per currency, so both are needed.
type HouseRef struct {
	Indicator string // e.g. "@FeeIncome", "@PaystackClearing"
	Currency  string
}

// GeneralLedgerID is the ledger Blnk keeps house ("@") accounts in.
const GeneralLedgerID LedgerID = "general_ledger_id"

// String returns the id.
func (id AccountID) String() string { return string(id) }

// String returns the id.
func (id LedgerID) String() string { return string(id) }

// String returns "@Indicator/CUR".
func (r HouseRef) String() string { return r.Indicator + "/" + strings.ToUpper(r.Currency) }

// isIndicator reports whether the id is an "@" house account rather than a balance id.
func (id AccountID) isIndicator() bool { return strings.HasPrefix(string(id), "@") }

// Blnk prefixes every id with the kind of object it names. They let the
// namespaces turn away the wrong kind of id before asking Blnk.
var idPrefixes = map[string]string{
	"ldg_": "ledger", "txn_": "transaction", "idt_": "identity",
	"bulk_": "batch", "mon_": "balance monitor", "rule_": "matching rule",
}

// foreignKind names the kind of object id belongs to when it is not want,
// judged by its prefix; "" when it may be a want.
func foreignKind(id, want string) string {
	if strings.HasPrefix(id, "@") {
		if want == "house account" {
			return ""
		}
		return "house account"
	}
	if id == string(GeneralLedgerID) {
		if want == "ledger" {
			return ""
		}
		return "ledger"
	}
	if strings.HasPrefix(id, "bln_") {
		if want == "account" {
			return ""
		}
		return "account"
	}
	for p, kind := range idPrefixes {
		if strings.HasPrefix(id, p) && kind != want {
			return kind
		}
	}
	return ""
}
