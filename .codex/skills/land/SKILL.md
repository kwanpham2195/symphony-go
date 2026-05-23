---
name: land
description:
  Land a PR by monitoring conflicts, resolving them, waiting for checks, and
  squash-merging when green; use when asked to land, merge, or shepherd a PR to
  completion.
---

# Land

## Goals

- Ensure the PR is conflict-free with main.
- Keep CI green and fix failures when they occur.
- Squash-merge the PR once checks pass.
- Do not yield until the PR is merged; keep the loop running unless blocked.

## Preconditions

- `gh` CLI is authenticated.
- You are on the PR branch with a clean working tree.

## Steps

1. Locate the PR for the current branch.
2. Run the full gate locally before any push:
   ```bash
   make check
   ```
3. If uncommitted changes exist, commit with the `commit` skill and push with the `push` skill.
4. Check mergeability and conflicts against main.
5. If conflicts exist, use the `pull` skill to merge `origin/main` and resolve, then `push` skill to publish.
6. Watch checks until complete:
   ```bash
   gh pr checks --watch
   ```
7. If checks fail, pull logs, fix, commit, push, and re-watch:
   ```bash
   gh pr checks
   gh run view <run-id> --log-failed
   ```
8. When all checks are green, squash-merge:
   ```bash
   pr_title=$(gh pr view --json title -q .title)
   pr_body=$(gh pr view --json body -q .body)
   gh pr merge --squash --subject "$pr_title" --body "$pr_body"
   ```

## Review Handling

- Address all review comments before merging (code fix or explicit pushback with rationale).
- For each comment: accept, clarify, or push back. State the mode before changing code.
- Reply before pushing code changes.

## Failure Handling

- If checks fail, inspect with `gh run view --log-failed`, fix locally, commit, push, re-watch.
- Use judgment on flaky failures (timeouts on one platform) — may proceed.
- If push is rejected, use the `pull` skill first.
- Do not use `--force`; only `--force-with-lease` when history was rewritten.
