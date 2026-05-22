---
tracker:
  kind: linear
  project_slug: "f694135aa121"
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
  interval_ms: 5000
workspace:
  root: /tmp/symphony_go_smoke_workspaces
agent:
  max_concurrent_agents: 1
  max_turns: 1
codex:
  command: /Users/kwanpham/Work/symphony-go/testdata/fake-codex/smoke.sh
---
You are working on issue {{ issue.identifier }}: {{ issue.title }}.

{% if issue.description %}
{{ issue.description }}
{% else %}
No description provided.
{% endif %}
