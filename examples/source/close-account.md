title: Close an account
group: ops
order: 4
lead: Close a customer account for good: sweep what is left to another account or out through a provider, with a reason and a second person's approval.
---
## When to close

A customer leaves Oja, an account was opened twice by mistake, or a merchant is offboarded. Closing is final. A closed account is never reopened, and every later movement into or out of it is refused. If the customer comes back, open a new account.

| Situation | Use |
|---|---|
| Stop money for a while during a review | `ops.FreezeAccount`. See [Freeze and unfreeze](freeze.html) |
| Move the balance to the customer's other account, then close | `ops.CloseAccount` with `SweepTo` set to that account |
| Pay the balance out to the customer's bank, then close | `ops.CloseAccount` with `SweepTo` set to the payout clearing account, or `wallet.Withdraw` first and close at zero |
| Close an account with nothing in it | `ops.CloseAccount` without `SweepTo` |
| A savings pot under an account | `ops.CloseAccount` on the pot, sweeping to its parent |

## Sweep to another account

@sample sweep | Ada closes her side-business account

- The remaining balance moves to `SweepTo` in one transfer, then the account is marked `closed`.
- The returned `*Account` is read back after the closure, so `Status` is `closed` and `Balance` is zero.
- `Reason`, `RequestedBy` and `ApprovedBy` are stored on the account as `status_reason`, `closed_requested_by` and `closed_approved_by`.
- The sweep is an ordinary Blnk transaction. Its `Reference` is the one you gave, its description is `Account closure`, and it carries `txn_type: account_closure` and `close_reason`.

## Pay the balance out

`SweepTo` also takes a house account. Sweep to a provider's clearing account when the money goes to the customer's bank.

@sample payout | Tunde leaves; Flutterwave pays his bank

- `ledger.ClearingAccount("flutterwave")` gives `@FlutterwaveClearing`. `ledger.AccountID("@PayoutClearing")` works the same way when you don't track payouts per provider.
- The sweep posts at once. It is not a pending withdrawal, so there is nothing to `Confirm` or `Cancel`. Send the bank transfer yourself, then clear it against your bank with `ops.RecordSettlement` as usual. See [Provider settlement](settlement.html).
- If you want the payout to go through your normal withdrawal flow (pending until Flutterwave confirms, auto-cancel on timeout), call `wallet.Withdraw` for the full balance first, `Confirm` it when Flutterwave reports success, then close the empty account.

## An empty account

@sample zero | A duplicate account opened by mistake

With a zero balance there is nothing to move, so `SweepTo` and `Reference` can be left out. `Reason` and `ApprovedBy` are still required.

## The fields

| Field | Required | What it does |
|---|---|---|
| `Reason` | yes | why the account is closed, e.g. `customer request` or `dormant 24 months`. Stored as `status_reason` and on the sweep as `close_reason` |
| `ApprovedBy` | yes | who approved the closure. Missing gives `ErrApprovalRequired` |
| `RequestedBy` | no | who asked for it. When set, it must differ from `ApprovedBy`, ignoring case |
| `SweepTo` | when the balance isn't zero | where the balance goes: a customer account, or a house account such as `@PayoutClearing` |
| `Reference` | when the balance isn't zero | the sweep's idempotency key |

## Approvals

Closing can't be undone, so it takes two people, like an [adjustment](adjustments.html). In Oja's back office, ops.kemi raises the request and lead.femi approves it.

@sample approval

- The check compares names without regard to case, so `ops.kemi` can't approve as `Ops.Kemi`.
- humanblnk records the names you pass. It doesn't know who your staff are, so check in your own app that the approver is allowed to approve.

## What blocks a closure

`CloseAccount` refuses with `ErrPendingActivity` while money could still reach or leave the account after it is closed. Each refusal names what to deal with first.

