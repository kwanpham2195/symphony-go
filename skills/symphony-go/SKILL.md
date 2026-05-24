---
name: symphony-go
description: Set up, configure, run, and troubleshoot symphony-go — an autonomous agent orchestrator that polls Linear issues and runs Codex sessions. Use when the user asks to install symphony, create a WORKFLOW.md, configure Linear integration, start the orchestrator, debug stuck agents, fix failing hooks, tune retry/backoff, set up workspaces, or any task related to running symphony-go in a repository.
---

# symphony-go

Set up, configure, run, and troubleshoot [symphony-go](https://github.com/kwanpham2195/symphony-go) — a Go service that turns Linear issues into autonomous Codex coding sessions.

## What symphony-go does

Symphony polls a Linear project board, picks up issues in active states (Todo, In Progress), creates isolated workspace directories, and launches Codex app-server agents to work on each issue. When an agent finishes, it opens a PR. Symphony handles retries, backoff, concurrency limits, and workspace cleanup.

## Prerequisites

Before setting up symphony-go, make sure these are installed:

1. **Codex CLI** — `npm install -g @openai/codex` (verify: `codex --version`)
2. **Linear API key** — create at https://linear.app/settings/api, export as `LINEAR_API_KEY`
3. **Linear project** — create a project in Linear, note the project **slug** (short ID in the project URL)
4. **Go 1.25+** — only if building from source; pre-built binaries at https://github.com/kwanpham2195/symphony-go/releases

## Setup

### Step 1: Install symphony-go

```bash
# Option A: go install
go install github.com/kwanpham2195/symphony-go/cmd/symphony@latest

# Option B: download binary
# https://github.com/kwanpham2195/symphony-go/releases

# Option C: build from source
git clone https://github.com/kwanpham2195/symphony-go.git
cd symphony-go && make build
```

### Step 2: Create WORKFLOW.md

Create a `WORKFLOW.md` file in the target repository. This file has two parts:

- **YAML front matter** — runtime configuration (tracker, workspace, hooks, codex settings)
- **Body** — a Liquid template rendered as the agent prompt for each issue

Minimal example:

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

You are working on Linear ticket {{ issue.identifier }}: {{ issue.title }}.

{{ issue.description }}
```

### Step 3: Validate and run

```bash
export LINEAR_API_KEY=lin_api_...

# Validate the workflow file
symphony --validate-only ./WORKFLOW.md

# Single poll cycle (dry run)
symphony --once ./WORKFLOW.md

# Start the service with dashboard
symphony --port 8080 ./WORKFLOW.md
```

## WORKFLOW.md Configuration

### Tracker

```yaml
tracker:
  kind: linear                    # required, only "linear" supported
  project_slug: "abc123"          # required, Linear project slug
  api_key: $LINEAR_API_KEY        # env var reference or literal token
  active_states:                  # states to pick up (default below)
    - Todo
    - In Progress
  terminal_states:                # states that trigger cleanup (default below)
    - Done
    - Closed
    - Cancelled
    - Canceled
    - Duplicate
```

### Workspace and hooks

```yaml
workspace:
  root: ~/code/my-workspaces      # default: $TMPDIR/symphony_workspaces

hooks:
  after_create: |                  # runs once after workspace dir is created
    git clone --depth 1 https://github.com/org/repo .
  before_run: |                    # runs before each agent attempt
    git fetch origin main
    git checkout -B main origin/main
  after_run: |                     # runs after each agent attempt (failure ignored)
    echo "attempt finished"
  before_remove: |                 # runs before workspace removal (failure ignored)
    echo "cleaning up"
  timeout_ms: 60000                # hook timeout (default: 60s)
```

Hook tips:
- `after_create` is fatal — if it fails, the workspace is removed and the issue retries.
- `before_run` is fatal — if it fails, the attempt fails and retries with backoff.
- `after_run` and `before_remove` are best-effort — failures are logged but ignored.
- Use `before_run` to create feature branches per issue. The env var `ISSUE_IDENTIFIER` is available.

### Agent

```yaml
agent:
  max_concurrent_agents: 5         # global concurrency limit (default: 10)
  max_concurrent_agents_by_state:  # per-state limits
    Todo: 2
    In Progress: 3
  max_turns: 20                    # max turns per session (default: 20)
  max_retry_backoff_ms: 300000     # max retry delay, 5 min (default: 300000)
```

### Codex

```yaml
codex:
  command: codex app-server        # the command symphony launches
  approval_policy: never           # "never" = auto-approve everything
  thread_sandbox: workspace-write  # or "danger-full-access" for git write access
  turn_timeout_ms: 3600000         # 1 hour per turn (default)
  stall_timeout_ms: 300000         # 5 min stall detection (default)
```

**Sandbox modes:**
- `workspace-write` — agent can write to workspace files but NOT `.git/`. Safe default.
- `danger-full-access` — agent can write everywhere including `.git/`. Required if the agent needs to `git commit/push`. Use in trusted environments only.
- `read-only` — agent can only read. Useful for analysis-only tasks.

### Server

```yaml
server:
  port: 8080                       # enables HTTP dashboard
```

### Workers (advanced)

```yaml
worker:
  ssh_hosts:                       # remote workers via SSH
    - user@host1
    - user@host2
  max_concurrent_agents_per_host: 3
```

## Prompt template (Liquid)

The body of WORKFLOW.md is a [Liquid](https://shopify.github.io/liquid/) template. Available variables:

| Variable | Type | Description |
|----------|------|-------------|
| `issue.identifier` | string | Issue key, e.g. `ENG-42` |
| `issue.title` | string | Issue title |
| `issue.description` | string | Issue description (may be empty) |
| `issue.state` | string | Current workflow state |
| `issue.labels` | string | Comma-separated labels |
| `issue.url` | string | Linear issue URL |
| `issue.blockers` | array | Related/blocking issues |
| `attempt` | int/nil | Retry attempt number (nil on first run) |

Example with retry context and blockers:

```liquid
You are working on {{ issue.identifier }}: {{ issue.title }}.

{% if attempt %}
This is retry attempt #{{ attempt }}. Resume from current workspace state.
{% endif %}

{{ issue.description }}

{% if issue.blockers.size > 0 %}
Related issues:
{% for b in issue.blockers %}
- {{ b.identifier }}: {{ b.title }}
{% endfor %}
{% endif %}
```

**Liquid gotcha:** empty Go strings are truthy in Liquid. Symphony maps them to `nil` so `{% if issue.description %}` works as expected.

## Dashboard

Start with `--port 8080`. Endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | HTML dashboard with auto-refresh |
| `/api/v1/state` | GET | Full JSON snapshot of all running/queued/retrying issues |
| `/api/v1/issues/{identifier}` | GET | Single issue debug state |
| `/api/v1/refresh` | POST | Trigger immediate poll cycle |

## Troubleshooting

### Agent can't git commit/push

**Symptom:** `Unable to create '.git/index.lock': Operation not permitted`

**Cause:** The sandbox mode is `workspace-write`, which blocks `.git/` writes.

**Fix:** Set `thread_sandbox: danger-full-access` in the codex config:

```yaml
codex:
  thread_sandbox: danger-full-access
```

### Agent not picking up issues

Check these in order:

1. **Project slug** — verify `tracker.project_slug` matches your Linear project. The slug is the short ID in the project settings URL.
2. **Active states** — verify `tracker.active_states` matches your Linear workflow state names exactly (case-sensitive).
3. **API key** — verify `LINEAR_API_KEY` is set and valid. Test with:
   ```bash
   curl -s -H "Authorization: $LINEAR_API_KEY" \
     -H "Content-Type: application/json" \
     -d '{"query":"{ viewer { id name } }"}' \
     https://api.linear.app/graphql
   ```
4. **Concurrency limit** — check if `max_concurrent_agents` is already reached.
5. **Dashboard** — check `http://localhost:8080/api/v1/state` for the orchestrator's view.

### Hook failures

**Symptom:** Issue keeps retrying, workspace gets recreated each time.

**Cause:** `after_create` or `before_run` hook is failing.

**Debug:** Check symphony logs for hook stderr output. Common issues:
- Git clone URL wrong or SSH key not available
- `npm install` / `go mod download` fails (network, auth)
- Branch doesn't exist

**Fix:** Test hooks manually:
```bash
mkdir /tmp/test-workspace && cd /tmp/test-workspace
# paste your after_create hook commands here
```

### Agent stuck / no progress

1. Check the dashboard for turn count and status.
2. Check `stall_timeout_ms` — default is 5 minutes. If the agent produces no output for this long, the turn is killed.
3. Check `max_turns` — if reached, the session ends.
4. Use `POST /api/v1/refresh` to trigger a re-poll.

### Retry backoff too aggressive

Backoff is exponential: 1s, 2s, 4s, 8s, ... up to `max_retry_backoff_ms` (default 5 min).

To reset, move the issue to a terminal state (Done/Cancelled) and back to Todo.

### Validate without running

```bash
symphony --validate-only ./WORKFLOW.md
```

This parses the workflow, resolves env vars, and validates all config fields without connecting to Linear or starting the orchestrator.

## Full WORKFLOW.md example (production-ready)

```markdown
---
tracker:
  kind: linear
  project_slug: "your-project-slug"
  api_key: $LINEAR_API_KEY
  active_states:
    - Todo
    - In Progress
  terminal_states:
    - Done
    - Closed
    - Cancelled
    - Canceled
    - Duplicate
polling:
  interval_ms: 30000
workspace:
  root: ~/code/my-workspaces
hooks:
  after_create: |
    git clone --depth 50 git@github.com:your-org/your-repo.git .
  before_run: |
    git fetch origin main
    git checkout -B main origin/main
    branch_name="${ISSUE_IDENTIFIER:-$(basename $PWD)}"
    branch_name=$(echo "$branch_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9._-]/-/g')
    git checkout -B "$branch_name" main
  timeout_ms: 120000
agent:
  max_concurrent_agents: 5
  max_turns: 20
  max_retry_backoff_ms: 300000
codex:
  command: codex app-server
  approval_policy: never
  thread_sandbox: danger-full-access
server:
  port: 8080
---

You are working on Linear ticket {{ issue.identifier }}: {{ issue.title }}.

{% if attempt %}
This is retry attempt #{{ attempt }}. Resume from current workspace state.
{% endif %}

## Issue

- Identifier: {{ issue.identifier }}
- Title: {{ issue.title }}
- Status: {{ issue.state }}
- Labels: {{ issue.labels }}
- URL: {{ issue.url }}

## Description

{% if issue.description %}
{{ issue.description }}
{% else %}
No description provided.
{% endif %}

## Instructions

1. Read AGENTS.md before starting.
2. Run tests before committing.
3. Create a PR and link it to the Linear issue.
4. This is unattended — never ask a human for follow-up.
```
