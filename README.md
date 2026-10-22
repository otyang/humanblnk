# humanblnk

**humanblnk is a Go library for keeping a wallet's or bank's books in
[Blnk Core](https://docs.blnkfinance.com).** You work in bookkeeping terms
(customers, accounts, transactions, holds, reversals) and exact money, and
humanblnk makes Blnk follow the rules a money ledger needs, which Blnk alone
leaves to you.

It's for developers building fintech products (wallets, neobanks,
marketplaces, payouts, remittance) who want Blnk's double-entry ledger without
re-learning its sharp edges in production.

```sh
go get github.com/otyang/humanblnk   # Go 1.27.1+
```

## Why not call Blnk directly?

Blnk is a solid double-entry engine, but it's deliberately low-level. These
are the gaps humanblnk closes:

| With Blnk alone | With humanblnk |
| --- | --- |
| Amounts are integers you scale yourself; one write at the wrong precision mixes units silently | Amounts are `money.Money`, exact decimals, sent in the currency's smallest unit at its own precision; entries read back are checked against it |
| A USD 10.00 transaction applied to an NGN (Nigerian naira) account lands as NGN 10.00 | A currency mismatch is refused with `money.ErrCurrencyMismatch` |
| A retried request hits a "reference already used" error that doesn't say whether your write applied | You build each reference from your own ids with `humanblnk.Reference`. A retried `Record` gets `ErrDuplicate` (it applied) or `ErrReferenceUsed` (something else holds that reference); a retried hold or reversal returns the one already made |
| Writes are queued by default: insufficient funds turns up later as a `REJECTED` status | Writes are synchronous: you get `ErrInsufficientFunds` from the call itself |
| A split (one transaction paying several accounts, such as an amount plus a fee) that fails partway reads as applied, though Blnk will refund its earlier parts | The entry reads as `StatusRejected` at once and its reference can't be reused. The balances still show the earlier parts until Blnk's worker refunds them (about 200 ms), so keep the worker running |
| Overdraft is a flag on each write: any write that sets it can take its source below zero | Overdraft is allowed only when every source is a system account (such as `@World`, money outside your books, or a provider's clearing account) |
| Customer money and your company's own can share a ledger | Customer accounts need an owner and live in the customer ledgers you name; your own accounts can't be opened there |
| Blnk's refund undoes a transaction once, in full; partial refunds are yours to track | Reversals, full or partial, are numbered per entry and never undo more than it moved, even when they run concurrently |

## What it isn't

- **Not a payment provider integration.** You talk to Stripe, Paystack or your
  bank yourself; humanblnk moves the money in your books to match.
- **Not a server.** It's a library inside your Go service, with no HTTP API of
  its own.
- **Not a store for personal data.** Customers are Blnk identities holding only
  your ids (such as `customer_id`). Names, documents and KYC stay in your own
  systems.
- **Not a replacement for Blnk.** You run Blnk Core 0.15.6 with its worker,
  Postgres and Redis. To start all four locally:

  ```sh
  docker compose -f deploy/docker-compose.yml up -d   # Blnk on http://localhost:5001
  ```

## A first look

```go
import (
	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

usd := currency.MustGet(currency.USD)
hb := humanblnk.New(blnk.New("http://localhost:5001"), // add blnk.WithAPIKey(key) if Blnk runs in secure mode
	humanblnk.WithAccountLedger(walletsLedgerID), // made once by a setup script (tutorial chapter 1)
)

// A customer and their wallet. Identities.Create is not safe to repeat:
// in a sign-up handler, try hb.Identities.FindByCustomer first.
owner, err := hb.Identities.Create(ctx, humanblnk.Meta{"customer_id": "cus_123"})
wallet, err := hb.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: usd}) // safe to repeat

// A card top-up. Stripe owes you the money until it settles, so it comes
// from Stripe's clearing account.
amount := money.MustParse("50.00", usd)
_, err = hb.Transactions.Record(ctx, humanblnk.Transaction{
	Ref:         humanblnk.Reference("deposit", "stripe", paymentIntentID), // the same on every retry
	From:        humanblnk.Single(humanblnk.ClearingAccount("stripe"), amount),
	To:          humanblnk.Single(humanblnk.AccountRef(wallet.ID), amount),
	Description: "Card top-up",
})
if errors.Is(err, humanblnk.ErrDuplicate) {
	err = nil // a retried webhook: already recorded
}

// A payout: reserve the money now, then settle or release it when the bank answers.
hold, err := hb.Holds.Place(ctx, humanblnk.Transaction{
	Ref:         humanblnk.Reference("payout", payoutID),
	From:        humanblnk.Single(humanblnk.AccountRef(wallet.ID), money.MustParse("20.00", usd)),
	To:          humanblnk.Single(humanblnk.World, money.MustParse("20.00", usd)),
	Description: "Payout to bank",
}, time.Now().Add(72*time.Hour))

acct, err := hb.Accounts.Get(ctx, wallet.ID)
fmt.Println(acct.Available) // "30.00": what the customer can spend now

if bankAccepted {
	_, err = hb.Holds.Capture(ctx, hold, money.Money{}) // all of it
} else {
	err = hb.Holds.Cancel(ctx, hold)
}
```

## Learn more

- **[Tutorial](https://otyang.github.io/humanblnk/docs/)**: thirteen chapters.
  Eleven walk through a real task (sign-up, deposits, transfers with fees,
  payouts, refunds, currency exchange, webhooks, reconciliation) with a
  program you can run against your own Blnk; the last two cover errors and
  retries, and a cheat sheet.
- **[Reference](https://pkg.go.dev/github.com/otyang/humanblnk)**: every
  service and method, the rules humanblnk enforces, what's safe to retry, its
  errors and its limits. Also in [doc.go](doc.go).
- **[money](money/README.md)**: exact amounts and currencies, usable on their
  own.

## Packages

| Package | What it is |
| --- | --- |
| `humanblnk` | Your books: `Client`, its services, and the vocabulary |
| [`money`](money/README.md) | An exact amount in one currency: parse, add, round, convert to the smallest unit |
| `blnk` | A Blnk Core HTTP client. You use it to build the client (`blnk.New`, `blnk.WithAPIKey`); only humanblnk calls Blnk through it |

Your own features (airtime top-ups, payouts, transfers) go in their own
packages and import `humanblnk`; humanblnk never imports them.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the layout, the tests and how the
tutorial is built.

## License

[MIT](LICENSE)
