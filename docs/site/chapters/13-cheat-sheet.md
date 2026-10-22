# Cheat sheet

Everything on one page. Import `github.com/otyang/humanblnk/ledger`; every type
below is `ledger.X`. Build one `*ledger.Ledger` at start-up with
`ledger.New(client, ledger.WithAccountLedger(id), ledger.WithCustomerLedgers(ids...))`
(chapter 1).

## Services

| Service | Methods |
| --- | --- |
| `books.Identities` | `Create(ctx, meta) (string, error)`, `Get(ctx, id)`, `UpdateMeta(ctx, id, meta)`, `FindByCustomer(ctx, customerID)` |
| `books.Accounts` | `OpenForCustomer(ctx, CustomerAccount)`, `CreateInternal(ctx, NewAccount)`, `Get(ctx, id)`, `List(ctx, AccountQuery)` |
| `books.Transactions` | `Record(ctx, Transaction)`, `RecordAll(ctx, []Transaction)`, `Preview(ctx, Transaction)`, `Reverse(ctx, Reversal)`, `Get(ctx, id)`, `GetByReference(ctx, ref)`, `List(ctx, EntryQuery)` |
| `books.Holds` | `Place(ctx, Transaction, expires) (HoldID, error)`, `Capture(ctx, HoldID, amount)`, `Cancel(ctx, HoldID)` |
| `books.Watches` | `Create(ctx, Watch)`, `Get(ctx, WatchID)`, `List(ctx, account)`, `Change(ctx, WatchID, Watch)`, `Delete(ctx, WatchID)` |
| `books.Reconciliation` | `AddRule`, `ChangeRule`, `RemoveRule`, `Upload(ctx, source, filename, r)`, `Run(ctx, StatementID, Run)`, `RunLines(ctx, []StatementLine, Run)`, `Result(ctx, RunID)` |
| `books.Books` | `Create(ctx, name, meta)`, `Get(ctx, id)`, `List(ctx, limit, offset)`, `Rename(ctx, id, name)` |
| package function | `ledger.VerifyWebhook(secret, r, tolerance) (Event, error)` |

## Naming accounts

| Helper | Gives | For |
| --- | --- | --- |
| `ledger.AccountRef("bln_…")` | that account | a customer or internal account, by id |
| `ledger.World` | `@World` | outside HumanBlnk: deposits come from it, payouts go to it |
| `ledger.FeeIncome` | `@FeeIncome` | fees HumanBlnk earned |
| `ledger.Adjustments` | `@Adjustments` | staff corrections |
| `ledger.ClearingAccount("paystack")` | `@PaystackClearing` | money a provider owes HumanBlnk, or HumanBlnk owes it |
| `ledger.SystemAccount("FXDesk")` | `@FXDesk` | any other system account; keep it in a variable |

## Building requests

| Helper or type | |
| --- | --- |
| `ledger.Reference("payout", "po_123")` | `"payout:po_123"`: build it from your own ids |
| `ledger.Single(account, amount)` | one posting |
| `ledger.Many(ledger.Posting{…}, …)` | several postings, for one side of a split |
| `Transaction` | `Ref`, `From`, `To`, `Description`, `Meta` |
| `Reversal` | `Ref`, `Entry`, `Amount`, `Reason` |
| `CustomerAccount` | `Owner`, `Currency`, `Ledger` |
| `NewAccount` | `Ledger`, `Owner`, `Currency`, `Meta` |
| `Watch` | `Account`, `Field`, `Op`, `Threshold`, `Description` |

## What comes back

| Type | Fields |
| --- | --- |
| `Account` | `ID`, `Ledger`, `Owner`, `Currency`, `Total`, `Held`, `Available`, `Meta`, `CreatedAt` |
| `Entry` | `ID`, `Ref`, `Status`, `Amount`, `From`, `To`, `Description`, `Meta`, `CreatedAt` |
| `Preview` | `WouldApply`, `Rejection`, `Changes []AccountChange`, `Notes` |
| `Identity` | `ID`, `Meta`, `CreatedAt` |
| `Event` | `Kind`, `Entry`, `Watch`, `Account`, `Data` |

Entry statuses: `StatusApplied` (money moved), `StatusHeld` (a hold),
`StatusVoid` (a released hold), `StatusRejected` (refused or rolled back),
`StatusQueued`.

## Ids

`HoldID`, `WatchID`, `StatementID`, `RuleID` and `RunID` are strings; convert
an id you stored, as in `ledger.HoldID(s)`. Owner (`idt_…`) and ledger
(`ldg_…`) ids are plain strings.

## Money

| Call | |
| --- | --- |
| `currency.Get(code)`, `currency.Lookup(code)`, `currency.MustGet(code)` | a currency: typed, stored, constant |
| `money.Parse(s, c)` | an exact amount |
| `m.Add`, `m.Sub`, `m.Cmp`, `m.Equal` | one currency only |
| `m.Percent(p, mode)`, `money.Round(d, c, mode)` | with `money.Up`, `money.Down` or `money.HalfEven` |
| `m.Minor()`, `money.FromMinor(n, c)` | to and from the smallest unit |
