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
| Reverse a payment that had a fee, or a whole split, fee included | `wallet.Refund` by the payment id |
| Refund part of a payment, possibly several times | `wallet.RefundPartial` |
| Give back what the recipient got, but keep the fee | `RefundPartial` by the payment id (it picks the recipient's leg) |
| Refund one leg of a split | `RefundPartial` or `Refund` by the leg's own id |
| The recipient has spent the money | collect it first; see [When the recipient has spent it](#when-the-recipient-has-spent-it) |
| Money still in escrow | `wallet.RefundEscrow`; see [Escrow](escrow.html) |
| Money on hold, not yet posted | `wallet.Cancel`; see [Card holds](card-holds.html) |

## A full refund: wallet.Refund

`Refund` asks Blnk to reverse the transaction. Blnk does it once, for the whole amount.

@sample full | Chidi Stores cancels a ₦15,000 order

- The refund's reference is the original's transaction id plus `_refund`.
- `Reason` is stored as `meta_data.refund_reason`. `Description` and `MetaData` go on the refund too.
- A second `Refund` of the same transaction fails with `ErrAlreadyFinalized`.
- Every customer account that received the money must still have it available. Otherwise nothing is reversed and the refund fails with `ErrInsufficientFunds`. See [When the recipient has spent it](#when-the-recipient-has-spent-it).
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

A transfer with a fee is stored as two legs: one to the recipient, one to `@FeeIncome`. `Refund` by the payment's id reverses both, so Ada gets the fee back too.

@sample with-fee | Ada paid ₦10,000 plus a ₦50 fee

- The receipt's `Legs` lists each reversal: the account the money came back from, the amount and the reversal's transaction id.
- Each leg is reversed with its own Blnk call. Its reference is the leg's id plus `_refund`.
- The receipt's `Amount` is the total this call refunded. Its `Reference` and `TransactionID` are those of the largest reversal, the payee's leg, which comes first in `Legs`.
- After a full refund, a second `Refund` fails with `ErrAlreadyFinalized`.

To give back only what the recipient got and keep the fee, use `RefundPartial` by the payment's id. It picks the recipient's leg.

@sample keep-fee

## A split

A [split payment](split-payments.html) works the same way. `Refund` by the split's id reverses every leg, Oja's share included.

@sample split-full | A card order is cancelled after it was split

The money goes back to where it came from. For a card payment that is `@PaystackClearing`. Return the money to Ada's card through Paystack, and the clearing account then matches what Paystack reports. See [Settlement](settlement.html).

To refund part of one recipient's share, use `RefundPartial` with the leg's id. For a split that paid several recipients the library can't guess which one is refunding, so the split's own id is refused. A split paid to one customer and `@FeeIncome` works like a payment with a fee: the customer's leg is picked.

@sample split-leg | Chidi Stores refunds ₦3,800 of its share of a card order

## When the recipient has spent it

Blnk reverses a transaction with overdraft allowed, so on its own it would take the money from the recipient even when they no longer have it. The library checks first. Each customer account that received money must still have that much available.

@sample spent | Tunde cashed out the money Ada sent by mistake

- The check runs before anything is reversed. A refused refund writes nothing, for every leg.
- House accounts such as `@FeeIncome` are not checked. They may go negative.
- `RefundPartial` refuses the same way.
- Collect the money from the recipient first, then refund. To claw it back by hand, see [Adjustments](adjustments.html).

## Retrying a refund

A full refund of a payment with several legs makes one Blnk call per leg. Each reversed leg is marked with `refunded_by` in its metadata. If the refund stops part way, for example because Blnk timed out, call `Refund` again with the same id: it skips the legs already reversed and finishes the rest.

@sample retry | The fee leg was reversed before the refund stopped

- The receipt's `Legs` lists only the reversals made by this call.
- Once every leg is reversed, `Refund` fails with `ErrAlreadyFinalized`.

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

Blnk records it with reference `<id>_refund` and overdraft allowed. The library has already checked that Chidi Stores holds ₦15,000, so its account can't go below zero. A partial refund of ₦2,000 (`RefundPartial`) makes the same entries for ₦2,000, as a transfer with `txn_type: refund`, `refund_of` and `refund_reason` in its metadata. The library then updates `refunded_precise` on the original.

A full refund of Ada's ₦10,000 payment with a ₦50 fee is two reversals, one per leg:

| Account | Debit | Credit |
|---|---|---|
| Chidi Stores | ₦10,000.00 | |
| `@FeeIncome` | ₦50.00 | |
| Ada | | ₦10,050.00 |

Each has reference `<leg id>_refund`, and each original leg gets `refunded_by` with the reversal's id in its metadata.

A partial refund of Chidi Stores' leg of a card order:

| Account | Debit | Credit |
|---|---|---|
| Chidi Stores | ₦3,800.00 | |
| `@PaystackClearing` | | ₦3,800.00 |

Reversing a fee leg debits `@FeeIncome` and credits the payer. House accounts may go negative here.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrAmountExceeded` | a partial refund over what is left to refund | refund at most the amount in the error |
| `ErrAlreadyFinalized` | `Refund` twice, or after every leg was reversed; `Refund` after partial refunds; `RefundPartial` after a full `Refund` | use the other call, or stop: it is already refunded |
| `ErrInsufficientFunds` | a customer account that received the money no longer has it available | collect it from the recipient first, then refund |
| `ErrInvalidInput` | the transaction isn't posted; `RefundPartial` on a split with several recipients given by its parent id; no `Reference` on a partial refund; no transaction id | refund the leg, or wait until it posts |
| `ErrNotFound` | no transaction with that id | check the id |
| `ErrAccountFrozen` / `ErrAccountClosed` | the payer or the recipient is frozen or closed | see [Freeze accounts](freeze.html) |

## Related

- [Disputes](disputes.html): hold the money while a complaint is looked into, before deciding on a refund.
- [Escrow](escrow.html): refund before the seller is ever paid.
- [Split payments](split-payments.html): where leg ids come from.
- [Adjustments](adjustments.html): approved corrections that are not refunds.
