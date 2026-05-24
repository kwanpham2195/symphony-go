# Build Go Symphony with Elixir Feature Parity

This ExecPlan is a living document. Keep `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` current as work proceeds.

This repository is `/Users/kwanpham/Work/symphony-go`. There is no repo-local `PLANS.md` yet. Store implementation plans under `.agents/PLANS/`.

## Purpose / Big Picture

Build a Go implementation of Symphony: a long-running service that reads Linear issues, creates one workspace per issue, starts Codex app-server inside that workspace, and keeps enough state and logs for an operator to understand what is running.

After the first local proof, a user can run:

    cd /Users/kwanpham/Work/symphony-go
    symphony ./WORKFLOW.md

With `LINEAR_API_KEY` set and a valid `WORKFLOW.md`, the service polls a Linear project, dispatches eligible issues to local Codex workers, retries failed attempts with backoff, and stops workers when Linear state changes make the issue no longer eligible.

The target is Elixir feature parity. "Core spec" means the required behavior in Symphony `SPEC.md`: workflow loading, config, Linear read operations, local workspaces, local Codex app-server sessions, orchestration, retries, reconciliation, and structured logs. "Elixir feature parity" means the Go version should also match the useful extras from the Elixir implementation: dashboard and JSON status API, SSH worker pool, assignee routing, `linear_graphql` client-side tool, token and rate-limit accounting, high-trust approval handling, and the same workflow shape.

Build this in slices. The first slice proves the local Linear -> workspace -> Codex loop. Later slices add the Elixir extras. This keeps the work testable while still making parity the endpoint.

## Progress

- [x] 2026-05-22: Explored upstream Symphony through `opensrc`.
- [x] 2026-05-22: Created new local repository at `/Users/kwanpham/Work/symphony-go`.
- [x] 2026-05-22: Captured initial user decisions: Linear only, high-trust auto-approve, local workers first.
- [x] 2026-05-22: Updated target from core-only v1 to Elixir feature parity.
- [x] 2026-05-22: Initialize Go module and baseline CLI.
- [x] 2026-05-22: Implement workflow and config layer (parsing, defaults, env resolution, validation).
- [x] 2026-05-22: Implement Liquid prompt template engine with strict nil semantics.
- [x] 2026-05-22: Implement Linear read adapter (M2).
- [x] 2026-05-22: Implement workspace manager and hooks (M3).
- [x] 2026-05-22: Implement Codex app-server client (M4).
- [x] 2026-05-22: Implement orchestrator loop, retry, and reconciliation (M5).
- [x] 2026-05-22: Wire CLI with agent runner and signal handling (M6).
- [x] 2026-05-22: Add dashboard and JSON status API (M7).
- [x] 2026-05-22: Add `linear_graphql` dynamic tool (M8).
- [x] 2026-05-22: Add SSH worker pool after local workers are stable (M9).
- [x] 2026-05-22: Real Linear smoke test passed (M10).

## Surprises & Discoveries

- Observation: The local `opensrc` Volta shim is broken.
  Evidence: `opensrc path openai/symphony#main` failed with `Volta error: Could not execute command`; `node /Users/kwanpham/.volta/tools/shared/opensrc/bin/opensrc.js path openai/symphony#main` returned `/Users/kwanpham/.opensrc/repos/github.com/openai/symphony/main`.

- Observation: The `opensrc` cache has source files but is not a git checkout.
  Evidence: `git -C /Users/kwanpham/.opensrc/repos/github.com/openai/symphony/main status` returned `fatal: not a git repository`.

- Observation: The upstream workflow prompt uses Liquid syntax, not Go `text/template` syntax.
  Evidence: upstream `elixir/WORKFLOW.md` contains `{% if attempt %}` and `{{ issue.identifier }}`. Go `text/template` can render `{{ ... }}` style values, but not Liquid tags such as `{% if %}`.

