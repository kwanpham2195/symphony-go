---
slug: sandbox-git-write-blocked
created_at: 2026-05-24T17:15:00+07:00
branch: main
head: 5d57ffb
status: ready
---

# Handoff: Codex sandbox blocks .git/ writes on macOS

## Goal

Make symphony's codex agent able to `git add`, `git commit`, and `git push` from
inside its workspace so the full workflow (code → commit → PR → land) works
end-to-end without human intervention.

## Current State

The codex agent can:
- Read all files in the workspace
- Write/modify source code files
- Run `make check-sandbox` (lint + unit tests)
- Post updates to Linear via `linear_graphql` tool

The codex agent **cannot**:
- `git add` — fails with `Unable to create '.git/index.lock': Operation not permitted`
- `git commit` — same
- `git push` — same (and also blocked by `networkAccess: false`)

The agent adapted by saving a patch file to `/private/tmp/` as a workaround.

## Why This Matters

Without git write access, symphony can only produce code changes but cannot
complete the PR workflow. A human must apply patches manually, which defeats
the purpose of autonomous orchestration.

## What I Checked

1. **Protocol-level sandbox settings** — tried all three `thread_sandbox` values:
   - `workspace-write` — blocks `.git/` writes
   - `danger-full-access` — still blocks `.git/` writes
   - Both with and without explicit `turn_sandbox_policy`

2. **Codex binary analysis** — the codex CLI (`codex-cli 0.133.0`) is a Rust
   binary at:
   ```
   /opt/homebrew/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex
   ```
   It uses macOS **seatbelt** (sandbox-exec) to enforce filesystem restrictions.
   Source paths in the binary reference:
   - `sandboxing/src/seatbelt.rs`
   - `cli/src/debug_sandbox/seatbelt.rs`

3. **Hardcoded `.git` protection** — `strings` on the binary shows:
   - `"The .git name may never be used"` — from a git validation library
   - `.git` appears multiple times in sandbox-related contexts
   - Seatbelt policy comments reference Chromium's sandbox policy

4. **Upstream Elixir comparison** — upstream uses `workspace-write` with the
   same `turn_sandbox_policy: { type: workspaceWrite }`. They likely run on
   Linux where `bubblewrap` (not seatbelt) is the sandbox, and bubblewrap may
   handle `.git/` differently.

5. **Codex config flags** — upstream passes
   `--config shell_environment_policy.inherit=all` to codex. Unknown if this
   affects sandbox behavior.

## Key Findings

- The original seatbelt conclusion was too broad.
- Codex can write `.git/` on macOS when the command runs with real
  `danger-full-access`. Evidence: `codex exec -s danger-full-access -a never`
  ran `git add a.txt` successfully in a temp repo.
- Symphony ignored `thread_sandbox: danger-full-access` when it built the
  default `turn/start` sandbox policy and still sent `workspaceWrite`.
- Root cause: app-server turn policy and thread sandbox were split. The thread
  used `danger-full-access`; the turn used implicit `workspaceWrite`.

## Open Questions

1. Does `codex --config sandbox.enable=false` or similar flag exist to disable
   the seatbelt sandbox entirely?
2. Does `shell_environment_policy.inherit=all` affect seatbelt file access rules?
3. Does the upstream Elixir symphony actually support git operations on macOS,
   or only on Linux?
4. Is there a codex app-server flag to whitelist `.git/` in the seatbelt profile?
5. Would running codex inside Docker on macOS bypass the seatbelt restriction?
6. Could the `after_run` hook (which runs outside the sandbox) apply patches
   and commit on behalf of the agent?

## Recommended Next Change

Use `thread_sandbox` when building the implicit turn sandbox policy:

- `danger-full-access` -> `{"type":"dangerFullAccess"}`
- `read-only` -> `{"type":"readOnly","networkAccess":false}`
- default / `workspace-write` -> existing `workspaceWrite` policy rooted at
  the issue workspace

Keep explicit `codex.turn_sandbox_policy` pass-through unchanged.

## Candidate Files

- `WORKFLOW.md` — sandbox config settings
- `internal/codex/client.go` — sends `sandboxPolicy` in `turn/start`
- `internal/config/config.go` — `CodexConfig.TurnSandboxPolicy`

## Validation / Commands

```bash
# Verify focused regression
GO111MODULE=on go test ./internal/codex -run TestRunTurn_DefaultsToDangerFullAccessTurnPolicy -count=1

# Full gates run after fix
GO111MODULE=on go test ./...
GO111MODULE=on go vet ./...
GO111MODULE=on go build ./...

# Historical reproduction before this fix
cd ~/code/symphony-go-workspaces/CFW-53
touch test.txt && git add test.txt
# Expected: fatal: Unable to create '.git/index.lock': Operation not permitted

# Check codex version
codex --version
# codex-cli 0.133.0

# Inspect seatbelt references in binary
CODEX_BIN="/opt/homebrew/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex"
strings "$CODEX_BIN" | grep seatbelt
```

## Relevant Artifacts / References

- Agent's saved patch: `/private/tmp/cfw-53-remove-domain-package.patch` (1940 lines)
- Codex binary: `/opt/homebrew/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex`
- Seatbelt source in codex: `sandboxing/src/seatbelt.rs`
- Upstream workflow: `/Users/kwanpham/.opensrc/repos/github.com/openai/symphony/main/elixir/WORKFLOW.md`
- Codex sandbox modes: `read-only`, `workspace-write`, `danger-full-access`
- Linear issue for this investigation: CFW-53 (cancelled due to sandbox blocker)
