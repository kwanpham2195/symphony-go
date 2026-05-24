---
name: push
description:
  Push current branch to origin and create or update the PR; use when asked to
  push, publish updates, or create a pull request.
---

# Push

## Prerequisites

- `gh` CLI authenticated (`gh auth status`).

## Steps

1. Identify current branch:
   ```bash
   branch=$(git branch --show-current)
   ```
2. Run validation before pushing:
   ```bash
   make check
   ```
3. Push to origin:
   ```bash
   git push -u origin HEAD
   ```
4. If push is rejected (non-fast-forward), use the `pull` skill to sync, then retry.
5. If push fails due to auth/permissions, stop and surface the error.
6. Ensure a PR exists:
   ```bash
   pr_state=$(gh pr view --json state -q .state 2>/dev/null || true)
   if [ "$pr_state" = "MERGED" ] || [ "$pr_state" = "CLOSED" ]; then
     echo "Branch tied to closed PR; create a new branch + PR."
     exit 1
   fi

   if [ -z "$pr_state" ]; then
     gh pr create --title "<clear title>"
   else
     gh pr edit --title "<updated title if scope changed>"
   fi
   ```
7. Write/update PR body with a clear description of the change.
8. Reply with the PR URL:
   ```bash
   gh pr view --json url -q .url
   ```
9. Move the Linear issue to **In Review** using the `linear` skill so the
   orchestrator stops re-dispatching.

## Notes

- Do not use `--force`; only `--force-with-lease` when history was rewritten.
- On branch updates, reconsider whether the PR title still matches the current scope.
- If the remote moved, use the `pull` skill first — do not change remotes or protocols as a workaround.
