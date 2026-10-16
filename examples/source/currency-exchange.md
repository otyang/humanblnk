title: Currency exchange
group: wallet
order: 7
lead: Convert money between a customer's accounts in two currencies at your rate, with exact rounding, an optional fee, and both sides posted together or not at all.
---
## The situation

Ada has dollars from freelance work and wants naira. Oja quotes her ₦1,532.40 per $1. `wallet.Exchange` takes the dollars from her USD account into Oja's FX pool and pays the naira out of the pool into her NGN account. Both legs post in one atomic Blnk batch: she never ends up with one side and not the other.

Pick the input by what the customer fixes:

| The customer says | Use |
|---|---|
| "Convert $100" | `Sell: ledger.Amount("100", "USD")`; the naira is worked out |
| "I need exactly $25" | `Buy: ledger.Amount("25", "USD")`; the naira to sell is worked out |
| "What would I get?" | `wallet.QuoteExchange` with the same input, see [Quotes](quotes.html) |
| Money to another person in another currency | an exchange into your own account, then a [transfer](transfers.html) |

Set exactly one of `Sell` and `Buy`.

## Sell a fixed amount

@sample sell | Ada sells $100 at ₦1,532.40

- `Rate` is units of the buy currency per 1 unit of the sell currency, as a decimal string. Here: naira per dollar.
- The fee comes from `Config.Fees` with `Kind: ledger.FeeExchange`, the sold amount and `BuyCurrency`. It is in the sell currency and charged on top: Ada pays $101.
- The receipt has both legs. `Debit` is the leg out of Ada's USD account, with reference `fx_01J8ZQ0T5C_sell`; `Credit` is the leg into her NGN account, `fx_01J8ZQ0T5C_buy`.
- Both legs carry `fx_pair`, `fx_rate`, `fx_reference`, `fx_sell` and `fx_buy` in their metadata.

## Buy a fixed amount

@sample buy | Tunde needs exactly $25

- The rate is still buy per sell: dollars per naira here, because Tunde sells naira.
- Oja's pricing charges nothing for selling naira, so `Fee` is zero. Set `Fee` on the input for a fixed fee, or `ledger.NoFee` for a free one.

## Rounding

Amounts are worked out in exact fractions, then rounded once to the currency's smallest unit. Rounding never costs the house:

- with `Sell`, the amount bought rounds **down**;
- with `Buy`, the amount sold rounds **up**.

@sample rounding

An amount so small that one side rounds to zero is refused with `ErrInvalidInput`.

## The FX pool: Oja's position

Every exchange passes through `@FXPool`, one balance per currency. Each balance is Oja's open position in that currency.

@sample pool | After both exchanges

- A positive pool balance means more of that currency came in from customers than went out. A negative one means Oja paid out more than it took in.
- Here Oja took in $75 more than it paid out, and paid out ₦114,896.44 more than it took in. Closing that position with a bank or liquidity provider happens outside this call; watch it with [Reports](reports.html) or [Balance alerts](balance-alerts.html).
- `Pool` on the input uses another house account instead, e.g. one pool per liquidity provider.
- The $1 fee went to `@FeeIncome` in USD.

## Retries

@sample replay

- The same input with the same reference returns both original legs with `Replayed: true`.
- A new rate is a new exchange. With the old reference it is `ErrDuplicateReference`, and nothing moves.

## Options

| Field | What it changes |
|---|---|
| `From` | customer account debited, in the sell currency |
| `To` | customer account credited, in the buy currency |
| `Sell` | how much leaves `From`; set this or `Buy` |
| `Buy` | how much must arrive in `To`; set this or `Sell` |
| `Rate` | buy units per 1 sell unit; a positive decimal string |
| `Fee` | in the sell currency, on top; required unless `Config.Fees` is set |
| `Reference` | required; the legs are `Reference` + `_sell` and `Reference` + `_buy` |
| `Pool` | the house account in the middle; default `@FXPool` |
| `Description` | on both legs; default "Exchange USD to NGN" |
| `MetaData` | your own keys, added to both legs |

## What happens in Blnk

Two transactions in one atomic bulk request, both `txn_type: exchange`. For Ada's $100:

| Leg | Account | Debit | Credit |
|---|---|---|---|
| `_sell` | Ada's USD account | $101.00 | |
| | `@FXPool` (USD) | | $100.00 |
| | `@FeeIncome` (USD) | | $1.00 |
| `_buy` | `@FXPool` (NGN) | ₦153,240.00 | |
| | Ada's NGN account | | ₦153,240.00 |

The `_buy` leg has `allow_overdraft` set, so the pool can pay out before it holds that currency. Limits apply to the `_sell` leg, as for any money leaving a customer account.

## Errors

@sample errors

| Error | When | What to do |
|---|---|---|
| `ErrInsufficientFunds` | `From` can't cover the sell amount plus fee | show the balance |
| `ErrLimitExceeded` | the sell leg is over `From`'s limit | see [Limits](limits.html) |
| `ErrInvalidInput` | both or neither of `Sell` and `Buy`; a bad rate; both accounts in one currency; an amount that rounds to zero; an amount in the wrong currency; no `Reference` | fix the input |
| `ErrWrongKind` | `From` or `To` is a house account | pass customer accounts |
| `ErrAccountFrozen` / `ErrAccountClosed` | either account is frozen or closed | stop the exchange |
| `ErrLedgerFrozen` | `From`'s ledger is frozen | wait until operations unfreezes it |
| `ErrDuplicateReference` | the reference was used for a different exchange | give each new rate or amount a new reference |

## Related

- [Quotes for confirm screens](quotes.html): `QuoteExchange` shows the fee, both amounts and the balance after.
- [Fees](fees.html): pricing exchanges, and `FeeRequest.Convert` for a fee set in another currency.
- [Money and precision](money.html): how amounts are stored.
- [House accounts](house-accounts.html) and [Reports](reports.html): reading the pool.
