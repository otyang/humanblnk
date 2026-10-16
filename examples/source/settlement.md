title: Provider settlement
group: ops
order: 5
lead: Record what Paystack paid into your bank and what your bank paid Flutterwave, so each provider's clearing account shows exactly what is still outstanding.
---
## Why settle

When Ada tops up ₦50,000 through Paystack, her wallet is credited at once, but the money is still with Paystack. humanblnk records that by debiting `@PaystackClearing`: Paystack owes Oja ₦50,000. A day later Paystack pays the day's collections into Oja's bank, less its fees. `ops.RecordSettlement` records that payment and brings the clearing account back down. Payouts work the other way: Flutterwave sends money to customers' banks, and Oja's bank pays Flutterwave for it, before or after.

Done every day, each clearing account holds only what is still outstanding with that provider, and finance can match it line by line against the provider's settlement report. See [Reconciliation](reconciliation.html).

| Situation | `Direction` | Clearing account | Bank |
|---|---|---|---|
| Paystack pays you the money it collected | `SettlementCollection` (the default) | credited | debited with the amount less the fee |
| Your bank funds Flutterwave for payouts, before or after they happen | `SettlementPayout` | debited | credited with the amount plus the fee |

In both directions the provider's fee goes to `@ProviderFees`, an expense.

## Collections: Paystack pays you

@sample collection | Paystack settles the day's ₦100,000

- `Amount` is the **gross** amount the settlement clears, the total of the deposits it covers. `Fee` is what Paystack kept. The bank receives `Amount - Fee`.
- `Provider: "paystack"` names `@PaystackClearing`, the same account `wallet.Deposit` debits with `Provider: "paystack"`.
- `Balance` is as Blnk stores it: asset and expense accounts run negative. `Natural` is the same figure on the account's normal side, so it reads positive when Paystack owes you money or the bank holds money.
- After the settlement `@PaystackClearing` is zero: Paystack owes nothing for these three top-ups.
- The entries post atomically. Either both land or neither does.

## Payouts: you fund Flutterwave

Oja keeps a balance with Flutterwave and tops it up from the bank. Record each top-up as a payout settlement.

@sample payout | Oja prefunds Flutterwave, then customers cash out

- The bank is credited `Amount + Fee` (₦60,250): that is what left it. `@FlutterwaveClearing` is debited ₦60,000, the money now sitting at Flutterwave.
- Each confirmed `wallet.Withdraw` with `Provider: "flutterwave"` credits `@FlutterwaveClearing`. After ₦45,000 of payouts, its natural balance is ₦15,000: what is left of the prefunded balance.
- If Flutterwave pays out first and bills you later, make the same call when you pay the bill. The withdrawals put `@FlutterwaveClearing` in credit (Oja owes Flutterwave), and the payout settlement brings it back to zero.
- For a payout settlement the fee can be any amount, including more than `Amount`. For a collection it must be less than `Amount`.

## Without a fee

@sample no-fee | A fee-free settlement with its own description

- Leave `Fee` out and only the main entry is made, so `Entries` has one item.
- `Direction` defaults to `SettlementCollection`.
- `Description` defaults to `Settlement <account>`, e.g. `Settlement PaystackClearing`. The fee entry gets the same text with ` fee` added.
- Every entry carries `txn_type: settlement`, `settlement_reference` and `provider` in its metadata.

## Retrying: the Reference

`Reference` is required. Use the provider's settlement id. The main entry uses it as is and the fee entry uses `Reference + "_fee"`.

@sample retry | The settlement job runs twice

- The same `Reference` with the same amounts returns the original entries with `Replayed: true`. Nothing is posted twice and the bank balance doesn't change.
- The same `Reference` with a different amount is refused with `ErrDuplicateReference`. A provider that corrects a settlement should send a new id; record the difference under it.

## Other accounts: ClearingAccount, BankAccount, FeeAccount

The defaults fit most cases. Override them when you have more than one bank account, or money that arrived without a named provider.

@sample overrides | Providus sweeps virtual-account collections into Oja's Providus account

| Field | Default | What it changes |
|---|---|---|
| `Provider` | none | names the clearing account: `"paystack"` → `@PaystackClearing` |
| `ClearingAccount` | none | an exact clearing account instead of `Provider`, e.g. `@DepositClearing` for deposits made without a provider |
| `BankAccount` | `@BankOperating` | the bank account the money reaches or leaves |
| `FeeAccount` | `@ProviderFees` | where the provider's fee is booked |
| `Description` | `Settlement <account>` | the entries' description |

- One of `Provider` or `ClearingAccount` is required. When both are set, `ClearingAccount` wins.
- New house accounts need no setup: Blnk creates `@BankProvidus` the first time it is used. Add them to `ledger.Chart` at start-up so `ops.GetHouseAccount`, `TrialBalance` and statements know their type and normal side.
- `ClearingAccount`, `BankAccount` and `FeeAccount` must be house accounts (`@Name`). They are posted with overdraft allowed, so a customer account id is refused with `ErrWrongKind` before anything is posted.

## What happens in Blnk

Blnk debits the source and credits the destination of each transaction.

Collection, ₦100,000 with a ₦1,500 fee:

| Account | Debit | Credit |
|---|---|---|
| `@BankOperating` | 98,500 | |
| `@ProviderFees` | 1,500 | |
| `@PaystackClearing` | | 100,000 |

Payout, ₦60,000 with a ₦250 fee:

| Account | Debit | Credit |
|---|---|---|
| `@FlutterwaveClearing` | 60,000 | |
| `@ProviderFees` | 250 | |
| `@BankOperating` | | 60,250 |

These are two Blnk transactions, the main one under `Reference` and the fee under `Reference_fee`, sent as one atomic bulk request. Settlements touch house accounts only, so they carry on while a customer account or ledger is frozen. The library refuses any other account.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no `Provider` or `ClearingAccount`; no `Reference`; a zero or negative `Amount`; a `Fee` in another currency; a collection fee equal to or above `Amount` | fix the call |
| `ErrWrongKind` | `ClearingAccount`, `BankAccount` or `FeeAccount` isn't a house account (`@Name`). Also matches `ErrInvalidInput` | pass the `@` name of a house account |
| `ErrDuplicateReference` | the `Reference` was used for a different settlement | use the provider's new settlement id |
| `ErrUnavailable` | Blnk can't be reached | retry with the same `Reference`; a settlement that did post comes back as a replay |

## Related

- [Deposits](deposits.html) and [Withdrawals](withdrawals.html): the movements that fill the clearing accounts.
- [House accounts](house-accounts.html): read `@PaystackClearing`, `@BankOperating` and `@ProviderFees`.
- [Reconciliation](reconciliation.html): match the clearing accounts against the provider's report.
- [Unidentified money](suspense.html): a collection that can't be matched to a customer still counts in the settlement.
