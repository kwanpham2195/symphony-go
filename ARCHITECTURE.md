# Architecture

Symphony Go is a single-binary service that polls Linear for issues, creates isolated workspaces, and runs Codex app-server sessions inside them.

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLI (cmd/symphony)                      │
│  Loads WORKFLOW.md, builds components, starts orchestrator      │
└──────┬──────────┬──────────┬──────────┬──────────┬──────────────┘
       │          │          │          │          │
       ▼          ▼          ▼          ▼          ▼
  ┌─────────┐ ┌────────┐ ┌───────┐ ┌────────┐ ┌────────┐
  │ Workflow │ │ Linear │ │Codex  │ │Workspace│ │ HTTP   │
  │ +Config  │ │Tracker │ │Client │ │Manager │ │Server  │
  └────┬─────┘ └───┬────┘ └───┬───┘ └───┬────┘ └───┬────┘
       │           │          │         │          │
       ▼           ▼          ▼         ▼          │
  ┌──────────────────────────────────────────┐     │
  │            Orchestrator                   │◄────┘
  │  poll → reconcile → validate → dispatch  │  (snapshot/refresh)
  │  retry queue, concurrency, stall detect  │
  └──────────────┬───────────────────────────┘
                 │
                 ▼
  ┌──────────────────────────┐
  │       Agent Runner       │
  │  workspace + prompt +    │
  │  codex turn loop         │
  └──────────┬───────────────┘
             │
             ▼
  ┌──────────────────────────┐
  │     Worker Pool          │
  │  local or SSH launcher   │
  └──────────────────────────┘
```

## Layers

The codebase follows five layers. Each layer depends only on layers below it.

### 1. Domain (`internal/domain`)

Shared types with no dependencies on other internal packages. Everything revolves around these types.

- `Issue` — normalized tracker issue (id, identifier, title, state, labels, blockers, timestamps)
- `Workspace` — filesystem workspace assigned to one issue (path, key, created flag)
- `AgentUpdate` — structured event from codex client to orchestrator
- `Snapshot` — point-in-time view of orchestrator state for the dashboard/API
- `TokenUsage`, `CodexTotals`, `RunningRow`, `RetryRow`

### 2. Integration (`internal/tracker`, `internal/codex`, `internal/workspace`, `internal/worker`)

Each package owns one external boundary. They depend on domain types and config, not on each other.

**Tracker** reads issues from Linear via GraphQL:
- `tracker.Tracker` interface — `FetchCandidateIssues`, `FetchIssuesByStates`, `FetchIssueStatesByIDs`
- `tracker/linear.Client` — implementation with pagination, normalization, `ExecuteGraphQL`
- Testable via injected `DoRequest` function and JSON fixtures in `testdata/`

**Codex** speaks the app-server JSON line protocol over subprocess stdio:
- `StartSession` — launch process, complete initialize/initialized/thread/start handshake
- `RunTurn` — send turn/start, stream events until turn/completed or failure
- Auto-approve command and file-change approvals under high-trust mode
- Dispatch registered dynamic tools on `item/tool/call`; unknown tools return failure
- `codex/tools.Tool` interface — `Name()`, `Spec()`, `Execute()`
- `codex/tools.LinearGraphQL` — proxies GraphQL through Symphony's Linear auth

**Workspace** manages per-issue directories:
- Create, reuse (preserves data), or remove stale non-directory paths
- Three safety invariants: inside root, not equal to root, no symlink escape
- Lifecycle hooks: `after_create` (fatal), `before_run` (fatal), `after_run` (ignored), `before_remove` (ignored)
- `SafeIdentifier` sanitizes issue identifiers to `[A-Za-z0-9._-]`

**Worker** abstracts process launch:
- `worker.Launcher` interface — `Start()`, `RunHook()`, `Host()`
- `LocalLauncher` — `bash -lc` in workspace dir
- `SSHLauncher` — remote execution via ssh
- `WorkerPool` — capacity tracking, least-loaded selection, preferred-host retry stability

### 3. Coordination (`internal/orchestrator`)

The single component that owns scheduling state. Depends on interfaces, not concrete types.

**State:**
- `running` map — issue ID to running entry (issue, session, timestamps, tokens, cancel func)
- `claimed` set — prevents duplicate dispatch
- `retryAttempts` map — issue ID to retry timer
- `codexTotals` — aggregate token counts and runtime seconds

**Tick cycle (every `polling.interval_ms`):**
1. **Reconcile stalls** — kill workers with no codex activity beyond `stall_timeout_ms`
2. **Reconcile states** — fetch current states for running issues; stop terminal (+ cleanup), stop non-active (no cleanup), update active
3. **Validate config** — skip dispatch if config is invalid
4. **Fetch candidates** — get active issues from Linear
5. **Sort** — priority ascending (1-4, null last), oldest `created_at`, identifier tiebreak
6. **Dispatch** — claim, select worker host, launch agent goroutine

**Dispatch eligibility:**
- Has id, identifier, title, state
- State is active and not terminal
- Not already claimed or running
- Global slots available (`max_concurrent_agents - running count`)
- Per-state slots available (`max_concurrent_agents_by_state`)
- Worker pool has capacity (when SSH is configured)
- Todo blocker rule: all blockers must be in terminal states

**Retry:**
- Normal exit → continuation retry (1 second delay)
- Failure → exponential backoff (`10s × 2^(attempt-1)`, capped at `max_retry_backoff_ms`)
- Retry handler re-fetches candidates, checks eligibility, dispatches or requeues

### 4. Glue (`internal/runner`)

Agent runner wires workspace, prompt, and codex into a single run lifecycle:

1. Create/reuse workspace
2. Run `before_run` hook
3. Start codex session
4. Render Liquid prompt with issue context
5. Run turns up to `max_turns` (first turn = full prompt, continuations = guidance)
6. Run `after_run` hook (best effort)

### 5. Surfaces (`cmd/symphony`, `internal/server`, `internal/workflow`)

**CLI** (`cmd/symphony/main.go`):
- Loads WORKFLOW.md, builds config, constructs all components
- `--validate-only` — parse and validate, then exit
- `--once` — single poll cycle, then exit
- `--port` — enable HTTP dashboard
- `--version` — print build info (set by goreleaser ldflags)
- Signal handling (SIGINT/SIGTERM) for graceful shutdown

**HTTP Server** (`internal/server`):
- `GET /` — HTML dashboard with running sessions, retry queue, token totals
- `GET /api/v1/state` — full JSON snapshot from orchestrator
- `GET /api/v1/issues/{identifier}` — issue-specific debug state
- `POST /api/v1/refresh` — trigger immediate poll cycle
- Reads from orchestrator `Snapshot()` only — no separate state

**Workflow** (`internal/workflow`):
- Parser: YAML front matter + Markdown body
- Prompt renderer: Liquid templates (`{{ issue.identifier }}`, `{% if attempt %}`)
- File watcher: fsnotify-based, reloads config on change, keeps last good on error

## Data Flow

```
WORKFLOW.md
    │
    ├──parse──► Config (typed settings, env resolved, defaults applied)
    │
    └──body───► Prompt template (Liquid)
                    │
                    ├── issue context ──► Rendered prompt
                    │
