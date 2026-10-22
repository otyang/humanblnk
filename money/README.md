# money

An exact amount in one currency ([money/currency](currency/README.md)).
Never a float: amounts are parsed from the decimal a person or client
wrote, kept as a `decimal.Decimal`, and sent as a string.

`money` imports `money/currency`, never the other way round. Every
constructor takes a `currency.Currency`, so the caller looks the code up
once and chooses how strictly: `currency.Get` for what people type,
`currency.Lookup` for codes from storage and other services.

```go
usd, ok := currency.Get(form.Currency)        // user input; Lookup for stored codes
m, err := money.Parse("100.50", usd)
m.String()                                    // "100.50": exactly the currency's decimals
minor, err := m.Minor()                       // 10050 (m.Precision() is 100): what a ledger stores
fee, err := m.Percent(decimal.RequireFromString("1.5"), money.Up) // 1.5% of m: "1.51"
sum, err := m.Add(fee)                        // same currency only, else ErrCurrencyMismatch
m.Equal(sum)                                  // never ==: Money does not compile with == or as a map key
```

| Rule | |
| --- | --- |
| Parsing | A plain decimal (`"100.5"`, `"-3"`). Exponents, a plus sign, separators and `".5"` are `ErrInvalidAmount`. More decimal places than the currency has are `ErrTooManyDecimals` (`1.005` USD), though trailing zeros are fine (`1500.00` JPY). The zero `currency.Currency` is `ErrUnknownCurrency`. |
| Range | At most 40 digits before the point, and written with at most 40 after it (enough for any uint256 count of an 18-decimal token). `New`, `Round`, `Percent`, `FromMinor`, `Add` and `Sub` refuse anything past that with `ErrOutOfRange`, quickly even for a decimal such as `1e100000000`, so every `Money` marshals and parses back. |
| Arithmetic | `Add`, `Sub`, `Cmp` only within one currency (`ErrCurrencyMismatch`), and never on the zero `Money` (`ErrUnknownCurrency`). `Equal` compares value and currency (`1.5` and `1.50` USD are equal) and, like `Cmp`, never holds for the zero `Money`. `IsZeroAmount` is whether the amount is 0; it is not named `IsZero`, so an `omitzero` JSON tag leaves out only the zero `Money`, never a balance of 0.00. |
| Smallest unit | `Minor` is the exact integer amount (`*big.Int`, so 18-decimal crypto fits), and `ErrUnknownCurrency` for the zero `Money`, so one never set is not stored as 0; `Precision` is 10^decimals; `FromMinor` goes back. |
| Rounding | Only where asked: `Round(decimal, currency, mode)` and `Percent(p, mode)`. A fee HumanBlnk charges rounds `Up`; an amount paid out or credited rounds `Down`; `HalfEven` is for reports. The zero `RoundingMode` is none of them: an unset or unknown mode is `ErrUnknownRoundingMode`, never a quiet `Down`. |
| JSON | `{"amount": "100.50", "currency": "USD"}`, exact: the keys exactly `amount` and `currency`, each once and nothing else (`encoding/json` would take `"AMOUNT"` and the last of a repeated key), the amount a string with no surrounding space, the code as `currency.Lookup` takes it (`"usd"` is refused). The zero `Money` (no currency, not `Valid`) is `null`, both ways. |

`String()` is the bare amount (`"3746.38"`), for logs and storage. Showing
an amount to a person (`"$3,746.38"`, `"3.746,38 €"`) is not this
library's job: apps format the JSON amount on the device, and a service
that writes emails or receipts formats it there.

| File | Contents |
| --- | --- |
| `money.go` | `Money`, the errors, `Parse`, `MustParse`, `New`, `Zero`, arithmetic, JSON |
| `money_minor.go` | `Minor`, `Precision` (method and function), `FromMinor` |
| `money_round.go` | `RoundingMode` (`Down`, `Up`, `HalfEven`), `Round`, `Percent` |
