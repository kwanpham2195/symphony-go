---
tracker:
  kind: linear
  project_slug: "004d9e34fd6a"
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
  root: ~/code/symphony-go-workspaces
hooks:
  after_create: |
    git clone --depth 50 git@github.com:kwanpham2195/symphony-go.git .
  before_run: |
    git fetch origin main
    git checkout -B main origin/main
    branch_name="${ISSUE_IDENTIFIER:-$(basename $PWD)}"
    branch_name=$(echo "$branch_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9._-]/-/g')
    git checkout -B "$branch_name" main
    go mod download
  timeout_ms: 120000
agent:
  max_concurrent_agents: 2
  max_turns: 20
  max_retry_backoff_ms: 300000
codex:
  command: codex app-server
  approval_policy: never
  thread_sandbox: workspace-write
server:
  port: 8080
---
You are working on a Linear ticket `{{ issue.identifier }}` in the symphony-go repository.

{% if attempt %}
Continuation context:

- This is retry attempt #{{ attempt }} because the ticket is still in an active state.
- Resume from the current workspace state instead of restarting from scratch.
- Do not repeat already-completed work unless needed for new changes.
{% endif %}

## Issue context

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

## Repository

This is a Go project. Key files:
- `AGENTS.md` — build/test commands, architecture, conventions
- `CONTRIBUTING.md` — git flow, branch naming, commit format
- `ARCHITECTURE.md` — system overview, layers, data flow

Read `AGENTS.md` before starting any work.

## Build and test

```bash
make check    # golangci-lint + go test -race
```

All changes must pass `make check` before committing.

## Git workflow

Follow GitHub Flow as documented in `CONTRIBUTING.md`:

1. You are already on a feature branch (created by the `before_run` hook).
2. Make changes, run `make check`.
3. Commit using the `commit` skill (conventional commits, scoped by package).
4. Push and create/update PR using the `push` skill.
5. Attach the PR to the Linear issue using the `linear` skill.
6. Move the issue to `In Progress` when starting work.

## Skills available

Use skills in `.codex/skills/` for git and Linear operations:
- `commit` — create conventional commits
- `push` — push branch and create/update PR
- `pull` — sync with origin/main, resolve conflicts
- `land` — merge PR (when issue reaches Merging state)
- `linear` — Linear GraphQL operations (comments, state changes, PR attachments)
- `debug` — investigate stuck symphony runs

## Rules

1. This is an unattended session. Never ask a human for follow-up actions.
2. Only stop early for true blockers (missing auth, permissions, secrets).
3. Work only in this repository copy. Do not touch other paths.
4. Run `make check` before every commit. Do not commit if it fails.
5. Keep files under ~500 lines. Split when they grow.
6. Add regression tests when fixing bugs.
7. Use interface fakes for orchestrator tests, not mocks.
8. Do not use `git add -f`.
