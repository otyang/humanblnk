title: Escrow
group: wallet
order: 10
lead: Take a buyer's money now and pay the seller only on delivery, in full or in parts, with refunds to the buyer from the same pot.
---
## The situation

Ada buys an ₦80,000 phone from Chidi Stores, a seller she has never used. She wants her money back if the phone never comes. Chidi Stores wants to know the money is there before it ships. With an escrow, the money leaves Ada's account at once but only reaches Chidi Stores when Oja releases it.

Each escrow is its own Blnk balance, opened for that one deal. What is still held is simply that balance, so Blnk itself refuses to pay out more than is left.

| Situation | Use |
|---|---|
| Take the buyer's money and hold it | `wallet.OpenEscrow` |
| Delivery confirmed: pay the seller | `wallet.ReleaseEscrow` |
| Order cancelled: give it back to the buyer | `wallet.RefundEscrow` |
| Part delivered, part returned | `RefundEscrow` and `ReleaseEscrow`, each with an `Amount` |
| Check what is still held | `wallet.GetEscrow` |

## Opening an escrow: wallet.OpenEscrow

@sample open | Ada pays ₦80,000 into escrow for an order from Chidi Stores

- The escrow account is a new account in Ada's ledger, currency and identity, with `Kind` set to `escrow`.
- The money moves from Ada into it straight away. Chidi Stores' balance doesn't change yet.
- `Reference` is required. A retry with the same reference returns the same escrow, so a timed-out request is safe to repeat.
- `Description` defaults to "Escrow". `MetaData` goes on the funding transaction.

### EscrowInput fields

| Field | What it does |
|---|---|
| `Payer` | the customer account the money comes from. A house account is refused |
| `Beneficiary` | where the money goes on release: a customer account, or a house account as `ledger.AccountID("@Name")`. Must differ from `Payer` |
| `Amount` | the amount taken from the payer, in the payer's currency |
| `Reference` | required; the idempotency key of the funding transaction |
| `Description` | text on the funding transaction |
| `MetaData` | your own keys on the funding transaction, e.g. an order id |

## Paying the seller: wallet.ReleaseEscrow

Leave `Amount` empty to release everything still held.

@sample release | Ada confirms delivery

Once nothing is held, `Status` changes from `held` to `settled`.

## Giving it back: wallet.RefundEscrow

`RefundEscrow` works the same way, but pays the payer.

@sample refund | The order is cancelled before it ships

## Part release, part refund

Set `Amount` to settle part of an escrow. Release and refund can be mixed in any order until nothing is left.

@sample partial | Two items, one arrives broken

- `Held` is what is still in escrow. `Amount` stays the amount first put in.
- Each release or refund needs its own `Reference`.

### EscrowSettleInput fields

| Field | What it does |
|---|---|
| `Amount` | how much to release or refund. Empty means everything still held |
| `Reference` | required; one per release or refund |
| `Description` | text on the transaction. Defaults to "Escrow release" or "Escrow refund" |
| `MetaData` | your own keys. `escrow_id` is added |

## Paying out more than is held

The escrow account can't go negative, so Blnk refuses to release or refund more than it holds. After everything is paid out, a call without `Amount` is refused because there is nothing left.

@sample over-release

## Oja as the beneficiary

The beneficiary can be a house account. Here Oja itself is paid when the job is done.

@sample house | A ₦2,500 booking paid to `@FeeIncome` on completion

## Escrow fields

| Field | What it is |
|---|---|
| `ID` | the escrow account's id. Pass it to `ReleaseEscrow`, `RefundEscrow` and `GetEscrow` |
| `Payer`, `Beneficiary` | as opened |
| `Amount` | the amount first put in |
| `Held` | what is still in escrow: the escrow account's balance |
| `Status` | `held` while anything remains, then `settled` |
| `Reference` | the funding transaction's reference |
| `Account` | the escrow account itself, with all its balances |

## What happens in Blnk

Opening makes a balance with `account_kind: escrow` and the payer, beneficiary, reference and amount in its metadata. Then each step is one transfer:

| Step | Account | Debit | Credit |
|---|---|---|---|
| Open (`escrow_fund`) | Ada | ₦80,000.00 | |
| | Escrow account | | ₦80,000.00 |
| Release (`escrow_release`) | Escrow account | ₦80,000.00 | |
| | Chidi Stores | | ₦80,000.00 |
| Refund (`escrow_refund`) | Escrow account | the amount | |
| | Ada | | the amount |

The name in brackets is the transaction's `txn_type`. A part release or refund makes the same entries for its `Amount`. Opening an escrow counts against Ada's [limits](limits.html); releases and refunds don't, because escrow accounts aren't limited.

> note: An account with an escrow still holding money can't be closed. Release or refund it first. See [Close an account](close-account.html).

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no beneficiary, or the same as the payer; no `Reference`; the id isn't an escrow account; nothing left to release or refund | fix the call; read `GetEscrow` before settling |
| `ErrWrongKind` | the payer is a house account | pay in from a customer account |
| `ErrInsufficientFunds` | the payer can't cover the escrow; a release or refund over what is held | top up, or settle at most `Held` |
| `ErrDuplicateReference` | the reference belongs to a transaction that isn't this escrow's funding | use a new reference |
| `ErrAccountFrozen` / `ErrAccountClosed` | the payer, or the account being paid, is frozen or closed | see [Freeze accounts](freeze.html) |
| `ErrLimitExceeded` | opening the escrow is over the payer's limit | see [Limits](limits.html) |

> warn: The escrow account is created before the money moves. If funding fails, for example for lack of money, an empty escrow account is left behind, and a retry with the same reference opens another. Check the payer's available balance with `GetAccount` before opening an escrow.

## Related

- [Split payments](split-payments.html): pay the seller and Oja's commission from one payment.
- [Disputes](disputes.html): when the seller was already paid.
- [Refunds](refunds.html): sending back money from a posted payment.
- [Create customer accounts](customer-accounts.html): escrow accounts next to pots and main accounts.
