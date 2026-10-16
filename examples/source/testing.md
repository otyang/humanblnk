title: Testing your app
group: setup
order: 6
lead: Test the code that calls humanblnk against an in-memory Blnk Core: control the clock, expire holds, run scheduled transfers and assert the webhooks Blnk would send.
---
## The situation

Oja's engineers want tests that run in milliseconds on a laptop and in CI, with no Docker and no shared Core. They also want to test what happens over time: a card hold that expires, a withdrawal Flutterwave never answers, a standing order on the 2nd of each month. Package `ledgertest` is an in-memory Blnk Core for that.

The real `ledger.Service` code runs unchanged against it, so your tests send the same requests production sends.

## Which one to use

| Situation | Use |
|---|---|
| Most tests | `ledgertest.New(t, cfg)`: a Core, a ledger and a `*ledger.Service`, closed when the test ends |
| Your own wiring: several services, an observer, your own `blnk.Client` | `ledgertest.NewServer()`, then `core.Client()` or `core.URL()` |
| Checking the library against a real Core | live tests with `BLNK_LIVE_URL` |

## ledgertest.New

@sample basic | Ada pays her rent

- `New` creates a ledger for the test and puts its id in `cfg.LedgerID`, unless you set one.
- It sets `cfg.Now` to the emulator's clock, unless you set one. Holds, limits and webhook time windows then follow that clock.
- It registers `core.Close` with `t.Cleanup`.
- Each call starts an empty Core, so tests don't share state and can run in parallel.

## The clock

The emulator's clock starts at 1 January 2026, 09:00 UTC, and moves only when you move it.

@sample clock

| Method | What it does |
|---|---|
| `core.Now()` | the emulator's time |
| `core.SetNow(t)` | moves the clock to `t`, then runs whatever became due |
| `core.Advance(d)` | moves the clock forward by `d`, then runs whatever became due |

"Whatever became due" means: scheduled transactions post, pending transactions past their expiry are cancelled, and those past their capture time are captured.

## A card hold that expires

@sample hold-expiry | Chidi Stores never captures Ada's ₦20,000

- Check the outcome through the balance and the `transaction.void` event. The void is a new record whose `ParentTransaction` is the hold.
- `wallet.GetTransaction` on the hold's own id still shows it as `INFLIGHT` after it expires or is cancelled, as Blnk keeps the original record. Don't assert on that status.
- More in [Card holds](card-holds.html).

## A withdrawal the provider never confirms

Withdrawals are pending by default and cancel themselves after `Config.WithdrawalHoldTTL` (24 hours).

@sample withdrawal-ttl | Flutterwave goes quiet on Ada's ₦10,000

More in [Withdrawals](withdrawals.html).

## Scheduled transfers and standing orders

@sample scheduled | ₦10,000 to Ada's rent pot on the 2nd of each month

- A scheduled transfer posts when the clock passes its time.
- The emulator rejects a scheduled transfer the account can't pay for when it runs. It becomes `rejected` and sends `transaction.rejected`.
- More in [Savings pots and standing orders](savings-pots.html).

## Asserting webhooks

The emulator records every webhook Blnk would send.

@sample events | A deposit, then a card hold that is cancelled

| Method | What it does |
|---|---|
| `core.Events()` | every webhook sent so far, oldest first, as `ledgertest.Event{Event, Data}` |
| `core.OnEvent(fn)` | calls `fn` for each webhook as it is sent. It runs under the server's lock: don't call the emulator from `fn` |
| `core.WebhookRequest(e, secret)` | the `*http.Request` Blnk would POST for `e`, signed with `secret` and the emulator's clock (empty secret: unsigned) |

The emulator sends the `transaction.*`, `bulk_transaction.applied` and `balance.monitor` events. It doesn't send `balance.created`, `identity.created` or `ledger.created`.

### Your webhook handler

@sample handler

More in [Webhooks](webhooks.html).

## ledgertest.NewServer: your own wiring

Use the server directly when a test needs more than one service, an observer, or a client you build yourself.

@sample server | The API and the back office share one Core

| Method | What it does |
|---|---|
| `ledgertest.NewServer()` | starts an emulator. Close it with `core.Close()` |
| `core.Client(opts...)` | a `*blnk.Client` for the emulator, with retries off; your options are added |
| `core.URL()` | the base URL, for `blnk.New` or `humanblnk.New` |
| `core.CreateLedger(name)` | adds a ledger and returns its id |

Set `Config.Now: core.Now` yourself when you build the service this way. `ledgertest.New` does it for you.

An `ObserverFunc` like `spy` lets a test assert which operations ran. See [Logging, metrics and tracing](observability.html).

## What the emulator covers

It implements what this library uses: health, ledgers, balances, identities, inflight transactions, splits, refunds, bulk, filters, metadata, balance monitors, historical balances, dry runs, instant reconciliation and signed webhooks. It behaves like Blnk Core 0.15 where that matters:

- A transaction with a fee or several recipients is stored only as its legs (`<ref>-1`, `<ref>-2`, …).
- House (`@`) accounts are stored by balance id.
- Blank descriptions are rejected.
- Filter results leave `parent_transaction` empty.

It is not a complete Blnk. For anything else in Blnk's API, test against a real Core.

## Live tests against a real Core

The library's own live test runs every kind of movement against a real Core, retries each one, and checks nothing posted twice. It writes test data, so point it at a Core you can throw away.

```bash
make live BLNK_LIVE_URL=http://localhost:5001
# the same as:
BLNK_LIVE_URL=http://localhost:5001 go test ./ledger -run TestLiveCore -count=1 -v
```

Set `BLNK_LIVE_API_KEY` too when the Core runs in secure mode. Without `BLNK_LIVE_URL` the test is skipped, so `go test ./...` stays offline.

Your own live tests can follow the same pattern:

```go
func TestLiveTopUp(t *testing.T) {
	url := os.Getenv("BLNK_LIVE_URL")
	if url == "" {
		t.Skip("set BLNK_LIVE_URL to run against a real Blnk Core")
	}
	client := blnk.New(url, blnk.WithAPIKey(os.Getenv("BLNK_LIVE_API_KEY")))
	l, err := client.CreateLedger(ctx, blnk.CreateLedgerRequest{Name: "oja live test"})
	if err != nil {
		t.Fatal(err)
	}
	svc := ledger.New(client, ledger.Config{LedgerID: ledger.LedgerID(l.LedgerID)})
	run := fmt.Sprint(time.Now().UnixNano()) // unique references per run
	// ... the same calls as your emulator tests, with references ending in run
}
```

> note: A real Core doesn't let you move its clock. Test expiry and scheduled transfers with the emulator, and keep live tests to what doesn't depend on time.

## Errors

The emulator returns Blnk's error codes, so the library maps them to the same sentinels as in production. Assert with `errors.Is`, as in [Errors](errors.html).

| Error | When | What to do |
|---|---|---|
| `ErrAlreadyFinalized` | confirming or cancelling a hold or withdrawal after the clock expired it | assert it in tests of late provider callbacks |
| `ErrNotFound` | an id from another emulator: each `New` starts empty | create what the test needs in the same test |
| `ErrUnavailable` | the emulator was closed, e.g. a call after the test ended | keep calls inside the test |

## Related

- [Webhooks](webhooks.html): the handler you test with `core.WebhookRequest`.
- [Card holds](card-holds.html), [Withdrawals](withdrawals.html), [Savings pots and standing orders](savings-pots.html): the timed flows.
- [Install and connect](setup.html): connecting to a real Core.
