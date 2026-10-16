title: Limits and KYC tiers
group: wallet
order: 15
lead: Cap what each customer can send per transaction, per day and per month, by KYC tier and currency, and show them what they have left.
---
## The situation

Oja's regulator ties spending to how well a customer is verified. A customer with only a phone number (tier1) can send ₦50,000 at a time and ₦200,000 a day. Once they verify a BVN (tier2) the caps go up. Oja also wants the app to say "you can send ₦20,000 more today".

humanblnk applies the limits; it doesn't decide who is on which tier. Your KYC system sets the tier on the account and humanblnk reads it on every payment.

## Configure the limits: Config.Limits

@sample config | Oja's tiers, with days starting at midnight in Lagos

Each `ledger.Limit` has:

| Field | What it does |
|---|---|
| `Tier` | the account's `meta_data.tier` it applies to; `""` matches accounts with no tier; `"*"` matches any tier |
| `Currency` | the account currency it applies to |
| `PerTransaction` | the most one movement can take out, fee included |
| `Daily` | the most that can leave the account since midnight |
| `Monthly` | the most that can leave the account since the 1st of the month |

- Amounts are decimals in `Currency`. An empty string means no cap of that kind.
- Tier and currency match without regard to case. The first limit that matches the account's tier and currency wins, so put `"*"` entries after the specific ones.
- An account with no matching limit is not limited at all.
- `Config.Location` sets where "midnight" and "the 1st" are. The default is UTC. For Oja, `time.LoadLocation("Africa/Lagos")` or a fixed UTC+1 zone.

## Where the tier comes from

The tier is the `tier` key in the account's `MetaData`. `OpenCustomer` copies its `MetaData` onto every account it opens, so setting `tier` there covers all of a customer's currencies. `OpenAccount` takes the same key. Limits read the account's metadata, not the identity's.

## Per-transaction caps

@sample per-transaction | Tunde is on tier1: ₦50,000 at a time

- The check uses the full amount leaving the account: for a withdrawal or transfer, `Amount` plus `Fee`.
- The call is refused before anything is written, with `ErrLimitExceeded`. The message says which cap and by how much.

## Daily and monthly caps, and LimitUsage

`wallet.LimitUsage` reports what the account has used and has left. Use it for the "you can send X more today" line in your app.

@sample daily | Tunde pays Chidi Stores four times, then tries a fifth

`LimitUsage` fields:

| Field | What it holds |
|---|---|
| `Limit` | the `*Limit` that applies; nil when none does |
| `DailyUsed` | what has left the account since midnight in `Config.Location` |
| `MonthlyUsed` | what has left since the 1st of the month |
| `DailyLeft` | `Daily` minus `DailyUsed`; nil when there is no daily cap |
| `MonthlyLeft` | `Monthly` minus `MonthlyUsed`; nil when there is no monthly cap |

- With neither a daily nor a monthly cap, `DailyUsed` and `MonthlyUsed` stay zero: usage isn't read.
- Reading usage lists the account's outgoing transactions for the period, so a daily or monthly check costs a Blnk query on every limited payment.
- Usage is read before the payment posts. Two payments racing each other can both pass the check.

## Holds count while they are held

A card authorisation reserves money, so it counts against the limit at once. Cancelling it gives the allowance back; a partial capture keeps only what was captured.

@sample holds | Ada's ₦80,000 card authorisation

Pending withdrawals and pending transfers work the same way: they count while pending, and a cancelled or expired one gives its amount back.

## Which movements are limited

@sample scope

| Movement | Checked before it posts | Counts in usage |
|---|---|---|
| `Withdraw`, `Transfer` | yes, `Amount` plus `Fee` | yes |
| `Hold` | yes | yes, while held, then what was captured |
| `Split`, `Exchange`, `OpenEscrow` | yes | yes |
| `Schedule`, `Transfer` with `ScheduledFor` | yes, when you create it, against that day's usage | see the note below |
| `Bulk` | no | yes |
| `Deposit` and other money coming in | no | no |
| A refund the account pays, an `ops.Adjust` debit | no | yes |
| Money leaving a pot or an escrow account | no limit applies | no |
| House accounts | no limit applies | no |

- Moving money into one of the customer's own pots is a transfer from their account, so it is checked and it counts.
- `Bulk` batches are never checked. Check payroll and other batches yourself before you send them. Usage still counts them afterwards, so `DailyLeft` can go below zero, as in the sample; show zero in your app when it does.
- Usage counts every movement out of the account, whichever call made it. A refund Chidi Stores pays a customer, or a correction that debits an account, uses up that account's allowance even though neither is checked.

> note: Scheduled transfers don't count in usage while they wait, and usage counts a transfer by the day it was created. A transfer scheduled in August that posts in September counts in neither month's usage.

## Upgrading a tier

humanblnk has no call to change an account's metadata. When your KYC system upgrades a customer, set `tier` on each of their accounts through Blnk's metadata endpoint (`POST /{balance_id}/metadata`), for example with the `blnk` client's `UpdateMetadata`. It merges the key into the existing metadata.

@sample upgrade | Tunde verifies his BVN and moves to tier2

- The new tier applies to the next payment: limits read the account fresh on every check.
- Usage carries over. Tunde has already sent ₦240,000 today, so on tier2 he has ₦260,000 left, not ₦500,000.
- Accounts opened later need the tier too: pass it in `MetaData` when you open them.

## A new day

Daily usage starts again at midnight in `Config.Location`; monthly usage on the 1st. With `Location` set to Lagos, 23:30 UTC is already the next day.

@sample next-day | 00:30 on 2 September in Lagos

## On the confirm screen

A quote runs the same checks, so the app can say "over your limit" before the customer confirms. See [Quotes](quotes.html).

@sample quote | Ada (tier2, ₦100,000 at a time) asks for ₦150,000

## What happens in Blnk

Nothing is written for a limit. The check reads the account (`GET /balances/{id}`) for its tier and, for daily or monthly caps, its outgoing transactions for the period (`POST /transactions/filter` by `source` and `created_at`). A refused payment never reaches Blnk. A payment that passes posts as usual.

## Errors

| Error | When | What to do |
|---|---|---|
| `ErrLimitExceeded` | the movement would break the per-transaction, daily or monthly cap | show what is left (`LimitUsage`), offer a tier upgrade, or try again tomorrow |
| `ErrInvalidInput` | a cap in `Config.Limits` isn't a valid decimal for the currency | fix the configuration |
| `ErrWrongKind` | `LimitUsage` was given a house account | pass a customer account |
| `ErrNotFound` | the account doesn't exist | check the id |

## Related pages

- [Fees and pricing](fees.html): fees count towards limits.
- [Card holds](card-holds.html), [Transfers](transfers.html), [Withdrawals](withdrawals.html), [Bulk payouts](bulk-payouts.html).
- [Quotes](quotes.html): `Allowed` and `Reason` on the confirm screen.
- [Freeze accounts](freeze.html): stopping an account altogether.
