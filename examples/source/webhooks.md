title: Webhooks
group: setup
order: 4
lead: Receive Blnk's events: verify the signature, decode the payload into receipts and alerts, and handle each delivery once.
---
## The situation

Blnk tells Oja when things happen: a transaction posted, a hold expired, a balance crossed a threshold, a bulk payout finished. It sends each event to one URL, Blnk's global webhook. Oja must check that the request really came from its Blnk, turn the JSON into something useful, and cope with Blnk sending the same event more than once.

Set the URL in Blnk's configuration (`notification.webhook.url` in `blnk.json`). Set `server.secret_key` too, so Blnk signs every delivery.

## Which one to use

| Situation | Use |
|---|---|
| A plain `net/http` endpoint | `svc.WebhookHandler(cfg, fn)`: verifies, decodes, calls `fn` and answers |
| Your own router or framework | `svc.ParseWebhook(r, cfg)`: verifies and decodes; you write the response |
| A body you already verified, e.g. from your own queue | `svc.DecodeEvent(body)`: decodes only |

## svc.WebhookHandler

@sample handler | Ada's Paystack top-up posts, and Blnk tells Oja

- `fn` gets the decoded `*ledger.Event`. Return nil and the handler answers 200.
- The emulator stands in for Blnk here: `core.WebhookRequest` builds the signed request Blnk would send. See [Testing your app](testing.html).

### What the handler answers

@sample answers

| Answer | When | Blnk retries? |
|---|---|---|
| 200 | `fn` returned nil | no |
| 401 | a missing or wrong signature, or a timestamp outside the window | yes, but it won't help: check the secret |
| 400 | the body isn't a Blnk webhook, or is over 5 MB | yes, but it won't help |
| 405 | not a POST | |
| 500 | `fn` returned an error | yes: that is how to ask for the event again |

> warn: Without a `Secret`, nothing is verified and anyone who finds the URL can post events to it. Leave `Secret` empty only if your Blnk doesn't sign webhooks.

## svc.ParseWebhook

Use it when you write the HTTP response yourself.

@sample parse

`ParseWebhook` returns `ErrSignature` for a bad signature and `ErrInvalidInput` for a body it can't decode. For a balance alert, it also reads the account to fill in the threshold's currency, so it can return the errors of that read.

## Signatures and the time window

Blnk sends two headers:

| Header | Value |
|---|---|
| `X-Blnk-Timestamp` | the Unix time in seconds |
| `X-Blnk-Signature` | lowercase hex HMAC-SHA256 of timestamp + `.` + raw body, keyed with `server.secret_key` |

With `Secret` set, the signature must match and the timestamp must be within `Tolerance` of `Config.Now`. That rejects an old delivery someone captured and sends again.

@sample stale

| `WebhookConfig` field | What it changes | Default |
|---|---|---|
| `Secret` | Blnk's `server.secret_key`. Set: unsigned and mis-signed deliveries fail with `ErrSignature`. Empty: nothing is verified | empty |
| `Tolerance` | how far the timestamp may be from now, either way; negative turns the check off | 5 minutes |

`blnk.Sign` and `blnk.VerifySignature` do the same check outside the library. `VerifySignature` refuses an empty secret, because anyone can sign with an empty key.

## Events and their payloads

| Event | Constant | Typed field |
|---|---|---|
| `transaction.applied` | `EventTransactionApplied` | `Receipt` |
| `transaction.inflight` | `EventTransactionInflight` | `Receipt` |
| `transaction.void` | `EventTransactionVoid` (also when a hold expires) | `Receipt` |
| `transaction.queued` | `EventTransactionQueued` | `Receipt` |
| `transaction.scheduled` | `EventTransactionScheduled` | `Receipt` |
| `transaction.rejected` | `EventTransactionRejected` | `Receipt` |
| `bulk_transaction.applied` | `EventBulkApplied` | `Bulk` |
| `bulk_transaction.inflight` | `EventBulkInflight` | `Bulk` |
| `bulk_transaction.failed` | `EventBulkFailed` | `Bulk` |
| `balance.monitor` | `EventBalanceAlert` | `Alert` |
| `balance.created` | `EventBalanceCreated` | `Account`, or `House` for an `@` account |
| `identity.created` | `EventIdentityCreated` | none: read `Data` |
| `ledger.created` | `EventLedgerCreated` | none: read `Data` |
| `system.error` | `EventSystemError` | none: read `Data` |

