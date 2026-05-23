.PHONY: build test test-race test-short test-acceptance vet lint check clean validate run

# Build the symphony binary
build:
	go build -o symphony ./cmd/symphony

# Run all tests
test:
	go test ./...

# Run all tests with race detector
test-race:
	go test -race -count=1 ./...

# Run unit tests only (skip acceptance)
test-short:
	go test -short ./...

# Run acceptance tests only
test-acceptance:
	go test -v -run TestAcceptance -timeout 60s .

# Run go vet
vet:
	go vet ./...

# Lint + vet (add staticcheck/golangci-lint here when available)
lint: vet

# Full CI gate: vet + race tests (matches .github/workflows/ci.yml)
check: vet test-race

# Remove build artifacts
clean:
	rm -f symphony

# Validate a workflow file (default: WORKFLOW.md)
validate:
	go run ./cmd/symphony --validate-only $(WORKFLOW)

# Run the orchestrator (default: WORKFLOW.md)
run:
	go run ./cmd/symphony $(ARGS)

# Run a single poll cycle then exit
run-once:
	go run ./cmd/symphony --once $(ARGS)
