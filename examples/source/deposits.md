title: Deposits and top-ups
group: wallet
order: 2
lead: Credit a customer when money arrives from outside: card top-ups, bank transfers and crypto, with fees, safe webhook retries and deposits held for review.
---
## The situation

Ada tops up her Oja wallet with her card. Paystack collects the money and sends you a `charge.success` webhook. You record it with `wallet.Deposit`: Ada's account goes up, and a clearing account for Paystack records that Paystack now owes Oja that money until it settles to your bank.

Pick the options by situation:

| Situation | Use |
|---|---|
| A card or bank top-up through a provider | `Provider: "paystack"` |
| Money arriving on a channel with its own clearing account | `ClearingAccount: "@WemaVirtualClearing"` |
| A free top-up | `Fee: ledger.NoFee` |
| A top-up with a fee deducted | `Fee` on the call, or leave it out and let `Config.Fees` price it |
| Money you want to check before the customer can spend it | `Pending: true`, then `wallet.Confirm` or `wallet.Cancel` |
| Money you can't match to a customer yet | not a deposit: `ops.HoldInSuspense`, see [Suspense](suspense.html) |

## A top-up through a provider

@sample provider | Ada tops up ₦50,000 through Paystack

- `Provider` names the clearing account debited: `"paystack"` becomes `@PaystackClearing`. `ledger.ClearingAccount` does the same conversion and also accepts `"@Paystack"` and `"@PaystackClearing"`.
- `ClearingAccount` overrides `Provider` with an exact house account.
- With neither, the deposit comes from `Config.DepositClearingAccount`, which is `@DepositClearing` by default.
- `Reference` is required. Use the provider's own payment reference, so the same payment can never be credited twice (see below).
- A clearing account is debit-normal, so Blnk shows it negative. `Natural` reads it the right way round: Paystack owes Oja ₦50,000. [Settlement](settlement.html) clears it when the money reaches your bank.
- Blnk creates each `@` account the first time it is used. There is nothing to set up.

## Fees

`Amount` is always the gross amount the provider collected. A fee is deducted from it and goes to `@FeeIncome`; the customer gets the rest.

### A fee on the call

@sample fee | Tunde tops up ₦10,000 and pays a ₦100 fee

- The fee's currency can be left empty: it is taken from `Amount`. A different currency is refused.
- The fee must be less than the amount.
- The receipt has one leg per destination. The fee and net amount are also stored in metadata as `fee`, `fee_precise`, `net_amount` and `net_precise`, for reports.

### Fees from Config.Fees

Every deposit must say what it charges. Either set `Fee`, or set `Config.Fees` to price every call that leaves `Fee` out. With neither, the deposit is refused before it reaches Blnk, so a fee can't be forgotten.

@sample config-fees | Oja charges 1.5%, capped at ₦2,000

- `Config.Fees` receives a `FeeRequest` with `Kind: ledger.FeeDeposit`, the receiving `Account`, the gross `Amount` and the `Provider`. See [Fees](fees.html) for the full pricing function.
- An explicit `Fee` always wins. `ledger.NoFee` makes one deposit free even when `Config.Fees` would charge.
- With no fee there is a single destination: no legs and no fee metadata.

## Retries and duplicate webhooks

Providers send the same webhook more than once. Because the reference is the provider's own, a repeat is harmless.

@sample replay

- Same reference, same account, amount and source: the original comes back with `Replayed: true`. Nothing is posted again.
- Same reference with a different amount, account or source: `ErrDuplicateReference`. Treat that as something to investigate, not something to retry.
- A retry is priced again by `Config.Fees`. If the price changed in between, the retry no longer matches and gets `ErrDuplicateReference`. Store the fee you charged and retry with it set on `Fee`.

## Holding a deposit for review

`Pending: true` records the deposit as pending (Blnk `INFLIGHT`). The money shows in `PendingIn` but not in `Balance` or `Available`, so Ada can't spend it yet.

@sample pending | Compliance checks a ₦900,000 transfer before it counts

- `wallet.Confirm(ctx, id, nil)` posts it. `wallet.Cancel` drops it and nothing is credited.
- A pending deposit with a fee has two legs, so it can only be confirmed in full. More in [Card holds and capture](card-holds.html).

## Crypto deposits

Amounts are decimal strings, stored as exact integers in the currency's smallest unit. BTC has precision 100000000 (satoshi); USDT and USDC 1000000; ETH 1000000000000000000. No floats are involved.

@sample crypto | 0.0125 BTC arrives on-chain

- Use something unique per on-chain output as the reference, such as `txid:vout`.
- An amount with more decimal places than the currency has is refused with `ErrInvalidInput`. Nothing is rounded.
- `Provider: "custody"` debits `@CustodyClearing`, one clearing account per custody provider.

## Frozen and closed accounts

@sample status

- A frozen account takes no money in or out. A deposit to it fails with `ErrAccountFrozen`.
- A closed account takes nothing, for good: `ErrAccountClosed`.
- A frozen ledger only stops money leaving its accounts. Deposits still go through.
- For money that arrives for a frozen or closed account, hold it with `ops.HoldInSuspense` and claim it or send it back later. See [Freeze and unfreeze](freeze.html), [Close an account](close-account.html) and [Suspense](suspense.html).

## What happens in Blnk

A deposit is one Blnk transaction from the clearing account to the customer, with `allow_overdraft` set on the source because the money came from outside. Its metadata has `txn_type: deposit`.

| Account | Debit | Credit |
|---|---|---|
| `@PaystackClearing` | ₦10,000 (gross) | |
| `@FeeIncome` | | ₦100 (fee) |
| Tunde's NGN account | | ₦9,900 (net) |

Without a fee there is one destination and the customer is credited the full amount. A transaction with a fee goes to Blnk with two destinations; Blnk stores it as two leg transactions, `<reference>-1` and `<reference>-2`. With `Pending: true` the same entries are held `INFLIGHT` until you confirm or cancel.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no `Reference`; no `Fee` and no `Config.Fees`; fee not less than the amount; too many decimal places; currency doesn't match the account | fix the call |
| `ErrWrongKind` | `AccountID` is a house account such as `@FeeIncome` | pass a customer account; post to house accounts with `ops` calls |
| `ErrDuplicateReference` | the reference was used for a different deposit | investigate; don't retry with a new reference |
| `ErrAccountFrozen` | the account is frozen or blocked | hold the money in suspense, or unfreeze first |
| `ErrAccountClosed` | the account is closed | hold the money in suspense and claim it for a new account |
| `ErrFeePricing` | `Config.Fees` returned an error or no fee | fix the pricing function |
| `ErrUnavailable` | Blnk can't be reached | retry with the same reference |

## Related

- [Quotes for confirm screens](quotes.html): show the fee before the customer pays.
- [Withdrawals and payouts](withdrawals.html): the other direction.
- [Webhooks](webhooks.html): receive the provider's and Blnk's notifications.
- [Settlement](settlement.html): clear `@PaystackClearing` against your bank.