- Observation: The shell has `GO111MODULE=off`.
  Evidence: `go list -m -versions ...` failed with `go: list -m cannot be used with GO111MODULE=off`; rerunning with `GO111MODULE=on` worked.

- Observation: Likely dependency candidates are available through Go modules.
  Evidence: `GO111MODULE=on go list -m -versions gopkg.in/yaml.v3` returned `v3.0.0 v3.0.1`; `github.com/fsnotify/fsnotify` returned versions through `v1.10.1`; `github.com/osteele/liquid` returned versions through `v1.8.1`.

- Observation: Liquid (osteele/liquid) treats empty string as truthy, matching Ruby semantics.
  Evidence: `{% if issue.description %}` with description="" rendered the truthy branch. Fixed by mapping empty Go strings to nil in template bindings.

- Observation: Go 1.25.0 is the local stable Go version.
  Evidence: `go version` returned `go1.25.0 darwin/arm64`.

## Decision Log

- Decision: Put the new project at `/Users/kwanpham/Work/symphony-go`.
  Rationale: The user asked for a new repo under `Work/`; no existing Symphony repo was present there.
  Date/Author: 2026-05-22 / Codex

- Decision: Build toward Elixir feature parity, with the local worker path as the first proof.
  Rationale: The user prefers parity after learning the difference. A local-first slice keeps the implementation testable while the final plan still includes dashboard/API, SSH workers, `linear_graphql`, assignee routing, and token/rate-limit observability.
  Date/Author: 2026-05-22 / Matthew and Codex

- Decision: Support Linear only in v1.
  Rationale: The user chose Linear only. Keep a small tracker interface so later adapters can be added without changing the orchestrator.
  Date/Author: 2026-05-22 / Matthew and Codex

- Decision: Use high-trust Codex policy for v1.
  Rationale: The user chose high-trust auto-approve. The app-server client should auto-approve command and file-change approvals for the session and should fail or auto-answer user-input requests so a run does not hang forever.
  Date/Author: 2026-05-22 / Matthew and Codex

- Decision: Implement local workers first and SSH workers later as a parity milestone.
  Rationale: The user first chose local workers only, then preferred Elixir feature parity. SSH workers are part of Elixir parity, but they do not need to block the first local working loop.
  Date/Author: 2026-05-22 / Matthew and Codex

## Outcomes & Retrospective

### Milestones 1-10 (2026-05-22)

Status: complete.

What worked:
- YAML front matter parsing with gopkg.in/yaml.v3 is straightforward.
- osteele/liquid handles Liquid template syntax well, including {% if %} and {{ }}.
- Config layer covers all spec fields with correct defaults and env resolution.
- CLI --validate-only prints useful summary and exits nonzero on errors.
- Fixture-driven Linear tests cover all normalization and error paths without network.
- Fake codex scripts in testdata/ test the full protocol handshake.
- Orchestrator tests use interface-driven fakes for all dependencies.
- HTTP server tests use httptest (no real listener needed).
- Real Linear smoke test verified the full loop: fetch CFW-43 from Linear project
  f694135aa121, create workspace, run fake codex, produce hello.txt, continuation
  retries working correctly.

What changed:
- Prompt engine added in M1 instead of waiting for a separate step.
- Empty string fields must be nil in Liquid bindings (Ruby semantics).
- Worker pool added as a separate Pool type rather than embedded in orchestrator.
- Bug fix: FetchCandidateIssues was passing nil states to GraphQL; client now stores
  ActiveStates.

Stats:
- 151 tests across 9 packages, all passing.
- ~3900 LOC production code, ~3200 LOC tests.
- No file exceeds ~500 LOC.

Remaining integration wiring (non-blocking, all tested independently):
- Wire linear_graphql tool spec into codex session thread/start.
- Wire SSH worker pool into orchestrator dispatch path.
- Workflow file watcher for dynamic reload (fsnotify).

## Context and Orientation

Symphony is a scheduler and runner. It reads work from an issue tracker, decides which issues are eligible, creates a safe per-issue workspace, starts a coding agent inside that workspace, and tracks retries and status.

