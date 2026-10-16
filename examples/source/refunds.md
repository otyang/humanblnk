title: Refunds
group: wallet
order: 9
lead: Send money back after a payment: in full in one step, or in parts, with over-refunds refused and every refund linked to the payment it reverses.
---
## The situation

Ada paid Chidi Stores for an order. Sometimes the whole order is cancelled. Sometimes only one item was out of stock, and Chidi Stores refunds part now and maybe more later. Oja must never refund more than Ada paid, and support staff must be able to see which refund belongs to which payment.

| Situation | Use |
|---|---|
| Reverse the whole payment, once | `wallet.Refund` |
| Refund part of a payment, possibly several times | `wallet.RefundPartial` |
| Reverse a payment that had a fee, or one leg of a split | `RefundPartial` by the payment id (it picks the recipient's leg), or either call by the leg's own id |
| Money still in escrow | `wallet.RefundEscrow`; see [Escrow](escrow.html) |
| Money on hold, not yet posted | `wallet.Cancel`; see [Card holds](card-holds.html) |

## A full refund: wallet.Refund

`Refund` asks Blnk to reverse the transaction. Blnk does it once, for the whole amount.

@sample full | Chidi Stores cancels a ₦15,000 order

- The refund's reference is the original's transaction id plus `_refund`.
- `Reason` is stored as `meta_data.refund_reason`. `Description` and `MetaData` go on the refund too.
- A second `Refund` of the same transaction fails with `ErrAlreadyFinalized`.
- The transaction must be posted. For a pending payment that was later confirmed, pass the id that `Confirm` returned (the record of the capture), not the id of the hold. This holds for `RefundPartial` too.

## Partial refunds: wallet.RefundPartial

`RefundPartial` sends part of the amount back as an ordinary transfer from the recipient to the payer. Each refund carries `meta_data.refund_of` with the original's id, and the original's `meta_data.refunded_precise` holds the running total in the smallest unit.

@sample partial | Two items out of stock in a ₦6,000 order

- `Reference` is required, one per refund. Retrying with the same reference replays that refund instead of counting it twice.
- The library adds up the refunds already linked to the payment. Anything over what is left fails with `ErrAmountExceeded`, and the error says how much is left.
- Once a payment has partial refunds, `Refund` is refused with `ErrAlreadyFinalized`. Refund the rest with `RefundPartial`.
- After a full `Refund`, `RefundPartial` is refused with `ErrAlreadyFinalized`.

> warn: Two partial refunds of the same payment sent at the same moment can both pass the remaining-amount check. Run refunds for one payment one at a time in your app, for example through one worker or a row lock.

## Finding refunds

@sample lookup

- `GetTransaction` takes a Blnk transaction id. `GetTransactionByReference` takes your reference.
- For a payment with a fee or a split, both rebuild the parent from its legs, so the parent id and reference still work.

## A payment with a fee

A transfer with a fee is stored as two legs: one to the recipient, one to `@FeeIncome`. `RefundPartial` by the payment's id picks the recipient's leg, so the fee stays with Oja.

@sample with-fee | Ada paid ₦10,000 plus a ₦50 fee

`Refund` can't be used with the payment's id, because Blnk has no stored transaction under it, only the legs. Refund each leg you want to reverse by the leg's own transaction id. To give the fee back, reverse the fee leg:

@sample fee-leg

## One leg of a split

A [split payment](split-payments.html) that paid several recipients needs the leg's id: the library can't guess which recipient is refunding. A split paid to one customer and `@FeeIncome` works like a payment with a fee: the customer's leg is picked.

@sample split-leg | Chidi Stores refunds ₦3,800 of its share of a card order

The refund goes back to where the money came from. For a card payment that is `@PaystackClearing`. Return the money to Ada's card through Paystack, and the clearing account then matches what Paystack reports. See [Settlement](settlement.html).

## Frozen and closed accounts

Blnk reverses a transaction without checking account status, so the library checks it before asking. Money leaves the original recipient and goes back to the payer, and both must be active.

@sample frozen | Ada's account is frozen during a fraud check

- A frozen account on either side gives `ErrAccountFrozen`. Unfreeze it and refund again.
- A closed account gives `ErrAccountClosed`, for good. To get money to a customer whose account is closed, see [Suspense](suspense.html).

## What happens in Blnk

A full refund of Ada's ₦15,000 payment to Chidi Stores (`Refund`):

| Account | Debit | Credit |
|---|---|---|
| Chidi Stores | ₦15,000.00 | |
| Ada | | ₦15,000.00 |

Blnk records it with reference `<id>_refund` and overdraft allowed. A partial refund of ₦2,000 (`RefundPartial`) makes the same entries for ₦2,000, as a transfer with `txn_type: refund`, `refund_of` and `refund_reason` in its metadata. The library then updates `refunded_precise` on the original.

A partial refund of Chidi Stores' leg of a card order:

| Account | Debit | Credit |
|---|---|---|
| Chidi Stores | ₦3,800.00 | |
| `@PaystackClearing` | | ₦3,800.00 |

Refunding a fee leg debits `@FeeIncome` and credits the payer. House accounts may go negative here.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrAmountExceeded` | a partial refund over what is left to refund | refund at most the amount in the error |
| `ErrAlreadyFinalized` | `Refund` twice; `Refund` after partial refunds; `RefundPartial` after a full `Refund` | use the other call, or stop: it is already refunded |
| `ErrInsufficientFunds` | `RefundPartial` when the recipient no longer has the money | collect it from the recipient first, or use `Refund` |
| `ErrInvalidInput` | the transaction isn't posted; a split with several recipients given by its parent id; no `Reference` on a partial refund; no transaction id | refund the leg, or wait until it posts |
| `ErrNotFound` | no transaction with that id; `Refund` on the parent id of a payment with a fee or a split | check the id; refund the legs by their own ids |
| `ErrAccountFrozen` / `ErrAccountClosed` | the payer or the recipient is frozen or closed | see [Freeze accounts](freeze.html) |

> warn: `Refund` posts even when the recipient has spent the money, because Blnk reverses with overdraft allowed. The recipient's customer account goes negative, as Tunde's did above. Use `RefundPartial` when the recipient must have the money to give back.

## Related

- [Disputes](disputes.html): hold the money while a complaint is looked into, before deciding on a refund.
- [Escrow](escrow.html): refund before the seller is ever paid.
- [Split payments](split-payments.html): where leg ids come from.
- [Adjustments](adjustments.html): approved corrections that are not refunds.
