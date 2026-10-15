# humanblnk

[![CI](https://github.com/otyang/humanblnk/actions/workflows/ci.yml/badge.svg)](https://github.com/otyang/humanblnk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/otyang/humanblnk.svg)](https://pkg.go.dev/github.com/otyang/humanblnk)
[![Go Report Card](https://goreportcard.com/badge/github.com/otyang/humanblnk)](https://goreportcard.com/report/github.com/otyang/humanblnk)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Unofficial Go library that makes [Blnk](https://docs.blnkfinance.com), the open-source double-entry ledger, simple to use: accounts, deposits, withdrawals, transfers, fees, holds, refunds, escrow, FX, statements and webhooks. Exact decimal money for fiat and crypto, plus an in-memory Blnk emulator for tests.

**Author:** [Otyang](https://github.com/otyang) · Not affiliated with Blnk Finance.

**Guide:** [otyang.github.io/humanblnk](https://otyang.github.io/humanblnk/), a full walkthrough built around a real wallet app (onboarding, top-ups, transfers, withdrawals, cards, FX, crypto, marketplace, escrow, disputes, savings, payroll, limits, statements, settlement, webhooks, testing). Every sample in it is a tested program.

| Package | What it is |
|---|---|
| [`humanblnk`](https://pkg.go.dev/github.com/otyang/humanblnk) | the front door: `humanblnk.New(url, cfg)` and everything from `ledger`, re-exported |
| [`ledger`](https://pkg.go.dev/github.com/otyang/humanblnk/ledger) | the `Ledger` interface your app depends on, and `Service`, its Blnk implementation |
| [`blnk`](https://pkg.go.dev/github.com/otyang/humanblnk/blnk) | typed HTTP client for Blnk Core: one method per endpoint, retries, structured errors |
| [`ledgertest`](https://pkg.go.dev/github.com/otyang/humanblnk/ledgertest) | in-memory Blnk Core for your tests |
| `cmd/example` | end-to-end walkthrough (`make example`) |

## Install

```bash
go get github.com/otyang/humanblnk@latest
```

Go 1.22 or later; no third-party dependencies. Needs a [Blnk Core](https://docs.blnkfinance.com) to talk to (tested against 0.15), or use `ledgertest` in tests.

**Stability:** humanblnk is pre-1.0. Minor versions may change the API, and every such change is listed in [CHANGELOG.md](CHANGELOG.md).

## Quick start

```go
import "github.com/otyang/humanblnk"

l := humanblnk.New("http://localhost:5001", humanblnk.Config{LedgerID: "ldg_..."},
	humanblnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))

if err := l.Check(ctx); err != nil { // fails now if Config.LedgerID doesn't exist
	log.Fatal(err)
}

ada, err := l.Accounts().Open(ctx, humanblnk.OpenAccountInput{Currency: "NGN"})

// Paystack confirmed ₦50,000; their reference makes a repeated webhook harmless.
_, err = l.Payments().Deposit(ctx, humanblnk.DepositInput{
	AccountID: ada.ID, Amount: humanblnk.Amount("50000", "NGN"), Fee: humanblnk.Amount("750", ""),
	Reference: "paystack_T629384756", Provider: "paystack",
})

_, err = l.Payments().Transfer(ctx, humanblnk.TransferInput{
	From: ada.ID, To: tunde.ID, Amount: humanblnk.Amount("12500", "NGN"),
	Fee: humanblnk.NoFee, Reference: "p2p_01J8ZK2M4Q",
})
if errors.Is(err, humanblnk.ErrInsufficientFunds) {
	// tell Ada she doesn't have enough
}
```

Create the ledger itself once, when you provision; `Create` is idempotent by `Key`, so running it again returns the same ledger instead of a second one:

```go
ng, err := l.Ledgers().Create(ctx, humanblnk.CreateLedgerInput{Key: "wallets-ng", Name: "Wallets Nigeria"})
// ng.ID is the LedgerID to put in Config.LedgerID; ng.Replayed is true on a re-run
```

The root package re-exports package `ledger`, so `humanblnk.Money` and `ledger.Money` are the same type; import whichever reads better. The examples below use `ledger`.

## Amounts: type decimals, get both forms back

You pass amounts as the decimal the user typed. Every `Receipt`, `Account` balance and fee leg comes back with both forms Blnk stores:

| You type | `Value` | `PreciseAmount` | `Precision` |
|---|---|---|---|
| `Amount("0.456", "BTC")` | `"0.456"` | `45600000` | `100000000` |
| `Amount("100.56", "USD")` | `"100.56"` | `10056` | `100` |
| `Amount("10000", "NGN")` | `"10000"` | `1000000` | `100` |

- What's sent to Blnk is always `precise_amount` + `precision` (exact). Blnk's float `amount` request field is never used, so `0.456` can't turn into `0.45599999`. Blnk derives `amount`/`amount_string` for display from these.
- `Money` marshals to JSON as `{"amount":"0.456","display":"0.456","currency":"BTC","precise_amount":"45600000","precision":100000000}` (`precise_amount` is a string so JavaScript doesn't lose digits on 18-decimal tokens). It also unmarshals from your API's request body, accepting `"0.456"` or a bare `0.456` — read from its literal text, never through `float64`.
- More decimals than the currency allows (`"100.567"` USD) is rejected before anything is sent. Exponents (`1e3`) are rejected.
- A fee written as `Amount("0.56", "")` inherits the main amount's currency. Partial confirms resolve against the *held* transaction's stored precision.
- A `Money` that names its own `Precision` (from `Precise`, or a client's JSON) is converted to the currency's precision with its value kept, or rejected if it has more decimals than the currency allows; it is never posted at a precision of its own.
- `ledger.Minor(10056, "USD")` still works if you already have the integer.
- `Fixed()` renders all decimal places (`"0.45600000"`); `String()` renders `"0.456 BTC"`.

**Showing amounts to people: `display`.** Every `Money` the service returns also carries `Display`, also sent as `"display"` in JSON, formatted per currency. Compute with `amount` / `precise_amount`; show `display`.

| Currency | `amount` (exact) | `display` |
|---|---|---|
| NGN, USD (fiat) | `"100"`, `"100.5"` | `"100.00"`, `"100.50"`: padded to the currency's decimals |
| JPY | `"1500"` | `"1500"` |
| USDT, USDC | `"25.5"` | `"25.50"`: stablecoins read as money |
| BTC | `"0.456"` | `"0.456"`: up to 8 places, trailing zeros trimmed |
| ETH | `"0.4567896"` | `"0.456789"`: up to 6 places, trimmed |

Display is **cut, never rounded up**, so a displayed balance can always be spent in full ("withdraw all" never fails for a rounding cent). Change a currency's rule with `Config.Display`, e.g. `{"ETH": {Decimals: 4, TrimZeros: true}}`; currencies not listed in it or in `BuiltinDisplay` show every decimal place of their precision, padded. Thousands separators and currency symbols are left to your front end, since they depend on the user's locale.

**Precision per currency.** Built in: BTC/LTC/BCH 1e8, ETH 1e18, SOL 1e9, USDT/USDC/TRX 1e6, zero-decimal fiat (JPY, XOF, XAF, UGX, ...) 1, 3-decimal fiat (KWD, BHD, ...) 1000; everything else 100. Override with `Config.Precisions`. Never change a currency's precision once money has moved: Blnk sums `precise_amount` on balances, so mixing precisions on one balance corrupts it.

## The interface

`ledger.Ledger` is what your app depends on; `ledger.Service` implements it on Blnk. Full signatures and types: [CONTRACT.md](CONTRACT.md).

Methods are grouped into namespaces, each its own interface:

```go
l.Ledgers()         // Create, Get, List, Freeze, Unfreeze
l.Accounts()        // Open, Get, Find, Freeze, Unfreeze, Close, History, BalanceAt, Statement, LimitUsage
l.System()          // Get, List, History, BalanceAt, Statement
l.Customers()       // Open, Get, OpenPot, ListPots
l.Payments()        // Deposit, Withdraw, Transfer, Split, Exchange, Bulk, Schedule, QuoteDeposit, QuoteWithdraw, QuoteTransfer
l.Pending()         // Hold, Confirm, Cancel, ConfirmMany, CancelMany
l.Transactions()    // Get, GetByReference, Refund, RefundPartial
l.Disputes()        // Open, Resolve
l.Escrows()         // Open, Get, Release, Refund
l.Operations()      // RecordSettlement, ParkInSuspense, AllocateSuspense, ReturnSuspense, Adjust, TrialBalance, WatchBalance, Unwatch
l.Reconciliations() // CreateRule, Start, Get
```

`Ledgers()`, `Accounts()` and `System()` each work on one kind of object only and take their own id type (`LedgerID`, `AccountID`, `SystemRef`), so a ledger id can't be passed to `Accounts().Freeze`, nor a system account to `Accounts().Close`. A plain string converted to the wrong type is refused at run time with `ErrWrongKind` before anything is written.

A component that only needs one area can depend on that interface alone, e.g. a payout worker on `ledger.Pending`. Helpers that don't call Blnk stay on `Service`: `ComputeFee` (also a package function), `Precision`, `Parse`, and the webhook functions.

### Ledgers, accounts and customers

| Method | What it does in Blnk |
|---|---|
| `Ledgers().Create` | `POST /ledgers`, made idempotent by a required `Key` (stored as `meta_data.ledger_key`): an existing ledger with the key is returned with `Replayed`, so a ledger is never created twice. If two processes race, the oldest wins and the other is marked `duplicate` |
| `Ledgers().Get` / `Ledgers().List` | `GET /ledgers/{id}` / `GET /ledgers` |
| `Ledgers().Freeze` / `Ledgers().Unfreeze` | status in the ledger's `meta_data`. With `EnforceAccountStatus` on, no money can leave its customer accounts (`ErrLedgerFrozen`); money can still come in. Status is cached for `LedgerStatusTTL` (30s) |
| `Accounts().Open` / `Accounts().Get` | `POST /balances` / `GET /balances/{id}`. Open first checks the ledger exists (`ErrLedgerNotFound`). An account exposes `Balance`, `Available` (balance − pending debits), pending in/out and `Kind` (`customer`, `pot`, `escrow`). Set `ExternalID` (your user id) and retries return the same account |
| `Accounts().Find` | `POST /balances/filter` by identity, currency, kind, status or any `meta_data` key. System accounts are never listed |
| `Accounts().Freeze` / `Accounts().Unfreeze` / `Accounts().Close` | status in `meta_data`, enforced when `EnforceAccountStatus` is on; a closed account can't be frozen or unfrozen. Closing refuses while anything is pending, sweeps the balance to `SweepTo` (not out of a frozen ledger), then marks it `closed` |
| `System().Get` / `System().List` | `GET /balances/indicator/{@X}/currency/{CUR}`, e.g. `@FeeIncome`, as a `SystemAccount` with its `Type`, `Normal` side and `Natural` balance from `ledger.Chart`. `History`, `BalanceAt` and `Statement` work as for accounts |
| `Service.Check` | verifies `Config.LedgerID` exists; call it at startup |
| `Customers().Open` / `Customers().Get` | Blnk identity + one account per currency in one call; safe to retry with `ExternalID` |
| `Customers().OpenPot` / `Customers().ListPots` | savings pots under a customer account (same currency and identity), optional goal |

### Moving money

| Method | Entries |
|---|---|
| `Payments().Deposit` | provider clearing (e.g. `@PaystackClearing`) → account; fee split to `@FeeIncome`. `Pending: true` holds it for checks |
| `Payments().Withdraw` | account → provider clearing. **Pending by default** with a 24h auto-cancel: `Pending().Confirm` on provider success, `Pending().Cancel` on failure. Fee on top |
| `Payments().Transfer` | account → account; optional fee, `Pending`, `ScheduledFor` |
| `Payments().Split` | one payment to several recipients: fixed amounts, percentages and one `Rest`. Every share is computed exactly here; percentage dust goes to `Rest` |
| `Payments().Exchange` | currency conversion through `@FXPool`, both legs in one atomic batch. Give `Sell` or `Buy` and a `Rate`; rounding never costs the house |
| `Pending().Hold` | pending authorisation with optional `ExpiresAt` / `CaptureAt` |
| `Pending().Confirm` / `Pending().Cancel` (+ `Pending().ConfirmMany` / `Pending().CancelMany`, ≤ 100) | finish pending transactions, fully or partially (a transaction with a fee only in full, or leg by leg) |
| `Payments().Bulk` | `POST /transactions/bulk`; atomic, pending and async options, ≤ 10,000 |
| `Payments().Schedule` | creates every occurrence as a scheduled transfer (Blnk has no recurrence); references `ref_1 … ref_N` |

Account fields take an `AccountID`. Where a system account is also allowed (`TransferInput.From`/`To`, `SplitInput.From`, `SplitPart.To`, `HoldInput.Destination`, `EscrowInput.Beneficiary`, `CloseInput.SweepTo`), pass it as `ledger.AccountID("@Name")`, e.g. `ledger.AccountID(ledger.ClearingAccount("paystack"))`. The customer-only fields (`DepositInput.AccountID`, `WithdrawInput.AccountID`, `HoldInput.AccountID`, `ExchangeInput.From`/`To`, `AdjustInput.AccountID`, `AllocateInput.AccountID`, `EscrowInput.Payer`, `PotInput.ParentID`) refuse it with `ErrWrongKind`.

### Refunds, disputes, escrow

| Method | Entries |
|---|---|
| `Transactions().Refund` | Blnk's one-shot full reversal (`POST /refund-transaction/{id}`); refused once partial refunds exist |
| `Transactions().RefundPartial` | reverse transfer for part of the amount, linked by `meta_data.refund_of`; the linked refunds are summed and over-refunds are refused (as is a transaction already fully refunded); the original records `refunded_precise` |
| `Disputes().Open` / `Disputes().Resolve` | holds the disputed amount in the recipient's account pointed back at the payer; `DisputeUpheld` returns it (partially if you like), `DisputeRejected` releases it |
| `Escrows().Open` / `Escrows().Release` / `Escrows().Refund` / `Escrows().Get` | each escrow is its own balance, so what's left is that balance and Blnk itself stops over-release |

### Fees, quotes, limits

**Every chargeable call says what it charges.** `Payments().Deposit`, `Withdraw`, `Transfer`, `Exchange`, `Schedule`, each `Bulk` item and the quotes need a fee from one of two places:

1. `Fee` on the call. Use `ledger.NoFee` for a free transaction; it states the decision instead of leaving it out.
2. `Config.Fees`, a function that prices every call without an explicit `Fee`. It runs on each call, so prices can come from your database and change at runtime:

```go
svc := ledger.New(client, ledger.Config{
	Fees: func(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
		if r.Account != nil && r.Account.MetaData["tier"] == "premium" {
			return ledger.NoFee, nil
		}
		rule, err := pricing.Rule(ctx, r.Kind, r.Amount.Currency) // e.g. transfer/NGN → 0.5%, min 10, max 100
		if err != nil {
			return ledger.Money{}, err // refuses the transaction with ErrFeePricing
		}
		return ledger.ComputeFee(rule, r.Amount)
	},
})
```

With neither, the call fails with `ErrInvalidInput` before anything reaches Blnk, so a fee can't be forgotten. An explicit `Fee` always overrides `Config.Fees`. Internal flows (escrow, suspense, adjustments, refunds, closures) never charge fees and are unaffected.

- `FeeRequest` carries the `Kind` (`deposit`, `withdrawal`, `transfer`, `exchange`), the paying customer `Account` (nil for a system account), the resolved `Amount`, the `Provider` and, for exchanges, the `BuyCurrency`.
- Fees are always in the transaction's currency: a Blnk transaction has one currency, and a fee is one of its legs. Keep rules per kind and currency, since fixed amounts, minimums and caps don't carry across currencies. To price a fee set in another currency, convert it inside the function: `r.Convert(ledger.Amount("0.50", "USD"), rate, ledger.RoundUp)`. A fee returned in the wrong currency is refused with `ErrFeePricing`.
- Exchange fees are charged in the sell currency.
- `Schedule` decides the fee once, when the plan is created, and charges it on every occurrence.
- A retried call is priced again. If the price changed in between, the retry carries a different total and gets `ErrDuplicateReference` instead of a replay; retry with the original `Fee` to be safe.
- `FeeRule` + `ComputeFee`: percentage + fixed, min/max, tiers, rounding mode. `ledger.ComputeFee` works on a resolved amount such as `FeeRequest.Amount`; `Service.ComputeFee` takes an amount as typed.
- `Payments().QuoteDeposit` / `Payments().QuoteWithdraw` / `Payments().QuoteTransfer`: fee, total debit, net credit and available balance after, plus `Allowed`/`Reason` (insufficient funds, limit, frozen). With `Config.CoreDryRun` (Core 0.15.3+) Blnk also dry-runs it.
- `Config.Limits`: per-transaction, daily and monthly caps per KYC tier (`meta_data.tier`) and currency → `ErrLimitExceeded`, on money leaving customer accounts (transfers, withdrawals, holds, splits, exchanges, escrow; `Bulk` batches are not checked). `Accounts().LimitUsage` shows what's left. The check reads usage, then posts, so two payments racing each other can both pass it.

### Statements and reports

- `Accounts().Statement(account, from, to)` (and `System().Statement(ref, from, to)` for a system account): opening balance, every posted entry with running balance, closing balance; `WriteCSV`. Entries are dated by when they took effect (`effective_date`), so a standing order appears on the day it posted, and a fee is listed right after the payment it belongs to. The total is checked against Blnk's historical balance at `to`.
- `Accounts().BalanceAt(account, t)` / `System().BalanceAt(ref, t)`: posted balance at any moment (`GET /balances/{id}/at`).
- `Operations().TrialBalance(currency)`: every balance summed. Double entry means it nets to zero; also returns every system account (as `SystemAccount`, with its type and `Natural` balance) and total customer funds.

### Back office

| Method | Entries |
|---|---|
| `Operations().RecordSettlement` | clears a provider clearing account against `@BankOperating`, with provider fees to `@ProviderFees`; atomic |
| `Operations().ParkInSuspense` / `Operations().AllocateSuspense` / `Operations().ReturnSuspense` | unidentified money → `@Suspense` → its owner once known, or back to the provider's clearing account |
| `Operations().Adjust` | manual correction against `@Adjustments`; reason and an approver (not the requester) are required |
| `Reconciliations().CreateRule` / `Reconciliations().Start` / `Reconciliations().Get` | Blnk reconciliation: records inline (≤ 10,000) or a CSV/JSON upload |
| `Operations().WatchBalance` / `Operations().Unwatch` | Blnk balance monitors → `balance.monitor` webhook. Watch a customer account with `AccountID`, or a system account with `System: ledger.SystemRef{...}` |

### Webhooks

```go
http.Handle("/webhooks/blnk", svc.WebhookHandler(ledger.WebhookConfig{Secret: os.Getenv("BLNK_SECRET_KEY")},
    func(ctx context.Context, e *ledger.Event) error {
        switch e.Name {
        case ledger.EventTransactionApplied: // e.Receipt
        case ledger.EventTransactionVoid:    // also sent when a hold expires
        case ledger.EventBalanceAlert:       // e.Alert
        case ledger.EventBulkApplied:        // e.Bulk
        case ledger.EventBalanceCreated:     // e.Account, or e.System for an "@" account
        }
        return nil // an error answers 500 and Blnk retries
    }))
```

With `Secret` set, signatures (`X-Blnk-Signature` = HMAC-SHA256 of `timestamp.body` with Blnk's `server.secret_key`) and a 5-minute timestamp window are checked. **Without a `Secret` nothing is verified**, so make sure the variable is set in production. Blnk delivers at least once, so make handlers idempotent.

### Observability

```go
var l ledger.Ledger = ledger.Instrument(svc, ledger.LogObserver(slog.Default()))
```

`Observer` gets every call's name (`"Payments.Transfer"`), context and error, which is enough for metrics or an OpenTelemetry span per call.

### Testing your app: `ledgertest`

```go
svc, core := ledgertest.New(t, ledger.Config{})
acct, _ := svc.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})
core.Advance(25 * time.Hour)        // expire holds, run scheduled transfers
for _, e := range core.Events() { … } // webhooks Blnk would have sent
```

An in-memory Blnk emulator: your tests run the real `Service` code, so they exercise the same requests production sends. It covers the behaviour this module uses (ledgers, balances, inflight, splits, refunds, bulk, filters, metadata, identities, monitors, historical balances, dry run, instant reconciliation, signed webhooks); it is not a full Blnk.

## Naming: fintech methods, accounting accounts

Method names stay in product language (`Payments().Deposit`, `Payments().Withdraw`, `Payments().Transfer`, `Pending().Hold`, `Pending().Confirm`, `Pending().Cancel`, `Transactions().Refund`) so they match your app and specs. The system accounts use accounting names, because that is what finance reads and reconciles.

**Chart of accounts** (constants and `ledger.Chart` in `ledger/accounts.go`; Blnk creates each on first use, in its general ledger):

| Account | `Type` | `Normal` | Role |
|---|---|---|---|
| `@FeeIncome` | income | credit | Every fee leg is credited here |
| `@<Provider>Clearing`, e.g. `@PaystackClearing` | asset | debit | Debited on deposits collected by that provider, credited on withdrawals it pays out; cleared by `Operations().RecordSettlement`. Not in `Chart`: any `@…Clearing` is an asset unless you add it |
| `@DepositClearing` | asset | debit | Default deposit source when no provider is given |
| `@PayoutClearing` | liability | credit | Default withdrawal destination when no provider is given |
| `@BankOperating` | asset | debit | Your bank account |
| `@ProviderFees` | expense | debit | Fees providers deduct at settlement |
| `@FXPool` | asset | debit | Open currency position from `Payments().Exchange`, one balance per currency |
| `@MerchantPayable` | liability | credit | Captured holds owed to merchants |
| `@Suspense` | liability | credit | Unidentified or unmatched funds |
| `@Adjustments` | equity | credit | Counterparty of manual corrections |

`System().Get` returns these as `SystemAccount`, with `Type` and `Normal` from `Chart` (`ChartEntry` looks one up) and `Natural`, the balance on the normal side. A payout-only provider whose clearing account you'd rather read as a payable can be added: `ledger.Chart["@FlutterwaveClearing"] = ledger.GLDef{Type: ledger.GLLiability, Normal: ledger.CreditNormal}`.

Pass `Provider: "paystack"` on a deposit or withdrawal and the right clearing account is used (`ledger.ClearingAccount("paystack")` == `"@PaystackClearing"`). `ClearingAccount: "@Exact"` overrides it.

Blnk debits the source and credits the destination, so debit-normal accounts (assets, expenses) run negative in `Balance` and credit-normal ones (liabilities, income, equity) run positive; `Natural` flips debit-normal ones so both read positive in their usual state.

**Entry status.** Each receipt has Blnk's `Status` plus `EntryStatus`: `posted` (APPLIED), `pending` (INFLIGHT), `voided` (VOID), `queued`, `scheduled`, `rejected`. `HistoryQuery.Statuses` accepts either vocabulary.

**Already have money on `@Fees` / `@World`?** Set `Config.FeeIncomeAccount: "@Fees"` and `Config.DepositClearingAccount: "@World"` (and `PayoutClearingAccount`) so balances aren't split across old and new accounts.

## Behaviour worth knowing

- **Idempotency.** Every movement needs a `Reference`. If Blnk returns `TXN_DUPLICATE_REFERENCE` and the stored transaction has the same amount, currency, source, destination(s) and type, you get the original back with `Replayed: true`. A different payload under the same reference returns `ErrDuplicateReference`. Use the provider's payment id as the reference for deposits.
- **Synchronous by default.** `skip_queue: true` is sent so you get `APPLIED`/`INFLIGHT` or a definite error (e.g. `ErrInsufficientFunds`) in the response. Set `Config.UseQueue` for high-throughput hot balances; you then get `Queued: true` receipts and must confirm via webhooks. Atomic flows (`Payments().Exchange`, `Operations().RecordSettlement`) always run synchronously.
- **Overdraft.** Only external sources (deposits, settlements, system counterparties) are sent with `allow_overdraft`; customer accounts never overdraft unless you set `AllowOverdraft`.
- **Errors.** Check with `errors.Is(err, ledger.ErrInsufficientFunds)` etc. — mapped from Blnk's stable `error_detail.code` (Core 0.15.0+). The raw `*blnk.APIError` stays reachable with `errors.As` for logging. `ErrWrongKind` (an id of the wrong kind of object) also matches `ErrInvalidInput`; `ErrLedgerNotFound` also matches `ErrNotFound`.
- **Ledger freeze** (`Ledgers().Freeze`, with `EnforceAccountStatus`) stops money leaving the ledger's customer accounts, including closure sweeps; deposits in and system postings carry on. Each `Service` caches a ledger's status for `Config.LedgerStatusTTL` (default 30s), so a freeze made by another process applies within that time. Pending transactions created before the freeze can still be confirmed.
- **Ledger keys.** `Ledgers().Create` finds ledgers by `meta_data.ledger_key`. Calls with the same key run one at a time in one process; across processes, a race can create two ledgers, after which every caller gets the oldest and the other is marked `duplicate` (`DuplicateOf` names the one to use) and refused by `Accounts().Open`. Create ledgers at provisioning time to avoid it.
- **Retries.** The client retries 423 (lock contention), 429, 502/503/504 and network errors with jittered backoff. Retrying a transaction POST is safe because of reference idempotency. Confirming a pending transaction has no reference, so a commit is retried only when Blnk refused it (423, 429, 503), never after a network error or gateway timeout that it may have acted on: look the transaction up before confirming again.
- **History** filters on Blnk's `balance_id` (source or destination), so every query is one exactly paginated request.
- **Split transactions** (anything with a fee) are stored by Blnk only as their legs, `<ref>-1`, `<ref>-2`, … (`_1` on newer Core), each pointing at a parent id that has no record of its own. `Transactions().Get`, `GetByReference` and retries rebuild the parent from its legs, and `Pending().Confirm`/`Cancel` on the parent id act on every leg. Statements list each leg, so a fee shows as its own line. `Disputes().Open` and `Transactions().RefundPartial` given a payment with a fee act on the recipient's leg automatically; a split paying several recipients needs the leg's own transaction id. Blnk doesn't keep legs in request order; the library never relies on it.
- **Reconciling payments with a fee** one-to-one doesn't match a provider's line, because Blnk stores the legs separately. Try `Strategy: "one_to_many"` with `GroupingCriteria: "parent_transaction"` and check the counts against your Core version; Blnk reports counts, not matched lines.
- **System accounts** (`@FeeIncome`, …) are stored by Blnk under their balance id. Receipts and history show that id; statements show the `@Name`. Read them with `System()`, by `SystemRef{Indicator, Currency}`; `Accounts()` refuses them even by balance id.
- **Descriptions** are required by Blnk; when you don't give one, the movement's type is used ("Deposit", "Escrow release", …).
- **Partial refunds racing** each other on the same transaction can both pass the remaining-amount check; serialise refunds per transaction if they can run concurrently.
- **Partial confirms of a transaction with a fee** are refused: Blnk would apply the amount to every leg in turn and stop part-way. Confirm it in full, or confirm one leg by its transaction id.

## Running

```bash
make check                                                    # format, vet, staticcheck, tests with -race (what CI runs)
make docs                                                     # regenerate docs.html and CONTRACT.md from the source
go run ./cmd/example                                          # in-memory emulator, no setup
BLNK_URL=https://your-core BLNK_API_KEY=... go run ./cmd/example # a real Blnk Core
```

Unit tests assert the exact request bodies sent to Blnk; flow tests run every feature against `ledgertest`. To check against a real Core (it writes test data):

```bash
BLNK_LIVE_URL=http://localhost:5001 go test ./ledger -run TestLiveCore -v
```

## Blnk Core versions

| Needs | Feature |
|---|---|
| 0.8.4 | historical balances (`Accounts().BalanceAt`, `Accounts().Statement`); global webhooks |
| 0.10.0 | instant reconciliation (`Reconciliations().Start` with `Records`) |
| 0.10.1 | `from_source` historical balances (used for exact statements) |
| 0.13.0 | signed webhooks |
| 0.13.2 | filters (`Accounts().History`, `Accounts().Find`, `Operations().TrialBalance`, `System().List`, `Ledgers().Create`, limits, `ExternalID`) |
| 0.14.1 | auto-capture `CaptureAt` (with `BLNK_QUEUE_INFLIGHT_COMMIT` / `inflight_commit_queue`) |
| 0.14.2 | `Pending().ConfirmMany` / `Pending().CancelMany` |
| 0.15.0 | structured error codes (older versions fall back to HTTP status) |
| 0.15.3 | `Config.CoreDryRun`; refund descriptions. **Older Core ignores `dry_run` and posts the transaction**, so leave `CoreDryRun` off there |

Blnk quirks this module works around: balance monitors only match `==` (not `=`), and their threshold goes through float math, so `Operations().WatchBalance` sends it in the smallest unit with precision 1. Filters reset a `limit` over 100 to 20, so this module never asks for more than 100.

## Author

Created and maintained by **[Otyang](https://github.com/otyang)**.

Released under the [MIT License](LICENSE). Copyright (c) 2026 Otyang.

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). To report a security issue, see [SECURITY.md](SECURITY.md).

humanblnk is an independent project and is not affiliated with or endorsed by Blnk Finance. Blnk is a trademark of its owners.