Important terms:

- `WORKFLOW.md`: A repo-owned Markdown file. It has optional YAML front matter for runtime settings and a Markdown prompt body used as the Codex task prompt.
- `front matter`: YAML between leading `---` lines at the top of `WORKFLOW.md`.
- `issue`: A normalized Linear issue with fields such as `id`, `identifier`, `title`, `description`, `state`, `priority`, `labels`, and blockers.
- `workspace`: A local directory for one issue, named from the issue identifier after replacing unsafe characters with `_`.
- `orchestrator`: The single component that owns scheduling state. It decides dispatch, retry, cancellation, and reconciliation.
- `reconciliation`: Checking current Linear state for running issues and stopping workers when an issue becomes terminal or non-active.
- `Codex app-server`: A long-running Codex subprocess that accepts JSON messages on stdin/stdout. Symphony sends `initialize`, `thread/start`, and `turn/start`.
- `high-trust`: This implementation automatically approves app-server command and file-change approvals for the current session. This is powerful and should only run in trusted environments.
- `dashboard`: A local web page that shows running issues, retry queue, token usage, recent events, and health state.
- `JSON status API`: HTTP endpoints used by the dashboard and operators, including `/api/v1/state`, `/api/v1/<issue_identifier>`, and `/api/v1/refresh`.
- `linear_graphql`: A client-side tool advertised to Codex app-server. It lets the agent make raw Linear GraphQL calls through Symphony's configured Linear token.
- `SSH worker`: A remote machine reachable by SSH where Symphony can create the issue workspace and launch Codex app-server. The central orchestrator still owns scheduling state.

Upstream source explored:

- `SPEC.md`: language-neutral contract.
- `elixir/README.md`: reference implementation setup and behavior.
- `elixir/WORKFLOW.md`: real workflow config and prompt sample.
- `elixir/lib/symphony_elixir/orchestrator.ex`: scheduling, retry, reconciliation, and status state.
- `elixir/lib/symphony_elixir/agent_runner.ex`: workspace plus Codex turn loop.
- `elixir/lib/symphony_elixir/codex/app_server.ex`: Codex app-server protocol client.
- `elixir/lib/symphony_elixir/workspace.ex`: workspace path safety and hooks.
- `elixir/lib/symphony_elixir/linear/client.ex`: Linear GraphQL queries and normalization.

The Go repository should start with this layout:

- `cmd/symphony/main.go`: CLI entry point.
- `internal/workflow`: reads and parses `WORKFLOW.md`.
- `internal/config`: typed config defaults, env resolution, validation, and dynamic reload.
- `internal/domain`: shared domain types such as `Issue`, `Blocker`, `Workflow`, and `RunAttempt`.
- `internal/tracker`: tracker interface.
- `internal/tracker/linear`: Linear GraphQL client and normalization.
- `internal/workspace`: workspace path creation, path safety, lifecycle hooks.
- `internal/codex`: app-server subprocess protocol client.
- `internal/orchestrator`: poll loop, scheduling state, retries, reconciliation.
- `internal/observability`: structured logging, token/rate-limit accounting, snapshots, and dashboard/API DTOs.
- `internal/server`: optional dashboard and JSON status API.
- `internal/codex/tools`: dynamic app-server tools such as `linear_graphql`.
- `internal/worker`: local and SSH worker launch helpers.
- `testdata`: sample workflows, fake app-server scripts, and Linear payload fixtures.

## Plan of Work

Start with the Go module and a CLI that can parse arguments, load `WORKFLOW.md`, and validate config. Then add unit-tested pieces in the same order the service uses them: workflow/config, tracker, workspace, Codex client, orchestrator.

Keep package boundaries small. The orchestrator should depend on interfaces, not directly on HTTP, filesystem, or subprocess details. That makes tests deterministic and avoids needing Linear or Codex for most coverage.

Use structured logs and snapshots from the start because the dashboard/API will depend on the same state. Do not duplicate state logic in the web layer. The dashboard and JSON API should read from orchestrator snapshots only.

