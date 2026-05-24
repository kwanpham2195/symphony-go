# AGENTS.md

Go implementation of [Symphony](https://github.com/openai/symphony) — a scheduler/runner that polls Linear, creates per-issue workspaces, and runs Codex app-server sessions.

## Build / Test

```bash
go test ./...
go vet ./...
go build ./...
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
| `internal` | Shared cross-package types: `Issue`, `Workspace`, `Workflow`, `AgentUpdate`, etc. |
| `internal/config` | Typed config from WORKFLOW.md front matter. Defaults, `$VAR` env, `~` expansion, validation. |
| `internal/workflow` | WORKFLOW.md parser (YAML front matter + body). Liquid prompt renderer. |
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

## Key Conventions

- **Liquid templates, not Go templates.** The prompt engine uses `github.com/osteele/liquid`. Syntax: `{{ issue.identifier }}`, `{% if attempt %}`. Empty Go strings must be mapped to `nil` in bindings (Liquid treats `""` as truthy).
- **Config defaults live in `config.go` `applyDefaults()`**. Do not duplicate defaults elsewhere.
- **Workspace safety.** Three invariants enforced in `workspace.go`: (1) workspace path inside root, (2) not equal to root, (3) no symlink escape. Always call `ValidatePath` before creating or launching in a workspace.
- **High-trust auto-approve.** `approval_policy: "never"` means auto-approve everything. The codex client checks `isAutoApprove("never")` → true. This is intentional (matches upstream Elixir).
- **Issue identifier sanitization.** `SafeIdentifier` replaces `[^A-Za-z0-9._-]` with `_`. Used for workspace directory names.

## Testing Patterns

- **Linear client:** fixture-driven tests in `internal/tracker/linear/testdata/*.json`. Inject `DoRequest` func to fake HTTP.
- **Codex client:** fake app-server bash scripts in `testdata/fake-codex/`. Each script simulates one protocol scenario (success, failure, approval, tool call, input required, process exit).
- **Orchestrator:** interface fakes (`fakeTracker`, `fakeWorkspace`, `fakeRunner`) in the test file. No network, no filesystem, no subprocesses.
- **Server:** `httptest.NewRecorder` against `srv.Handler()`. No real listener.
- **Workspace:** `t.TempDir()` for isolation. Tests create real dirs, symlinks, and hook scripts.

## Upstream Reference

Upstream reference: [openai/symphony](https://github.com/openai/symphony). Key files: `SPEC.md`, `elixir/lib/symphony_elixir/orchestrator.ex`, `elixir/lib/symphony_elixir/codex/app_server.ex`, `elixir/lib/symphony_elixir/linear/client.ex`.

## ExecPlan

The living implementation plan is at `.agents/PLANS/2026-05-22-build-go-symphony.md`. Keep its Progress, Surprises, Decision Log, and Outcomes sections current.
