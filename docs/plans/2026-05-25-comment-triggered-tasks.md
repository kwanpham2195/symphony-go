# CFW-60: Comment-Triggered Agent Tasks

This ExecPlan is a living document. Keep `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` current as work proceeds.

Plans index: [`docs/plans/README.md`](README.md)

## Purpose / Big Picture

After this change, when a human leaves a comment on an In Review issue, Symphony automatically moves the issue back to In Progress and dispatches the agent with the comment embedded in the prompt. This closes the "review feedback loop" — the most common idle gap in agent-assisted workflows.

Example flow:
1. Agent finishes, pushes PR, moves issue to In Review.
2. Human reviews, comments "fix the failing test in auth_test.go."
3. Symphony detects the comment, transitions the issue to In Progress, dispatches the agent.
4. Agent reads the comment, fixes the test, pushes, moves issue back to In Review.

Also includes a GC simplification: stop treating non-terminal, non-active issues as orphans. Only GC terminal issues. This prevents the workspace from being deleted while an issue sits in In Review.

## Progress

- [x] Milestone 1: GC simplification (remove orphan logic)
- [x] Milestone 2: Comment type + tracker interface + Linear implementation
- [x] Milestone 3: Orchestrator comment polling loop
- [x] Milestone 4: Prompt template comment bindings
- [ ] Milestone 5: End-to-end verification (manual, requires live Linear)

## Surprises & Discoveries

(None yet.)

## Decision Log

- **Only trigger on In Review comments.** Active issues already have a running agent. Terminal issues are done. In Review is the only state where a comment means "wake up." Date: 2026-05-25.
- **Auto-transition to In Progress on dispatch.** The agent shouldn't work in In Review state — that state means "waiting for human." Moving to In Progress keeps the state machine consistent. Date: 2026-05-25.
- **Polling, not webhooks.** Symphony is a local CLI with no inbound HTTP. Webhooks need a public endpoint. Polling fits our architecture. Date: 2026-05-25.
- **GC: only terminal, drop orphans.** In Review workspaces were being classified as orphans and deleted after 48h. This breaks comment-triggered re-dispatch. Simpler rule: only GC terminal issues. Date: 2026-05-25.
- **Anti-loop via viewer ID.** Comments posted by the agent (via the Linear API key) come from a specific user. Query `{ viewer { id } }` at startup, skip comments from that user. Date: 2026-05-25.
- **Per-issue last-handled comment ID.** `lastCommentCheck` alone is not enough. After an agent finishes and the issue returns to In Review, a restart within the lookback window would re-dispatch the same comment. Store the last-handled comment ID per issue in orchestrator state. Skip any comment with `ID <= lastHandled`. Date: 2026-05-25.
- **Aggregate multiple comments per dispatch.** If several reviewers comment between polls, concatenate all new comments into a single dispatch rather than silently dropping extras. Template binding is `comments` (list). Date: 2026-05-25.
- **Comment data rides on Issue, not AgentRunner.** Adding `*Comment` to `AgentRunner.Run` would break the interface and all fakes. Instead, add `TriggerComments []Comment` to `internal.Issue`. The field flows through existing dispatch paths untouched. Date: 2026-05-25.
- **Separate TrackerWriter interface.** The `Tracker` interface is documented as read-only. `TransitionIssueState` and `ViewerID` go into a new `TrackerWriter` interface so the read contract stays clean. Date: 2026-05-25.
- **State resolution belongs in the tracker, not the orchestrator.** The orchestrator needs to transition In Review → In Progress but should not know how a specific tracker maps state names to IDs. `TrackerWriter.ResolveStateID(ctx, stateName)` lets each implementation handle this: Linear queries team workflow states for UUIDs, GitHub maps to open/closed or Projects v2 option IDs. The orchestrator calls `ResolveStateID` at startup and caches the result. Date: 2026-05-25.
- **Extract comment polling to its own file.** `orchestrator.go` is already ~890 lines. Comment polling lives in `internal/orchestrator/comments.go` to stay under the 500 LOC guideline. Date: 2026-05-25.

## Outcomes & Retrospective

(To be filled as milestones complete.)

## Context and Orientation

### Current Architecture

The orchestrator polls Linear for issues in active states (Todo, In Progress) on a fixed interval. It dispatches agents for new active issues, reconciles running agents against state changes, and retries on failure. Once an issue moves to In Review, the orchestrator stops the agent and releases the claim.

