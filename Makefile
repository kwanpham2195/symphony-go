.PHONY: build test test-race test-unit vet lint check clean validate run run-once release-dry

# Build the symphony binary
build:
	go build -o symphony ./cmd/symphony

# Run all tests
test:
	go test ./...

# Run all tests with race detector (matches CI)
test-race:
	go test -race -count=1 ./...

# Run unit tests only (no acceptance tests that need network)
test-unit:
	go test -race -count=1 ./cmd/... ./internal/...

# Run go vet
vet:
	go vet ./...

# Run golangci-lint
lint:
	golangci-lint run ./...

# Full CI gate: lint + race tests
check: lint test-race

# Remove build artifacts
clean:
	rm -f symphony
	rm -rf dist/

# Validate a workflow file (pass WORKFLOW=path)
# Example: make validate WORKFLOW=./testdata/workflows/minimal.md
validate:
	go run ./cmd/symphony --validate-only $(WORKFLOW)

# Run the orchestrator (pass ARGS for flags and workflow path)
# Example: make run ARGS="--port 8080 ./WORKFLOW.md"
run:
	go run ./cmd/symphony $(ARGS)

# Run a single poll cycle then exit
# Example: make run-once ARGS=./WORKFLOW.md
run-once:
	go run ./cmd/symphony --once $(ARGS)

# Dry-run goreleaser to verify config without publishing
release-dry:
	goreleaser release --snapshot --clean
