# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Until v1.0.0, minor versions may
change the API; such changes are listed under **Changed**.

## [Unreleased]

### Changed
- `ops.Adjust` and `ops.CloseAccount` require `RequestedBy` as well as
  `ApprovedBy`, and the two must be different people. Before, leaving
  `RequestedBy` empty skipped the check, so one person could approve their
  own correction or closure.

### Added
- Example pages for every use case, in `examples/` and at
  https://otyang.github.io/humanblnk/examples/. Every sample on them runs in
  the test suite, and the output shown is what it printed.

### Fixed
- A savings pot's goal had an empty `Display` when returned by `OpenPot` and
  `ListPots`.
- Money could reach a frozen or closed account: `Hold` now checks its
  `Destination`, and `Confirm` and `ConfirmMany` check the accounts receiving
  the money. `ConfirmMany` fails only the affected item. `Cancel` is still
  always allowed, and disputes are exempt, so money can be ring-fenced for a
  frozen payer and returned to them.
- A `txn_type` in a movement's `MetaData` replaced the library's own, which
  could keep a transfer out of limit usage. The library's value now wins.
- `RecordSettlement`, `HoldInSuspense` and `ReturnFromSuspense` accepted a
  customer account as a clearing, bank or fee account and could overdraw it.
  They now require house accounts (`ErrWrongKind` otherwise).
- `Refund` could take a customer account below zero, and failed for a payment
  with a fee or a split. It now refuses with `ErrInsufficientFunds` before
  reversing anything, reverses fee-carrying and split payments leg by leg, and
  a retry finishes any legs left.
- A refused `OpenEscrow` left an empty escrow account behind, and each retry
  opened another. The checks now run first, and a retry reuses the account.
- Limit usage counted refunds, adjustments and other back-office movements,
  never counted scheduled transfers on the day they posted, and could report a
  negative amount left. Fixed.
- `ExchangeQuote.Debit.Display` was empty; `Precise`, `ComputeFee` and
  `Convert` now fill `Display`. `Parse("")` and `ParseDecimal("")` returned
  zero instead of `ErrInvalidInput`.
- Some error messages repeated "invalid input", and wrong-kind errors read
  "is a account".
- Calling `Schedule` again after the first occurrence had run failed with
  "first occurrence must be in the future"; it now returns the plan.

## [0.1.0] - 2026-10-16

First release: a wallet and financial-operations domain layer for Go, built on
top of Blnk Core 0.15.4 or later (self-hosted or Blnk Cloud).

### Added
- **Two handles, one per domain.** `l.Wallet()` for what customers do and
  `l.Ops()` for what the business does. Give customer-facing code only a
  `Wallet`: freezing, closing, adjusting and settling are on `Ops`.
- **Wallet domain:** customers with one account per currency and savings pots;
  deposits, withdrawals (held until the provider confirms), transfers, bulk
  transfers, split payments, currency exchange and standing orders; quotes that
  Blnk dry-runs; card-style holds with partial capture; full and partial
  refunds; escrow; disputes; checked statements and point-in-time balances;
  KYC-tier limits; fee pricing through `Config.Fees`.
- **Financial-operations domain:** idempotent ledger provisioning by `Key`;
  house accounts with a chart of accounts (type, normal side, natural
  balance); freezing and closing accounts and ledgers, with approvals for
  closure; provider settlement; suspense for unidentified money; approved
  adjustments; trial balance with total customer funds; balance alerts;
  reconciliation against provider reports.
- **Rules Blnk leaves to you, enforced on every movement:** account and ledger
  status, limits, fees, idempotent references, exact decimal money for fiat and
  crypto, and separate types for ledgers, customer accounts and house
  accounts (`ErrWrongKind` on a mix-up).
- **`blnk` package:** a typed client for Blnk Core with `context` on every
  call, safe retries, structured errors and Blnk Cloud support.
- **Webhooks** as domain events with signature checks, and an `Observer` for
  logs, metrics and tracing.
- **`ledgertest`:** an in-memory Blnk Core for testing apps without Docker.
