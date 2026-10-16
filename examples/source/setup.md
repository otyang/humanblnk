title: Install and connect
group: setup
order: 1
lead: Add the module, connect to Blnk Core or Blnk Cloud, check the setup at start-up, and hand each part of your app the one handle it needs.
---
## The situation

Oja runs a wallet app in Nigeria. Its Go services keep every balance in Blnk Core. Before the first customer signs up, Oja needs three things: the library in its module, a client that reaches Core, and a ledger for customer accounts. Then each service gets the one handle it should use.

## Install

```bash
go get github.com/otyang/humanblnk@latest
```

| Requirement | Version |
|---|---|
| Go | 1.27 or later. The module has no third-party dependencies. |
| Blnk Core | 0.15.4 or later, self-hosted or on Blnk Cloud. CI tests against 0.15.6 and 0.15.4. |

Core before 0.15.3 ignores `dry_run` and would post a quoted transaction, so it is not supported. The library is pre-1.0: minor versions may change the API, and every change is in `CHANGELOG.md`.

## Which constructor to use

| Situation | Use |
|---|---|
| The usual app | `humanblnk.New(url, cfg, opts...)`: one call, one import |
| You want the client too, e.g. to call `client.Health` or Blnk endpoints the library doesn't cover | `blnk.New(url, opts...)`, then `ledger.New(client, cfg)` |
| Tests | `ledgertest.New(t, cfg)`, an in-memory Core. See [Testing your app](testing.html) |

`humanblnk.New` builds the `blnk.Client` and calls `ledger.New` for you. The root package re-exports package `ledger` with type aliases, so `humanblnk.Config` and `ledger.Config` are the same type. `humanblnk.NewClient` and `humanblnk.NewService` are the two-step names.

## Provision the ledger once

Customer accounts live in a Blnk ledger. Create it once, from a provisioning job, and keep its id. `CreateLedger` is idempotent by `Key`, so running the job again returns the same ledger.

@sample provision | Oja creates its Nigerian ledger

- `Check` with no `Config.LedgerID` only checks the client and Core health.
- Store `ng.ID` in your configuration (for example `BLNK_LEDGER_ID`). Every service reads it into `Config.LedgerID`.
- More in [Provision ledgers](ledgers.html).

## Connect: humanblnk.New

@sample connect | Oja's API server at start-up

- The emulator here plays Core. In production `blnkURL` is your Core URL, such as `http://localhost:5001`, or `https://ENTERPRISE_PUBLIC_URL/core` for an Enterprise instance.
- `WithAPIKey` sets the `X-Blnk-Key` header. Core needs it when it runs in secure mode.
- The client is safe for concurrent use. Build one `Service` at start-up and share it.

In a real `main` it looks like this:

```go
svc := humanblnk.New(os.Getenv("BLNK_URL"),
	humanblnk.Config{LedgerID: humanblnk.LedgerID(os.Getenv("BLNK_LEDGER_ID"))},
	humanblnk.WithAPIKey(os.Getenv("BLNK_API_KEY")))
if err := svc.Check(ctx); err != nil {
	log.Fatal(err)
}
```

## Connect in two steps: blnk.New and ledger.New

Build the client yourself when you want to tune it or keep a reference to it.

@sample two-step

### Client options

| Option | What it changes | Default |
|---|---|---|
| `WithAPIKey(key)` | sends `X-Blnk-Key` on every request | no key |
| `WithInstanceID(id)` | the Blnk Cloud instance (`instance_…`), sent as the `instance_id` query parameter | none |
| `WithRetries(n, base)` | how often retryable failures are retried, with jittered backoff from `base` | 3 retries, 200ms |
| `WithHTTPClient(h)` | the `*http.Client` used | a client with a 30s timeout |
| `WithUserAgent(ua)` | the `User-Agent` header | `humanblnk-go/0.1.0` |

- Retried failures: 423 (lock contention), 429 (rate limit), 503 (queue backpressure), 502, 504 and network errors. Retrying a payment is safe because Blnk enforces its `Reference`.
- Confirming or cancelling a pending transaction carries no reference, so it is retried only on 423, 429 and 503, when Blnk certainly did nothing. It is never retried after a network error or a gateway timeout.
- Redirects are followed only to the same scheme and host, so the API key never goes anywhere else. Your own `http.Client` is copied, not changed.

## Blnk Cloud

On Blnk Cloud you call Blnk's proxy, `CloudProxyURL` (`https://api.cloud.blnkfinance.com/proxy`), with a Cloud API key and your instance id:

```go
svc := humanblnk.New(humanblnk.CloudProxyURL,
	humanblnk.Config{LedgerID: humanblnk.LedgerID(os.Getenv("BLNK_LEDGER_ID"))},
	humanblnk.WithAPIKey(os.Getenv("BLNK_CLOUD_API_KEY")),
	humanblnk.WithInstanceID(os.Getenv("BLNK_INSTANCE_ID"))) // "instance_…"
```

