# Cheat sheet

Everything on one page. Import `github.com/otyang/humanblnk`; every type
below is `humanblnk.X`. Build one `*humanblnk.Ledger` at start-up with
`humanblnk.New(client, humanblnk.WithAccountLedger(id), humanblnk.WithCustomerLedgers(ids...))`
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
| package function | `humanblnk.VerifyWebhook(secret, r, tolerance) (Event, error)` |

## Naming accounts

| Helper | Gives | For |
| --- | --- | --- |
| `humanblnk.AccountRef("bln_…")` | that account | a customer or internal account, by id |
| `humanblnk.World` | `@World` | outside HumanBlnk: deposits come from it, payouts go to it |
| `humanblnk.FeeIncome` | `@FeeIncome` | fees HumanBlnk earned |
| `humanblnk.Adjustments` | `@Adjustments` | staff corrections |
| `humanblnk.ClearingAccount("paystack")` | `@PaystackClearing` | money a provider owes HumanBlnk, or HumanBlnk owes it |
| `humanblnk.SystemAccount("FXDesk")` | `@FXDesk` | any other system account; keep it in a variable |

## Building requests

| Helper or type | |
| --- | --- |
| `humanblnk.Reference("payout", "po_123")` | `"payout:po_123"`: build it from your own ids |
| `humanblnk.Single(account, amount)` | one posting |
| `humanblnk.Many(humanblnk.Posting{…}, …)` | several postings, for one side of a split |
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
an id you stored, as in `humanblnk.HoldID(s)`. Owner (`idt_…`) and ledger
(`ldg_…`) ids are plain strings.

## Money

| Call | |
| --- | --- |
| `currency.Get(code)`, `currency.Lookup(code)`, `currency.MustGet(code)` | a currency: typed, stored, constant |
| `money.Parse(s, c)` | an exact amount |
| `m.Add`, `m.Sub`, `m.Cmp`, `m.Equal` | one currency only |
| `m.Percent(p, mode)`, `money.Round(d, c, mode)` | with `money.Up`, `money.Down` or `money.HalfEven` |
| `m.Minor()`, `money.FromMinor(n, c)` | to and from the smallest unit |
