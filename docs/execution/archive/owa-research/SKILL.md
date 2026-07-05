---
name: owa-multi-agent-coding
description: "Use when building software with a multi-agent Orchestrator-Worker-Adversary pipeline. Covers OWA loop setup, model-to-role allocation, local-first execution, context pruning, parallel gates, pitfalls, and post-build audit. Project-agnostic — adapt the placeholders to your stack."
version: 1.0.0
author: Parvez Syed / Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [owa, multi-agent, orchestration, cost-optimization, quality-assurance]
    related_skills: []
---

# OWA Multi-Agent Coding Workflow

## Overview

The Orchestrator-Worker-Adversary (OWA) pattern is a multi-agent
development workflow where agents are assigned opposing incentives:
one plans and dispatches, one writes code, one tries to break it. A
fourth agent verifies integration at milestone boundaries. Code
quality scales positively because the adversary has zero incentive
to be polite.

This skill is project-agnostic. Every `<PLACEHOLDER>` is a customization
point. The workflow is stack-agnostic but the examples lean toward a
compiled, testable backend (Go, Rust, TypeScript) because that is where
OWA's value is highest. For non-testable work (prototypes, design
exploration), use a single agent — OWA is overkill (see When OWA Is
Overkill).

## When to Use

- You are building a real software product with an agentic coding tool
  and want quality + cost control, not just a demo.
- A subtask touches security, state transitions, cross-component
  contracts, or anything where a late-discovered regression is
  expensive.
- You have an orchestration layer (e.g., Hermes Agent, Claude Code with
  subagents, a custom script runner) that can dispatch and merge agent
  outputs without copy-paste chaos.
- You can run build/test/lint locally on your own machine.

## Don't Use For

- Mechanical changes: a one-line config bump, a doc typo, a dependency
  version pin. Use a fast path (single worker agent, no adversary) — see
  "When OWA Is Overkill" below.
- Prototypes or exploratory design with no test surface. OWA needs
  something to break.
- Solo hobby scripts with no real cost of a regression.

## Phase 0: Strategic Scoping (Before the Swarm)

The most common failure mode: letting an LLM generate a 200-task execution
plan and then trying to execute all of it. A 200-task plan is a liability,
not an asset.

1. Write your PRD on the strongest reasoning model you have access to.
2. Ask that model to decompose into an execution plan.
3. Bounce the plan through a *different* strong coding model. Ask it to
   challenge the plan, not approve it.
4. Force the model to define a rigid P0 MVP. Push everything else to P1+.
5. Kill the rest. The primary ROI of expensive models in this phase is
   identifying what *not* to build.

Output of Phase 0: a short, cut-down list of P0 subtasks (5-15 items),
each with a one-sentence scope and an acceptance criterion. If you have
200 tasks, you failed Phase 0.

## Phase 1: Model-to-Role Allocation

Match the model's nature to the job. Capability is role-specific, not
monolithic.

| Role | Tier | Cost (example) | Function | Directive |
|---|---|---|---|---|
| Orchestrator | Flagship (highest reasoning) | `$TIER_HIGH`/call | Judgment, dispatch, merge | High reasoning, low hallucination. Maintains global state. **Does not edit code.** |
| Worker | Mid-tier (good code generation) | `$TIER_MID`/call | Execution, generation | Polite, accurate code. Mechanical tool use. |
| Adversary | Free or subscription | `$0`/call | Destructive testing | Incentivized to break the worker's code. Writes failing tests. |
| Verifier | Flagship | `$TIER_HIGH`/call, **1x per block** | Integration assurance | Runs once per block, not per subtask. Cross-subtask integration only. |
| Doc/summary subagents | Cheap/fast | `$TIER_LOW`/call | Summarization | Reads diffs, returns 5-line summaries. Never used for judgment. |

### Customization

Replace the placeholders with your actual models. Example mapping:

```
ORCHESTRATOR_MODEL=<your-highest-reasoning-model>
WORKER_MODEL=<your-best-code-generation-model>
ADVERSARY_MODEL=<your-free-or-subscription-model>
VERIFIER_MODEL=<same-as-orchestrator-usually>
DOC_SUBAGENT_MODEL=<your-cheapest-fast-model>
```

### Hard Rules

1. **No silent fallback.** If an assigned model is down, halt the pipeline
   and surface to a human. A silent Orchestrator fallback to a cheaper
   model reintroduces exactly the judgment failures (missed breaks, wrong
   merges) that motivated the role split. Fail loudly.
