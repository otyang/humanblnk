# Errors and retries

humanblnk's errors are its own; when Blnk refused the call they also wrap
Blnk's, for your logs. A Blnk error humanblnk has no name for comes back as a
`*blnk.APIError`, and `VerifyWebhook` returns `blnk.ErrBadSignature` or
`blnk.ErrStale` when the check fails. Always compare with `errors.Is`, never on the message.

## What each error means

| Error | Means | What to do |
| --- | --- | --- |
| `humanblnk.ErrInsufficientFunds` | the source account can't cover it | tell the customer; nothing moved |
| `humanblnk.ErrDuplicate` | this exact write already happened, applied | treat as **success**; the existing entry comes back with it |
| `humanblnk.ErrReferenceUsed` | this write didn't apply: the reference holds a different transaction, or this one was refused partway | after a refusal, record again under a **new** reference; otherwise investigate how two writes got the same reference |
| `humanblnk.ErrNotFound` | no such account, entry or hold | check the id |
| `humanblnk.ErrUnbalanced` | `From` and `To` don't add up to the same amount | fix the postings |
| `humanblnk.ErrAlreadyReversed` | nothing (or not that much) is left to reverse | nothing to do; pass a `Ref` to get your own reversal back on a retry |
| `humanblnk.ErrHoldSettled` | the hold was already captured, cancelled or expired | look at what happened before acting again |
| `humanblnk.ErrInvalid` | refused before reaching Blnk: missing reference, negative amount, wrong ledger, … | a bug in the caller; don't retry |
| `humanblnk.ErrReadBack` | `RecordAll` **applied**, but reading the entries back failed | don't record it again; look the entries up by reference |
| `money.ErrCurrencyMismatch` | an amount's currency doesn't match the account's | a bug in the caller |
| `money.ErrUnknownCurrency` | a `Money` that was never set, or an unknown code | a bug in the caller |

The blnk client already retries transient failures (3 times by default),
and only where a repeat is harmless. After that, a `*blnk.APIError` with a
4xx status means Blnk refused the call. A network error or a gateway timeout
means **the outcome is unknown**: retry with the same arguments if the table
below says the call is safe to repeat.

## What is safe to repeat

| Call | Safe to repeat? | Because |
| --- | --- | --- |
| `Transactions.Record`, `RecordAll` | yes, with the same `Ref` | a used reference is checked: `ErrDuplicate` if it happened |
| `Transactions.Reverse` | yes, with the same `Ref` | returns the reversal already made; without a `Ref`, a full reversal says `ErrAlreadyReversed` once done |
| `Holds.Place` | yes, with the same transaction | returns the same hold, captured or not; `ErrHoldSettled` if it was released |
| `Holds.Capture` (full) | yes | finds the earlier capture |
| `Holds.Capture` (partial) | **wait a few seconds first** | Blnk can't tell a repeated partial capture still being processed from a new one |
| `Holds.Cancel` | yes | |
| `Accounts.OpenForCustomer` | yes | one account per owner, ledger and currency |
| `Identities.Create` | **no** | use `FindByCustomer` first |
| `Accounts.CreateInternal`, `Ledgers.Create` | **no** | look with `List` first |
| `Watches.Create`, `Watches.Change` | **no** | look with `List` first; `Change` makes a new watch each time |
| `Identities.UpdateMeta` | yes | it merges the keys |
| `Ledgers.Rename`, `Reconciliation.ChangeRule` | yes | they replace, not add |
| `Watches.Delete`, `Reconciliation.RemoveRule` | yes | a repeat may return `ErrNotFound` |
| `Reconciliation.Upload`, `AddRule`, `Run`, `RunLines` | **no** | each call makes a new statement, rule or run |
| every read (`Get`, `List`, `GetByReference`, `Accounts.System`, `Preview`) | yes | |

## References, in one rule

Build every reference from **your own ids**, with `humanblnk.Reference`:

| Call | Reference |
| --- | --- |
| `humanblnk.Reference("deposit", "paystack", paystackRef)` | `deposit:paystack:ps_123` |
| `humanblnk.Reference("transfer", transferID)` | `transfer:tr_42` |
| `humanblnk.Reference("payout", payoutID)` | `payout:po_7` |

Never use a random value or a timestamp: a retry must produce the same
reference, so Blnk can refuse to apply it twice.

## When a split or batch fails partway

Blnk moves a split (and a `RecordAll` batch) one part at a time. humanblnk
sends it as a hold and captures it at once, so if a later part fails:

1. You get the error (say `ErrInsufficientFunds`), and nothing moved: Blnk
   released the earlier parts' holds before it answered.
2. The references stay used. Recording again with the same `Ref` returns
   `ErrReferenceUsed`; use a new one.

`Get` and `GetByReference` show the entry as `StatusRejected`.

If your process stops between the hold and its capture (a crash, a deploy),
the money is reserved but hasn't moved. Retry with the same `Ref` within
`humanblnk.RecordHold` (15 minutes) and the capture finishes. After that the
hold expires, the money goes back, and the reference reads as
`ErrReferenceUsed`. Keep Blnk's worker running and monitored: it expires
holds.
