title: Savings pots and standing orders
group: wallet
order: 12
lead: Let customers put money aside in named pots with a goal, and move money on a schedule: a monthly savings plan, a weekly allowance, a subscription.
---
## The situation

Ada wants to save ₦600,000 for next year's rent. She opens a "Rent" pot in the Oja app, moves money in when she can, and sets up ₦25,000 to go in on the 1st of every month. If she is short one month, that month is skipped and the plan carries on.

| Situation | Use |
|---|---|
| Open a pot under a customer account | `wallet.OpenPot` |
| List a customer's pots | `wallet.ListPots` |
| Move money into or out of a pot now | `wallet.Transfer` |
| Move the same amount on a schedule | `wallet.Schedule` |
| Move money once, later | `wallet.Transfer` with `ScheduledFor`; see [Transfers](transfers.html) |

## Opening pots

A pot is an account under a customer account. It takes its parent's currency, ledger and identity.

@sample open | A rent pot with a goal, and a travel pot without one

- `Name` is unique per parent. Opening the same name again returns the existing pot.
- `Goal` is optional and only recorded. Nothing stops a pot going past it, and nothing moves when it is reached.
- `Pot` embeds `*Account`, so it has `ID`, `Balance`, `Available` and the rest, plus `Name`, `Parent` and `Goal`.
- `ListPots` returns the pots under one parent. `GetCustomer` leaves pots out.

## Moving money in and out

There is no special call. A pot is an account, so money moves with `Transfer`, in either direction.

@sample move

- Pass a fee as for any transfer: `ledger.NoFee` here, or let `Config.Fees` price it. See [Fees](fees.html).
- Pots aren't limited, but a transfer out of Ada's main account counts against her [limits](limits.html).

## A standing order: wallet.Schedule

`Schedule` creates every occurrence at once, as a Blnk scheduled transaction. Blnk posts each one itself when its time comes.

@sample schedule | ₦25,000 into the rent pot on the 1st of every month for a year

- Occurrence `i` gets the reference `Reference` + `_i`: `rent_plan_ada_1` to `rent_plan_ada_12`.
- Every occurrence carries `recurring_reference`, `occurrence` and `occurrences` in its metadata, so you can find the whole plan.
- Nothing leaves Ada's account until an occurrence runs, and her balance isn't checked when the plan is made.
- The fee is priced once, when you call `Schedule`, and the same fee is charged on every occurrence.

### RecurringInput fields

| Field | What it does |
|---|---|
| `Transfer` | the transfer to repeat. `Reference` is required and is the base of every occurrence's reference. `Pending` is not allowed |
| `First` | when the first occurrence runs. Must be in the future |
| `Every` | the gap between occurrences, an `Interval` |
| `Count` | how many occurrences, from 1 to 120 |

### Intervals

| Interval | Meaning |
|---|---|
| `ledger.Daily` | `Interval{Days: 1}` |
| `ledger.Weekly` | `Interval{Days: 7}` |
| `ledger.Monthly` | `Interval{Months: 1}` |
| `ledger.Interval{Days: 14}` | every two weeks |
| `ledger.Interval{Months: 3}` | every quarter |

@sample intervals | The second run for each interval, starting 4 January 2027

> warn: Monthly dates follow Go's `time.AddDate`. A plan starting on the 31st lands on the 1st of the next month in shorter months. Start monthly plans on the 28th or earlier.

## Retrying

Calling `Schedule` again with the same input returns the occurrences already created, each with `Replayed` set. Nothing is scheduled twice.

@sample retry

> note: A retry must still have `First` in the future. Once the first occurrence's time has passed, the same input is refused with `ErrInvalidInput`.

## When occurrences run

In production, Blnk posts each occurrence at its time. In tests, move the in-memory Blnk's clock with `core.SetNow` or `core.Advance`, and whatever is due runs. See [Testing](testing.html).

@sample run | After 2 November, two occurrences have posted

## When the money isn't there

An occurrence that runs when Ada's balance is too low is refused by Blnk with status `REJECTED`. The later occurrences are not affected.

@sample short | Ada is ₦15,000 short on 1 December

Watch for `ledger.EventTransactionRejected` on your [webhook](webhooks.html) to tell Ada her saving was skipped. To make it up, send a one-off transfer.

> warn: humanblnk has no call to cancel scheduled occurrences, and `Cancel` only works on pending (INFLIGHT) transactions. An account with occurrences still scheduled can't be closed. Choose `Count` with this in mind. Blnk posts occurrences itself, so freezing an account doesn't stop them either.

## What happens in Blnk

Each occurrence is its own transaction with `scheduled_for` set and status `SCHEDULED`. When it runs, it posts like a transfer:

| Account | Debit | Credit |
|---|---|---|
| Ada | ₦25,000.00 | |
| Rent pot | | ₦25,000.00 |

Opening a pot makes a Blnk balance with `account_kind: pot`, `parent_account` and `pot_name` in its metadata, and `pot_goal` when there is a goal. No money moves when a pot is opened.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | `Count` outside 1 to 120; `First` not in the future; a zero or negative interval; `Pending` set; no `Reference` | fix the plan |
| `ErrNotPending` | `Cancel` on a scheduled occurrence | let it run, or reverse it after it posts |
| `ErrPendingActivity` | closing an account with occurrences still scheduled | wait for them to run |
| `ErrLimitExceeded` | the transfer is over the sender's limit when the plan is made | see [Limits](limits.html) |
| `ErrWrongKind` | `OpenPot` with a house account as the parent | use a customer account |

If an occurrence fails while the plan is being created, `Schedule` returns the occurrences already made with the error. Call it again with the same input to finish the plan.

## Related

- [Create customer accounts](customer-accounts.html): pots next to main accounts and escrows.
- [Transfers](transfers.html): one-off and one-time scheduled transfers.
- [Bulk payouts](bulk-payouts.html): many transfers now rather than one transfer many times.
- [Close an account](close-account.html): pots and scheduled transfers block closing.