2. **Trial before trusting.** A free model that is "good on paper" for
   Orchestrator will miss real breaks and call wrong merges. Trial it on
   one block; audit the result; keep it only if it catches what the
   Flagship model catches. In our run, the free model was an excellent
   Adversary and a poor Orchestrator — the strengths did not transfer.
3. **Orchestrator does not edit code.** Strip the file-patch tool from the
   orchestrator profile. It can read, dispatch, merge, document — nothing
   else. Every fix, including a trivial lint correction, routes through a
   Worker. An orchestrator that also writes code is a single agent with
   extra steps.

## Phase 2: The OWA Loop (Per Subtask)

For each subtask, run this loop. See `references/owa-loop-reference.md`
for the detailed step-by-step with dispatch commands.

```
1. Orchestrator dispatches Worker on an isolated worktree/branch.
   Worker writes code, runs tests locally, commits to the branch.
   Worker outputs a structured summary (status, files changed, acceptance
   criteria met/unmet).

2. Orchestrator runs the local gate on the worktree:
   build + test + race + lint + security-scan, in PARALLEL (see Phase 4).
   Clear all tool caches first (see Pitfalls).

3. If gate fails → dispatch a fix Worker (same branch), repeat from 2.

4. Orchestrator dispatches the Adversary on the worktree.
   Adversary reads the code, writes aggressive failing tests, runs them,
   reports breaks (or "confirmed-safe").

5. If Adversary breaks → dispatch a fix Worker (same branch), repeat
   from 2.

6. Orchestrator merges locally (no PR, no push — see Phase 3).

7. Orchestrator writes an OWA record to a file (per-subtask doc with
   worker summary, adversary report, fix history, merge commit, gate
   result). See `templates/owa-record-template.md`.

8. Prune the worktree and branch.

9. Next subtask (repeat from 1).
```

### Context Efficiency

Per-subtask context should be tiny:
- Worker JSON output: ~500 bytes
- Local gate result: ~200 bytes
- Adversary report: ~1KB
- OWA record: ~2KB (written to file, not inline in orchestrator context)
- Total per subtask: ~5KB

If your per-subtask context is above ~10KB, something is bloating it
(see Phase 4: Context Pruning). The entire block should run in ONE
orchestrator session without a "one subtask per session" rule.

## Phase 3: Local-First Execution (Cost Control)

### Why Local-First

Routing every subtask through hosted CI (GitHub Actions, etc.) adds
15-30 minutes of overhead per subtask (PR create, CI dispatch, status
report, merge API, post-merge CI) with zero additional verification
value if your CI hardware IS your dev machine. The cloud round-trip is
not a best practice; it is a tax.

### Local-First Flow

1. Do all work locally. Worker commits to a local branch. Gate runs
   locally. Adversary runs locally. Merge is local.
2. No PRs per subtask. No push per subtask. No CI per subtask.
3. At block completion (all subtasks merged to local main): push once,
   create issues/PRs as a batch, run CI once.

### Expected Savings (from our measured run)

| Metric | GitHub per-subtask | Local-first | Saving |
|---|---|---|---|
| Wall clock per subtask | 40-80 min | 20-45 min | ~50% |
| Token cost per block | $15-40 | $5-15 | ~65% |
| GitHub API calls per subtask | 20+ | 0 | 100% |
| Manual re-invocations per block | 5 | 0 | 100% |

### Self-Hosted CI Runner (if you still want CI)

If you want CI as a safety net, run it on your own machine via a
self-hosted runner. Zero billed minutes, 3-29x faster than hosted CI
(depending on the job). See `references/self-hosted-ci-setup.md`.

CAUTION: macOS GitHub Actions runners bill at a 10x multiplier vs
Linux. In our run, this exhausted a 3,000-minute monthly quota in ~290
actual minutes of compute. Self-hosting eliminated this entirely.

## Phase 4: Context Pruning (Before Each Block)

Every loaded tool, skill, and MCP schema is tokens you pay for on every
API call in the session. An orchestrator shipping 20KB of irrelevant
schema (vision, video, TTS, browser) on a backend build burns ~250K
tokens per block for zero value.

### Pruning Checklist (run before block start)

1. Disable toolsets the build does not use. For a backend build, disable:
   vision, video, image_gen, video_gen, tts, browser, computer_use,
   social media tools, music tools, home automation, clarify, cronjob.
2. Keep only: web (if needed for research), terminal, file,
   code_execution, delegation, todo, memory.
3. Remove MCP servers the orchestrator does not call. (e.g., if your
   checkpoint script uses the `gh` CLI, you do not need a GitHub MCP
   server injecting 4KB of schema per call.)
4. Move irrelevant skills to a holding directory (reversible). Keep
   only the skill categories the build needs.
