# Testing your service

**Scenario:** the airtime service from chapter 7 needs tests that run on every
`go test`: on a laptop, in CI, with no Blnk, Docker or network.

`humanblnktest` starts an **in-memory Blnk** for each test and gives you a
`humanblnk.Client` on it. Your code runs exactly as in production: the same
humanblnk, the same Blnk client, the same rules, against a Blnk that lives in
the test's memory and disappears when it ends.

```go
hb := humanblnktest.New(t)           // a fresh, empty Blnk, closed when t ends
ada := hb.Customer(t, "NGN")         // an owner and their wallet
hb.Fund(t, ada, "5000")              // a deposit from @World
air := airtime.New(hb.Holds, ...)    // hb embeds a *humanblnk.Client (hb.Client)
hb.Available(t, ada)                 // "4000.00"
```

## Step 1: fake what isn't yours

The airtime package depends on two things: humanblnk's holds, and a provider.
The holds are real, from the in-memory Blnk. Only the provider is faked: it
answers however the test says and counts its calls.

{{code 13-testing/airtime_test.go provider}}

## Step 2: test each outcome

A table of what the provider answers, and what Ada can spend afterwards.
Each case gets its own Blnk, so cases never see each other's money:

{{code 13-testing/airtime_test.go outcomes}}

## Step 3: test the retry

The bug that costs money is a retry that charges twice. The in-memory Blnk
refuses a reused reference the way Blnk does, so this tests the real thing:

{{code 13-testing/airtime_test.go retry}}

## Step 4: test the refusal

{{code 13-testing/airtime_test.go funds}}

## The whole test

{{file 13-testing/airtime_test.go}}

Run it:

```sh
go test ./docs/examples/13-testing
```

## How close is it to Blnk?

The in-memory Blnk copies the Blnk Core 0.15.6 behaviour humanblnk relies
on:

- System accounts (`@World`, `@FeeIncome`, clearing accounts) are made the
  first time a transaction names them.
- A reused reference is refused, so `ErrDuplicate` and `ErrReferenceUsed`
  come back as they would.
- A split is recorded a posting at a time. If a later posting fails, the
  earlier ones move and are refunded about 200 ms later, as Blnk's worker
  does. `humanblnktest.WithRollbackDelay` stretches that window, to test what
  your code sees inside it.
- Holds stay `INFLIGHT`; captures and releases are transactions of their own.
- Watches fire `balance.monitor`: `humanblnktest.WithWebhook(url, secret)`
  sends signed webhooks to your handler, so `VerifyWebhook` can be tested end
  to end. Delivery is best effort and gives up after 5 seconds, so a handler
  must answer rather than block (say, on a full channel).

humanblnk's contract tests run against both the in-memory Blnk and a real
one, so a difference in what they cover fails CI. Where they don't look,
it isn't Blnk:

- Nothing is ever queued: every write applies at once, and queued balances
  are always 0.
- Hold expiry happens on the first request after it, to the second.
- Splits take exact amounts (`precise_distribution`) only, and a batch can't
  hold a split. Batches, commits and voids can't be previewed.
- `effective_date` and backdating are ignored.
- Reconciliation matches `one_to_one` only.

humanblnk itself uses none of these, but a raw `blnk.Client` on `hb.URL`
could.

## Options

| Option | For |
| --- | --- |
| `WithCustomerLedger("Savings")` | another customer ledger; its id is `hb.CustomerLedgers["Savings"]` |
| `WithRollbackDelay(d)` | how long a failed split's applied part stays before its refund |
| `WithWebhook(url, secret)` | send Blnk's webhooks to a test server |
| `WithClientOptions(...)` | pass options to `humanblnk.New`, such as `WithAccountCacheSize` |

Keep a test against a real Blnk too, for the behaviour your service relies
on most: a few in CI with `docker compose -f deploy/docker-compose.yml up -d`
cost little and catch what no fake can.
