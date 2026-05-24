# CFW-51: Add Pi RPC as an alternative agent runner

This ExecPlan is a living document. Keep `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` current as work proceeds.

## Purpose / Big Picture

After this change, Symphony can run agents using Pi's RPC mode instead of the Codex app-server. Users set `runner: pi` in their WORKFLOW.md and Symphony launches Pi subprocesses, sends prompts, and maps Pi events back to the orchestrator's existing tracking and stall-detection systems.

The user-visible outcome: a WORKFLOW.md with `runner: pi` produces the same orchestrator behavior (dispatch, retry, reconciliation, stall detection, dashboard) as `runner: codex`, but the agent subprocess speaks the Pi RPC protocol instead of the Codex app-server protocol.

## Progress

- [ ] Not started

## Surprises & Discoveries

(None yet)

## Decision Log

(None yet)

## Outcomes & Retrospective

(None yet)

## Context and Orientation

### Key files

| File | Role |
|------|------|
| `internal/codex/client.go` | Current Codex app-server JSONL client. ~450 LOC. Manages subprocess, 3-step handshake, turn streaming, approval auto-respond, tool dispatch. |
| `internal/codex/session.go` | `LiveSession` metadata struct for tracking. |
| `internal/codex/tools/` | Dynamic tools (e.g. `linear_graphql`). Pi handles tools internally, so this is Codex-only. |
| `internal/runner/runner.go` | `Runner` struct implementing `AgentRunner`. Wires workspace + hooks + prompt + codex client. Owns the multi-turn loop. |
| `internal/orchestrator/orchestrator.go` | Poll loop, dispatch, stall detection, state reconciliation. Depends on `AgentRunner` interface (single method: `Run`). |
| `internal/config/config.go` | Typed config from WORKFLOW.md front matter. Has `CodexConfig` section. |
| `internal/agent.go` | `AgentUpdate` and `TokenUsage` domain types. |
| `cmd/symphony/main.go` | CLI wiring. Creates codex client, runner, orchestrator. |

### AgentRunner interface

The orchestrator depends on a single interface:

    type AgentRunner interface {
        Run(ctx context.Context, issue internal.Issue, attempt *int, updates chan<- internal.AgentUpdate) error
    }

The runner sends `AgentUpdate` events on the `updates` channel. The orchestrator uses these to track stalls (via `LastCodexTimestamp`), accumulate token usage, and count turns.

### Pi RPC protocol (summary)

Pi RPC uses JSONL over stdin/stdout. Key points:

- **No handshake.** Just send a `prompt` command.
- **Commands** are JSON objects with a `type` field: `prompt`, `abort`, `get_state`, `get_session_stats`, etc.
- **Responses** have `type: "response"` and include `success: true/false`. Correlated by optional `id` field.
- **Events** are JSON objects with a `type` field: `agent_start`, `agent_end`, `turn_start`, `turn_end`, `message_update`, `tool_execution_start/update/end`, `compaction_start/end`, `auto_retry_start/end`.
- **Completion signal** is `agent_end`, which contains all generated messages.
- **Token usage** is in `AssistantMessage.usage` inside `turn_end` and `agent_end` events.
- **Shutdown** by sending `abort` then closing stdin.

### How Codex multi-turn works vs. Pi

Codex: The runner loop in `runner.go` sends `turn/start` for each turn (up to `max_turns`). Each turn is an explicit request-response cycle.

Pi: One `prompt` command triggers a full agent run. Pi handles its own multi-turn internally (tool calls, retries, compaction). The runner just waits for `agent_end`. If the orchestrator wants another pass (continuation), it dispatches `Run()` again (which is how the existing retry/continuation already works).

This means the Pi runner's `Run()` sends one prompt and streams events until `agent_end`. The `max_turns` config does not map directly. Instead, it could be treated as max prompt sends (continuations) if the orchestrator re-dispatches.

## Plan of Work

### 1. Add `PiConfig` to config and wire `runner` discriminator

Add a `Runner` field (string, default `"codex"`) and a `PiConfig` struct to `config.go`. Make `Codex.Command` validation conditional on `Runner == "codex"`. Add `Pi.Command` validation when `Runner == "pi"`.

Files: `internal/config/config.go`, `internal/config/config_test.go`

### 2. Create `internal/pi/client.go`

