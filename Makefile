.PHONY: check test lint fmt docs live example

check: lint test ## Everything CI runs

test: ## Tests with the race detector
	go test -race -count=1 ./...

lint: ## gofmt, go vet and staticcheck
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt':"; gofmt -l .; exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

fmt: ## Format the code
	gofmt -w .

docs: ## Regenerate docs.html from ledger/docs_examples_test.go
	./scripts/gendocs.sh

live: ## Tests against a real Blnk Core (writes test data): make live BLNK_LIVE_URL=http://localhost:5001
	BLNK_LIVE_URL=$${BLNK_LIVE_URL:-http://localhost:5001} go test ./ledger -run TestLiveCore -count=1 -v

example: ## Walkthrough against the in-memory emulator (set BLNK_URL for a real Core)
	go run ./cmd/example
