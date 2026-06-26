# OWA Loop — Detailed Step-by-Step

This is the canonical OWA loop per subtask. Adapt dispatch commands to
your orchestration layer (Hermes, Claude Code subagents, custom scripts).

## Per-Subtask Flow

### Step 1: Dispatch Worker

```
Orchestrator writes task prompt to /tmp/<block>-<subtask>-prompt.md
  (scope, acceptance criteria, lint rules, security constraints)

Orchestrator dispatches Worker on an isolated worktree:
  <dispatch-command> <branch> <prompt-file> <worktree-path>

Worker:
  - Reads the task prompt
  - Writes code in the worktree
  - Runs tests locally (unit tests only — the full gate is Step 2)
  - Commits to the local branch (NO PR, NO push)
  - Outputs a structured JSON summary:
      { status, files_changed, acceptance_met, notes }
```

CAUTION: Worker dispatch can take 10-30 min. Use a transport that
survives long runs (tmux, or a configurable-timeout runner). Do NOT use
a background process with a 60s default timeout — it will be SIGTERM'd
mid-task. (See pitfall #1 in SKILL.md.)

### Step 2: Local Gate (Parallel)

```
Orchestrator runs the local gate on the worktree:

  1. Clear all tool caches (linter cache, build cache, test cache).
     This is MANDATORY — a stale cache produces false-green results.
  2. Run gate components in PARALLEL:
       build  → log to /tmp/gate-build.log
       test   → log to /tmp/gate-test.log
       race   → log to /tmp/gate-race.log
       lint   → log to /tmp/gate-lint.log
       secscan→ log to /tmp/gate-secscan.log
  3. Poll for completion; check each log for PASS/FAIL.

If any component FAILs → dispatch a fix Worker (same branch), repeat
from Step 2.
```

See `scripts/parallel-gate-template.sh` and
`scripts/local-gate-template.sh`.

### Step 3: Dispatch Adversary

```
Orchestrator dispatches Adversary on the worktree:
  <dispatch-command> --profile adversary --toolsets terminal,file
    --prompt "<adversary-prompt>"

Adversary:
  - Reads the Worker's code in the worktree
  - Writes aggressive tests designed to FAIL (break tests)
  - Runs the break tests
  - Reports:
      BREAK (severity, description, test, fix hint)
    or
      CONFIRMED-SAFE (tests written, all held)

If BREAK → dispatch a fix Worker (same branch), repeat from Step 2.
```

The Adversary gets ONLY terminal + file tools. No browser, no web, no
vision. It does not need them. It needs to read code and run tests.

See `templates/adversary-prompt.md`.

### Step 4: Local Merge

```
Orchestrator merges the branch to local main:
  git merge --no-ff <branch> -m "<block>-<subtask>: <title>"

NO PR. NO push. NO CI dispatch. This is local-first.
```

### Step 5: Write OWA Record

```
Orchestrator writes the per-subtask OWA record to:
  docs/owa-records/<block>-<subtask>.md

Contents:
  - Worker summary (files changed, acceptance met/unmet)
  - Gate result (each component PASS/FAIL, fix cycles if any)
  - Adversary report (breaks found, severity, fixes applied)
  - Merge commit SHA
  - Verifier section: "Deferred to block-end" (see Step 6)
```

See `templates/owa-record-template.md`.

### Step 6: Prune and Advance

```
  git worktree remove --force <worktree>
  git branch -D <branch>
  git worktree prune

  → Next subtask (repeat from Step 1).
```

## Block-End (After All Subtasks Merged)

### Step 7: Preliminary Block Gate

```
Orchestrator runs the full block gate locally (all components, parallel).
  If FAIL → dispatch fix Worker for the failing subtask, re-run.
  If PASS → proceed to Step 8 (Verifier).
```

### Step 8: Block-End Verifier (1x, Fresh Context)

```
Orchestrator dispatches Verifier in a FRESH session (not the same
  orchestrator session — fresh context, no accumulated state):
  <dispatch-command> --profile verifier --toolsets terminal,file
    --prompt "<verifier-prompt>"

Verifier runs:
  1. Full block gate (build + test + race + lint + secscan + e2e)
  2. All adversary tests pass on merged main
  3. Cross-subtask integration review (read merged code, verify
       acceptance across subtask boundaries)
  4. Report: VERIFY PASS or VERIFY FAIL with evidence per criterion

If FAIL → dispatch fix Worker, re-run gate + Verifier.
If PASS → write block-end verifier report to docs/owa-records/<block>-block-end.md
       → proceed to Post-Build Audit.
```

See `templates/verifier-prompt.md`.

### Step 9: Post-Build Audit

See `references/post-build-audit.md`.

### Step 10: Block Checkpoint (Push + Issues)

```
  <checkpoint-script> <block-number>
  → pushes local main to remote
  → batch-creates issues from OWA records (if using issue tracking)
  → runs CI once on remote (safety net)

Post-checkpoint:
  - Close all block issues with detailed closure comments
  - Restore pruned skills/toolsets (see scripts/post-build-restore-template.sh)
  - Delete stale local branches: git branch | grep -v main | xargs git branch -D
```

See `scripts/block-checkpoint-template.sh`.
