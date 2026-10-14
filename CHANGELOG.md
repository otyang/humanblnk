# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Until v1.0.0, minor versions may
change the API; such changes are listed under **Changed**.

## [Unreleased]

### Added
- Import the module root: `import "github.com/otyang/humanblnk"`. Package
  `humanblnk` re-exports package `ledger` (types are aliases) and adds
  `humanblnk.New(blnkURL, cfg, opts...)`; a test keeps the re-exports complete.

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
- Guide with tested, real-world samples: https://otyang.github.io/humanblnk/

[Unreleased]: https://github.com/otyang/humanblnk/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/otyang/humanblnk/releases/tag/v0.1.0
