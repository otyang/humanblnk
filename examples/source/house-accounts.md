title: House accounts and the chart of accounts
group: ops
order: 2
lead: Read the business's own accounts, such as @FeeIncome and @PaystackClearing, know what each one means in the books, and add or rename your own.
---
## What house accounts are

Every movement has two sides. When Paystack collects ₦50,000 for Ada, Ada's account goes up, and something on Oja's side must record that Paystack now holds ₦50,000 that belongs to Oja. That other side is a **house account**: an account that belongs to the business itself ("the house"), not to a customer.

In Blnk, house accounts are indicator balances whose name starts with `@`. Blnk creates one the first time a transaction uses it, one per currency, in its general ledger. Nobody opens them. They change only through the movements that use them (deposits, withdrawals, fees, settlement, adjustments), so the Ops methods for them only read.

| Need | Use |
|---|---|
| One house account in one currency | `ops.GetHouseAccount` with a `ledger.HouseRef` |
| Every house account in a currency | `ops.ListHouseAccounts` |
| Its transactions | `ops.HouseHistory` |
| Its balance at a moment | `ops.HouseBalanceAt` |
| A statement for a period | `ops.HouseStatement` |
| What an account means in the books | `ledger.Chart`, `ledger.ChartEntry` |
| The clearing account of a provider | `ledger.ClearingAccount` |

The samples on this page run after one day of activity: Ada tops up ₦50,000 through Paystack (₦750 fee), Tunde tops up ₦20,000 (₦300 fee), Ada withdraws ₦10,000 through Flutterwave (₦50 fee), and in the evening Paystack settles Ada's ₦50,000 to Oja's bank, keeping a ₦750 provider fee.

## Read one: ops.GetHouseAccount

A house account is named by its indicator and a currency, together a `ledger.HouseRef`.

@sample get | Fee income and the Paystack clearing account

`HouseAccount` fields:

| Field | What it holds |
|---|---|
| `ID` | Blnk's balance id for this indicator and currency |
| `Indicator`, `Currency`, `Precision` | the name, currency and precision |
| `LedgerID` | Blnk's general ledger, `general_ledger_id` |
| `Type` | `asset`, `liability`, `equity`, `income` or `expense`, from the chart |
| `Normal` | the side its balance normally sits on: `debit` or `credit` |
| `Balance`, `PendingIn`, `PendingOut`, `Credits`, `Debits` | the figures as Blnk stores them |
| `Natural` | `Balance` on the account's normal side |
| `CreatedAt`, `MetaData` | as Blnk stores them |

`Ref()` returns the `HouseRef` for the account, ready for the other house methods.

### Balance and Natural

Blnk takes money from the source and adds it to the destination. A deposit takes ₦50,000 from `@PaystackClearing`, so in Blnk that balance runs negative. In the books it is an asset: Paystack owes Oja the money.

`Natural` turns the balance the right way round for its type. For a debit-normal account (assets, expenses) it is `Balance` with the sign flipped; for a credit-normal one it equals `Balance`. In their usual state, all house accounts read positive in `Natural`. Here `@PaystackClearing` reads ₦20,000: Tunde's top-up, which Paystack has not settled yet.

## Every house account: ops.ListHouseAccounts

@sample list | Oja's naira house accounts

- The list is in the order Blnk created the accounts. Accounts never used in the currency are not there.
- `@FlutterwaveClearing` reads `-10000.00` in `Natural`. By default every `@…Clearing` account is an asset, but Flutterwave only pays out for Oja, so it holds money Oja owes. The chart below fixes that.
- `wallet.FindAccounts` never lists house accounts. This is the way to see them.

## The chart of accounts: ledger.Chart

`ledger.Chart` maps each indicator to a `GLDef` (its `Type` and `Normal` side). These are the accounts humanblnk uses, each with a constant:

@sample chart

| Constant | Account | Type | Role |
|---|---|---|---|
| `FeeIncomeAccount` | `@FeeIncome` | income | every fee leg is credited here |
| `DepositClearingAccount` | `@DepositClearing` | asset | the deposit source when no provider is named |
| `PayoutClearingAccount` | `@PayoutClearing` | liability | the withdrawal destination when no provider is named |
| `MerchantPayableAccount` | `@MerchantPayable` | liability | captured card-style holds owed to merchants |
| `SuspenseAccount` | `@Suspense` | liability | money that arrived but can't be matched yet |
| `FXPoolAccount` | `@FXPool` | asset | the open currency position from exchanges, one balance per currency |
| `AdjustmentsAccount` | `@Adjustments` | equity | the other side of manual corrections |
| `BankOperatingAccount` | `@BankOperating` | asset | your bank account |
| `ProviderFeesAccount` | `@ProviderFees` | expense | fees providers keep at settlement |

`ledger.ChartEntry(indicator)` looks an indicator up: its `Chart` entry, else asset and debit-normal for any `@…Clearing` account, else `GLUnknown` (an empty type).

