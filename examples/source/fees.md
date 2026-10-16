title: Fees and pricing
group: wallet
order: 14
lead: Charge for deposits, withdrawals, transfers and exchanges: a fee on each call, or one pricing function for the whole app, with every fee posted to @FeeIncome.
---
## How fees work

Oja charges for some movements and not others. A Paystack top-up costs 1.5%, a transfer to a friend costs ₦10, a Flutterwave payout costs $0.50 in the customer's currency, and Chidi Stores, a tier3 merchant, pays nothing.

humanblnk never guesses a fee. Every chargeable call must say what it charges, or the call fails before it reaches Blnk. The chargeable calls are `Deposit`, `Withdraw`, `Transfer`, `Exchange`, `Schedule`, each item of `Bulk`, and the quotes for them.

| Situation | Use |
|---|---|
| A few calls, or prices decided by the caller | `Fee` on the call |
| A deliberately free movement | `Fee: ledger.NoFee` |
| One price list for the whole app | `Config.Fees`, a `FeeFunc` |
| Prices as percentage, fixed, minimum, maximum or bands | `FeeRule` with `ComputeFee` |
| A fee set in another currency | `FeeRequest.Convert` inside your `FeeFunc` |
| A promotion or a waiver | `Fee` on the call; it skips `Config.Fees` |

Movements that are not customer payments never take a fee: holds, splits, escrow, refunds, disputes, suspense, settlement and adjustments.

## A fee on the call

Set `Fee` on the input. It is always in the transaction's currency, so you can write it with an empty currency.

@sample explicit | A ₦750 fee on a top-up, and a free transfer

- A deposit fee is taken out of `Amount`: Paystack received ₦50,000, Ada gets ₦49,250 and `@FeeIncome` gets ₦750.
- A withdrawal or transfer fee is charged on top: the sender pays `Amount` plus `Fee` and the recipient gets `Amount`.
- A movement with a fee is recorded as one Blnk transaction with two destinations. `Receipt.Legs` lists them: the fee leg has a fixed amount (`75000` kobo), the customer's leg takes what is `left`.
- The receipt's `MetaData` records `fee`, `net_amount` and their `_precise` forms, for your reports.
- `ledger.NoFee` says "free" on purpose. It is not the same as leaving `Fee` out.

## Fee rules: FeeRule and ComputeFee

A `FeeRule` describes a price without writing arithmetic. `svc.ComputeFee` applies it to an amount as typed and returns a fee ready to pass as `Fee`.

@sample rule | The same three amounts under four rules, and the three rounding modes

| Field | What it does |
|---|---|
| `Percent` | a percentage of the amount, e.g. `"1.5"` (a trailing `%` is accepted) |
| `Fixed` | a flat part added to the percentage |
| `Min` | a floor on the fee, applied after `Percent` and `Fixed` |
| `Max` | a cap on the fee, applied last |
| `Rounding` | how the percentage part rounds to the smallest unit: `RoundHalfUp` (default), `RoundUp` (in the house's favour), `RoundDown` (in the customer's favour) |
| `Tiers` | bands that replace the fields above: the first tier whose `UpTo` is at or above the amount applies; a tier with an empty `UpTo` covers everything above |

- Amounts in a rule are decimals in the transaction's currency, so keep one rule per currency.
- `ledger.ComputeFee(rule, r.Amount)` is the same calculation for an amount that is already resolved, such as `FeeRequest.Amount`. Use it inside a `FeeFunc`.
- If no tier covers the amount (every tier has an `UpTo` and the amount is above all of them), `ComputeFee` returns `ErrInvalidInput`.

## A fee set in another currency: Convert

Oja's payout fee is "$0.50 everywhere". `FeeRequest.Convert` turns it into the transaction's currency at a rate you supply (units of the transaction currency per 1 unit of the fee currency), rounded to that currency's precision with the mode you choose.

@sample convert

The result is in the transaction's currency, which is the only currency a fee can be in. You get the rate; humanblnk does not fetch exchange rates.

## One price list: Config.Fees

Set `Config.Fees` to a `FeeFunc` and every chargeable call without its own `Fee` asks it for the price. It runs on every call, so prices can change while the app is running: read them from your database and finance can reprice without a deploy.

@sample config | Oja's price list, used by a deposit, a withdrawal and a merchant transfer

The function gets a `FeeRequest`:

| Field | What it holds |
|---|---|
| `Kind` | `ledger.FeeDeposit`, `FeeWithdrawal`, `FeeTransfer` or `FeeExchange` |
| `Account` | the customer account charged, with its `MetaData` (e.g. `tier`): the receiving account for a deposit, the sending account otherwise; nil when that side is a house account |
| `Amount` | the transaction amount, resolved (currency and precision set): the gross amount for a deposit, what the recipient gets for a withdrawal or transfer, the amount sold for an exchange |
| `Provider` | for deposits and withdrawals, e.g. `"paystack"` |
| `BuyCurrency` | for exchanges, the currency bought |

What it returns decides the call:

- a `Money` in `r.Amount`'s currency, or with an empty currency: that is the fee;
- `ledger.NoFee`: the call is free;
- an error: the call is refused with `ErrFeePricing`, and your error is kept as the cause;
- an empty `ledger.Money{}`, or a fee in another currency: also `ErrFeePricing`.

An exchange's fee is in the sell currency and charged on top of the amount sold. `Schedule` asks once, when the plan is created, and charges that fee on every occurrence. `Bulk` asks once per item. Quotes (`QuoteDeposit`, `QuoteWithdraw`, `QuoteTransfer`, `QuoteExchange`) call the same function, so the fee a customer sees on the confirm screen is the fee they pay. See [Quotes](quotes.html).

## Waivers and promotions

A `Fee` on the call always wins over `Config.Fees`. Use that for a free weekend, a goodwill gesture, or a discount you work out yourself.

@sample waiver | A free transfer, and a half-price one priced from the quote

- Record why in `MetaData` (here `promo`), so the waiver shows in reports.
- For a discount, take the usual price from a quote and pass the reduced fee.
- Customers who never pay, like Chidi Stores on tier3, belong in the `FeeFunc` itself (it returns `NoFee` for them), not in every call.

## Retries are priced again

A retry with the same `Reference` and the same entries returns the original with `Replayed: true`. With `Config.Fees`, the retry is priced again. If the price changed between the first attempt and the retry, the entries differ and the retry is refused with `ErrDuplicateReference`.

@sample retry

> tip: Keep the fee from the first attempt (from the quote or the receipt) and pass it as `Fee` when you retry. The retry then matches the original whatever the price list says now.

## Where fees go: @FeeIncome

Every fee leg is credited to `@FeeIncome` in the transaction's currency, a house account of type income. Read it with `ops.GetHouseAccount`.

@sample income | Oja's naira fee income after the calls above

- In Blnk, each fee is its own leg with a reference like `p2p_20004-1`, so `@FeeIncome`'s history lists the fee legs only.
- If your books already use another account, such as `@Fees`, set `Config.FeeIncomeAccount`. See [House accounts](house-accounts.html).

## What happens in Blnk

A ₦50,000 Paystack deposit with a ₦750 fee:

| Account | Debit | Credit |
|---|---|---|
| `@PaystackClearing` | ₦50,000 | |
| Ada's account | | ₦49,250 |
| `@FeeIncome` | | ₦750 |

A ₦20,000 Flutterwave withdrawal with a ₦766.20 fee:

| Account | Debit | Credit |
|---|---|---|
| Ada's account | ₦20,766.20 | |
| `@FlutterwaveClearing` | | ₦20,000 |
| `@FeeIncome` | | ₦766.20 |

A ₦3,000 transfer with a ₦10 fee:

| Account | Debit | Credit |
|---|---|---|
| Ada's account | ₦3,010 | |
| Tunde's account | | ₦3,000 |
| `@FeeIncome` | | ₦10 |

A movement with no fee is a single entry from one account to the other, with no `@FeeIncome` leg.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | no `Fee` and no `Config.Fees`; a fee in another currency than the amount; a negative fee; a deposit fee equal to or larger than the deposit | fix the call; pass `ledger.NoFee` for a free movement |
| `ErrFeePricing` | `Config.Fees` returned an error, an empty `Money`, or a fee in another currency | fix the price list; `errors.As` or the message gives your function's error |
| `ErrDuplicateReference` | a retry priced differently from the first attempt | retry with the original `Fee` |
| `ErrInsufficientFunds` | the sender can't cover `Amount` plus `Fee` | ask the customer to top up |
| `ErrLimitExceeded` | `Amount` plus `Fee` is over the customer's limit | see [Limits](limits.html) |

## Related pages

- [Quotes](quotes.html): show the fee before the customer confirms.
- [Limits and KYC tiers](limits.html): fees count towards limits.
- [House accounts](house-accounts.html): `@FeeIncome` and the rest of the chart of accounts.
- [Deposits](deposits.html), [Withdrawals](withdrawals.html), [Transfers](transfers.html), [Currency exchange](currency-exchange.html), [Bulk payouts](bulk-payouts.html).
