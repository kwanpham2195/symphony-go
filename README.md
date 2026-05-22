# Symphony Go

A Go implementation of [Symphony](https://github.com/openai/symphony): a long-running service that reads Linear issues, creates per-issue workspaces, and runs Codex app-server sessions inside them.

## Quick Start

```bash
# Set your Linear API key
export LINEAR_API_KEY=lin_api_...

# Validate a workflow file
go run ./cmd/symphony --validate-only ./WORKFLOW.md

# Run a single poll cycle (useful for smoke testing)
go run ./cmd/symphony --once ./WORKFLOW.md

# Run the service
go run ./cmd/symphony ./WORKFLOW.md

# Run with dashboard on port 8080
go run ./cmd/symphony --port 8080 ./WORKFLOW.md
```

## WORKFLOW.md

The workflow file is Markdown with optional YAML front matter. The front matter configures runtime settings; the Markdown body is the prompt template.

```markdown
---
tracker:
  kind: linear
  project_slug: "my-project"
  api_key: $LINEAR_API_KEY
polling:
  interval_ms: 30000
workspace:
  root: ~/code/symphony-workspaces
agent:
  max_concurrent_agents: 5
  max_turns: 20
codex:
  command: codex app-server
  approval_policy: never
server:
  port: 8080
---
You are working on issue {{ issue.identifier }}: {{ issue.title }}.

{% if attempt %}
This is retry attempt #{{ attempt }}.
{% endif %}

{{ issue.description }}
```

See `testdata/workflows/minimal.md` for a working example.

## High-Trust Mode

This implementation uses **high-trust auto-approve** by default:
- Command execution approvals are auto-approved for the session.
- File change approvals are auto-approved for the session.
- User input requests fail the run immediately (non-interactive).

Only run in environments where you trust the Codex agent's actions.

## Architecture

```
cmd/symphony/          CLI entry point
internal/
  config/              Typed config with defaults, env resolution, validation
  workflow/            WORKFLOW.md parsing and Liquid prompt rendering
  domain/              Shared types (Issue, Workspace, Snapshot, etc.)
  tracker/             Tracker interface
  tracker/linear/      Linear GraphQL client and normalization
  workspace/           Per-issue workspace lifecycle and hooks
  codex/               Codex app-server JSON line protocol client
  codex/tools/         Dynamic tools (linear_graphql)
  orchestrator/        Poll loop, dispatch, retry, reconciliation
  runner/              Agent runner (workspace + prompt + codex)
  server/              HTTP dashboard and JSON status API
  worker/              Local and SSH worker pool
  observability/       Structured logging
testdata/
  workflows/           Sample workflow files
  fake-codex/          Fake app-server scripts for testing
```

## Dashboard and API

When `--port` or `server.port` is set, the HTTP server exposes:

- `GET /` - HTML dashboard with running sessions, retry queue, token totals
- `GET /api/v1/state` - Full JSON snapshot
- `GET /api/v1/issues/{identifier}` - Issue-specific debug state
- `POST /api/v1/refresh` - Trigger immediate poll cycle

## Testing

```bash
go test ./...
```

151 tests cover: workflow parsing, config defaults and validation, Linear GraphQL normalization with fixtures, workspace path safety and hooks, Codex protocol handshake and turn lifecycle, orchestrator dispatch/retry/reconciliation, HTTP API responses, linear_graphql tool, and worker pool capacity.

## Config Reference

| Field | Default | Description |
|-------|---------|-------------|
| `tracker.kind` | required | `linear` |
| `tracker.api_key` | `$LINEAR_API_KEY` | Linear API token or `$VAR` |
| `tracker.project_slug` | required | Linear project slug |
| `tracker.active_states` | `["Todo", "In Progress"]` | States eligible for dispatch |
| `tracker.terminal_states` | `["Closed", "Cancelled", "Canceled", "Duplicate", "Done"]` | States that trigger cleanup |
| `polling.interval_ms` | `30000` | Poll interval in milliseconds |
| `workspace.root` | `$TMPDIR/symphony_workspaces` | Workspace root directory |
| `agent.max_concurrent_agents` | `10` | Global concurrency limit |
| `agent.max_turns` | `20` | Max turns per worker session |
| `agent.max_retry_backoff_ms` | `300000` | Max retry delay (5 minutes) |
| `codex.command` | `codex app-server` | Codex app-server command |
| `codex.stall_timeout_ms` | `300000` | Stall detection timeout |
| `hooks.after_create` | none | Shell script run after workspace creation |
| `hooks.before_run` | none | Shell script run before each agent attempt |
| `hooks.after_run` | none | Shell script run after each agent attempt |
| `hooks.timeout_ms` | `60000` | Hook execution timeout |
| `server.port` | none | HTTP server port (enables dashboard) |
| `worker.ssh_hosts` | none | SSH hosts for remote workers |
| `worker.max_concurrent_agents_per_host` | none | Per-host concurrency cap |