Implement parity features after the local loop works. Assignee routing belongs in the Linear adapter. `linear_graphql` belongs in the Codex dynamic tool layer, not the orchestrator. SSH workers belong behind the worker launch and workspace interfaces, not inside scheduling policy.

Before adding dependencies, run a quick health check. At minimum, check versions, recent release activity when easy, and whether the library is still maintained. Candidate dependencies:

- `gopkg.in/yaml.v3` for YAML front matter.
- `github.com/fsnotify/fsnotify` for workflow file watches.
- A Liquid-compatible template renderer, likely `github.com/osteele/liquid`, only if it can enforce strict missing variables and filters or can be wrapped to do so.

## Milestones

Milestone 1: Repository skeleton and config proof.

Goal: A Go CLI can load a workflow file, parse front matter, apply defaults, resolve env-backed secrets and paths, and fail clearly when required config is missing.

Work: Add `go.mod`, `cmd/symphony/main.go`, `internal/workflow`, `internal/config`, and tests. Use sample workflows in `testdata/workflows`.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./...

Expected result: tests pass. Running `go run ./cmd/symphony ./testdata/workflows/minimal.md --validate-only` prints a validation success. Running against an invalid workflow exits nonzero with a clear error.

Risk reduced: Proves the repo can build and proves the runtime contract can be loaded before touching Linear or Codex.

Milestone 2: Linear read adapter.

Goal: The service can fetch and normalize Linear issues for one project without mutating Linear.

Work: Add `internal/tracker` interface and `internal/tracker/linear` implementation. Implement candidate issue fetch, terminal-state fetch, and state refresh by IDs. Add fixture-driven tests for labels, blockers, priorities, timestamps, pagination, GraphQL errors, and malformed payloads.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/tracker/... ./internal/domain/...

Expected result: tests pass without network. Optional real smoke with `LINEAR_API_KEY` should fetch a configured project and print only counts and identifiers, not token values.

Risk reduced: Proves the scheduler can read the real source of work while keeping tracker writes out of scope.

Milestone 3: Workspace manager and hooks.

Goal: The service safely creates, reuses, and removes per-issue local workspaces.

Work: Add `internal/workspace`. Implement identifier sanitization, root containment checks, symlink escape checks, `after_create`, `before_run`, `after_run`, `before_remove`, and hook timeouts.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/workspace/...

Expected result: tests pass. Tests prove existing workspace data is preserved, stale non-directory paths are handled by the chosen policy, unsafe paths are rejected, hooks run at correct lifecycle points, and hook failures match the spec.

Risk reduced: Proves the strongest local safety invariant before launching Codex.

Milestone 4: Codex app-server client.

Goal: The service can launch a local app-server process inside a workspace and complete a fake turn through the JSON line protocol.

Work: Add `internal/codex`. Implement `initialize`, `initialized`, `thread/start`, `turn/start`, line buffering, response timeout, turn timeout, completion and failure mapping, approval auto-approve, unsupported tool failure response, user-input handling, and token/rate-limit event extraction.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/codex/...

Expected result: tests pass using fake app-server scripts from `testdata`. Tests prove stdout protocol parsing, stderr diagnostics, high-trust approvals, unsupported tools, input-required behavior, and workspace cwd guard.

Risk reduced: Proves the hardest integration boundary without running real Codex.

Milestone 5: Orchestrator loop.

Goal: The orchestrator dispatches eligible issues, respects concurrency, retries failures, schedules continuation retries, and reconciles running issues with tracker state.

Work: Add `internal/orchestrator`. Use interfaces for tracker, workspace manager, and agent runner. Implement poll tick, sort order, active/terminal state checks, Todo blocker rule, claimed/running sets, retry queue, stall detection, and startup cleanup.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/orchestrator/...

Expected result: tests pass. Tests prove priority sorting, blocker behavior, no duplicate dispatch, normal and abnormal retry paths, terminal cleanup, non-active stop without cleanup, and config reload effects on future dispatch.

