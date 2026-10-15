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

Requires Go 1.22 or later. There are no third-party dependencies; please keep
it that way unless there is a strong reason.

## Guidelines

- **Money is never a float.** Use `Money`, `big.Int` and `big.Rat`.
- **Every movement needs a test**: a flow test against `ledgertest`, plus a
  request-body test when the change affects what is sent to Blnk.
- **Blnk behaviour changes go in the emulator too**, so `ledgertest` keeps
  matching real Core. If you observed something on a real Core, say which
  version in the pull request.
- **Documented samples live in** `ledger/docs_examples_test.go`. After
  changing a sample or any exported API, run `make docs` and commit
  `docs.html` and `CONTRACT.md` (CI checks they are current).
- **New exported names in `ledger`** must be re-exported in `humanblnk.go`;
  `TestReexportsEverything` tells you which.
- **Each namespace acts on one kind of object.** `Ledgers()`, `Accounts()`
  and `System()` load what they work on through `loadLedger`/`ledgerFor`,
  `loadAccount` and `loadSystem` (`ledger/guard.go`), which refuse ids of
  another kind; new methods must do the same.
- Keep exported names in product language (`Deposit`, `Confirm`) and system
  accounts in accounting language (`@FeeIncome`).

## Releases

Maintainers tag releases as `vX.Y.Z` and record changes in `CHANGELOG.md`.
