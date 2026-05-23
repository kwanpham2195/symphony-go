# Contributing

## Development

```bash
git clone https://github.com/kwanpham2195/symphony-go.git
cd symphony-go
go test ./...
```

## Before Submitting

Run the full gate:

```bash
go build ./...
go vet ./...
go test -race ./...
```

All three must pass. CI runs the same checks with `-race`.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new feature
fix: correct bug
refactor: restructure code
docs: update documentation
chore: maintenance task
test: add or fix tests
ci: CI/CD changes
```

## Testing

- Unit tests live next to the code they test (`*_test.go`).
- Use `t.TempDir()` for filesystem tests.
- Use interface fakes for orchestrator tests, not mocks.
- Use `testdata/` fixtures for Linear client and codex protocol tests.
- Run with `-race` to catch data races.

## Code Style

- Keep files under ~500 lines. Split when they grow.
- The orchestrator depends on interfaces, not concrete types.
- No global state. Pass dependencies explicitly.