### Clearing accounts, one per provider

`Provider: "paystack"` on a deposit or withdrawal uses `@PaystackClearing`, and `Provider: "flutterwave"` uses `@FlutterwaveClearing`. `ledger.ClearingAccount` gives the name: it accepts `"paystack"`, `"@Paystack"` or `"@PaystackClearing"`, and returns `@DepositClearing` for an empty provider. To use an exact account, set `ClearingAccount` on the input instead of `Provider`.

One clearing account per provider means each provider's balance is what that provider still owes you, or you owe it, and can be reconciled against that provider's settlement report. See [Settlement](settlement.html).

## History, balance at a moment, statement

@sample history | @PaystackClearing over the day

- `HouseHistory` takes the same `HistoryQuery` as `wallet.History` and returns the same `HistoryPage`, newest first.
- A deposit with a fee is recorded as two legs from the clearing account (`paystack_T100201-1` to Ada, `paystack_T100201-2` to `@FeeIncome`), so both appear.
- `HouseBalanceAt` returns the posted balance at that instant as Blnk stores it, so a debit-normal account reads negative. At noon, Paystack held both top-ups: `-70000.00`.

@sample statement | The day's statement for @PaystackClearing

`HouseStatement` works like `wallet.Statement`: posted entries only, dated by when they took effect, checked against Blnk's historical balance. `Indicator` is set on the statement, and each line's `Counterpart` names the other account, as an id for a customer account or as `@Name` for a house account. See [Statements](statements.html).

## Adding your own accounts to the chart

Add or change entries in `ledger.Chart` at start-up, before the app serves requests. The map is not safe to change while other goroutines read it.

@sample custom | Flutterwave as a payable, and a marketing budget

- With `@FlutterwaveClearing` marked a liability, its `Natural` reads positive: Oja owes Flutterwave ₦10,000 for the payout it made.
- `@Marketing` is a new house account. A transfer from `AccountID("@Marketing")` creates it in Blnk; it needs `AllowOverdraft` because it starts at zero. The chart makes it an expense, so ₦500 of bonuses reads as ₦500 spent.
- An indicator not in the chart and not ending in `Clearing` has an empty `Type` and `Normal`, and its `Natural` equals `Balance`.

## Renaming the accounts humanblnk uses

If your books already carry money on other indicators, point humanblnk at them so balances aren't split across two accounts:

| Config field | Default | Used for |
|---|---|---|
| `FeeIncomeAccount` | `@FeeIncome` | every fee leg |
| `DepositClearingAccount` | `@DepositClearing` | a deposit with no `Provider` or `ClearingAccount` |
| `PayoutClearingAccount` | `@PayoutClearing` | a withdrawal with no `Provider` or `ClearingAccount` |

@sample overrides | Oja's older books used @Fees and @World

A renamed account keeps the meaning of the one it replaces: `@Fees` reads as income, and `@World` as an asset, like `@DepositClearing`. Set these once and never change them after money has moved, or balances will sit in two places.

## What happens in Blnk

The day on this page, as entries (Blnk takes from the debit side and adds to the credit side):

| Movement | Account | Debit | Credit |
|---|---|---|---|
| Ada's top-up | `@PaystackClearing` | ₦50,000 |  |
| Ada's top-up | Ada's account |  | ₦49,250 |
| Ada's top-up | `@FeeIncome` |  | ₦750 |
| Tunde's top-up | `@PaystackClearing` | ₦20,000 |  |
| Tunde's top-up | Tunde's account |  | ₦19,700 |
| Tunde's top-up | `@FeeIncome` |  | ₦300 |
| Ada's withdrawal | Ada's account | ₦10,050 |  |
| Ada's withdrawal | `@FlutterwaveClearing` |  | ₦10,000 |
| Ada's withdrawal | `@FeeIncome` |  | ₦50 |
| Paystack settles | `@BankOperating` | ₦49,250 |  |
| Paystack settles | `@ProviderFees` | ₦750 |  |
| Paystack settles | `@PaystackClearing` |  | ₦50,000 |

Blnk creates each `@` balance in its general ledger the first time it is used, and stores it on transactions by its balance id. Receipts and history therefore show the id; statements show the `@Name`.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrWrongKind` | a house account, by `@Name` or by its balance id, passed to a wallet method such as `wallet.GetAccount`; also matches `ErrInvalidInput` | read it with `ops.GetHouseAccount` |
| `ErrInvalidInput` | an indicator that doesn't start with `@`; no currency | fix the `HouseRef` |
| `ErrNotFound` | the account has never been used in that currency | nothing to read yet |

## Related pages

- [Fees and pricing](fees.html): what lands in `@FeeIncome`.
- [Settlement](settlement.html): clearing provider accounts against your bank.
- [Suspense](suspense.html), [Adjustments](adjustments.html), [Currency exchange](currency-exchange.html): the other house accounts at work.
- [Reports](reports.html): the trial balance across every account.
- [Statements](statements.html): the customer-side history and statements.
