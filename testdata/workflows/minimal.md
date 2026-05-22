---
tracker:
  kind: linear
  project_slug: test-project
  api_key: $LINEAR_API_KEY
polling:
  interval_ms: 10000
workspace:
  root: /tmp/symphony_test_workspaces
agent:
  max_concurrent_agents: 5
  max_turns: 10
codex:
  command: codex app-server
---
You are working on issue {{ issue.identifier }}: {{ issue.title }}.

{% if attempt %}
This is retry attempt #{{ attempt }}.
{% endif %}

Issue description:
{% if issue.description %}
{{ issue.description }}
{% else %}
No description provided.
{% endif %}
