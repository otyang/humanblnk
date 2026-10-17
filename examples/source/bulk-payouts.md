title: Bulk payouts and payroll
group: wallet
order: 13
lead: Send up to 10,000 transfers in one call: payroll that pays everyone or no one, batches held for approval, and big runs processed in the background.
---
## The situation

Chidi Stores pays its staff through Oja on the 25th of each month. Every salary must go out, or none should, so no one is paid twice when the run is retried. Some batches, like quarterly commissions, need the finance lead's approval before any money moves. And on busy days Oja sends a few thousand cash-back credits at once.

`wallet.Bulk` sends many transfers to Blnk as one batch. The options decide how the batch behaves.

| Situation | Options |
|---|---|
| Payroll: everyone or no one | `Atomic: true` |
| Independent items where one failure shouldn't stop the rest | no options |
| Hold the batch until someone approves it | `Pending: true`, then `Confirm` or `Cancel` with the `BatchID` |
| A very large batch you don't want to wait for | `Async: true`, outcome by webhook |

## Payroll: Atomic

Each item is an ordinary `TransferInput`. Build the list in your own order: a slice keeps it the same on every run.

@sample payroll | Chidi Stores pays two staff on the 25th

- Every item needs its own `Reference`, unique within the batch. Look any item up later with `GetTransactionByReference`.
- Every item needs a fee: `ledger.NoFee`, an amount, or `Config.Fees` to price it. A fee is charged to the item's sender and paid to `@FeeIncome`, as for a transfer.
- The account status of every sender and recipient is checked first. One frozen or closed account refuses the whole call.

### BulkReceipt fields

| Field | What it is |
|---|---|
| `BatchID` | Blnk's id for the batch. Pass it to `Confirm` or `Cancel` for a pending batch |
| `Status` | `applied`, `inflight` (with `Pending`) or `processing` (with `Async`) |
| `Count` | how many items Blnk recorded |
| `Items` | each item's `Reference`, `TransactionID`, `Status` and `Recorded`, in the order given. Empty with `Async` |
| `Replayed` | every item was already recorded by an earlier call with the same items |

## When one item fails

With `Atomic`, one item Blnk can't post fails the whole batch and nothing moves.

@sample atomic-fail | October's run asks for more than Chidi Stores has

Without `Atomic`, Blnk posts the items in order and stops at the first one it can't post. The items before it stay posted. `Bulk` returns the error together with a receipt whose `Status` is `partial` and whose `Items` say which went through.

@sample non-atomic

Sending the same batch again is safe, even after a timeout. Items already recorded are replayed, provided they are unchanged, and only the rest are sent. An item whose reference was used for a different transfer refuses the call with `ErrDuplicateReference`.

## Waiting for approval: Pending

`Pending: true` creates every item INFLIGHT. The money is reserved at the sender but not moved. Pass the `BatchID` to `Confirm` to post every item, or to `Cancel` to release them all.

@sample pending | Quarterly commissions approved; a refund run turned down

- `wallet.Confirm(ctx, batch.BatchID, nil)` and `wallet.Cancel(ctx, batch.BatchID)` act on the whole batch.
- To settle items one by one instead, pass each item's transaction id to `Confirm` or `Cancel`, or up to 100 at once to `ConfirmMany` and `CancelMany`. See [Card holds](card-holds.html).

## Large batches: Async

`Async: true` returns at once with status `processing`. Blnk works through the batch in the background and sends the outcome to your webhook as `bulk_transaction.applied`, `bulk_transaction.inflight` or `bulk_transaction.failed`.

@sample async | The webhook carries the batch id

- `e.Bulk` has `BatchID`, `Status`, `Count` and, for a failed batch, `Error`.
- Match the event to your run by `BatchID`. See [Webhooks](webhooks.html).

## Limits are not checked

`Config.Limits` apply to transfers, but not to bulk items. The same ₦150,000 that a transfer refuses goes through in a batch.

@sample limits | Every account may send at most ₦100,000 per transaction

Only use `Bulk` for money the business sends, or check each customer's limits yourself before building the batch. See [Limits](limits.html).

## BulkTransferInput fields

| Field | What it does |
|---|---|
| `Items` | 1 to 10,000 transfers. `Pending` on an item is ignored: the batch decides |
| `Atomic` | all items post, or none do. The zero value is `false`, so set it for payroll |
| `Pending` | every item INFLIGHT; confirm or cancel with `BatchID` |
| `Async` | process in the background; the outcome arrives by webhook |

## What happens in Blnk

The call is one `POST /transactions/bulk` with `atomic`, `inflight` and `run_async` set from the options. Each item becomes its own transaction with `txn_type: transfer`. For the September payroll:

| Account | Debit | Credit |
|---|---|---|
| Chidi Stores | ₦205,000.00 | |
| Tunde | | ₦85,000.00 |
| Ada | | ₦120,000.00 |

Chidi Stores' debit is the sum of two transactions of ₦85,000 and ₦120,000. An item with a fee also credits `@FeeIncome` with the fee. With `Pending`, the same entries are INFLIGHT until the batch is confirmed.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no items; more than 10,000; a reference used twice in the batch; an item with no fee and no `Config.Fees`; an item with no reference, no amount, or the same sender and recipient | fix the item the message names |
| `ErrInsufficientFunds` | an atomic batch has an item its sender can't cover | fund the sender, or remove the item |
| `ErrAccountFrozen` / `ErrAccountClosed` | any sender or recipient is frozen or closed | remove that item; see [Freeze accounts](freeze.html) |
| `ErrDuplicateReference` | in an atomic batch, an item's reference was already used by another transaction | use new references for a new run |
| `ErrFeePricing` | `Config.Fees` failed for an item | see [Fees](fees.html) |

An error about one item names its position, such as `item 1`, counted from zero.

## Related

- [Transfers](transfers.html): one transfer at a time, with limits checked.
- [Savings pots and standing orders](savings-pots.html): one transfer repeated over time.
- [Withdrawals](withdrawals.html): paying staff out to their banks through Flutterwave.
- [Webhooks](webhooks.html): handling `bulk_transaction.*` events.
