# blnkledger

A Go interface layer over the [Blnk](https://docs.blnkfinance.com/reference/overview) double-entry ledger: accounts, deposits, withdrawals, transfers, holds, refunds, bulk payouts and history.

```
blnk/     thin typed HTTP client — one method per Blnk Core endpoint, structured errors, retries
ledger/   domain layer — the `Ledger` interface your app depends on, implemented by `Service`
cmd/example/  end-to-end walkthrough against a real Core instance
```

No third-party dependencies. Go 1.22+.

## Quick start

```go
client := blnk.New("https://your-core.example.com", blnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))
var l ledger.Ledger = ledger.New(client, ledger.Config{LedgerID: "ldg_..."})

w, _ := l.Accounts().Open(ctx, ledger.OpenAccountInput{Currency: "NGN"})

l.Payments().Deposit(ctx, ledger.DepositInput{
    AccountID: w.ID, Amount: ledger.Amount("100.56", "USD"), Fee: ledger.Amount("0.56", ""),
    Reference: "paystack_" + paymentRef, Provider: "paystack",
})
l.Payments().Withdraw(ctx, ledger.WithdrawInput{AccountID: btcAccount, Amount: ledger.Amount("0.456", "BTC"), Fee: ledger.NoFee, Reference: "out-1"})
```

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
l.Accounts()        // Open, Get, GetSystem, Find, Freeze, Unfreeze, Close, History, BalanceAt, Statement, LimitUsage
l.Customers()       // Open, Get, OpenPot, ListPots
l.Payments()        // Deposit, Withdraw, Transfer, Split, Exchange, Bulk, Schedule, QuoteDeposit, QuoteWithdraw, QuoteTransfer
l.Pending()         // Hold, Confirm, Cancel, ConfirmMany, CancelMany
l.Transactions()    // Get, GetByReference, Refund, RefundPartial
l.Disputes()        // Open, Resolve
l.Escrows()         // Open, Get, Release, Refund
l.Operations()      // RecordSettlement, ParkInSuspense, AllocateSuspense, Adjust, TrialBalance, WatchBalance, Unwatch
l.Reconciliations() // CreateRule, Start, Get
```

A component that only needs one area can depend on that interface alone, e.g. a payout worker on `ledger.Pending`. Helpers that don't call Blnk stay on `Service`: `ComputeFee` (also a package function), `Precision`, `Parse`, and the webhook functions.

### Accounts and customers

| Method | What it does in Blnk |
|---|---|
| `Accounts().Open` / `Accounts().Get` | `POST /balances` / `GET /balances/{id}`. An account exposes `Balance`, `Available` (balance − pending debits), pending in/out and `Kind` (`customer`, `system`, `pot`, `escrow`). Set `ExternalID` (your user id) and retries return the same account |
| `Accounts().GetSystem` | `GET /balances/indicator/{@X}/currency/{CUR}`, e.g. `@FeeIncome` |
| `Accounts().Find` | `POST /balances/filter` by identity, currency, kind, status or any `meta_data` key |
| `Accounts().Freeze` / `Accounts().Unfreeze` / `Accounts().Close` | status in `meta_data`, enforced when `EnforceAccountStatus` is on. Closing refuses while anything is pending, sweeps the balance to `SweepTo`, then marks it `closed` |
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
| `Pending().Confirm` / `Pending().Cancel` (+ `Pending().ConfirmMany` / `Pending().CancelMany`, ≤ 100) | finish pending transactions, fully or partially |
| `Payments().Bulk` | `POST /transactions/bulk`; atomic, pending and async options, ≤ 10,000 |
| `Payments().Schedule` | creates every occurrence as a scheduled transfer (Blnk has no recurrence); references `ref_1 … ref_N` |

### Refunds, disputes, escrow

| Method | Entries |
|---|---|
| `Transactions().Refund` | Blnk's one-shot full reversal (`POST /refund-transaction/{id}`) |
| `Transactions().RefundPartial` | reverse transfer for part of the amount, linked by `meta_data.refund_of`; the original tracks `refunded_precise` and over-refunds are refused |
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
- `Config.Limits`: per-transaction, daily and monthly caps per KYC tier (`meta_data.tier`) and currency → `ErrLimitExceeded`. `Accounts().LimitUsage` shows what's left.

### Statements and reports

- `Accounts().Statement(account, from, to)`: opening balance, every posted entry with running balance, closing balance; `WriteCSV`. The total is checked against Blnk's historical balance at `to`.
- `Accounts().BalanceAt(account, t)`: posted balance at any moment (`GET /balances/{id}/at`).
- `Operations().TrialBalance(currency)`: every balance summed. Double entry means it nets to zero; also returns system account balances and total customer funds.

### Back office

| Method | Entries |
|---|---|
| `Operations().RecordSettlement` | clears a provider clearing account against `@BankOperating`, with provider fees to `@ProviderFees`; atomic |
| `Operations().ParkInSuspense` / `Operations().AllocateSuspense` | unidentified money → `@Suspense` → its owner once known |
| `Operations().Adjust` | manual correction against `@Adjustments`; reason and an approver (not the requester) are required |
| `Reconciliations().CreateRule` / `Reconciliations().Start` / `Reconciliations().Get` | Blnk reconciliation: records inline (≤ 10,000) or a CSV/JSON upload |
| `Operations().WatchBalance` / `Operations().Unwatch` | Blnk balance monitors → `balance.monitor` webhook |

### Webhooks

```go
http.Handle("/webhooks/blnk", svc.WebhookHandler(ledger.WebhookConfig{Secret: os.Getenv("BLNK_SECRET_KEY")},
    func(ctx context.Context, e *ledger.Event) error {
        switch e.Name {
        case ledger.EventTransactionApplied: // e.Receipt
        case ledger.EventTransactionVoid:    // also sent when a hold expires
        case ledger.EventBalanceAlert:       // e.Alert
        case ledger.EventBulkApplied:        // e.Bulk
        }
        return nil // an error answers 500 and Blnk retries
    }))
```

Signatures (`X-Blnk-Signature` = HMAC-SHA256 of `timestamp.body` with Blnk's `server.secret_key`) and a 5-minute timestamp window are checked. Blnk delivers at least once, so make handlers idempotent.

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

An in-memory Blnk emulator: your tests run the real `Service` code, so they exercise the same requests production sends. It covers the behaviour this module uses (balances, inflight, splits, refunds, bulk, filters, metadata, identities, monitors, historical balances, dry run, instant reconciliation, signed webhooks); it is not a full Blnk.

## Naming: fintech methods, accounting accounts

Method names stay in product language (`Payments().Deposit`, `Payments().Withdraw`, `Payments().Transfer`, `Pending().Hold`, `Pending().Confirm`, `Pending().Cancel`, `Transactions().Refund`) so they match your app and specs. The system accounts use accounting names, because that is what finance reads and reconciles.

**Chart of accounts** (constants in `ledger/accounts.go`; Blnk creates each on first use):

| Account | Type | Role |
|---|---|---|
| `@FeeIncome` | Income | Every fee leg is credited here |
| `@<Provider>Clearing`, e.g. `@PaystackClearing` | Asset (receivable) | Debited on deposits collected by that provider; cleared by `Operations().RecordSettlement` |
| `@<Provider>Clearing`, e.g. `@FlutterwaveClearing` | Liability (payable) | Credited on withdrawals paid out by that provider; cleared by `Operations().RecordSettlement` |
| `@DepositClearing` / `@PayoutClearing` | Asset / Liability | Defaults when no provider is given |
| `@BankOperating` | Asset | Your bank account |
| `@ProviderFees` | Expense | Fees providers deduct at settlement |
| `@FXPool` | Position | Open currency position from `Payments().Exchange`, one balance per currency |
| `@MerchantPayable` | Liability | Captured holds owed to merchants |
| `@Suspense` | Suspense | Unidentified or unmatched funds |
| `@Adjustments` | Equity / expense | Counterparty of manual corrections |

Pass `Provider: "paystack"` on a deposit or withdrawal and the right clearing account is used (`ledger.ClearingAccount("paystack")` == `"@PaystackClearing"`). `ClearingAccount: "@Exact"` overrides it.

Blnk debits the source and credits the destination, so debit-normal accounts (assets, expenses) run negative and credit-normal ones (liabilities, income) run positive.

**Entry status.** Each receipt has Blnk's `Status` plus `EntryStatus`: `posted` (APPLIED), `pending` (INFLIGHT), `voided` (VOID), `queued`, `scheduled`, `rejected`. `HistoryQuery.Statuses` accepts either vocabulary.

**Already have money on `@Fees` / `@World`?** Set `Config.FeeIncomeAccount: "@Fees"` and `Config.DepositClearingAccount: "@World"` (and `PayoutClearingAccount`) so balances aren't split across old and new accounts.

## Behaviour worth knowing

- **Idempotency.** Every movement needs a `Reference`. If Blnk returns `TXN_DUPLICATE_REFERENCE` and the stored transaction has the same amount, currency, source, destination(s) and type, you get the original back with `Replayed: true`. A different payload under the same reference returns `ErrDuplicateReference`. Use the provider's payment id as the reference for deposits.
- **Synchronous by default.** `skip_queue: true` is sent so you get `APPLIED`/`INFLIGHT` or a definite error (e.g. `ErrInsufficientFunds`) in the response. Set `Config.UseQueue` for high-throughput hot balances; you then get `Queued: true` receipts and must confirm via webhooks. Atomic flows (`Payments().Exchange`, `Operations().RecordSettlement`) always run synchronously.
- **Overdraft.** Only external sources (deposits, settlements, system counterparties) are sent with `allow_overdraft`; customer accounts never overdraft unless you set `AllowOverdraft`.
- **Errors.** Check with `errors.Is(err, ledger.ErrInsufficientFunds)` etc. — mapped from Blnk's stable `error_detail.code` (Core 0.15.0+). The raw `*blnk.APIError` stays reachable with `errors.As` for logging.
- **Retries.** The client retries 423 (lock contention), 429, 502/503/504 and network errors with jittered backoff. Retrying a transaction POST is safe because of reference idempotency.
- **History** filters on Blnk's `balance_id` (source or destination), so every query is one exactly paginated request.
- **Split transactions** (anything with a fee) are stored by Blnk only as their legs, `<ref>-1`, `<ref>-2`, … (`_1` on newer Core), each pointing at a parent id that has no record of its own. `Transactions().Get`, `GetByReference` and retries rebuild the parent from its legs, and `Pending().Confirm`/`Cancel` on the parent id act on every leg. Statements list each leg, so a fee shows as its own line; to refund or dispute one leg, use the leg's transaction id.
- **System accounts** (`@FeeIncome`, …) are stored by Blnk under their balance id. Receipts and history show that id; statements show the `@Name`.
- **Descriptions** are required by Blnk; when you don't give one, the movement's type is used ("Deposit", "Escrow release", …).
- **Partial refunds racing** each other on the same transaction can both pass the remaining-amount check; serialise refunds per transaction if they can run concurrently.

## Running

```bash
go test ./... -race
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
| 0.13.2 | filters (`Accounts().History`, `Accounts().Find`, `Operations().TrialBalance`, limits, `ExternalID`) |
| 0.14.1 | auto-capture `CaptureAt` (with `BLNK_QUEUE_INFLIGHT_COMMIT` / `inflight_commit_queue`) |
| 0.14.2 | `Pending().ConfirmMany` / `Pending().CancelMany` |
| 0.15.0 | structured error codes (older versions fall back to HTTP status) |
| 0.15.3 | `Config.CoreDryRun`; refund descriptions. **Older Core ignores `dry_run` and posts the transaction**, so leave `CoreDryRun` off there |

Blnk quirks this module works around: balance monitors only match `==` (not `=`), and their threshold goes through float math, so `Operations().WatchBalance` sends it in the smallest unit with precision 1. Filters reset a `limit` over 100 to 20, so this module never asks for more than 100.
