title: Money and amounts
group: setup
order: 2
lead: Pass amounts as the decimal the user typed. Every amount comes back exact, in both forms Blnk stores, with a display string for people.
---
## The situation

Ada types "50,000" to top up in naira, "0.00012345" to buy bitcoin, and "20.5" to send USDT. Oja must store each one exactly, never as a float, and show it back in a form she can read and spend. `ledger.Money` carries all of that.

## The three forms of an amount

| Field | Example for 0.456 BTC | What it is |
|---|---|---|
| `Value` | `"0.456"` | the human decimal, trailing zeros trimmed |
| `PreciseAmount` | `45600000` | the integer in the smallest unit, a `*big.Int`. Blnk sums this on balances |
| `Precision` | `100000000` | the multiplier: `Value` × `Precision` = `PreciseAmount` |
| `Display` | `"0.456"` | for people to read, set on every `Money` the library returns, including from `Parse`, `Precise` and `ParseDecimal`. Never compute with it |

Only `precise_amount` and `precision` are sent to Blnk, never its float `amount`, so `0.456` can't turn into `0.45599999`.

## Which one to use

| You have | Use |
|---|---|
| What the user typed | `ledger.Amount("1,500.50", currency.NGN)` |
| The same, resolved now so you can read `PreciseAmount` | `svc.Parse("1500.50", currency.NGN)` |
| An integer in the smallest unit, e.g. from a provider's webhook in kobo | `ledger.Minor(1050075, currency.NGN)` |
| An integer and its precision | `ledger.Precise(big.NewInt(45600000), 100000000, currency.BTC)` |
| A decimal and a precision of your choice, without a `Service` | `ledger.ParseDecimal("12.3456", "OJA", 10000)` |
| A JSON body from your app's client | `json.Unmarshal` into a `ledger.Money` |

## Amount and Parse

`Amount` keeps the text and upper-cases the currency. It doesn't resolve anything, so `PreciseAmount` is still nil. Every call that takes a `Money` resolves it with the currency's precision. `svc.Parse` does the same now, so you can read both forms and `Display`.

@sample amount

- `String()` gives `"0.456 BTC"`. `Fixed()` gives every decimal place the precision allows.
- `IsZero()`, `Sign()` and `IsPositive()` compare without floats. `Sign` and `IsPositive` need a resolved amount.

### Commas, zeros and precision by currency

@sample currencies

- Commas and underscores are dropped, so `"1,500.50"` is `150050` kobo.
- `JPY` has no decimals and `KWD` has three: the precision follows the currency, not a fixed two places.

## Integers you already hold

@sample other-forms

`Minor` leaves `Precision` and `Display` empty, and the call fills them from the currency. `Precise` and `ParseDecimal` set everything up front. Their `Display` follows the built-in rule for the currency, because they have no `Service` to read `Config.Currencies` from. A `Money` that carries a different precision from the currency's is converted with its value kept, or refused if it has more decimals than the currency allows. It is never posted at the wrong scale.

## Rejected amounts

@sample rejected

More decimal places than the currency allows, exponents, anything that isn't a number and an empty amount fail with `ErrInvalidInput` before anything is sent to Blnk. Nothing is rounded for you.

## Fees: an empty currency

A fee is always in the transaction's currency. Write it with an empty currency and it takes the amount's.

@sample fee | Paystack keeps ₦750 of Ada's ₦50,000

- `ledger.NoFee` is a zero fee. Use it for a free transaction when `Config.Fees` isn't set.
- A fee in another currency is refused. For an exchange, the fee is in the sell currency. See [Fees](fees.html).
- The deposit records the gross amount; `meta_data.fee` and `meta_data.net_amount` hold the split.

### What happens in Blnk

| Account | Debit | Credit |
|---|---|---|
| `@PaystackClearing` | ₦50,000 | |
| Ada's NGN account | | ₦49,250 |
| `@FeeIncome` | | ₦750 |

## JSON

`Money` marshals to both forms plus the display string. `precise_amount` is a string so JavaScript clients don't lose digits on 18-decimal assets.

@sample json | Out to your app, and back in from a client

- On the way in, `amount` may be a string (`"0.1"`) or a bare number (`0.1`). A number is read from its literal text, never through `float64`.
- `currency` is upper-cased. `precise_amount` and `precision` are optional. When both `amount` and `precise_amount` are given, they must agree.
- `display` is ignored on the way in.

## Currencies: package currency

humanblnk knows 179 currencies without any configuration: every current ISO 4217 code, plus BTC, ETH, SOL, TRX, LTC, BCH, XRP, ADA, DOGE, BNB, POL and the stablecoins USDT, USDC and DAI. They live in package `currency`, one constant per code:

```go
import "github.com/otyang/humanblnk/currency"

currency.NGN // "NGN": Nigerian Naira · ₦ · 2 decimals
currency.BTC // "BTC": Bitcoin · ₿ · 8 decimals
currency.ETH // "ETH": Ether · Ξ · 18 decimals, shown to 6
```

The constants are plain strings, so they go anywhere humanblnk takes a currency (`ledger.Amount("500", currency.NGN)`), and a typo such as `currency.NGM` is a compile error. `currency.Get` returns the details:

@sample package | Naira and bitcoin, and the stablecoins

