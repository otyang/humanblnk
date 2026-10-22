# Setting up

Blnk groups accounts into **ledgers**. HumanBlnk needs only a few, made **once**:

| Ledger | Holds |
| --- | --- |
| Customer Wallets | every customer's spending accounts, in every currency |
| Savings (optional) | customers' savings accounts |
| Operations (optional) | HumanBlnk's own accounts that aren't system accounts, such as an escrow pool |

Add a ledger only for a different kind of money. Never make one per customer
or per currency. You never create the General Ledger or the system accounts
(`@World`, `@FeeIncome`, clearing accounts): Blnk makes them the first time a
transaction uses them.

## Step 1: create the ledgers, once

Run this from a setup script or migration, **not** when your service starts.
`Ledgers.Create` isn't safe to repeat: each call makes another ledger. If
you're not sure the setup already ran, look first with
`Ledgers.List(ctx, 10, 0)`.

{{code 01-setup/main.go once}}

## Step 2: build the Client at start-up

Every time your service starts, build one `*humanblnk.Client` from the ids in
your config, and share it. Everything hangs off it:

{{code 01-setup/main.go startup}}

| Option | Why |
| --- | --- |
| `WithAccountLedger(id)` | The ledger `Accounts.OpenForCustomer` uses when you don't name one. Don't change it once customers have accounts: their wallets live in the old one. |
| `WithCustomerLedgers(ids...)` | Other ledgers that hold only customer accounts. `CreateInternal` refuses them, so a customer account can never be made without an owner. |

Connect with `blnk.New(url)`, adding `blnk.WithAPIKey(key)` when Blnk runs in
secure mode.

## The whole example

{{run 01-setup}}

The other examples skip these steps with `demo.Setup()`
([`docs/examples/demo`](https://github.com/otyang/humanblnk/blob/main/docs/examples/demo/demo.go)),
which makes a fresh wallets ledger per run, and `demo.Customer`, which
onboards a customer as chapter 2 shows. A real service doesn't need them.
