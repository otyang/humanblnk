#!/bin/sh
# Regenerates docs.html: runs the documented samples (which also writes the
# statement CSV they show), then builds the page from them.
set -e
cd "$(dirname "$0")/.."
csv=$(mktemp)
DOCS_CSV_OUT="$csv" go test ./ledger -run 'TestDocsSamples|TestRentTransfer' -count=1
python3 scripts/gendocs.py ledger/docs_examples_test.go docs.html "$csv"
rm -f "$csv"
