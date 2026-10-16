title: Logging, metrics and tracing
group: setup
order: 5
lead: Wrap the service once with ledger.Instrument and every wallet and ops call is logged, counted or traced, with its name and its error.
---
## The situation

Oja's on-call engineer wants to see every ledger call in the logs, a dashboard of transfers that worked, were refused or failed, and a trace span per call inside each HTTP request. None of that should touch the code that calls `wallet.Transfer`.

## How it works

`ledger.Instrument(l, o)` returns a `ledger.Ledger` whose `Wallet()` and `Ops()` report every call to the observer `o`. The calls behave exactly as before.

| Piece | What it is |
|---|---|
| `ledger.Observer` | an interface with one method: `Start(ctx, op) (context.Context, func(err error))` |
| `ledger.ObserverFunc` | a function used as an `Observer` |
| `ledger.LogObserver(logger)` | a ready-made `Observer` that logs to a `*slog.Logger` |
| `ledger.Instrument(l, o)` | wraps any `Ledger`, such as `*ledger.Service`, with `o` |

`Start` runs before the call. The context it returns is the one the call uses. The function it returns runs after the call, with the call's error (nil on success).

## Which one to use

| You want | Use |
|---|---|
| A log line per call | `ledger.LogObserver(logger)` |
| Counters or latency histograms | your own `ObserverFunc` |
| A trace span per call | your own `ObserverFunc` that starts a span and returns its context |
| Several of these | wrap more than once: `Instrument` takes and returns a `Ledger` |

## Instrument and ObserverFunc

@sample instrument

- Operation names are `"Wallet.<Method>"` and `"Ops.<Method>"`, such as `"Wallet.Transfer"` or `"Ops.CloseAccount"`. Use them as metric labels or span names.
- The error's own `Op` field is longer (`ledger.Wallet.Transfer`). The observer gets the short name.
- Hand `l.Wallet()` and `l.Ops()` to the rest of the app as before. Code that depends on the `ledger.Wallet` and `ledger.Ops` interfaces doesn't change.

## Logs: LogObserver

@sample log | Oja logs with log/slog

| Level | Message | When |
|---|---|---|
| Info | `ledger call` | the call succeeded |
| Warn | `ledger call refused` | a business refusal: `ErrInsufficientFunds`, `ErrLimitExceeded`, `ErrAccountFrozen`, `ErrAccountClosed` or `ErrLedgerFrozen` |
| Error | `ledger call failed` | every other error: bad input, Blnk unreachable, a bug |

- Every line has `op` and `duration`; failed calls add `error`. The sample drops `time` and `duration` only so its output stays the same on every run.
- Logs go through `InfoContext`, `WarnContext` and `ErrorContext`, so a handler that reads trace ids from the context sees them.
- Refusals are normal business. Alert on the Error level, not on Warn.

In production:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
var l ledger.Ledger = ledger.Instrument(svc, ledger.LogObserver(logger))
wallet, ops := l.Wallet(), l.Ops()
```

## Metrics

Count each call by operation and outcome. Classify the error with `errors.Is`, the same way `LogObserver` does.

@sample metrics | Transfers that worked, were refused, or failed

With Prometheus, the function body becomes `calls.WithLabelValues(op, outcome).Inc()`. Measure latency with `time.Now()` in `Start` and `time.Since` in the returned function.

## Tracing and the context

The observer sees the caller's context and can return a new one. Values you put on the request context, such as a request id, reach the observer.

@sample context

With OpenTelemetry, start a span in `Start` and end it in the returned function. The library then makes its Blnk requests with the span's context, so an instrumented HTTP client nests them under it.

```go
tracer := otel.Tracer("oja/ledger")
tracing := ledger.ObserverFunc(func(ctx context.Context, op string) (context.Context, func(error)) {
	ctx, span := tracer.Start(ctx, op)
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
})

client := blnk.New(os.Getenv("BLNK_URL"), blnk.WithAPIKey(os.Getenv("BLNK_API_KEY")),
	blnk.WithHTTPClient(&http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}))
svc := ledger.New(client, cfg)
var l ledger.Ledger = ledger.Instrument(ledger.Instrument(svc, tracing), ledger.LogObserver(logger))
```

## What is and isn't observed

| Observed | Not observed |
|---|---|
| every method on `Wallet()` and `Ops()` of the instrumented `Ledger` | calls on the unwrapped `svc`, or on handles taken from it |
| | `svc.Check`, `svc.Parse`, `svc.Display`, `svc.ComputeFee` |
| | `svc.ParseWebhook`, `svc.DecodeEvent` and `svc.WebhookHandler` |

> tip: Take `Wallet()` and `Ops()` only from the instrumented `Ledger`. A handle taken from `svc` directly bypasses the observer.

## Errors

The observer never changes a call's result. It gets the same error the caller gets, and the same sentinels apply: see [Errors](errors.html).

| Error | When | What to do |
|---|---|---|
| any sentinel, at Warn | a business refusal | nothing; count it |
| any sentinel, at Error | bad input, Blnk unreachable, a configuration mistake | investigate; `ErrUnavailable` for long means Blnk is down |

## Related

- [Errors](errors.html): what each sentinel means.
- [Install and connect](setup.html): handing out the handles.
- [Testing your app](testing.html): assert calls in tests with an `ObserverFunc`.
