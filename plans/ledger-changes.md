# ledger-changes

Split the `ledger` package so each namespace operates on exactly one kind of
Blnk object: `Ledgers()` on ledgers, `Accounts()` on customer balances,
`System()` on `@` indicator balances. Treated as a new API: a clean break with
no deprecation wrappers, released as v0.2.0 (allowed pre-1.0 per CHANGELOG).

- **Branch:** `feature-refactor/ledger-count-status-field`
- **Status:** implemented on `feature-refactor/ledger-count-status-field` (v0.2.0)

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
| 2 | Operations that legitimately target either kind of account (e.g. `AllocateSuspense` returning money to a clearing account) | **Yes, handle explicitly: split.** `AllocateSuspense` takes a customer `AccountID`; new `ReturnSuspense` sends money back to a provider's clearing account. |
| 3 | Include `Ledgers().Create`? | **Yes.** |
| 4 | Ledger creation | **Idempotent: no duplicate ledgers.** Required `Key`; same key returns the existing ledger. |
| 5 | Opening an account | **Validate the ledger exists first**; missing ledger → `ErrLedgerNotFound`. |

## Idempotent ledger creation

Blnk's `POST /ledgers` has no idempotency key and names aren't unique, so the
library provides it:

```go
type CreateLedgerInput struct {
    Key      string         // REQUIRED, unique, e.g. "wallets-ng"; stored as meta_data.ledger_key
    Name     string
    MetaData map[string]any
}
```

1. Look up `meta_data.ledger_key == Key` → found: return it (`Replayed = true`).
   Same key with a different name → conflict error, not a silent replay.
2. Not found: create with `ledger_key` in metadata.
3. Look up again: if a race produced several, the **oldest is canonical** and is
   returned to everyone; the others are tagged `status: duplicate`,
   `duplicate_of: <canonical id>` and refused by `Accounts().Open`.

Races are further reduced by an in-process `singleflight` per key, and the docs
recommend creating ledgers at provisioning/startup, not in request handlers.
Lookup uses a new `FilterLedgers` (`POST /ledgers/filter`); verify metadata
filtering against live Blnk first, falling back to paging the list.

## Ledger existence check on account open

- `Accounts().Open` loads its ledger through `loadLedger` before creating the
  balance: not found → `ErrLedgerNotFound`; `duplicate` → refused, naming the
  canonical ledger; `frozen` → allowed (freeze only blocks outgoing money).
- Positive results are cached for the process lifetime (ledgers aren't
  deleted); not-found is never cached.
- `Service.Check(ctx)` verifies `Config.LedgerID` at startup so a bad config
  fails at boot.
- Pots and escrows inherit an existing parent ledger; no extra check.

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
- [x] `type LedgerID string`, `type AccountID string`,
      `type SystemRef struct{ Indicator, Currency string }`, each with `Valid()`.
- [x] `Ledger` result type (id, key, name, status — active/frozen/duplicate —
      metadata, created at, `Replayed`).
- [x] `CreateLedgerInput` with required `Key`; new error kind `ErrLedgerNotFound`.
- [x] `SystemAccount` type with `Type` (Asset/Liability/Income/Expense) and
      `Normal` (Debit/Credit); shared `Balances` struct embedded in both
      `Account` and `SystemAccount`.
- [x] Chart of accounts becomes a `Chart` registry (indicator → type/normal side);
      clearing accounts typed by naming rule; `Config` overrides map onto it.
- [x] Remove `SystemAccount` from `AccountKind` (customer, pot, escrow only).

### 2. Guards — `ledger/guard.go`
- [x] `loadAccount(ctx, op, AccountID)`: prefix check, fetch, reject `@` indicator.
- [x] `loadLedger(ctx, op, LedgerID)`: prefix check, fetch.
- [x] `loadSystem(ctx, op, SystemRef)`: require `@`, fetch by indicator.
- [x] Every namespace method loads through its guard; remove the ad-hoc `@`
      check in `Close`.

### 3. Namespaces
- [x] `Accounts()`: all methods take `AccountID` and use `loadAccount`;
      `Find` always excludes indicator balances; `Open` strips `account_kind`
      and `status` from caller metadata; remove `GetSystem`.
- [x] `System()`: new; `History`/`BalanceAt`/`Statement` share private helpers
      (keyed by balance id) with `Accounts()`.
- [x] `Ledgers()`: new; `Get` on existing client call; add `ListLedgers` and
      `FilterLedgers` to `blnk/api.go`; `Freeze`/`Unfreeze` write ledger metadata.
- [x] `Ledgers().Create`: idempotent by `Key` — lookup, create, re-lookup
      (oldest wins, others tagged duplicate), `singleflight` per key.
- [x] `Accounts().Open`: validate ledger via `loadLedger` (existence cache).
- [x] `Service.Check(ctx)`: verify `Config.LedgerID` at startup.

### 4. Ledger freeze enforcement
- [x] `checkActive` also checks the status of each **source** customer
      balance's ledger (outgoing only, per decision 1).
- [x] Short-lived cache of ledger status keyed by ledger id; document the
      staleness window.
- [x] `@` system accounts are never blocked by ledger status.

### 5. Callers
- [x] `Customers`, `Escrows`, pots, `Payments`, `Pending`, `Disputes`,
      `Operations` inputs take `AccountID` where they now take `string`.
- [x] Mixed-target operations per decision 2 (split `AllocateSuspense` etc.).
- [x] `TrialBalance.System` → `[]*SystemAccount`.
- [x] Events: add `Ledger` / `SystemAccount` payloads where relevant.
- [x] `instrument.go`: Observer wrappers for `Ledgers()` and `System()`.

### 6. Fake server and tests
- [x] `ledgertest`: ledger endpoints (create, get, list, metadata).
- [x] Update the existing test suite to the new types.
- [x] New tests:
  - ledger id → `Accounts().Freeze` rejected
  - system balance id → `Accounts().Get/Close/Statement/LimitUsage` rejected
  - `Find` excludes system balances
  - spoofed `account_kind` in `Open` ignored
  - frozen ledger blocks outgoing transfer/withdrawal
  - frozen ledger still allows deposits into its accounts
  - fees and settlements to `@` accounts still post while ledger frozen
  - `Ledgers().Create/Get/List/Freeze/Unfreeze` round trip
  - `Create` twice with same key → one ledger, second call `Replayed`
  - same key, different name → conflict
  - simulated race → oldest ledger returned, duplicate refused by `Open`
  - `Open` on missing ledger → `ErrLedgerNotFound`
  - `Open` on frozen ledger → allowed
  - `Check` with bad `Config.LedgerID` → error

### 7. Docs and release
- [x] Update `cmd/example`, README, `docs.html`; regenerate `CONTRACT.md`
      (`scripts/gencontract.py`) and docs (`scripts/gendocs.sh`).
- [x] `blnk/version.go` → `0.2.0`; CHANGELOG entry listing every breaking
      change under **Changed**.
- [x] `go vet`, tests and Makefile CI targets pass before pushing.

## Notes from implementation

- Movement inputs that legitimately accept a system account (transfer, split,
  hold destination, escrow beneficiary, close sweep) keep `AccountID` and take
  it as `AccountID("@Name")`; the customer-only ones (deposit, withdraw, hold
  account, exchange, adjust, allocate, escrow payer, pot parent) refuse it.
- `WatchInput` takes either `AccountID` or `System SystemRef`.
- Ledger freeze also blocks `Accounts().Close` from sweeping money out.
- `Ledgers().Create` relies on `POST /ledgers/filter` (Core 0.13.2+ filters);
  not yet verified against a live Core.
