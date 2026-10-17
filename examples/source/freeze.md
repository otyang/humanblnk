title: Freeze and unfreeze
group: ops
order: 3
lead: Stop money moving on one account, or on a whole ledger, while you investigate, then lift the freeze. Nothing is reversed and no balance changes.
---
## When to freeze

Oja freezes when it needs time to look at something: a login from a new device on Tunde's account, a chargeback pattern, a court order, or a regulator's instruction that covers every agent float. A freeze is a status, not a movement. The balance stays where it is, and lifting the freeze puts the account back exactly as it was.

| Situation | Use | What stops |
|---|---|---|
| One customer's account looks compromised | `ops.FreezeAccount` | every new movement into or out of that account |
| A whole group of accounts must stop paying out | `ops.FreezeLedger` | money leaving any customer account in the ledger; money coming in carries on |
| The customer is leaving for good | `ops.CloseAccount` | everything, for good. See [Close an account](close-account.html) |

Freezing is on `svc.Ops()`. A customer-facing server that only holds a `ledger.Wallet` can't freeze or unfreeze anything.

## Freeze one account: ops.FreezeAccount

@sample account | Tunde's account is frozen after a suspicious login

- A frozen account refuses money **in both directions**: transfers out, withdrawals, transfers from other customers, card holds paying it and deposits from a provider all fail with `ErrAccountFrozen`.
- The check runs in humanblnk before anything is sent to Blnk, so a refused call writes nothing.
- The `reason` is stored on the account. It is for your staff and your audit trail; the customer never sees it unless you show it.
- Freezing an account that is already frozen just updates the reason.

> warn: A deposit refused because the account is frozen is still money the provider collected. Paystack has it, and the clearing account doesn't show it yet. Park it in `@Suspense` with `ops.HoldInSuspense` and claim it for Tunde once the freeze is lifted. See [Unidentified money](suspense.html).

## See the status

@sample status | What support sees on Tunde's account

- `Account.Status` reads `frozen`. Accounts that were never frozen read `active`.
- `MetaData` keeps `status_reason` and `status_changed_at` (UTC, RFC 3339) from the last status change.
- The balance doesn't move, but `Available` drops to zero: the freeze holds everything that was available on `@FrozenFunds`, so a scheduled transfer coming due can't spend it (below). The hold is never posted, and `UnfreezeAccount` releases it. The payout and card hold Tunde started before the freeze are still held as they were.
- Quotes run the same checks, so `wallet.QuoteTransfer` comes back with `Allowed: false` and `Reason` set to `ErrAccountFrozen`. Use it to grey out the Send button in the app.

## What a freeze does not stop

The freeze is checked when a movement starts, and again when a pending transaction would settle into the account. Some things that started before the freeze still finish:

@sample pending | Work started before the freeze

- `wallet.Confirm` checks only the accounts that receive the money. A payout already sent to Flutterwave can be confirmed, because the money leaves the frozen account. A pending transfer **into** the frozen account can't be confirmed: `ErrAccountFrozen`. `ConfirmMany` checks the same way.
- `wallet.Cancel` is never checked. A card hold can be released, and a pending transfer in can be cancelled so the money goes back to its sender.
- Scheduled transfers are posted by Blnk itself when they fall due, so humanblnk can't refuse them then. The freeze's hold on the available balance is what stops them: Blnk finds nothing to spend and rejects them. A scheduled transfer created with `AllowOverdraft` still posts. Once the account is unfrozen, the ones still to come run as normal.
- Hold expiry and auto-capture (`HoldInput.CaptureAt`) still happen when due: the money was already reserved, as with a card authorisation. If a capture must not happen, cancel the hold with `wallet.Cancel` after you freeze.
- The freeze hold itself can't be confirmed or cancelled with `wallet.Confirm` or `wallet.Cancel` (`ErrWrongKind`); only `UnfreezeAccount` releases it. A dispute against the frozen account still opens: it takes its share of the money from the freeze hold.
- `ops.FreezeLedger` holds no balances, so scheduled transfers in a frozen ledger still post. Freeze the accounts themselves to stop those.

