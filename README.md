# Symphony Go

Symphony turns Linear issues into autonomous coding sessions. You manage the work — Symphony manages the agents.

Instead of babysitting a coding agent through each task, you drop issues into a Linear project board. Symphony picks them up, creates isolated workspaces, launches [Codex](https://github.com/openai/codex) agents, and streams their progress. When an agent finishes, it opens a PR. You review the PR, not the agent.

This is a Go implementation of [OpenAI's Symphony](https://github.com/openai/symphony), built from their [spec](https://github.com/openai/symphony/blob/main/SPEC.md).

> [!WARNING]
> Early-stage software. Run in trusted environments only. The default `approval_policy: never` auto-approves all agent actions.

## Prerequisites

**Codex CLI** — Symphony launches [Codex](https://github.com/openai/codex) as its agent backend. Install it and sign in (or set `OPENAI_API_KEY`):

```bash
npm install -g @openai/codex
codex --version   # should print codex-cli 0.1xx.x
```

**Linear account + API key** — Symphony reads issues from [Linear](https://linear.app). Create a personal API key at [Linear Settings → API](https://linear.app/settings/api):

1. Create an API key with read/write access.
2. Export it: `export LINEAR_API_KEY=lin_api_...`

**Linear project** — Create a project in Linear and note its **slug** (the short ID in the project URL, e.g. `004d9e34fd6a`). Symphony polls this project for issues in "Todo" or "In Progress" states.

**A git repository** — The agent needs a codebase to work in. Your workflow's `after_create` hook should clone it into each workspace (see [Quick Start](#quick-start)).

**Go 1.25+** — Only needed if building from source. Pre-built binaries are available on the [Releases](https://github.com/kwanpham2195/symphony-go/releases) page.

## Supported Trackers

| Tracker | Status | Notes |
|---------|--------|-------|
| [Linear](https://linear.app) | Supported | GraphQL API, project-based polling, pagination |
| GitHub Issues | Not yet | Planned — the `Tracker` interface is pluggable |
| Jira | Not yet | Planned |

The orchestrator talks to a `Tracker` interface, not Linear directly. Adding a new tracker means implementing one interface with a few methods (fetch candidate issues, fetch by IDs). See [`internal/tracker/tracker.go`](internal/tracker/tracker.go) for the interface definition.

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                        Linear Board                         │
│                                                             │
│   Todo          In Progress          Done                   │
│  ┌──────┐      ┌──────────────┐    ┌──────────────┐        │
│  │ENG-8 │─────▶│ENG-5  (agent)│    │ENG-3  ✓ (PR) │        │
│  │ENG-9 │      │ENG-7  (agent)│    │ENG-4  ✓ (PR) │        │
│  └──────┘      └──────────────┘    └──────────────┘        │
└─────────────────────────────────────────────────────────────┘
        │                 ▲
        │  polls every    │  agent updates
        │  30 seconds     │  issue status
        ▼                 │
┌─────────────────────────────────────────────────────────────┐
│                      Symphony (Go)                          │
│                                                             │
│  Orchestrator ──▶ Workspace Manager ──▶ Codex App-Server    │
│  (poll, dispatch,   (create dirs,       (run agent turns,   │
│   retry, backoff)    run hooks)          stream events)     │
│                                                             │
│  Dashboard: http://localhost:8080                            │
└─────────────────────────────────────────────────────────────┘
```

1. **Poll** — Symphony reads your Linear project board for issues in "Todo" or "In Progress" states.
2. **Workspace** — For each issue, it creates a directory and runs setup hooks (e.g., `git clone`).
3. **Agent** — It launches a Codex app-server in that workspace with a rendered prompt containing the issue details.
4. **Retry** — If the agent fails, Symphony retries with exponential backoff. If it succeeds, the agent handles PRs and issue updates through its own tools.
5. **Cleanup** — When an issue moves to "Done" or "Closed," Symphony removes the workspace.

Symphony is a **scheduler and tracker reader**. It does not write to Linear. The Codex agent handles ticket updates, PRs, and comments through its own tools (like `linear_graphql`).

## Quick Start

### Install

```bash
# From source
go install github.com/kwanpham2195/symphony-go/cmd/symphony@latest

# Or download a binary from Releases
# https://github.com/kwanpham2195/symphony-go/releases
```

### Create a workflow file

Copy [`WORKFLOW.example.md`](WORKFLOW.example.md) and edit it:

```markdown
---
tracker:
  kind: linear
  project_slug: "your-project-slug"
  api_key: $LINEAR_API_KEY
workspace:
  root: ~/code/my-workspaces
hooks:
  after_create: |
    git clone --depth 1 https://github.com/your-org/your-repo .
codex:
  command: codex app-server
  approval_policy: never
---

You are working on {{ issue.identifier }}: {{ issue.title }}.

{{ issue.description }}

{% if issue.blockers.size > 0 %}
Related issues:
{% for b in issue.blockers %}
- {{ b.identifier }}: {{ b.title }}
{% endfor %}
{% endif %}
```

The YAML front matter configures the runtime. The body is a [Liquid](https://shopify.github.io/liquid/) template rendered with issue context for each agent session.

### Run

```bash
export LINEAR_API_KEY=lin_api_...

# Check your workflow file is valid
symphony --validate-only ./WORKFLOW.md

# Run one poll cycle to see what would happen
symphony --once ./WORKFLOW.md

# Start the service with a dashboard
symphony --port 8080 ./WORKFLOW.md
```

## Dashboard

Start with `--port 8080` (or set `server.port` in the workflow). The dashboard shows running agents, retry queues, and issue states.

| Endpoint | Description |
|----------|-------------|
| `GET /` | HTML dashboard (auto-refresh) |
| `GET /api/v1/state` | Full JSON snapshot |
| `GET /api/v1/issues/{id}` | Issue debug state |
| `POST /api/v1/refresh` | Trigger immediate poll |

## Configuration

All settings go in the YAML front matter of your workflow file.

<details>
<summary>Full config reference</summary>

| Field | Default | Description |
|-------|---------|-------------|
| `tracker.kind` | required | `linear` |
| `tracker.api_key` | `$LINEAR_API_KEY` | Linear API token (or `$VAR` for env lookup) |
| `tracker.project_slug` | required | Linear project slug |
| `tracker.active_states` | `["Todo", "In Progress"]` | Issue states eligible for dispatch |
| `tracker.terminal_states` | `["Closed", "Cancelled", "Canceled", "Duplicate", "Done"]` | States that trigger workspace cleanup |
| `polling.interval_ms` | `30000` | Poll interval in milliseconds |
| `workspace.root` | `$TMPDIR/symphony_workspaces` | Root directory for workspaces |
| `agent.max_concurrent_agents` | `10` | Max agents running at once |
| `agent.max_concurrent_agents_by_state` | `{}` | Per-state concurrency limits |
| `agent.max_turns` | `20` | Max turns per agent session |
| `agent.max_retry_backoff_ms` | `300000` | Max retry delay (5 min) |
| `codex.command` | `codex app-server` | Codex app-server command |
| `codex.approval_policy` | impl-defined | Approval policy (`never` = auto-approve all) |
| `codex.thread_sandbox` | `workspace-write` | Sandbox mode (`workspace-write`, `danger-full-access`, `read-only`) |
| `codex.turn_timeout_ms` | `3600000` | Turn timeout (1 hour) |
| `codex.read_timeout_ms` | `5000` | Handshake response timeout |
| `codex.stall_timeout_ms` | `300000` | Stall detection timeout |
| `hooks.after_create` | — | Run after workspace creation (fatal on failure) |
| `hooks.before_run` | — | Run before each agent attempt (fatal on failure) |
| `hooks.after_run` | — | Run after each agent attempt (failure ignored) |
| `hooks.before_remove` | — | Run before workspace removal (failure ignored) |
| `hooks.timeout_ms` | `60000` | Hook execution timeout |
| `server.port` | — | HTTP dashboard port |
| `worker.ssh_hosts` | — | SSH hosts for remote workers |
| `worker.max_concurrent_agents_per_host` | — | Per-host concurrency cap |

</details>

## Architecture

See [`ARCHITECTURE.md`](ARCHITECTURE.md) for the full package map.

The short version: the orchestrator depends on interfaces (`Tracker`, `WorkspaceManager`, `AgentRunner`), not concrete types. This makes it easy to test with fakes and to swap components (e.g., different trackers or worker backends).

```
cmd/symphony/           CLI entry, wires everything
internal/
  config/               Config parsing, defaults, validation
  workflow/             WORKFLOW.md parser, Liquid renderer, file watcher
  tracker/linear/       Linear GraphQL client
  workspace/            Workspace lifecycle, path safety, hooks
  codex/                Codex app-server protocol client
  codex/tools/          Dynamic tools (linear_graphql)
  orchestrator/         Poll loop, dispatch, retry, reconciliation
  runner/               Glue: workspace + prompt + codex
  server/               HTTP dashboard and JSON API
  worker/               Local + SSH launchers, worker pool
```

## Development

```bash
make check          # lint + tests with -race (full CI gate)
make check-sandbox  # lint + unit tests only (no network)
make build          # build binary
make release-dry    # dry-run goreleaser
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for conventions and [`docs/smoke.md`](docs/smoke.md) for a real Linear smoke test walkthrough.

## License

[MIT](LICENSE)