Key files:
- `internal/orchestrator/orchestrator.go` — poll loop, dispatch, reconciliation (~890 LOC)
- `internal/tracker/tracker.go` — `Tracker` interface (read-only)
- `internal/tracker/linear/client.go` — Linear GraphQL client
- `internal/runner/runner.go` — agent runner (workspace + prompt + codex)
- `internal/workflow/prompt.go` — Liquid prompt rendering
- `internal/workspace/gc.go` — workspace garbage collection
- `internal/issue.go` — `Issue` type
- `internal/config/config.go` — typed config

### Linear Comment API

Linear exposes `issue.comments` with a `CommentFilter` that supports `createdAt` filtering. Comments have `user { id name }` and `botActor { id name }` fields. The `{ viewer { id } }` query returns the API key owner's user ID.

### GC Current Behavior

The GC has three buckets: terminal (remove after TTL), active (never touch), and "everything else" (orphan — remove after orphan TTL). In Review falls into "everything else."

## Plan of Work

### Part A: GC Simplification

Remove the orphan classification from the GC. The new logic:
- Terminal issue workspace + past TTL → remove
- Terminal issue workspace + past artifact TTL → clean artifacts
- Everything else → skip

Remove `OrphanTTLMS` from config defaults and the GCConfig struct. Remove `handleOrphan`, `OrphanDirs` from `GCResult`, and the orphan branch in `Collect`. Update all tests that reference orphan behavior.

### Part B: Comment Trigger

**B1. New types**

Add `Comment` struct in `internal/comment.go`:

    type Comment struct {
        ID        string
        Body      string
        IssueID   string
        UserID    string
        UserName  string
        BotActor  bool
        CreatedAt time.Time
        ParentID  string
    }

Add a field to `internal.Issue`:

    TriggerComments []Comment `json:"trigger_comments,omitempty"`

This carries comment context through the existing dispatch path without changing the `AgentRunner` interface or any fakes.

**B2. Tracker and TrackerWriter interfaces**

The existing `Tracker` interface is documented as read-only. Keep it that way. Add `FetchRecentComments` to `Tracker` (it is a read):

    FetchRecentComments(ctx context.Context, issueIDs []string, since time.Time) (map[string][]Comment, error)

Create a new `TrackerWriter` interface in `internal/tracker/writer.go`:

    type TrackerWriter interface {
        ViewerID(ctx context.Context) (string, error)
        ResolveStateID(ctx context.Context, stateName string) (string, error)
        TransitionIssueState(ctx context.Context, issueID string, stateID string) error
    }

`ResolveStateID` maps a human-readable state name (e.g. "In Progress") to a platform-specific ID. This keeps the orchestrator tracker-agnostic — it never knows whether the ID is a Linear UUID, a GitHub Projects option ID, or just "open".

`FetchRecentComments` returns a map of issueID → comments created after `since`. `ViewerID` returns the API key owner's user ID for anti-loop filtering. The Linear client implements both interfaces.

**B3. Linear implementation**

Add a GraphQL query to fetch comments on multiple issues filtered by `createdAt > since`. Add `ViewerID` query: `{ viewer { id } }`.

**B4. Orchestrator comment loop**

All comment polling logic lives in a new file `internal/orchestrator/comments.go` to keep `orchestrator.go` under the 500 LOC guideline (it is already ~890 lines).

Add to the orchestrator struct:
- `trackerWriter tracker.TrackerWriter` — injected via `Deps`
- `viewerID string` — populated at startup via `TrackerWriter.ViewerID()`
- `lastCommentCheck time.Time` — initialized to `now - lookback`
- `lastHandledComment map[string]string` — issueID → last handled comment ID
- `commentTickCounter int` — only check comments every N ticks
- `checkComments(ctx)` method called from `Tick()` **before** `dispatch`

Ordering in `Tick()`: `reconcile` → `checkComments` → `fetch` → `dispatch`. This prevents a race where `checkComments` transitions an issue to In Progress and the same tick's `dispatch` picks it up without comment context.

`checkComments` flow:
1. Collect issue IDs in In Review state. `FetchIssuesByStates` already exists on the Tracker interface — call it with `[]string{"In Review"}`.
2. Call `FetchRecentComments(ctx, issueIDs, lastCommentCheck)`.
3. For each issue, collect all new comments: skip if `comment.UserID == viewerID` or `comment.BotActor`. Skip if `comment.ID == lastHandledComment[issueID]`. Skip if issue is already running or claimed.
4. **Claim the issue first** (`claimed[issueID] = true`) before calling `TransitionIssueState`. This prevents the regular dispatch path from grabbing the issue if the transition completes before `checkComments` finishes.
5. Transition issue to In Progress via `TrackerWriter.TransitionIssueState`.
6. Attach all collected comments to the issue as `issue.TriggerComments` and dispatch with `attempt=nil`.
7. Update `lastHandledComment[issueID]` to the latest comment ID.
8. If transition or dispatch fails, release the claim.

