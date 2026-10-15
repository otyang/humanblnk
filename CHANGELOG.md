# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Until v1.0.0, minor versions may
change the API; such changes are listed under **Changed**.

## [Unreleased]

## [0.2.0] - 2026-10-15

Ledgers, customer accounts and system accounts are now separate namespaces,
each working on one kind of object only.

### Added
- `Ledgers()` namespace: `Create`, `Get`, `List`, `Freeze`, `Unfreeze`.
  `Create` is idempotent by a required `Key`, so a ledger is never created
  twice; if two processes race, the oldest ledger wins and the other is marked
  `duplicate` (and refused by `Accounts().Open`).
- Ledger freeze: with `EnforceAccountStatus` on, no money can leave the
  customer accounts of a frozen ledger (`ErrLedgerFrozen`); money can still
  come in, and system accounts are never blocked. `Config.LedgerStatusTTL`
  (default 30s) sets how long a ledger's status is cached.
- `System()` namespace: `Get`, `List`, `History`, `BalanceAt`, `Statement` for
  "@" accounts, returning `SystemAccount` with its `Type`, `Normal` side and
  `Natural` balance from the new `Chart` of accounts (`ChartEntry`, `GLDef`,
  `GLType`, `NormalSide`).
- Id types `AccountID`, `LedgerID` and `SystemRef`, and `GeneralLedgerID`.
- `Service.Check` verifies `Config.LedgerID` exists at startup.
- `Operations().ReturnSuspense` sends parked money back to the provider's
  clearing account.
- Errors `ErrLedgerNotFound` (also matches `ErrNotFound`), `ErrLedgerFrozen`,
  and `ErrWrongKind` (also matches `ErrInvalidInput`).
- `blnk.Client.ListLedgers` and `FilterLedgers`; the `ledgertest` emulator
  serves `GET /ledgers` and `POST /ledgers/filter`.

### Changed
- Every `Accounts()` method takes an `AccountID` and refuses ledger ids, other
  ids and system accounts with `ErrWrongKind`, before writing anything.
  Previously `Freeze`/`Unfreeze` would write status onto any Blnk object, and
  `Close` could sweep a system account given by its balance id.
- `Accounts().GetSystem` is removed: use `System().Get(ctx, SystemRef{...})`.
- `Accounts().Find` never lists system accounts; `AccountQuery.Kind` accepts
  customer, pot or escrow only.
- `Accounts().Open` checks the ledger exists (`ErrLedgerNotFound`), refuses
  the general ledger, and ignores the managed metadata keys (`status`,
  `account_kind`, `external_id`, ...) in `OpenAccountInput.MetaData`, so a
  kind or status can't be spoofed. `Freeze`/`Unfreeze` refuse closed accounts.
- `AccountKind` no longer has `SystemAccount`; that name is now the type of
  system accounts. `Account.Indicator` is removed. `Account` embeds the new
  `Balances` struct (same fields and JSON).
- `Config.LedgerID`, `OpenAccountInput.LedgerID` and `Account.LedgerID` are
  `LedgerID`; account ids in inputs and results are `AccountID` (`Account.ID`,
  deposit/withdraw/hold/transfer/split/exchange/escrow/pot/close/adjust/
  allocate fields, `Escrows()` and `Customers().ListPots` arguments). Fields
  that also accept a system account say so; pass it as `AccountID("@Name")`.
- Deposit, Withdraw, Hold, Exchange, AllocateSuspense, Adjust, escrow payers
  and pot parents refuse system accounts; send money back to a provider with
  `ReturnSuspense`. Adjust's `Counterparty` must be a system account.
- `TrialBalance.System` is `[]*SystemAccount`.
- `WatchInput` takes `AccountID` (customer) or `System` (a `SystemRef`).
- Webhooks: `balance.created` for a system account sets `Event.System`
  instead of `Event.Account`.

## [0.1.0] - 2026-10-14

First release.

### Added
- `ledger`: wallet operations on Blnk, grouped into namespaces — `Accounts`,
  `Customers`, `Payments`, `Pending`, `Transactions`, `Disputes`, `Escrows`,
  `Operations`, `Reconciliations`.
- Accounts and customers: open (idempotent with `ExternalID`), find, freeze,
  close with sweep, savings pots, history, point-in-time balances, statements
  with CSV export.
- Payments: deposits, withdrawals (pending until confirmed, auto-cancel),
  transfers, marketplace splits, atomic currency exchange, bulk transfers,
  standing orders, and quotes that run the same checks without moving money.
- Holds with partial capture; refunds (full and partial), disputes, escrow.
- Explicit fees: every chargeable call states its fee (`NoFee` for free) or
  prices it through `Config.Fees`; `FeeRule` with percent, fixed, min, max,
  tiers and rounding.
- Exact decimal `Money` for fiat and crypto, with per-currency `Display`.
- Limits per KYC tier and currency; account status enforcement.
- Back office: provider settlement, suspense, approved adjustments, trial
  balance, balance alerts, reconciliation.
- Webhooks: signature verification and typed events; `Instrument` and
  `LogObserver` for logs, metrics and tracing.
- `blnk`: typed client for Blnk Core with retries and structured errors.
- `ledgertest`: in-memory Blnk Core emulator that behaves like Core 0.15.
- Import the module root: `import "github.com/otyang/humanblnk"` re-exports
  package `ledger` (types are aliases) and adds `humanblnk.New(blnkURL, cfg, opts...)`.
- Safety checks verified against Blnk Core 0.15: amounts always post at their
  currency's precision; retries replay only the identical payment; partial
  confirms of a payment with a fee, over-refunds and over-disputes are refused;
  inflight commits are never blindly retried; webhook verification refuses an
  empty secret.
- `CONTRACT.md` and the guide are generated from the source and checked in CI.
- Guide with tested, real-world samples: https://otyang.github.io/humanblnk/

[Unreleased]: https://github.com/otyang/humanblnk/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/otyang/humanblnk/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/otyang/humanblnk/releases/tag/v0.1.0
