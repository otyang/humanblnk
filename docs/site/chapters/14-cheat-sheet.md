# Cheat sheet

Everything on one page. Import `github.com/otyang/humanblnk`; every type
below is `humanblnk.X`. Build one `*humanblnk.Client` at start-up with
`humanblnk.New(client, humanblnk.WithAccountLedger(id), humanblnk.WithCustomerLedgers(ids...))`
(chapter 1).

## Services

| Service | Methods |
| --- | --- |
| `hb.Identities` | `Create(ctx, meta) (string, error)`, `Get(ctx, id)`, `UpdateMeta(ctx, id, meta)`, `FindByCustomer(ctx, customerID)` |
| `hb.Accounts` | `OpenForCustomer(ctx, CustomerAccount)`, `CreateInternal(ctx, NewAccount)`, `Get(ctx, id)`, `System(ctx, humanblnk.FeeIncome, usd)`, `List(ctx, AccountQuery) ([]Account, total, error)` |
| `hb.Transactions` | `Record(ctx, Transaction)`, `RecordAll(ctx, []Transaction)`, `Preview(ctx, Transaction)`, `Reverse(ctx, Reversal)`, `Get(ctx, id)`, `GetByReference(ctx, ref)`, `List(ctx, EntryQuery) ([]Entry, total, error)` |
| `hb.Holds` | `Place(ctx, Transaction, expires) (HoldID, error)`, `Capture(ctx, HoldID, amount)`, `Cancel(ctx, HoldID)` |
| `hb.Watches` | `Create(ctx, Watch)`, `Get(ctx, WatchID)`, `List(ctx, account)`, `Change(ctx, WatchID, Watch)`, `Delete(ctx, WatchID)` |
| `hb.Reconciliation` | `AddRule`, `ChangeRule`, `RemoveRule`, `Upload(ctx, source, filename, r)`, `Run(ctx, StatementID, Run)`, `RunLines(ctx, []StatementLine, Run)`, `Result(ctx, RunID)` |
| `hb.Ledgers` | `Create(ctx, name, meta)`, `Get(ctx, id)`, `List(ctx, limit, offset)`, `Rename(ctx, id, name)` |
| package function | `humanblnk.VerifyWebhook(secret, r, tolerance) (Event, error)` |

## Naming accounts

| Helper | Gives | For |
| --- | --- | --- |
| `humanblnk.AccountRef("bln_…")` | that account | a customer or internal account, by id |
| `humanblnk.World` | `@World` | money outside your books: deposits come from it, payouts go to it |
| `humanblnk.FeeIncome` | `@FeeIncome` | fees you earned |
| `humanblnk.Adjustments` | `@Adjustments` | staff corrections |
| `humanblnk.ClearingAccount("paystack")` | `@PaystackClearing` | money a provider owes you, or you owe it |
| `humanblnk.SystemAccount("FXDesk")` | `@FXDesk` | any other system account; keep it in a variable |

## Building requests

| Helper or type | |
| --- | --- |
| `humanblnk.Reference("payout", "po_123")` | `"payout:po_123"`: build it from your own ids |
| `humanblnk.Single(account, amount)` | one posting |
| `humanblnk.Many(humanblnk.Posting{…}, …)` | several postings, for one side of a split |
| `Transaction` | `Ref`, `From`, `To`, `Description`, `Meta` |
| `Reversal` | `Ref`, `Entry` (an entry's id, or a captured hold's), `Amount`, `Reason` |
| `CustomerAccount` | `Owner`, `Currency`, `Ledger` |
| `NewAccount` | `Ledger`, `Owner`, `Currency`, `Meta` |
| `Watch` | `Account`, `Field`, `Op`, `Threshold`, `Description` |

## What comes back

| Type | Fields |
| --- | --- |
| `Account` | `ID`, `Ledger`, `Owner`, `Currency`, `Total`, `Available`, `OutgoingHeld`, `IncomingHeld`, `Meta`, `CreatedAt` |
| `Entry` | `ID`, `Ref`, `Status`, `Amount`, `From`, `To`, `Description`, `Meta`, `CreatedAt` |
| `Preview` | `WouldApply`, `Rejection`, `Changes []AccountChange`, `Notes` |
| `Identity` | `ID`, `Meta`, `CreatedAt` |
| `Ledger` | `ID`, `Name`, `Meta`, `CreatedAt` |
| `Watch` | what you sent, plus `ID`, `CreatedAt` |
| `MatchRule` | what you sent, plus `ID`, `CreatedAt` |
| `RunResult` | `ID`, `Statement`, `Status`, `Matched`, `Unmatched`, `StartedAt`, `CompletedAt` |
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

## Testing

Import `github.com/otyang/humanblnk/humanblnktest` (chapter 13).

| Call | |
| --- | --- |
| `hb := humanblnktest.New(t, opts...)` | `*Books`, embedding a `*humanblnk.Client` (`hb.Client`) on a fresh in-memory Blnk, closed when `t` ends |
| `hb.Customer(t, "USD")` | an owner and their wallet: its `AccountRef` |
| `hb.Fund(t, account, "100.00")` | a deposit from `@World` |
| `hb.Available(t, account)` | what it can spend now, such as `"70.00"` |
| `humanblnktest.Money(t, "30.00", "USD")` | an amount, failing the test if it can't parse |