5. For specialist agents (Adversary, Verifier): give them ONLY terminal
   + file. Nothing else. They do not need a browser to break your code.

### Pruning Script Template

See `scripts/pre-build-prune-template.sh`. Customize the toolset list
for your stack. Restore after block completion (see
`scripts/post-build-restore-template.sh`).

### Expected Savings

| Metric | Unpruned | Pruned | Saving |
|---|---|---|---|
| System prompt per orchestrator call | ~20KB irrelevant schema | 0 | ~20KB/call |
| Tokens per 5-subtask block | — | ~250K saved | ~1MB context |
| Per-subtask context footprint | 20-30K tokens | ~5KB | ~99% |
| Sessions per block | 5 (one subtask each) | 1 (whole block) | 80% |

## Phase 5: Parallel Gate Execution

Sequential gate runs (build → test → race → lint → security-scan) take
3-5 minutes even when each component is fast. Run them in parallel and
the wall clock collapses to the latency of the slowest single component
(~1 minute).

### Parallel Gate Template

See `scripts/parallel-gate-template.sh`. The pattern:

```bash
for component in build test race lint security-scan; do
  <run-something-backgrounded-and-log-to-file> "$component"
done
# Poll for completion; check each log
```

For Docker-based e2e tests: split the suite into 4-5 batches with unique
network identifiers and run concurrently. No verification step that can
run in parallel should run in series.

### Path-Filtered CI (if using CI)

Attach path filters to each block gate so it only runs when its own code
or shared dependencies change. Touching only one package runs only that
package's gate and skips slow suite-wide tests entirely. See
`references/path-filtered-ci.md`.

## Phase 6: Block-End Verification

After all subtasks are merged to local main, run ONE Verifier pass —
not one per subtask.

### Why Not Per-Subtask

In our run, a per-subtask Verifier caught 0 novel issues across 9
subtasks (Block 5 audit). Every "blocker" it raised was a
re-confirmation of an Adversary finding, a stale-branch false positive,
or environmental noise. Per-subtask verification is theater that costs
~89% more API calls for zero additional value.

### Block-End Verifier Checklist

The Verifier (fresh context, Flagship model) runs:
1. Full block gate: build + test + race + lint + security-scan + e2e.
2. All adversary tests pass on the merged main (not just on the
   per-subtask branches).
