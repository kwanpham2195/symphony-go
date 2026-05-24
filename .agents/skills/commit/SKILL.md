---
name: commit
description:
  Create a well-formed git commit from current changes using session history for
  rationale and summary; use when asked to commit, prepare a commit message, or
  finalize staged work.
---

# Commit

## Goals

- Produce a commit that reflects the actual code changes and the session context.
- Follow conventional commits (type prefix, short subject, wrapped body).
- Include both summary and rationale in the body.

## Steps

1. Read session history to identify scope, intent, and rationale.
2. Inspect changes (`git status`, `git diff`, `git diff --staged`).
3. Stage intended changes (`git add -A`) after confirming scope.
4. Sanity-check newly added files; flag build artifacts, logs, or temp files before committing.
5. Choose a conventional type and optional scope (e.g., `feat(orchestrator):`, `fix(codex):`, `refactor(config):`).
6. Write subject line in imperative mood, <= 72 characters, no trailing period.
7. Write a body with:
   - Summary of key changes (what changed).
   - Rationale and trade-offs (why it changed).
   - Tests or validation run (or note if not run).
8. Wrap body lines at 72 characters.
9. Create commit with `git commit -F <tempfile>` (avoid `-m` with `\n`).
10. Verify staged diff matches the commit message before committing.

## Validation

Run before committing:

```bash
make check
```

## Template

```
<type>(<scope>): <short summary>

Summary:
- <what changed>

Rationale:
- <why>

Tests:
- <command or "not run (reason)">
```