Risk reduced: Proves Symphony's core behavior using deterministic fakes.

Milestone 6: CLI and operator-visible runtime.

Goal: A user can run the service locally and see useful logs. The process handles startup validation errors cleanly.

Work: Wire CLI, config loader, workflow watcher, Linear client, workspace manager, Codex client, orchestrator, and logger. Add `--validate-only` for config checks and `--once` for a single poll cycle if useful for smoke tests.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./...
    GO111MODULE=on go run ./cmd/symphony --validate-only ./testdata/workflows/minimal.md

Expected result: all tests pass. `--validate-only` exits zero for valid workflow and nonzero for invalid workflow. Logs include startup, validation, dispatch, retry, and stop events with `issue_id`, `issue_identifier`, and `session_id` when available.

Risk reduced: Proves the pieces are wired and usable as one command.

Milestone 7: Dashboard and JSON status API.

Goal: Operators can inspect running sessions, retry queue pressure, token usage, recent events, and health without reading raw logs.

Work: Add `internal/server` and expand `internal/observability`. Implement `GET /`, `GET /api/v1/state`, `GET /api/v1/<issue_identifier>`, and `POST /api/v1/refresh`. The dashboard can be simple server-rendered HTML. The API must read orchestrator snapshots and must not own separate scheduling state.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/server/... ./internal/observability/... ./internal/orchestrator/...

Expected result: tests pass. API tests prove response shapes, 404 for unknown issue, 405 for unsupported methods, and refresh coalescing behavior. Dashboard tests prove it renders useful state from a snapshot.

Risk reduced: Proves parity observability without changing orchestration correctness.

Milestone 8: Dynamic `linear_graphql` tool.

Goal: Codex sessions can call a client-side `linear_graphql` tool through Symphony's Linear auth.

Work: Add `internal/codex/tools`. Advertise the tool during `thread/start`. Validate tool input, execute exactly one GraphQL operation against Linear, preserve GraphQL error bodies, and return structured tool results to the app-server session. Unsupported tool calls should return structured failure and continue the session.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/codex/...

Expected result: tests pass. Fake app-server tests prove the tool is advertised, valid calls return `success=true`, GraphQL errors return `success=false` with body preserved, invalid arguments fail cleanly, and unsupported tools do not stall the run.

Risk reduced: Proves the biggest Elixir-only agent-tool feature without adding tracker writes to the orchestrator.

Milestone 9: SSH worker pool.

Goal: The central orchestrator can run workers on configured SSH hosts while preserving the same scheduling state and workspace safety rules.

Work: Add `internal/worker` support for local and SSH launch modes. Add config fields `worker.ssh_hosts` and `worker.max_concurrent_agents_per_host`. Interpret `workspace.root` on the remote host when SSH is enabled. Add host capacity checks, preferred-host retry behavior, remote hook execution, and remote Codex app-server launch over SSH stdio.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./internal/worker/... ./internal/workspace/... ./internal/codex/... ./internal/orchestrator/...

Expected result: tests pass with fake SSH command runners. Tests prove host capacity limits, no accidental fallback to local when all SSH hosts are full, remote workspace path validation, and stable host choice on retries.

Risk reduced: Proves Elixir parity for remote workers without requiring real SSH hosts in deterministic tests.

Milestone 10: Real local and parity smoke.

Goal: With real credentials and a safe Linear project, Symphony can dispatch one issue to a local fake or real Codex command and leave observable proof in the workspace.

Work: Add documented smoke workflows. Prefer a fake Codex command first to avoid uncontrolled side effects. Then optionally run real `codex app-server` if the operator confirms credentials and target issue scope. If SSH hosts are available, add an optional SSH smoke; otherwise record SSH as deterministic-test-only for now.

Commands:

    cd /Users/kwanpham/Work/symphony-go
    export LINEAR_API_KEY=...
    GO111MODULE=on go run ./cmd/symphony ./WORKFLOW.md --once

