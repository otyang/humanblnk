# Currency exchange

**Scenario:** Ada sells $100 at ₦1,550.25 to the dollar. Her USD wallet goes
down by $100 and her NGN wallet up by ₦155,025, together or not at all.

One transaction can't do this: a transaction is in one currency. So it's
**two** transactions, recorded atomically with `RecordAll`.

## Your FX desk

The other side of each half is your company's own account for currency it buys and
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

`RecordAll` holds the batch and captures it at once, as `Record` does a
split (chapter 5), so if a later item fails, the earlier ones never move.