`Data` always holds the raw JSON payload, for any event.

| Typed field | Fields |
|---|---|
| `Receipt` | `TransactionID`, `Reference`, `Status` (Blnk's: `APPLIED`, `INFLIGHT`, …), `EntryStatus` (`posted`, `pending`, …), `Amount`, `Source`, `Destination`, `Legs`, `Description`, `MetaData`, `CreatedAt` |
| `Bulk` (`*BulkEvent`) | `BatchID`, `Status`, `Count` (transactions that didn't fail), `Error` |
| `Alert` (`*Watch`) | `ID`, `AccountID` (the Blnk balance id), `Field`, `Operator`, `Threshold` |
| `Account` / `House` | the new account, as `wallet.GetAccount` or `ops.GetHouseAccount` return it |

### A balance alert

@sample alert | Oja watches for Ada's balance going over ₦100,000

Blnk's monitor payload has no currency. `ParseWebhook` and `WebhookHandler` read the account and fill in the threshold's currency and display. Blnk sends the alert after every update while the condition holds, so debounce it. More in [Balance alerts](balance-alerts.html).

### Decoding a body you trust

@sample decode

`DecodeEvent` doesn't verify anything and doesn't call Blnk. An alert's threshold has only `PreciseAmount`, in the smallest unit. Use it only on bodies you verified before.

## Handle each event once

Blnk delivers at least once, and retries on any answer that isn't 2xx. Key your handling on the event name and the transaction id, and skip what you have done.

@sample idempotent

- Store the key in your database with a unique constraint, in the same database transaction as the work it guards.
- Answer 200 for a repeat. An error makes Blnk send it again.
- A pending transaction produces several events: `transaction.inflight`, then `transaction.applied` or `transaction.void` when it is confirmed, cancelled or expires. The later event's `Receipt` is a new record: its `ParentTransaction` is the pending transaction's id and its `Reference` is generated. Look the payment up by `ParentTransaction`, not by `Reference`.

## A production handler

```go
http.Handle("/webhooks/blnk", svc.WebhookHandler(
	ledger.WebhookConfig{Secret: os.Getenv("BLNK_SECRET_KEY")},
	func(ctx context.Context, e *ledger.Event) error {
		switch e.Name {
		case ledger.EventTransactionApplied:
			return notifyReceived(ctx, e.Receipt) // idempotent on e.Name + e.Receipt.TransactionID
		case ledger.EventTransactionVoid:
			return notifyCancelled(ctx, e.Receipt) // a hold or pending withdrawal was cancelled or expired
		case ledger.EventBalanceAlert:
			return pageFinance(ctx, e.Alert)
		case ledger.EventBulkFailed:
			return reportBatch(ctx, e.Bulk.BatchID, e.Bulk.Error)
		}
		return nil
	}))
```

## Errors

| Error | When | What to do |
|---|---|---|
| `ErrSignature` | `Secret` is set and the signature is missing or wrong, or the timestamp is outside `Tolerance`. The cause is `blnk.ErrBadSignature` or `blnk.ErrStale` | answer 401; check `Secret` matches `server.secret_key`, and the server clock |
| `ErrInvalidInput` | the body isn't JSON with an `event` name, a known event's payload doesn't decode, or the body is over 5 MB | answer 400 |
| errors from reading the account | a balance alert for an account `ParseWebhook` can't read | answer 500 so Blnk retries |

## Related

- [Testing your app](testing.html): produce real signed deliveries in tests.
- [Balance alerts](balance-alerts.html): set up `ops.WatchBalance`.
- [Card holds](card-holds.html) and [Withdrawals](withdrawals.html): where `transaction.void` comes from.
- [Bulk payouts](bulk-payouts.html): the `bulk_transaction.*` events.
