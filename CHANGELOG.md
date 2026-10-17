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
- Currencies moved to the new package `currency`. `Config.Display`,
  `Config.Precisions`, `BuiltinDisplay`, `BuiltinPrecisions` and
  `ledger.DisplayRule` are replaced by `Config.Currencies` (each entry a whole
  `currency.Currency`, replacing the built-in one) and `currency.Display`.
  `DisplayRule.Format(m)` is now `m.Format(rule)`. Precisions follow ISO 4217 minor units,
  so codes that used to fall back to 100 now get their own: CLP, ISK, VND
  and other zero-decimal currencies 1; IQD, JOD and LYD 1000.
- Unknown currency codes are refused with `ErrInvalidInput` by `OpenAccount`,
  `OpenCustomer`, `Parse` and every movement. Set
  `Config.UnknownCurrency: ledger.AllowUnknown` for the old behaviour.
  `Config.DefaultPrecision` applies only to such codes.
- A `Config.Currencies` entry with a mistake (say a mistyped `Kind`) makes
  its currency refused everywhere until it is fixed, instead of running on
  the built-in entry. `Kind` is accepted in any case.
- `Bulk` reports each item: `BulkReceipt.Items` (new `BulkItem`), and
  `Count` is the number of items Blnk recorded, not the number of
  transactions with their fee legs. A batch that isn't atomic and stops part
  way returns the error together with a receipt (`Status: "partial"`)
  listing what was posted. Sending the same batch again replays what was
  recorded and sends only the rest.
- `FreezeAccount` holds the account's available balance on the new house
  account `@FrozenFunds`, so scheduled transfers that fall due while it is
  frozen are rejected by Blnk. `UnfreezeAccount` releases it; `Confirm` and
  `Cancel` refuse it (`ErrWrongKind`).
- `Bulk` items count against their senders' limits: each item against the
  per-transaction cap, each sender's items together against the daily and
  monthly caps. A sender over a cap refuses the whole batch with
  `ErrLimitExceeded` before anything is sent. Before, bulk batches weren't
  checked, so a customer could send past their limits in one batch.
- Pots move money only to and from their parent account, and escrow
  accounts only through `ReleaseEscrow` and `RefundEscrow`. Deposits,
  withdrawals, transfers, holds, splits, exchanges and bulk items that use
  them otherwise are refused with `ErrWrongKind`.

### Added
- Package `currency`: every current ISO 4217 code plus BTC, ETH, SOL, TRX,
  LTC, BCH, XRP, ADA, DOGE, BNB, POL, USDT, USDC and DAI, each a constant
  (`currency.NGN`) with a name, symbol, kind, precision and display rule
  (`currency.Get`, `currency.All`). Symbols are unique (`A$`, `CA$`, `$`).
  `currency.New` defines your own. `svc.Currency(code)` and
  `svc.Currencies(kinds...)` return what a Service uses, overrides included.
- `svc.Check` reports mistakes in `Config.Currencies`.
- Example pages for every use case, in `examples/` and at
  https://otyang.github.io/humanblnk/examples/. Every sample on them runs in
  the test suite, and the output shown is what it printed.

### Fixed
- Scheduled transfers (`Schedule`, `TransferInput.ScheduledFor`) posted at
  once on Blnk Core: Core ignores `scheduled_for` on a transaction sent
  with `skip_queue`. They now always go through Blnk's queue. A standing
  order with `Count: 12` no longer takes all twelve payments on the day it
  is made. `GetTransaction` and `GetTransactionByReference` return a
  scheduled transfer's outcome once it has run, and `CloseAccount` no
  longer counts scheduled transfers that have already run.
- A payment could be paid back twice: disputed after a full refund, or
  disputed twice. Refunds, partial refunds and disputes now count against
  the same amount; a dispute with no `Amount` takes what is left.
- A dispute couldn't be opened when the recipient was frozen.
- Each account now records the precision it was opened with
  (`meta_data.precision`) and is always read at it, so changing a
  currency's precision no longer misreads existing balances. Movements on
  an account at another precision are refused.
- `Refund` didn't check that the transaction was posted.
- Amounts, rates and percentages accepted fractions (`"200/2"`), hex
  (`"0x10"`) and binary (`"0b11"`). Only plain decimals are accepted.
- A currency code with spaces (`"NGN "`) was checked trimmed but stored
  as given.
- `OpenEscrow` in a currency that isn't configured opened the escrow
  account before refusing; a retry with the same reference but another
  amount or beneficiary returned the old escrow.
- A ledger status read that started before this process froze or
  unfroze the ledger could cache the old status.
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
