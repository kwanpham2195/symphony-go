# Symphony Go

A Go implementation of [Symphony](https://github.com/openai/symphony): a long-running service that reads Linear issues, creates per-issue workspaces, and runs Codex app-server sessions inside them.

## Install

```bash
go install github.com/kwanpham2195/symphony-go/cmd/symphony@latest
```

Or download a binary from [Releases](https://github.com/kwanpham2195/symphony-go/releases).

Or build from source:

```bash
git clone https://github.com/kwanpham2195/symphony-go.git
cd symphony-go
make build
```

## Quick Start

```bash
export LINEAR_API_KEY=lin_api_...

# Validate a workflow
symphony --validate-only ./WORKFLOW.md

# Single poll cycle (smoke test)
symphony --once ./WORKFLOW.md

# Run the service
symphony ./WORKFLOW.md

# Run with dashboard
symphony --port 8080 ./WORKFLOW.md

# Show version
symphony --version
```

See [`WORKFLOW.example.md`](WORKFLOW.example.md) for a copy-paste-ready workflow template.

## How It Works

```
Linear (Todo/In Progress)
        │
        ▼
   Symphony polls every N seconds
        │
        ▼
   For each eligible issue:
   1. Create workspace dir (~/workspaces/<issue-id>/)
   2. Run after_create hook (e.g. git clone)
   3. Run before_run hook (e.g. npm install)
   4. Launch codex app-server in workspace
   5. Send rendered prompt with issue context
   6. Stream turns until done or max_turns
   7. Run after_run hook
        │
        ├── Issue moves to Done/Closed → cleanup workspace
        ├── Codex fails → exponential backoff retry
        └── Issue still active → continuation retry in 1s
```

Symphony is a **scheduler/runner and tracker reader**. It does not write to Linear. The codex agent handles ticket updates, PRs, and comments through its own tools.

## WORKFLOW.md

Markdown file with optional YAML front matter for runtime settings. The body is a [Liquid](https://shopify.github.io/liquid/) prompt template.

```markdown
---
tracker:
  kind: linear
  project_slug: "my-project"
  api_key: $LINEAR_API_KEY
workspace:
  root: ~/code/symphony-workspaces
hooks:
  after_create: |
    git clone --depth 1 https://github.com/org/repo .
agent:
  max_concurrent_agents: 5
codex:
  command: codex app-server
  approval_policy: never
---
You are working on {{ issue.identifier }}: {{ issue.title }}.
{{ issue.description }}
```

## High-Trust Mode

This implementation uses **high-trust auto-approve** (`approval_policy: never`):
- Command execution and file change approvals are auto-approved for the session.
- User input requests fail the run immediately (non-interactive).

Only run in environments where you trust the Codex agent's actions.

## Architecture

```
cmd/symphony/              CLI entry point, wires all components
internal/
  *.go                     Shared types: Issue, Workspace, Workflow, AgentUpdate
  config/                  Typed config: defaults, $VAR env, ~ expansion, validation
  workflow/                WORKFLOW.md parser, Liquid prompt renderer, fsnotify watcher
  tracker/                 Tracker interface (read-only)
  tracker/linear/          Linear GraphQL client, pagination, normalization
  workspace/               Per-issue workspace lifecycle, path safety, hooks
  codex/                   Codex app-server JSON line protocol client
  codex/tools/             Dynamic tools (linear_graphql)
  orchestrator/            Poll loop, dispatch, retry/backoff, reconciliation, snapshot
  runner/                  Agent runner: workspace + prompt + codex glue
  server/                  HTTP dashboard and JSON status API
  worker/                  Local + SSH launcher, worker pool with capacity tracking
  observability/           Structured logging (slog)
testdata/
  workflows/               Sample workflow files
  fake-codex/              Fake app-server scripts for testing
```

## Dashboard and API

Enable with `--port PORT` or `server.port` in the workflow:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | HTML dashboard (auto-refresh) |
| `/api/v1/state` | GET | Full JSON snapshot |
| `/api/v1/issues/{identifier}` | GET | Issue-specific debug state |
| `/api/v1/refresh` | POST | Trigger immediate poll cycle |

## Development

```bash
make check       # full CI gate: vet + tests with -race
make test        # run all tests
make build       # build binary
make validate WORKFLOW=./testdata/workflows/minimal.md
make release-dry # dry-run goreleaser
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for conventions and [`docs/smoke.md`](docs/smoke.md) for a real Linear smoke test guide.

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
| `agent.max_concurrent_agents_by_state` | `{}` | Per-state concurrency limits |
| `agent.max_turns` | `20` | Max turns per worker session |
| `agent.max_retry_backoff_ms` | `300000` | Max retry delay (5 minutes) |
| `codex.command` | `codex app-server` | Codex app-server command |
| `codex.approval_policy` | impl-defined | Codex approval policy |
| `codex.thread_sandbox` | `workspace-write` | Codex sandbox mode |
| `codex.turn_timeout_ms` | `3600000` | Turn timeout (1 hour) |
| `codex.read_timeout_ms` | `5000` | Handshake response timeout |
| `codex.stall_timeout_ms` | `300000` | Stall detection timeout |
| `hooks.after_create` | none | Script after workspace creation (fatal on failure) |
| `hooks.before_run` | none | Script before each agent attempt (fatal on failure) |
| `hooks.after_run` | none | Script after each agent attempt (failure ignored) |
| `hooks.before_remove` | none | Script before workspace removal (failure ignored) |
| `hooks.timeout_ms` | `60000` | Hook execution timeout |
| `server.port` | none | HTTP server port (enables dashboard) |
| `worker.ssh_hosts` | none | SSH hosts for remote workers |
| `worker.max_concurrent_agents_per_host` | none | Per-host concurrency cap |

## License

[MIT](LICENSE)
