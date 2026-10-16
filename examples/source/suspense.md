title: Unidentified money
group: ops
order: 6
lead: Park money that arrived but can't be matched to a customer in @Suspense, then pay it to its owner or send it back to the provider.
---
## When money has no owner

Paystack tells Oja that ₦15,000 arrived, but the narration is blank and the sender's name matches no customer. Or the money is for an account that has since been closed or frozen. Oja can't credit anyone yet, but the money is real: Paystack holds it and will settle it with the rest. Suspense records it on the books now, in one place, until someone works out whose it is.

| Situation | Use | Money moves |
|---|---|---|
| Money arrived and you can't tell whose it is | `ops.HoldInSuspense` | clearing account → `@Suspense` |
| Support finds the owner | `ops.ClaimFromSuspense` | `@Suspense` → the customer's account |
| Nobody claims it, or the provider reverses it | `ops.ReturnFromSuspense` | `@Suspense` → clearing account |
| You know the customer and the account is open | `wallet.Deposit`. See [Deposits](deposits.html) | clearing account → customer |

## Park it: ops.HoldInSuspense

@sample hold | ₦15,000 with no matching customer

- `Provider: "paystack"` takes the money from `@PaystackClearing`, the same account deposits use. Paystack still owes it, so it settles with the rest of the day's collections. See [Provider settlement](settlement.html).
- `ClearingAccount` names an exact clearing account instead. With neither, `Config.DepositClearingAccount` is used (`@DepositClearing` by default).
- The clearing account must be a house account (`@Name`). It is posted with overdraft allowed, so a customer account id is refused with `ErrWrongKind`.
- `Reference` is required. Use the provider's payment id: a retry with the same reference and amount returns the original, and the same reference can't be used for a different amount.
- `Note` is what you know about the money: sender, bank, narration. It becomes the description and is stored as `suspense_note`. Without a note the description is `Unidentified funds`.
- `MetaData` keys are stored on the entry next to `suspense_note`. Put what your back office searches on there, such as the sender's name.
- `@Suspense` is a liability: Oja owes this money to someone. Its natural balance is the total waiting to be matched.

## Pay it to its owner: ops.ClaimFromSuspense

@sample claim | Support matches it to Tunde

- `AccountID` must be a customer account. The money arrives like any other movement, so a frozen or closed account refuses it.
- `SuspenseReference` links the claim to the `Reference` you held it under. It is stored as `suspense_reference` so you can trace every claim back.
- `Note` becomes the description. Without one it is `Suspense claim`.
- No fee is charged.

## Part of the money

One transfer can cover more than one customer. Claim each part separately with its own `Reference` and the same `SuspenseReference`.

@sample partial | One ₦30,000 transfer split between Ada and Tunde

> warn: `@Suspense` is one pool per currency. humanblnk checks a claim against what the pool holds, not against the item named in `SuspenseReference`. Track what is left of each item in your own back office, for example by summing claims per `suspense_reference`.

## Send it back: ops.ReturnFromSuspense

@sample return | Unclaimed after 14 days, reversed to the sender

- The money goes back to the provider's clearing account. `Provider: "paystack"` means `@PaystackClearing`. `ClearingAccount` names an exact one; with neither, `Config.DepositClearingAccount` is used.
- The clearing account must be a house account (`@Name`). A customer account id is refused with `ErrWrongKind`.
- In the sample `@PaystackClearing` drops from ₦53,000 to ₦45,000: Paystack no longer owes Oja the ₦8,000, because it is sending the money back to the sender. The ₦45,000 left is the earlier suspense money Paystack has not settled yet.
- `Note` becomes the description. Without one it is `Suspense return`.

## Money for a closed account

A closed account refuses every deposit with `ErrAccountClosed`, but a late payment can still reach the provider. Hold it in suspense, then claim it for the customer's new account.

@sample closed | A late payment for Chidi Stores' old account

The same works for a frozen account: hold the money while the account is frozen and claim it once it is unfrozen. See [Close an account](close-account.html) and [Freeze and unfreeze](freeze.html).

## The fields

| Input | Field | Required | What it does |
|---|---|---|---|
| `SuspenseInput` | `Amount` | yes | the amount that arrived |
| `SuspenseInput` | `Provider` or `ClearingAccount` | no | where it arrived; default `Config.DepositClearingAccount` |
| `SuspenseInput` | `Reference` | yes | the idempotency key, e.g. the provider's payment id |
| `SuspenseInput` | `Note` | no | what is known about it; the description and `suspense_note` |
| `SuspenseInput` | `MetaData` | no | your own keys, stored on the entry |
| `ClaimFromSuspenseInput` | `AccountID` | yes | the customer account it belongs to |
| `ClaimFromSuspenseInput` | `Amount`, `Reference` | yes | how much, and the idempotency key |
| `ClaimFromSuspenseInput` | `SuspenseReference`, `Note` | no | the held item it comes from; the description |
| `ReturnFromSuspenseInput` | `Provider` or `ClearingAccount` | no | where it goes back to; default `Config.DepositClearingAccount` |
| `ReturnFromSuspenseInput` | `Amount`, `Reference` | yes | how much, and the idempotency key |
| `ReturnFromSuspenseInput` | `SuspenseReference`, `Note` | no | the held item it comes from; the description |

## What happens in Blnk

Blnk debits the source and credits the destination. Each call makes one transaction.

`HoldInSuspense`, ₦15,000 through Paystack (`txn_type: suspense_in`):

| Account | Debit | Credit |
|---|---|---|
| `@PaystackClearing` | 15,000 | |
| `@Suspense` | | 15,000 |

`ClaimFromSuspense` to Tunde (`txn_type: suspense_out`):

| Account | Debit | Credit |
|---|---|---|
| `@Suspense` | 15,000 | |
| Tunde's account | | 15,000 |

`ReturnFromSuspense` to Paystack, ₦8,000 (`txn_type: suspense_return`):

| Account | Debit | Credit |
|---|---|---|
| `@Suspense` | 8,000 | |
| `@PaystackClearing` | | 8,000 |

The hold is posted with overdraft allowed on the clearing account, as deposits are. Claims and returns are not, so `@Suspense` can never go below zero.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInsufficientFunds` | a claim or return for more than `@Suspense` holds in that currency | check what is still held; it may already have been claimed |
| `ErrWrongKind` | a claim to a house account such as `@FeeIncome`; a hold or return whose clearing account is a customer account. Also matches `ErrInvalidInput` | pass the customer's account for a claim, a `@` clearing account for a hold or return |
| `ErrAccountFrozen` | a claim to a frozen account | wait until it is unfrozen; the money stays in suspense |
| `ErrAccountClosed` | a claim to a closed account | claim it for the customer's new account |
| `ErrInvalidInput` | no `Reference`; a zero amount | fix the call |
| `ErrDuplicateReference` | a `Reference` reused for a different amount or account | use a new reference for each hold, claim and return |

## Related

- [Provider settlement](settlement.html): money in suspense is still part of what the provider settles.
- [Close an account](close-account.html) and [Freeze and unfreeze](freeze.html): why a deposit might be refused.
- [House accounts](house-accounts.html): read `@Suspense` and its history.
- [Reconciliation](reconciliation.html): find unmatched payments in the provider's report.