Multiple comments: if several comments arrived between polls, all are collected, attached to `Issue.TriggerComments`, and rendered together in the prompt. No comments are silently dropped.

**B5. State transition**

`TransitionIssueState` and `ResolveStateID` live on the `TrackerWriter` interface (see B2). The Linear client implements `ResolveStateID` by querying the team's workflow states and matching by name. A future GitHub tracker would map state names to open/closed or Projects v2 field option IDs.

The orchestrator calls `ResolveStateID(ctx, "In Progress")` once at startup and caches the returned ID. It passes that cached ID to `TransitionIssueState` on each comment-triggered dispatch. The orchestrator has no platform-specific state resolution logic.

**B6. Prompt template**

The comment data flows through `Issue.TriggerComments`. No change to `RenderPrompt` signature — it already receives the full `Issue`. Extend `buildBindings` in `prompt.go` to include comments from the issue:

    comments[]  — list of comment objects
    comments[].body
    comments[].user_name
    comments[].created_at

The workflow template can use:

    {% if comments %}
    Reviewer feedback on {{ issue.identifier }}: {{ issue.title }}:
    {% for c in comments %}

    **{{ c.user_name }}** ({{ c.created_at }}):
    > {{ c.body }}
    {% endfor %}

    Address all feedback above.
    {% else %}
    ... existing prompt ...
    {% endif %}

This handles one or many comments. The `AgentRunner` interface stays unchanged.

**B7. Config**

Add to `config.go`:

    type CommentsConfig struct {
        Enabled           bool
        PollIntervalTicks int    // check every N ticks (default: 3)
        LookbackMS        int    // startup lookback window (default: 300000 = 5min)
        ReviewState       string // state to watch (default: "In Review")
    }

## Milestones

### Milestone 1: GC Simplification

**Goal:** Remove orphan classification. Only GC terminal workspaces.

**Work:**
- Remove `OrphanTTLMS` from `GCConfig` and `applyDefaults`
- Remove `OrphanDirs` from `GCResult`
- Remove `handleOrphan` method
- Remove orphan branch in `Collect` — the `default` case becomes `continue`
- Update/remove orphan-related tests
- Update `WORKFLOW.example.md` if it documents orphan TTL

**Commands:**

    cd /Users/kwanpham/Work/symphony-go
    go test ./internal/workspace/... -run TestGC
    make check

**Expected result:** All GC tests pass. No orphan removal behavior. Terminal workspaces still cleaned after TTL.

**Risk reduction:** Eliminates the risk of In Review workspaces being deleted.

### Milestone 2: Comment Type + Tracker Interface + Linear Implementation

**Goal:** Symphony can fetch recent comments from Linear for a set of issue IDs.

**Work:**
- Create `internal/comment.go` with `Comment` struct
- Add `TriggerComments []Comment` field to `internal.Issue`
- Add `FetchRecentComments` to `Tracker` interface (read method)
- Create `TrackerWriter` interface in `internal/tracker/writer.go` with `ViewerID`, `ResolveStateID`, and `TransitionIssueState`
- Implement both interfaces in `internal/tracker/linear/client.go`
- Add fixture-driven tests in `internal/tracker/linear/`
- Stub methods on orchestrator fakes

**Commands:**

    go test ./internal/tracker/linear/... -v
    make check

**Expected result:** Linear client can query comments filtered by createdAt and return parsed `Comment` structs. `ViewerID` returns the API key owner's ID. `ResolveStateID("In Progress")` returns the correct Linear workflow state UUID. `TransitionIssueState` can move an issue.

### Milestone 3: Orchestrator Comment Polling

**Goal:** The orchestrator detects new comments on In Review issues, transitions them to In Progress, and dispatches the agent.

**Work:**
- Add `CommentsConfig` to config
- Add `TrackerWriter` to orchestrator `Deps`
- Create `internal/orchestrator/comments.go` (new file, keeps orchestrator.go under 500 LOC)
- Add comment polling state (`viewerID`, `lastCommentCheck`, `lastHandledComment`, counter)
- Add `checkComments(ctx)` method with claim-before-transition ordering
- Call from `Tick()` **before** `dispatch`, gated on `commentTickCounter`
- Add orchestrator tests with `fakeTracker` + `fakeTrackerWriter` returning comments
- Test: multiple comments on same issue → all appear in `TriggerComments`
- Test: restart within lookback window → `lastHandledComment` prevents re-dispatch

**Commands:**

    go test ./internal/orchestrator/... -v -run TestComment
    make check

