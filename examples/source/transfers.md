title: Transfers between customers
group: wallet
order: 3
lead: Move money from one account to another: instant, with a fee, held until both sides agree, scheduled for later, or to and from the business's own accounts.
---
## The situation

Ada sends Tunde his share of the rent from inside the Oja app. No provider is involved: the money moves between two accounts in the same ledger with `wallet.Transfer`. The sender pays the amount plus any fee; the recipient gets the amount.

Pick the options by situation:

| Situation | Use |
|---|---|
| A normal payment, posted at once | `wallet.Transfer` |
| A payment with a fee | `Fee` on the call, or leave it out and let `Config.Fees` price it |
| A payment that settles later, possibly for less | `Pending: true`, then `wallet.Confirm` and `wallet.Cancel` |
| A payment on a future date | `ScheduledFor` |
| A repeating payment | `wallet.Schedule`, see [Savings pots and standing orders](savings-pots.html) |
| Cashback, airtime, or any payment to or from the business | `From` or `To` as `ledger.AccountID("@Name")` |
| One payment to several people | `wallet.Split`, see [Split payments](split-payments.html) |
| Many payments at once | `wallet.Bulk`, see [Bulk payouts](bulk-payouts.html) |

## A transfer

@sample basic | Ada sends Tunde ₦12,500

- `Amount` is what the recipient receives.
- `Reference` is required. Generate it once, when the customer taps "Send", and reuse it on every retry.
- `Description` is stored on the transaction and shows on both statements. Without one, Blnk gets "Transfer".
- The receipt's `EntryStatus` is `posted`: the money has moved.

## With a fee

The fee is charged to the sender on top of `Amount` and goes to `@FeeIncome`.

@sample fee | Config.Fees charges ₦10

- Leave `Fee` out and `Config.Fees` prices the transfer with `Kind: ledger.FeeTransfer` and the sender's account. Set `Fee` to charge a fixed amount, or `ledger.NoFee` for a free one. With neither `Fee` nor `Config.Fees`, the call is refused.
- The receipt's `Amount` is the total debited. The legs show what went to the recipient and what went to fees.
- Fee and net amount are also in metadata as `fee`, `fee_precise`, `net_amount` and `net_precise`.

## Pending: settle later

`Pending: true` reserves the money on the sender's account without moving it. It leaves `Available` at once but stays in `Balance` until you confirm.

@sample pending | Ada reserves ₦30,000 for a phone, then pays ₦25,000

- `wallet.Confirm(ctx, id, nil)` settles all of it. With an amount, only that part settles and the rest stays held.
- `wallet.Cancel` releases whatever is still held.
- A pending transfer with a fee has two legs, so it can only be confirmed in full.
- Unlike a withdrawal, a pending transfer has no automatic expiry. For a hold that lapses, use `wallet.Hold` with `ExpiresAt`, see [Card holds and capture](card-holds.html).

## Scheduled for later

`ScheduledFor` asks Blnk to post the transfer at that time. Until then it is `SCHEDULED` and nothing moves.

@sample scheduled | ₦2,000 tomorrow morning

- The in-memory Blnk used here posts it when its clock passes the date (`core.Advance`). Real Blnk does the same on its own schedule.
- Account status and limits are checked when you create it. When Blnk posts it later, humanblnk is not involved, so those checks are not repeated. That is why `ops.CloseAccount` refuses an account with scheduled transfers.
- A transfer can't be both pending and scheduled.

## To and from house accounts

`From` and `To` also accept a house account, written `ledger.AccountID("@Name")`. Use this for money the business pays to a customer, or a customer pays to the business.

@sample house | Cashback in, airtime out

- A house account starts at zero, so paying out of it needs `AllowOverdraft: true`. Its balance going negative is the cost of the cashback.
- House accounts are never frozen and have no limits.
- `Config.Fees` gets a nil `Account` when the sender is a house account.
- Read house balances with `ops.GetHouseAccount`. See [House accounts](house-accounts.html).

## Overdrafts

`AllowOverdraft: true` lets the sender's balance go below zero. Blnk sets no floor, so your app decides who may overdraw and by how much.

@sample overdraft | Tunde uses his overdraft

## Retries

@sample replay

- The same input with the same reference returns the original with `Replayed: true`. Nothing is posted twice.
- The same reference with a different amount, account or kind of movement is `ErrDuplicateReference`.

## What happens in Blnk

A transfer is one Blnk transaction with `txn_type: transfer`. For the ₦5,000 transfer with a ₦10 fee:

| Account | Debit | Credit |
|---|---|---|
| Ada's NGN account | ₦5,010 | |
| Tunde's NGN account | | ₦5,000 |
| `@FeeIncome` | | ₦10 |

Without a fee there is one destination. With a fee, Blnk stores two leg transactions, `<reference>-1` and `<reference>-2`. A pending transfer holds the same entries `INFLIGHT`; a scheduled one is stored with `scheduled_for` and posts at that time.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInsufficientFunds` | the sender's available balance is less than amount plus fee | show the balance; top up first |
| `ErrLimitExceeded` | over the sender's per-transaction, daily or monthly limit | show `wallet.LimitUsage`; see [Limits](limits.html) |
| `ErrInvalidInput` | no `Reference` or `To`; same account on both sides; pending and scheduled together; no fee and no `Config.Fees` | fix the call |
| `ErrAccountFrozen` | either account is frozen or blocked | wait for review; see [Freeze and unfreeze](freeze.html) |
| `ErrAccountClosed` | either account is closed | stop; the account takes nothing |
| `ErrLedgerFrozen` | the sender's ledger is frozen | wait until operations unfreezes it |
| `ErrDuplicateReference` | the reference was used for a different payment | use a new reference only for a new payment |

## Related

- [Quotes for confirm screens](quotes.html): fee, total and balance after, before Ada taps Confirm.
- [Refunds](refunds.html): send a transfer back, in full or in part.
- [Escrow](escrow.html): hold a payment until delivery.
- [Fees](fees.html) and [Limits](limits.html).
