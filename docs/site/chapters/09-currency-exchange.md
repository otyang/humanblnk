# Currency exchange

**Scenario:** Ada sells $100 at ₦1,550.25 to the dollar. Her USD wallet goes
down by $100 and her NGN wallet up by ₦155,025, together or not at all.

One transaction can't do this: a transaction is in one currency. So it's
**two** transactions, recorded atomically with `RecordAll`.

## HumanBlnk's FX desk

The other side of each half is HumanBlnk's own account for currency it buys and
sells. Make it a system account, kept in a variable so a typo can't create
another one. As a system account it may go below zero, so it never blocks an
exchange.

{{code 09-exchange/main.go desk}}

## The exchange

The naira amount is a credit to Ada, so it rounds down. The rate goes in
`Meta` on both halves, for statements and audits.

{{code 09-exchange/main.go exchange}}

`RecordAll` rules:

- Every transaction applies, or none does.
- They may be in different currencies.
- Each has **one posting on each side** (no splits).
- The entries come back in the order you passed.

## Retries and failures

A retry after a timeout moves nothing twice:

{{code 09-exchange/main.go retry}}

| Result | Means |
| --- | --- |
| `ErrDuplicate`, with the entries | the whole exchange already happened |
| `ErrReferenceUsed` | a reference holds something that didn't apply: record it again under new ones |
| `ErrInsufficientFunds` | nothing applied |
| `ErrReadBack` | it **applied**, but reading the entries back failed. Don't record it again; look it up with `GetByReference`. |

## The whole example

{{run 09-exchange}}

If a later item fails after an earlier one applied, Blnk refunds the earlier
one from its background worker a moment later, just as for a split
(chapter 5).