New package `internal/pi` with a `Client` struct that:
- Launches `pi --mode rpc --no-session` as a subprocess via `bash -lc`
- Sends `prompt` commands (JSONL on stdin)
- Optionally sends `set_model` if `pi.model` is configured
- Reads JSONL events from stdout
- Maps events to `internal.AgentUpdate`
- Sends `abort` + closes stdin on shutdown
- Handles `compaction_start/end` and `auto_retry_start/end` by emitting heartbeat updates (to prevent stall false-positives)

No handshake. No approval handling. No tool dispatch. Much simpler than the Codex client.

Files: `internal/pi/client.go`

### 3. Create fake Pi RPC scripts for testing

Bash scripts in `testdata/fake-pi/` that simulate Pi RPC behavior:
- `success.sh`: prompt response + agent_start + turn events + agent_end
- `fail.sh`: prompt response + agent_start + message with error
- `compaction.sh`: includes compaction_start/end events (tests stall suppression)
- `retry.sh`: includes auto_retry_start/end events

Files: `testdata/fake-pi/*.sh`

### 4. Write `internal/pi/client_test.go`

Fixture-driven tests using fake scripts. Test:
- Successful prompt + event stream + agent_end detection
- Token usage extraction from turn_end events
- Graceful shutdown (abort + close)
- Stall-safe events during compaction/retry windows

Files: `internal/pi/client_test.go`

### 5. Create `PiRunner` in `internal/runner/pi_runner.go`

A separate `PiRunner` struct implementing `AgentRunner`. Shares workspace and hook logic with the existing `Runner` but uses the Pi client instead of Codex.

The flow:
1. Create workspace (via `WorkspaceManager`)
2. Run `before_run` hook
3. Launch Pi subprocess (start session)
4. Optionally send `set_model` command
5. Render prompt (same Liquid template system)
6. Send `prompt` command
7. Stream events until `agent_end`, mapping each to `AgentUpdate` on the channel
8. Run `after_run` hook
9. Stop session

No multi-turn loop inside `Run()`. Pi handles tool calls internally. If the orchestrator wants another pass (continuation), it dispatches `Run()` again (which is how the existing retry/continuation already works).

Files: `internal/runner/pi_runner.go`, `internal/runner/pi_runner_test.go`

### 6. Extract shared runner lifecycle

Both `Runner` and `PiRunner` share: workspace creation, hook execution, prompt rendering. Extract a `runLifecycle` helper or embed a common struct to avoid duplication.

Files: `internal/runner/runner.go`, `internal/runner/pi_runner.go`

### 7. Wire runner selection in `cmd/symphony/main.go`

Based on `cfg.Runner`:
- `"codex"` (default): build `codex.Client` + `runner.New()` (current path)
- `"pi"`: build `pi.Client` + `runner.NewPiRunner()`

The orchestrator gets whichever `AgentRunner` is selected. No other changes needed in orchestrator.

Files: `cmd/symphony/main.go`

### 8. Update config reload path

The workflow watcher calls `codexClient.UpdateConfig()` and `agentRunner.UpdatePrompt()`. For Pi mode, it should call `piClient.UpdateConfig()` and `piRunner.UpdatePrompt()` instead. Add an interface or conditional.

Files: `cmd/symphony/main.go`

### 9. Update `--validate-only` output

Show Pi config when `runner: pi`.

Files: `cmd/symphony/main.go`

## Milestones

### Milestone 1: Config + Pi client (standalone, no runner)

**Goal:** Pi client can launch a subprocess, send a prompt, stream events, and shut down. Fully testable with fake scripts.

**Work:** Steps 1-4 above.

**Verify:**

    go test ./internal/config/... ./internal/pi/...

All tests pass. Config accepts `runner: pi` with `pi:` section. Pi client handles success, failure, compaction, and retry scenarios.

**Risk reduction:** Proves the Pi RPC protocol parsing works and event mapping is correct before wiring into the runner.

### Milestone 2: PiRunner + wiring

**Goal:** End-to-end: orchestrator dispatches an issue, PiRunner launches Pi, prompt is sent, events flow back to orchestrator.

**Work:** Steps 5-9 above.

**Verify:**

    make check

All tests pass including runner tests with Pi fakes. Config validation is correct for both modes.

**Risk reduction:** Proves the runner abstraction works and the orchestrator's stall detection, token tracking, and retry all function correctly with Pi events.

### Milestone 3: Manual smoke test

