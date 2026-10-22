# ledger

HumanBlnk's books, kept in [Blnk Core](https://docs.blnkfinance.com) 0.15.6. It
speaks [`money.Money`](../money/README.md) and its own errors; nothing else in
HumanBlnk moves money through `clients/blnk`.

```go
books := ledger.New(blnk.New(blnkURL, blnk.WithAPIKey(key)), ledger.WithAccountLedger(walletsBookID))

owner, _ := books.Identities.Create(ctx, ledger.Meta{"customer_id": customerID}) // once per customer; keep it
wallet, _ := books.Accounts.OpenForCustomer(ctx, ledger.CustomerAccount{Owner: owner, Currency: ngn}) // safe to repeat

// A deposit: Paystack owes HumanBlnk until it settles.
_, err := books.Transactions.Record(ctx, ledger.Transaction{
	Ref:         ledger.Reference("deposit", "paystack", ev.Reference), // the same on every retry
	From:        ledger.Single(ledger.ClearingAccount("paystack"), amount),
	To:          ledger.Single(ledger.AccountRef(wallet.ID), amount),
	Description: "Bank transfer",
})
if errors.Is(err, ledger.ErrDuplicate) { /* already recorded: a retried webhook */ }
// ErrReferenceUsed: the reference holds something that didn't apply (a rolled-back split); use a new one

// A transfer with a fee: one atomic split.
books.Transactions.Record(ctx, ledger.Transaction{
	Ref:  ledger.Reference("transfer", id),
	From: ledger.Single(ada, total),
	To:   ledger.Many(ledger.Posting{Account: bola, Amount: amount}, ledger.Posting{Account: ledger.FeeIncome, Amount: fee}),
	Description: "Transfer to Bola",
})

// A payout: reserve, then settle or give back.
hold, _ := books.Holds.Place(ctx, payout, time.Now().Add(72*time.Hour))
books.Holds.Capture(ctx, hold, money.Money{}) // or books.Holds.Cancel(ctx, hold)
```

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

Plus `ledger.VerifyWebhook` for Blnk's webhooks, and the vocabulary:
`Transaction`, `Posting`, `Single`, `Many`, `Reference`, `World`, `FeeIncome`,
`Adjustments`, `SystemAccount`, `ClearingAccount`.

## The rules

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
| `ledger` | `Ledger`, `New`, and every public name re-exported (`aliases.go`) |
| `ledger/types` | The public types, errors and helpers, defined once |
| `ledger/accounts`, `transactions`, `holds`, `watches`, `reconcile`, `books`, `events` | One package per service |
| `ledger/internal/core` | Shared and private: the Blnk client, conversion, the currency check, error mapping |

A new public type goes in `types` and gets an alias in `aliases.go`.

## Tests

```sh
go test ./ledger/...                                    # against a fake Blnk (clients/blnk/blnkfake)

docker compose -f deploy/docker-compose.yml up -d       # Blnk 0.15.6 + Postgres
BLNK_URL=http://localhost:5001 go test -tags integration ./ledger/
```

The integration tests check what the unit tests can only assume: how Blnk
stores splits, that funds errors are immediate, that a partial capture releases
the rest, and that concurrent reversals never exceed the entry.
