# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Until v1.0.0, minor versions may
change the API; such changes are listed under **Changed**.

## [Unreleased]

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
