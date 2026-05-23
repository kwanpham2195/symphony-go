---
name: debug
description:
  Investigate stuck runs and execution failures by tracing Symphony logs
  with issue/session identifiers; use when runs stall, retry repeatedly, or
  fail unexpectedly.
---

# Debug

## Goals

- Find why a run is stuck, retrying, or failing.
- Correlate Linear issue identity to a Codex session quickly.

## Correlation Keys

- `issue_identifier`: human ticket key (e.g., `CFW-44`)
- `issue_id`: Linear UUID
- `session_id`: Codex thread-turn pair (`<thread_id>-<turn_id>`)

## Quick Triage

1. Find recent logs for the ticket by `issue_identifier`.
2. Extract `session_id` from matching lines.
3. Trace that `session_id` across start, stream, completion/failure, and stall handling.
4. Classify: timeout/stall, app-server startup failure, turn failure, or orchestrator retry loop.

## Commands

```bash
# Search by ticket key
rg -n "issue_identifier=CFW-44" /path/to/logs

# Pull session IDs for that ticket
rg -o "session_id=[^ ;]+" /path/to/logs | sort -u

# Trace one session end-to-end
rg -n "session_id=<thread>-<turn>" /path/to/logs

# Focus on stuck/retry signals
rg -n "stalled|scheduling retry|turn_timeout|turn_failed|session failed" /path/to/logs
```

## Classification

| Pattern | Cause |
|---------|-------|
| `stalled.*restarting with backoff` | No codex activity beyond stall_timeout_ms |
| `session failed` | App-server startup error |
| `turn_failed` / `turn_cancelled` | Turn execution failure |
| `agent failed.*scheduling retry` | Worker error, will retry with backoff |
| Repeated dispatch + immediate failure | Config error or workspace issue |

## Dashboard

If the HTTP server is running, check `/api/v1/state` for current running sessions and retry queue state. Check `/api/v1/issues/<identifier>` for issue-specific debug info.
