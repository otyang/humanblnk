title: Statements and history
group: wallet
order: 16
lead: Show a customer's activity, build a monthly statement that is checked against Blnk, export it as CSV, and read a balance at any moment.
---
## The situation

Ada opens the Oja app and scrolls through her recent activity. At the end of September she downloads a statement for her landlord, who wants to see an opening balance, every payment and a closing balance that adds up. Oja's support team also needs her balance as it stood on a given day.

The samples on this page run on one month of Ada's activity: a Paystack top-up with a fee, a transfer to Tunde with a fee, an order from Chidi Stores, a transfer into her rent pot scheduled for the 25th, a Flutterwave withdrawal confirmed the next day, Tunde paying back half a cab fare, and a withdrawal Flutterwave never confirmed.

| Need | Use |
|---|---|
| A list of recent activity, with pending and scheduled items | `wallet.History` |
| A document for a period: opening, entries, closing | `wallet.Statement` |
| The same as a file | `Statement.WriteCSV` |
| One balance at one moment, e.g. month end | `wallet.BalanceAt` |
| The same for a house account such as `@FeeIncome` | `ops.HouseHistory`, `ops.HouseStatement`, `ops.HouseBalanceAt` |

## Recent activity: wallet.History

`History` lists the account's transactions, newest first, in every status.

@sample history | The five most recent items

- Each item is a `ledger.Receipt` with `Reference`, `EntryStatus` (`posted`, `pending`, `voided`, `queued`, `scheduled`, `rejected`), `Amount`, `Description`, `CreatedAt` and the rest.
- `Total` is the number of matching transactions, for paging.
- History shows every record Blnk keeps. A movement with a fee appears as its legs (`p2p_20001-1`, `p2p_20001-2`). Confirming a pending withdrawal adds a posted record for each leg (here `ref_txn_000024` and `ref_txn_000025`), and the original pending records stay in the list.
- For a list of what really moved the balance, use `Statement`.

### Filtering and paging: HistoryQuery

@sample filter

| Field | What it does |
|---|---|
| `Direction` | `ledger.All` (default), `ledger.Incoming` (the account received), `ledger.Outgoing` (the account paid) |
| `Statuses` | only these statuses, in either vocabulary: `"posted"` or Blnk's `"APPLIED"`, `"pending"` or `"INFLIGHT"`, and so on; empty means all |
| `Since` | created at or after this time |
| `Until` | created before this time |
| `Limit` | page size; default 20, at most 100 |
| `Offset` | where the page starts |

`HistoryPage` returns `Items`, the `Limit` and `Offset` used, and `Total`. `Since` and `Until` filter on when a transaction was created, so a scheduled transfer is found by the day it was set up.

## A statement: wallet.Statement

`Statement` covers `[from, to)`: from the first moment, up to but not including the last. Use the customer's timezone for the dates.

@sample statement | Ada's September statement, in Lagos time

`Statement` fields:

| Field | What it holds |
|---|---|
| `AccountID`, `Currency` | the account |
| `From`, `To` | the period as you asked for it |
| `Opening` | Blnk's historical balance just before `From` |
| `Credits`, `Debits` | total in and total out over the period |
| `Closing` | `Opening` plus `Credits` minus `Debits` |
| `Lines` | one `StatementLine` per posted entry |

Each `StatementLine` has `Date`, `Reference`, `Description`, `Credit` (money in, zero for a debit), `Debit` (money out, zero for a credit), `Balance` (the running balance after the line), `Counterpart` and the full `Receipt`.

How the lines are built:

- Only posted (`APPLIED`) entries are listed. Pending holds, pending withdrawals, voided and scheduled transfers are left out. The withdrawal Flutterwave never confirmed expired and is not on the statement.
- Entries are dated by when they took effect. The rent transfer was created on 5 September and appears on 25 September, the day it posted. The withdrawal requested on the 10th appears on the 11th, when Flutterwave confirmed it.
- A fee is its own line, straight after the payment it belongs to, with `@FeeIncome` as the counterpart.
- `Counterpart` is the other account: a customer account id, or the `@Name` of a house account such as `@PaystackClearing`.
- A deposit's fee never touches Ada's account: she is credited the net amount, ₦49,250.

### Checked against Blnk

The running balance is worked out from the lines and then compared with Blnk's own historical balance at `To`. If they differ, for example because an entry was backdated or posted while the statement was being read, `Statement` returns `ErrConflict` instead of a statement that doesn't add up. Ask again.

A statement holds at most 10,000 lines. A longer period is refused with `ErrInvalidInput`; ask for one month at a time.

## Export: WriteCSV

`WriteCSV` writes the statement to any `io.Writer`: an HTTP response, a file or a buffer.

@sample csv

- The first row is the header, then an opening row, one row per line, and a closing row with the period's total credits, total debits and the closing balance.
- Amounts are plain decimals with every decimal place (`49250.00`), and a zero credit or debit is left blank.
- Dates are in UTC, in RFC 3339.

## A balance at a moment: wallet.BalanceAt

@sample balance-at | Ada's balance at four moments

- `BalanceAt` returns the posted balance at that instant. A pending amount isn't included: on 10 September the withdrawal was held but not yet confirmed, so the balance still includes it.
- It uses Blnk's historical balance endpoint, recomputed from transactions. That needs Blnk Core 0.10.1 or later.
- `GetAccount` gives the balance now, with `Available` (balance minus pending debits).

## What happens in Blnk

Nothing is written. These calls only read:

| Call | Blnk reads |
|---|---|
| `History` | `POST /transactions/filter` by `balance_id` (or `source` / `destination` for a direction), newest first |
| `Statement` | the historical balance before `From` and at `To` (`GET /balances/{id}/at`), and the `APPLIED` transactions by `effective_date`, 100 per request |
| `BalanceAt` | `GET /balances/{id}/at` |

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | `from` is not before `to`; more than 10,000 lines in the period | fix the dates; ask for a shorter period |
| `ErrWrongKind` | a house account was passed to a wallet method | use `ops.HouseStatement` and the other house methods |
| `ErrNotFound` | the account doesn't exist | check the id |
| `ErrConflict` | the lines don't add up to Blnk's balance at `To` | ask again; if it persists, look for backdated entries |

## Related pages

- [House accounts](house-accounts.html): the same history and statements for `@FeeIncome` and the clearing accounts.
- [Fees and pricing](fees.html): where the fee lines come from.
- [Savings pots and standing orders](savings-pots.html): scheduled transfers.
- [Withdrawals](withdrawals.html): pending, confirmed and expired payouts.
- [Reports](reports.html): balances across the whole ledger.
