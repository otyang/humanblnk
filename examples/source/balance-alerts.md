title: Balance alerts
group: ops
order: 9
lead: Have Blnk tell you when a balance crosses a line: a customer running low, a provider owing too much, a payout waiting too long.
---
## When to use an alert

Oja wants to push a message to Ada when her balance drops below ₦1,000. Finance wants to hear when Paystack owes more than ₦100,000 that hasn't been settled. Operations wants to know when a large payout is waiting on a merchant's account. None of this needs polling: Blnk can watch a balance and call your webhook when a condition holds.

`ops.WatchBalance` creates a Blnk balance monitor. When the condition holds, Blnk sends the `balance.monitor` webhook, which humanblnk decodes as `ledger.EventBalanceAlert` with the alert in `Event.Alert`.

| Situation | Use |
|---|---|
| Watch a customer account, pot or escrow | `ops.WatchBalance` with `AccountID` |
| Watch one of the business's own accounts | `ops.WatchBalance` with `House: ledger.HouseRef{...}` |
| Stop watching | `ops.UnwatchBalance` with the watch's `ID` |
| Check balances once a night instead | `ops.TrialBalance`; see [Trial balance and reports](reports.html) |

## Watch a customer account

@sample account | Low-balance alert for Ada

- Set exactly one of `AccountID` and `House`.
- `Field` is the balance figure compared. It defaults to `balance`, the settled balance.
- `Threshold` can leave the currency empty; it takes the account's currency. A different currency is refused.
- `MetaData` is stored on the Blnk monitor. Use it to note what the watch is for.
- Keep `ID` (`mon_…`) to unwatch later, and to know which watch an alert belongs to.

## Watch a house account

House accounts are named by `HouseRef`, as everywhere in `Ops`.

@sample house | Paystack owes Oja more than ₦100,000

- The condition is checked against the balance as Blnk stores it. `@PaystackClearing` is an asset, which Blnk stores as a negative number, so "Paystack owes more than ₦100,000" is `balance < -100000`. A credit-normal account such as `@FeeIncome` reads positive, so "fee income above ₦1m" is `balance > 1000000`. See [House accounts](house-accounts.html) for which accounts are which.
- Blnk creates a house account the first time money moves through it. Watching one that has never been used fails with `ErrNotFound`. Set up house watches after the first movement, or retry them from a start-up job.

## Receiving the alert

Alerts arrive through your global Blnk webhook, like transaction events. In production, mount `svc.WebhookHandler` and handle `ledger.EventBalanceAlert`; see [Webhooks](webhooks.html). In tests, the in-memory Blnk records every webhook it would have sent: read them with `core.Events()` and turn one into a real HTTP request with `core.WebhookRequest(e, secret)`.

@sample receive | Ada pays Chidi Stores ₦4,500, then ₦200

- `Event.Alert` is a `*ledger.Watch` with the watch's `ID`, the `AccountID` (Blnk balance id) of the watched account, `Field`, `Operator` and `Threshold`.
- Blnk's payload has no currency. `svc.ParseWebhook` and `svc.WebhookHandler` read the account and fill in the threshold's currency and display value. `svc.DecodeEvent` doesn't call Blnk, so after it the threshold holds only the raw smallest-unit number in `PreciseAmount`.
- The alert doesn't carry the balance. Read it with `wallet.GetAccount` or `ops.GetHouseAccount` if you need it.

## Alerts repeat: debounce them

Blnk checks the condition after every update of the balance and sends the webhook every time it holds, not only when it first becomes true. Ada got two alerts above, one per payment. Keep a flag per watch on your side, set it when you notify, and clear it when the balance recovers.

@sample debounce | Notify once per drop

- Three payments while below ₦1,000 send three alerts. Ada gets one push.
- The top-up takes her above the threshold. No alert is sent, and the handler clears the flag.
- The next drop alerts again.
- Keep the flag in your database, not in memory. Blnk delivers at least once and may deliver to any of your instances.

## Fields and operators

@sample fields

| `Field` | Compares |
|---|---|
| `balance` (default) | the settled balance |
| `credit_balance` | the lifetime total of credits |
| `debit_balance` | the lifetime total of debits |
| `inflight_balance` | the pending (INFLIGHT) balance |
| `inflight_debit_balance` | pending money going out, e.g. withdrawals not yet confirmed |
| `inflight_credit_balance` | pending money coming in |

`Operator` is one of `<`, `<=`, `>`, `>=`, `==` and `!=`. `=` is accepted and sent as `==`, because Blnk accepts `=` but its evaluator never matches it.

The threshold is sent to Blnk as an integer in the smallest unit with precision 1, so ₦1,000.00 goes as `100000`. Blnk computes monitor values with float math, and this keeps the threshold exact.

## Stop watching: ops.UnwatchBalance

@sample unwatch

- `UnwatchBalance` deletes the Blnk monitor. Nothing more is sent for it. Here Chidi Stores' ₦2,000 pending payout would have matched the `inflight_debit_balance > 1000` watch.
- Deleting a watch that no longer exists is `ErrNotFound`. Treat that as done if you are cleaning up.
- An alert already on its way can still arrive after you unwatch. Ignore alerts for watch ids you no longer hold.

## What happens in Blnk

No money moves. `WatchBalance` creates one balance monitor (`POST /balance-monitors`) on the account's Blnk balance, with `field`, `operator`, `value` in the smallest unit and `precision: 1`. `UnwatchBalance` deletes it (`DELETE /balance-monitors/{id}`). The monitor stays until you delete it: it isn't removed when it fires.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | both `AccountID` and `House` set, or neither; an unknown `Field` or `Operator`; a threshold in another currency; an empty watch id | fix the call |
| `ErrWrongKind` | a house account passed as `AccountID`, or a customer account as `House` | use `House: ledger.HouseRef{...}` for `@` accounts |
| `ErrNotFound` | the account doesn't exist; the house account has never been used; unwatching a watch that is gone | check the id; create house watches after first use |
| `ErrUnavailable` | Blnk can't be reached after retries | retry |

## Related pages

- [Webhooks](webhooks.html) to mount the handler and verify signatures.
- [Trial balance and reports](reports.html) for the nightly view of every balance.
- [House accounts](house-accounts.html) for which accounts run negative in Blnk.
- [Testing](testing.html) for `core.Events()` and `core.WebhookRequest`.
