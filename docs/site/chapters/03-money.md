# Money

**Scenario:** a price of ₦1,500.50, a 1.5% fee, and the total that goes into
the ledger and out as JSON.

`money.Money` is an exact amount in one currency. It is never a float:
amounts are parsed from the decimal someone wrote and kept exact. Every
amount you give the ledger is a `money.Money`. This chapter needs no Blnk.

## Currencies

{{code 03-money/main.go currency}}

## Parsing

Parsing is strict, so a bad amount is caught at the edge of your system.

{{code 03-money/main.go parse}}

| Input | Result |
| --- | --- |
| `"1500.50"`, `"-3"`, `"1500"` | fine |
| `"1500.505"` | `ErrTooManyDecimals`: NGN has 2 |
| `"1e3"`, `"1,500"`, `".5"`, `"+3"` | `ErrInvalidAmount` |

`money.MustParse` panics instead of returning an error. Use it only for
constants in your own code, never for what a client sent.

## Arithmetic

{{code 03-money/main.go arith}}

- `Add`, `Sub` and `Cmp` work only within one currency; anything else is
  `money.ErrCurrencyMismatch`.
- Compare with `Equal` or `Cmp`, **never `==`**. `1.5` and `1.50` USD are
  `Equal`.
- Also: `IsPositive`, `IsNegative`, `IsZeroAmount`, `Sign`, `Neg`, `Abs`,
  `Amount` (the decimal), `Code`, `Currency`, `Valid`.

## The smallest unit

Ledgers store integers: kobo, cents, wei. `Minor` and `FromMinor` convert
exactly, even for 18-decimal crypto.

{{code 03-money/main.go minor}}

## Rounding: only where you ask

Nothing rounds by itself. Where you need it, you choose the direction.

| Mode | Use for |
| --- | --- |
| `money.Up` | a fee HumanBlnk charges: never undercharge by a fraction of a kobo |
| `money.Down` | an amount paid out or credited: never pay out more than exists |
| `money.HalfEven` | reports |

{{code 03-money/main.go round}}

## JSON

The amount is a string, so nothing in between turns it into a float.
Decoding is strict: exactly `amount` and `currency`, with the code in
capitals.

{{code 03-money/main.go json}}

## The whole example

{{run 03-money}}
