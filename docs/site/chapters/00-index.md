# Build money features with humanblnk

**humanblnk is a Go library for keeping a wallet's or bank's books in
[Blnk Core](https://docs.blnkfinance.com).** You work in bookkeeping terms
(customers, accounts, transactions, holds, reversals) and exact money, and
humanblnk makes Blnk follow the rules a money ledger needs, which Blnk alone
leaves to you:

- amounts are exact and always sent at the currency's own precision;
- a currency mismatch is refused, not applied as the wrong money;
- a retried money movement can never apply twice;
- insufficient funds is an error from the call, not a status found later;
- only system accounts go below zero, never a customer's;
- reversals, full or partial, never undo more than the entry moved.

It's a library inside your Go service, not a server, and it moves money only
in Blnk: you talk to Stripe, Paystack or your bank yourself, and humanblnk
moves the money in your books to match. Customer data stays in your systems;
Blnk holds only your ids.

This guide takes one real task per chapter (a customer signing up, a deposit
arriving, a refund) and walks through it with a Go program you can run.

Every line of Go on this site is taken from the example programs in
[`docs/examples`](https://github.com/otyang/humanblnk/tree/main/docs/examples),
which are compiled with the rest of the project. If the API changes, an
example that no longer fits stops compiling, so what you read here always
matches the source. The **Output** tabs show what each example printed
against a real Blnk; your ids will differ.

## What you need

- Go 1.24 or later.
- A [Blnk Core](https://docs.blnkfinance.com) 0.15.6 you can reach. The
  examples use `$BLNK_URL`, or `http://localhost:5001` if it's unset. To run
  one locally, with its worker, Postgres and Redis:

  ```sh
  docker compose -f deploy/docker-compose.yml up -d
  ```

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
- **System accounts** are your company's own: `@World` (money outside your books), `@FeeIncome`,
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
[13. Testing your service](13-testing.md) ·
[14. Cheat sheet](14-cheat-sheet.md)
