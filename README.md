# HumanBlnk

A Go Domain based Wrapper Implementation on the Blnk Ledger. Money is kept exactly (never a float), and
every account and transaction lives in [Blnk Core](https://docs.blnkfinance.com)
0.15.6, a double-entry ledger.

| Package | What it does |
| --- | --- |
| [`money`](money/README.md) | An exact amount in one currency: parse, add, round, convert to the smallest unit |
| `humanblnk` (this directory) | HumanBlnk's books: customer accounts, transactions, holds, reversals, watches, reconciliation. Speaks `money.Money` and its own errors |
| `blnk` | The Blnk Core HTTP client. Only `humanblnk` uses it |
| `internal/...` | The services behind `humanblnk.Ledger`; not importable from outside this module |

Business features (airtime, payouts, transfers) go in their own packages and
import `humanblnk`; `humanblnk` never imports them.

## Run it locally

You need Go 1.27+ and Docker.

```sh
docker compose -f deploy/docker-compose.yml up -d   # Blnk 0.15.6, its worker, Postgres, Redis
go test ./...                                       # unit tests, against a fake Blnk
BLNK_URL=http://localhost:5001 go test -tags integration .   # against the real one
```

## Example

```go
import (
	"github.com/otyang/humanblnk"
	"github.com/otyang/humanblnk/blnk"
	"github.com/otyang/humanblnk/money"
	"github.com/otyang/humanblnk/money/currency"
)

ngn := currency.MustGet(currency.NGN)
books := humanblnk.New(blnk.New("http://localhost:5001"),
	humanblnk.WithAccountLedger(walletsLedgerID), // made once at setup: books.Books.Create(ctx, "Customer Wallets", nil)
)

// A customer and their wallet. OpenForCustomer is safe to call again.
owner, _ := books.Identities.Create(ctx, humanblnk.Meta{"customer_id": "cus_123"})
wallet, _ := books.Accounts.OpenForCustomer(ctx, humanblnk.CustomerAccount{Owner: owner, Currency: ngn})

// A deposit arrives through Paystack.
amount := money.MustParse("5000", ngn)
_, err := books.Transactions.Record(ctx, humanblnk.Transaction{
	Ref:         humanblnk.Reference("deposit", "ps_ref_9"), // from your own ids, the same on every retry
	From:        humanblnk.Single(humanblnk.ClearingAccount("paystack"), amount),
	To:          humanblnk.Single(humanblnk.AccountRef(wallet.ID), amount),
	Description: "Bank transfer",
})
if errors.Is(err, humanblnk.ErrDuplicate) {
	// already recorded: a retried webhook
}

// A payout: reserve the money, then settle or give it back.
hold, err := books.Holds.Place(ctx, humanblnk.Transaction{
	Ref:         humanblnk.Reference("payout", "po_7"),
	From:        humanblnk.Single(humanblnk.AccountRef(wallet.ID), money.MustParse("2000", ngn)),
	To:          humanblnk.Single(humanblnk.World, money.MustParse("2000", ngn)),
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

- **Every write has a reference** built from your own ids (`humanblnk.Reference`).
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
[tutorial](https://otyang.github.io/humanblnk/docs/). Below: every
service, all the rules, and known limits.

## What it does

| `books.` | Methods | For |
| --- | --- | --- |
| `Identities` | `Create`, `Get`, `UpdateMeta`, `FindByCustomer` | Owners for customer accounts, holding no personal data (only references such as `customer_id`) |
| `Accounts` | `OpenForCustomer`, `CreateInternal`, `Get`, `List` | Balances: money in one currency. Customer accounts always have an owner, one per ledger and currency; HumanBlnk's own accounts live outside the customer ledgers |
| `Transactions` | `Record`, `RecordAll`, `Reverse`, `Preview`, `Get`, `GetByReference`, `List` | Moving money now; undoing it; reading what was recorded |
| `Holds` | `Place`, `Capture`, `Cancel` | Reserving money now, settling it later |
| `Watches` | `Create`, `Get`, `List`, `Change`, `Delete` | Thresholds that fire the `balance.monitor` webhook |
| `Reconciliation` | `Upload`, `AddRule`, `ChangeRule`, `RemoveRule`, `Run`, `RunLines`, `Result` | Matching provider and bank statements against entries |
| `Books` | `Create`, `Get`, `List`, `Rename` | Blnk ledgers: named groups of accounts |

Each service has a type of its own (`IdentityService`, `AccountService`,
`TransactionService`, `HoldService`, `WatchService`, `ReconciliationService`,
`BookService`), so code that needs only one can take just that.

Plus `humanblnk.VerifyWebhook` for Blnk's webhooks, and the vocabulary:
`Transaction`, `Posting`, `Single`, `Many`, `Reference`, `World`, `FeeIncome`,
`Adjustments`, `SystemAccount`, `ClearingAccount`.

## All the rules

| Rule | Why |
| --- | --- |
| Amounts reach Blnk only in their currency's smallest unit, with the currency's own precision | Blnk balances are bare integers; one transaction at another precision would mix units silently |
| A transaction's currency must match every account it touches | Blnk doesn't check: it would apply USD 10.00 to an NGN account as ₦10.00 |
| Every write carries a reference, the same on every retry | Blnk refuses a reference twice, so a retry can't apply twice. A refused reference is looked up: `ErrDuplicate` means this write happened; `ErrReferenceUsed` means it holds something that didn't apply (Blnk keeps a rolled-back split's references), so only a new reference will do |
| Reversals, full or partial, are numbered per entry | They take turns, even concurrently, and never undo more than the entry moved. A reversal takes from where the entry put the money: a customer who spent it can't be pushed below zero |
| Writes are synchronous (Blnk's `skip_queue`) | Otherwise insufficient funds would surface later as a `REJECTED` status, not as an error |
| Only system accounts (`@World`, clearing accounts) may go below zero | They do by design; a customer account never may |
| `Place`, `Capture` and `Cancel` are safe to repeat with the same arguments; wait a few seconds before repeating a timed-out partial `Capture` | Blnk's commit takes no reference, so `Capture` looks for an earlier capture first; a partial one still running in Blnk isn't visible yet |
| `Cancel` only releases holds; `Transactions.Reverse` is the only way to undo money that moved | Corrections stay in the books as entries of their own |
| Partial reversals can't add up to more than the entry, even concurrently | Each claims a numbered reference; Blnk's unique index makes them take turns |

Errors: `ErrInsufficientFunds`, `ErrDuplicate`, `ErrReferenceUsed`, `ErrNotFound`, `ErrUnbalanced`,
`ErrAlreadyReversed`, `ErrHoldSettled`, `ErrInvalid`, `ErrReadBack`, and
`money.ErrCurrencyMismatch`. Each wraps Blnk's error, for logs.

## Limits

- A split (several postings on one side) is reversed and captured only in full.
- Blnk can't filter "from or to" together with other conditions, so an account's
  full history is two `Transactions.List` calls, one with `From`, one with `To`.
- `RecordAll` reads its entries back after the batch; if that fails the batch
  still applied, and the error is `ErrReadBack`.

## Layout

| Package | Contents |
| --- | --- |
| `humanblnk` | `Ledger`, `New`, and every public name re-exported (`aliases.go`) |
| `internal/types` | The public types, errors and helpers, defined once |
| `internal/accounts`, `transactions`, `holds`, `watches`, `reconcile`, `books`, `identities`, `events` | One package per service |
| `internal/core` | Shared and private: the Blnk client, conversion, the currency check, error mapping |

A new public type goes in `internal/types` and gets an alias in `aliases.go`;
a new service gets a field on `Ledger` and a `…Service` alias.

## Tests

```sh
go test ./...                                           # against a fake Blnk (blnk/blnkfake)

docker compose -f deploy/docker-compose.yml up -d       # Blnk 0.15.6 + Postgres
BLNK_URL=http://localhost:5001 go test -tags integration .
```

The integration tests check what the unit tests can only assume: how Blnk
stores splits, that funds errors are immediate, that a partial capture releases
the rest, and that concurrent reversals never exceed the entry.

## License

[MIT](LICENSE)
