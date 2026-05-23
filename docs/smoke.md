# Smoke Test Guide

Run a real end-to-end test against Linear with a fake codex command. This proves the full loop without side effects in your codebase.

## Prerequisites

- `LINEAR_API_KEY` set in your environment
- A Linear team (this guide uses team key `CFW` as an example)
- Go installed

## 1. Create a Linear project

```bash
curl -s -X POST https://api.linear.app/graphql \
  -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation($input: ProjectCreateInput!) { projectCreate(input: $input) { success project { id name slugId } } }",
    "variables": {
      "input": {
        "name": "symphony-go-smoke",
        "teamIds": ["YOUR_TEAM_ID"]
      }
    }
  }'
```

To find your team ID:

```bash
curl -s -X POST https://api.linear.app/graphql \
  -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query":"query { teams { nodes { id name key } } }"}' | python3 -m json.tool
```

Save the `slugId` from the response — you need it for the workflow file.

## 2. Create a test issue

```bash
curl -s -X POST https://api.linear.app/graphql \
  -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { id identifier title state { name } } } }",
    "variables": {
      "input": {
        "title": "Smoke test: echo hello from symphony-go",
        "description": "Create a file called hello.txt with the contents hello from symphony-go.",
        "teamId": "YOUR_TEAM_ID",
        "projectId": "YOUR_PROJECT_ID"
      }
    }
  }'
```

Save the issue `id` from the response.

## 3. Move the issue to Todo

The issue starts in Backlog. Move it to Todo so symphony picks it up.

First, find the Todo state ID for your team:

```bash
curl -s -X POST https://api.linear.app/graphql \
  -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query":"query { workflowStates(filter: { team: { key: { eq: \"YOUR_TEAM_KEY\" } } }) { nodes { id name } } }"}' | python3 -m json.tool
```

Then update the issue:

```bash
curl -s -X POST https://api.linear.app/graphql \
  -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success issue { identifier state { name } } } }",
    "variables": {
      "id": "YOUR_ISSUE_ID",
      "input": { "stateId": "YOUR_TODO_STATE_ID" }
    }
  }'
```

## 4. Write a smoke workflow

Create `smoke-test.md` (or use `testdata/workflows/smoke.md`):

```markdown
---
tracker:
  kind: linear
  project_slug: "YOUR_PROJECT_SLUG"
  api_key: $LINEAR_API_KEY
polling:
  interval_ms: 5000
workspace:
  root: /tmp/symphony_go_smoke_workspaces
agent:
  max_concurrent_agents: 1
  max_turns: 1
codex:
  command: ./testdata/fake-codex/smoke.sh
---
You are working on issue {{ issue.identifier }}: {{ issue.title }}.

{% if issue.description %}
{{ issue.description }}
{% else %}
No description provided.
{% endif %}
```

The fake codex script (`testdata/fake-codex/smoke.sh`) completes the app-server handshake and writes `hello.txt` in the workspace. No real codex or model calls.

## 5. Run symphony

Validate first:

```bash
go run ./cmd/symphony --validate-only ./smoke-test.md
```

Then run the service (Ctrl+C to stop):

```bash
go run ./cmd/symphony ./smoke-test.md
```

You should see logs like:

```
level=INFO msg="orchestrator starting" poll_interval_ms=5000 max_concurrent_agents=1
level=INFO msg="dispatching issue" issue_id=... issue_identifier=CFW-43
level=INFO msg="turn completed" issue_identifier=CFW-43 turn=1 session_id=smoke-thread-001-smoke-turn-001
level=INFO msg="agent completed; scheduling continuation check" ...
```

The service will keep re-dispatching (issue is still in Todo). Press Ctrl+C after a few cycles.

## 6. Verify the workspace

```bash
cat /tmp/symphony_go_smoke_workspaces/CFW-43/hello.txt
```

Expected output:

```
hello from symphony-go
smoke test completed at 2026-05-22T14:37:13Z
```

## 7. Clean up

Move the issue to Done so it stops being eligible:

```bash
curl -s -X POST https://api.linear.app/graphql \
  -H "Authorization: $LINEAR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success } }",
    "variables": {
      "id": "YOUR_ISSUE_ID",
      "input": { "stateId": "YOUR_DONE_STATE_ID" }
    }
  }'
```

Remove the workspace:

```bash
rm -rf /tmp/symphony_go_smoke_workspaces
```

## What the smoke test proves

- Symphony fetches candidate issues from Linear via GraphQL
- Issue filtering by project slug and active states works
- Workspace is created under the configured root with sanitized identifier
- Fake codex app-server handshake completes (initialize, thread/start, turn/start)
- Turn completes and agent runner exits normally
- Continuation retry fires and re-dispatches while issue stays active
- Workspace persists across runs (hello.txt is overwritten each time, not lost)
- No paths outside the workspace root are touched