| Blocker | Why | How to clear it |
|---|---|---|
| a pending transaction (hold, withdrawal, pending deposit or transfer) | it would settle into or out of a closed account | `wallet.Confirm` or `wallet.Cancel` it |
| a scheduled transfer into or out of the account | Blnk posts it by itself, so humanblnk couldn't stop it later | wait until it posts. humanblnk has no call to remove one |
| a pot under the account that is still open | the pot's money would be stranded | close the pot first with `ops.CloseAccount`, sweeping to the parent |
| an escrow paying into or out of the account that still holds money | releasing or refunding it would touch a closed account | `wallet.ReleaseEscrow` or `wallet.RefundEscrow` it |
| a negative balance | an overdraft must be settled first | fund the account, or correct it with `ops.Adjust` (`ErrInvalidInput`) |

### Pending and scheduled transactions

@sample pending | Ada's card hold, then her standing order

### Pots and escrows

@sample pots-escrows | Ada's Rent pot, then an escrow with Chidi Stores

- A pot is an account, so it is closed with the same call. Give its parent as `SweepTo` when it has money in it.
- An escrow blocks the closure whether the account is the payer or the beneficiary. An escrow that has been fully released or refunded doesn't.

## After the closure

@sample final

- `Status` stays `closed`. `ops.UnfreezeAccount` and `ops.FreezeAccount` refuse it with `ErrAccountClosed`.
- Every new movement touching the account is refused with `ErrAccountClosed`: deposits, transfers in and out, withdrawals, holds, refunds back to it.
- Calling `CloseAccount` again on a closed account returns it unchanged without checking the input, so a retry is safe.

## Money that arrives late

A provider reversal or a late transfer can still turn up after the account is closed. The deposit is refused with `ErrAccountClosed`, but the provider has the money. Park it in `@Suspense` with `ops.HoldInSuspense`, then claim it for the customer's new account with `ops.ClaimFromSuspense`, or send it back with `ops.ReturnFromSuspense`. [Unidentified money](suspense.html) walks through it with a late payment for Chidi Stores' old account.

## What happens in Blnk

The sweep is one transaction from the account to `SweepTo`, for the whole balance. Blnk debits the source and credits the destination.

Ada's side account, ₦15,500 to her main account:

| Account | Debit | Credit |
|---|---|---|
| Ada's side account | 15,500 | |
| Ada's main account | | 15,500 |

Tunde's account, ₦125,000 paid out through Flutterwave:

| Account | Debit | Credit |
|---|---|---|
| Tunde's account | 125,000 | |
| `@FlutterwaveClearing` | | 125,000 |

Then the balance's metadata is updated: `status: closed`, `status_reason`, `closed_requested_by`, `closed_approved_by` and `status_changed_at`. An account with a zero balance gets the metadata update only. No fee is charged on a sweep.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no `Reason`; a balance but no `SweepTo` or `Reference`; `SweepTo` is the account itself; the balance is negative | fix the call, or settle the overdraft |
| `ErrApprovalRequired` | no `ApprovedBy`, or `ApprovedBy` is the same person as `RequestedBy` | get a second person to approve |
| `ErrPendingActivity` | a pending or scheduled transaction, an open pot, or an escrow holding money | clear the blocker the message names, then retry |
| `ErrAccountClosed` | `SweepTo` is closed; any movement on the account after closure | sweep somewhere else; open a new account for the customer |
| `ErrAccountFrozen` | `SweepTo` is frozen | unfreeze it or sweep somewhere else. The account being closed may itself be frozen |
| `ErrLedgerFrozen` | the account's ledger is frozen and there is a balance to sweep | wait for `ops.UnfreezeLedger` |
| `ErrWrongKind` | a house account such as `@FeeIncome` passed as the account to close | house accounts are never closed |

## Related

- [Freeze and unfreeze](freeze.html): stop an account for a while instead of for good.
- [Unidentified money](suspense.html): money that arrives for a closed account.
- [Savings pots and standing orders](savings-pots.html) and [Escrow](escrow.html): the things that block a closure.
- [Provider settlement](settlement.html): clear the payout clearing account against your bank.