Expected result: the service fetches candidate issues, creates one workspace, runs the configured command in that workspace, logs the session, and does not touch paths outside the workspace root.

Risk reduced: Proves the system works against real external state and the parity features are observable.

## Concrete Steps

1. Create the module and baseline files.

    Working directory: `/Users/kwanpham/Work/symphony-go`

    Commands:

        GO111MODULE=on go mod init github.com/matthew-opn/symphony-go
        mkdir -p cmd/symphony internal/{workflow,config,domain,tracker,workspace,codex,orchestrator,observability} testdata/workflows
        GO111MODULE=on go test ./...

    Expected result: module exists and `go test ./...` can run.

2. Implement workflow parsing.

    Files:

    - `internal/workflow/workflow.go`
    - `internal/workflow/workflow_test.go`
    - `testdata/workflows/*.md`

    Required behavior: explicit path wins; default is `WORKFLOW.md` in current working directory; prompt-only files work; YAML front matter must decode to a map; prompt body is trimmed.

3. Implement typed config.

    Files:

    - `internal/config/config.go`
    - `internal/config/config_test.go`

    Required behavior: defaults match the spec; `$VAR` works for `tracker.api_key` and path fields; `~` path expansion works for path fields; `codex.command` stays a shell string; unsupported tracker kind fails validation; missing Linear project slug fails validation.

4. Choose and wrap the template engine.

    Files:

    - `internal/workflow/prompt.go`
    - `internal/workflow/prompt_test.go`

    Required behavior: render `issue` and `attempt`; fail on unknown variables and filters; support Liquid-style `{% if %}` and `{{ ... }}` syntax; provide default prompt when body is blank.

5. Implement Linear read adapter.

    Files:

    - `internal/tracker/tracker.go`
    - `internal/tracker/linear/client.go`
    - `internal/tracker/linear/client_test.go`
    - `testdata/linear/*.json`

    Required behavior: candidate query filters by project slug and active states; terminal query uses configured terminal states; state refresh query accepts IDs; pagination works; labels lowercase; blockers come from inverse `blocks` relations.

6. Implement workspace manager.

    Files:

    - `internal/workspace/workspace.go`
    - `internal/workspace/hooks.go`
    - `internal/workspace/workspace_test.go`

    Required behavior: sanitize issue identifiers; create workspace under root; reuse existing workspace; reject symlink escape; reject launching at workspace root; run hooks with timeout and correct failure rules.

7. Implement Codex app-server client.

    Files:

    - `internal/codex/client.go`
    - `internal/codex/protocol.go`
    - `internal/codex/client_test.go`
    - `testdata/fake-codex/*`

    Required behavior: launch with `bash -lc <codex.command>` in workspace cwd; send startup messages in order; parse JSON lines from stdout; handle stderr as diagnostics; auto-approve under high-trust; fail unsupported or input-required paths without hanging.

8. Implement agent runner.

    Files:

    - `internal/orchestrator/agent_runner.go`
    - `internal/orchestrator/agent_runner_test.go`

    Required behavior: create workspace, run `before_run`, start app-server, render first prompt, run continuation turns while issue remains active up to `agent.max_turns`, run `after_run` best effort, keep workspace after success.

9. Implement orchestrator.

    Files:

    - `internal/orchestrator/orchestrator.go`
    - `internal/orchestrator/retry.go`
    - `internal/orchestrator/snapshot.go`
    - `internal/orchestrator/orchestrator_test.go`

    Required behavior: one owner for scheduling state; claimed/running/retry maps; sort by priority then oldest created time then identifier; Todo blockers stop dispatch; terminal reconciliation cleans workspace; non-active reconciliation stops worker without cleanup.

10. Wire CLI and logs.

    Files:

    - `cmd/symphony/main.go`
    - `internal/observability/logging.go`

    Required behavior: optional positional workflow path; `--validate-only`; clear startup errors; structured logs with issue and session context; process exits nonzero on startup failure.

