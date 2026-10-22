# Reconciliation

**Scenario:** Paystack's daily settlement report lists three deposits. HumanBlnk
recorded two of them. Reconciliation finds the one that's missing.

## Step 1: say what counts as a match

A **rule** is a list of criteria that must all hold. Make it once and reuse
it. Matching on reference works because a deposit's reference was built from
Paystack's own id (chapter 4), and Paystack's report carries that id.

{{code 11-reconciliation/main.go rule}}

| `Criterion` | |
| --- | --- |
| `Field` | `MatchAmount`, `MatchDate`, `MatchReference`, `MatchDescription`, `MatchCurrency` |
| `Op` | `MatchEquals`, `MatchGreaterThan`, `MatchLessThan`, `MatchContains` (with `Pattern`) |
| `Drift` | how far apart they may be: a fraction of the amount for `MatchAmount` (`0.01` is 1%), seconds for `MatchDate` |

## Step 2: run it on the report

Up to 10,000 lines can be sent directly with `RunLines`:

{{code 11-reconciliation/main.go lines}}

A line's `ID` is the provider's id, and it must be **unique across every
statement**: Blnk refuses a line whose id it has stored before.

`Run` options: `Strategy` (`OneToOne` by default, `OneToMany` or
`ManyToOne`, with `GroupBy` to group, such as `"reference"`) and `DryRun`
(match without recording the results).

## Step 3: wait for the result

It runs in the background, so poll:

{{code 11-reconciliation/main.go poll}}

## A report as a file

Bigger reports are uploaded: CSV or JSON with the columns `id`, `amount`,
`currency`, `reference`, `description` and `date` (RFC 3339), with amounts
in major units (`10000`, not kobo). Then run it with
`Reconciliation.Run(ctx, statement, ledger.Run{…})`.

{{code 11-reconciliation/main.go upload}}

## The whole example

{{run 11-reconciliation}}

## What it doesn't do (yet)

- **It doesn't list which lines didn't match.** Blnk 0.15.6 reports counts
  only. To find the missing ones, check each line yourself with
  `Transactions.GetByReference`.
- **It doesn't fetch reports.** Something has to download them and call
  `Upload` or `RunLines`, on a schedule you choose.
- **It doesn't check balances.** To confirm the clearing account matches what
  Paystack owes, read it with `Accounts.Get` and compare it with the
  provider's total. `Get` takes a balance id, not `@PaystackClearing`: take it
  from any deposit entry's `From` posting, which names the clearing account
  by its balance id.
