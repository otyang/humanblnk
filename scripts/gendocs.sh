#!/bin/sh
# Regenerates currency/list.go, docs.html, CONTRACT.md and
# examples/: builds the currency list from scripts/currencies.csv, runs the documented samples
# (which also writes the statement CSV they show), builds the guide from them,
# rebuilds the contract from the Go source, and builds the example pages from
# examples/source.
set -e
cd "$(dirname "$0")/.."
python3 scripts/gencurrencies.py .
gofmt -w currency/list.go
csv=$(mktemp)
DOCS_CSV_OUT="$csv" go test ./ledger -run 'TestDocsSamples|TestRentTransfer' -count=1
python3 scripts/gendocs.py ledger/docs_examples_test.go docs.html "$csv"
python3 scripts/gencontract.py .
python3 scripts/genexamples.py
rm -f "$csv"
