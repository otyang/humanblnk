# currency

Every currency humanblnk can hold: the ISO 4217 currencies in circulation and
the crypto assets it supports, each with how many decimal places its
smallest unit has.

```go
c, ok := currency.Get(" ngn ")    // user input: NGN, true
c.Code(), c.Name(), c.Decimals()   // "NGN", "Naira", 2
c, ok = currency.Lookup(row.Code)  // stored or received: exact codes only; "ngn" is refused
currency.MustGet(currency.BTC)     // a code known to exist; Decimals() is 8
currency.All(currency.Crypto)      // AVAX, BNB, BTC, …, USDC, USDT: ordered by code
```

- **The list is closed.** A code that is not on it is refused, never given a
  guessed precision; `Add` is the only way onto it at run time. ISO fund
  codes, precious metals and testing codes
  (`XAU`, `XDR`, `XTS`, …) are left out.
- **Clean input once, at the edge.** `Get` ignores case and surrounding
  space, for codes people type. Everywhere else, such as codes read from
  the database or another service, use `Lookup`, which takes only the exact
  code: a stored `"usd "` is a bug to catch, not to accept, since it would
  not equal `"USD"` in comparisons, map keys or SQL.
- **A `Currency` comes only from the list.** Its fields are unexported, so
  one with made-up decimals cannot be built; the zero `Currency` is the only
  other value, and it is not `Valid`.
- **No dependencies.** Code that only needs currencies (config, accounts,
  validation) imports this package alone, without `money`'s decimal maths.
- **Every code is a constant** (`currency.USD`), so a misspelt code does
  not compile.
- **Decimals never change** once money has moved in a currency: the ledger
  stores amounts in the smallest unit, so a change would rescale every
  balance. Crypto assets use their on-chain decimals (ETH 18, BTC 8, USDT
  and USDC 6).

## Adding your own currency at run time

```go
c, err := currency.Add(currency.Spec{
	Code:     "ACMEPTS",
	Name:     "Acme Point",
	Kind:     currency.Crypto,
	Decimals: 2,
})
```

- It lasts only as long as the process: call `Add` at startup, from a
  source that outlives it (the database, config), on every start.
- A code already on the list is refused with `ErrExists`, never replaced,
  so `Add(Spec{Code: "USD", Decimals: 0, …})` cannot change USD's decimals.
- `Spec` is only the request; the `Currency` that `Add` returns is the
  currency, and still cannot be built by hand.
- The code must match `^[A-Z][A-Z0-9]{1,9}$`, and decimals be 0 to 18.
- There is no constant for it (`currency.HUMANBLNK` does not exist); use the
  returned `Currency` or `Lookup("HUMANBLNK")`.

## Changing the list

The list is generated. Edit `currencies.csv` (code, name, `fiat` or
`crypto`, decimals), then run:

```sh
go generate ./money/currency   # rewrites list.go
```

`TestListMatchesTheCSV` fails when `list.go` was not regenerated, and
`TestFiatCodesAreISO4217` when a fiat code is not an ISO 4217 code.

| File | Contents |
| --- | --- |
| `currency.go` | `Currency` (with `Code`, `Name`, `Kind`, `Decimals`, `Valid`, `String`), `Kind` (`Fiat`, `Crypto`), `Add`, `Spec`, `ErrExists`, `Lookup`, `Get`, `MustGet`, `All` |
| `list.go` | Generated: the constants and the table |
| `currencies.csv` | The source list |
| `gen/` | The generator (`go generate`) |