**Goal:** Run Symphony with `runner: pi` against a real Pi installation and a Linear project.

**Verify:** Start symphony with a WORKFLOW.md that has `runner: pi`. Observe:
1. Issue is picked up from Linear
2. Pi subprocess starts
3. Prompt is sent, events stream to dashboard
4. Agent completes, orchestrator schedules continuation check
5. No stall false-positives during normal operation

**Risk reduction:** Proves real-world Pi RPC behavior matches the protocol docs.

## Concrete Steps

All commands run from the repo root.

### Step 1: Config changes

Add to `config.go`:

    type PiConfig struct {
        Command       string
        Model         string
        TurnTimeoutMS int
        ReadTimeoutMS int
    }

Add `Runner string` and `Pi PiConfig` to `Config`. Default `Runner` to `"codex"`. Default `Pi.Command` to `"pi --mode rpc --no-session"`. Parse from `raw["runner"]` and `raw["pi"]`. Make `Codex.Command` validation conditional.

Verify:

    go test ./internal/config/...

### Step 2: Pi client

Create `internal/pi/client.go` with:

    type Client struct { ... }
    type Session struct { ... }
    type TurnResult struct { Status string; Usage *internal.TokenUsage }

    func NewClient(cfg *config.Config, logger *slog.Logger) *Client
    func (c *Client) StartSession(ctx context.Context, workspace string) (*Session, error)
    func (c *Client) SendPrompt(ctx context.Context, sess *Session, prompt string, onUpdate func(internal.AgentUpdate)) (*TurnResult, error)
    func (c *Client) SetModel(sess *Session, model string) error
    func (c *Client) StopSession(sess *Session) error

Key behaviors:
- `StartSession`: launch subprocess, no handshake, just start scanning stdout
- `SendPrompt`: send `{"type":"prompt","message":"...","id":"req-1"}`, wait for response, then stream events until `agent_end`
- Event loop maps events to `AgentUpdate` using this table:

| Pi event | AgentUpdate.Event |
|----------|-------------------|
| agent_start | session_started |
| agent_end | turn_completed |
| turn_start | notification |
| turn_end | notification (extract usage) |
| tool_execution_start | notification |
| tool_execution_end | tool_call_completed |
| message_update | notification |
| compaction_start | compaction_started (heartbeat) |
| compaction_end | compaction_ended (heartbeat) |
| auto_retry_start | auto_retry_started (heartbeat) |
| auto_retry_end | auto_retry_ended (heartbeat) |

All mapped events carry a timestamp, preventing stall detection from firing during long-silent periods.

Verify:

    go build ./internal/pi/...

### Step 3: Fake Pi scripts

Create `testdata/fake-pi/success.sh`:

    #!/bin/bash
    read -r line  # prompt command
    echo '{"type":"response","command":"prompt","success":true,"id":"req-1"}'
    echo '{"type":"agent_start"}'
    echo '{"type":"turn_start"}'
    sleep 0.1
    echo '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done"}],"usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0}}}'
    echo '{"type":"agent_end","messages":[]}'

Create similar scripts for failure, compaction, and retry scenarios.

Verify:

    bash testdata/fake-pi/success.sh <<< '{"type":"prompt","message":"hi","id":"req-1"}'

### Step 4: Pi client tests

Create `internal/pi/client_test.go` following the same pattern as `internal/codex/client_test.go`: use `testdataPath()` to locate fake scripts, create a `testPiConfig()`, test `StartSession` + `SendPrompt` + `StopSession`.

Verify:

    go test ./internal/pi/...

### Step 5: PiRunner

Create `internal/runner/pi_runner.go`:

    type PiRunner struct {
        cfg    *config.Config
        wsMgr  *workspace.Manager
        piC    *pi.Client
        logger *slog.Logger
        mu     sync.RWMutex
        prompt string
    }

    func NewPiRunner(...) *PiRunner
    func (r *PiRunner) Run(ctx context.Context, issue internal.Issue, attempt *int, updates chan<- internal.AgentUpdate) error
    func (r *PiRunner) UpdatePrompt(prompt string)

`Run()` flow:
1. `wsMgr.CreateForIssue(ctx, issue)`
2. `wsMgr.RunBeforeRunHook(ctx, ws, issue)`
3. `defer wsMgr.RunAfterRunHook(ctx, ws, issue)`
4. `piC.StartSession(ctx, ws.Path)`
5. `defer piC.StopSession(sess)`
6. If model configured: `piC.SetModel(sess, cfg.Pi.Model)`
7. `workflow.RenderPrompt(prompt, issue, attempt)`
8. `piC.SendPrompt(ctx, sess, rendered, onUpdate)`
9. Check result status, return error on failure