Linear API ─────────┤
    │               │
    └── issues ─────┘
                    │
         Orchestrator decides dispatch
                    │
                    ▼
         Runner creates workspace
                    │
                    ▼
         Codex subprocess launched
         (bash -lc <command> in workspace)
                    │
                    ├── stdin:  initialize, thread/start, turn/start
                    ├── stdout: JSON line events (turn/completed, approvals, tools)
                    └── stderr: diagnostics (logged, not parsed)
                    │
                    ▼
         AgentUpdate events ──► Orchestrator state ──► Snapshot ──► Dashboard/API
```

## Key Design Decisions

**Interface-driven orchestrator.** The orchestrator depends on `Tracker`, `WorkspaceManager`, and `AgentRunner` interfaces. Tests use fakes with no network, no filesystem, no subprocesses. This makes the scheduling logic fully deterministic in tests.

**Liquid, not Go templates.** The upstream Elixir uses Liquid. We use `github.com/osteele/liquid` for compatibility. Important: empty Go strings must be `nil` in template bindings because Liquid treats `""` as truthy (Ruby semantics).

**High-trust auto-approve.** `approval_policy: "never"` means auto-approve everything. The codex client checks `isAutoApprove("never") → true`. This matches the upstream Elixir behavior. It is intentional and should only run in trusted environments.

**Worker pool is optional.** When `worker.ssh_hosts` is empty (default), everything runs locally. The pool is `nil` and all capacity checks pass. SSH adds host selection, per-host limits, and preferred-host retry stability without changing the orchestrator's scheduling logic.

**Dashboard reads snapshots.** The HTTP server calls `Snapshot()` on the orchestrator and renders the result. It never modifies orchestrator state (except `POST /api/v1/refresh` which triggers a tick). This keeps the server stateless and testable with `httptest`.

**Dynamic reload via fsnotify.** The CLI watches WORKFLOW.md. On change, it re-parses config, validates, and updates components in place. Invalid reloads are logged and ignored — the service keeps running with the last good config.
