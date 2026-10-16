title: Reconciliation
group: ops
order: 10
lead: Match a provider's report against the ledger line by line, with Blnk's matching rules, and find the payments that are in one and not the other.
---
## Why reconcile

Every morning Paystack sends Oja a report of yesterday's collections. Each line should match a deposit Oja recorded. A line with no deposit means a customer paid and wasn't credited. A deposit with no line means Oja credited money Paystack never collected. The [trial balance](reports.html) can't catch either, because a wrong entry still balances.

Blnk has a reconciliation engine. You give it the provider's lines and one or more matching rules, and it reports how many lines matched a ledger transaction.

| Situation | Use |
|---|---|
| A report of up to 10,000 lines you have parsed | `ops.StartReconciliation` with `Records` |
| A bigger CSV or JSON export | `ops.StartReconciliation` with `File` and `Filename` |
| Lines match only on reference, currency and exact amount | no rule needed: the default rule does that |
| The provider rounds, or you match on other fields | `ops.CreateMatchingRule`, then pass its id in `RuleIDs` |
| See the result without recording it | `DryRun: true` |

## Reconcile a report: ops.StartReconciliation

@sample inline | Paystack's report for 1 September

- `Source` names where the lines came from, such as `paystack`. It is required.
- Each `ExternalRecord` needs an `ID` (the provider's line id), an `Amount` with a currency, and usually the `Reference` you sent to Blnk. `Description` and `Date` are passed on for rules that use them.
- `StartReconciliation` starts the run and returns its state. A large run is still `started` or `in_progress` when it returns, so poll `ops.GetReconciliation` until `Done()`. `Done()` is true when `Status` is `completed` or `failed`.
- `Matched` and `Unmatched` count the provider's lines. `paystack_T404` has no deposit, so it is the one to investigate.

> note: Blnk reports counts only. It has no endpoint that lists which lines matched. To find the unmatched lines, look each reference up with `wallet.GetTransactionByReference`, or reconcile smaller groups of lines.

### Fields of a run

| Field | Meaning |
|---|---|
| `ID` | the run's id, for `GetReconciliation` |
| `Status` | `started`, `in_progress`, `completed` or `failed` |
| `Matched` | provider lines matched to a ledger transaction |
| `Unmatched` | provider lines with no match |
| `DryRun` | true when the run recorded nothing |
| `StartedAt`, `CompletedAt` | when Blnk started and finished it |

## Matching rules: ops.CreateMatchingRule

Without `RuleIDs`, humanblnk creates one rule the first time it is needed: reference equals, currency equals, amount equals. It reuses that rule for the life of the `Service`, so each new process creates one more copy in Blnk. Write your own when the provider's figures differ slightly from yours.

@sample rule | Flutterwave rounds ₦12,345.67 to ₦12,346

- `CreateMatchingRule` stores the rule in Blnk and returns its id. Create rules once, when you provision, and keep the ids in your configuration.
- A rule matches when every one of its criteria holds. With several `RuleIDs`, a line matches when any rule does.
- `ledger.MatchingRule` and `ledger.MatchingCriterion` are Blnk's own types, re-exported.

| Criterion field | Meaning |
|---|---|
| `Field` | `amount`, `date`, `description`, `reference` or `currency` |
| `Operator` | `equals`, `greater_than`, `less_than` or `contains` |
| `Value`, `Pattern` | a fixed value or pattern, for rules that need one |
| `AllowableDrift` | for `amount`, a fraction (`0.001` is 0.1%, Blnk Core 0.15.0+); for `date`, seconds |

## Try it first: DryRun

@sample dry-run

`DryRun: true` runs the match and returns the counts without recording the result in Blnk. Use it while you tune a rule.

## Movements with a fee

A movement with a fee is stored in Blnk only as its legs, `<ref>-1`, `<ref>-2` and so on. The provider's report has one line under the original reference, for the full amount. No single ledger transaction has that reference or that amount, so a one-to-one match fails.

@sample fee-legs | A ₦20,000 deposit with a ₦300 fee

The leg numbers aren't in request order: don't assume `-1` is the customer's leg.

You have three ways to handle this:

1. Match the line against the legs together, with Blnk's `one_to_many` strategy grouped by `parent_transaction`. Try it against your Blnk version first.
2. Reconcile deposits with a fee separately, against a rule that doesn't use the reference, such as amount and date.
3. Don't charge a fee on deposits. A deposit with `Fee: ledger.NoFee` is one transaction and matches one-to-one.

The first option looks like this:

```go
run, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{
	Source:           "paystack",
	Records:          report,
	Strategy:         "one_to_many",        // one provider line, several ledger entries
	GroupingCriteria: "parent_transaction", // the legs of one movement
})
```

`Strategy` is `one_to_one` by default. `many_to_one` matches several provider lines against one ledger entry. `GroupingCriteria` applies to both grouped strategies.

## A file instead of records

For more than 10,000 lines, upload the provider's export. humanblnk uploads it to Blnk, then starts the run on the upload.

```go
f, err := os.Open("paystack_2026-09-01.csv")
check(err)
defer f.Close()

run, err := ops.StartReconciliation(ctx, ledger.ReconcileInput{
	Source:   "paystack",
	File:     f,
	Filename: "paystack_2026-09-01.csv", // default: "<Source>.csv"
})
check(err)
// poll ops.GetReconciliation(ctx, run.ID) until run.Done()
```

The file is CSV or JSON with the columns `id`, `amount`, `currency`, `reference`, `description` and `date`. `amount` is in major units (`50000.00`, not kobo) and `date` is RFC3339. Set either `Records` or `File`, not both.

## What the in-memory Blnk supports

The samples on this page run against `ledgertest`, the in-memory Blnk. It supports instant reconciliation with inline `Records`, the `one_to_one` strategy, and the criteria `reference`, `currency` and `description` (`contains`) and `amount` (`equals`, with drift). Each run completes at once. It doesn't support `File` uploads or the `one_to_many` and `many_to_one` strategies, so test those against a real Blnk. See [Testing](testing.html).

## What happens in Blnk

No money moves. Reconciliation only reads the ledger.

| Call | Blnk request |
|---|---|
| `CreateMatchingRule` | `POST /reconciliation/matching-rules` |
| `StartReconciliation` with `Records` | `POST /reconciliation/start-instant` with the lines inline (Core 0.10.0+) |
| `StartReconciliation` with `File` | `POST /reconciliation/upload` with the file, then `POST /reconciliation/start` with its upload id |
| `GetReconciliation` | `GET /reconciliation/{id}` |

The first run without `RuleIDs` also creates the default rule. Amounts are sent in major units, as Blnk expects.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no `Source`; neither or both of `Records` and `File`; more than 10,000 records; a record without an id, amount or currency; a rule with no name or criteria; a strategy Blnk refuses | fix the call; upload a `File` for big reports |
| `ErrNotFound` | an unknown rule id, or an unknown run id | check the ids you stored |
| `ErrUnavailable` | Blnk can't be reached after retries | start the run again |

A run that Blnk couldn't finish comes back with `Status: "failed"` and no error. Check `Status` after `Done()`.

## After reconciling

- Money that arrived but matches no customer goes to [Suspense](suspense.html) until someone identifies it.
- A deposit recorded twice, or for the wrong amount, is fixed with [Manual adjustments](adjustments.html).
- Once the lines match, record the provider's payout to your bank with [Settlement](settlement.html).
- [Trial balance and reports](reports.html) shows the clearing account's balance to compare with the provider's own figure.
