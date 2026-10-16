# humanblnk

[![CI](https://github.com/otyang/humanblnk/actions/workflows/ci.yml/badge.svg)](https://github.com/otyang/humanblnk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/otyang/humanblnk.svg)](https://pkg.go.dev/github.com/otyang/humanblnk)
[![Go Report Card](https://goreportcard.com/badge/github.com/otyang/humanblnk)](https://goreportcard.com/report/github.com/otyang/humanblnk)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**A wallet and financial-operations domain layer for Go, built on top of [Blnk](https://docs.blnkfinance.com).**

**Author:** [Otyang](https://github.com/otyang) · Not affiliated with Blnk Finance.

**Guide:** [otyang.github.io/humanblnk](https://otyang.github.io/humanblnk/), a walkthrough built around a real wallet business. Every sample in it is a tested program.

**Examples:** [otyang.github.io/humanblnk/examples](https://otyang.github.io/humanblnk/examples/), a detailed page for every use case (creating accounts, deposits, card holds, escrow, settlement, reconciliation and the rest) with full code, the real output, the Blnk entries each one makes, and the errors to expect.

## What humanblnk is

A wallet business has two jobs. It moves its customers' money: sign-ups, top-ups, transfers, card payments, withdrawals, refunds. And it runs its own books: fees earned, money held by payment providers, settlements, unmatched payments, corrections, month-end close.

[Blnk](https://docs.blnkfinance.com) is excellent at the bottom of that stack. It is a double-entry ledger: balances, transactions, inflight amounts, and the guarantee that every debit has a matching credit. But Blnk deliberately knows nothing about your business. It has no idea what a customer, a fee, a KYC limit, a frozen account or a settlement is. Every wallet built on Blnk has to write that layer itself.

**humanblnk is that layer, written once.** It is the *domain layer*: the code that knows the business. Your app calls it in the business's own words, and it turns each call into correct Blnk entries:

```go
wallet.Withdraw(ctx, ledger.WithdrawInput{AccountID: ada, Amount: ledger.Amount("20000", "NGN"),
	Reference: "wd_01J8ZM7R2X", Provider: "flutterwave"})
```

Behind that one call: Ada's account and ledger are checked for freezes, her KYC limit is checked, the fee is priced by your rules (₦50 here), the money is reserved (not yet sent) as a Blnk inflight transaction with a fee leg to `@FeeIncome`, and the hold is set to cancel itself in 24 hours if Flutterwave never answers. Written against Blnk directly, that is a few hundred lines in your app. With humanblnk it is a rule the library already enforces, the same way, everywhere.

```
Your app            screens, HTTP handlers, jobs, provider webhooks        the product
   │  calls wallet.X / ops.X
humanblnk           the wallet and financial-operations domains            the business rules
   │                customers, fees, limits, status, approvals, reports
blnk (client)       typed HTTP, retries, structured errors, Blnk Cloud
   │  REST
Blnk Core           balances and double-entry transactions                 the ledger of record
```

### Who does what

| | Your app | humanblnk | Blnk Core |
|---|---|---|---|
| Talks to | customers, staff, payment providers | your app and Blnk | humanblnk |
| Knows about | screens, sessions, providers' APIs, KYC checks | customers, accounts, fees, limits, holds, disputes, settlements, house accounts | balances and transactions |
| Decides | what the customer asked for, and when | whether it is allowed, what it costs, which entries it becomes | whether the entries balance and the money is there |
| Stores | your own users and records | nothing of its own: everything lives in Blnk (status and links as metadata) | every balance and entry |

### Following one withdrawal through the layers

| Step | Layer | What happens |
|---|---|---|
| 1 | Your app | Ada taps "Withdraw ₦20,000 to GTBank". The app generates a reference once and calls `wallet.QuoteWithdraw`, then `wallet.Withdraw` when she confirms. |
| 2 | humanblnk | Refuses if Ada's account or its ledger is frozen or closed, or if she is over her tier's limit. Prices the fee with your `Config.Fees` (₦50 here). Builds the entries: Ada → `@FlutterwaveClearing` ₦20,000, Ada → `@FeeIncome` ₦50, held inflight with a 24-hour expiry. |
| 3 | Blnk Core | Checks Ada has ₦20,050 available, reserves it, and returns the inflight transaction. |
| 4 | Your app | Calls Flutterwave's transfer API with the transaction id as the reference. |
| 5 | Your app → humanblnk | Flutterwave's webhook says SUCCESSFUL: `wallet.Confirm`. FAILED: `wallet.Cancel`, and the money is Ada's again. |
| 6 | humanblnk → Blnk | Commits or voids the inflight transaction. A confirm carries no reference, so humanblnk retries it only when Blnk certainly didn't act: it can't post twice. |
| 7 | Later, finance | Flutterwave's settlement report: `ops.RecordSettlement` clears `@FlutterwaveClearing` against the bank. `ops.TrialBalance` proves the books still add up. |

### What humanblnk is not

- **Not a ledger.** It stores nothing itself. Blnk is the record; humanblnk can be restarted, scaled out or replaced without losing a cent.
- **Not a payment gateway.** It never calls Paystack, Flutterwave or a bank. Your app does, and tells humanblnk what happened.
- **Not a KYC or identity system.** You decide a customer's tier; humanblnk applies the limits you configured for it.
- **Not a full Blnk SDK.** The `blnk` package covers what the domain layer needs. For anything else in Blnk's API, call Blnk directly; the domain rules then don't apply to those calls.

## Two domains, two handles

| Handle | For | Its methods live in |
|---|---|---|
| **`l.Wallet()`** | what customers do: open accounts, add money, pay, cash out, hold, save, buy, dispute | `wallet_*.go` |
| **`l.Ops()`** | what the business does: provision ledgers, freeze and close, run its own accounts, settle with providers, park and claim unknown money, correct, prove the books, reconcile | `ops_*.go` |

Both come from `ledger.Ledger`, the interface your app depends on; `ledger.Service` implements it on Blnk. Code both domains share lives in `core_*.go`. Hand each part of your app only the handle it needs: a customer-facing server that holds a `ledger.Wallet` can't freeze or close accounts, adjust balances or record settlements, because those methods aren't on it.

```go
wallet, ops := l.Wallet(), l.Ops()
wallet.Deposit(ctx, …)         // wallet_payments.go
ops.RecordSettlement(ctx, …)   // ops_settlement.go
```

## What the domain layer adds

Blnk gives you primitives and leaves the rules to you. humanblnk is where those rules live:

| Concern | Blnk on its own | With humanblnk |
|---|---|---|
| Vocabulary | balances, transactions, indicators, inflight | customers, accounts, deposits, withdrawals, holds, pots, escrows, disputes, house accounts |
| Money | float `amount` or integer `precise_amount` | exact decimals typed as the user wrote them; fiat and crypto precision built in; rounding that never costs the house |
| Idempotency | per transaction reference | every operation takes a `Reference`; ledgers by `Key`; customers and accounts by `ExternalID` |
| Account status | stored, not enforced | frozen, closed and frozen-ledger states refuse money on every movement |
| Fees | none | every chargeable operation states its fee, or `Config.Fees` prices it; fees post as legs to `@FeeIncome` |
| Limits | none | per KYC tier and currency: per transaction, daily, monthly |
| Approvals | none | closing an account and manual adjustments need an approver who isn't the requester |
| Kinds of object | one balance type for everything | ledgers, customer accounts and house accounts are separate types; mix-ups fail with `ErrWrongKind` |
| Who can do what | anyone with the API key | customer-facing code gets `Wallet`; freezing, closing, adjusting and settling are only on `Ops` |
| Chart of accounts | free-form `@` indicators | `ledger.Chart`: each house account has a type (asset, liability, …) and a normal side |
| Previews | `dry_run` | `Quote*` calls: fee, total, balance after, and Blnk's own yes/no |
| Reports | balances and history | statements checked against Blnk, a trial balance, total customer funds |
| Testing | a running Core | `ledgertest`, an in-memory Core, running the real request code |

| Package | What it is |
|---|---|
| [`humanblnk`](https://pkg.go.dev/github.com/otyang/humanblnk) | the front door: `humanblnk.New(url, cfg)` and everything from `ledger`, re-exported |
| [`ledger`](https://pkg.go.dev/github.com/otyang/humanblnk/ledger) | the domain layer: the `Ledger` interface and `Service`, its Blnk implementation |
| [`blnk`](https://pkg.go.dev/github.com/otyang/humanblnk/blnk) | typed HTTP client for Blnk Core: `context` on every call, safe retries, structured errors, Blnk Cloud |
| [`ledgertest`](https://pkg.go.dev/github.com/otyang/humanblnk/ledgertest) | in-memory Blnk Core for your tests |
| `cmd/example` | end-to-end walkthrough (`make example`) |

## Requirements

- **Go 1.27** or later. No third-party dependencies.
- **Blnk Core 0.15.4** or later, self-hosted or on [Blnk Cloud](#blnk-cloud). Every CI run tests against Core **0.15.6** and 0.15.4.

**Stability:** humanblnk is pre-1.0. Minor versions may change the API; every change is listed in [CHANGELOG.md](CHANGELOG.md).

## Install

```bash
go get github.com/otyang/humanblnk@latest
```

## Quick start

```go
import "github.com/otyang/humanblnk"

l := humanblnk.New("http://localhost:5001", humanblnk.Config{LedgerID: "ldg_..."},
	humanblnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))

// At start-up: client configured, Core reachable, ledger exists.
if err := l.Check(ctx); err != nil {
	log.Fatal(err)
}

wallet, ops := l.Wallet(), l.Ops()

// Wallet: a customer signs up, tops up and pays a friend.
ada, err := wallet.OpenCustomer(ctx, humanblnk.CustomerInput{
	FirstName: "Ada", LastName: "Obi", Currencies: []string{"NGN"}, ExternalID: "user_8812",
})
adaNGN := ada.Accounts["NGN"]

_, err = wallet.Deposit(ctx, humanblnk.DepositInput{
	AccountID: adaNGN.ID, Amount: humanblnk.Amount("50000", "NGN"), Fee: humanblnk.Amount("750", ""),
	Reference: "paystack_T629384756", Provider: "paystack", // Paystack's reference: a repeated webhook is harmless
})

_, err = wallet.Transfer(ctx, humanblnk.TransferInput{
	From: adaNGN.ID, To: tundeNGN.ID, Amount: humanblnk.Amount("12500", "NGN"),
	Fee: humanblnk.NoFee, Reference: "p2p_01J8ZK2M4Q",
})
if errors.Is(err, humanblnk.ErrInsufficientFunds) {
	// tell Ada she doesn't have enough
}

// Financial operations: what the business earned, and whether the books add up.
fees, _ := ops.GetHouseAccount(ctx, humanblnk.HouseRef{Indicator: humanblnk.FeeIncomeAccount, Currency: "NGN"})
tb, _ := ops.TrialBalance(ctx, "NGN")
fmt.Println(fees.Natural.Display, tb.Balanced, tb.CustomerFunds.Display)
```

Provision the ledger itself once. `CreateLedger` is idempotent by `Key`, so running the job again returns the same ledger:

```go
ng, err := ops.CreateLedger(ctx, humanblnk.CreateLedgerInput{Key: "wallets-ng", Name: "Wallets Nigeria"})
// ng.ID goes in Config.LedgerID; ng.Replayed is true on a re-run
```

The root package re-exports package `ledger` with type aliases, so `humanblnk.Money` and `ledger.Money` are the same type. The examples below use `ledger`.

### Blnk Cloud

On Blnk Cloud you call Blnk's proxy with a Cloud API key and your instance ID:

```go
l := humanblnk.New(humanblnk.CloudProxyURL, cfg,
	humanblnk.WithAPIKey(os.Getenv("BLNK_CLOUD_KEY")),
	humanblnk.WithInstanceID("instance_abc123"))
```

Every request then carries `instance_id`. A proxy URL without an instance ID, or an ID that doesn't start with `instance_`, is a configuration error that `Check` reports at start-up. For every client, redirects to another host or from https to http are refused, so your API key never leaves the host you configured.

## The domain model

Three kinds of object, each with its own id type:

| Kind | Id type | Methods | Who owns it |
|---|---|---|---|
| Ledger: a group of customer accounts, e.g. one per market | `LedgerID` (`ldg_…`) | `ops.CreateLedger`, `GetLedger`, `ListLedgers`, `FreezeLedger` | the business |
| Customer account, and the pots and escrows under it | `AccountID` (`bln_…`) | `wallet.OpenAccount`, `GetAccount`, … ; `ops.FreezeAccount`, `CloseAccount` | a customer |
| House account: the business's own `@` accounts (`@FeeIncome`, `@PaystackClearing`, …) | `HouseRef{Indicator, Currency}` | `ops.GetHouseAccount`, `ListHouseAccounts`, `HouseStatement`, … | the business |

Each method works on its own kind only. Passing a ledger id to `ops.FreezeAccount` doesn't compile; a plain string converted to the wrong type is refused at run time with `ErrWrongKind`, before anything is written. House accounts change only through the domain's own operations, so their methods only read.

Every method, by file (full signatures in [CONTRACT.md](CONTRACT.md)):

```go
wallet := l.Wallet()                                                       // what customers do
wallet.OpenCustomer, GetCustomer, OpenPot, ListPots                        // wallet_customers.go
wallet.OpenAccount, GetAccount, FindAccounts, History                      // wallet_accounts.go
wallet.Statement, BalanceAt                                                // wallet_statements.go
wallet.LimitUsage                                                          // wallet_limits.go
wallet.Deposit, Withdraw, Transfer, Bulk                                   // wallet_payments.go
wallet.Split                                                               // wallet_split.go
wallet.Exchange                                                            // wallet_fx.go
wallet.Schedule                                                            // wallet_recurring.go
wallet.QuoteDeposit, QuoteWithdraw, QuoteTransfer, QuoteExchange           // wallet_quote.go
wallet.Hold, Confirm, Cancel, ConfirmMany, CancelMany                      // wallet_pending.go
wallet.GetTransaction, GetTransactionByReference, Refund, RefundPartial    // wallet_transactions.go
wallet.OpenEscrow, GetEscrow, ReleaseEscrow, RefundEscrow                  // wallet_escrow.go
wallet.OpenDispute, ResolveDispute                                         // wallet_dispute.go

ops := l.Ops()                                                             // what the business does
ops.CreateLedger, GetLedger, ListLedgers                                   // ops_ledgers.go
ops.FreezeAccount, UnfreezeAccount, CloseAccount, FreezeLedger, UnfreezeLedger // ops_controls.go
ops.GetHouseAccount, ListHouseAccounts, HouseHistory, HouseBalanceAt, HouseStatement // ops_house.go
ops.RecordSettlement                                                       // ops_settlement.go
ops.HoldInSuspense, ClaimFromSuspense, ReturnFromSuspense                  // ops_suspense.go
ops.Adjust                                                                 // ops_adjust.go
ops.TrialBalance                                                           // ops_reports.go
ops.WatchBalance, UnwatchBalance                                           // ops_watch.go
ops.CreateMatchingRule, StartReconciliation, GetReconciliation             // ops_reconcile.go
```

Helpers that don't call Blnk stay on `Service` (`core_*.go`): `Check`, `ComputeFee`, `Precision`, `Parse`, `Display` and the webhook functions.

## Money

You pass amounts as the decimal the user typed. Every `Receipt`, balance and fee leg comes back with both forms Blnk stores:

| You type | `Value` | `PreciseAmount` | `Precision` |
|---|---|---|---|
| `Amount("0.456", "BTC")` | `"0.456"` | `45600000` | `100000000` |
| `Amount("100.56", "USD")` | `"100.56"` | `10056` | `100` |
| `Amount("10000", "NGN")` | `"10000"` | `1000000` | `100` |

- Only `precise_amount` + `precision` are sent to Blnk, never its float `amount`, so `0.456` can't become `0.45599999`.
- `Money` marshals to JSON as `{"amount":"0.456","display":"0.456","currency":"BTC","precise_amount":"45600000","precision":100000000}` and unmarshals from `"0.456"` or a bare `0.456`, read from its literal text.
- More decimals than the currency allows, and exponents (`1e3`), are rejected before anything is sent.
- A fee written as `Amount("0.56", "")` takes the main amount's currency. `ledger.Minor(10056, "USD")` works if you already hold the integer.
- **`Display`** is for people: fiat and stablecoins padded (`"100.50"`), crypto trimmed and cut, never rounded up (`"0.456789"` for ETH), so a displayed balance can always be spent in full. Change it per currency with `Config.Display`.
- **Precision** is built in for BTC/LTC/BCH (1e8), ETH (1e18), SOL (1e9), USDT/USDC/TRX (1e6), zero-decimal fiat (JPY, XOF, …) and 3-decimal fiat (KWD, …); everything else is 100. Override with `Config.Precisions`, and never change a currency's precision once money has moved.

## Wallet domain

### Customers and accounts

| Operation | What it does |
|---|---|
| `wallet.OpenCustomer` / `GetCustomer` | a Blnk identity plus one account per currency; safe to retry with `ExternalID` (your user id) |
| `wallet.OpenPot` / `ListPots` | savings pots under a customer account, with an optional goal |
| `wallet.OpenAccount` / `GetAccount` | a single account. `OpenAccount` checks the ledger exists (`ErrLedgerNotFound`) and ignores the metadata keys this library manages (`status`, `account_kind`, `external_id`). With `ExternalID` a retry returns the same account |
| `wallet.FindAccounts` | by identity, currency, kind, status or any `meta_data` key. House accounts are never listed |
| `wallet.History` / `BalanceAt` / `Statement` / `LimitUsage` | activity newest first, posted balance at any moment, a checked statement, limit usage |

`Account` shows `Balance` (posted), `Available` (what the customer can spend now) and `PendingOut` (reserved by holds and pending withdrawals).

### Moving money

| Operation | Entries |
|---|---|
| `wallet.Deposit` | provider clearing (e.g. `@PaystackClearing`) → account; fee to `@FeeIncome`. `Pending: true` holds it for checks |
| `wallet.Withdraw` | account → provider clearing. **Pending by default** with a 24h auto-cancel: `wallet.Confirm` on provider success, `Cancel` on failure. Fee on top |
| `wallet.Transfer` | account → account; optional fee, `Pending`, `ScheduledFor` |
| `wallet.Split` | one payment to several recipients: fixed amounts, percentages and one `Rest`, computed exactly here |
| `wallet.Exchange` | currency conversion through `@FXPool`, both legs in one atomic batch. Give `Sell` or `Buy` and a `Rate`; rounding never costs the house |
| `wallet.Bulk` | many transfers at once; atomic, pending and async options, up to 10,000 |
| `wallet.Schedule` | a standing order: every occurrence as a scheduled transfer; references `ref_1 … ref_N` |
| `wallet.Hold` | a card-style authorisation with optional `ExpiresAt` / `CaptureAt` |
| `wallet.Confirm` / `Cancel` / `ConfirmMany` / `CancelMany` | capture or release, fully or partially (a transaction with a fee only in full, or leg by leg) |

Account fields take an `AccountID`. Some fields also accept a house account: `TransferInput.From`/`To`, `SplitInput.From`, `SplitPart.To`, `HoldInput.Destination`, `EscrowInput.Beneficiary` and `CloseInput.SweepTo`. Pass it as `ledger.AccountID("@Name")`, e.g. `ledger.AccountID(ledger.ClearingAccount("paystack"))`. The customer-only fields refuse it with `ErrWrongKind`: `DepositInput.AccountID`, `WithdrawInput.AccountID`, `HoldInput.AccountID`, `ExchangeInput.From`/`To`, `AdjustInput.AccountID`, `ClaimFromSuspenseInput.AccountID`, `EscrowInput.Payer` and `PotInput.ParentID`.

### Quotes: the confirm screen

`QuoteDeposit`, `QuoteWithdraw`, `QuoteTransfer` and `QuoteExchange` preview an operation without posting it: the amount, fee, total debit, what arrives, and the available balance before and after. They run the same domain checks as the real call (amounts, fee, account and ledger status, limits), then **Blnk dry-runs it** (`dry_run`, both legs for an exchange), so `Allowed` is Blnk's own answer. A refusal comes back as `Allowed: false` with a `Reason` (`ErrInsufficientFunds`, `ErrLimitExceeded`, `ErrAccountFrozen`, …); malformed input is an error.

### Commerce: refunds, disputes, escrow

| Operation | Entries |
|---|---|
| `wallet.Refund` | a full reversal, once. A payment with a fee or a split is reversed leg by leg, fee included. Refused if the recipient no longer has the money (`ErrInsufficientFunds`), or once partial refunds exist |
| `wallet.RefundPartial` | reverse transfer for part of the amount, linked by `meta_data.refund_of`; over-refunds refused |
| `wallet.OpenDispute` / `ResolveDispute` | holds the disputed amount in the recipient's account pointed back at the payer; `DisputeUpheld` returns it (partially if you like), `DisputeRejected` releases it |
| `wallet.OpenEscrow` / `ReleaseEscrow` / `RefundEscrow` / `GetEscrow` | buyer protection: each escrow is its own balance, so what's left is that balance and over-release is impossible |

### Fees and limits

**Every chargeable operation says what it charges.** Either set `Fee` on the call (`ledger.NoFee` for free), or set `Config.Fees` to price calls centrally. With neither, the call fails before reaching Blnk.

```go
svc := ledger.New(client, ledger.Config{
	Fees: func(ctx context.Context, r ledger.FeeRequest) (ledger.Money, error) {
		if r.Account != nil && r.Account.MetaData["tier"] == "premium" {
			return ledger.NoFee, nil
		}
		rule, err := pricing.Rule(ctx, r.Kind, r.Amount.Currency) // e.g. transfer/NGN → 0.5%, min 10, max 100
		if err != nil {
			return ledger.Money{}, err // refuses the call with ErrFeePricing
		}
		return ledger.ComputeFee(rule, r.Amount)
	},
})
```

- `FeeRequest` carries:
  - `Kind`: `deposit`, `withdrawal`, `transfer` or `exchange`;
  - `Account`: the paying customer account (nil for a house account);
  - `Amount`: resolved;
  - `Provider`;
  - `BuyCurrency`, for exchanges.
- A fee is always in the transaction's currency (an exchange's in the sell currency). Convert inside your function with `r.Convert(ledger.Amount("0.50", "USD"), rate, ledger.RoundUp)`.
- `FeeRule` + `ComputeFee`: percentage + fixed, min/max, tiers, rounding mode.
- A retry is priced again; if the price changed meanwhile, the retry gets `ErrDuplicateReference`. Retry with the original `Fee` to be safe.
- `Config.Limits`: per-transaction, daily and monthly caps per KYC tier (`meta_data.tier`) and currency → `ErrLimitExceeded`. They apply to money customers send: transfers, withdrawals, holds, splits, exchanges and escrow. Refunds, adjustments and other back-office movements neither are checked nor count. `Bulk` batches aren't checked but do count. A scheduled transfer counts on the day it posts. `wallet.LimitUsage` shows what's left, never below zero. Usage is read before posting, so two payments racing each other can both pass.

### Statements

- `wallet.Statement(account, from, to)`: opening balance, every posted entry with the balance after it, closing balance; `WriteCSV`. Entries are dated by when they took effect, and a fee is listed right after its payment. The total is checked against Blnk's historical balance, so a statement that doesn't add up is an error, never a wrong document.
- `wallet.BalanceAt`: posted balance at any moment.

## Financial-operations domain

### Ledgers

| Operation | What it does |
|---|---|
| `ops.CreateLedger` | idempotent by a required `Key` (stored as `meta_data.ledger_key`): an existing ledger with the key comes back with `Replayed`. Same key, different name: `ErrDuplicateReference`. If two processes race and both create, the oldest wins and the other is marked `duplicate` |
| `ops.GetLedger` / `ListLedgers` | read ledgers |
| `ops.FreezeLedger` / `UnfreezeLedger` | no money can leave the ledger's customer accounts (`ErrLedgerFrozen`); money can still come in |
| `Service.Check` | client configuration, Core health, and that `Config.LedgerID` exists. Call it at start-up |

### House accounts and the chart of accounts

House accounts are the business's own accounts ("house" is the business itself; everything else belongs to customers). They are the other side of every customer entry and what finance reconciles. In Blnk they are `@` indicator balances, kept in its general ledger and created on first use, one per currency. `ledger.Chart` gives each one its accounting meaning:

| Account | `Type` | `Normal` | Role |
|---|---|---|---|
| `@FeeIncome` | income | credit | every fee |
| `@<Provider>Clearing`, e.g. `@PaystackClearing` | asset | debit | money a provider collected or paid out for you, not yet settled with your bank. Any `@…Clearing` not in `Chart` is an asset |
| `@DepositClearing` | asset | debit | deposit source when no provider is named |
| `@PayoutClearing` | liability | credit | withdrawal destination when no provider is named |
| `@BankOperating` | asset | debit | your bank account |
| `@ProviderFees` | expense | debit | fees providers keep at settlement |
| `@FXPool` | asset | debit | open currency position from exchanges, one balance per currency |
| `@MerchantPayable` | liability | credit | captured holds owed onward |
| `@Suspense` | liability | credit | money that arrived but can't be matched yet |
| `@Adjustments` | equity | credit | counterparty of manual corrections |

`ops.GetHouseAccount` / `ListHouseAccounts` return `HouseAccount`, with its `Type`, `Normal` side and `Natural`, its balance on that side. Blnk debits the source and credits the destination, so debit-normal accounts run negative in `Balance`; `Natural` flips them, so every account reads positive in its usual state. `ops.HouseHistory`, `HouseBalanceAt` and `HouseStatement` work as for customer accounts. Add your own indicators to `Chart` at start-up, e.g. `ledger.Chart["@FlutterwaveClearing"] = ledger.GLDef{Type: ledger.GLLiability, Normal: ledger.CreditNormal}` for a payout-only provider.

`Provider: "paystack"` on a deposit or withdrawal uses `@PaystackClearing` (`ledger.ClearingAccount("paystack")`); `ClearingAccount: "@Exact"` overrides it. If you already have money on `@Fees` or `@World`, set `Config.FeeIncomeAccount`, `DepositClearingAccount` and `PayoutClearingAccount` so balances aren't split.

### Back office

| Operation | Entries |
|---|---|
| `ops.RecordSettlement` | clears a provider clearing account against `@BankOperating`, provider fees to `@ProviderFees`; atomic |
| `ops.HoldInSuspense` | money you can't match yet: clearing → `@Suspense` |
| `ops.ClaimFromSuspense` / `ReturnFromSuspense` | `@Suspense` → its owner's account, or back to the provider's clearing account |
| `ops.Adjust` | manual correction against `@Adjustments`; reason and an approver (not the requester) required |
| `ops.WatchBalance` / `UnwatchBalance` | balance alerts → `balance.monitor` webhook. Watch a customer account with `AccountID` or a house account with `House: ledger.HouseRef{...}` |
| `ops.CreateMatchingRule` / `StartReconciliation` / `GetReconciliation` | match a provider's report against the ledger: records inline (up to 10,000) or a CSV/JSON upload |

### Reports

- `ops.TrialBalance(currency)`: every balance summed (zero in double entry), every house account as `HouseAccount`, and `CustomerFunds`, the total you owe customers.
- `ops.HouseStatement` / `HouseBalanceAt`: the same checked statement and point-in-time balance as for customers, for any house account.

### Controls: freeze and close

Blnk itself ignores status, so this layer enforces it on **every** movement, including full refunds:

| | Effect |
|---|---|
| Account frozen (`ops.FreezeAccount`) | nothing in or out (`ErrAccountFrozen`) |
| Account closed (`ops.CloseAccount`) | nothing in or out, for good (`ErrAccountClosed`). For late money (a provider refund, say), hold it with `HoldInSuspense`, then claim it for the customer's new account or return it |
| Ledger frozen (`ops.FreezeLedger`) | nothing leaves its customer accounts, closure sweeps included (`ErrLedgerFrozen`); deposits in and house postings carry on |

`ops.CloseAccount` is final. It needs a `Reason` and an `ApprovedBy` other than the requester, and refuses while anything is pending or scheduled, while a pot under the account is open or an escrow on it still holds money. It sweeps the balance to `SweepTo`, then marks the account `closed`. A closed account can't be frozen or unfrozen.

Each movement reads the customer accounts it touches (one request each; `Bulk` reuses what it already read). It also checks the ledger of each account money leaves, using a cached status that refreshes every `Config.LedgerStatusTTL` (default 30s). Confirming a pending transaction checks again that the accounts receiving the money aren't frozen or closed; cancelling one is never refused, since it only returns reserved money. Some movements aren't checked: the ones Blnk posts by itself (scheduled transfers, auto-capture, hold expiry) and calls made to Blnk directly. That's why `CloseAccount` refuses while transfers are scheduled.

## Integration

### Webhooks

```go
http.Handle("/webhooks/blnk", svc.WebhookHandler(ledger.WebhookConfig{Secret: os.Getenv("BLNK_SECRET_KEY")},
	func(ctx context.Context, e *ledger.Event) error {
		switch e.Name {
		case ledger.EventTransactionApplied: // e.Receipt
		case ledger.EventTransactionVoid:    // also sent when a hold expires
		case ledger.EventBalanceAlert:       // e.Alert
		case ledger.EventBulkApplied:        // e.Bulk
		case ledger.EventBalanceCreated:     // e.Account, or e.House for an "@" account
		}
		return nil // an error answers 500 and Blnk retries
	}))
```

With `Secret` set, two things are checked: the signature (`X-Blnk-Signature`, HMAC-SHA256 of `timestamp.body`) and a 5-minute timestamp window. **Without a `Secret` nothing is verified.** Blnk delivers at least once, so make handlers idempotent.

### Observability

```go
var l ledger.Ledger = ledger.Instrument(svc, ledger.LogObserver(slog.Default()))
```

`Observer` gets every operation's name (`"Wallet.Transfer"`), context and error: enough for metrics or one OpenTelemetry span per call. Business refusals log at Warn, failures at Error.

### Testing your app: `ledgertest`

```go
svc, core := ledgertest.New(t, ledger.Config{})
acct, _ := svc.Wallet().OpenAccount(ctx, ledger.OpenAccountInput{Currency: "NGN"})
core.Advance(25 * time.Hour)          // expire holds, run scheduled transfers
for _, e := range core.Events() { … } // webhooks Blnk would have sent
```

`ledgertest` is an in-memory Blnk Core. Your tests run the real `Service` code, so they exercise the same requests production sends. It covers what this module uses (health, ledgers, balances, inflight, splits, refunds, bulk, filters, metadata, identities, monitors, historical balances, dry run, instant reconciliation, signed webhooks); it isn't a full Blnk.

## Behaviour worth knowing

- **Idempotency.**
  - Every movement needs a `Reference`. Use the provider's payment id for deposits.
  - A retry with the same reference and the same payload returns the original with `Replayed: true`.
  - A different payload under the same reference is `ErrDuplicateReference`.
- **Synchronous by default.** `skip_queue: true` is sent, so you get `APPLIED`/`INFLIGHT` or a definite error in the response. `Config.UseQueue` routes writes through Blnk's queue for very hot balances; results then arrive by webhook.
- **Retries.**
  - The client retries lock contention (423), rate limits (429), 502/503/504 and network errors, with jittered backoff. Retrying a payment is safe because of its reference.
  - Confirming a pending transaction has no reference, so it's retried only when Blnk certainly didn't act, never after a network error or gateway timeout.
  - Every call takes a `context.Context`: cancel it and the call stops.
- **Errors.** Check with `errors.Is(err, ledger.ErrInsufficientFunds)` and the other sentinels, mapped from Blnk's `error_detail.code`. `ErrWrongKind` also matches `ErrInvalidInput`; `ErrLedgerNotFound` also matches `ErrNotFound`. The raw `*blnk.APIError` stays reachable with `errors.As`.
- **Overdraft.** Only external sources (deposits, settlements, house counterparties) are sent with `allow_overdraft`; customer accounts never overdraft unless you set `AllowOverdraft`. Fields that must name the business's own accounts (settlement and suspense clearing, bank and fee accounts) refuse a customer account with `ErrWrongKind`, and a full refund checks the recipient still holds the money, because Blnk reverses with overdraft allowed.
- **Fee legs.** Blnk stores a movement with a fee only as its legs (`<ref>-1`, `<ref>-2`, …).
  - Lookups, retries and `Confirm`/`Cancel` rebuild the parent.
  - Disputes and partial refunds act on the recipient's leg. A split paying several recipients needs the leg's own id.
  - Statements list each leg.
  - Reconciling one-to-one won't match a provider line: try `Strategy: "one_to_many"` with `GroupingCriteria: "parent_transaction"`.
- **House account ids.** Blnk stores `@` accounts by balance id: receipts and history show the id, statements the `@Name`.
- **Descriptions** are required by Blnk; without one, the operation's name is used ("Deposit", "Escrow release", …).
- **Concurrent partial refunds** of one transaction can both pass the remaining-amount check; serialise them.
- **Ledger keys.** Calls with the same key run one at a time in a process. Across processes, a race can create two ledgers; the oldest then wins. Create ledgers at provisioning time.
- **Balance alerts** fire after every update while the condition holds; debounce them. Blnk's monitor threshold goes through float math, so `WatchBalance` sends it in the smallest unit with precision 1.

## Blnk Core versions

| | |
|---|---|
| Minimum | **0.15.4**: dry run (0.15.3) for quotes, and the current error codes |
| Tested on | **0.15.6** and 0.15.4, built from source in CI, with Postgres 16 and Redis 7 |

Blnk Core before 0.15.3 ignores `dry_run` and would post a quoted transaction, so it isn't supported.

## Running

```bash
make check                                                     # gofmt, vet, staticcheck, tests with -race (what CI runs)
make docs                                                      # regenerate docs.html and CONTRACT.md from the source
make live BLNK_LIVE_URL=http://localhost:5001                  # tests against a real Core (writes test data)
go run ./cmd/example                                           # walkthrough on the in-memory emulator
BLNK_URL=https://your-core BLNK_API_KEY=... go run ./cmd/example # on a real Core
```

There are three layers of tests: unit tests assert the exact request bodies sent to Blnk, flow tests run every operation against `ledgertest`, and live tests run against a real Core.

## Author

Created and maintained by **[Otyang](https://github.com/otyang)**.

Released under the [MIT License](LICENSE). Copyright (c) 2026 Otyang.

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). To report a security issue, see [SECURITY.md](SECURITY.md).

humanblnk is an independent project and is not affiliated with or endorsed by Blnk Finance. Blnk is a trademark of its owners.
