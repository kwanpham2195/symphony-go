# AGENTS.md

Go implementation of [Symphony](https://github.com/openai/symphony) — a scheduler/runner that polls Linear, creates per-issue workspaces, and runs Codex app-server sessions.

## Build / Test

```bash
make check          # full CI gate: golangci-lint + go test -race (matches CI exactly)
make check-sandbox  # sandbox-safe: lint + unit tests only (no network-binding acceptance tests)
make build          # build binary
```

Run a single package:

```bash
go test ./internal/orchestrator/...
```

Validate a workflow file without starting the service:

```bash
LINEAR_API_KEY=... go run ./cmd/symphony --validate-only ./testdata/workflows/minimal.md
```

## Architecture

| Package | Owns |
|---------|------|
| `cmd/symphony` | CLI entry point. Wires all components. |
| `internal/*.go` | Shared types: `Issue`, `Workspace`, `Workflow`, `AgentUpdate`, etc. |
| `internal/config` | Typed config from WORKFLOW.md front matter. Defaults, `$VAR` env, `~` expansion, validation. |
| `internal/workflow` | WORKFLOW.md parser (YAML front matter + body). Liquid prompt renderer. fsnotify watcher. |
| `internal/tracker` | `Tracker` interface (read-only). |
| `internal/tracker/linear` | Linear GraphQL client. Pagination, normalization, `ExecuteGraphQL`. |
| `internal/workspace` | Per-issue workspace lifecycle: create/reuse/remove, path safety, hooks. |
| `internal/codex` | Codex app-server JSON line protocol client. |
| `internal/codex/tools` | Dynamic tools (`linear_graphql`). |
| `internal/orchestrator` | Poll loop, dispatch, retry/backoff, reconciliation, snapshot. Depends on interfaces only. |
| `internal/runner` | Agent runner glue: workspace + prompt + codex. |
| `internal/server` | HTTP dashboard and JSON status API. |
| `internal/worker` | Local + SSH launcher, worker pool with capacity tracking. |

The orchestrator depends on interfaces (`Tracker`, `WorkspaceManager`, `AgentRunner`), not concrete types. Tests use fakes.

See [`ARCHITECTURE.md`](ARCHITECTURE.md) for the full system overview.

## Key Conventions

- **Liquid templates, not Go templates.** The prompt engine uses `github.com/osteele/liquid`. Syntax: `{{ issue.identifier }}`, `{% if attempt %}`. Empty Go strings must be mapped to `nil` in bindings (Liquid treats `""` as truthy).
- **Config defaults live in `config.go` `applyDefaults()`**. Do not duplicate defaults elsewhere.
- **Workspace safety.** Three invariants enforced in `workspace.go`: (1) workspace path inside root, (2) not equal to root, (3) no symlink escape. Always call `ValidatePath` before creating or launching in a workspace.
- **High-trust auto-approve.** `approval_policy: "never"` means auto-approve everything. The codex client checks `isAutoApprove("never")` → true. This is intentional (matches upstream Elixir).
- **Issue identifier sanitization.** `SafeIdentifier` replaces `[^A-Za-z0-9._-]` with `_`. Used for workspace directory names.
- **Sandbox alignment.** `thread_sandbox` and `turn_sandbox_policy` must match. `defaultTurnSandboxPolicy()` in `client.go` handles this. Use `danger-full-access` if the agent needs `git commit/push`.

## Testing Patterns

- **Linear client:** fixture-driven tests in `internal/tracker/linear/testdata/*.json`. Inject `DoRequest` func to fake HTTP.
- **Codex client:** fake app-server bash scripts in `testdata/fake-codex/`. Each script simulates one protocol scenario (success, failure, approval, tool call, input required, process exit, sandbox trace).
- **Orchestrator:** interface fakes (`fakeTracker`, `fakeWorkspace`, `fakeRunner`) in the test file. No network, no filesystem, no subprocesses.
- **Server:** `httptest.NewRecorder` against `srv.Handler()`. No real listener.
- **Workspace:** `t.TempDir()` for isolation. Tests create real dirs, symlinks, and hook scripts.

## Git Flow

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for full details. Summary:

- GitHub Flow: feature branches from `main`, squash-merge only
- Branch naming: `<issue-key>/<short-description>` (e.g. `eng-44/assignee-routing`)
- Conventional commits, scoped by package (e.g. `fix(codex): ...`)
- `make check` must pass before opening a PR
- CI required + 1 review + branch up to date

## Codex Skills

Six skills in `.agents/skills/` for agent workflows: `commit`, `push`, `pull`, `land`, `linear`, `debug`.

## Upstream Reference

Upstream: [openai/symphony](https://github.com/openai/symphony). Key files: `SPEC.md`, `elixir/lib/symphony_elixir/orchestrator.ex`, `elixir/lib/symphony_elixir/codex/app_server.ex`.

## ExecPlan

Execution plans live in `docs/plans/`. See [`docs/plans/README.md`](docs/plans/README.md) for the index with status (active/completed).
