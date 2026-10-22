# Watches and webhooks

**Scenario:** Your app texts Ada when her balance drops below ₦2,000. Blnk tells
your service through a webhook, and your service must be sure each webhook really came from
Blnk.

## Step 1: watch the balance

A **watch** is a threshold on one account. While it holds, Blnk sends the
webhook event `balance.monitor` after every update of the account.

{{code 10-watches-and-webhooks/main.go watch}}

| Field | Values |
| --- | --- |
| `Field` | `WatchBalance` (Total), `WatchCredits`, `WatchDebits`, `WatchOutgoingHeld` (held out of it), `WatchIncomingHeld` (held into it), `WatchHeldNet` |
| `Op` | `WatchAbove`, `WatchAtLeast`, `WatchBelow`, `WatchAtMost`, `WatchEquals`, `WatchNotEquals` |
| `Threshold` | a `money.Money` in the account's currency, at most 2^53 in its smallest unit (about ₦90 trillion, or 0.009 ETH). Larger is `ErrInvalid`: Blnk can't store it exactly. |

## Step 2: move the line

`Change` returns a watch with a **new id**: Blnk can't change a threshold in
place, so the new watch is made before the old one is deleted. The account is
never unwatched in between, but both may fire once. `List` with `""` lists
every watch.

{{code 10-watches-and-webhooks/main.go manage}}

## Step 3: receive webhooks safely

Blnk signs each webhook with its `server.secret_key`. `humanblnk.VerifyWebhook`
checks the signature and the age, then decodes it. Never act on a body that
fails the check. This is the handler you mount at the URL Blnk sends
webhooks to:

{{code 10-watches-and-webhooks/main.go handler}}

| `Event` field | |
| --- | --- |
| `Kind` | the event: `humanblnk.EventBalanceMonitor`, `EventTransactionApplied`, `EventTransactionVoid`, … |
| `Watch`, `Account` | for `balance.monitor`: which watch fired, on which account |
| `Entry` | for `transaction.*` events: the entry |
| `Data` | the raw data, as Blnk sent it |

`Record` sends a split as a hold and captures it at once (chapter 5), and
`RecordAll` its batch, so Blnk sends `transaction.inflight` for each posting
and then `transaction.applied` for each capture. The applied ones carry the
transaction's own `Ref` in `Entry.Ref`. React to `transaction.applied` for
money that moved.

The tolerance rejects old deliveries replayed by an attacker; five minutes is
sensible, and is what `VerifyWebhook` uses if you pass 0. Answer anything
that isn't 2xx and Blnk sends the webhook again, so reject only a bad
signature or a stale one, and acknowledge what you can't use.

**Blnk can deliver the same event more than once**, each time freshly
signed. Make the handler safe to repeat: text the customer once per watch
and day, say, or act on an entry once by its `ID`.

The example signs requests the way Blnk does and sends a genuine one and a
forged one to a test server.

## Step 4: stop watching

{{code 10-watches-and-webhooks/main.go delete}}

## The whole example

{{run 10-watches-and-webhooks}}

To receive real webhooks, set Blnk's webhook URL to your handler, and give
your service the same secret as Blnk's `server.secret_key` (see Blnk's
configuration docs).
