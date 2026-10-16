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
| `Display` | `"0.456"` | for people to read, set on every `Money` the library returns. Never compute with it |

Only `precise_amount` and `precision` are sent to Blnk, never its float `amount`, so `0.456` can't turn into `0.45599999`.

## Which one to use

| You have | Use |
|---|---|
| What the user typed | `ledger.Amount("1,500.50", "NGN")` |
| The same, resolved now so you can read `PreciseAmount` | `svc.Parse("1500.50", "NGN")` |
| An integer in the smallest unit, e.g. from a provider's webhook in kobo | `ledger.Minor(1050075, "NGN")` |
| An integer and its precision | `ledger.Precise(big.NewInt(45600000), 100000000, "BTC")` |
| A decimal and a precision of your choice, without a `Service` | `ledger.ParseDecimal("12.3456", "OJA", 10000)` |
| A JSON body from your app's client | `json.Unmarshal` into a `ledger.Money` |

## Amount and Parse

`Amount` keeps the text and upper-cases the currency. It doesn't resolve anything, so `PreciseAmount` is still nil. Every call that takes a `Money` resolves it with the currency's precision. `svc.Parse` does the same now, so you can read both forms.

@sample amount

- `String()` gives `"0.456 BTC"`. `Fixed()` gives every decimal place the precision allows.
- `IsZero()`, `Sign()` and `IsPositive()` compare without floats. `Sign` and `IsPositive` need a resolved amount.

### Commas, zeros and precision by currency

@sample currencies

- Commas and underscores are dropped, so `"1,500.50"` is `150050` kobo.
- `JPY` has no decimals and `KWD` has three: the precision follows the currency, not a fixed two places.

## Integers you already hold

@sample other-forms

`Minor` leaves `Precision` empty, and the call fills it from the currency. `Precise` sets everything up front. A `Money` that carries a different precision from the currency's is converted with its value kept, or refused if it has more decimals than the currency allows. It is never posted at the wrong scale.

## Rejected amounts

@sample rejected

More decimal places than the currency allows, exponents and anything that isn't a number fail with `ErrInvalidInput` before anything is sent to Blnk. Nothing is rounded for you.

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

## Display

`Display` is cut, never rounded up, so a displayed balance can always be spent in full. The exact amount stays in `Value`.

| Currency | Rule | Example |
|---|---|---|
| Fiat not listed below | every decimal place of its precision, padded | `100.50` USD, `5000` JPY, `1.250` KWD |
| `USDT`, `USDC` | 2 places, padded: stablecoins read as money | `20.50` |
| `BTC`, `LTC`, `BCH` | up to 8 places, trailing zeros dropped | `0.5` |
| `ETH`, `TRX` | up to 6 places, trailing zeros dropped | `0.123456` |
| `SOL` | up to 4 places, trailing zeros dropped | `1.25` |

These are `ledger.BuiltinDisplay`. Change any currency with `Config.Display`, which takes a `DisplayRule` per currency.

@sample display | Oja shows whole naira and 4 places of ETH

| `DisplayRule` field | What it changes |
|---|---|
| `Decimals` | the most decimal places shown, capped at the currency's precision |
| `TrimZeros` | drop trailing zeros instead of padding to `Decimals` |

`svc.Display(m)` formats any resolved `Money`, and `svc.DisplayRule(currency)` says which rule applies: `Config.Display`, then `BuiltinDisplay`, then every decimal place, padded. A rule also works on its own:

@sample format

## Precision: Config.Precisions

Each currency has a precision, the number of smallest units in one whole unit.

| Currencies | Precision |
|---|---|
| `BTC`, `LTC`, `BCH` | 100000000 (8 places) |
| `ETH` | 1000000000000000000 (18 places) |
| `SOL` | 1000000000 (9 places) |
| `USDT`, `USDC`, `TRX` | 1000000 (6 places) |
| `JPY`, `KRW`, `XOF`, `XAF`, `UGX`, `RWF` | 1 (no decimals) |
| `KWD`, `BHD`, `OMR`, `TND` | 1000 (3 places) |
| everything else | `Config.DefaultPrecision`, default 100 |

These are `ledger.BuiltinPrecisions`. Add or override currencies with `Config.Precisions`. Each value must be a power of 10.

@sample precisions | Oja adds its own loyalty points, OJA, at 4 places

> warn: Keep a currency's precision fixed once money has moved. Blnk sums `precise_amount` on balances, so mixing precisions corrupts the totals.

## Errors

| Error | When | What to do |
|---|---|---|
| `ErrInvalidInput` | more decimals than the currency allows; exponent notation; not a number; a fee in another currency; a negative fee; a zero or negative amount; no amount; `Value` and `PreciseAmount` disagree | show the user the allowed format, e.g. two decimal places for NGN |
| `ErrInvalidInput` | a precision that isn't a power of 10 | fix `Config.Precisions` or the `Precise` / `ParseDecimal` call |

## Related

- [Fees](fees.html): price fees with `Config.Fees` and `ComputeFee`.
- [Currency exchange](currency-exchange.html): rates and rounding between currencies.
- [Install and connect](setup.html): every `Config` field.
