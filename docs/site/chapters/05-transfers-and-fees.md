# Transfers and fees

**Scenario:** Ada sends Bola ₦10,000. You charge a 1.5% fee on top. The app
shows Ada what will happen before she confirms, and refuses if she can't
afford it.

## Step 1: build the transfer as one split

The payment to Bola and your fee are **one** transaction with two postings
on the `To` side. It applies in full or not at all: Bola is never paid
without the fee being recorded, or the other way round.

{{code 05-transfers/main.go transfer}}

Both sides must add up to the same amount, or it's `ErrUnbalanced`. One side
may have several postings, but not both: Blnk moves one account to many, or
many to one.

## Step 2: the dry run

`Preview` checks the transaction exactly as `Record` would, funds included,
but moves nothing and leaves the reference unused:

{{code 05-transfers/main.go preview}}

When it wouldn't apply, the reason is in `p.Rejection` and `err` is nil.
`errors.Is(p.Rejection, humanblnk.ErrInsufficientFunds)` works.

> **A preview is not a reservation.** Another payment can spend the money between `Preview` and `Record`, and `Record` checks funds again. To reserve money, use a hold (chapter 6).

## Step 3: record it

The same transaction, for real:

{{code 05-transfers/main.go record}}

If Ada can't afford it, the dry run says why, and `Record` refuses with
`humanblnk.ErrInsufficientFunds`. Nothing moves:

{{code 05-transfers/main.go toomuch}}

## Step 4: her history

{{code 05-transfers/main.go history}}

Newest first. `List` shows each posting of a split as its own entry (`…-1`,
`…-2`). Blnk can't combine "from or to" with other conditions, so a full
history is two queries: one with `From`, one with `To`. `EntryQuery` also
filters by `Status`, `Since` and `Until`.

## The whole example

{{run 05-transfers}}

## Good to know

- **A split that fails partway.** Blnk records a split one posting at a time.
  If a later posting fails, the earlier ones have already moved, and Blnk
  refunds them from its background worker a moment later (about 200 ms).
  Until then they show in the accounts. `Record` still returns the error.
  Keep Blnk's worker running and monitored.
- **Retrying that split** with the same `Ref` returns `ErrReferenceUsed`, not
  `ErrDuplicate`: nothing stands under that reference, so record it again
  under a new one (chapter 12).
