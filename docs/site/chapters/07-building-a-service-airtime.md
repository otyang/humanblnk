# Building a service: airtime

**Scenario:** Ada buys ₦1,000 of airtime from her wallet. The airtime provider
(VTpass, say) can deliver, decline, or not answer in time.

This chapter shows the shape every business feature takes: its own package
that **imports `humanblnk`**, never the other way round. `humanblnk` knows nothing
about airtime; the airtime package decides what to take and when.

## The design: a hold around the provider call

The provider is outside HumanBlnk and can fail or time out, so the money is held
first, not taken outright:

1. **Hold** the amount. If Ada can't afford it, stop before calling the
   provider.
2. **Call the provider.**
3. Delivered: **capture**. Declined: **cancel**. No clear answer: leave it
   held, and settle it when the provider's webhook or a status check answers.

The money goes to the provider's clearing account,
`humanblnk.ClearingAccount("vtpass")`: what HumanBlnk owes the provider, or how much
of a prepaid float is used.

## Step 1: depend on interfaces

The package names the small part of the ledger it uses, so tests can pass a
fake. `hb.Holds` satisfies `Holds`.

{{code 07-airtime/airtime/airtime.go interfaces}}

## Step 2: buy

`Buy` places the hold, calls the provider, then settles. It's safe to call
again with the same `Purchase`: `Place` returns the same hold, and a full
capture can't move money twice.

{{code 07-airtime/airtime/airtime.go buy}}

## Step 3: settle

`Settle` captures or cancels. The provider's webhook handler calls it for a
purchase that was pending.

{{code 07-airtime/airtime/airtime.go settle}}

## Step 4: wire it up

The example plays a provider that delivers, declines, or times out,
depending on the phone number:

{{code 07-airtime/main.go provider}}

{{code 07-airtime/main.go wire}}

## The whole service

{{file 07-airtime/airtime/airtime.go}}

## The whole example

{{run 07-airtime}}

## What a real service adds

- **Its own table** (purchase id, wallet, phone, amount, hold id, status), so
  the webhook and a retry job can find pending purchases and call `Settle`.
- **A provider that is safe to retry:** send your purchase id as the
  provider's request id, so a retry never sends airtime twice.
- **No hold expiry:** if Blnk expired the hold while the provider had in fact
  delivered, the airtime would be free.
- **A fee or commission:** make `To` a split with `humanblnk.Many(...)`. Splits
  capture only in full, which is all `Settle` does.
