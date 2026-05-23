---
name: pull
description:
  Pull latest origin/main into the current branch and resolve merge conflicts.
  Use when Codex needs to sync a feature branch with origin before pushing.
---

# Pull

## Steps

1. Verify git status is clean (or commit/stash first).
2. Enable rerere:
   ```bash
   git config rerere.enabled true
   git config rerere.autoupdate true
   ```
3. Fetch latest refs:
   ```bash
   git fetch origin
   ```
4. Sync remote feature branch first:
   ```bash
   git pull --ff-only origin $(git branch --show-current)
   ```
5. Merge main:
   ```bash
   git -c merge.conflictstyle=zdiff3 merge origin/main
   ```
6. If conflicts, resolve them (see guidance below), then:
   ```bash
   git add <files>
   git merge --continue
   ```
7. Run validation:
   ```bash
   make check
   ```
8. Summarize: call out challenging conflicts and how they were resolved.

## Conflict Resolution

- Inspect before editing: `git status`, `git diff`, `git diff --merge`.
- With zdiff3, conflict markers include base (`|||||||`), ours (`<<<<<<<`), theirs (`>>>>>>>`).
- Summarize the intent of both changes. Decide the correct outcome. Then edit.
- Prefer minimal, intention-preserving edits.
- Resolve one file at a time. Run tests after each batch.
- Use `ours`/`theirs` only when certain one side should win entirely.
- After resolving, verify no markers remain: `git diff --check`.
- For generated files: resolve source first, then regenerate.
- For import conflicts: accept both, then let lint/typecheck remove unused.

## When to Ask

Only ask the user when:
- The resolution depends on product intent not inferable from code.
- The conflict crosses an API surface or migration where choosing wrong breaks consumers.
- Two mutually exclusive designs have equivalent merit and no clear local signal.

Otherwise, proceed, document the decision in the commit, and leave reviewable history.
