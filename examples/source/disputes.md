title: Disputes
group: wallet
order: 11
lead: Freeze the money in question at the recipient while a complaint is looked into, then send it back to the payer or release it, in full or in part.
---
## The situation

Ada paid Chidi Stores ₦9,800 and says the item never arrived. Chidi Stores has already been paid, so the money sits in its account. While Oja looks into it, Chidi Stores must not be able to spend that money, and nothing should move until there is a decision.

`wallet.OpenDispute` puts the disputed amount on hold in the recipient's account, pointed back at the payer. `wallet.ResolveDispute` ends it.

| Decision | Call | What happens to the held money |
|---|---|---|
| Open the case | `OpenDispute` | held at the recipient |
| The payer wins | `ResolveDispute(…, ledger.DisputeUpheld, nil)` | goes to the payer |
| The payer wins part of it | `ResolveDispute(…, ledger.DisputeUpheld, &amount)` | that amount goes to the payer; the rest stays held |
| The recipient wins | `ResolveDispute(…, ledger.DisputeRejected, nil)` | released back to the recipient |

## Opening a dispute

@sample open | Ada disputes a ₦9,800 payment

- The dispute is a hold: an INFLIGHT transaction from Chidi Stores to Ada. Its `TransactionID` is the dispute id you pass to `ResolveDispute`.
- Chidi Stores' balance is unchanged, but its available balance drops by the disputed amount, so it can't spend it. Ada sees the amount as pending in.
- The hold has no expiry. It stays until you resolve the dispute.
- `meta_data.dispute_of` holds the disputed transaction's id and `dispute_reason` the reason.

### DisputeInput fields

| Field | What it does |
|---|---|
| `TransactionID` | the disputed transaction. It must be posted. For a split paying several people, the leg's id |
| `Amount` | how much is disputed. Empty means the whole amount |
| `Reference` | required; the hold's idempotency key |
| `Reason` | stored as `dispute_reason`. The hold's description is "Dispute: " and the reason, or the original's reference when there is no reason |
| `MetaData` | your own keys, e.g. your support ticket |

## The payer wins: DisputeUpheld

@sample upheld

The held money is posted to Ada. Chidi Stores' balance drops by the amount.

## The recipient wins: DisputeRejected

@sample rejected | The courier's photo shows the right item delivered

The hold is released and Chidi Stores can spend the money again. A rejected dispute always releases everything still held, so it takes no amount.

## Part of a payment, part of a decision

Dispute only part of a payment with `Amount`. When upholding, pass an amount to send only that much back. The rest stays held until you resolve again: uphold more, or reject to release the rest.

@sample partial | One ₦4,000 item of a ₦12,000 order; half of the claim is upheld

## Payments with a fee, and splits

A payment with a fee is stored as two legs: the recipient's and `@FeeIncome`'s. The dispute picks the recipient's leg by itself, so the fee is not held. A [split](split-payments.html) that paid several recipients must be disputed leg by leg, by the leg's own id.

@sample legs

For a card order split out of `@PaystackClearing`, the hold points back at `@PaystackClearing`. If the dispute is upheld, the money goes back there, and you return it to Ada's card through Paystack. A split paid to one customer and `@FeeIncome` works like a payment with a fee.

> note: For a pending payment that was later confirmed, dispute the id that `Confirm` returned. The id of the hold is not a posted transaction.

## What happens in Blnk

Opening the ₦9,800 dispute makes an INFLIGHT transaction with `txn_type: dispute`. Nothing is posted yet:

| Account | Debit | Credit |
|---|---|---|
| Chidi Stores | ₦9,800.00 (pending) | |
| Ada | | ₦9,800.00 (pending) |

`DisputeUpheld` commits it, so the entries post. With an amount, only that amount posts and the rest stays pending. `DisputeRejected` voids what is still pending, so no entries post for it. The original payment is never changed.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrAmountExceeded` | `Amount` is more than the disputed transaction; upholding more than is still held | use at most the amount still held |
| `ErrInsufficientFunds` | the recipient no longer has the disputed amount available | dispute what is there, or recover the rest another way, e.g. an [adjustment](adjustments.html) |
| `ErrInvalidInput` | the transaction isn't posted; a split with several recipients given by its parent id; an amount on a rejected dispute; no `Reference`; an unknown outcome | fix the call; dispute the leg |
| `ErrAlreadyFinalized` | resolving a dispute that is already fully resolved | nothing to do |
| `ErrNotFound` | no transaction with that id | check the id |
| `ErrAccountFrozen` / `ErrAccountClosed` | the recipient is frozen or closed when the dispute is opened | see [Freeze accounts](freeze.html) |

## Related

- [Refunds](refunds.html): send money back without a hold first.
- [Card holds](card-holds.html): how holds, confirm and cancel work underneath.
- [Escrow](escrow.html): hold the money before the seller is paid, instead of after.
- [Split payments](split-payments.html): where leg ids come from.
