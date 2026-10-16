title: Trial balance and reports
group: ops
order: 8
lead: Prove the books add up, see what you owe customers and what the business holds, and close the month with checked statements for any house account.
---
## What finance needs

At the end of every day Oja's finance team wants three answers. Do the books still add up? How much do we owe customers? Where is that money: in the bank, or still with Paystack? At month-end they also need each house account's closing balance and the entries behind it.

| Question | Use |
|---|---|
| Do the books add up? | `ops.TrialBalance`, check `Balanced` |
| How much do we owe customers? | `ops.TrialBalance`, read `CustomerFunds` |
| What does each house account hold? | `ops.TrialBalance`, read `House`; or `ops.GetHouseAccount` for one |
| A house account's balance on a past date | `ops.HouseBalanceAt` |
| The entries behind a house account for a period | `ops.HouseStatement` |
| A customer's statement | `wallet.Statement`; see [Statements](statements.html) |

## Some activity first

The samples below run on two days of activity: two Paystack deposits with fees, two transfers, a Flutterwave payout, a cashback adjustment, and the next day's settlements.

@sample activity | 1 and 2 September at Oja

## The trial balance: ops.TrialBalance

`TrialBalance` reads every balance in one currency, 100 per request, and adds them up.

@sample trial

- `Balanced` is true when all the balances sum to zero. In double entry every transaction debits one account and credits another by the same amount, so the total is always zero. `Net` is that total.
- `CustomerFunds` is the sum of every customer account, pot and escrow: the money Oja owes its customers. `CustomerAccounts` is how many there are.
- `House` lists every house account with its `Type` and `Normal` side from `ledger.Chart`. `Natural` is the balance on its normal side, so each account reads positive in its usual state. `Balance` is as Blnk stores it: debit-normal accounts run negative.
- `House` comes back in creation order. Sort it before you print or compare it.
- It reads every balance in that currency, in every ledger. For a large ledger that is many requests, so run it from a job, not on a request path.

### What Balanced means

`Balanced: true` means nothing is half-posted: everything that left one account arrived in another. A false result means entries were lost, or something changed balances without going through double entry. Treat it as an incident.

`Balanced: true` doesn't mean the figures are right. A deposit recorded twice, or a fee charged at the wrong rate, still balances. Catch those with [Reconciliation](reconciliation.html) against the provider's report and fix them with [Manual adjustments](adjustments.html).

## A nightly check

Run the trial balance every night for every currency you hold, and alert when it doesn't balance.

@sample nightly

A currency with no balances at all, like USD here, comes back balanced with zero funds.

## Customer funds and safeguarding

In many countries a wallet business must keep customer money apart from its own and show that it holds enough to pay every customer back. The trial balance gives both sides of that comparison.

@sample safeguarding | Is customer money covered?

- Net house assets are the asset accounts (the bank, money Paystack still owes) less the liability accounts other than customers (money owed to a payout provider, money in `@Suspense`).
- The difference is the business's own money: fees earned, less provider fees paid and cashback given. Here ₦1,275 − ₦750 − ₦500 = ₦25.
- If the difference goes negative, customer money is paying for the business's costs. Move money into the bank account and record it.
- `@BankOperating` is the bank balance as the ledger knows it. Compare it with your real bank statement too.

> note: `@FlutterwaveClearing` shows as an asset because every `@…Clearing` account not in `ledger.Chart` is one. Oja only pays out through Flutterwave, so for Oja it is money owed to Flutterwave. Add `ledger.Chart["@FlutterwaveClearing"] = ledger.GLDef{Type: ledger.GLLiability, Normal: ledger.CreditNormal}` at start-up so `Natural` and the sum above treat it that way. See [House accounts](house-accounts.html).

## Month-end: HouseBalanceAt and HouseStatement

On 2 October, after a new transfer has already earned a fee, finance closes September.

@sample month-end | Closing September for @FeeIncome

- `ops.HouseBalanceAt(ctx, ref, t)` returns the posted balance at `t`. Pass midnight on the first of the next month to get the month-end figure. Pending (INFLIGHT) amounts aren't included.
- `ops.HouseStatement(ctx, ref, from, to)` lists the posted entries from `from` up to, but not including, `to`, with opening and closing balances and totals. It checks its running balance against Blnk's historical balance at `to`, so a statement that doesn't add up is an error, not a wrong document.
- `Counterpart` shows `@Name` for house accounts and the account id for customer accounts.
- A movement with a fee is stored as legs, so each fee is its own line.

### A debit-normal account

`HouseBalanceAt` returns the balance as Blnk stores it, so an asset reads negative. Use `Natural` from `GetHouseAccount` or the trial balance for the everyday reading.

@sample clearing | What Paystack still owes

Compare this figure with Paystack's own report of what it hasn't settled yet. When they differ, [Reconciliation](reconciliation.html) finds the lines that don't match.

## A customer statement

The same checked statement is available for a customer account through `wallet.Statement`. More in [Statements](statements.html).

@sample customer | Ada's September

The transfer to Tunde shows as two lines: the payment and its ₦25 fee.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no currency; a house indicator that doesn't start with `@`; `from` not before `to`; a statement over 10,000 lines | fix the call; split a long period by month |
| `ErrWrongKind` | a customer account or ledger id where a house account is expected, or the other way round | use `HouseRef` for house accounts and `wallet.Statement` for customers |
| `ErrNotFound` | the house account has never been used in that currency, so Blnk hasn't created it | nothing to report yet |
| `ErrUnavailable` | Blnk can't be reached after retries | run the job again later |

## Related pages

- [House accounts](house-accounts.html) for what each `@` account means.
- [Settlement](settlement.html) to clear provider accounts against the bank.
- [Reconciliation](reconciliation.html) to match a provider's report line by line.
- [Statements](statements.html) for customer statements.
- [Balance alerts](balance-alerts.html) to hear about a balance as it changes, not at night.