A proxy URL without an instance id, or an id that doesn't start with `instance_`, is a configuration mistake. Every call returns it, and `Check` reports it at start-up (see below).

## Check at start-up: svc.Check

`svc.Check(ctx)` verifies, in order:

1. The client is configured correctly: an `http://` or `https://` base URL, and a valid instance id for Blnk Cloud.
2. Core is up and reaches its database (`GET /health` answers `UP`).
3. `Config.LedgerID`, when set, is a ledger id, the ledger exists, and it isn't marked as a duplicate of another ledger.

### A wrong ledger id

@sample check-ledger

The error starts with `Config.LedgerID:` so the log says which setting is wrong. `ErrLedgerNotFound` also matches `ErrNotFound`, and `ErrWrongKind` also matches `ErrInvalidInput`.

### A wrong client setup

@sample check-client

### Core not reachable

@sample check-down

> note: Client configuration mistakes and an unreachable Core both come back from `Check` as `ErrUnavailable`. Log the message: it names the actual problem. Retrying won't fix a configuration mistake.

## Config fields

| Field | What it changes | Default |
|---|---|---|
| `LedgerID` | the ledger new customer accounts open in. `Check` verifies it | none; `OpenAccount` needs it unless the call sets `LedgerID` |
| `Fees` | a `FeeFunc` that prices every chargeable call with no explicit `Fee`. See [Fees](fees.html) | none: each call must set `Fee` (`ledger.NoFee` for free) |
| `Limits` | per-tier, per-currency caps on money leaving customer accounts. See [Limits](limits.html) | no limits |
| `Location` | where days and months start, for daily and monthly limits | UTC |
| `Display` | how amounts are shown, per currency. See [Money](money.html) | `BuiltinDisplay`, then every decimal place, padded |
| `Precisions` | smallest-unit multiplier per currency | `BuiltinPrecisions`, then `DefaultPrecision` |
| `DefaultPrecision` | precision for currencies in neither map | 100 |
| `FeeIncomeAccount` | where fees are credited | `@FeeIncome` |
| `DepositClearingAccount` | deposit source when no `Provider` is named | `@DepositClearing` |
| `PayoutClearingAccount` | withdrawal destination when no `Provider` is named | `@PayoutClearing` |
| `UseQueue` | send writes through Blnk's queue; results arrive by webhook | false: `skip_queue: true`, so you get the result in the response |
| `WithdrawalHoldTTL` | how long a pending withdrawal waits for the provider before it cancels itself; negative disables expiry | 24h |
| `LedgerStatusTTL` | how long a ledger's frozen or active status is cached; negative reads it every time | 30s |
| `Now` | the clock | `time.Now` |

> tip: If you already have money on older house accounts such as `@Fees` or `@World`, set `FeeIncomeAccount`, `DepositClearingAccount` and `PayoutClearingAccount` to them so balances aren't split across two accounts.

## Give each part of the app one handle

`svc` implements `ledger.Ledger`, which has two handles:

| Handle | For | Examples |
|---|---|---|
| `wallet := svc.Wallet()` | what customers do | open accounts, deposit, transfer, withdraw, hold, refund |
| `ops := svc.Ops()` | what the business does | freeze and close accounts, settle with providers, adjust, trial balance |

Hand each part of your app only the handle it needs. A customer-facing server that holds a `ledger.Wallet` can't freeze or close accounts, adjust balances or record settlements, because those methods aren't on it.

@sample handles | The API server gets the wallet, the back office gets ops

- Depend on the `ledger.Ledger`, `ledger.Wallet` and `ledger.Ops` interfaces, not on `*ledger.Service`. That lets you wrap the service with `ledger.Instrument` for logs and metrics ([Logging, metrics and tracing](observability.html)) or swap in the emulator in tests.
- Helpers that are not domain operations stay on `*ledger.Service`: `Check`, `Parse`, `Precision`, `Display`, `ComputeFee`, `ParseWebhook`, `DecodeEvent` and `WebhookHandler`.

## Errors

| Error | When | What to do |
|---|---|---|
| `ErrUnavailable` | a bad base URL; a Cloud proxy URL without a valid `instance_…` id; Core down or its database unreachable | read the message; fix the configuration or start Core |
| `ErrLedgerNotFound` | `Config.LedgerID` names no ledger | fix the id; run provisioning first |
| `ErrWrongKind` | `Config.LedgerID` holds another kind of id, such as a `bln_…` balance id | put the ledger id (`ldg_…`) there |
| `ErrInvalidInput` | `Config.LedgerID` names a ledger marked as a duplicate; the message names the one to use | switch to the ledger the message names |

## Next

- [Money and amounts](money.html): how to pass amounts.
- [Errors](errors.html): every error and what to tell the user.
- [Webhooks](webhooks.html): receive Blnk's events.
- [Create customer accounts](customer-accounts.html): the first wallet call.
