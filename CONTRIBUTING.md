# Contributing

You need Go 1.27.1+ and Docker.

## Layout

| Package | Contents |
| --- | --- |
| `humanblnk` | `Client`, `New`, the options, and every public name re-exported (`aliases.go`) |
| `internal/types` | The public types, errors and helpers, defined once |
| `internal/accounts`, `transactions`, `holds`, `watches`, `reconcile`, `ledger`, `identities` | One package per service |
| `internal/events` | Webhook verification and decoding, behind `VerifyWebhook` |
| `internal/core` | Shared and private: the Blnk client, conversion, the currency check, error mapping |
| `blnk`, `blnk/blnkfake` | The Blnk Core HTTP client, and a fake Blnk for unit tests |
| `money`, `money/currency` | Exact amounts and the currency list |
| `docs/site`, `docs/site/chapters`, `docs/examples` | The tutorial: its generator, its chapters, and the programs every snippet comes from |
| `deploy` | A local Blnk 0.15.6 (Docker Compose) and its config, for the integration tests and the tutorial |

A new public type goes in `internal/types` and gets an alias in `aliases.go`;
a new service gets a field on `Client` and a `…Service` alias. The reference
lives in `doc.go`: change it with the code.

## Tests

```sh
go test ./...                                           # against a fake Blnk (blnk/blnkfake)

docker compose -f deploy/docker-compose.yml up -d       # Blnk 0.15.6, its worker, Postgres, Redis
BLNK_URL=http://localhost:5001 go test -tags integration .
```

The integration tests check what the unit tests can only assume: how Blnk
stores splits, that funds errors are immediate, that a partial capture releases
the rest, and that concurrent reversals never exceed the entry.

CI runs gofmt, `go mod tidy -diff`, `go generate ./money/currency`, `go vet`
(with and without the `integration` tag), `go test -race ./...`, and the
integration tests against a real Blnk.

## The tutorial

Every line of Go on the site comes from `docs/examples`, which compile with
the rest of the module. After changing a chapter or an example, from the
repository root:

```sh
go run ./docs/site            # rebuild docs/*.html
go run ./docs/site -record    # also rerun every example against $BLNK_URL and save its output
```

`docs/site` has a test that fails if the committed HTML is out of date.
