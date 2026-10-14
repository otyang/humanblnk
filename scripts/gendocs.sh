#!/bin/sh
# Regenerates docs.html and CONTRACT.md: runs the documented samples (which
# also writes the statement CSV they show), builds the guide from them, and
# rebuilds the contract from the Go source.
set -e
cd "$(dirname "$0")/.."
csv=$(mktemp)
DOCS_CSV_OUT="$csv" go test ./ledger -run 'TestDocsSamples|TestRentTransfer' -count=1
python3 scripts/gendocs.py ledger/docs_examples_test.go docs.html "$csv"
python3 scripts/gencontract.py .
rm -f "$csv"