3. Cross-subtask integration review: read the merged code, verify
   acceptance criteria across subtask boundaries (e.g., "subtask A's
   output is correctly consumed by subtask B's input").
4. Report: VERIFY PASS or VERIFY FAIL with evidence per criterion.

If FAIL: dispatch a fix Worker for the failing subtask, re-run gate +
Verifier. If PASS: proceed to Post-Build Audit.

## Phase 7: Post-Build Audit

An agent's closure comment ("CI green, all tests pass") is a claim, not
evidence. Verify against ground truth before declaring the block done.

### Audit Checklist

- [ ] Fix commits are present on the REMOTE, not just local. Agents
      commit locally and sometimes forget to push. `git log
      origin/main` must show the fix.
- [ ] CI is green on the remote main branch. Do not trust the closure
      comment — check the actual CI status.
- [ ] E2E tests that CI skips (e.g., Docker-gated suites) actually pass
      locally on the merged code.
- [ ] Any security suppressions (CVE ignores, lint exceptions) have
      factually accurate reasoning. "No fix available" is wrong if a fix
      exists in a later version.
- [ ] The CI workflow actually runs the current block's gate, not a
      stale first-block gate.

See `references/post-build-audit.md` for the full checklist.

## When OWA Is Overkill

The full OWA loop (Worker + Adversary + Verifier + Audit) is justified
when a subtask touches security, state machine transitions,
cross-component contracts, or anything where a regression is expensive
to discover late.

It is NOT justified for:
- One-line config bumps
- Doc typos
- Dependency version pins (no logic change)
- Mechanical refactors with no behavioral change (e.g., rename a local
  variable)

For trivial work, maintain a fast path: dispatch a single Worker, run
the gate, merge. Skip the Adversary and Verifier. Running the full loop
on a trivial change burns feature-level costs for zero additional
assurance.

## Common Pitfalls

1. **Transport timeouts vs agent failures.** A long-running Worker
   (10-30 min) may be silently killed by a 60-second transport-layer
   timeout, leaving the Orchestrator hanging. Symptom: the Worker
   silently disappears mid-task. Fix: use a transport that survives long
   runs (e.g., tmux sessions, or a runner with a configurable timeout
   aligned to max task duration, not API defaults).

2. **Directory isolation breaches.** A Worker told to work in an
   isolated branch will `cd` back to the main repo and commit there if
   the main repo path is named anywhere in the prompt. Naming a
   location is an implicit instruction to navigate there. Fix: never
   name the main repo path in the Worker prompt. Give it the worktree
   path only.

3. **Stale state validations.** A passing lint or test gate is invalid
   if it relies on a cached state from a previous run. Symptom: the gate
   "passes" on stale results and hides real issues. Fix: force cache
   invalidation at the start of every gate (delete the linter cache, the
   test cache, any build artifact cache).

4. **Asynchronous racing.** If a fix task is injected after an
   integration failure, an auto-dispatcher may promote the next feature
   task in parallel. The fix and the next feature race; both touch the
   same files; one wins, one corrupts. Fix: require explicit synchronous
   blocks before initiating corrective branches. Block the next task
   until the fix merges.

5. **Self-reported "CI green" is a claim.** Agents commit locally and
   may forget to push. A closure comment saying "CI green" on a red
   remote is a real thing that happened. Fix: always verify on the remote
   branch state, not on the agent's self-report.

6. **Per-subtask Verifier is theater.** A Verifier run on every subtask
   catches what the Adversary already caught, or noise. It costs ~89%
   more API calls for zero novel value. Fix: Verifier runs once per
   block, at block-end, in a fresh context, focused on cross-subtask
   integration.

7. **Mega-session context burn.** Running all subtasks in one
   Orchestrator session without pruning causes context to grow to
   100K+ tokens; the model re-processes the entire history on every
   call. This is the single largest token-waste event in a build. Fix:
   prune before each block (Phase 4), and if context still grows, split
   the block across sessions at a natural breakpoint.

8. **Orchestrator edits code.** If the orchestrator has a file-patch
   tool, it will use it under pressure, bypassing the Adversary and the
   gate. Fix: strip the tool from the orchestrator profile at the
   tool-schema level. Prompts are suggestions; tool availability is a
   guarantee.

9. **No-fallback violation.** If the Orchestrator model is down and the
   pipeline silently falls back to a cheaper model, every judgment call
   (merge, scope, break assessment) degrades. Fix: hard-stop the pipeline
   on any assigned-model outage. Surface to a human. Never substitute.

10. **Duplicate CI gates.** If you run the same gate locally AND via
    CI on a self-hosted runner that IS your dev machine, you are paying
    the same compute twice plus the CI round-trip. Fix: in local-first
    mode, the local gate IS the CI. Push once at block-end; let CI run
    once as a safety net, not per subtask.

## Verification Checklist

- [ ] Phase 0 output: a cut-down P0 subtask list (5-15 items), not 200
- [ ] Model-to-role allocation written down, with the No-Fallback rule
- [ ] Orchestrator profile has NO file-patch tool
- [ ] Worker prompt does NOT name the main repo path
- [ ] Local gate script clears all caches before running
- [ ] Parallel gate script runs components concurrently, not sequentially
- [ ] Pruning script run before block start; irrelevant toolsets/skills/MCPs disabled
- [ ] Adversary and Verifier profiles have ONLY terminal + file
- [ ] Verifier runs once per block (block-end), not per subtask
- [ ] OWA record written per subtask (worker summary, adversary report, fixes, merge, gate)
- [ ] Post-build audit run against REMOTE state, not closure comments
- [ ] CVE/security suppressions have factually accurate reasoning
- [ ] Fast path defined for trivial changes (no Adversary/Verifier)
- [ ] Pruning restored after block completion (skills/toolsets re-enabled)

## Reference Files

- `references/owa-loop-reference.md` — detailed OWA loop step-by-step
  with dispatch commands
- `references/model-allocation-template.md` — model-to-role assignment
  template with customization placeholders
- `references/self-hosted-ci-setup.md` — self-hosted CI runner setup
- `references/path-filtered-ci.md` — path-filtered CI configuration
- `references/post-build-audit.md` — full post-build audit checklist
- `references/cost-optimization-checklist.md` — all cost-optimization
  steps in one checklist
- `templates/orchestrator-prompt.md` — orchestrator system prompt
  template
- `templates/worker-prompt.md` — worker prompt template
- `templates/adversary-prompt.md` — adversary prompt template
- `templates/verifier-prompt.md` — block-end verifier prompt template
- `templates/owa-record-template.md` — per-subtask OWA record format
- `scripts/pre-build-prune-template.sh` — context pruning script
- `scripts/post-build-restore-template.sh` — restore after block
- `scripts/parallel-gate-template.sh` — parallel gate execution
- `scripts/local-gate-template.sh` — local gate runner
- `scripts/block-checkpoint-template.sh` — block-end push + issue creation
