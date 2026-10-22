# Refunds and reversals

**Scenario:** Ada pays a shop ₦15,000 for three items. One comes back (a
₦5,000 refund), the support agent clicks "refund" twice, and then the order
is cancelled. Separately, a payout to Ada's bank bounces.

Refunds, corrections, chargebacks and bounced payouts all use one call:
`Transactions.Reverse`. It moves money back the way an entry moved it.

## The order

A merchant is a customer too, with a wallet of its own:

{{code 08-refunds/main.go order}}

## A partial refund

{{code 08-refunds/main.go partial}}

| `Reversal` field | |
| --- | --- |
| `Entry` | the entry id (`Entry.ID` from `Record`) |
| `Amount` | the zero `Money` undoes all of it, or all that's left |
| `Ref` | your id for this reversal. Required for a partial one, recommended for a full one. |
| `Reason` | required, for the audit trail |

## Clicked twice: the same refund

`Reverse` is idempotent on `Ref`. The second click returns the refund
already made, and no money moves:

{{code 08-refunds/main.go twice}}

Without a `Ref`, a retried full reversal can only answer
`ErrAlreadyReversed`: it can't tell its own refund from someone else's.
Always pass one.

## Never more than was paid

Reversals of one entry, full and partial, can never add up to more than it
moved, even when two run at the same time (they take turns).

{{code 08-refunds/main.go toomuch}}

## Cancel the rest

A full reversal after partial ones undoes only what's left. Once nothing is
left, `Reverse` is `ErrAlreadyReversed`.

{{code 08-refunds/main.go rest}}

## A bounced payout

A payout recorded straight to `@World` that the bank returns is reversed the
same way. The money comes back from `@World`, which as a system account may
go below zero:

{{code 08-refunds/main.go bounce}}

## The whole example

{{run 08-refunds}}

## Good to know

- **A refund takes money from where it went.** If the shop had already spent
  the ₦15,000, the refund would fail with `ErrInsufficientFunds`: a customer
  is never pushed below zero. System accounts (`@World`, `@FeeIncome`,
  clearing accounts) may go negative.
- **Splits reverse in full only.** Reversing a transfer with a fee undoes
  both postings in one entry. Which postings of a split to undo in part is a
  business decision: reverse it in full, then record what should stand.
- **Only applied entries can be reversed.** For money still in a hold, use
  `Holds.Cancel`.
- Each reversal is its own entry, referenced `reversal:<entry>:<n>` and
  tagged `reversal_of`, `reversal_ref` and `reversal_reason` in its metadata.