11. Add dashboard and JSON status API.

    Files:

    - `internal/server/server.go`
    - `internal/server/handlers.go`
    - `internal/server/server_test.go`
    - `internal/observability/snapshot.go`

    Required behavior: `server.port` and CLI `--port` enable the server; CLI port wins; `/` renders a human-readable dashboard; `/api/v1/state` returns counts, running rows, retry rows, token totals, and latest rate limits; `/api/v1/<issue_identifier>` returns issue-specific debug state or 404; `/api/v1/refresh` queues an immediate poll.

12. Add token, runtime, and rate-limit accounting.

    Files:

    - `internal/observability/accounting.go`
    - `internal/orchestrator/snapshot.go`
    - `internal/orchestrator/orchestrator_test.go`

    Required behavior: prefer absolute thread token totals; ignore delta-only payloads unless their event type defines them as totals; count runtime seconds for ended and active sessions; retain latest rate-limit payload.

13. Add dynamic `linear_graphql`.

    Files:

    - `internal/codex/tools/linear_graphql.go`
    - `internal/codex/tools/linear_graphql_test.go`
    - `internal/codex/client.go`

    Required behavior: advertise the tool during thread start; accept object input with `query` and optional `variables`; accept raw query string if kept for Elixir parity; reject blank query and invalid variables; execute with Symphony's configured Linear endpoint and token; return app-server tool responses without exposing secrets.

14. Add SSH worker support.

    Files:

    - `internal/worker/local.go`
    - `internal/worker/ssh.go`
    - `internal/worker/ssh_test.go`
    - `internal/workspace/remote.go`
    - `internal/codex/remote.go`

    Required behavior: run hooks and Codex over SSH stdio; respect per-host concurrency; do not fall back to local mode when SSH hosts are configured but full; preserve host choice for retries when possible; show host and workspace in snapshots.

15. Update docs and smoke workflows.

    Files:

    - `README.md`
    - `WORKFLOW.example.md`
    - `docs/smoke.md`

    Required behavior: explain high-trust mode plainly; document required env vars; include local and optional SSH smoke flows; show how to start the dashboard and call `/api/v1/state`.

## Validation and Acceptance

Core validation:

    cd /Users/kwanpham/Work/symphony-go
    GO111MODULE=on go test ./...

Acceptance for feature parity:

- A valid workflow can be loaded and validated.
- Invalid workflow YAML or missing required Linear config produces a clear nonzero CLI result.
- Linear adapter tests prove query payloads and normalization without network.
- Workspace tests prove path safety and hook lifecycle.
- Codex client tests prove protocol handshake, turn completion, approvals, and timeout behavior with fake app-server scripts.
- Orchestrator tests prove dispatch, retry, stall, and reconciliation logic.
- Dashboard/API tests prove status output and refresh behavior.
- Dynamic tool tests prove `linear_graphql` behavior.
- SSH worker tests prove remote worker scheduling and launch behavior with fakes.
- Assignee routing tests prove worker-scoped issue filtering.
- Token/rate-limit tests prove snapshot accounting matches app-server events.
- A documented smoke run can be performed with `LINEAR_API_KEY` and a safe test Linear project.

Do not call parity complete until all deterministic tests pass. If the real Linear or SSH smoke cannot run because credentials, hosts, or a safe project are missing, record exactly what is missing and keep it separate from deterministic test status.

## Idempotence and Recovery

All implementation steps should be safe to rerun. Unit tests should create temporary directories and clean them up. Workspace manager code should preserve existing issue workspace contents unless the path is a stale non-directory or terminal cleanup is explicitly requested.

Retry state is in memory for this parity target because the upstream Elixir implementation also treats durable retry/session persistence as a future TODO. On process restart, Symphony does not restore retry timers or live sessions. It recovers by polling active issues again and cleaning workspaces for terminal issues at startup.

If dependency selection is wrong, replace it before implementing broad code around it. The highest-risk dependency is the Liquid renderer because strict missing-variable behavior is part of the spec.