| `currency.Currency` field | What it is |
|---|---|
| `Code` | `"NGN"` |
| `Name` | `"Nigerian Naira"` |
| `Kind` | `currency.Fiat`, `currency.Crypto` or `currency.Stablecoin` |
| `Symbol` | `"₦"`. Every symbol is unique: AUD is `A$`, CAD `CA$`; a currency with no symbol of its own uses its code |
| `Precision` | smallest units per whole unit: 100 for NGN, 1 for JPY, 100000000 for BTC. `Decimals()` gives the places: 2, 0, 8 |
| `Display` | how amounts are shown (below) |

- Fiat precisions are the ISO 4217 minor units. Crypto precisions are the chain's smallest unit: satoshi for BTC, wei (10¹⁸) for ETH.
- `Get` and `All` return copies, so nothing can change the built-in list.
- The fields have JSON tags, ready for an API that lists currencies:

@sample currency-json

## Display

`Display` is cut, never rounded up, so a displayed balance can always be spent in full. The exact amount stays in `Value`.

| Currency | Rule | Example |
|---|---|---|
| Fiat | every decimal place of its precision, padded | `100.50` USD, `5000` JPY, `1.250` KWD |
| `USDT`, `USDC`, `DAI` | 2 places, padded | `20.50` |
| `BTC`, `LTC`, `BCH`, `DOGE` | up to 8 places, trailing zeros dropped | `0.5` |
| `ETH`, `BNB`, `POL`, `TRX`, `XRP`, `ADA` | up to 6 places, trailing zeros dropped | `0.123456` |
| `SOL` | up to 4 places, trailing zeros dropped | `1.25` |

Change any of them with `Config.Currencies`. Each entry replaces the whole currency, so start from the built-in one with `currency.MustGet` and change what you need with `WithDisplay`, `WithName` or `WithSymbol`:

@sample display | Oja shows whole naira and 4 places of ETH

| `currency.Display` field | What it changes |
|---|---|
| `Decimals` | the most decimal places shown, capped at the currency's precision |
| `TrimZeros` | drop trailing zeros instead of padding to `Decimals` |

`svc.Display(m)` formats any resolved `Money` by its currency's rule, and `svc.DisplayRule(code)` says which rule applies. `m.Format(rule)` uses any rule you like, and a rule also formats smallest units on their own:

@sample format

## Adding your own: Config.Currencies

`currency.New` builds a currency that isn't built in, from its code, name, kind, symbol and precision. The precision must be a power of 10. It is shown with every decimal place, padded for fiat and trimmed otherwise.

@sample precisions | Oja adds its own loyalty points, OJA, at 4 places, and renames USDT

> warn: Keep a currency's precision fixed once money has moved. Blnk sums `precise_amount` on balances, so mixing precisions corrupts the totals.

Each account records the precision it was opened with (`meta_data.precision`). Its balances are always read at that precision, so they stay right if the configuration changes later. Money is never posted to it at another one: a `Service` configured with a different precision for its currency refuses every movement on it with `ErrInvalidInput`, and the message names the precision to set back. Accounts opened after the change use the new precision.

## What your service uses: svc.Currency

`currency.Get` is the built-in list. `svc.Currency` and `svc.Currencies` are what your service actually uses, with `Config.Currencies` applied. Read names, symbols and rules from the Service when you show balances, so your overrides are respected. Balances don't carry the name or symbol: look them up by `acct.Currency`.

@sample lookup | Show a balance with its symbol and name

## Unknown currencies

A code that is neither built in nor in `Config.Currencies` is refused with `ErrInvalidInput` when opening accounts, parsing amounts and moving money, so a typo can't open an account. Set `Config.UnknownCurrency: ledger.AllowUnknown` to accept any code at `Config.DefaultPrecision` (default 100). `DefaultPrecision` applies only to such codes, never to a built-in one.

An account opened earlier in a code that is no longer configured keeps working for calls that take no amount: `Refund`, `Confirm`, `Cancel` and the sweep when closing it. Anything that takes an amount needs the currency configured again.

@sample unknown

## Configuration mistakes

Mistakes in `Config.Currencies` don't stop `ledger.New`, but a currency whose entry has a mistake is refused everywhere, with the mistake as the error, until it is fixed. It is never quietly run on the built-in entry, whose precision may be exactly what the entry meant to change. `svc.Check` reports every mistake, so call it at start-up. `Kind` may be written in any case.

@sample config-errors

## Errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | more decimals than the currency allows; exponent notation; not a number; a fee in another currency; a negative fee; a zero or negative amount; an empty amount, also in `svc.Parse` and `ParseDecimal`; `Value` and `PreciseAmount` disagree | show the user the allowed format, e.g. two decimal places for NGN |
| `ErrInvalidInput` | an unknown currency (see above) | add it to `Config.Currencies`, or fix the typo |
| `ErrInvalidInput` | a precision that isn't a power of 10 | fix `Config.Currencies` or the `Precise` / `ParseDecimal` call |
| `ErrInvalidInput` from `Check` | a mistake in `Config.Currencies` | fix the entries the message names |

## Related

- [Fees](fees.html): price fees with `Config.Fees` and `ComputeFee`.
- [Currency exchange](currency-exchange.html): rates and rounding between currencies.
- [Install and connect](setup.html): every `Config` field.
