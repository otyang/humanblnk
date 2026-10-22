# Build money features with HumanBlnk

A hands-on guide to HumanBlnk, a Go backend for a bank or wallet. Each
chapter takes one real task (a customer signing up, a deposit arriving, a
refund) and walks through it with a Go program.

Every line of Go on this site is taken from the example programs in
[`docs/examples`](https://github.com/otyang/humanblnk/tree/main/docs/examples),
which are compiled with the rest of the project. If the API changes, an
example that no longer fits stops compiling, so what you read here always
matches the source. The **Output** tabs show what each example printed
against a real Blnk; your ids will differ.

## What you need

- Go 1.27.1 or later.
- A [Blnk Core](https://docs.blnkfinance.com) 0.15.6 you can reach. The
  examples use `$BLNK_URL`, or `http://localhost:5001` if it's unset.

Every example creates its own ledger and customers, so you can run them in
any order, as often as you like:

```sh
BLNK_URL=https://blnk.example.com go run ./docs/examples/05-transfers
```

## The ideas in one minute

- **`money.Money`** is an exact amount in one currency. Never a float.
- **An account** holds money in one currency. A customer's account always has
  an **owner**: a Blnk identity that holds no personal data.
- **A transaction** moves money `From` some postings `To` others, in one
  atomic write. Every transaction carries a **reference** built from your own
  ids, so a retry can never apply it twice.
- **System accounts** are HumanBlnk's own: `@World` (outside HumanBlnk), `@FeeIncome`,
  and a clearing account per payment provider. They may go below zero;
  customer accounts never do.
- **A hold** reserves money now and settles it later.
- **A reversal** moves money back the way an entry moved it.

## Chapters

**Foundations:** [1. Setting up](01-setup.md) ·
[2. Customers and accounts](02-customers-and-accounts.md) ·
[3. Money](03-money.md)

**Moving money:** [4. Deposits](04-deposits.md) ·
[5. Transfers and fees](05-transfers-and-fees.md) ·
[6. Payouts and holds](06-payouts-and-holds.md) ·
[7. Building a service: airtime](07-building-a-service-airtime.md) ·
[8. Refunds and reversals](08-refunds-and-reversals.md) ·
[9. Currency exchange](09-currency-exchange.md)

**Running it:** [10. Watches and webhooks](10-watches-and-webhooks.md) ·
[11. Reconciliation](11-reconciliation.md) ·
[12. Errors and retries](12-errors-and-retries.md) ·
[13. Cheat sheet](13-cheat-sheet.md)
