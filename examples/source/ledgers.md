title: Provision ledgers
group: ops
order: 1
lead: Create the Blnk ledgers your customer accounts live in, safely and once, and check at start-up that the app points at the right one.
---
## The situation

A Blnk ledger is a group of accounts. Oja keeps one ledger per market: Nigeria first, then Ghana and Kenya. Each market's customer accounts live in its own ledger, so they can be reported on, and frozen, separately.

Ledgers are created when you provision, by a deploy job or a setup script, not while serving customers. The app then gets the ledger id from configuration (`Config.LedgerID`) and checks it at start-up.

| Situation | Use |
|---|---|
| Provisioning a market | `ops.CreateLedger` with a `Key` |
| Reading one ledger | `ops.GetLedger` |
| Listing ledgers, e.g. in an admin tool | `ops.ListLedgers` |
| Making sure the app is configured right | `svc.Check` at start-up |
| Stopping all money leaving a market | `ops.FreezeLedger`, see [Freeze](freeze.html) |

## Create a ledger: ops.CreateLedger

@sample create | The provisioning job, run twice

- `Key` is required. It is your stable name for the ledger and what makes the call safe to repeat: a second call with the same `Key` returns the ledger the first one made, with `Replayed` set. It is stored on the ledger as `meta_data.ledger_key`.
- `Name` is the display name. It defaults to `Key`.
- `MetaData` is stored on the ledger. The keys humanblnk manages (`ledger_key`, `status`, `status_reason`, `status_changed_at`, `duplicate_of`) are ignored when you pass them.
- Blnk itself has no idempotency for ledgers and allows two ledgers with the same name, which is why the `Key` matters.

`LedgerInfo` fields:

| Field | What it holds |
|---|---|
| `ID` | the Blnk ledger id (`ldg_…`), a `ledger.LedgerID` |
| `Key` | the `Key` it was created with; empty for ledgers made outside humanblnk |
| `Name` | the display name |
| `Status` | `active`, `frozen` or `duplicate` |
| `DuplicateOf` | for a duplicate, the ledger to use instead |
| `CreatedAt` | when Blnk created it |
| `MetaData` | your metadata plus the keys humanblnk manages |
| `Replayed` | true when a ledger with this `Key` already existed and this is it |

## One ledger per market

Open each customer's accounts in their market's ledger with `LedgerID`. Without it, `OpenAccount` and `OpenCustomer` use `Config.LedgerID`.

@sample markets | Ada in Nigeria, Kofi in Ghana

The ledger must exist; `OpenAccount` checks that before creating anything. See [Create customer accounts](customer-accounts.html).

House accounts such as `@FeeIncome` don't belong to your ledgers. Blnk keeps them in its general ledger, `ledger.GeneralLedgerID` (`general_ledger_id`), one per currency, whatever ledger the customer side is in. See [House accounts](house-accounts.html).

## Read ledgers: GetLedger and ListLedgers

@sample read

- `GetLedger` always reads the ledger fresh from Blnk, so its `Status` is current.
- `ListLedgers` returns ledgers oldest first. `LedgerQuery` has `Limit` (default 20, at most 100) and `Offset`. `LedgerPage` returns `Items` with the `Limit` and `Offset` used.
- The list includes every ledger in Blnk: here Blnk's own General Ledger, and the ledger the in-memory Blnk for these examples starts with, which have no `Key`.

## Check the configuration at start-up: svc.Check

Put the ledger id in `Config.LedgerID` and call `svc.Check` before serving traffic. A wrong id then fails at boot, not at the first sign-up.

@sample check

`Check` verifies that:

1. the client is configured correctly (URL, and the instance id for Blnk Cloud);
2. Blnk Core is up and reaches its database (`GET /health`);
3. `Config.LedgerID`, when set, exists and is not a duplicate.

The error wraps `ErrUnavailable` when Core can't be reached, and `ErrLedgerNotFound` when the ledger is missing. humanblnk needs Blnk Core 0.15.4 or later.

## Two provisioning jobs at once

Calls in one process with the same `Key` run one at a time, so they can't both create. Two processes racing can: both look, find nothing, and create. humanblnk settles this on the next `CreateLedger` with that key. The oldest ledger is the one every caller gets; the others are marked `status: duplicate` with `duplicate_of` naming the oldest.

@sample twins | Two "oja-ke" ledgers from a race

- `OpenAccount` refuses a duplicate ledger with `ErrInvalidInput`, naming the one to use, so no customer money lands in it.
- `svc.Check` refuses a `Config.LedgerID` that is a duplicate the same way.
- The sample makes the twins straight in Blnk to stand in for the race. Your code only ever calls `CreateLedger`.

> tip: Run provisioning from one job, and create ledgers before the app starts serving customers. Then a race can't happen at all.

## Freezing a ledger

`ops.FreezeLedger` stops money leaving every customer account in a ledger, for example on a regulator's order. Money can still come in, and house accounts are never blocked. A frozen ledger's `Status` reads `frozen`. Other app instances see the change within `Config.LedgerStatusTTL` (default 30 seconds), the time the status is cached when checking movements. See [Freeze](freeze.html).

## What happens in Blnk

| Call | Blnk records |
|---|---|
| `CreateLedger`, first time | looks for a ledger with `meta_data.ledger_key` (`POST /ledgers/filter`), then `POST /ledgers` with `name` and `meta_data` including `ledger_key` and `status: active`, then looks again for a twin |
| `CreateLedger`, again | the lookup only; nothing is written |
| `CreateLedger` after a race | marks each younger twin with `status: duplicate` and `duplicate_of` (`POST /{ledger_id}/metadata`) |
| `GetLedger` | `GET /ledgers/{id}` |
| `ListLedgers` | `GET /ledgers?limit=&offset=` |

No money moves.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrDuplicateReference` | the `Key` is already used by a ledger with another `Name` | pick another key, or use the existing name |
| `ErrInvalidInput` | no `Key`; opening an account in a duplicate ledger | add a key; use the ledger named in the message |
| `ErrLedgerNotFound` | the ledger doesn't exist; also matches `ErrNotFound` | fix the id or the configuration |
| `ErrWrongKind` | an account id or house account passed as a ledger id; also matches `ErrInvalidInput` | pass a `LedgerID` |
| `ErrUnavailable` | Blnk can't be reached | retry; the call is safe to repeat with the same `Key` |

## Related pages

- [Setup](setup.html): connecting to Blnk and the provisioning step.
- [Create customer accounts](customer-accounts.html): `LedgerID` on `OpenAccount`.
- [House accounts](house-accounts.html): the general ledger and the chart of accounts.
- [Freeze](freeze.html): `FreezeLedger` and `UnfreezeLedger`.