Verify:

    go test ./internal/runner/...

### Step 6: Wire in main.go

In `cmd/symphony/main.go`, after config is built:

    var agentRunner orchestrator.AgentRunner
    switch cfg.Runner {
    case "pi":
        piClient := pi.NewClient(cfg, logger)
        agentRunner = runner.NewPiRunner(cfg, wsMgr, piClient, wf.PromptTemplate, logger)
    default:
        codexClient := codex.NewClient(cfg, logger)
        codexClient.RegisterTool(tools.NewLinearGraphQL(tracker))
        agentRunner = runner.New(cfg, wsMgr, codexClient, wf.PromptTemplate, logger)
    }

Update the workflow watcher reload callback similarly.

Verify:

    make check

## Validation and Acceptance

### Unit tests

    make check

Must pass with no failures. Coverage for:
- Config parsing with `runner: pi` and `pi:` section
- Pi client: success, failure, compaction, retry event streams
- PiRunner: full lifecycle with fake Pi scripts
- Config validation: `runner: pi` requires `pi.command`, does not require `codex.command`

### Integration smoke test (manual)

1. Install Pi: `npm install -g @earendil-works/pi-coding-agent`
2. Create a test WORKFLOW.md with `runner: pi`
3. Run: `go run ./cmd/symphony ./test-workflow.md`
4. Observe: issue pickup, Pi subprocess launch, event streaming, completion.

### Acceptance criteria

- `runner: codex` (default) works exactly as before. Zero regression.
- `runner: pi` launches Pi, sends prompts, streams events to the orchestrator.
- Stall detection does not false-positive during Pi compaction or auto-retry.
- Token usage from Pi events appears in the dashboard/snapshot.
- Graceful shutdown sends `abort` before killing Pi.

## Idempotence and Recovery

- All changes are additive. No existing code is removed or renamed.
- The `runner` config defaults to `"codex"`, so existing WORKFLOW.md files work unchanged.
- If a milestone fails partway, re-run tests for that milestone. No cleanup needed.
- The Pi client's `StopSession` is safe to call multiple times (idempotent close).

## Artifacts and Notes

### WORKFLOW.md example for Pi runner

    ---
    runner: pi
    tracker:
      kind: linear
      api_key: $LINEAR_API_KEY
      project_slug: my-project
    pi:
      command: "pi --mode rpc --no-session"
      model: "anthropic/claude-sonnet-4-20250514:high"
      read_timeout_ms: 30000
      turn_timeout_ms: 600000
    ---
    You are working on {{ issue.identifier }}: {{ issue.title }}.
    {{ issue.description }}

## Interfaces and Dependencies

### New types

    // internal/config/config.go
    type PiConfig struct {
        Command       string
        Model         string
        TurnTimeoutMS int
        ReadTimeoutMS int
    }

    // Config gets new fields:
    //   Runner string   // "codex" | "pi"
    //   Pi     PiConfig

### New package

    // internal/pi/client.go
    type Client struct { ... }
    type Session struct { ... }
    type TurnResult struct {
        Status string
        Usage  *internal.TokenUsage
    }
    func NewClient(cfg *config.Config, logger *slog.Logger) *Client
    func (c *Client) StartSession(ctx context.Context, workspace string) (*Session, error)
    func (c *Client) SendPrompt(ctx context.Context, sess *Session, prompt string, onUpdate func(internal.AgentUpdate)) (*TurnResult, error)
    func (c *Client) SetModel(sess *Session, model string) error
    func (c *Client) StopSession(sess *Session) error
    func (c *Client) UpdateConfig(cfg *config.Config)

### New runner

    // internal/runner/pi_runner.go
    type PiRunner struct { ... }
    func NewPiRunner(...) *PiRunner
    func (r *PiRunner) Run(ctx context.Context, issue internal.Issue, attempt *int, updates chan<- internal.AgentUpdate) error
    func (r *PiRunner) UpdatePrompt(prompt string)

### Dependencies

No new external Go dependencies. Pi is launched as a subprocess. The Pi RPC protocol is JSONL — parsed with `encoding/json` (already used).