| Started before the freeze | After the freeze |
|---|---|
| Pending withdrawal or hold paid by the frozen account | `Confirm` and `Cancel` still work |
| Pending transfer or hold paying the frozen account | `Confirm` is refused; `Cancel` works |
| Scheduled transfer | rejected by Blnk when due, for lack of available funds |
| Hold with `ExpiresAt` or `CaptureAt` | released or captured by Blnk when due |
| Any new `Transfer`, `Withdraw`, `Deposit`, `Hold`, `Split`, `Exchange`, `Refund` | refused with `ErrAccountFrozen` |

## Unfreeze: ops.UnfreezeAccount

@sample unfreeze | Tunde confirms the device was his

- The account goes back to `active` and the reason is cleared. `status_changed_at` is set to the time of the unfreeze.
- The refused transfer can be retried with the same `Reference`. Nothing was written the first time, so it posts normally.
- Unfreezing an account that isn't frozen sets it to `active` again and does no harm. A closed account can't be unfrozen.

## Freeze a whole ledger: ops.FreezeLedger

Oja keeps agent floats in their own ledger, `Oja Agents`. When the regulator orders a hold on agent payouts, one call stops all of them.

@sample ledger | A regulator's hold on every agent float

- Nothing can **leave** a customer account in a frozen ledger: transfers out, withdrawals, holds and closure sweeps fail with `ErrLedgerFrozen`.
- Money can still **come in**: deposits from Paystack and transfers from accounts in other ledgers post as usual.
- House accounts (`@PaystackClearing`, `@BankOperating`, `@FeeIncome` and the rest) live in Blnk's general ledger and are never blocked, so settlement and fee postings carry on.
- The status and reason are stored on the ledger. Read them with `ops.GetLedger`: `Status` is `frozen` or `active`.
- An account freeze and a ledger freeze are separate. Unfreezing the ledger doesn't unfreeze an account that was frozen on its own.

## Several processes: Config.LedgerStatusTTL

Checking a ledger's status on every movement would cost one extra request each time, so each `Service` caches the status it last read. `Config.LedgerStatusTTL` sets how long.

@sample ttl | The admin tool freezes; the API server sees it after the cache expires

| `LedgerStatusTTL` | Behaviour |
|---|---|
| `0` (not set) | 30 seconds |
| a positive duration | the status is read again once the cached copy is older than this |
| a negative duration | the status is read from Blnk on every movement |

- The `Service` that calls `FreezeLedger` or `UnfreezeLedger` drops its own cached copy at once. Only other processes wait for the TTL.
- Account status is not cached. An account freeze applies to every process on the next movement.
- Set a negative TTL if a ledger freeze must apply everywhere within the same second, and accept the extra request per movement.

## What happens in Blnk

A freeze moves no money, so it makes no entries. Each call writes metadata only:

| Call | Blnk records |
|---|---|
| `FreezeAccount` | on the balance: `status: frozen`, `status_reason`, `status_changed_at` |
| `UnfreezeAccount` | on the balance: `status: active`, `status_reason` cleared, `status_changed_at` |
| `FreezeLedger` | on the ledger: `status: frozen`, `status_reason`, `status_changed_at` |
| `UnfreezeLedger` | on the ledger: `status: active`, `status_reason` cleared, `status_changed_at` |

Blnk itself doesn't act on these fields. humanblnk enforces them on every movement it makes. A call made to Blnk directly, outside humanblnk, is not blocked.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrAccountFrozen` | a movement touches a frozen account, in or out; `Confirm` would settle a pending transaction into one | wait for the freeze to be lifted; park incoming provider money in [suspense](suspense.html) |
| `ErrLedgerFrozen` | money would leave a customer account in a frozen ledger | wait for `UnfreezeLedger`; money in still works |
| `ErrAccountClosed` | freezing, unfreezing or moving money on a closed account | open a new account for the customer |
| `ErrWrongKind` | `FreezeAccount` on a house account such as `@FeeIncome`, or `FreezeLedger` on the general ledger | house accounts are never frozen |
| `ErrNotFound` | the account doesn't exist | check the id |
| `ErrLedgerNotFound` | `FreezeLedger` on a ledger that doesn't exist | check the id |

## Related

- [Close an account](close-account.html): the permanent version of a freeze.
- [Unidentified money](suspense.html): where to park money that can't reach a frozen or closed account.
- [Provision ledgers](ledgers.html): split accounts into ledgers so you can freeze a group at once.
- [Errors](errors.html): how `errors.Is` works with every sentinel.
