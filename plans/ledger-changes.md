# ledger-changes

Split the `ledger` package so each namespace operates on exactly one kind of
Blnk object: `Ledgers()` on ledgers, `Accounts()` on customer balances,
`System()` on `@` indicator balances. Treated as a new API: a clean break with
no deprecation wrappers, released as v0.2.0 (allowed pre-1.0 per CHANGELOG).

- **Branch:** `feature-refactor/ledger-count-status-field`
- **Status:** planned, not started

## Problem (as of v0.1.0)

- `Accounts().Get`, `Freeze`, `Unfreeze`, `Close`, `History`, `BalanceAt`,
  `Statement`, `LimitUsage` accept any balance id, including system (`@`)
  balances. `Close` only rejects ids starting with `@`, so a system balance
  passed by its `bln_…` id can be swept (e.g. all of `@FeeIncome`).
- `Freeze`/`Unfreeze` call Blnk's generic `POST /{id}/metadata` with no check,
  so they will also label a ledger, transaction or identity as frozen.
- `Accounts().Find` returns system balances mixed with customer ones.
- `Accounts().Open` copies caller metadata verbatim, so `account_kind` can be
  spoofed (`"system"`, `"pot"`, `"escrow"`).
- There is no ledger namespace; a ledger cannot be created, read or frozen
  through `ledger`, and nothing enforces a ledger's status.
- `GetSystem` returns an `*Account` with meaningless fields (`Status`,
  `IdentityID`, `Available`); debit/credit-normal sides exist only in comments.

## Decisions

| # | Question | Decision |
|---|---|---|
| 1 | What does a frozen ledger block? | **Outgoing money only** from its customer accounts, same as account freeze. Incoming money and internal postings to `@` system accounts still post. |
| 2 | Operations that legitimately target either kind of account (e.g. `AllocateSuspense` returning money to a clearing account) | **Yes, handle explicitly.** Proposed: split into separate methods/fields so each stays strictly typed (e.g. `AllocateSuspense` → customer `AccountID`; `ReturnSuspense` → `SystemRef`). Confirm split vs. union field before step 5. |
| 3 | Include `Ledgers().Create`? | **Yes.** |

## Target API

```go
type Ledger interface {
    Ledgers()  Ledgers   // Blnk ledgers only
    Accounts() Accounts  // customer balances (+ pots, escrows) only
    System()   System    // "@" indicator balances only
    Customers() Customers
    Payments()  Payments
    Pending()   Pending
    Transactions() Transactions
    Disputes()  Disputes
    Escrows()   Escrows
    Operations() Operations
    Reconciliations() Reconciliations
}
```

| Namespace | Methods | Accepts |
|---|---|---|
| `Ledgers()` | Create, Get, List, Freeze, Unfreeze | `LedgerID` (`ldg_…`) |
| `Accounts()` | Open, Get, Find, Freeze, Unfreeze, Close, History, BalanceAt, Statement, LimitUsage | `AccountID` (`bln_…`, non-`@`) |
| `System()` | Get, List, History, BalanceAt, Statement | `SystemRef{Indicator, Currency}` |

## Steps

### 1. Types — `ledger/ids.go`, `ledger/accounts.go`
- [ ] `type LedgerID string`, `type AccountID string`,
      `type SystemRef struct{ Indicator, Currency string }`, each with `Valid()`.
- [ ] `Ledger` result type (id, name, status, metadata, created at).
- [ ] `SystemAccount` type with `Type` (Asset/Liability/Income/Expense) and
      `Normal` (Debit/Credit); shared `Balances` struct embedded in both
      `Account` and `SystemAccount`.
- [ ] Chart of accounts becomes a `Chart` registry (indicator → type/normal side);
      clearing accounts typed by naming rule; `Config` overrides map onto it.
- [ ] Remove `SystemAccount` from `AccountKind` (customer, pot, escrow only).

### 2. Guards — `ledger/guard.go`
- [ ] `loadAccount(ctx, op, AccountID)`: prefix check, fetch, reject `@` indicator.
- [ ] `loadLedger(ctx, op, LedgerID)`: prefix check, fetch.
- [ ] `loadSystem(ctx, op, SystemRef)`: require `@`, fetch by indicator.
- [ ] Every namespace method loads through its guard; remove the ad-hoc `@`
      check in `Close`.

### 3. Namespaces
- [ ] `Accounts()`: all methods take `AccountID` and use `loadAccount`;
      `Find` always excludes indicator balances; `Open` strips `account_kind`
      and `status` from caller metadata; remove `GetSystem`.
- [ ] `System()`: new; `History`/`BalanceAt`/`Statement` share private helpers
      (keyed by balance id) with `Accounts()`.
- [ ] `Ledgers()`: new; `Create`/`Get` on existing client calls; add
      `ListLedgers` to `blnk/api.go`; `Freeze`/`Unfreeze` write ledger metadata.

### 4. Ledger freeze enforcement
- [ ] `checkActive` also checks the status of each **source** customer
      balance's ledger (outgoing only, per decision 1).
- [ ] Short-lived cache of ledger status keyed by ledger id; document the
      staleness window.
- [ ] `@` system accounts are never blocked by ledger status.

### 5. Callers
- [ ] `Customers`, `Escrows`, pots, `Payments`, `Pending`, `Disputes`,
      `Operations` inputs take `AccountID` where they now take `string`.
- [ ] Mixed-target operations per decision 2 (split `AllocateSuspense` etc.).
- [ ] `TrialBalance.System` → `[]*SystemAccount`.
- [ ] Events: add `Ledger` / `SystemAccount` payloads where relevant.
- [ ] `instrument.go`: Observer wrappers for `Ledgers()` and `System()`.

### 6. Fake server and tests
- [ ] `ledgertest`: ledger endpoints (create, get, list, metadata).
- [ ] Update the existing test suite to the new types.
- [ ] New tests:
  - ledger id → `Accounts().Freeze` rejected
  - system balance id → `Accounts().Get/Close/Statement/LimitUsage` rejected
  - `Find` excludes system balances
  - spoofed `account_kind` in `Open` ignored
  - frozen ledger blocks outgoing transfer/withdrawal
  - frozen ledger still allows deposits into its accounts
  - fees and settlements to `@` accounts still post while ledger frozen
  - `Ledgers().Create/Get/List/Freeze/Unfreeze` round trip

### 7. Docs and release
- [ ] Update `cmd/example`, README, `docs.html`; regenerate `CONTRACT.md`
      (`scripts/gencontract.py`) and docs (`scripts/gendocs.sh`).
- [ ] `blnk/version.go` → `0.2.0`; CHANGELOG entry listing every breaking
      change under **Changed**.
- [ ] `go vet`, tests and Makefile CI targets pass before pushing.
