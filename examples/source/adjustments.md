title: Manual adjustments
group: ops
order: 7
lead: Correct a customer balance by hand, for cashback, goodwill or a posting mistake, with a reason, a requester and a second person who approves it.
---
## When to adjust

Most money moves because something happened outside: a deposit, a payout, a card payment. Sometimes Oja has to change a balance by hand. Paystack sent a webhook twice and Tunde was credited twice. Marketing promised Ada ₦500 cashback. Support wants to pay Tunde ₦1,000 for a failed transfer.

`ops.Adjust` posts that correction as an ordinary Blnk transaction between the customer account and a house account. It refuses to run without a reason and an approver who isn't the requester, and it writes all three into the transaction's metadata, so every hand-made change can be traced later.

Pick the call by situation:

| Situation | Use |
|---|---|
| Give a customer money the business pays for (cashback, goodwill, a fee you forgive) | `ops.Adjust` with `AdjustCredit` |
| Take back money posted by mistake | `ops.Adjust` with `AdjustDebit` |
| Undo one specific transaction in full | `wallet.Refund`; see [Refunds](refunds.html) |
| Money arrived and you don't know whose it is | `ops.HoldInSuspense`; see [Suspense](suspense.html) |
| A provider's report differs from the ledger | find the gap first with [Reconciliation](reconciliation.html), then adjust |

`Adjust` is on `Ops` only. Customer-facing code holds a `Wallet` and can't call it.

## Credit a customer: cashback

Kemi from operations asks for Ada's cashback. Femi, her lead, approves it.

@sample cashback | ₦500 cashback for Ada

- `Type: ledger.AdjustCredit` moves money from the house account into the customer's account.
- The description is `Adjustment: ` followed by your `Reason`.
- `MetaData` holds `adjust_reason`, `requested_by` and `approved_by`, plus `txn_type: adjustment`, which humanblnk sets on every adjustment. Filter on it when you audit corrections.
- No fee is charged and no [limits](limits.html) are checked: an adjustment isn't a customer payment.

## Take money back: an error correction

@sample correction | A duplicated deposit is reversed

- `Type: ledger.AdjustDebit` moves money from the customer's account to the house account.
- Put the reference of the bad entry in `Reason`, so the audit trail points at it.
- If the duplicate is one transaction you can name, `wallet.Refund` also works. Use `Adjust` when the fix doesn't map to one transaction, or when it is part of one.

## Safe to retry

`Reference` is the idempotency key, as for every movement. The same reference with the same input returns the original with `Replayed: true` and moves nothing.

@sample retry

Build the reference from what the correction is about (`adj_dup_paystack_T300`), not from a timestamp, so a second click in your ops tool can't post the correction twice.

## Choosing the other side: Counterparty

By default the other side of every adjustment is `@Adjustments`. Set `Counterparty` to post against another house account, for example to charge goodwill to a marketing budget.

@sample counterparty | Goodwill paid from @Marketing

- `Counterparty` must be a house account name starting with `@`. A customer account id is refused.
- A house account is created on first use, so `@Marketing` exists once this runs.
- `@Marketing` isn't in `ledger.Chart`, so its type is unknown and its balance shows as Blnk stores it: negative, because it was debited. Add it to `Chart` at start-up as an expense (`ledger.GLDef{Type: ledger.GLExpense, Normal: ledger.DebitNormal}`) and its `Natural` balance reads positive. See [House accounts](house-accounts.html).

## Going below zero: AllowOverdraft

A debit adjustment can't take the account below zero unless you say so. The usual case is a claw-back where the customer has already spent the money.

@sample overdraft | Claw back ₦25,000 from a ₦20,500 balance

- Without `AllowOverdraft`, Blnk refuses the debit with `ErrInsufficientFunds` and nothing moves.
- With it, the account goes negative. Ada now owes Oja ₦4,500. Her next deposit brings the balance back up from there.
- `AllowOverdraft` only matters for `AdjustDebit`. A credit always posts, because the house account is the source and may go negative.

## The @Adjustments account

`@Adjustments` is in the chart as equity, credit-normal. Its lifetime credits and debits show how much was taken back from and given to customers by hand.

@sample house

Here ₦28,000 came in from the two debits and ₦500 went out as cashback. The goodwill went to `@Marketing`, so it isn't counted here. Read the full list of entries with `ops.HouseStatement`; see [Trial balance and reports](reports.html).

## The fields

| Field | Required | What it does |
|---|---|---|
| `AccountID` | yes | the customer account corrected. A house account is refused with `ErrWrongKind` |
| `Type` | yes | `ledger.AdjustCredit` (money in) or `ledger.AdjustDebit` (money out) |
| `Amount` | yes | a positive amount in the account's currency |
| `Reason` | yes | why. Stored as `adjust_reason` and in the description |
| `ApprovedBy` | yes | who approved it. Stored as `approved_by` |
| `RequestedBy` | no | who asked for it. Stored as `requested_by`. When set, it must differ from `ApprovedBy`, ignoring case |
| `Reference` | yes | idempotency key |
| `Counterparty` | no | the house account on the other side; default `@Adjustments` |
| `AllowOverdraft` | no | lets an `AdjustDebit` take the account below zero |

> warn: humanblnk checks that the two names differ. It doesn't know who your staff are. Take `RequestedBy` from the logged-in user and `ApprovedBy` from a real approval step in your own tool, never from the same form field.

> tip: When `RequestedBy` is empty only `ApprovedBy` is checked. Always set both so the record shows two people.

## What happens in Blnk

One transaction, with `txn_type: adjustment` in its metadata.

| Adjustment | Account | Debit | Credit |
|---|---|---|---|
| `AdjustCredit` ₦500 | `@Adjustments` (or `Counterparty`) | 500 | |
| | Ada's account | | 500 |
| `AdjustDebit` ₦3,000 | Tunde's account | 3,000 | |
| | `@Adjustments` (or `Counterparty`) | | 3,000 |

A credit is sent with `allow_overdraft`, since the house account is the source. A debit is sent with `allow_overdraft` only when you set `AllowOverdraft`. Blnk stores house accounts by balance id, so the receipt's `Source` or `Destination` shows the id of `@Adjustments`, not its name.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrApprovalRequired` | no `ApprovedBy`, or `ApprovedBy` is the same person as `RequestedBy` | get a second person to approve |
| `ErrInvalidInput` | no `Reason`, no `Reference`, no or bad `Type`, a non-positive `Amount`, or a `Counterparty` that doesn't start with `@` | fix the call |
| `ErrWrongKind` | `AccountID` is a house account or a ledger id | pass a customer account |
| `ErrInsufficientFunds` | an `AdjustDebit` would take the account below zero | set `AllowOverdraft` if that is intended |
| `ErrAccountFrozen`, `ErrAccountClosed` | the account is frozen or closed | unfreeze it first; a closed account can't be corrected. See [Freeze accounts](freeze.html) |
| `ErrLedgerFrozen` | an `AdjustDebit` on an account whose ledger is frozen | unfreeze the ledger |
| `ErrDuplicateReference` | the reference was already used for a different movement | use a new reference |

## Related pages

- [Refunds](refunds.html) to reverse one transaction.
- [Suspense](suspense.html) for money you can't match to a customer yet.
- [Reconciliation](reconciliation.html) to find the gaps that need adjusting.
- [Trial balance and reports](reports.html) to check the books after corrections.