**Expected result:** Fake tracker returns comments on an In Review issue → orchestrator claims, transitions state, dispatches agent with all comments attached. Anti-loop: comment from viewerID is skipped. Duplicate: same comment ID is not re-dispatched.

### Milestone 4: Prompt Template Comment Bindings

**Goal:** The agent receives the triggering comments in its prompt.

**Work:**
- Extend `buildBindings` in `prompt.go` to read `issue.TriggerComments` and expose `comments` list in Liquid
- No change to `RenderPrompt` signature or `AgentRunner` interface
- Add render tests: zero comments (falsy), one comment, multiple comments

**Commands:**

    go test ./internal/workflow/... -v
    make check

**Expected result:** `{% if comments %}` block renders when comments are present. Each comment's body, user name, and timestamp are available. Empty `TriggerComments` → `comments` is nil/falsy in Liquid.

### Milestone 5: End-to-End Verification

**Goal:** Full flow works against real Linear (manual test).

**Work:**
- Add `comments` section to `WORKFLOW.example.md`
- Manual test: create an issue, move to In Review, post a comment, observe Symphony pick it up
- Verify anti-loop: agent's own comments don't re-trigger

**Commands:**

    LINEAR_API_KEY=... go run ./cmd/symphony ./WORKFLOW.md

**Expected result:** Symphony detects the comment, logs the transition, dispatches the agent with the comment in the prompt.

## Validation and Acceptance

1. `make check` passes (lint + all tests).
2. GC no longer removes In Review workspaces regardless of age.
3. A comment on an In Review issue triggers agent dispatch within `poll_interval * poll_interval_ticks` milliseconds.
4. The agent prompt contains the comment body and author name.
5. Agent-authored comments (matching viewer ID) do not trigger re-dispatch.
6. Bot-authored comments do not trigger re-dispatch.
7. An issue already running is not re-dispatched on comment.
8. The issue transitions from In Review to In Progress before dispatch.
9. Multiple comments between polls are all included in the prompt.
10. Restarting Symphony within the lookback window does not re-dispatch an already-handled comment.
11. The `Tracker` interface remains read-only; write operations use `TrackerWriter`.

## Idempotence and Recovery

- Comment polling uses two layers of dedup: (1) `lastCommentCheck` timestamp filters the GraphQL query, (2) `lastHandledComment[issueID]` stores the ID of the most recent comment already dispatched for each issue. On restart, `lastCommentCheck` resets to `now - lookback`, but `lastHandledComment` is also reset — so the orchestrator may re-fetch old comments. However, if the issue is already in In Progress or active (agent running), the comment is skipped. The remaining edge case is: agent finished, issue is back in In Review, same comment re-fetched. This is handled by persisting `lastHandledComment` to a JSON file in the workspace root (`$WORKSPACE_ROOT/.symphony_comment_state.json`) on each update and loading it on startup.
- State transition is idempotent — calling `issueUpdate` with the same stateId twice is a no-op in Linear.
- If the transition succeeds but dispatch fails, the claim is released. The issue is in In Progress with no agent. The next regular poll tick picks it up as a candidate issue and dispatches normally (without comment context — acceptable fallback).

## Artifacts and Notes

(To be filled during implementation.)

## Interfaces and Dependencies

### New Types

    // internal/comment.go
    type Comment struct {
        ID, Body, IssueID, UserID, UserName, ParentID string
        BotActor                                       bool
        CreatedAt                                      time.Time
    }

### Extended Tracker Interface (read-only, existing contract preserved)

    // internal/tracker/tracker.go
    type Tracker interface {
        FetchCandidateIssues(ctx context.Context) ([]internal.Issue, error)
        FetchIssuesByStates(ctx context.Context, states []string) ([]internal.Issue, error)
        FetchIssueStatesByIDs(ctx context.Context, ids []string) ([]internal.Issue, error)
        FetchRecentComments(ctx context.Context, issueIDs []string, since time.Time) (map[string][]internal.Comment, error)
    }

### New TrackerWriter Interface

    // internal/tracker/writer.go
    type TrackerWriter interface {
        ViewerID(ctx context.Context) (string, error)
        ResolveStateID(ctx context.Context, stateName string) (string, error)
        TransitionIssueState(ctx context.Context, issueID string, stateID string) error
    }

### New Config

    // internal/config/config.go
    type CommentsConfig struct {
        Enabled           bool
        PollIntervalTicks int
        LookbackMS        int
        ReviewState       string
    }

### Dependencies

No new external dependencies. Uses existing `github.com/osteele/liquid` for prompt rendering and Linear GraphQL API.