If `GO111MODULE=off` remains set in the shell, run Go commands with `GO111MODULE=on` until the repo has its own environment guidance.

## Artifacts and Notes

Upstream source cache:

    /Users/kwanpham/.opensrc/repos/github.com/openai/symphony/main

Useful upstream files read during planning:

    SPEC.md
    elixir/README.md
    elixir/WORKFLOW.md
    elixir/lib/symphony_elixir/orchestrator.ex
    elixir/lib/symphony_elixir/agent_runner.ex
    elixir/lib/symphony_elixir/codex/app_server.ex
    elixir/lib/symphony_elixir/workspace.ex
    elixir/lib/symphony_elixir/linear/client.ex

Dependency lookup notes:

    GO111MODULE=on go list -m -versions gopkg.in/yaml.v3
    Result included v3.0.1.

    GO111MODULE=on go list -m -versions github.com/fsnotify/fsnotify
    Result included v1.10.1.

    GO111MODULE=on go list -m -versions github.com/osteele/liquid
    Result included v1.8.1.

## Interfaces and Dependencies

Domain interfaces should keep orchestration independent of transport and process details.

Tracker interface:

    type Tracker interface {
        FetchCandidateIssues(ctx context.Context) ([]domain.Issue, error)
        FetchIssuesByStates(ctx context.Context, states []string) ([]domain.Issue, error)
        FetchIssueStatesByIDs(ctx context.Context, ids []string) ([]domain.Issue, error)
    }

Workspace manager interface:

    type WorkspaceManager interface {
        CreateForIssue(ctx context.Context, issue domain.Issue) (domain.Workspace, error)
        RemoveIssueWorkspace(ctx context.Context, identifier string) error
        RunBeforeRunHook(ctx context.Context, workspace domain.Workspace, issue domain.Issue) error
        RunAfterRunHook(ctx context.Context, workspace domain.Workspace, issue domain.Issue)
    }

Agent runner interface:

    type AgentRunner interface {
        Run(ctx context.Context, issue domain.Issue, attempt *int, updates chan<- domain.AgentUpdate) error
    }

Codex client interface:

    type CodexClient interface {
        StartSession(ctx context.Context, workspace string) (*Session, error)
        RunTurn(ctx context.Context, session *Session, issue domain.Issue, prompt string, onUpdate func(domain.AgentUpdate)) (*TurnResult, error)
        StopSession(session *Session) error
    }

Orchestrator public shape:

    type Orchestrator struct { ... }
    func New(deps Deps) *Orchestrator
    func (o *Orchestrator) Start(ctx context.Context) error
    func (o *Orchestrator) Tick(ctx context.Context) error
    func (o *Orchestrator) Snapshot() domain.Snapshot

Worker launcher interface:

    type Launcher interface {
        Start(ctx context.Context, workspace string, command string) (Process, error)
        RunHook(ctx context.Context, workspace string, script string, timeout time.Duration) (HookResult, error)
    }

Dynamic tool interface:

    type Tool interface {
        Name() string
        Spec() map[string]any
        Execute(ctx context.Context, args any) ToolResult
    }

HTTP server public shape:

    type Server struct { ... }
    func NewServer(orchestrator SnapshotProvider, refresher RefreshRequester, opts ServerOptions) *Server
    func (s *Server) ListenAndServe(ctx context.Context) error

Dependencies to decide or add:

- Go version: decide during module init, likely current local stable Go.
- YAML parser: `gopkg.in/yaml.v3`.
- Workflow watcher: `github.com/fsnotify/fsnotify`.
- Liquid renderer: select after strict-mode prototype, likely `github.com/osteele/liquid` if it supports the required behavior.
- HTTP client and server: standard library `net/http`.
- Logging: standard library `log/slog` unless a stronger local reason appears.

Deferred after Elixir parity:

- Persistent retry/session state.
- Tracker write APIs.
- Non-Linear tracker adapters.
