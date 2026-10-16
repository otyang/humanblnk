title: Split payments and marketplaces
group: wallet
order: 8
lead: Pay one amount out to several accounts in a single transaction: a seller, a rider and Oja's commission, with every share computed exactly.
---
## The situation

Ada pays ₦25,000 by card for an order from Chidi Stores. Oja keeps 5% commission, Tunde gets a fixed ₦1,500 for the delivery, and Chidi Stores gets the rest. All three must be paid from the one payment, at the same moment, and the shares must add up to the kobo.

`wallet.Split` does this. It turns every part into an exact amount in your code, then sends one Blnk transaction with one destination per part.

| Situation | `From` | `AllowOverdraft` |
|---|---|---|
| A card or bank payment arrives through a provider | the provider's clearing account, e.g. `ledger.AccountID(ledger.ClearingAccount("paystack"))` | `true` |
| A customer pays from their wallet balance | the customer's account | `false` (the default) |

## A card payment arriving

The money comes from outside, so `From` is `@PaystackClearing`, the account that tracks what Paystack owes Oja. It goes negative like it does for a deposit, which is why `AllowOverdraft` is set.

@sample card-order | ₦25,000 order: 5% commission, ₦1,500 delivery, the rest to the seller

- `Percent: "5"` is 5% of `Amount`. Decimals like `"7.5"` work too.
- `Amount` on a part is a fixed sum in the payment's currency.
- `Rest: true` takes whatever is left after the other parts. At most one part can be `Rest`.
- A part's `To` can be a customer account or a house account such as `ledger.FeeIncomeAccount`.
- `Reference` is the idempotency key. Use the provider's payment id; a retry with the same parts returns the original with `Replayed` set.

## The parts: SplitPart

Set exactly one of `Amount`, `Percent` or `Rest` on each part.

| Field | What it does |
|---|---|
| `To` | the recipient: a customer account, or a house account as `ledger.AccountID("@Name")` |
| `Amount` | a fixed amount. Its currency, if given, must match the payment |
| `Percent` | a share of the total, more than 0 and at most 100, e.g. `"10"` or `"2.5"` |
| `Rest` | everything not taken by the other parts. Must be more than zero |
| `Narration` | the text on that leg. Defaults to `SplitInput.Description` |

## Rounding

Percentages rarely land on whole kobo. Each percentage part is rounded down, and the leftover kobo (the dust) goes to the `Rest` part. With no `Rest` part, the dust goes to the first percentage part. Nothing is left to Blnk's own rounding.

@sample rounding | ₦100.01 split 50/50, then 7.5% with a Rest part

> note: Dust is only ever a few kobo, fewer than the number of parts. If fixed amounts and percentages leave more than that unassigned, and there is no `Rest` part, the split is refused rather than handing the gap to someone.

## Paying from a customer account

When Ada pays from her wallet balance, `From` is her account. Customer accounts never go negative, so a split larger than her available balance fails with `ErrInsufficientFunds`.

@sample from-customer | Ada pays ₦12,000 from her balance with a ₦200 service charge

- The split counts against Ada's [limits](limits.html), like a transfer.
- The account status of `From` and of every customer recipient is checked first: a [frozen](freeze.html) or closed account refuses the whole split.

## Holding the money first: Pending

`Pending: true` reserves the money and creates every leg INFLIGHT. Ada's balance stays the same, but her available balance drops. Confirm it when the order is accepted, or cancel it to release the money.

@sample pending | Hold ₦8,000 until the stock is confirmed

- `wallet.Confirm(ctx, id, nil)` posts every leg. `wallet.Cancel(ctx, id)` releases them all.
- A transaction with several legs can't be partly confirmed by its parent id. Confirm in full, or confirm one leg at a time by that leg's own transaction id. See [Card holds](card-holds.html).

## The receipt and each leg

The receipt has one `Leg` per part: the account, the exact amount, the narration and the leg's own transaction id.

@sample legs

Blnk stores a split only as its legs, one transaction per destination, each pointing to the parent with `ParentTransaction`. The parent id from the receipt still works for lookups: the library rebuilds the parent from its legs.

@sample lookup | Look the payment up again, and find the leg that paid the seller

Keep each leg's transaction id. A [refund](refunds.html) or a [dispute](disputes.html) of a split that paid several people must name the leg it is about.

> tip: Find a leg by its `Account`, not by its position. Blnk doesn't keep legs in the order you sent them. After a lookup, a house account shows as its Blnk balance id rather than `@FeeIncome`.

## SplitInput fields

| Field | What it does |
|---|---|
| `From` | the account debited: a customer account, or a house account such as `@PaystackClearing` |
| `Amount` | the total debited from `From` |
| `Parts` | two or more recipients |
| `Reference` | required. The idempotency key |
| `AllowOverdraft` | lets `From` go negative. Set it when `From` is a provider clearing account |
| `Pending` | create every leg INFLIGHT; confirm or cancel later |
| `Description` | the transaction's description, and the narration of parts that have none. Defaults to "Split" |
| `MetaData` | your own keys. `txn_type` is set to `split` |

A split has no `Fee` field. Take Oja's cut as a part paid to `ledger.FeeIncomeAccount`, as in the first sample.

## What happens in Blnk

The ₦25,000 card order makes one Blnk transaction with three destinations, stored as three legs:

| Account | Debit | Credit |
|---|---|---|
| `@PaystackClearing` | ₦25,000.00 | |
| `@FeeIncome` | | ₦1,250.00 |
| Tunde | | ₦1,500.00 |
| Chidi Stores | | ₦22,250.00 |

Each leg is sent as an exact `precise_distribution` in kobo, so Blnk does no rounding of its own. A payment from Ada's account looks the same with Ada's account on the debit side. With `Pending`, the same entries are INFLIGHT until confirmed. `@PaystackClearing` is cleared when Paystack settles: see [Settlement](settlement.html).

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | fewer than two parts; a part with none or more than one of `Amount`, `Percent` and `Rest`; two `Rest` parts; nothing left for `Rest`; parts adding up to more than `Amount`, or to less with no `Rest`; a bad percent; a part in another currency; no `Reference` | fix the parts |
| `ErrInsufficientFunds` | `From` is a customer account without enough available balance | ask the customer to top up, or charge less |
| `ErrAccountFrozen` / `ErrAccountClosed` | `From` or a customer recipient is frozen or closed | resolve the account first; see [Freeze accounts](freeze.html) |
| `ErrLimitExceeded` | the split is over `From`'s limit | see [Limits](limits.html) |
| `ErrDuplicateReference` | the reference was used for a different transaction | use a new reference |

## Related

- [Refunds](refunds.html) and [Disputes](disputes.html): undoing one leg of a split.
- [Escrow](escrow.html): hold a buyer's money until delivery, then pay the seller.
- [Fees](fees.html): charging a fee on ordinary transfers.
- [Settlement](settlement.html): clearing `@PaystackClearing` when Paystack pays out.
