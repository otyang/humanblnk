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
| `humanblnk.ErrReferenceUsed` | this write didn't apply: the reference holds a different transaction, or this one rolled back | after a rollback, record again under a **new** reference; otherwise investigate how two writes got the same reference |
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
| `Transactions.Reverse` | yes, with the same `Ref` | returns the reversal already made |
| `Holds.Place` | yes, with the same transaction | returns the same hold |
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
| every read (`Get`, `List`, `GetByReference`, `Preview`) | yes | |

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

Blnk records a split (and a `RecordAll` batch) one part at a time. If a later
part fails:

1. You get the error (say `ErrInsufficientFunds`), but the earlier parts have
   already moved.
2. Blnk refunds them from its background worker a moment later (about
   200 ms). Until then they show in the accounts.
3. The references stay used. Once Blnk's refund has landed, recording again
   with the same `Ref` returns `ErrReferenceUsed`; use a new one.

`Get` and `GetByReference` show a split's entry as `StatusRejected`, even
before the refund lands. An earlier part of a `RecordAll` batch shows as
`StatusRejected` only once its refund lands: until then it reads
`StatusApplied`, and retrying the batch returns a lookup error ("looking it
up failed") for the part Blnk never recorded. Wait and retry. Keep Blnk's worker running and monitored: if it's
down, the earlier parts stay moved until it restarts.
