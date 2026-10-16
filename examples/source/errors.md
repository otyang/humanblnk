title: Errors
group: setup
order: 3
lead: Every error the library returns, how to check for it with errors.Is, what to tell the customer, and which ones are worth retrying.
---
## The situation

Ada tries to send Tunde more than she has. Oja's app must show her a clear message, not a stack trace. Support needs the detail in the logs. And the payments worker must know whether to try again. Every error from `wallet` and `ops` carries all three.

## Check with errors.Is

Each error matches one sentinel from package `ledger`. Branch on the sentinel, never on the message text.

@sample is | Ada sends more than she has

The message reads from the outside in: the operation, the sentinel, then the cause. Here the cause is Blnk's own answer.

## What an error carries: OpError

Errors from the library are a `*ledger.OpError`.

@sample as

| Field | What it holds |
|---|---|
| `Op` | the operation, e.g. `ledger.Wallet.Transfer` or `ledger.Ops.CloseAccount` |
| `Kind` | the sentinel, e.g. `ErrInsufficientFunds`. `errors.Is` matches it |
| `Cause` | what went wrong underneath: a `*blnk.APIError`, a network error, or the library's own reason |

- `errors.As(err, &ae)` reaches the `*blnk.APIError` when Blnk answered. It has `Status` (HTTP), `Code` (Blnk's `error_detail.code`, such as `TXN_INSUFFICIENT_FUNDS`), `Message`, `Details` and the raw `Body`. Log them; don't show them to customers.
- Refusals the library makes itself, such as a frozen account or a missing reference, have no `*blnk.APIError`. Nothing was sent to Blnk.
- A cancelled or timed-out `context` comes back as `context.Canceled` or `context.DeadlineExceeded`, not wrapped.

## Business refusals

These are normal outcomes. The customer's request was understood and refused.

@sample refusals | Over a limit, to a frozen account, into a closed account

## Mistakes in the call

These mean the request was wrong. Fix the code or the input.

@sample mistakes

- `ErrWrongKind` also matches `ErrInvalidInput`. `ErrLedgerNotFound` also matches `ErrNotFound`.
- A duplicate reference is only an error when the payload differs. The same reference with the same payload is a replay (see below).

## The wrong state for this step

@sample state | A card hold on Ada's account

## Retrying: replays and transient errors

Every movement takes a `Reference`. Send the same call again with the same reference and the same payload, and you get the original back with `Replayed: true`. Nothing posts twice. So when you don't know whether a call worked, retry it unchanged.

@sample retry

- The client already retries lock contention (423), rate limits (429), 502, 503, 504 and network errors before it returns. See [Install and connect](setup.html) for `WithRetries`.
- What reaches you as `ErrUnavailable` or `ErrConflict` is worth retrying later, with the same reference.
- Business refusals and mistakes give the same answer every time. Don't retry them.
- `wallet.Confirm` and `wallet.Cancel` carry no reference. After a network error or a gateway timeout, read the transaction with `wallet.GetTransaction` before you try again.
- `blnk.IsRetryable(err)` reports whether Blnk's last answer had a retryable status.

## What to tell the customer

Map sentinels to your own words in one place.

@sample message

## Every error

| Error | When | What to do |
|---|---|---|
| `ErrInsufficientFunds` | the account's available balance doesn't cover the amount and fee | tell the customer; offer a smaller amount |
| `ErrLimitExceeded` | the movement is over the tier's per-transaction, daily or monthly limit in `Config.Limits` | tell the customer; show what's left with `wallet.LimitUsage` |
| `ErrAccountFrozen` | an account the money leaves or enters is frozen or blocked | tell the customer to contact support; `ops.UnfreezeAccount` when cleared |
| `ErrAccountClosed` | an account the money leaves or enters is closed; freezing or unfreezing a closed account | use another account; for late money see [Suspense](suspense.html) |
| `ErrLedgerFrozen` | money would leave an account whose ledger is frozen | wait for `ops.UnfreezeLedger` |
| `ErrInvalidInput` | a missing reference, account or amount; a missing fee with no `Config.Fees`; too many decimals; other malformed input; a 4xx from Blnk with no closer match | fix the call; don't retry |
| `ErrWrongKind` | an id of another kind, e.g. a house account where a customer account is needed. Also matches `ErrInvalidInput` | pass the right kind of id |
| `ErrNotFound` | the account, transaction, identity or other object doesn't exist | check the id |
| `ErrLedgerNotFound` | the ledger doesn't exist. Also matches `ErrNotFound` | fix `Config.LedgerID` or the `LedgerID` you passed |
| `ErrDuplicateReference` | the reference was already used for a different transaction, or a ledger `Key` with a different name | use a new reference for a new payment; retry with the original payload otherwise |
| `ErrAlreadyFinalized` | the transaction is already confirmed, cancelled or refunded; a full refund after partial refunds | read its status; nothing to do |
| `ErrNotPending` | confirming or cancelling a transaction that isn't pending | read its status |
| `ErrAmountExceeded` | confirming or refunding more than is left | use the remaining amount |
| `ErrPendingActivity` | closing an account with pending or scheduled transactions, an open pot or a funded escrow | finish those first, then close |
| `ErrApprovalRequired` | closing an account or adjusting a balance with no `ApprovedBy`, or approved by the requester | get a second person to approve |
| `ErrFeePricing` | your `Config.Fees` returned an error, no fee, or a fee in another currency | fix the pricing function or its data |
| `ErrSignature` | a webhook with a bad or missing signature, or outside the time window | answer 401; see [Webhooks](webhooks.html) |
| `ErrUnauthorized` | Blnk answered 401 or 403: a missing or wrong API key, or too few permissions | fix `WithAPIKey` |
| `ErrConflict` | Blnk reported a conflict or a locked resource after the client's retries; a statement that doesn't match Blnk's balance | retry later with the same reference |
| `ErrUnavailable` | Blnk couldn't be reached or failed after the client's retries; a client configuration mistake | retry later; if it persists, check `svc.Check` |

> note: Quotes don't fail on business refusals. `wallet.QuoteTransfer` and the other quotes return `Allowed: false` with the sentinel in `Reason`, so the confirm screen can say why. See [Quotes](quotes.html).

## Logging errors

`Instrument` with `LogObserver` logs every call with its error: business refusals (`ErrInsufficientFunds`, `ErrLimitExceeded`, `ErrAccountFrozen`, `ErrAccountClosed`, `ErrLedgerFrozen`) at Warn, everything else at Error. See [Logging, metrics and tracing](observability.html).

## Related

- [Install and connect](setup.html): client retries and `svc.Check`.
- [Limits](limits.html), [Freeze an account](freeze.html), [Close an account](close-account.html): where the refusals come from.
- [Card holds](card-holds.html): confirm and cancel.
