# Contributing

Thanks for helping improve humanblnk. Issues and pull requests are welcome.

## Before you start

For anything bigger than a small fix, open an issue first describing the use
case in product terms ("refund a split payment to two buyers"), so we can
agree on the API before you write it.

## Development

```bash
make check   # gofmt, go vet, staticcheck, tests with -race (what CI runs)
make docs    # regenerate docs.html and CONTRACT.md after changing the API or a sample
make live    # tests against a real Blnk Core (writes test data)
```

Requires Go 1.27 or later. There are no third-party dependencies; please keep
it that way unless there is a strong reason.

The library supports Blnk Core 0.15.4 and later. CI's live job builds Core
0.15.4 and 0.15.6 from source and runs `TestLiveCore*` against each; run
`make live BLNK_LIVE_URL=…` against your own Core before changing anything
that talks to Blnk.

## Guidelines

- **Money is never a float.** Use `Money`, `big.Int` and `big.Rat`.
- **Every movement needs a test**: a flow test against `ledgertest`, plus a
  request-body test when the change affects what is sent to Blnk.
- **Blnk behaviour changes go in the emulator too**, so `ledgertest` keeps
  matching real Core. If you observed something on a real Core, say which
  version in the pull request.
- **Documented samples live in** `ledger/docs_examples_test.go` (the guide)
  and `examples/source/` (the example pages). After changing a sample or any
  exported API, run `make docs` and commit `docs.html`, `CONTRACT.md` and
  `examples/` (CI checks they are current).
- **An example page** is `examples/source/<page>.md` plus the samples in
  `examples/source/<page>_test.go`. A sample sits between
  `// sample: <page>/<name>` and `// end`, wrapped in `begin(...)` and
  `end()`; what it prints is shown as its output. `@sample <name>` in the
  Markdown places it on the page. See `scripts/genexamples.py` for the
  Markdown the pages accept.
- **New exported names in `ledger`** must be re-exported in `humanblnk.go`;
  `TestReexportsEverything` tells you which.
- **Each method acts on one kind of object.** Ledger, customer-account and
  house-account methods load what they work on through `loadLedger`/`ledgerFor`,
  `loadAccount` and `loadHouse` (`ledger/core_guard.go`), which refuse ids of
  another kind; new methods must do the same.
- **File prefixes name the domain.** In `ledger/`, a method on the `Wallet`
  handle lives in a `wallet_*.go` file, a method on `Ops` in an `ops_*.go`
  file, and code both share in `core_*.go`. `blnk/` has one file per Blnk Core
  resource, and `ledgertest/` uses the same names for its fakes.
- Keep exported names in product language (`Deposit`, `Confirm`) and house
  accounts in accounting language (`@FeeIncome`).

## Releases

Maintainers tag releases as `vX.Y.Z` and record changes in `CHANGELOG.md`.
