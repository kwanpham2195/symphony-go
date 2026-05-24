# Contributing

## Development

```bash
git clone https://github.com/kwanpham2195/symphony-go.git
cd symphony-go
make check   # lint + test with -race
```

## Git Flow

We use **GitHub Flow**: feature branches off `main`, squash-merge PRs.

```
main ──────●──────●──────●──
            \          /
             ●───●───●
             CFW-44/assignee-routing
```

### Branch naming

`<issue-key>/<short-description>` — e.g., `CFW-44/assignee-routing`

### Workflow

1. Create a branch from `main`
2. Make changes, run `make check`
3. Push and open a PR against `main`
4. CI must pass + 1 review required
5. Squash-merge (only merge method allowed)
6. Branch auto-deletes after merge

### Branch protection on `main`

- Squash merge only (no merge commits, no rebase)
- CI `test` job must pass
- Branch must be up to date with `main`
- 1 approval required
- No force push, no branch deletion

## Before Submitting

```bash
make check   # runs: golangci-lint + go test -race
```

This matches CI exactly. Do not open a PR until this passes locally.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(orchestrator): add per-state concurrency limits
fix(codex): handle empty tool name in item/tool/call
refactor(config): extract env resolution helpers
docs: update smoke test guide
test(workspace): add symlink escape test
chore: go mod tidy
ci: add golangci-lint to workflow
build: add goreleaser config
```

Scope is optional but encouraged — use the package name.

## Testing

- Unit tests live next to the code (`*_test.go`).
- Use `t.TempDir()` for filesystem tests.
- Use interface fakes for orchestrator tests, not mocks.
- Use `testdata/` fixtures for Linear client and codex protocol tests.
- Run with `-race` to catch data races.
- Add a regression test when fixing a bug.

## Code Style

- Keep files under ~500 lines. Split when they grow.
- The orchestrator depends on interfaces, not concrete types.
- No global state. Pass dependencies explicitly.
- `golangci-lint` enforces: errcheck, govet, staticcheck, unused, gocritic, misspell, errorlint, gofmt.

## Codex Skills

If you're working with Codex agents in this repo, 6 skills are available in `.codex/skills/`:

| Skill | When to use |
|-------|-------------|
| `commit` | Creating commits with conventional format |
| `push` | Pushing branch and creating/updating PR |
| `pull` | Syncing with origin/main, resolving conflicts |
| `land` | Shepherding a PR to merge (CI watch, squash-merge) |
| `linear` | Linear GraphQL operations via `linear_graphql` tool |
| `debug` | Investigating stuck or failing symphony runs |

## Releases

Releases are automated via GoReleaser. To publish:

```bash
git tag v0.2.0
git push origin v0.2.0
```

CI builds cross-compiled binaries and publishes to [GitHub Releases](https://github.com/kwanpham2195/symphony-go/releases).
