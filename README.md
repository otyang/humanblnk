# HumanBlnk

A Go Domain based Wrapper Implementation on the Blnk Ledger. Money is kept exactly (never a float), and
every account and transaction lives in [Blnk Core](https://docs.blnkfinance.com)
0.15.6, a double-entry ledger.

| Package | What it does |
| --- | --- |
| [`money`](money/README.md) | An exact amount in one currency: parse, add, round, convert to the smallest unit |
| [`ledger`](ledger/README.md) | HumanBlnk's books: customer accounts, transactions, holds, reversals, watches, reconciliation |
| `blnk` | The Blnk Core HTTP client. Only `ledger` uses it |

Business features (airtime, payouts, transfers) go in their own packages and
import `ledger`; `ledger` never imports them.

## Run it locally

You need Go 1.27+ and Docker.

```sh
docker compose -f deploy/docker-compose.yml up -d   # Blnk 0.15.6, its worker, Postgres, Redis
go test ./...                                       # unit tests, against a fake Blnk
BLNK_URL=http://localhost:5001 go test -tags integration ./ledger/   # against the real one
```

## Example

```go
import (
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/ledger"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

ngn := currency.MustGet(currency.NGN)
books := ledger.New(blnk.New("http://localhost:5001"),
	ledger.WithAccountLedger(walletsLedgerID), // made once at setup: books.Books.Create(ctx, "Customer Wallets", nil)
)

// A customer and their wallet. OpenForCustomer is safe to call again.
owner, _ := books.Identities.Create(ctx, ledger.Meta{"customer_id": "cus_123"})
wallet, _ := books.Accounts.OpenForCustomer(ctx, ledger.CustomerAccount{Owner: owner, Currency: ngn})

// A deposit arrives through Paystack.
amount := money.MustParse("5000", ngn)
_, err := books.Transactions.Record(ctx, ledger.Transaction{
	Ref:         ledger.Reference("deposit", "ps_ref_9"), // from your own ids, the same on every retry
	From:        ledger.Single(ledger.ClearingAccount("paystack"), amount),
	To:          ledger.Single(ledger.AccountRef(wallet.ID), amount),
	Description: "Bank transfer",
})
if errors.Is(err, ledger.ErrDuplicate) {
	// already recorded: a retried webhook
}

// A payout: reserve the money, then settle or give it back.
hold, err := books.Holds.Place(ctx, ledger.Transaction{
	Ref:         ledger.Reference("payout", "po_7"),
	From:        ledger.Single(ledger.AccountRef(wallet.ID), money.MustParse("2000", ngn)),
	To:          ledger.Single(ledger.World, money.MustParse("2000", ngn)),
	Description: "Payout to bank",
}, time.Time{})
if bankAccepted {
	books.Holds.Capture(ctx, hold, money.Money{}) // all of it
} else {
	books.Holds.Cancel(ctx, hold)
}

acct, _ := books.Accounts.Get(ctx, wallet.ID)
fmt.Println(acct.Available) // "3000.00": what the customer can spend now
```

## Rules worth knowing

- **Every write has a reference** built from your own ids (`ledger.Reference`).
  Retrying with it can't apply twice: `ErrDuplicate` means it already happened.
  `ErrReferenceUsed` means the reference holds something that didn't apply,
  so record it again under a new one.
- **Check `Available`, not `Total`,** before letting a customer spend.
- **Customer accounts** are opened with `Accounts.OpenForCustomer` (an owner is
  required). HumanBlnk's own accounts use `Accounts.CreateInternal`.
- **Undo money that moved** with `Transactions.Reverse`. `Holds.Cancel` only
  releases a hold.
- **Keep personal data out of Blnk.** Identities hold references such as your
  customer id, nothing more.

For a step-by-step guide with runnable real-life examples (deposits,
transfers, payouts, refunds, exchange, reconciliation), read the
[tutorial](https://otyang.github.io/humanblnk/docs/). See [ledger/README.md](ledger/README.md) for every
service, and known limits.

## License

[MIT](LICENSE)
