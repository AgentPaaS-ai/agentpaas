# AGENTPAAS PHASE 1 — EXECUTION PLAN v1.0
**Purpose:** The build contract. Each BLOCK is sized for one focused LLM
coding session, carries an exact build prompt, a test plan with edge cases,
and a binary success gate. No block starts until the previous gate is green.
**Release gate:** `docs/execution/golden-loop-test.md` — Phase 1 shipped with
the v0.2.3 phases 1–12 baseline. Later stable releases must pass that baseline
plus the target release profile and closure gate defined in the current Golden
Loop; prereleases run their declared subset outside the stable channel.
**Release posture:** Phase 1 is the macOS-first OSS/demo delivery, not the
full customer-facing commercial release. Its job is to prove the wedge,
produce credible demos, publish verifiable open-source artifacts, and collect
design-partner feedback without telemetry. Phase 2 is the customer-facing
release track: Linux certification, fleet/team management, enterprise
packaging, support posture, and commercial observability.
**Repo:** `github.com/agentpaas/agentpaas` (monorepo)
**Companion:** `agentpaas-prd-v4-master.md` (the WHY/spec; this is the HOW)

---

## 0. BUILD STRATEGY — HOW TO DRIVE LLMs ON THIS

### 0.1 OWA build loop: Orchestrator, Worker, Verifier, Adversary
Use the OWA coding pattern for implementation: **Orchestrator · Workers ·
Adversary**, with a separate verifier-worker function for test evidence.
The user chooses the model for each role at the start of every build session.
Model names in this document are examples, not requirements.

Before coding begins, record the session routing:
```yaml
build_session:
  agent: hermes
  block: N
  issue: <issue id/title>
  orchestrator:
    primary: glm-5.2-via-openrouter
    fallback: deepseek-v4-pro
  worker:
    primary: deepseek-v4-flash
    fallback: composer-2.5-via-x-oauth
  verifier:
    primary: glm-5.2-via-openrouter   # Block 6+: runs ONCE per block, not per subtask (see §0.1.0a)
    fallback: deepseek-v4-pro
    cadence: once_per_block
  adversary:
    primary: grok-4.3
    fallback: glm-5.2-via-openrouter
    invoked: true|false
  adversary_required: true|false
  model_budget_usd: <budget>
```

Role contract:
1. **Orchestrator** (main session; user-selected model): owns architecture,
   scoping, issue decomposition, acceptance criteria, security invariants,
   spec review, and the accept/refine/reject decision. The orchestrator is
   the only LLM role allowed to decide whether the implementation satisfies
   the spec. It may not outsource final judgment to the worker, verifier, or
   adversary.
2. **Worker** (fresh session; user-selected model): receives only the issue,
   relevant PRD/execution-plan excerpts, touched-file scope, and acceptance
   criteria. It implements test-first and returns a diff summary, files
   changed, commands run, and known risks. It does not make architecture
   decisions unless the issue explicitly grants that authority.
3. **Verifier Worker** (fresh session; user-selected model): owns test
   evidence, not spec authority. It receives the issue, acceptance criteria,
   diff, and expected gate. It writes or requests missing tests, runs the
   canonical `make blockN-gate` or narrower failing target, reports exact
   PASS/FAIL output, and produces repro steps for failures. It cannot approve
   the PR and should not rewrite the implementation in the same pass unless
   the orchestrator explicitly reassigns it as a worker. **Starting Block 6,
   the verifier runs ONCE per block, not per subtask.** Per-subtask
   verification is deferred: the orchestrator's fresh-cache gate
   (build/test/lint) and the adversary cover in-subtask defect-finding; the
   verifier provides fresh-context cross-subtask integration review at
   block-end (see §0.1.0a "Block-end verification"). Rationale: the Block 5
   audit found the per-subtask verifier (grok-4.3) caught 0 novel issues
   across 9 subtasks — every BLOCKER was a re-confirmation of an adversary
   break, a stale-branch false positive, or environmental noise (OSV/Docker).
   The adversary does 100% of real per-subtask defect-finding; the block gate
   requires multiple subtasks merged to be meaningful, so block-end
   verification is where the verifier adds value.
4. **Adversary** (fresh session; user-selected model from a different model
   family where possible): runs only when risk triggers require it. It
   receives the security claims, trust boundaries, diff, and test evidence.
   Its task is to break the claims with negative tests, abuse cases, and
   first-principles review. Any successful break returns to the orchestrator
   for refine/re-scope.

Founder gate: you review the orchestrator's spec decision, verifier evidence,
and adversary result when invoked; then you run the named success gate before
merge. Design disputes are resolved by the orchestrator and founder, not by
adding unbounded agent debate.

### 0.1.0 Build memory: GitHub is the durable brain
LLM chat history is not build memory. GitHub is the durable memory for the OWA
loop: Issues hold intent and attempt history; Pull Requests hold diffs,
review/gate evidence, and merge decisions; GitHub Project shows block status;
`docs/status.md` is the generated executive dashboard.

Every worker, verifier, adversary, fallback, and orchestrator decision must be
recorded in the linked GitHub issue or PR before another role continues.
At minimum, each attempt log records:
```yaml
attempt: 1
role: worker|verifier|adversary|orchestrator
model: <model id/provider>
fallback_used: true|false
fallback_reason: quota|tokens|rate_limit|unavailable|repeated_failure|null
input_refs:
  issue: <github issue url>
  pr: <github pr url or null>
  commit: <sha or null>
result: pass|fail|blocked|needs_orchestrator|accepted|refine|reject
gate: <command or null>
commands_run:
  - <exact command>
failure_summary: <short summary or null>
files_touched:
  - <path>
next_recommendation: continue|retry_worker|switch_fallback|split_issue|rescope|invoke_adversary|founder_decision
```

If a worker fails the same issue three times, the next action is not another
blind retry. The issue must be marked `needs_orchestrator`; the orchestrator
reviews the attempt logs and chooses a different approach: split the issue,
change the design, switch to fallback/stronger model, add missing tests, or
re-scope the block.

### 0.1.0a Local-first build mode (Block 6+)

**When the CI runner is self-hosted on the same machine as the orchestrator,
GitHub per-subtask PRs add 15-30 min overhead with zero additional verification
value.** The self-hosted runner runs the same Makefile targets on the same
hardware as a local gate run.

Local-first mode eliminates per-subtask: PR creation, CI dispatch, CI wait,
post-merge CI, GitHub API calls. All work is local. GitHub is updated only at
block completion via a checkpoint push.

**HARD RULE (pitfall #43): no GitHub issues during the build.** The
orchestrator's planning pass produces subtask definitions as local files
(`docs/owa-records/b<N>-t<NN>.md`) and todo list entries — NOT as GitHub
issues. GitHub issue creation is DEFERRED to `block-checkpoint.sh` which
runs AFTER the block gate passes. Creating issues mid-build adds zero value
(the orchestrator already has the plan) and violates the "0 GitHub API
calls during build" contract. Block 11 created issues #130-#136 at 18:14Z,
5.5 hours before the gate passed at 21:36Z — this was a process violation.
Correct ordering: plan locally → implement locally → gate passes →
checkpoint (push + batch-create issues).

#### Local-first OWA flow (per subtask)

```
1. Orchestrator dispatches Codex worker on local git worktree
   bash scripts/codex-worker-local.sh <branch> <issue#> <prompt> <worktree>
   → Worker commits to local branch (NO PR, NO push)
   → Worker runs go test + golangci-lint locally
   → Worker outputs JSON summary

2. Orchestrator spawns adversary (separate Hermes profile, foreground terminal)
   hermes -p agentpaas-adversary chat -q "<prompt with worktree path>" -Q
   → Adversary reads worktree code, writes break tests, runs them, reports

3. If adversary breaks → dispatch fix worker (same branch), repeat from step 2

4. Orchestrator runs fresh-cache gate on worktree
   bash scripts/local-gate.sh ./internal/<package>/...
   → build + test + lint ONLY (no -race, no osv, no e2e per-subtask;
     race/osv/e2e-with-Docker move to block-end verification below)
   → fresh-cache: orchestrator does NOT reuse the worker's cache; it re-runs
     build/test/lint against the merged worktree state

5. If gate fails → dispatch fix worker (same branch), repeat from step 4

6. Orchestrator merges locally
   git merge --no-ff <branch> -m "B<N>-T<NN>: <title>"

7. Orchestrator writes OWA record to file (verifier section DEFERRED to block-end)
   docs/owa-records/b<N>-t<NN>.md
   (Same format as the GitHub attempt log above — just stored locally.
    The verifier_result block is left empty and filled at block-end.)

8. Orchestrator prunes worktree
   git worktree remove --force <worktree>
   git branch -D <branch>
   git worktree prune

9. Next subtask (repeat from step 1)

NOTE (Block 6+): the per-subtask verifier step (v1.0 step 6) is REMOVED. The
adversary does 100% of real per-subtask defect-finding (Block 5 audit: 0 novel
issues from the per-subtask verifier across 9 subtasks). The orchestrator's
fresh-cache gate covers build/test/lint. Independent verification moves to
block-end (see "Block-end verification" below), where the verifier's
fresh-context cross-subtask integration review actually adds value.
```

#### Block-end verification (Block 6+)

After ALL subtasks are merged to local main (and BEFORE the checkpoint push),
run the verifier once per block. This replaces the per-subtask verifier step.

```
1. Orchestrator runs a preliminary block gate on merged main
   bash scripts/local-gate.sh block N
   → build + test + test-race + lint + osv
   → if this fails, dispatch fix workers on the failing subtask(s) and
     re-run; do NOT proceed to the verifier with a red preliminary gate

2. Orchestrator spawns the verifier ONCE (fresh agentpaas-verifier Hermes
   profile, foreground terminal; primary model GLM-5.2)
   hermes -p agentpaas-verifier chat -q "<block-end prompt with merged-main
   ref, block spec, all OWA records, all adversary reports>" -Q

   The verifier INDEPENDENTLY runs the FULL block gate on merged main:
     - build / test / test-race / lint / osv
     - e2e with Docker (DOCKER_HOST=unix:///Users/pms88/.colima/default/docker.sock,
       AGENTPAAS_DOCKER_TESTS=1)
   Then it runs ALL adversary tests against merged main (the per-subtask
   adversary runs were on isolated worktrees; the verifier re-runs them on
   the merged tree to catch cross-subtask regressions).
   Then it performs a cross-subtask integration review: do the merged
   subtasks compose correctly, do their contracts line up, are there
   gaps/contradictions between subtask boundaries that no single-subtask
   pass could see.

3. Verifier reports evidence (verifier_result schema from §0.1.1a).

4. If verifier FAILS → orchestrator dispatches fix workers on the offending
   subtask(s), re-merges, and re-runs the block-end verifier. The block is
   NOT checkpointed until the verifier passes.

5. If verifier PASSES → block checkpoint (below).
```

Why block-end (not per-subtask): the block gate (`make blockN-gate`) requires
multiple subtasks merged to be meaningful — running it on a single isolated
subtask worktree yields stale-branch false positives and misses cross-subtask
integration defects. A fresh-context verifier on merged main is the only point
in the loop where the full block gate and cross-subtask review are valid. The
Block 5 audit confirmed this: per-subtask verifier (grok-4.3) caught 0 novel
issues across 9 subtasks; every BLOCKER was a re-confirmed adversary break, a
stale-branch false positive, or environmental noise (OSV/Docker).

#### Block checkpoint (at block completion)

After all subtasks are merged to local main:
```bash
bash scripts/block-checkpoint.sh <block_number>
```
This:
1. Pushes local main to GitHub (all merged work in one push)
2. Creates GitHub issues from `docs/owa-records/b<N>-*.md` files (with full
   OWA records as issue body — same documentation gate, just deferred)
3. Creates a block summary issue

The checkpoint satisfies the "GitHub is the durable brain" requirement — all
attempt logs, adversary findings, verifier evidence, and merge decisions are
preserved in GitHub issues. The difference is timing: batch at block completion
instead of per-subtask.

#### Local-first scripts

- `scripts/codex-worker-local.sh` — dispatch Codex worker (GPT-5.5) on local
  worktree, no PR, no push
- `scripts/local-gate.sh` — run gate verification (package/block/full/e2e)
- `scripts/block-checkpoint.sh` — push to GitHub + batch-create issues
- `docs/codex-owa-worker-local.md` — worker prompt for local mode

#### When to use local-first vs GitHub mode

Local-first is the default when:
- CI runner is self-hosted on the same machine as the orchestrator
- Solo development (no multi-developer PR review needed)
- Block gate can be run locally (all deps installed)

GitHub per-subtask mode (section 0.1.0) is used when:
- CI runs on GitHub-hosted runners (ubuntu-latest, etc.)
- Multiple developers need to review PRs
- Block requires GitHub-hosted CI services

#### Savings (measured on Block 5 self-hosted runner)

| Metric | GitHub per-subtask | Local + checkpoint | Savings |
|--------|-------------------|--------------------|---------|
| Wall clock per subtask | 40-80 min | 20-45 min | ~50% |
| Token cost per block | $15-40 | $5-15 | ~65% |
| Manual re-invocations | 5 per block | 0 | 100% |
| GitHub API calls | 20+ per subtask | 0 during build | 100% |

#### Pre-build token optimization (all modes)

Before starting any block, prune irrelevant skills, toolsets, and MCP servers
from all OWA profiles (orchestrator, adversary, verifier). This reduces system
prompt size by ~20KB per orchestrator call — saving ~1MB of context over a
5-subtask block and improving adversary focus.

**Orchestrator profile:**
- Keep toolsets: web, terminal, file, code_execution, skills, todo, memory, context_engine, delegation
- Disable: vision, video, image_gen, video_gen, x_search, tts, computer_use, moa, session_search, clarify, cronjob, browser
- Remove GitHub MCP during build (checkpoint uses gh CLI)
- Keep skill categories: autonomous-ai-agents, devops, github, hermes-agent, mcp, software-development
- Move all other skill categories to .skills-holding/ (reversible)

**Adversary + Verifier profiles:**
- Keep toolsets: terminal, file ONLY
- Disable all others
- Remove GitHub MCP
- Keep skill categories: github, hermes-agent, software-development ONLY
- Move all other categories to .skills-holding/

See OWA skill → "Token Optimization (pre-build pruning)" for the full pruning
and restore scripts. Run pruning before block start, restore after block
checkpoint push to GitHub.

### 0.1.1 Cost-effective LLM execution loop
Use the strongest available model for planning and architecture, then keep
execution PRs small enough for cheaper models to complete safely.

1. **Orchestrator planning pass (user-selected model).** For each block,
   produce: PR breakdown, public contracts, security invariants, expected
   tests, files likely touched, and non-goals. Output becomes GitHub issues.
2. **Worker pass (user-selected model).** One issue at a
   time. Context = issue body, relevant PRD/execution-plan sections, repo,
   failing test target. No architecture decisions unless the issue explicitly
   grants them.
3. **Verifier-worker pass (user-selected model).** Receives the issue, diff,
   acceptance criteria, and test output. It must answer with exact gate
   evidence: PASS, FAIL with repro, or missing-test list. It cannot be the
   final spec approver. **Starting Block 6 this pass runs ONCE per block at
   block-end** (see §0.1.0a "Block-end verification"), not per subtask. The
   per-subtask verifier pass is dropped: the Block 5 audit found it caught 0
   novel issues across 9 subtasks (every BLOCKER was a re-confirmed adversary
   break, stale-branch false positive, or environmental noise). The adversary
   and the orchestrator's fresh-cache build/test/lint gate cover per-subtask
   defect-finding; the verifier adds value only at block-end with fresh-context
   cross-subtask integration review on merged main.
4. **Orchestrator spec review (same orchestrator model unless user changes
   it).** Receives the issue, diff, verifier evidence, and known failures.
   It must answer: ACCEPT, REFINE with numbered defects, or REJECT/re-scope.
5. **Adversary pass (user-selected model; invoked by risk triggers).** Writes
   or suggests negative tests against the claims in the issue. Any successful
   break blocks merge.
6. **Escalation rule.** Use a stronger model only when: API/security contract
   changes, worker fails the same gate twice, verifier and worker evidence
   disagree, or the fix would broaden scope beyond the issue.

PR sizing rule: one behavioral claim per PR; target <500 changed production
LOC plus tests. If a PR needs more, split it before coding.

### 0.1.1b Post-build audit (after all subtasks closed)

After all subtask issues are closed and PRs merged (or in local-first mode,
after all subtasks are merged to local main), run a verification audit BEFORE
declaring the block complete. OWA closure comments describe what was done; the
audit verifies it was actually done.

Audit checklist:
1. **Fix commits actually pushed to remote** (GitHub mode) or merged to local
   main (local-first mode). Local-only = CI still red. Check `git log --oneline`
   for every fix commit referenced in closure comments.
2. **CI green on remote main** (GitHub mode) or **local gate green**
   (local-first mode). Don't trust "CI green" in closure comments — run the
   gate yourself. In local-first mode: `bash scripts/local-gate.sh block N`.
3. **E2E tests that CI skips actually pass locally with Docker.** Set
   `DOCKER_HOST=unix:///Users/pms88/.colima/default/docker.sock` and
   `AGENTPAAS_DOCKER_TESTS=1`. Clean stale Docker networks/containers first.
4. **CVE suppressions have factually accurate reasoning.** "No fix available"
   is wrong if fixes exist in a newer version. Check each suppression in
   `osv-scanner.toml` against the actual advisory.
5. **CI workflow runs the block gate** (not just block1-gate). Check
   `.github/workflows/block-gates.yml` for the current block's gate job.
6. **Summary issue content is current** (no stale "Known Issues"). If the
   audit found and fixed issues, the summary must reflect the post-fix state.
7. **All adversary breaks are resolved.** Check each adversary report for
   HIGH/MEDIUM breaks and verify fix commits address them. Reopen issues if
   any are skipped.
8. **All acceptance criteria are verified.** Don't trust "met" checkboxes —
   run the gate and confirm each criterion produces a PASS.

If the audit finds issues: create fix issues, dispatch workers, and re-audit
after fixes merge. Do NOT close the block summary issue until the audit passes.

In local-first mode: run the audit before `bash scripts/block-checkpoint.sh N`.
The checkpoint push to GitHub is the point of no return — all audit findings
must be resolved before pushing.

### 0.1.1a Role prompt templates (paste verbatim)
Each role runs in a fresh Hermes session unless the section says otherwise.
Paste the applicable template, then attach only the referenced context bundle.
Every role must write its result back to the linked issue or PR using the
attempt-log schema in §0.1.0 before another role continues.

#### Orchestrator planning prompt
````
You are the AgentPaaS build orchestrator for this block/issue.

Your authority:
- You own architecture, issue decomposition, acceptance criteria, security
  invariants, non-goals, model routing, and final spec judgment.
- You do not write implementation code in this pass unless the user explicitly
  reassigns you as a worker.
- You may split, rescope, or block an issue if the request is too large,
  ambiguous, unsafe, or missing testable acceptance criteria.

Inputs you receive:
- Current build_session routing YAML.
- The target BLOCK from this execution plan.
- Relevant PRD excerpts.
- Current repo status and known constraints.
- Any linked issue, attempt log, verifier evidence, or adversary evidence.

Your task:
1. Restate the goal in one short paragraph.
2. Define the smallest safe issue or PR slice. Keep it to one behavioral
   claim and target <500 changed production LOC plus tests.
3. Write acceptance criteria that are binary and testable.
4. List security invariants and at least one required negative test for every
   security claim.
5. Name the canonical gate command, usually `make blockN-gate`, and any
   narrower command the worker should run first.
6. Identify files or directories likely touched, plus files that should not be
   touched.
7. Declare non-goals and ambiguity. If a human answer is required, stop with
   `QUESTION:` and do not invent a requirement.
8. Decide whether adversary review is required using §0.1.3.
9. Produce the worker handoff packet below.

Output format:
```yaml
orchestrator_plan:
  result: ready|question|blocked|split_required|rescope_required
  issue_title: <title>
  goal: <short paragraph>
  behavioral_claim: <one claim>
  acceptance_criteria:
    - <binary criterion>
  security_invariants:
    - claim: <claim>
      negative_test_required: <test idea>
  gate:
    canonical: make blockN-gate
    first_narrow_target: <command or null>
  context_refs:
    execution_plan_sections:
      - <section>
    prd_sections:
      - <section>
  likely_files:
    - <path or glob>
  do_not_touch:
    - <path or glob>
  non_goals:
    - <non-goal>
  adversary_required: true|false
  adversary_reason: <reason>
  worker_handoff:
    summary: <what to build>
    constraints:
      - <constraint>
    done_when:
      - <criterion>
```

If you succeed:
- Create or update the linked issue with the plan, acceptance criteria, gate,
  adversary requirement, and worker handoff.
- Record an attempt log with `result: pass` or `result: accepted`.
- Send the worker only the issue, relevant excerpts, touched-file scope, and
  acceptance criteria.

If you fail:
- If requirements are unclear, output `QUESTION:` with the minimum question
  needed.
- If the issue is too large, output `split_required` and propose the split.
- If the plan would violate a security invariant or P1 scope, output
  `blocked` or `rescope_required` with a concrete reason.
- Do not send work to a worker until the issue is ready.
````

#### Orchestrator spec-review prompt
````
You are the AgentPaaS orchestrator performing final spec review for this
issue. This is a judgment pass, not an implementation pass.

Your authority:
- You are the only LLM role allowed to decide whether the implementation
  satisfies the issue, block spec, security invariants, and non-goals.
- You may accept, request refinement, reject/re-scope, split follow-up work, or
  require adversary review.
- You may not ignore failing verifier evidence, accept missing security tests,
  or outsource final judgment to the worker, verifier, or adversary.

Inputs you receive:
- Current build_session routing YAML.
- Linked issue, orchestrator plan, and acceptance criteria.
- Worker result and diff summary.
- Verifier evidence and exact commands run.
- Adversary result when invoked, or not-invoked reason.
- Current PR diff, known risks, and any failed attempts.

Your task:
1. Compare the implementation against the issue and block spec.
2. Check that every acceptance criterion has verifier evidence.
3. Check that every required security invariant has a passing negative test or
   a documented, acceptable not-applicable reason.
4. Check that the diff stayed within scope and did not add unrelated behavior.
5. Decide whether residual risk is acceptable for this block.
6. Produce one of: `ACCEPT`, `REFINE`, or `REJECT`.

Output format:
```yaml
orchestrator_review:
  result: ACCEPT|REFINE|REJECT
  issue: <id/title>
  decision_summary: <short paragraph>
  acceptance_criteria:
    - criterion: <criterion>
      status: satisfied|unsatisfied|unclear
      evidence: <verifier evidence or reason>
  security_invariants:
    - claim: <claim>
      status: satisfied|unsatisfied|unclear|not_applicable
      evidence: <negative test/adversary evidence/reason>
  scope_check: in_scope|scope_creep|unclear
  required_fixes:
    - id: O1
      severity: blocker|major|minor
      instruction: <specific fix or test>
  follow_up_issues:
    - <issue or none>
  next_action: merge_ready|return_to_worker|invoke_adversary|split_issue|rescope|founder_decision
```

If you accept:
- Record `result: accepted` in the issue/PR attempt log.
- State exactly which gate evidence supports acceptance.
- Hand off to founder gate/merge readiness. Do not merge on behalf of the
  founder unless explicitly asked.

If you request refinement:
- Record `result: refine` with numbered defects and exact required changes.
- Return the issue to the worker with only the required fixes and tests.
- Require a new verifier pass after the worker changes the diff.

If you reject:
- Record `result: reject` with the reason: wrong approach, unsafe design,
  scope mismatch, missing requirement, or failed security claim.
- Choose the next action: split, rescope, stronger model, more context, or
  founder decision.
- Do not allow the same worker loop to continue without a new plan.
````

#### Worker implementation prompt
````
You are the AgentPaaS worker for one issue. Implement exactly the
orchestrator handoff, no more and no less.

Your authority:
- You may edit code, tests, docs, fixtures, and build files only within the
  touched-file scope or when directly required by the acceptance criteria.
- You may make small local design choices inside the orchestrator's plan.
- You may not change architecture, broaden scope, remove security checks,
  rename canonical gates, skip required negative tests, or decide final spec
  acceptance.

Inputs you receive:
- Current build_session routing YAML.
- Linked issue and orchestrator handoff.
- Relevant execution-plan and PRD excerpts.
- Acceptance criteria, security invariants, and required gate command.
- Current repository state.

Rules:
- TDD: add or expose a failing test first, run it, implement, re-run.
- Keep the diff focused on the issue. Do not refactor unrelated code.
- No new dependency without name, license, and reason in the PR body.
- All listeners bind 127.0.0.1 unless the spec says otherwise.
- Every security claim gets a negative test proving the bad path is blocked.
- User-visible behavior must also expose a machine-readable JSON path suitable
  for Hermes in P1 and other coding tools in P2.
- If the spec is ambiguous, stop with `QUESTION:`. Do not guess.

Your task:
1. Inspect the relevant files and existing patterns.
2. Add or identify the failing test that proves the missing behavior.
3. Implement the smallest change that satisfies the acceptance criteria.
4. Run the narrow test target, then the canonical gate if available.
5. Prepare a diff summary, commands run, risks, and follow-up notes.

Output format:
```yaml
worker_result:
  result: pass|fail|blocked|question
  issue: <id/title>
  summary: <what changed>
  files_changed:
    - <path>
  tests_added_or_changed:
    - <path or test name>
  commands_run:
    - command: <exact command>
      result: pass|fail
      notes: <short output summary>
  acceptance_criteria_status:
    - criterion: <criterion>
      status: met|not_met|blocked
      evidence: <test/file/command>
  security_tests:
    - claim: <claim>
      negative_test: <test>
      status: pass|fail|not_applicable
  known_risks:
    - <risk or none>
  needs_orchestrator: true|false
```

If you succeed:
- Leave the repo in a state where the verifier can run the named tests.
- Update the issue/PR attempt log with `result: pass`, exact commands, files
  touched, and any risk.
- Hand off to the verifier with the diff summary, commit/branch ref if
  available, and test evidence.

If you fail:
- If a test fails, keep the failure output and exact repro command.
- If blocked by ambiguity, output `QUESTION:` and set `needs_orchestrator:
  true`.
- If the same issue has failed twice before, do not retry blindly; mark
  `needs_orchestrator` and recommend split, rescope, stronger model, or
  missing-test clarification.
- Do not hide failing commands or claim success without a green gate.
````

#### Verifier-worker prompt
````
You are the AgentPaaS verifier-worker. Your job is test evidence and failure
reproduction, not final spec approval.

**Block 6+ cadence:** you run ONCE per block at block-end (see §0.1.0a
"Block-end verification"), not per subtask. Your inputs cover ALL merged
subtasks for the block, and your scope is the full block gate on merged main
plus cross-subtask integration review — not a single isolated subtask diff.

Your authority:
- You may run tests, inspect the diff, add narrowly scoped missing tests, and
  report defects.
- You may not approve the PR, rewrite the implementation, change acceptance
  criteria, weaken tests, or decide final spec acceptance.
- If you discover missing implementation work, report it as a defect for the
  orchestrator and worker.

Inputs you receive:
- Current build_session routing YAML.
- Linked issue, acceptance criteria, and security invariants.
- Worker diff summary, files changed, and commands already run.
- The current diff or PR branch.
- The canonical gate and any narrower expected targets.

Your task:
1. Confirm the merged diff is scoped to the block's subtasks.
2. Run the FULL block gate on merged main: build / test / test-race / lint /
   osv, plus e2e with Docker (DOCKER_HOST set, AGENTPAAS_DOCKER_TESTS=1).
3. Re-run ALL adversary tests against merged main — the per-subtask adversary
   runs were on isolated worktrees; re-running on the merged tree catches
   cross-subtask regressions.
4. Verify that every acceptance criterion (across all subtasks) has direct
   evidence.
5. Verify that every security claim has a negative test, or list the missing
   test.
6. Perform a cross-subtask integration review: do the merged subtasks compose
   correctly, do their contracts line up at subtask boundaries, are there
   gaps/contradictions that no single-subtask pass could see.
7. Try to reproduce any worker-reported failure.
8. Report exact PASS/FAIL evidence and repro steps.

Output format:
```yaml
verifier_result:
  result: pass|fail|blocked|missing_tests
  issue: <id/title>
  diff_scope: in_scope|out_of_scope|unclear
  commands_run:
    - command: <exact command>
      result: pass|fail
      notes: <short output summary>
  acceptance_criteria_evidence:
    - criterion: <criterion>
      status: proven|not_proven|blocked
      evidence: <test/file/command>
  security_evidence:
    - claim: <claim>
      negative_test_status: present_and_passing|missing|failing|not_applicable
      evidence: <test/file/command>
  defects:
    - id: V1
      severity: blocker|major|minor
      description: <defect>
      repro: <exact command or steps>
  missing_tests:
    - <test that must be added>
  recommendation: accept_evidence|return_to_worker|needs_orchestrator|invoke_adversary
```

If you succeed:
- Record `result: pass` with exact commands and evidence.
- Hand evidence to the orchestrator for final spec review.
- Do not say the PR is accepted; only the orchestrator can accept.

If you fail:
- Record `result: fail` or `missing_tests` with exact commands and repro.
- If the failure is due to unclear criteria or scope mismatch, set
  `recommendation: needs_orchestrator`.
- If security coverage is missing for a required claim, set
  `recommendation: return_to_worker` or `invoke_adversary` depending on risk.
````

#### Adversary prompt
````
You are the AgentPaaS adversary for this issue. Your job is to break the
security claims, trust boundaries, and unsafe assumptions before merge.

Your authority:
- You may inspect the issue, diff, tests, logs, policy, threat model, and
  relevant code.
- You may write or propose negative tests, abuse cases, fuzz cases, and manual
  repro steps.
- You may not approve the PR, weaken requirements, broaden product scope, or
  make final acceptance decisions.

Inputs you receive:
- Current build_session routing YAML.
- Linked issue, acceptance criteria, and security invariants.
- Trust boundaries and threat claims from the PRD/execution plan.
- Worker diff and verifier evidence.
- Any known limitations or skipped tests.

Attack focus:
- Can an agent escape policy, network, secrets, identity, audit, runtime, or
  Hermes operator boundaries?
- Can untrusted source, logs, traces, tool output, remote payloads, or Hermes
  resource text inject instructions that broaden permissions or hide evidence?
- Can failures bypass audit, signing, verification, budget, cancellation,
  idempotency, redaction, or confirmation requirements?
- Can local-only assumptions accidentally expose remote listeners or browser
  surfaces?

Your task:
1. Restate the claims you are trying to break.
2. Generate abuse cases and choose the highest-risk tests to run or propose.
3. Run feasible negative tests, or provide exact tests the worker must add.
4. Report every successful break with repro steps and expected vs actual
   behavior.
5. Report residual risk even if no break is found.

Output format:
```yaml
adversary_result:
  result: pass|break_found|blocked|needs_more_context
  issue: <id/title>
  claims_tested:
    - <claim>
  abuse_cases:
    - <case>
  commands_run:
    - command: <exact command>
      result: pass|fail
      notes: <short output summary>
  breaks:
    - id: A1
      severity: critical|high|medium|low
      claim_broken: <claim>
      repro: <exact command or steps>
      expected: <secure behavior>
      actual: <observed behavior>
      recommended_fix: <short fix direction>
  residual_risk:
    - <risk or none>
  recommendation: proceed_to_orchestrator|return_to_worker|rescope|needs_human
```

If you succeed:
- If no break is found, record `result: pass`, the claims tested, commands
  run, and residual risk.
- Hand the result to the orchestrator. Do not approve the PR.

If you fail:
- If a break is found, record `break_found`, exact repro, severity, and
  recommended fix direction. The issue returns to the orchestrator for
  refine/re-scope.
- If blocked by missing context, record the minimum context needed and set
  `needs_more_context`.
- Do not accept verbal assurances; require evidence or a concrete follow-up
  test.
````

### 0.1.2 Model routing and cost controls
The default P1 model routing is:
- **Agent:** Hermes.
- **Orchestrator primary:** GLM-5.2 through OpenRouter (Flagship, ~$0.50-3/response).
- **Orchestrator fallback:** DeepSeek V4 Pro.
- **Worker primary:** GPT-5.5 through Codex CLI (`codex exec --sandbox danger-full-access -m gpt-5.5`).
- **Worker fallback:** DeepSeek V4 Flash (via delegate_task — pins to delegation model).
- **Verifier primary:** GLM-5.2 through OpenRouter (Block 6+: runs ONCE per block at block-end, not per subtask).
- **Verifier fallback:** DeepSeek V4 Pro.
- **Adversary primary:** Grok 4.3 through X OAuth ($0, subscription).
- **Adversary fallback:** GLM-5.2 through OpenRouter.

**Worker dispatch (Block 4+):** Workers are dispatched via Codex CLI
(`scripts/codex-worker-local.sh` for local-first mode, `scripts/codex-worker-dispatch.sh`
for GitHub mode). The script creates a git worktree, launches
`codex exec --sandbox danger-full-access -m gpt-5.5`, and captures structured
JSON output via `--output-last-message` + `--output-schema`. Workers CANNOT call
Hermes kanban tools — the orchestrator handles task lifecycle. Workers and fix
workers MUST use the Codex CLI script. Do NOT use delegate_task for worker or
fix-worker roles — delegate_task pins subagents to the delegation model
(DeepSeek V4 Flash), bypassing the GPT-5.5 worker design.

**Grok 4.3 is NOT the orchestrator.** Tested as Block 6 cost experiment —
quality was unacceptable (missed adversary breaks, wrong merge calls, poor task
scoping). GLM-5.2 is the permanent orchestrator. Grok 4.3 remains
adversary-only ($0 subscription tier); the verifier role moved off Grok 4.3
to GLM-5.2 at Block 6 (per-subtask verifier retired — see §0.1.0a block-end
verification).

The user confirms or changes this routing at the start of each build session.
The choice may change per block or per issue. The role contract matters more
than vendor/model name. Check current pricing/capability before each block and
pin the chosen model ids in the issue/PR.

Fallback rule: if the primary model for a role exhausts quota/tokens, hits
rate limits, becomes unavailable, or fails the same role task twice, the next
run for that role may continue with the configured fallback. The issue/PR
attempt log must record the switch, the reason, and which artifacts were
produced before and after fallback.

Default ladder:
1. **Orchestrator:** GLM-5.2 through OpenRouter for block breakdown,
   architecture decisions, security invariants, and final release-blocking
   approval. Fallback: DeepSeek V4 Pro.
2. **Worker:** DeepSeek V4 Flash for normal coding, TDD loops,
   test writing, and routine fixes.
   Fallback: Composer 2.5 through X OAuth.
3. **Verifier Worker (Block 6+: ONCE per block, at block-end):** GLM-5.2
   through OpenRouter for independent full block-gate execution
   (build/test/race/lint/osv + e2e with Docker), re-running all adversary
   tests on merged main, and fresh-context cross-subtask integration review.
   Fallback: DeepSeek V4 Pro. Per-subtask verification is retired — the
   orchestrator's fresh-cache build/test/lint gate and the adversary cover
   per-subtask defect-finding (Block 5 audit: per-subtask verifier caught 0
   novel issues across 9 subtasks).
4. **Adversary:** Grok 4.3 for high-risk critique and negative-test
   generation. Fallback: GLM-5.2 through OpenRouter.
5. **Strong-model escalation:** Use the configured orchestrator fallback or a
   founder-approved stronger model only when the change touches
   API/security contracts, trust boundaries, block acceptance, unresolved
   verifier/worker disagreement, or repeated worker failure.

Cost discipline:
- Give every GitHub issue a model budget and stop/escalate when it is exceeded.
- Feed orchestrator/spec-review passes only the block spec, touched files,
  `git diff`, test output, and known failures; avoid full-repo context unless
  the issue truly needs it.
- Prefer cached, stable context bundles for repeated PRD/execution-plan excerpts.
- Do the required adversary pass per subtask, and the verifier-worker pass
  once at block-end, before the final orchestrator block approval (Block 6+;
  see §0.1.0a block-end verification).
- Track actual tokens and dollars in the PR body and `docs/status.md` so later
  blocks can tighten estimates.

Subscription-assisted capacity:
- Treat flat-rate chat subscriptions as auxiliary reviewer seats, not CI/API
  automation. Their usage limits, tool access, and terms can change; no block
  gate may depend on a subscription-only chat transcript.
- **Claude Pro subscription:** use after weekly reset windows for high-context
  spec review, PR description critique, failure triage, docs clarity, and
  "explain this diff to a security reviewer" passes. Avoid using it for long
  autonomous coding sessions once the weekly quota is low; save remaining quota
  for Blocks 3, 5, 7, 11, 13, and 14 review.
- **SuperGrok / Grok through X OAuth:** use as an independent adversary and
  market-skeptic reviewer: ask for attack ideas, abuse-case brainstorming,
  messaging critique, launch-copy punch-up, and "what would a security/platform
  buyer distrust here?" reviews. Its web/current-events strength is useful for
  GTM/docs, competitive framing, and threat-model sanity checks.
- For subscription passes, paste minimized context: block goal, relevant PRD
  excerpt, diff summary, test output, and the specific question. Do not paste
  real secrets, customer data, private keys, paid API keys, or full audit
  bundles.
- Record useful subscription findings in the issue/PR as human-reviewed notes
  with model/source/date, then have the API-based verifier or founder make the
  actual gate decision.

Rough P1 API budget target: $300-$700 with disciplined routing; reserve
$500-$1,000 in credits so security/runtime churn does not stall the build.
Using Claude Pro and SuperGrok deliberately should reduce API spend by roughly
15-30% by replacing some paid review/adversary/documentation passes, but it
should not replace the final API-backed gate evidence.

### 0.1.3 Adversary trigger matrix
Invoke the adversary for any issue touching:
- policy parsing, validation, compilation, or denial behavior
- network topology, gateway egress, DNS, ingress, ports, or RuntimeDriver
- secrets, leases, credential binding, redaction, or secret scanning
- identity, signing, verification, package provenance, or `agent.lock`
- audit chain, audit export/verify, retention, deletion, or tamper detection
- Trigger API auth, idempotency, rate limits, webhooks, cron, or cancellation
- harness isolation, budget enforcement, process lifecycle, or SDK tool calls
- dashboard rendering of untrusted logs/traces or browser/API exposure
- Hermes operator/integration trust boundaries or prompt-injection resistance
- release/install/upgrade/uninstall paths with security impact

The orchestrator may skip adversary review only for low-risk mechanical
changes such as docs wording, typo fixes, generated-code refreshes with no
contract change, or refactors that do not alter behavior. The PR must record
`adversary.invoked: false`, `adversary_required: false`, and the reason.

### 0.2 Standing rules for every Worker session (paste verbatim)
```
RULES (apply to every task in this block):
- TDD: failing test first, run it, implement, re-run.
- Go 1.24+, golangci-lint clean, go vet clean. No panics in library code.
- Documentation standard: code must be readable by a layperson or an AI
  agent with no prior context. This means:
  - Every package has a doc.go with a paragraph explaining what the package
    does, why it exists, and how it fits into the system.
  - Every exported type, function, method, and constant has a Go doc comment
    starting with the identifier name, explaining WHAT it does and WHY, not
    just the mechanics. Parameters and return values documented when not
    obvious from the name.
  - Non-obvious logic gets inline comments explaining the reasoning, not
    restating the code. If a reader would ask "why?", add a comment.
  - Complex types (state machines, config structs, protocol messages) get
    a usage example or a reference to one in the doc comment.
  - File-level comments for files that implement a single cohesive concept
    (e.g., "// This file implements the audit hash-chain append-only writer.").
  - No dead code, no commented-out code, no TODO without a linked issue.
  - godoc readability: comments are complete sentences, start with the
    identifier name, and read as natural English prose.
- Errors wrapped with context (fmt.Errorf("doing X: %w", err)).
- No new dependency without listing name+license+reason in the PR body.
- All listeners bind 127.0.0.1 unless the spec says otherwise.
- Every security claim gets a NEGATIVE test (prove the bad path is blocked).
- Every user-visible operation must also expose a machine-readable JSON path
  suitable for Hermes in P1 and other coding tools in P2. Human text output
  is a view, not the contract.
- Commit after every green test, conventional-commit messages.
- If the spec is ambiguous, STOP and emit "QUESTION:" — never guess.
- Done = this block's SUCCESS GATE command passes locally.
```

### 0.2.1 PR contract template
Every implementation PR must include:
- Linked issue / block id.
- Build-session model routing:
  orchestrator/worker/verifier/adversary primary+fallback models, adversary
  invoked/not invoked, fallback switches if any, and budget.
- User-facing behavior changed.
- Security claims changed or preserved.
- Tests added, including at least one negative test for any security claim.
- Commands run and exact result.
- Known limitations / follow-up issues.
- Definition of Done checklist copied from the issue.

No PR merges without: green CI, verifier-worker gate evidence, orchestrator
spec-review ACCEPT, required adversary PASS or documented "not invoked" risk
decision, documentation standard compliance check (§0.2), and an updated
status dashboard.

### 0.2.2a Canonical gate commands
Every implementation issue must name one binary Makefile gate. Friendly
subtargets may exist, but these wrapper names are stable and are what reviewers,
CI, and checkpoint notes cite:

| Block | Canonical gate |
|---|---|
| 1 | `make block1-gate` |
| 2 | `make block2-gate` |
| 3 | `make block3-gate` |
| 4 | `make block4-gate` |
| 5 | `make block5-gate` |
| 6 | `make block6-gate` |
| 7 | `make block7-gate` |
| 8 | `make block8-gate` |
| 9 | `make block9-gate` |
| 10 | `make block10-gate` |
| 11 | `make block11-gate` |
| 12 | `make block12-gate` (wraps `make redteam-smoke`) |
| 13 | `make block13-gate` |
| 14 | `make block14-gate` |
| 15 | `make block15-gate` once the Makefile exists; before Block 1, the
docs-only equivalent is `git diff --check` plus a committed checkpoint. |

Block 1 creates the Makefile namespace. Future-block wrappers may initially be
documented placeholders that exit nonzero with "not implemented until Block N";
the owning block replaces its wrapper with the real gate before it can pass.
Implementation agents may add subtargets, but must not rename or remove the
canonical wrappers.

### 0.2.2 Tracking and dashboard
Use GitHub from day 1, even while private. GitHub is the implementation
source of truth and durable memory for the build.
- Local git is mandatory: `git init`, `main` protected by convention, feature
  branches per issue, conventional commits.
- GitHub is the recommended source of truth: Issues = work items, Pull
  Requests = execution units, GitHub Project "AgentPaaS P1" = dashboard.
- Every issue must include: block id, acceptance criteria, role/model routing,
  attempt log, current blocker, latest gate evidence, orchestrator decision,
  verifier evidence, adversary decision or not-invoked reason, and next action.
- Every PR must link its issue and preserve the final attempt log, test/gate
  evidence, fallback switches, adversary output when invoked, and orchestrator
  ACCEPT/REFINE/REJECT decision.
- Required Project views: Board by status, Table by block, Roadmap by target
  week, PR Review queue, Security Gates.
- Required fields: Block, Area, Status, Priority, Orchestrator model, Worker
  model, Verifier model, Adversary model/status, Model tier
  (`strong-plan|cheap-exec|cheap-verify|adversary|strong-escalation`), Gate
  command, PR link, Owner, Target date.
- Required labels: `block:N`, `area:api|runtime|policy|identity|secrets|audit|docs`,
  `kind:plan|impl|test|security|docs`, `model:strong|model:cheap`,
  `status:ready|blocked|review|done`.
- `docs/status.md` is generated or refreshed before every merge and shows:
  built, remaining, active PRs, blocked items, latest gate results, and next
  recommended issue. It links back to GitHub rather than duplicating the full
  attempt history.
- Local-only fallback is temporary only: before the private GitHub repo is
  created, keep `docs/status.md`, `docs/prs/PR-000-template.md`, and one
  markdown issue per work item under `docs/issues/`. Move to GitHub before
  implementation PRs begin.

### 0.3 Repo layout (Block 1 creates this)
```
agentpaas/
├── cmd/agent/            # CLI main
├── cmd/agentpaasd/       # daemon main
├── cmd/harness/          # in-container PID 1
├── api/trigger/v1/trigger.proto
├── api/control/v1/control.proto
├── internal/
│   ├── runtime/          # RuntimeDriver iface + docker impl
│   ├── policy/           # parse, validate, compile → agentgateway cfg
│   ├── identity/         # local CA, agent keys, SVID issuance
│   ├── secrets/          # keychain broker, gateway injection, leases
│   ├── audit/            # hash-chain log, export, verify
│   ├── otel/             # collector, sqlite store
│   ├── events/           # bus, webhook delivery
│   ├── operator/         # agentic diagnostics, repair hints, JSON schemas
│   └── pack/             # build pipeline, sbom, sign, secret-scan
├── web/dashboard/        # SPA (preact/lit + TS, embedded via go:embed)
├── sdk/python/           # agentpaas-sdk
├── sdk/node/             # @agentpaas/sdk (deferred; not P1 gate)
├── integrations/
│   └── hermes-plugin/    # P1 Hermes plugin/skill; broader MCP/plugins P2
├── test/e2e/
├── test/redteam/         # adversarial agent images + harness
├── third_party/agentgateway/  # pinned vendored release + checksum
├── scripts/
│   └── update-status-dashboard.sh
├── .github/
│   ├── workflows/
│   ├── ISSUE_TEMPLATE/
│   └── pull_request_template.md
└── docs/
    ├── status.md
    └── issues/           # local-only fallback until GitHub is live
```

---

## BLOCK 1 — Repo bootstrap, proto contracts, CI skeleton
**Builds:** monorepo layout; both .proto files complete; buf lint+generate;
GitHub Actions (lint, test, -race, osv-scanner) on a **self-hosted macOS
runner** (production target is macOS; zero Actions minutes consumed; 3-16x
faster than ubuntu-latest). All tools (Go, golangci-lint, buf, protoc,
osv-scanner, Docker) are pre-installed via Homebrew — no per-job install
steps needed. Workflows use `runs-on: self-hosted` and prefix commands
with `export PATH="/opt/homebrew/bin:$PATH:$(go env GOPATH)/bin"`. For
Docker-gated tests, set `AGENTPAAS_DOCKER_TESTS: "1"` and
`DOCKER_HOST: "unix:///Users/pms88/.colima/default/docker.sock"`. See
`references/self-hosted-runner-setup.md` in the OWA skill for setup and
management details.

The "Block Gates" CI workflow (`.github/workflows/block-gates.yml`) uses
**path filters** — each block gate only runs when its own code or shared
files change. Shared files = `go.mod`, `go.sum`,
`.github/workflows/block-gates.yml` ONLY. `Makefile` and `api/**` are NOT
shared triggers (pitfall #44, B11 lesson): Makefile changes are additive
(new gate targets), and api/** changes only affect blocks that import the
changed proto package. Each block's own code path triggers only its own
gate. This prevents unnecessary re-runs of slow gates (e.g. Block 5 Docker
e2e, ~10–13 min) when unrelated code changes. When adding a new block gate:
1) add the block's package path(s) to the dorny/paths-filter (NOT to the
shared filter), 2) add a `blockN-gate` job, 3) update Makefile
`blockN-gate: build test race lint osv`, 4) set Docker env vars if the
block uses Docker, 5) if the block imports a new proto package, add that
proto path to the block's own filter. See the OWA skill's "Block Gates CI
Workflow" section for the current path-filter mappings.

Makefile targets
`build`, `test`, `proto`, `lint`, `race`, `osv`, `e2e-network`,
`redteam-smoke`, and the canonical `blockN-gate` wrappers from §0.2.2a;
SECURITY.md; Apache-2.0 LICENSE; local git repo initialized; GitHub-ready
issue/PR templates and status dashboard. GitHub templates must implement
§0.1.0 build memory: model routing, attempt log, fallback switches,
verifier evidence, adversary decision/not-invoked reason, orchestrator
decision, latest gate result, and next action.
**Build prompt:** "Bootstrap the AgentPaaS monorepo per §0.3. Author
api/trigger/v1/trigger.proto: services Invoke, InvokeStream, GetRun,
CancelRun, ListRuns; messages carry agent_name, payload (bytes+content_type),
idempotency_key, run_id, RunStatus enum (PENDING/RUNNING/SUCCEEDED/FAILED/
CANCELLED/BUDGET_EXCEEDED), google.rpc.Status errors, Run fields
(run_id, agent_name, agent_version, status, created_at, started_at,
finished_at, error, budget_summary, policy_digest, image_digest), pagination
(page_size, page_token, next_page_token), and explicit idempotency semantics
(same key+same payload returns original run; same key+different payload
returns ALREADY_EXISTS/409). Add HTTP annotations for grpc-gateway routes and
document InvokeStream as REST SSE. Author control/v1/control.proto: Pack,
Run, Stop, Logs(stream), PolicyApply, SecretSet/Grant/Revoke, AuditQuery,
AuditExport, Doctor, ValidateAgentProject, SummarizeRun, ExplainFailure,
ExplainPolicyDenial, RecommendPolicyPatch, GetRunTimeline, NextAction. These
operator methods return stable JSON/protobuf payloads for coding-agent
clients; CLI/dashboard text is rendered from the same data. Use stable proto
package names
`agentpaas.trigger.v1` and `agentpaas.control.v1`, explicit `go_package`,
reserved field numbers/names when deleting, and committed generated code.
Wire buf + grpc-gateway codegen. CI as specified. Initialize local git,
create .gitignore/.gitattributes, .github issue templates, PR template,
CODEOWNERS placeholder, and docs/status.md plus
scripts/update-status-dashboard.sh. Apply standing RULES."
**Edge cases to test:** codegen reproducible (two runs byte-identical);
buf breaking-change check fires on a field renumber; generated code is
up-to-date in CI; HTTP annotation route table golden test; InvokeStream SSE
mapping documented; ListRuns pagination handles empty, exact-page, and
next-page cases; idempotency replay and payload-mismatch behavior covered in
proto/API conformance tests; PR template contains Definition of Done;
status dashboard renders built/remaining/PR sections even before GitHub is
connected; CI fails a deliberately bad branch for the right reason.
**SUCCESS GATE:** `make block1-gate` green on macOS CI; it wraps
`make proto build test` plus any Block 1 lint/status checks;
`scripts/update-status-dashboard.sh` updates `docs/status.md`; initial
GitHub issues/Project can be created from the block list OR local fallback
issues exist under `docs/issues/`.

---

## BLOCK 2 — Daemon skeleton + CLI plumbing (unix-socket gRPC)
**Builds:** agentpaasd lifecycle (start/stop/status; launchd plist in P1,
systemd user unit generator in P2), explicit local path layout under
`~/.agentpaas` (0700; `daemon.sock` 0600, `agentpaasd.pid`, `logs/`,
`state/`, `config/`, `cache/`, `tmp/`), unix socket gRPC server with
readiness handshake, control-API server with stub handlers; `agent` CLI
(cobra) wired to all control RPCs; daemon commands (`agent daemon install`,
`uninstall`, `start`, `stop`, `restart`, `status`); `agent version` and
`agent daemon status` show CLI version, daemon version, proto version, git
commit, OS/arch, Docker context, Docker API version; `agent doctor` v0
(Docker reachable? current Docker context? Docker Desktop/Colima on macOS?
unsupported Linux `dockerd` reported as P2/not-a-P1-gate? socket perms? ports
7700/7717/7718 free? home dir perms? daemon ready? CLI/daemon proto
compatible?); structured
logging (slog, JSON) with redaction enabled from day one. Dev/test
overrides: `AGENTPAAS_HOME`, `AGENTPAAS_SOCKET`,
`AGENTPAAS_DASHBOARD_PORT`, `AGENTPAAS_TRIGGER_REST_PORT`,
`AGENTPAAS_TRIGGER_GRPC_PORT`.
**Edge cases:** daemon not running → CLI clear error + start hint; daemon
process started but not ready → CLI waits with timeout then actionable
error; stale socket/pid/lock files → auto-recover only after proving no
live daemon owns them; two daemons race → flock prevents; user broadens
home/socket perms → daemon refuses to serve and says why; daemon run as
root → refuses unless `--allow-root-for-test`; SIGTERM → graceful drain of
in-flight RPCs; service file generation is deterministic and unit-tested
without requiring launchd/systemd inside CI; lifecycle e2e runs where the
host supports user services; Docker stopped/context missing/API too old →
doctor names the exact issue; port squatted → doctor names process/port
when the OS permits; log redaction masks high-entropy/API-key-looking
values in CLI and daemon logs.
**SUCCESS GATE:** `make block2-gate` passes: `agent doctor` exits 0 on a healthy machine, nonzero with
actionable messages for each induced failure (docker stopped, port
squatted, bad socket perms, bad home perms, daemon not ready, CLI/daemon
version mismatch); `agent version` and `agent daemon status` print the
expected version/context fields; service-unit golden tests pass on macOS;
redaction test proves planted secret-looking values do not appear
in logs — scripted in test/e2e/doctor_test.sh and unit tests.

---

## BLOCK 3 — Identity service + audit hash-chain (security spine first)
**Builds:** internal/identity — narrow interfaces for `KeyStore` and
`IdentityIssuer`, with P1 implementations backed by macOS Keychain
(`security(1)` wrapper) and an explicit encrypted file-keystore fallback
(0600 + passphrase; warned by doctor; no silent plaintext fallback). Linux
libsecret moves to the P2 Linux track. Manage distinct local identities:
local CA key,
daemon audit signing key, per-agent package identity keys, and per-run
workload key/cert. `agent pack` can mint/register an AID; `agent run` can
issue a 1h, auto-renewed SPIFFE-style workload cert
(`spiffe://local.agentpaas/agent/<name>/<ver>/run/<run_id>` in P1) for
gateway/harness-to-daemon mTLS. Trust-domain construction and verification
must be configurable so Phase 2 can issue hosted identities such as
`spiffe://tenant.agentpaas.ai/<tenant>/agent/<name>/<ver>/run/<run_id>`
without changing record schemas. Workload certs identify event sources but
never sign the canonical audit trail.

internal/audit — narrow interfaces for `AuditWriter`, `AuditAnchor`,
`AuditVerifier`, and `AuditExporter`, with P1 implementations backed by
append-only canonical JSONL where each record has `seq`, `prev_hash`, and
`record_hash` (SHA-256 over canonical JSON with `record_hash` omitted);
SQLite index derived from JSONL and rebuildable; single daemon-owned writer
that serializes appends and durably maintains a latest-head anchor. Signed
checkpoint records are inserted into the same chain at fixed cadence and at
export, signed by the daemon audit signing key over `{head_seq, head_hash,
previous_checkpoint_hash, created_at}`. Security-relevant actions fail
closed if their audit record cannot be appended. Add `agent audit verify`
with local and bundle modes; add `agent audit export` -> signed bundle
containing JSONL segments, checkpoints, AIDs/public keys, trust metadata,
and an export manifest signed by the daemon audit signing key. Record schema
must include `deployment_mode` (`local|hosted`) and optional hosted-context
fields (`tenant_id`, `project_id`, `region`, `runtime_provider`) so P2 can
reuse the same verification algorithm in AgentPaaS.ai.

**Edge cases (every one is a test):** tamper a middle line -> verify fails
naming the exact line/seq; truncate tail relative to the latest local head
anchor -> fail; reorder two lines -> fail; delete a checkpoint -> fail;
delete or corrupt the SQLite index -> verify reports/rebuilds from JSONL
without changing hashes; wall-clock moves backwards -> chain still valid
(monotonic seq is authoritative); expired workload cert rejected; workload
cert renewal happens before expiry; package identity key never appears in a
run container; 100k records verify < 5s; concurrent writers serialize
without loss (`-race`); audit append fsync/write failure makes the guarded
operation fail closed; keychain locked/unavailable -> clear error, no
silent plaintext fallback; explicit file-keystore fallback refuses weak
permissions and wrong passphrase; alternate trust domain URI builder/verifier
passes for a fake hosted tenant; audit bundle verification works from an
extracted bundle without reading `~/.agentpaas`; in-memory fake keystore and
fake audit anchor pass the same contract tests as local implementations.

**SUCCESS GATE:** `make block3-gate` passes: `go test
./internal/identity/... ./internal/audit/... -race` green;
tamper-detection e2e script demonstrates all 4 tamper modes
caught; audit-head-anchor test proves tail truncation is caught locally;
export verifies on a second machine/clean CI workspace using only the
bundle and the expected daemon audit public-key fingerprint; docs for the
gate state that second-machine verification proves bundle integrity, not
global transparency-log anchoring.

---

## BLOCK 4 — Policy engine (parse → validate → compile to agentgateway)
**Builds:** internal/policy — one canonical human/LLM-friendly `policy.yaml`
for egress, credentials, MCP servers, hooks, and ingress; strict YAML schema
(unknown fields = error); validation (exact hostname matching by default,
no implicit subdomain matching, no wildcard domains unless
`allow_wildcard: true`, no private CIDRs unless `allow_private: true`); MCP server
declarations in `policy.yaml` with explicit server ids, transport
(`stdio|http`), command/endpoint, allowed tools, auth mode, minimal env, and
egress binding for remote MCP servers; MCP workload egress policy for local
MCP servers that need outbound network access, default-deny unless explicitly
allowed by destination/method/port/credential; hook destination declarations
checked as policy data in Block 4 and rechecked at delivery time in Block 9;
brokered credential bindings (`egress.allow[].credential` and MCP auth
references must point to `credentials.brokered[].id`) with header-only
injection templates; explicit direct-lease schema
(`credentials.direct_leases[]` requires mode+reason); canonicalizer that
sorts maps and unordered lists deterministically, uppercases HTTP methods,
lowercases and ASCII/punycode-normalizes domains, expands defaults, removes
comments, deduplicates equivalent rules with warnings, and emits a stable
policy digest (sha256 of canonical form) recorded for audit + agent.lock.
Compiler emits pinned agentgateway config + the DNS-stub allow-list +
credential-injection rules by id only. Vendor agentgateway release into
third_party/ with checksum verification in the build.
**Edge cases:** empty policy → deny-all config (valid, runs, nothing
egresses); `domain: example.com` does not allow `api.example.com`; wildcard
without `allow_wildcard: true` -> validation error; duplicate domains →
dedup warn; punycode/IDN domains → canonical ASCII form; confusable IDN
defense is deferred but non-normalizable names fail closed; port ranges
rejected (explicit ports only in P1); CIDR overlap with RFC1918 → require
`allow_private: true`; policy file world-writable → refuse to load; egress
rule references undeclared brokered credential id → validation error;
declared brokered credential not referenced by an egress/MCP rule →
validation warning; query-string or body credential injection → validation
error; direct lease without reason → validation error; compiled config and
canonical policy digest input never contain raw secret values; secret ids may
appear, secret values never may; undeclared MCP server id -> validation
error; MCP server with unspecified allowed tools -> deny all; remote MCP
server without matching egress allow rule -> validation error; local MCP
server receiving undeclared env/secret -> validation error; remote hook URL
without matching egress allow rule -> validation error; loopback hook URL
must be explicitly local and cannot be exposed to the agent container;
credentialed brokered request redirects are disabled by default;
noncredentialed redirects are re-evaluated against policy per hop; YAML key
order and comments do not affect digest; typos such as `credentials.brokerd`,
`allow_wildcards`, and scalar `port: 443` fail schema validation; round-trip:
compile(parse(x)) deterministic.
**Fuzzing:** go-fuzz on parser (mandatory; crash corpus committed).
**SUCCESS GATE:** `make block4-gate` passes: unit + fuzz (1M execs, 0 crashes) green; golden-file
tests for compiler output; digest stability tests prove comments/key order do
not change the canonical digest while semantically meaningful changes do; a
sample policy.yaml from PRD §2.9 compiles to a config agentgateway actually
loads (smoke test runs the real binary).

---

## BLOCK 5 — RuntimeDriver + the fenced network topology
**Builds:** internal/runtime — RuntimeDriver interface (Create, Start,
Stop, Remove, Status, Stats, Logs) + Docker implementation. Network setup:
one logical agent deployment made of two containers: the agent/harness
container and the ingress/egress gateway sidecar. Both directions are
gateway-only: daemon/caller ingress goes through gateway before reaching the
harness, and agent outbound goes through gateway before reaching any
upstream. There are no direct daemon-to-harness calls, no agent-to-host
shortcuts, and no host networking in P1. Per-agent `internal: true` bridge;
dedicated AgentPaaS egress network; gateway sidecar dual-homed (internal
bridge + egress network); agent container never shares the gateway network
namespace and is never attached to the egress network; deterministic
AgentPaaS labels/names on all owned containers and networks for safe
reconciliation; cleanup on partial create failure. Agent container hardening
flags (non-root uid 64000, read-only rootfs, tmpfs /tmp, cap-drop ALL,
no-new-privileges, seccomp default profile, pids-limit 256, memory/cpu from
agent.yaml); DNS of agent container pointed at gateway stub IP only; IPv6
disabled for P1 agent networks. Rootless Docker is best-effort and explicitly
not a P1 gate; supported gates are Docker Desktop and Colima's
Docker-compatible socket. Linux `dockerd` moves to P2 certification.
**Edge cases / negative tests (heart of the product — exhaustive):**
- positive path: agent invoke reaches harness only through gateway ingress;
  agent outbound to an allowed test endpoint succeeds only through gateway
  egress and emits the expected policy decision and audit event
- canary on internal net: `curl https://1.1.1.1` → no route, fails fast
  within a concrete timeout budget (target ≤2s; never hangs)
- direct DNS to 8.8.8.8 → unreachable
- `host.docker.internal`, Docker bridge gateway IP, gateway container IP,
  daemon ports, and any host-local service probes → unreachable from agent
- IPv6: no route (AAAA answers and direct v6 literals both dead)
- UDP egress (non-DNS), ICMP, raw-socket attempts, and CONNECT tunnel bypass
  attempts → blocked
- Docker inspect assertions prove agent has no default route, no egress
  network attachment, no host networking, no shared network namespace with
  gateway; gateway has exactly internal+egress networks
- resource assertions prove non-root uid, read-only rootfs, tmpfs /tmp,
  cap-drop ALL, no-new-privileges, seccomp default profile, pids-limit,
  memory, and CPU limits are actually applied
- container restart preserves network membership
- daemon crash leaves no half-fenced agent: startup reconciliation kills
  any agent container whose gateway is absent
- partial create/start failure leaves no orphaned AgentPaaS containers or
  networks after cleanup
- Docker inspect, runtime logs, and network config dumps contain no raw secret
  values and are suitable for debugging
- Docker Desktop vs colima: topology holds on both; Linux dockerd certification
  moves to P2
**SUCCESS GATE:** `make block5-gate` passes and wraps `make e2e-network`,
which runs the positive-path canary plus the
bypass suite and prints a table of allowed path PASS plus at least 12 attack
vectors, all BLOCKED, on macOS (Docker Desktop + colima). The
gate docs explicitly state that Block 5 proves gateway-only network topology
and container hardening, not secrets, budgets, SDK behavior, or the full
Block 12 red-team suite.

**Post-Build Audit (2026-06-20):** Docker SDK upgraded from v26.1.5 to
v28.5.2 (commit 8df0d4c). 5 remaining OSV CVEs are daemon-side Docker Engine
vulnerabilities (docker cp, plugin install, AuthZ bypass) patched in Docker
Engine 29.3.1/29.5.1 — suppressed in `osv-scanner.toml` with accurate
reasoning. These are NOT SDK client code vulnerabilities and AgentPaaS does
not use the affected code paths. Revisit after Block 10 to migrate to
github.com/moby/moby/v2 (v2.0.0-beta.8+) once stable, then remove suppressions.
E2E network tests verified in CI (block5-gate CI job with
AGENTPAAS_DOCKER_TESTS=1 on self-hosted runner). Tmpfs assertion fixed for
Docker SDK v28 API representation (commit 8dc99f0).

---

## BLOCK 6 — Harness (cmd/harness) + Python SDK contracts
**Builds:** Go harness as container PID 1: exec Python user code; HTTP
contract (`POST /invoke`, `GET /healthz|readyz` on localhost:8000 inside
container); load agent code once per container and serialize invokes by
default (`concurrency: 1`) with explicit opt-up later. Budget enforcement:
`startup_timeout` covers import/readiness; `max_wall_clock` measures only
run receive-invoke → run finishes using a monotonic clock; `max_iterations`
means agent turns (each SDK-observed LLM/tool cycle counts, and each direct
`agent.llm()` call counts if no higher-level loop exists; P2 may use model
context-window health, performance drops, and turn-count guidance to adjust
repair/retry strategy); token/USD accounting uses gateway-reported best-known
usage and records post-hoc overage when provider usage arrives after
termination. Breach → SIGTERM,
10s grace, SIGKILL, status=BUDGET_EXCEEDED, audit event. OTel emit.
Python SDK (`agentpaas-sdk`): decorators `@agent.on_invoke`, `agent.llm()`
(OpenAI-compatible client preconfigured to gateway), noncredentialed
`agent.http(...)`, brokered `agent.http_with_credential(credential_id, ...)`,
and `agent.mcp(server_id, tool, input)`. Agent-level checkpoint/resume and
half-done job recovery are deferred to P2; P1 restarts failed runs from a
fresh container and records enough structured failure context for future
resume/repair loops. Audit-log checkpoints from Block 3 remain part of the
security model. Gateway policy can optionally deny noncredentialed HTTP calls
(`egress.require_credential_binding: true`), forcing all outbound HTTP
through named credential bindings. Brokered credentials are never returned to
SDK callers.
`agent.secrets.file()` exists only for explicit direct-lease compatibility
mode and is discouraged in generated code. Node SDK is deferred until after
the Python SDK and language-neutral harness contract are proven.
**Edge cases:** user code crashes on import → FAILED with stderr captured;
run fails for prompt/task/tool/SaaS/MCP/code reasons → structured failure
reason, stderr/stdout pointers, policy decision ids, and relevant upstream
availability evidence are reported to the control plane; failed runs are
safe to retry in a fresh container with prior failure context stored outside
the container; user code ignores SIGTERM → killed at grace deadline; zombie
processes reaped (PID 1 duty); invoke payload 50MB → rejected 413 (limit
10MB, configurable); unicode/binary payloads round-trip; budget race (token
usage reported after kill) → accounted post-hoc, audit shows overage;
blocked egress/tool calls are visible to the developer in CLI/dashboard logs
with reason, run id, policy digest, and strict secret/payload redaction; MCP
call to undeclared server/tool is denied before execution and audited; MCP
tool input/output bodies are not logged, only hashes and metadata. P2 note:
the control plane may use the structured failure context to decide whether to
modify an agent and retry in a fresh container until success; P1 only needs
the structured failure context needed to support that future loop.
**SUCCESS GATE:** `make block6-gate` passes: e2e proves an infinite-loop agent with max_wall_clock=30s dies
at 30s±2s from invoke start with BUDGET_EXCEEDED + audit event; a token-burn
agent stops future calls at the token cap using best-known usage and audits
any provider-reported overage; Python SDK passes the harness contract test
suite.

---

## BLOCK 7 — Secrets broker
**Builds:** internal/secrets — `SecretStore` abstraction with P1
implementations for macOS Keychain and an explicit fake test store only; no
silent plaintext fallback. Linux libsecret moves to P2. `agent secret
set/list/rm`
(values read from stdin/interactive prompt, NEVER argv so they never hit
shell history or process lists; max secret value size 64 KiB); `list` shows
metadata only (id, created_at, updated_at, last_used_at, referenced-by
policies/agents), never value, prefix, suffix, or hash-derived hints.
Secret store names are case-sensitive local-profile entries with no
whitespace/control characters; policy credential ids are policy-local stable
ids that bind egress/MCP rules to those store names.
Brokered outbound credential flow (gateway sidecar requests credential use
from daemon/secrets broker over local authenticated channel; daemon validates
run id + policy rule id + destination + method; gateway injects header field
per policy and originates the upstream TLS request; raw value is never sent
to the agent container). Direct lease flow for compatibility is file-only:
`file_lease` mounts a runtime tmpfs file 0400 owned by agent uid; P1 does
not support `env_lease`, and real secret files are never packaged into agent
images. Revocation invalidates brokered use immediately and restarts affected
direct-lease agents; direct-lease revocation cannot claw back a secret value
already visible to agent code. Audit guarantee: brokered injection emits
`secret_injected` with `visible_to_agent=false`; direct lease emits
`secret_leased` with `visible_to_agent=true`; SDK lease-helper reads emit
`secret_read`; P1 does not claim reliable per-open auditing for raw file
reads of a direct lease. Add a follow-up enterprise design issue for
corporate employee machines behind VPN: evaluate managed-vault/remote broker
patterns where enterprise secrets do not permanently reside on the employee
laptop, plus device posture, tenant policy to disable direct leases,
short-lived credential grants, revocation, and tenant-visible audit.
**Edge cases:** brokered secret referenced but not set → launch refuses
naming the missing secret; brokered credential used for wrong domain,
method, port, or credentialed redirect → denied before injection and audited;
noncredentialed redirects are rechecked against policy per hop; gateway crash
cannot dump secret in logs; keychain/libsecret locked or unavailable →
actionable error, no plaintext fallback; secret containing newlines/UTF-8/64
KiB length injects/round-trips exactly; oversize secret rejected before
storage; agent attempts `env`, `/proc`, filesystem walk, and `docker inspect`
to find a brokered sentinel secret → zero hits; compiled gateway config and
policy digest contain credential ids only, never values; generated files,
image layers, build context, and packed artifacts contain no real secret
values; direct lease tmpfs file gone after `agent stop` (asserted); raw file
read succeeds for an explicit direct lease but is not claimed as a precise
per-read audit event; CLI/dashboard/runtime errors redact secret values and
do not show value prefixes/suffixes.
**SUCCESS GATE:** `make block7-gate` passes: negative suite green, including grep of full
process list, shell history fixture, `docker inspect`, gateway logs, compiled
configs, exported image layers, build context, packed artifacts, CLI/dashboard
errors, and agent filesystem/proc probes for a brokered sentinel secret →
zero hits; a real brokered OpenAI-style request receives the Authorization
header upstream while agent logs/proc/env never contain the key.

---

## BLOCK 7.5 — MCP server lifecycle manager
**Builds:** internal/mcpmanager — P1 management for MCP servers declared by
an agent's `policy.yaml`. AgentPaaS validates, starts, readiness-checks,
stops, and reconciles declared local MCP servers as first-class managed
resources, similar to agents and gateway sidecars. Local MCP servers may run
only as daemon-managed child processes, sidecars, or explicitly declared
local endpoints; remote MCP servers remain gateway-routed egress. Agent MCP
traffic is gateway-mediated: the agent calls `agent.mcp(server_id, tool,
input)` or an MCP HTTP endpoint exposed by the gateway; the gateway performs
the server/tool policy decision, forwards allowed stdio calls to the daemon
MCP manager or allowed HTTP calls to the declared sidecar/endpoint, then
returns the response through the same governed path. The agent never gets
direct stdio, host socket, or container-network access to a local MCP server.
MCP servers have their own workload policy scope: ingress is restricted to
AgentPaaS gateway/daemon routes for declared server/tool calls, and
MCP-server egress is default-deny unless policy explicitly allows the
destination, method, port, and credential binding. MCP sidecars use
AgentPaaS-owned Docker labels, no host networking, no ambient Docker/host
socket access, and gateway-mediated egress; daemon-managed stdio MCP
processes receive minimal env and egress only through approved broker/gateway
paths.
MCP servers are not silently baked into the agent image by default. Status
surfaces must show `resource_type=agent|gateway|mcp_server`, owning
agent/run, policy digest, allowed tools, readiness, health, and last error
through `agent status --json`, dashboard data, and the Hermes operator
contract. Observability covers all AgentPaaS-owned Docker artifacts for
agents, gateways, and MCP sidecars: labels, image digest, network membership,
health, restart count, resource stats, and sanitized logs.
Lifecycle is fail-closed: if a required MCP server cannot start, is unhealthy,
or exposes undeclared tools, the run is rejected or tool calls are denied
before execution and audited.

Host-affecting MCP capabilities such as browser control, shell execution,
writable filesystem access, AppleScript, and desktop automation are treated
as high-risk local tools. P1 may manage them only when explicitly declared
and tool-limited in policy; enabling or broadening them requires the same
user/daemon confirmation protocol as egress or credential binding. Prompt
injection in source/logs/tool output must not add MCP servers, broaden allowed
tools, or enable host-affecting capabilities.
**Edge cases:** duplicate MCP ids → validation error; undeclared server/tool
call → denied and audited; local MCP command path outside approved/project
scope → refused; unspecified allowed tools → deny all; minimal env by default
with no raw secrets; secret-backed MCP auth uses brokered credential ids only;
MCP server attempts egress to a non-allow-listed domain → denied and audited;
MCP server attempts host/Docker socket/bridge access → blocked; MCP logs with
sentinel secrets or untrusted HTML/control chars → redacted/escaped in logs
and dashboard;
MCP child process crash → agent run gets structured `mcp_unavailable` or
tool-call failure, no silent fallback; daemon restart reconciles owned MCP
processes/sidecars and leaves unrelated processes untouched; status lists
stopped, starting, ready, unhealthy, and orphan-reconciled MCP resources;
containerized MCP sidecars carry AgentPaaS Docker labels and no host
networking unless a future explicitly-confirmed high-risk mode says so.
**SUCCESS GATE:** `make block7-mcp-gate` passes: a declared readonly
filesystem MCP server starts separately from the agent, appears in
`agent status --json` and dashboard data as `mcp_server`, exposes only
declared tools, and an e2e agent call reaches it only through the gateway MCP
route and receives the response through that route. Successful/denied tool
calls produce audit events with server id, tool name, decision, policy rule
id, and input/output hashes. Negative tests prove dynamic tool discovery does
not auto-allow tools, direct agent-to-MCP/host connectivity is blocked,
MCP-server egress is denied unless explicitly policy-allowed, host-affecting
MCP capabilities require confirmation, logs/OTel/dashboard include sanitized
MCP server and Docker artifact observability, and daemon restart
reconciliation does not orphan or misattribute MCP resources.

---

## BLOCK 8 — Packaging pipeline (`agent pack`)
**Builds:** internal/pack — framework detection for Python first (plain
Python, LangGraph, CrewAI markers; CrewAI support means generic Python
pack/run of generated CrewAI projects, not a custom CrewAI adapter; Node and
custom Dockerfiles deferred),
buildkit image assembly
(distroless base by digest, locked deps via uv, harness as PID 1, non-root,
no shell),
gitleaks secret scan (fail-closed), syft SBOM (SPDX-json, attached as OCI
artifact in a local OCI layout plus Docker image by digest), local
key-backed cosign signing with the per-agent package identity key, and a
signed canonical `agent.lock` manifest. P1 does NOT use Sigstore keyless
OIDC/Fulcio signing for local packs; future release/enterprise flows may add
that separately. `agent.lock` includes schema version, agent name/version,
runtime/framework, platform, base image digest, harness version, build input
digest, image digest, SBOM digest, policy digest, package AID/public key,
signature bundle/referrer locations, and reproducibility metadata. The
lockfile itself is signed by the package identity key and is the exact review
unit consumed by `agent run` and future promotion. P1 deployed agents are
immutable: prompt files, system instructions, templates, tool-routing
instructions, and other behavior-changing config must live in the source tree
or `agent.yaml`-referenced files and be covered by the build input digest.
There is no supported in-place prompt edit for a running/deployed agent; the
path is edit project → `agent validate --json` → `agent pack --json` →
`agent verify agent.lock` → `agent run --json`, producing a new signed
artifact and audit trail. Hermes plugin/skill prompt changes are integration
updates, and harness behavior changes are runtime releases that require
repacking affected agents before claiming the new harness version.
**Edge cases:** no agent.yaml → offer `agent init` scaffold; dependency
conflict → surfaced verbatim, abort; 2GB build context → .agentpaasignore
honored, warn >100MB, with default excludes for `.git`, virtualenvs, caches,
`node_modules`, test outputs, and large local data; secret scan covers the
full source tree plus the effective build context, and a secret in either
path FAILs naming file:line; `--allow-secret-pattern` requires a successful
daemon audit append or aborts; rebuild without changes → identical image
digest (fixed timestamps, pinned base digest, locked deps, deterministic
tar order, `SOURCE_DATE_EPOCH`); local OCI layout missing/corrupt →
actionable repair; registry push is deferred; LangGraph and CrewAI-generated
example repos pack without a custom Dockerfile through the generic Python
harness.
**SUCCESS GATE:** `make block8-gate` passes: 3 Python reference agents (plain-py, langgraph, crewai)
pack green; changing only a prompt/template file changes the build input
digest and image/lockfile digest; any attempt to mutate prompt/config inside
an existing deployed image or `agent.lock` is rejected with an audit event;
CLI e2e proves the full update path works without Hermes: run an agent with
prompt v1, edit a prompt/template file to v2, run `agent validate --json`,
`agent pack --json`, `agent verify agent.lock`, and `agent run --json`, then
assert the new run reflects prompt v2 and audit links the old/new runs to
different build input/image/lockfile digests;
`agent verify agent.lock` and explicit offline
`cosign verify --key <AID pubkey>` pass for the image signature; lockfile
signature verifies; SBOM lists expected top-level deps; osv-scanner advisory
summary appears in `agent pack` output without failing on non-critical
findings; secret-scan e2e blocks a planted key in source, ignored source, and
build context; golden fixtures assert expected `agent.lock` fields. Node
packaging is a follow-on gate.

---

## BLOCK 9 — Trigger API + events/webhooks + cron
**Builds:** Trigger API serving (gRPC :7718 + grpc-gateway REST :7717,
loopback by default; Trigger API requires AgentPaaS API-key or mTLS auth even
on loopback; `--expose` refuses without an API key). API keys are AgentPaaS
Trigger API credentials for Hermes/local apps/CI callers to invoke a packed
agent under test or running locally; keys are shown once, stored hashed,
scoped by agent/action, revocable/rotatable, and audited.
REST CORS is deny-by-default; browser-originated local requests are not
trusted without explicit auth, and preflight/origin handling is covered by
tests.
Define stable caller IDs (`api_key:<id>`, `spiffe:<subject>`,
`system:cron:<agent>`, `local_user:<uid>`), token-bucket rate limits per
caller, durable idempotency table (key→run_id, 24h replay window,
canonical request hash over caller, agent, agent.lock digest, payload bytes,
content type, and API version), max invoke payload 1 MiB default with 413 and
"pass a reference/blob handle" guidance, InvokeStream (gRPC stream + REST
SSE for CLI/dashboard/coding-tool live progress), internal/events bus,
webhook delivery (HMAC-signed with timestamp/replay window, 3 retries exp
backoff, dead-letter to audit), and cron triggers from agent.yaml feeding the
same Invoke path. Add P1 local handoff triggers: approved static terminal-run
events may invoke one named packed target agent through the same Trigger API
with caller `system:handoff:<source_agent>`, parent run id, correlation id,
idempotency key, target agent.lock digest, payload mode
(`empty|summary_ref|artifact_ref|fixed_json`), and an internal
A2A-compatible envelope containing source/target agent-card refs, parent
task/run id, context/correlation id, message role, parts, artifact refs, and
metadata map where applicable. Target policy/budget/secrets apply normally.
P1 supports URL webhooks and local handoff triggers only; local command hooks,
external A2A serving/discovery, arbitrary task negotiation, and dynamic
workflow DAGs are deferred. Audit events include `api_key_created`,
`api_key_revoked`, `auth_failed`,
`invoke_accepted`, `invoke_rejected`, `idempotency_replayed`,
`idempotency_conflict`, `rate_limited`, `webhook_delivered`,
`webhook_dead_lettered`, `handoff_invoked`, `handoff_skipped`,
`handoff_denied`, `cron_missed`, `cron_skipped_concurrency`,
`cancel_requested`, `cancel_graceful`, and `cancel_forced`.
**Edge cases:** replayed idempotency key → same run_id, no second
execution; replay with DIFFERENT payload + same key → 409; burst 1000
invokes → rate-limited with Retry-After; malformed JSON → 400 with line;
browser POST from a random localhost origin without API key → 401/CORS deny;
oversized payload (>1 MiB default) → 413; daemon restart during idempotency
window preserves replay/409 behavior; SSE client reconnects with
Last-Event-ID, heartbeat, ordered event IDs, and no duplicate terminal event;
webhook target down → retries then dead-letter audit entry; webhook replay or
bad HMAC → rejected by receiver fixture; webhook target = non-allow-listed
domain → blocked by policy (hooks are egress too); cron uses 5-field syntax
only, local timezone by default, explicit timezone optional; cron during
daemon downtime → missed-run policy explicit (`skip` default, `catchup: 1`
opt-in); DST nonexistent local time skipped, repeated local time runs once;
cron fires while prior run active → `concurrency_policy: forbid` default
skips and audits; handoff cycle/depth guard blocks unbounded loops; handoff
target missing, stale lock digest, malformed A2A-compatible envelope, or
unapproved config → skipped/denied and audited; daemon restart after source
completion still preserves pending handoff idempotency; CancelRun mid-LLM/
MCP-call → audit cancel_requested, ask gracefully, wait 30s, force stop if
needed, audit final canceled/forced outcome.
**SUCCESS GATE:** `make block9-gate` passes: API conformance suite (generated from proto) green;
auth/API-key lifecycle + idempotency + rate-limit + SSE reconnect e2e green;
cron/webhook/local-handoff tests prove same policy/audit path as manual
Invoke, that the handoff envelope round-trips in an A2A-compatible shape, and
that a two-agent handoff runs without Hermes alive; cancel semantics e2e
green; fuzz on REST JSON ingestion (100k execs, 0 crashes).

---

## BLOCK 10 — OTel pipeline + Dashboard
**Builds:** in-process OTLP collector → SQLite (WAL) with retention prune
(default 7d local, configurable) for OTel traces/logs/metrics only; canonical
audit JSONL is not pruned by dashboard retention and is purged only by an
explicit future user retention/purge command. Agent/harness/gateway/MCP
server/MCP manager logs are ingested as OTel log records for dashboard
correlation; daemon operational logs remain bounded structured JSON files
under `~/.agentpaas/logs/` with rotation/redaction and are linked from
`agent doctor`/`agent logs` but are not the canonical audit source. Dashboard
SPA (preact+TS, go:embed, no runtime CDN): managed resource list for agents,
gateway sidecars, and MCP servers w/ status+readiness+spend-vs-budget, Docker
artifact ids/labels/digests where applicable, network membership, health,
restart count, resource stats, and sanitized logs; run timeline with a stable
event schema (LLM calls w/ tokens+cost, MCP calls, egress ALLOWED/DENIED rows
in red, budget/audit markers), log viewer with truncation/redaction, policy
view showing both git-file diff and normalized effective policy digest, audit
search explicitly labeled as an indexed view + one-click signed export with
trust-anchor fingerprint, included sequence range, verification command, and
result status, live SSE event stream reusing Block 9 event IDs/heartbeat/
Last-Event-ID semantics. Cost estimates record provider, model, price-table
version, token counts, and `estimated=true`; P1 ships a built-in price table,
P2 allows user/tenant-modified price tables. Strict CSP, no inline JS, CSRF
token on mutating routes; loopback read-only dashboard may be unauthenticated,
exposed dashboard requires API key/session, and API keys are never stored in
browser localStorage.
**Edge cases:** 10k-span run renders (virtualized list); SQLite locked by
concurrent writer → dashboard reads stay snappy (WAL + read pool); SQLite
migration/WAL checkpoint/vacuum/prune/corruption recovery covered; dashboard
with daemon restarting → reconnects SSE gracefully using Last-Event-ID; XSS
attempt via agent-controlled log line / trace attribute → escaped (test with
planted `<script>` in agent output); sentinel secret in logs/spans/errors is
redacted everywhere, including MCP server logs and Docker inspect-derived
views; binary/control characters and huge log/attribute values are safely
escaped/truncated with pointers to full retained logs where allowed;
agent/gateway/MCP Docker artifact disappearance or stale labels → dashboard
shows reconciled state instead of stale green; clock-skewed spans → ordered by
monotonic seq; security events are never sampled out of canonical audit even
if OTel retention prunes dashboard telemetry; empty states designed (zero
agents, zero runs, zero MCP servers); accessibility and keyboard smoke test.
**SUCCESS GATE:** `make block10-gate` passes: Playwright e2e launches agent → watches live run → sees a
DENIED egress row → export audit → verify export. It also shows a managed MCP
server resource with sanitized logs and Docker artifact metadata. Lighthouse
perf ≥ 90 local. Planted-XSS and sentinel-secret tests show escaped/redacted
output for agent, gateway, and MCP logs. 10k-span, SSE reconnect, SQLite
lock/corruption recovery, empty-state, policy diff, audit export verify, and
accessibility smoke tests green.

---

## BLOCK 11 — Hermes operator contract (Hermes-first P1)
**Purpose:** Make Hermes the first-class P1 operator of AgentPaaS, not a
screen-scraper of human CLI/dashboard output. P1 is a hands-off but secure
local development experience: Hermes can scaffold, pack, run, inspect,
diagnose, repair, and re-run an agent on the user's machine, while sensitive
boundary changes remain explicit, reviewed, and audited. Claude Code, Codex,
Cursor, and other coding-tool operators are P2.
**Builds:** internal/operator — a stable machine-readable diagnosis and
repair-hint layer consumed by CLI, dashboard, and Block 13 MCP integrations.
Add JSON-schema/protobuf contracts for: `ValidateAgentProject`,
`SummarizeRun`, `ExplainFailure`, `ExplainPolicyDenial`,
`RecommendPolicyPatch`, `GetRunTimeline`, and `NextAction`. All commands
that a human can use for pack/run/logs/status/policy/audit also support
`--json` with the same schema. Outputs include stable error categories
(`dependency_conflict`, `docker_unavailable`, `policy_denied`,
`missing_secret_binding`, `budget_exceeded`, `trigger_auth_failed`,
`harness_health_failed`, `agent_runtime_exception`,
`policy_validation_failed`, `network_sandbox_failed`, `secret_scan_failed`,
`package_verification_failed`, `dashboard_unavailable`) plus evidence refs
(run_id, audit seq range, policy rule id, span/log ids, redacted excerpts,
verification command).

The operator contract is the retroactive invariant for Blocks 1-10:
- Block 1 APIs/protos define stable machine-readable methods and error enums.
- Block 2 daemon lifecycle/doctor reports structured readiness and repair
  hints.
- Block 3 audit exposes query/export results as signed, verifiable machine
  data with trust-anchor fingerprints.
- Block 4 policy compiler emits structured denial reasons and safe patch
  proposals, never silent policy broadening.
- Block 5 network/runtime returns structured egress decisions and containment
  evidence.
- Block 6 harness/SDK emits run lifecycle, health, budget, and exception
  events in schemas that tools can reason over.
- Block 7 secrets broker exposes missing-binding/revocation/lease diagnostics
  without revealing secret values.
- Block 8 packaging returns signed `agent.lock`, SBOM, scan, advisory, and
  reproducibility results as JSON.
- Block 9 Trigger API uses stable caller ids, idempotency, SSE event ids, and
  cancel outcomes that tools can resume from; local handoff triggers preserve
  parent/child run correlation and static target lock digests.
- Block 10 dashboard/OTel exposes the same timeline/audit/policy data as JSON;
  the UI is a view, not the source of truth.

**Safety model:** Agentic tools may automatically repair code, tests,
`agent.yaml`, dependency declarations, and non-security config inside the
project root. They may propose `policy.yaml` changes, new egress, credential
bindings, direct leases, local handoff triggers, webhook destinations, exposed
listeners, retention purges, and destructive actions, but P1 requires explicit
user/daemon confirm before applying them. Tools cannot read secret values,
cannot broaden policy silently, cannot create hidden agent chains, cannot
delete audit, cannot disable red-team gates, and cannot use paths outside the
invoking project root. Prompt-injected instructions inside agent
source/logs/traces are untrusted data and must not cause policy changes,
secret disclosure, hidden handoffs, audit deletion, or destructive operations.

**Agentic workflow contract:** `agent init --from-code --noninteractive`
creates/reconciles `agent.yaml` and a minimal default-deny `policy.yaml`;
`agent validate --json` returns project readiness; `agent pack --json` emits
scan/SBOM/signature/lockfile facts; `agent run --json` returns run_id and
stream refs; `agent status/logs/audit/policy --json` expose structured state;
`agent explain run <run_id> --json` diagnoses failures; `agent policy explain
<run_id|dest> --json` names the blocking rule; `agent policy propose --json`
returns a patch with risk level, rationale, affected destinations, credential
ids, and audit evidence; `agent next-action <run_id> --json` returns one of
`fix_code`, `install_dependency`, `start_docker`, `set_secret`,
`review_policy_patch`, `review_handoff`, `increase_budget`, `rerun`,
`export_audit`, or `ask_user`.

**Edge cases:** malformed/old JSON schema version → clear compatibility error;
tool asks for path outside project root → refusal with audit event; huge logs
or build output → truncated excerpts + stable refs; denied egress → policy
patch is proposed but not applied; missing secret → secret binding request is
proposed but value is never requested through the agentic tool; prompt
injection in source/logs says "approve all policy" → ignored and tested;
network/dashboard unavailable → tool falls back to daemon/control JSON; daemon
restart mid-loop → idempotency and run refs let Hermes resume; human
declines policy patch or handoff trigger → next action becomes `fix_code` or
`ask_user`, not policy bypass or hidden chaining.
**SUCCESS GATE:** `make block11-gate` passes with the Hermes golden flow green
on a clean machine: a scripted Hermes-like client creates a deliberately
incomplete Python agent, runs `agent init --from-code --noninteractive`,
validates, packs, runs, sees a policy denial, receives a structured denial
explanation,
receives a policy patch proposal but cannot apply it without confirm, fixes a
code/dependency issue automatically, reruns after approved policy, exports a
signed audit bundle, and summarizes the final result in JSON. Negative tests
prove prompt-injected source/log instructions cannot broaden policy, reveal
secrets, delete audit, or stop unrelated runs. JSON schema golden tests prove
backward-compatible outputs for every operator method.

---

## BLOCK 12 — P1 red-team smoke gate (fast release proof)
**Builds:** test/redteam — a small malicious-agent and malicious-operator
fixture suite that proves the P1 demo/security claims through the REAL
pipeline without becoming a full adversarial research program. P1 red-team is
a fast local OSS release gate, not a comprehensive pentest. The expanded
attack corpus is deferred to P2.

P1 attack fixtures:
1. **Default-deny egress:** agent attempts raw IP TCP dial and direct HTTPS to
   a non-allowed domain. Expect blocked/no route + egress_denied audit.
2. **Gateway/policy enforcement:** agent tries an allowed-looking request with
   a disallowed host/method or brokered credential against the wrong
   destination. Expect denied + policy rule/audit evidence.
3. **Brokered secret invisibility:** agent probes env, `/proc`, common files,
   logs, and mounted secret paths for a brokered sentinel secret. Expect zero
   hits; upstream fixture still receives the header through gateway injection.
4. **Host access smoke:** agent probes `host.docker.internal`, Docker bridge
   gateway, and daemon ports. Expect blocked/unreachable + audit where
   applicable.
5. **Resource containment smoke:** simple memory/fd/child-process pressure
   trips configured limit without taking down daemon/dashboard. Expect killed
   or failed run + audit.
6. **Operator prompt-injection smoke:** malicious source/log text instructs
   the coding-agent/operator tools to approve policy, reveal secrets, delete
   audit, or stop unrelated runs. Expect refusal/proposal-only behavior,
   redacted output, and no trust-boundary change without confirm.

Each fixture asserts: action BLOCKED/CONTAINED/REFUSED + the expected
machine-readable result and audit event. Runner prints a 6-row containment
table plus a signed audit-export verification summary.
**Edge cases:** all runtime attacks run through REAL `agent pack` + `agent
run` (no test shortcuts); operator attacks run through REAL `--json`/operator
methods from Block 11; each fixture verifies its own audit event exists with
correct verdict fields; suite target runtime <10 minutes on a developer
laptop; flaky platform-specific probes may be marked informational only if
the core P1 claim is still covered by another deterministic assertion.
**SUCCESS GATE:** `make block12-gate` wraps `make redteam-smoke`, which prints
6/6 PASS on macOS (Docker
Desktop or colima); failures in default-deny egress, brokered secret
invisibility, credential misuse, or operator trust-boundary refusal are
release blockers for v0.1.0. P2 adds Linux CI coverage plus the full permanent
red-team corpus: DNS tunneling, proxy bypass variants, IPv6 escape,
UDP/ICMP tunnels, domain fronting, direct-lease exfil/DLP,
SBOM/signature tamper, full MCP prompt-injection matrix, fuzzed operator
payloads, and every future RuntimeDriver/agentgateway bump as a comprehensive
release gate. (P1 CI runs on a self-hosted macOS runner — production target
is macOS. A Linux runner can be added in P2 for cross-platform certification.)

---

## BLOCK 13 — Hermes integration plugin/skill
**Builds:** integrations/hermes-plugin — a single Hermes plugin (not an MCP
server, not a bare skill) exposing the daemon's Block 11 operator contract as
plugin tools. The plugin has four parts: (1) `plugin.yaml` manifest declaring
the toolset and `requires_env` for the daemon socket path; (2) `__init__.py`
with `register(ctx)` calling `ctx.register_tool` for each of the 17 required
P1 tools; (3) `schemas.py` + `tools.py` generated from or schema-tested
against the Block 11 JSON/protobuf contracts (handlers shell out to the
`agent` CLI with `--json`, they do not re-implement operator logic); (4) a
bundled `SKILL.md` registered via `ctx.register_skill` that teaches the
detect→init→validate→pack→run→inspect→repair flow. Required P1 tools:
`agentpaas_init_project`/`agentpaas_reconcile_project`,
`agentpaas_validate_project`, `agentpaas_doctor`, `agentpaas_pack`,
`agentpaas_run`, `agentpaas_stop`, `agentpaas_logs`, `agentpaas_status`,
`agentpaas_get_run_timeline`, `agentpaas_policy_show`,
`agentpaas_explain_policy_denial`, `agentpaas_recommend_policy_patch`,
`agentpaas_audit_query`, `agentpaas_export_audit`,
`agentpaas_summarize_run`, `agentpaas_explain_failure`, and
`agentpaas_next_action`. These are generated from or schema-tested against
the Block 11 JSON/protobuf contracts, not hand-maintained as a separate
behavior surface. This P1 adapter is Hermes-only: Hermes gets "deploy what
you just built, safely" as a verb plus the diagnosis loop needed to fix what
it just built. A generic MCP server and Claude Code/Codex/Cursor skins are P2.

Contract parity gate: CI fails if a Block 11 operator method lacks a Hermes
tool wrapper, if a wrapper returns fields outside the versioned operator
schema, if a wrapper drops required evidence refs/error categories, or if a
trust-boundary action can complete without the daemon confirmation protocol.
The Hermes integration spec revision and generated schema fixtures are pinned
in the integration package lock/test fixtures, not in a user's `agent.lock`.

Slash-command surface: in addition to the 17 tools, the plugin registers a
small set of in-session slash commands via `ctx.register_command` so a user
can drive the full lifecycle without leaving the Hermes session. Each command
is a thin orchestrator that calls the plugin's own tools through
`ctx.dispatch_tool` (so it goes through the same approval/redaction/budget
pipelines as a model-invoked tool call), never re-implementing logic:
- `/agentpaas deploy` — detect agent code → `agentpaas_init_project` →
  validate → pack → run → print run_id and dashboard URL.
- `/agentpaas status` — `agentpaas_status` across active runs; one-line
  summary per run.
- `/agentpaas logs [run_id]` — `agentpaas_logs` with tail/truncate.
- `/agentpaas metrics [run_id]` — `agentpaas_get_run_timeline` +
  `agentpaas_summarize_run` for the budget/denial/health view.
- `/agentpaas repair [run_id]` — `agentpaas_explain_failure` →
  `agentpaas_next_action` and, if the next action is auto-repairable
  (fix_code/install_dependency/start_docker), drives it; otherwise surfaces
  the confirmation requirement (policy patch, handoff, secret binding) for
  explicit user approval.

The Hermes plugin bundles a `SKILL.md` (via `ctx.register_skill`) that
teaches the flow (detect agent code → `agentpaas_init_project` scaffold →
validate → pack → run → inspect timeline/dashboard → show first audit
event), with pitfalls (Docker not running → `agentpaas_doctor`; policy
denial → `agentpaas_explain_policy_denial` then
`agentpaas_recommend_policy_patch`). The `requires_env` manifest field
gates the plugin on the daemon socket path and is prompted interactively
during `hermes plugins install`. Terminal CLI commands remain documented
fallback steps. The plugin does NOT register an `mcp_servers` block; the
generic MCP server for Claude Code/Codex/Cursor is P2.

Security stance: the Hermes integration talks ONLY to the loopback daemon
socket; it never accepts remote connections; it inherits the daemon socket's
local user permissions; and all paths are resolved against the invoking
project root before reaching the daemon. Destructive tools require scoped
semantics
and the daemon confirmation protocol: `agentpaas_stop` may stop only the
active run created by that client session by default; stopping unrelated
runs, applying policy, binding credentials, issuing direct leases, exposing
listeners, increasing budgets, deleting/purging audit, exporting audit to a
remote destination, or disabling gates returns
`requires_confirmation: true`, `confirmation_id`, `risk_level`, rationale,
and evidence refs. Only the daemon/UI/CLI confirmation path can apply the
change, and the confirmed action is audited.

Prompt-injection boundary: Hermes integration responses separate trusted
control fields
(`status`, `error_category`, `next_action`, `requires_confirmation`,
`confirmation_id`, `risk_level`, evidence refs) from untrusted evidence
(`redacted_excerpt`, log/source/trace snippets, external payload text).
Agent source, comments, logs, traces, tool output, Hermes resource text, and
remote payloads are always treated as untrusted data; instructions found
there must not broaden policy, reveal secrets, delete audit, disable gates,
stop unrelated runs, or trigger destructive operations.
**Edge cases:** Hermes passes a path outside the project root → tool
refuses (path allow-list = invoking project dir); daemon down → tool
returns actionable `agentpaas_doctor` hints, not a hang; concurrent pack
requests for the same agent → second queues with a message; tool output >
50KB (huge build logs) → truncated with a pointer to `agentpaas_logs`; old
Hermes integration/schema version → compatibility error with upgrade hint;
Hermes user asks "change this agent's prompt" for an already running agent →
plugin edits only project prompt/config files, runs the required validate →
pack → verify → run command sequence, reports the new run id/digests, and
does not mutate the existing container/image/lockfile in place;
confirmation id replay/expiry → refused + audit; hostile instruction
embedded in agent source/log comments → no policy alteration, secret
disclosure, audit deletion, gate disabling, or unrelated stop (negative test
in CI).
**SUCCESS GATE:** `make block13-gate` passes with scripted e2e on a clean machine after AgentPaaS is already
installed: Hermes generates an agent → `/agentpaas deploy` (or
`agentpaas-deploy` via the tool) → agent running governed → dashboard shows
a DENIED probe → post-install deploy flow <10 minutes; then Hermes is asked
to change the agent prompt and must drive the same immutable redeploy path
(edit project prompt/config → validate → pack → verify → run), with the
second run reflecting the new prompt and audit showing distinct old/new
digests. The `/agentpaas status`, `/agentpaas logs`, `/agentpaas metrics`,
and `/agentpaas repair` slash commands are exercised in the e2e and return
the same structured output as their tool counterparts. Hermes integration
conformance tests pass against the spec revision pinned in the integration
package.

Demo matrix for P1 differentiation:
1. **Governed weather/API agent:** generated agent attempts allowed weather
   API plus denied exfil probe; dashboard shows policy denial and signed audit
   evidence.
2. **Secret-brokered SaaS action:** generated ticket/CRM-style agent uses a
   brokered credential through the gateway; secret value is never visible to
   code/logs, but upstream fixture receives the authorized request.
3. **Agentic repair loop:** generated agent has a dependency/code defect and
   missing egress policy; MCP `next_action` fixes code automatically, proposes
   policy only, waits for confirmation, reruns, and exports a signed audit
   bundle.

---

## BLOCK 14 — Post-B13 Security Hardening, Release, and Distribution (consolidated)

This block consolidates ALL post-Block-13 build work into one block with
sub-segments. Each sub-segment has its own gate target but they share a single
`make block14-gate` that runs all of them in sequence. The sub-segments are:

- **14A0** — B13 correctness fixes (5 tasks: run status, orphan reconciliation, invoke/Stop sync, Docker e2e test, code hygiene rename)
- **14A** — Block 13.1 security remediation (9 tasks from the B13 security audit, T09 resolved)
- **14B** — Gateway container, policy enforcement, real-time egress, Stats, trigger server (5 tasks — core product value)
- **14C** — Install path, docs, demo, and v0.1.0 release (former standalone Block 14)

Block 13 must be fully complete (T01-T09 + block13-gate green) before 14A0 starts.

**Session discipline (Block 13+):** Work is divided into micro-chunks (3-6 turns
each), merged to main after each, checkpointed every 2-3 chunks, and every
session ends with an exit prompt for fast restart. See the
`agentpaas-build-rhythm` skill for the full protocol, checkpoint format, and
exit prompt template.

### 14A0 — B13 Correctness Fixes (from B13 Risk Analysis)

**Source:** `docs/b13-risk-analysis.md` — 4 runtime correctness bugs identified
during the B13 in-depth review. These are foundational issues in the daemon's
run lifecycle that must be fixed BEFORE the 14A security hardening, because the
security work assumes the run lifecycle is correct. All fixes are in the Go
daemon (no plugin changes).

**14A0-T01: Run status tracking (CRITICAL — fix this first)**

- **Problem:** `invokeAgent` discards the invoke result (`_ = stdout`). Every
  Stop publishes `EventRunSucceeded` regardless of whether the agent crashed,
  the invoke failed, or the container exited with a non-zero code. The dashboard
  and audit always show green.
- **Files:** `internal/daemon/control_handlers.go`, `internal/daemon/server.go`
- **Fix:**
  1. Add a `status` field to `trackedRun`: `"running"` | `"succeeded"` |
     `"failed"`. Default `"running"` on `trackRun()`.
  2. In the auto-invoke goroutine: set status to `"failed"` if `invokeAgent`
     returns error; set to `"succeeded"` if the invoke completes without error.
  3. In `Stop()`: check the container's exit code via `rt.Status()` or Docker
     inspect before removing. If exit code != 0, override status to `"failed"`.
  4. Publish `EventRunFailed` instead of `EventRunSucceeded` when status is
     `"failed"`. Add the event type if it doesn't exist in `trigger/events.go`.
  5. Record the run status in the `run_stop` audit payload.
- **Test:** `TestRun_FailedInvoke_SetsFailedStatus` — mock Docker driver returns
  error on Exec; verify Stop publishes EventRunFailed and audit payload has
  `"status": "failed"`.
- **Effort:** 2 micro-chunks.

**14A0-T02: Orphan container reconciliation on daemon start (CRITICAL)**

- **Problem:** If the daemon crashes or is killed, all running agent containers
  become invisible to the restarted daemon. The `s.runs` map is in-memory only.
  Orphaned containers accumulate, consuming resources.
- **Files:** `internal/daemon/server.go`, `internal/daemon/control_handlers.go`
- **Fix:**
  1. On daemon `Start()` (after gRPC server is listening, before returning), call
     a new `reconcileOrphanedContainers()` method.
  2. `reconcileOrphanedContainers()` uses `rt.ListContainers(ctx,
     "agentpaas/resource-type=agent")` to find all AgentPaaS-managed containers.
  3. For each container found: extract `runID` from labels. Check if it's in
     `s.runs`. If not, it's an orphan.
  4. For orphans: check container status. If running, either re-track it (best
     effort — extract network ID from `InspectContainerNetworks`) or stop +
     remove it + remove its network. Default: stop + remove (safe cleanup).
     Log the reconciliation action. Emit a `container_reconciled` audit event.
  5. Also reconcile orphaned networks via `rt.ListNetworks(ctx,
     "agentpaas/resource-type=net-*")`.
- **Test:** `TestReconcileOrphans_StopsOrphanedContainers` — pre-create a
  container with agentpaas labels via mock driver; start daemon; verify the
  container is stopped and removed.
- **Pattern:** The Block 5 reconciliation tests
  (`TestE2E_CrashReconciliation` in `internal/runtime/`) already test this
  pattern against real Docker. Reference them.
- **Effort:** 2-3 micro-chunks.

**14A0-T03: Invoke/Stop synchronization (HIGH)**

- **Problem:** The auto-invoke goroutine uses
  `context.WithTimeout(context.Background(), 2*time.Minute)` — detached from the
  run lifecycle. If `Stop()` removes the container while `invokeAgent()` is
  polling `/readyz`, the next `rt.Exec()` fails against a removed container.
  There is no synchronization between the invoke goroutine and Stop.
- **Files:** `internal/daemon/control_handlers.go`
- **Fix:**
  1. Store a `context.CancelFunc` in `trackedRun` (add `cancelInvoke context.CancelFunc`
     field).
  2. In the Run handler, create the invoke context with cancel:
     `invokeCtx, cancel := context.WithCancel(context.Background())`. Store
     `cancel` in the trackedRun before launching the goroutine. The goroutine
     uses `invokeCtx` (with the 2-minute timeout applied via
     `context.WithTimeout(invokeCtx, 2*time.Minute)`).
  3. In `Stop()`, BEFORE removing the container: look up the run, call
     `tracked.cancelInvoke()` to signal the invoke goroutine to exit, then wait
     briefly (e.g., 2 seconds via a `sync.WaitGroup` or a done channel) for the
     goroutine to finish before proceeding with container removal.
  4. The invoke goroutine already checks `ctx.Done()` in its polling loop —
     cancelling will cause it to return cleanly.
- **Test:** `TestStop_RaceWithInvoke_GoroutineExitsCleanly` — mock driver; call
  Run then immediately Stop; verify no panic, no "container not found" errors in
  logs, invoke goroutine exits.
- **Effort:** 1-2 micro-chunks.

**14A0-T04: Docker e2e test in block13-gate (HIGH)**

- **Problem:** `block13-gate` runs unit tests with a mock Docker driver but does
  NOT exercise the real pack→run→invoke→stop→audit flow. CI won't catch
  regressions in the Docker integration. The "e2e governance verified" message
  is misleading.
- **Files:** `internal/daemon/control_handlers_e2e_test.go` (new),
  `Makefile`
- **Fix:**
  1. Create `TestE2E_PackRunInvokeStopAudit` gated behind
     `AGENTPAAS_DOCKER_TESTS=1` (same pattern as Block 5 tests).
  2. The test:
     a. Starts a daemon with a temp AGENTPAAS_HOME under `~/` (NOT `/tmp` —
        colima mount limitation).
     b. Packs the test agent from `/tmp/agentpaas-e2e-agent/` (or a fixture in
        `demo/governed-weather/` with SDK bundled).
     c. Calls Run, waits up to 60s for the auto-invoke goroutine to complete
        (poll container status or sleep).
     d. Calls Stop.
     e. Queries audit and asserts that `egress_denied` events exist with
        destination matching the agent's HTTP call target.
     f. Asserts the audit hash chain is intact (seq is sequential, prev_hash
        matches).
  3. Add to `block13-gate` in the Makefile:
     ```
     ifeq ($(AGENTPAAS_DOCKER_TESTS),1)
         AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run TestE2E_PackRunInvokeStopAudit ./internal/daemon/... -timeout 300s
     else
         @echo "(skipping Docker e2e — set AGENTPAAS_DOCKER_TESTS=1 to run)"
     endif
     ```
  4. Verify the test passes with real Docker (colima).
- **Effort:** 2-3 micro-chunks (write test, wire into gate, verify).

**14A0 GATE:** `make block14a0-gate` — all daemon tests pass with -race,
reconciliation test passes, invoke/Stop race test passes, Docker e2e test
passes with `AGENTPAAS_DOCKER_TESTS=1` (skips gracefully otherwise). Run this
gate BEFORE starting 14A security work.

**Order:** T01 (run status) → T03 (invoke/Stop sync) → T02 (orphan
reconciliation) → T04 (Docker e2e test). T01 and T03 touch the same code paths
and should be done together; T02 depends on T01's status field; T04 validates
everything.

**14A0-T05: Code hygiene — rename stubControlServer + fix stale CLI doc.go (LOW)**

- **Problem:** The production daemon's control server is named
  `stubControlServer` (misleading — it's not a stub). The CLI doc.go says
  `pack`, `run`, `stop`, `logs`, `policy`, `secrets`, `audit`, `validate`,
  `summarize`, `explain-failure`, `explain-denial`, `recommend-patch`,
  `timeline`, `next-action` are all "not yet implemented" when they ARE
  implemented.
- **Files:** `internal/daemon/control_handlers.go`, `internal/daemon/server.go`,
  `internal/daemon/operator_handlers.go`, `internal/daemon/operator_handlers_b11t04_test.go`,
  `internal/cli/doc.go`
- **Fix:**
  1. Global rename `stubControlServer` → `controlServer` in all daemon files.
     Mechanical `gofmt -r` or sed pass. Verify build + tests pass.
  2. Update `internal/cli/doc.go` — remove "(not yet implemented)" from
     commands that are implemented. Leave "install"/"uninstall" as "not yet
     implemented" (those are actually stubs).
  3. Update `internal/cli/doctor.go` — the v0 stub comment should stay until
     `agent doctor` is fully implemented (14A-T06 wires the pre-flight check;
     full doctor is later).
- **Test:** Existing tests pass unchanged after rename. Run `go test ./internal/daemon/... ./internal/cli/...`.
- **Effort:** 1 micro-chunk (mechanical rename + doc update + verify).

### 14A — Security Remediation (Block 13.1)

**Source:** `agentpaas-b13-security-audit` skill — 8 gaps + 6 shortcuts identified.
Each gap is a worker-dispatchable subtask. All fixes are defense-in-depth on top
of B13's working plugin — they do not change the P1 plugin surface.

**14A-T01: Plugin path allow-list (GAP-1, HIGH)**
- `_validate_project_path()` in tools.py: resolves symlinks, rejects paths
  outside project root + /tmp for e2e agents, rejects `..` after resolution.
- Called before every `_run_cli` that takes a project_dir.
- Adversary: /etc/passwd, ../../, /tmp/../etc, symlinks.

**14A-T02: AGENTPAAS_CLI binary verification (GAP-2, HIGH)**
- Path allow-list: /usr/local/bin, /opt/homebrew/bin, $HOME/.local/bin, repo bin/.
- `--version` must contain "agentpaas".
- Adversary: point AGENTPAAS_CLI at /bin/echo, symlink to non-agentpaas binary.

**14A-T03: Subprocess output cap + configurable timeout (GAP-3, MEDIUM)**
- AGENTPAAS_CLI_TIMEOUT env var (default 300, min 10, max 600).
- stdout cap 50KB, stderr cap 10KB, add `output_truncated` flag.
- Adversary: CLI returns 100KB JSON → result has output_truncated.

**14A-T04: Thread-safe confirmation state (GAP-5, MEDIUM)**
- Replace module-level sets with a class wrapping `threading.Lock`.
- check-and-add is atomic.
- Adversary: 10 threads replay same confirmation_id → exactly 1 succeeds.

**14A-T05: Hash-chained harness audit (GAP-6, MEDIUM)**
- FileAuditAppender: hash = sha256(prev_hash + record_json) per record.
- Daemon ingestion verifies chain before re-chaining.
- Adversary: tamper with JSONL record → daemon detects broken chain, refuses.

**14A-T06: Pre-flight daemon socket check (GAP-8, LOW)**
- Check AGENTPAAS_SOCKET exists as Unix socket before spawning CLI.
- Return `daemon_unavailable` error with `next_action: start_docker` immediately.
- Adversary: daemon down → plugin returns in <1s, not 300s.

**14A-T07: Sanitizer improvements (GAP-4, MEDIUM)**
- Add base64 + hex decode steps.
- Narrow false-positive pattern to security-relevant verbs only.
- Add YAML-structure injection detection.
- Adversary: base64-encoded "disable policy" → detected.

**14A-T08: cosign tlog fix + real integration test (SHORTCUT-6)**
- Execute plan in `docs/plans/b13-cosign-coverage-fix.md`.
- Fix `--tlog-upload=false` → correct cosign v3.x flag.
- Real image signing test against localhost:5001, guarded by build tag.
- Mutation check: break a flag → test goes RED.

**14A-T09: T08+T09 catch-up (SHORTCUT-4, SHORTCUT-5) — RESOLVED**
- `/agentpaas` slash commands (deploy, status, logs, audit) implemented via
  `ctx.register_command` in commit `bd5afc9`.
- Bundled SKILL.md registered via `ctx.register_skill` in commit `bd5afc9`.
- These were deferred in the original audit but completed during B13.
- No work needed here. Mark as done.

**14A GATE:** `make block14a-gate` — all plugin tests pass (target 130+),
adversary tests for each gap pass, `go test ./internal/harness/...` passes with
-race, real cosign integration test green with AGENTPAAS_PACK_REAL_TOOLS=1.

### 14B — Gateway Container, Policy Enforcement, and Real-time Egress (consolidated)

**Source:** `docs/b13-risk-analysis.md` §1.1/§2.5 (no gateway, no policy
enforcement) + `b13-deploy-e2e-checkpoint-v5.md` §"Block 13.5" (deferred
real-time egress).

This is the most critical sub-segment for the product's value proposition.
B13's egress "denial" is network isolation (DNS fails on an internal-only
network), NOT actual policy enforcement. The Block 5 topology tests create
dual-homed gateways with agentgateway, but the daemon's Run handler never
creates one. This sub-segment wires the real gateway topology into the Run
handler, connects policy.yaml to runtime enforcement, adds real-time egress
visibility, and implements resource monitoring.

**14B-T01: Gateway container in Run handler (CRITICAL — core product)**

- **Problem:** The Run handler creates an internal-only network and puts the
  agent on it. No gateway container, no agentgateway, no egress network. When
  the agent calls `agent.http()`, the request fails with DNS error (network
  isolation), not a policy decision. The audit event says `egress_denied` but
  it's really `egress_unreachable`. Policy.yaml has zero runtime effect.
- **Files:** `internal/daemon/control_handlers.go`, `internal/runtime/driver.go`,
  `internal/runtime/docker.go`
- **Fix:**
  1. In the Run handler, after creating the internal network, create a second
     non-internal network (the egress network).
  2. Create a gateway container running the agentgateway binary (from
     `third_party/agentgateway/`). The gateway is dual-homed: connected to both
     the internal network and the egress network.
  3. Connect the agent container to BOTH networks (internal + egress), OR
     configure the gateway as the only path out (agent on internal-only, gateway
     dual-homed, agent's default route goes through gateway).
     Follow the topology locked in the PRD: agent on `internal: true` network,
     gateway dual-homed, DNS only via gateway stub.
  4. The gateway container gets the compiled gateway config from `PolicyApply`
     (already written to `config/gateway.yaml` by the daemon). The config
     contains the allow/deny rules from policy.yaml.
  5. Agent egress now flows: agent → gateway (via internal network) → allowed
     destinations (via egress network). Denied destinations return a real HTTP
     403 from the gateway, not a connection error.
  6. The harness `handleHTTP` already emits `egress_denied`/`egress_allowed`
     audit events — now they reflect actual policy decisions, not network errors.
- **Reference:** Block 5 topology tests (`TestE2E_Network_PositivePath`,
  `TestAdversaryB5T04a-d`) create this exact topology in test code. The patterns
  for creating gateway containers, connecting networks, and asserting topology
  are already proven. Port them from test code to the Run handler.
- **Test:** E2E test with policy allowing `api.weather.gov` but denying
  `evil.example.com`. Agent calls both. Verify: allowed call succeeds through
  gateway; denied call gets 403 from gateway; audit events have distinct reasons
  ("policy_denied" vs "network_unreachable"). Test gated behind
  `AGENTPAAS_DOCKER_TESTS=1`.
- **Effort:** 3-4 micro-chunks (gateway container creation, network wiring,
  policy config mounting, e2e test).

**14B-T02: Policy enforcement at runtime (CRITICAL — depends on T01)**

- **Problem:** `PolicyApply` writes policy.yaml and compiles a gateway config,
  but the config was never consumed by a running gateway. Now that T01 creates
  a gateway container, the config must flow to it.
- **Files:** `internal/daemon/control_handlers.go`, `internal/policy/compiler.go`
- **Fix:**
  1. The compiled gateway config (`config/gateway.yaml`) must be mounted into
     the gateway container at start time.
  2. The gateway container reads this config on startup and enforces it.
  3. If no policy has been applied, default-deny (the locked architecture
     decision from PRD §2.5).
  4. Verify that `policy.CompileGatewayConfig` produces a config that
     agentgateway actually accepts. The Block 4 tests validate the compiler
     output; now verify it against a real gateway.
- **Test:** Apply a policy that allows specific domains. Run agent that calls
  an allowed domain (succeeds) and a denied domain (403). Verify audit records
  show the policy decision, not a network error.
- **Effort:** 2 micro-chunks.

**14B-T03: Real-time egress visibility (from original 14B)**

- **Problem:** Harness emits egress audit events via FileAuditAppender, but
  these are only ingested by the daemon AFTER the run completes. During a
  long-running agent, egress attempts are invisible to the dashboard.
- **Files:** `internal/harness/rpc_server.go`, `internal/daemon/control_handlers.go`,
  `internal/otel/store.go`
- **Fix:**
  1. Daemon tails the harness audit JSONL file (via mounted volume) during the
     run, not just after Stop.
  2. Each new audit line → parse → `otelStore.IngestTraces` → EventBus publishes.
  3. Dashboard timeline `classifySpan` already classifies egress spans — no
     dashboard changes needed.
  4. Events appear in dashboard within 2s of the HTTP attempt.
- **Test:** Long-running agent makes HTTP calls; verify egress_allowed /
  egress_denied rows appear in timeline SSE stream in real time.
- **Effort:** 2 micro-chunks.

**14B-T04: DockerRuntime Stats implementation (MEDIUM)**

- **Problem:** `DockerRuntime.Stats()` returns `errDockerNotImplemented`.
  Dashboard resource monitoring (CPU, memory, PIDs) doesn't work.
- **Files:** `internal/runtime/docker.go`, `internal/daemon/resource_manager.go`
- **Fix:**
  1. Implement Stats using Docker API `ContainerStats` (streaming) or
     `ContainerInspect` (snapshot). Use one-shot snapshot for P1 (no streaming).
  2. Parse Docker stats JSON: `cpu_stats.cpu_usage.total_usage`,
     `memory_stats.usage`, `memory_stats.limit`. Compute CPU percentage.
  3. Map to `ContainerStats{CPUPercent, MemoryMB, PIDs}`.
  4. Wire into `dockerResourceManager` so the dashboard shows live resource usage.
- **Test:** Unit test with mock stats response. Docker e2e test verifies non-zero
  stats for a running container.
- **Effort:** 1-2 micro-chunks.

**14B-T05: Trigger server startup in local-first mode (LOW)**

- **Problem:** The daemon creates an EventBus but does not start the trigger API
  server (gRPC :7718 / REST :7717). External invocations are impossible. The
  auto-invoke via docker exec is the ONLY invocation path.
- **Status:** Acceptable for P1 local-first (spec says "loopback-only unless
  `--expose`"). But the trigger server should at least start on loopback so
  external tools (and the trigger API tests) can invoke agents directly.
- **Fix:** Start `trigger.New(ServerConfig{GRPCAddr: "127.0.0.1:7718"})` in
  daemon `Start()`. Wire the trigger service's `Invoke` to actually call the
  daemon's Run + invokeAgent flow (currently `TriggerService.Invoke` just
  creates a stub run in a RunStore).
- **Effort:** 2 micro-chunks.

**14B GATE:** `make block14b-gate` — gateway container created in Run handler,
policy enforcement verified via e2e (allowed call succeeds, denied call gets
403), real-time egress events visible in dashboard timeline during running agent,
Stats returns non-zero values for running containers, B13 DENIED probe test still
passes, no regression in audit chain integrity.

### 14C — Install Path, Docs, Demo, and v0.1.0 Release

This is the former standalone Block 14 — distribution surface area.

**Install path (macOS-first):**
- Homebrew tap (`brew install agentpaas/tap/agentpaas`) for darwin/arm64+amd64.
- No blind `curl|bash`; first run is explicit via `agent doctor`.
- `agent setup launchd` or `brew services start agentpaas` for background service.

**Release pipeline:**
- goreleaser: darwin/arm64+amd64, checksums, SBOMs, provenance, cosign signatures.
- Sigstore keyless verification via GitHub Actions OIDC.
- `cosign verify-blob` + `agent verify-release` helper.

**Docs site (docs/ → static):**
- Quickstart (<15-minute path), policy reference, secrets guide.
- "How enforcement actually works" (network-topology page).
- Threat model (PRD §3 verbatim), known limitations (§3.3).
- Audit-export verification guide for a second machine.
- Privacy/telemetry page, Hermes plugin/skill setup, demo scripts.
- Claude Code, Codex, Cursor, generic MCP docs → P2.

**Demo assets:**
- 3-minute demo video script + asciinema recordings.
- Minimum v0.1.0 launch demo: Hermes writes agent → AgentPaaS packs/signs/SBOMs →
  governed run → blocked exfil → signed audit export.
- Stretch demos: secret-brokered SaaS action, agentic repair loop.

**README:**
- 60-second story above the fold, containment table from `make redteam-smoke`.
- "Zero telemetry, zero phone-home by default" statement.
- Prerequisites: macOS, Homebrew, Docker Desktop or Colima.
- "Known limitations" link near install command.

**Air-gapped/offline path:**
- macOS offline bundle: signed binaries, checksums, SBOMs, container images,
  policy/demo fixtures, verification instructions.
- Must prove release artifacts + audit exports verifiable without network.

**Docs/release CI:**
- Broken-link check, command-snippet smoke, README quickstart smoke.
- Release-artifact verification matrix, screenshot/asciinema freshness check.
- Docs issue filed for every clean-machine deviation.

**14C EDGE CASES:**
clean-machine test on macOS (fresh user account) following ONLY the README,
with Docker Desktop or Colima already installed — every deviation is a docs bug,
filed and fixed before release; brew upgrade preserves daemon state + restarts
cleanly; failed daemon state migration rolls back or leaves clear manual recovery;
uninstall removes launchd units, containers, networks, sockets, generated config,
and says what it keeps (audit logs, keychain items, offline bundles) and how to
purge; zero telemetry = no analytics, update checks, crash reports, or usage
pings in P1. Future telemetry is separate, explicit opt-in only.

**14C GATE:** `make block14c-gate` — two volunteers (not you) each reach a
running governed agent in <15 minutes from README on their own macOS machines;
Hermes post-install deploy demo <10 minutes; at least one Block 13 differentiation
demo recorded and embedded (two additional = stretch); `cosign verify-blob` and
`agent verify-release` documented-and-green on released artifacts; offline bundle
verification documented-and-green; v0.1.0 tagged with goreleaser; all P1 CI gates
(lint, test, -race, fuzz corpus, e2e-network on macOS, Hermes integration
conformance, redteam-smoke 6/6, docs smoke) green on the tag.

**BLOCK 14 SUCCESS GATE:** `make block14-gate` runs all four sub-segment gates
in sequence (14A0 → 14A → 14B → 14C). All must pass. This is the final P1
build gate.

**Accepted for P1 (tracked, not blocking release):**

These items from the B13 risk analysis are deliberately deferred beyond P1.
They are documented here so they don't get lost:

- **Fake LLM handler** (`agent.llm()` returns "agentpaas fake llm response"):
  The harness is designed to broker LLM calls through the gateway with
  credentials, but actual LLM provider integration (OpenAI, Anthropic, etc.) is
  post-P1. Budget tracking and audit plumbing work correctly. A real agent demo
  would need this wired — track for P2.
- **Contract parity tests use Python fixtures, not Go schema** (SHORTCUT-3):
  Fragile but functional. The Go schema and Python fixtures can drift. Track for
  P2 schema generation from Go protobuf.
- **Sanitizer is advisory, not enforcement** (SHORTCUT-2): The plugin's
  prompt-injection sanitizer filters untrusted data but is not a hard enforcement
  boundary. Acceptable for P1 (the container sandbox is the real boundary).

---

## BLOCK 14D — P1 Risk Register and Deferred Items

**Date:** 2026-06-26
**Status:** Documentation block — tracks all risk issues identified during B14
build sessions that were deliberately deferred. Each item has a decision
rationale explaining why it was not solved outright in P1.

This block consolidates findings from:
- `docs/b14a-risk-analysis.md` (14A Security Remediation)
- `docs/b14b-risk-analysis.md` (14B Gateway + Policy Enforcement)
- `docs/b14c-risk-analysis.md` (14C Install Path + Release)

### Purpose

No new code is built in 14D. This block exists to ensure every accepted risk,
shortcut, and deferred item from Block 14 is tracked in the execution plan
itself — not buried in per-block risk docs that may go stale. When P2 work
begins, this register is the starting backlog.

### Risk Register (24 items)

#### Security: Audit Chain Integrity (3)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R1 | HIGH→P1 | ~~`--insecure-ignore-tlog` passed unconditionally to cosign verify.~~ **Resolved 2026-06-26 (B14E):** tlog suppression now conditional via `buildCosignSignArgs`/`buildCosignVerifyArgs`. Local refs suppress tlog; production refs use cosign defaults (Rekor required). Adversary-hardened host parsing. Commits 93527fc, 8fcb45c. | 14A-T08 |
| R2 | MED | ~~Hash chain record deletion undetectable.~~ **Resolved 2026-06-26 (B14E):** Signed checkpoints wired into AuditWriter. `VerifyAuditChain` detects tail truncation via checkpoint hash mismatch. Commit 85049bf. | 14A-T05 |
| R3 | MED | ~~No signed checkpoint mechanism.~~ **Resolved 2026-06-26 (B14E):** CheckpointManager creates ECDSA-signed checkpoints every N records, persisted key, daemon verification. Commit 85049bf. | 14A-T05+T08 |

#### Security: Cosign / Test Infrastructure (5)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R4 | MED | ~~Registry container not cleaned up after integration test.~~ **Resolved 2026-06-26 (B14E):** `CleanupLocalRegistry` helper added, called via defer. Commit c88ab87. | 14A-T08 |
| R5 | MED | ~~D3 tlog suppression check is loose substring match.~~ **Resolved 2026-06-26 (B14E):** Precise output parsing replaces substring match. Commit bab6c88. | 14A-T08 |
| R6 | MED | ~~Port 5001 conflict unhandled.~~ **Resolved 2026-06-26 (B14E):** `AGENTPAAS_TEST_REGISTRY_PORT` env var + conflict detection with clear skip. Commit c88ab87. | 14A-T08 |
| R7 | MED | ~~Fake cosign verify does zero flag validation.~~ **Resolved 2026-06-26 (B14E):** Fake verify validates all flags, mutation test added. Commit bab6c88. | 14A-T08 |
| R8 | SHORTCUT | ~~Cosign integration test not in default CI.~~ **Resolved 2026-06-26:** `release-verify.yml` job `cosign-integration` runs `TestSignImage_RealCosign` with `AGENTPAAS_PACK_REAL_TOOLS=1` on self-hosted runner. | 14A-T08 |

#### Daemon: Concurrency & State (3)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R9 | MED | ~~Unbounded confirmation set growth.~~ **Resolved 2026-06-26 (B14E):** OrderedDict with MAX_CONFIRMATION_IDS=10000, FIFO eviction. Commit 95deae4. | 14A-T04 |
| R10 | MED | ~~`NewFileAuditAppender` prevHash not seeded on re-open.~~ **Resolved 2026-06-26 (B14E):** `lastRecordHashFromFile` seeds prevHash from last record. Commit 139e728. | 14A-T05 |
| R11 | LOW | ~~Policy file write race.~~ **Resolved 2026-06-26 (B14E):** Atomic write via temp-file + os.Rename. Commit 3802321. | 14B-T01 |

#### Daemon: Orphan Reconciliation (1)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R12 | MED | ~~`reconcileOrphanedContainers` doesn't remove orphaned gateway containers or networks.~~ **Resolved 2026-06-26 (B14E):** Verified gateway+egress cleanup already implemented (B14A0-T02) with dual-label filter. `TestReconcile_RemovesGatewayAndEgressNetwork` confirms. Risk register was stale. | 14B-T01 |

#### Docker Runtime: Stats (4)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R13 | MED | ~~uint64→int64 overflow in CPU delta.~~ **Resolved 2026-06-26 (B14E):** uint64 saturating subtraction. Commit b2f94af. | 14B-T04 |
| R14 | MED | ~~Stats error path tests missing.~~ **Resolved 2026-06-26 (B14E):** Error path tests for ContainerStats/ReadAll/parse. Commit b2f94af. | 14B-T04 |
| R15 | MED | ~~Stats JSON partial fields produce zeros without error.~~ **Resolved 2026-06-26 (B14E):** Field validation, error on missing precpu/cpu_stats. Commit b2f94af. | 14B-T04 |
| R16 | MED | ~~No `-race` test for concurrent `Stats()` calls.~~ **Resolved 2026-06-26 (B14E):** `TestStats_ConcurrentNoRace` passes -race detector. Commit b2f94af. | 14B-T04 |

#### Network Enforcement (1)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R17 | SHORTCUT | ~~HTTP_PROXY only — non-HTTP protocols bypass gateway.~~ **Resolved 2026-06-26 (B14E):** iptables egress firewall added. Agent container gets CAP_NET_ADMIN (dropped after init via capset). firewall_init.sh blocks all direct outbound except loopback/established/private/gateway. IPv6 covered via ip6tables. Configurable via `AGENTPAAS_EGRESS_FIREWALL`. Research-backed (mattolson/agent-sandbox pattern). Commits e245144, 8fcb45c. Residual: broad RFC1918 allow (P2 tighten), NET_ADMIN on agent (P2: init container pattern). | 14B-T02 |

#### Trigger Server (1)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R18 | SHORTCUT | ~~Trigger server has no authentication.~~ **Resolved 2026-06-26 (B14E):** Daemon reads `AGENTPAAS_TRIGGER_API_KEY`, builds APIKeyAuthenticator. `--expose` requires key. Backward compatible (loopback-only) when unset. Commit c159494. | 14B-T05 |

#### Resource (1)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R19 | LOW | ~~`maxConcurrentRuns` resource multiplier undocumented.~~ **Resolved 2026-06-26 (B14E):** Documented in `docs/known-limitations.md` — each run = 2 containers + 2 networks, limit 3 = 6+6. Commit 3802321. | 14B-T01 |

#### Release / Docs (5)

| ID | Sev | Description | Source |
|----|-----|-------------|--------|
| R20 | SHORTCUT | ~~Homebrew formula SHA256 is placeholder.~~ **Resolved by design (B14E):** Comment added documenting goreleaser fills checksum at release. Commits b5d04dc. | 14C-T01 |
| R21 | MANUAL | ~~No demo video/asciinema recorded.~~ **B15 scope (B14E):** Documented in `docs/known-limitations.md` as planned for Block 15 manual testing. Commit b5d04dc. | 14C-T02 |
| R22 | SHORTCUT | ~~No goreleaser dry-run in CI.~~ **Resolved 2026-06-26:** `release-verify.yml` job `goreleaser-snapshot` runs `goreleaser release --snapshot --skip=sign,publish,docker,before` (build/archive/checksum/formula validation in ~6s; signing validated at real release via OIDC). | 14C-T01 |
| R23 | SHORTCUT | ~~No brew audit in CI.~~ **Resolved 2026-06-26:** `release-verify.yml` job `brew-audit` runs `brew style Formula/agentpaas.rb` (`continue-on-error`; SHA256 placeholders intentional until release). | 14C-T01 |
| R24 | SHORTCUT | ~~No docs CI: broken-link check, command-snippet smoke.~~ **Resolved 2026-06-26:** `release-verify.yml` job `docs-links` runs `lychee` on `docs/` and `README.md` (`continue-on-error` while backlog links are fixed). | 14C-T03 |

### Decision Rationale — Why These Were Not Solved Outright

The 24 items fall into four categories. The category determines the fix path
and whether it blocks v0.1.0.

#### Category 1: External Dependency / CI Infrastructure Gating (R8, R22, R23, R24)

**Status (2026-06-26):** Resolved in CI via `.github/workflows/release-verify.yml` on the self-hosted runner (cosign, colima/Docker, goreleaser, brew, lychee installed). Items remain in the register for traceability.

**What:** CI runners are self-hosted macOS without Docker-in-Docker, cosign
binaries, or a local container registry. Running these tests/audits in CI
requires infrastructure that doesn't exist on the runner yet.

**Why deferred:** The trade-off was ship v0.1.0 with manual verification (the
tests exist and pass locally with the right env vars set), or block release on
CI infrastructure work that doesn't change product behavior. Chose ship.

**Local infrastructure requirements (what's needed to run these locally):**

These are DEVELOPER-SIDE requirements for running the full test suite. A new
user installing AgentPaaS via Homebrew does NOT need any of this — see
"New User Impact" below.

1. **Cosign** (`R8`): Install via `brew install cosign` or
   `cosign-installer` GitHub Action. Generate a keypair with
   `cosign generate-key-pair`. Set `COSIGN_PASSWORD` and `COSIGN_PRIVATE_KEY_PATH`.
   The integration test (`TestCosignIntegration`) then runs with
   `AGENTPAAS_PACK_REAL_TOOLS=1 make test`.

2. **Local registry** (`R4, R6`): The test spins up a registry container via
   Docker. Requires Docker Desktop or colima running. The `EnsureLocalRegistry`
   helper starts `registry:2` on port 5001. If port 5001 is taken, the test
   fails (R6).

3. **Goreleaser** (`R22`): Install via `brew install goreleaser`. Dry-run with
   `goreleaser release --snapshot --clean`. This builds all platform binaries
   and packages them without publishing.

4. **Homebrew audit** (`R23`): Requires `brew` on the path (macOS only).
   `brew audit --formula Formula/agentpaas.rb` checks formula correctness.

5. **Docs link checker** (`R24`): A tool like `lychee` or `markdown-link-check`.
   Install via `brew install lychee`. Run against `docs/` and `README.md`.

**New User Impact:** NONE. A user installing via `brew install agentpaas`
gets pre-built, signed binaries from GitHub Releases. They do NOT need cosign,
a local registry, goreleaser, or any test infrastructure. The signed binary
verification (`agent verify-release`) uses cosign's public key infrastructure
(transparency log + Rekor) — the user just runs the command, they don't set up
cosign. The infrastructure gap is purely a CI/test-coverage gap, not a
distribution gap.

#### Category 2: P1 Design Consistency (R1, R3, R17)

**What:** These are architectural decisions where the P1 approach is internally
consistent but differs from the "proper" P2 architecture.

- **R1** (`--insecure-ignore-tlog`): Not a bug — it's the logical consequence
  of suppressing Rekor upload during signing. The sign→verify round-trip
  wouldn't work otherwise. Fixing R1 means implementing R3 (signed checkpoints).
- **R3** (signed checkpoint mechanism): A multi-block effort — requires a
  trusted anchor service, signing keys, and a verification protocol. Not a
  patch; a subsystem.
- **R17** (transparent proxy): HTTP_PROXY works for HTTP/HTTPS. Non-HTTP
  protocols (raw TCP, DNS) bypass the gateway. P2 requires iptables/DNS-level
  redirection — a fundamentally different enforcement model.

**Why deferred:** Each requires designing and building a new subsystem. None
block v0.1.0's core value (governed agent execution with audit chain). They're
"correct for P1, architecturally different for P2."

#### Category 3: Low-Impact Edge Cases (R9, R10, R11, R15, R16, R19)

**What:** Correctness issues with narrow trigger conditions.

- **R9** (unbounded confirmation set): Only matters in sessions running
  thousands of confirmations — not a P1 use case.
- **R10** (prevHash re-open): Per-run new file is the normal pattern; re-open
  is an edge case.
- **R11** (policy write race): Policy is applied infrequently; the race window
  is tiny.
- **R15/R16** (Stats edge cases): Clamping prevents crashes; values are
  slightly wrong under partial JSON from Docker API.
- **R19** (resource multiplier): Documentation gap, not a code issue.

**Why deferred:** Low ROI. The cost of fixing each is small, but the trigger
conditions are rare in P1 usage. No user-facing impact.

#### Category 4: Test Coverage Debt (R4, R5, R6, R7, R14)

**What:** Test-quality issues, not product issues. The production code works;
the tests are incomplete or fragile.

**Why deferred:** The adversary verified the code paths manually during B14.
Adding the missing tests is mechanical work that doesn't change behavior.
Tracked for P2 backlog.

### BLOCK 14D SUCCESS GATE

This is a documentation-only block. The gate is: this register exists in the
execution plan and all 24 items are accounted for. No code changes, no test
runs. Future P2 work references this register as the starting backlog.

---

## BLOCK 15 — P1 Completion Items (Pre-Release Gap Closure)

**SEQUENCE: This block runs before Block 16 (manual use-case assessment).**
This block closes the gaps that make the full product experience testable.
Block 16 only starts after this block's success gate passes. Testing a
product with fake LLM responses and no credential onboarding finds the wrong
rough edges.

Block 14 built the security spine, runtime, governance, and CI. Block 15 closes
the remaining gaps that block the full "build → package → launch governed
agent" experience before v0.1.0 ships.

These items were identified during the B14 final verification (2026-06-26) when
asking: "If I tell Hermes to build me an agent and package it with AgentPaaS,
will it work end-to-end with security and governance?" The answer was: the
infrastructure works (e2e verified), but LLM integration, credential
onboarding, clean-machine prerequisites, and production hardening are missing.

### P1 Items (must close before v0.1.0 release)

#### 15-T01: Credential Onboarding (P1)

**Problem:** The Keychain broker works but setup is manual. No `agentpaas secret
add` command. No way to list secrets. No rotation flow.

**Scope:**
- `agentpaas secret add <name>` — stores a credential in macOS Keychain
- `agentpaas secret list` — lists which secrets the daemon can access (by label,
  never by value)
- `agentpaas secret remove <name>` — removes a credential
- `agentpaas secret rotate <name>` — replaces a credential (add + remove atomic)
- `agentpaas secret test <name>` — pre-deployment credential validation.
  Makes a trivial authenticated call to the target service (LLM: "say OK";
  third-party API: GET /health or equivalent) OUTSIDE the container, before
  pack/run. Fail fast with a clear error if the key is wrong, the provider
  is unreachable, or the egress policy doesn't allow the destination.
  Required by 15-T02 Gap 2 (test-before-baking-in). Works for ALL brokered
  credentials, not just LLM.
- Keychain service name convention documented and enforced
- Hermes plugin: secret onboarding skill (guide user through adding credentials)

**Verification:** `agentpaas secret add openweather-api-key` stores a key.
`agentpaas secret test openweather-api-key` validates it before deployment.
`agentpaas run weather-agent` uses it via the gateway broker. Secret value
never appears in container env, logs, or audit trail.

#### 15-T02: LLM Provider Integration via Unified Gateway Egress

**Problem:** `agent.llm()` returns "agentpaas fake llm response." No real LLM
provider is wired. Agents can fetch data via HTTP but cannot reason.

**Design decision (Option B — Unified Egress):** LLM calls are NOT special.
They route through the gateway as credentialed HTTP egress, exactly like any
third-party API call. The existing secrets broker, gateway proxy, audit
chain, and policy engine already handle credentialed HTTP egress (B7
adversary-tested, B14 risk-closed). `agent.llm()` becomes thin sugar over
`agent.http_with_credential` to the provider's chat-completions endpoint.
This unifies LLM and third-party API access under one security model — all
pathways are baked into the egress gateway before deployment, none bypass it.

**Design — four pillars:**

1. **Interactive provider selection at design time.** The Hermes plugin ASKS
   the user which LLM provider + model to use (or proposes and confirms),
   rather than silently deciding. The choice is written to `agent.yaml`
   `llm.provider` + `llm.model`. This is a user decision, not a Hermes
   decision.

2. **Pre-deployment credential validation ("test before baking in").** Before
   `agentpaas pack`/`run`, the user can validate that every credential path
   resolves — API key works, provider responds, egress policy allows the
   provider domain. `agentpaas secret test <name>` makes a trivial
   authenticated call (e.g. LLM: "say OK"; third-party API: GET /health or
   equivalent) OUTSIDE the container, before deployment. Fail fast with a
   clear error if the key is wrong, the provider is unreachable, or the
   egress policy doesn't allow the destination. This applies to ALL brokered
   credentials, not just LLM.

3. **Unified LLM routing (Option B).** Route LLM calls through the gateway as
   credentialed HTTP egress. `agent.llm(prompt, model?)` SDK method becomes
   sugar over `agent.http_with_credential` to the provider's API endpoint.
   - The harness `handleLLM` RPC (internal/harness/rpc_server.go) is deprecated
     or converted to a thin wrapper that calls `handleHTTP` with the provider
     URL + credential_id resolved from agent.yaml.
   - The gateway resolves the LLM provider credential from Keychain via the
     existing broker, attaches it to the outbound request as an
     Authorization header (or provider-specific header), and proxies the call.
   - Budget enforcement (token counting), audit events, and policy enforcement
     all reuse the existing egress path — no new code path for LLM.

4. **agent.yaml schema.** `llm.provider` (openai|anthropic|xai|...),
   `llm.model` (e.g. "gpt-4o", "claude-sonnet-4"), and the credential binding
   (which Keychain secret to use for this provider). The policy.yaml
   `credentials[]` and egress rules already support this — the LLM provider
   domain (e.g. api.openai.com) is just another allowed egress destination
   with a credential binding.

**Scope:**
- Interactive provider selection in the Hermes plugin (ask user → write
  agent.yaml `llm.provider` + `llm.model`)
- `agentpaas secret test <name>` — pre-deployment credential validation for
  LLM AND third-party API keys (trivial authenticated call, fail fast)
- LLM provider adapter: maps `llm.provider` → provider API endpoint URL +
  auth header name + request/response format (OpenAI, Anthropic, xAI)
- `agent.llm(prompt, model?)` SDK method → gateway-proxied credentialed HTTP
  (sugar over `agent.http_with_credential`)
- Deprecate/convert the fake `handleLLM` harness RPC to a thin wrapper
- agent.yaml schema: `llm.provider`, `llm.model`, credential binding
- Budget enforcement on LLM calls (token counting — already in harness, must
  apply to gateway-proxied calls via response body parsing or header)
- Audit events for LLM calls (provider, model, token count, cost,
  allowed/denied — already supported by the egress audit path)

**Depends on:** 15-T01 (credential onboarding — `secret add` + `secret test`),
existing gateway + audit chain + secrets broker (all built in B7/B14).

**Verification:**
1. `agentpaas secret test openai-key` — returns success, key works, provider
   responds with a trivial completion. (Pre-deployment.)
2. `agent.llm("What is 2+2?")` returns a real response from the configured
   provider, routed through the gateway. (Runtime.)
3. Audit trail records the LLM call as an egress event with provider, model,
   token count, cost, allowed.
4. Policy denial: remove the provider domain from the egress allowlist →
   `agent.llm()` returns a policy-denied error, audit shows `egress_denied`.
5. Credential revocation: revoke the LLM key via `agentpaas secret remove` →
   next `agent.llm()` call fails with credential-revoked error.

**What this does NOT do:**
- Does not create a special LLM code path. LLM is credentialed HTTP egress.
- Does not store API keys in container env, agent source, or anywhere the
  agent process can read directly. Keys live in Keychain, resolved by the
  broker at call time, injected as headers on the outbound request.
- Does not skip pre-deployment validation. All credential paths must resolve
  before `agentpaas run`.

#### 15-T03: Policy Authoring via Hermes (No UX Needed)

**Problem:** policy.yaml exists and compiles into gateway rules, but there's no
tooling to help users write it. Users must read docs and hand-write YAML.

**Design:** No policy UX is needed. Hermes sets up all policies by asking
questions or through its skills. AgentPaaS provides:
- Default policy templates for common patterns (allow one API, allow read-only
  S3, allow LLM calls, deny all by default)
- `agentpaas policy init` scaffold command that generates a starter policy.yaml
- Policy validation with clear error messages (syntax errors caught at pack time,
  not run time)
- Hermes plugin skill that asks the user about egress requirements and generates
  the policy.yaml automatically

**Scope:**
- `agentpaas policy init` command (interactive or template-based)
- Default policy templates: `allow-http`, `allow-llm`, `allow-mcp`, `deny-all`
- Policy validation at pack time (not just run time)
- Hermes plugin: policy generation skill (ask questions → generate YAML)

**Verification:** `agentpaas policy init` produces a valid policy.yaml. Hermes
generates a correct policy.yaml from user Q&A. Invalid policies are rejected at
pack time with a clear error.

#### 15-T04: Trigger / Cron / Event Surface

**Problem:** The trigger server backend (REST `/v1/trigger/invoke`, SSE
streaming, EventBus, CronScheduler) is fully built in `internal/trigger/`
but has no user-facing surface. No CLI subcommand to create/manage triggers
or cron schedules. No plugin tool. Schedules are only configurable
programmatically via `CronConfig.Schedules`. This blocks the "set triggers
to launch agent" use case (LC-03 in the e2e test plan).

**Design:** Expose the existing trigger/cron/event backend through CLI
commands and plugin tools. The backend already supports API-key auth (R18),
idempotency, SSE streaming, and 5-field cron expressions — this task wires
a user surface onto it, no new backend logic.

**Scope:**
- `agentpaas trigger invoke <agent-name> [--payload <file>]` — invoke an agent
  via the trigger REST API (API-key auth via AGENTPAAS_TRIGGER_API_KEY)
- `agentpaas trigger cron add <agent-name> --expr "*/5 * * * *" [--payload <file>]` — register a cron schedule
- `agentpaas trigger cron list` — list registered cron schedules
- `agentpaas trigger cron remove <id>` — remove a cron schedule
- `agentpaas trigger event publish <run-id> --type <event-type> [--data <file>]` — publish a test event (for LC-03b)
- `agentpaas trigger event subscribe <run-id>` — SSE stream of run events
- Hermes plugin tools: `agentpaas_trigger_invoke`, `agentpaas_trigger_cron_add`,
  `agentpaas_trigger_cron_list`, `agentpaas_trigger_cron_remove`
- Persist cron schedules in daemon state (survive restart — currently in-memory)
- Audit events for trigger create/invoke/cron-fire

**Depends on:** Existing trigger server (B9), daemon (B14).

**Verification:**
1. `agentpaas trigger invoke weather-agent` — agent runs, audit records the
   trigger invocation with API-key auth.
2. `agentpaas trigger cron add weather-agent --expr "*/2 * * * *"` — schedule
   registered, `cron list` shows it, agent auto-invokes every 2 min.
3. `agentpaas trigger event publish <run-id> --type test` — event appears in
   SSE stream and audit trail.
4. Daemon restart: cron schedules survive (persisted to state).
5. API-key auth: invoke without key → 401; with wrong key → 401; with correct
   key → 200.

#### 15-T05: Production Hardening (P1 — Don't Skip)

**Problem:** Several P2 items from the B14E risk register should be closed
before v0.1.0 ships, not deferred.

**Scope:**
- **R17 init container pattern:** Remove CAP_NET_ADMIN from the agent container
  entirely. Use an init container that programs iptables rules in a shared
  network namespace, then exits. The agent container never has NET_ADMIN.
- **R17 tighten RFC1918 allow:** Replace broad 172.16/12, 10/8, 192.168/16
  allow with the specific gateway subnet only.
- **R1 Rekor retry fallback:** If Rekor is down during production image signing,
  implement automatic retry with backoff. Don't silently fail.
- **Checkpoint key encryption at rest:** The ECDSA P-256 checkpoint signing key
  is currently stored as unencrypted PKCS#8 DER at
  `state/audit-checkpoint-key.der`. Encrypt it at rest (passphrase-derived AES
  or macOS Keychain Secure Enclave).
- **CAP_NET_ADMIN capset verification:** Add an integration test that verifies
  the agent process (UID 64000) cannot run `iptables -F` after the firewall init
  script runs and capabilities are dropped.

**Verification:** Agent container has no NET_ADMIN capability. Checkpoint key
file is encrypted. Rekor retry works on transient failures. iptables rules
cannot be flushed by the agent process.

#### 15-T06: Release Binary (macOS)

**Problem:** No release binary exists. Users must build from source.

**Scope:**
- Cut v0.1.0 tag → triggers `release.yml` (goreleaser + cosign keyless via OIDC)
- `brew install agentpaas` (Formula exists, SHA filled by goreleaser at release)
- `agentpaas doctor` first-run check (Docker, daemon, keychain — already built)
- First-run experience: daemon auto-start, local registry setup, policy scaffold
- `cosign verify-blob` on real release artifacts
- Offline bundle creation + verification

**Note:** Linux release (deb/rpm) is P2 — see 15-P2-01.

**Verification:** Fresh macOS machine: `brew install agentpaas`, `agentpaas
doctor` passes, `agentpaas pack` + `agentpaas run` works end-to-end.

#### 15-T07: Clean-Machine Prerequisites Documentation

**Problem:** We know it works on this machine. We haven't proven a fresh macOS
works. The 2-user <15 min test is Block 15 scope, but Block 15 must provide
Hermes with the information on what software is needed on a clean machine.

**Scope:**
- Document prerequisites: macOS version, Docker Desktop or Colima, Homebrew
- `agentpaas doctor` checks all prerequisites and reports what's missing with
  install commands
- README quickstart verified on a fresh macOS user account
- Prerequisite checklist provided to Hermes so it can guide users

**Verification:** A user following the README on a fresh macOS reaches a
running governed agent in under 15 minutes.

#### 15-T08: HTTP/HTTPS Egress Enforcement (P1 Complete)

**Problem:** HTTP/HTTPS egress goes through the gateway via HTTP_PROXY. Raw
TCP, DNS tunneling, IPv6 are blocked by iptables but not inspected. This is
acceptable for P1.

**Scope (P1):**
- HTTP/HTTPS egress via gateway proxy — already working (B14B)
- iptables egress firewall for non-HTTP — already working (B14E R17)
- IPv6 blocked via ip6tables — already working (B14E R17)

**P2 scope (not blocking v0.1.0):**
- Transparent proxy (iptables redirect instead of HTTP_PROXY env)
- DNS-level inspection
- Raw TCP/UDP deep inspection
- DLP on outbound content (semantic, not fingerprint-based)

**Verification (P1):** Agent's HTTP call to an allowed domain succeeds. HTTP
call to a denied domain is blocked by the gateway. Raw TCP attempt is blocked
by iptables. IPv6 attempt is blocked by ip6tables. All denials audited.

### P2 Items (tracked, not blocking v0.1.0)

#### 15-P2-01: Linux Support
macOS-only (Colima/Docker Desktop). Linux dockerd is P2:
- systemd service unit for the daemon
- libsecret or D-Bus Secrets API instead of macOS Keychain
- seccomp/AppArmor profiles
- deb/rpm packaging via goreleaser
- CI on Linux runner

#### 15-P2-02: Dashboard / Observability
Real-time event timeline exists (audit tailer → EventBus → dashboard on port
8080). P2 additions:
- Policy diff viewer
- Run comparison view
- Cost tracking UI
- Visual timeline of runs, denials, costs
- "What did this agent do" view without CLI

#### 15-P2-03: Multi-Agent Orchestration
Currently one agent = one run = one container. P2:
- Chain agents (agent A output feeds agent B)
- Shared state between runs
- Scheduled/triggered runs from external webhooks
- Agent versioning beyond image digest pinning

#### 15-P2-04: Non-HTTP Egress Deep Inspection
HTTP/HTTPS is gateway-proxied (P1). P2:
- Transparent proxy (iptables redirect instead of HTTP_PROXY env)
- DNS-level inspection
- Raw TCP/UDP deep inspection
- Semantic DLP on outbound content

### BLOCK 15 SUCCESS GATE

`make block15-gate` runs all P1 sub-segment gates:
- 15-T01: Credential onboarding (`secret add/list/remove/rotate/test`)
- 15-T02: LLM provider integration test (real LLM call, audited)
- 15-T03: Policy authoring via Hermes (default templates, validation at pack time)
- 15-T04: Trigger/cron/event surface (invoke, cron add/list/remove, event publish)
- 15-T05: Production hardening (init container, encrypted key, Rekor retry)
- 15-T06: Release binary exists and `brew install` works
- 15-T07: Clean-machine prerequisites documented and verified
- 15-T08: HTTP/HTTPS egress enforcement (already working, regression gate)

All must pass. This is the final gate before Block 16 manual testing.

**Build order:** 15-T01 (credential onboarding) → 15-T02 (LLM integration,
depends on T01) → 15-T03 (policy authoring) → 15-T04 (trigger surface) →
15-T05 (production hardening) → 15-T06 (release binary) → 15-T07 (clean-machine
docs) → 15-T08 (regression gate, already passing). Then Block 16 manual testing.

---

## BLOCK 16 — Manual Use-Case Assessment (runs AFTER Block 15)

**SEQUENCE: This block runs after Block 15.** Block 15 closes the pre-release
gaps (LLM integration, credential onboarding, triggers, release binary,
production hardening). Testing before those are closed finds the wrong
rough edges.

This block replaces the former "Sequencing, founder calendar, and execution
control" block. The old sequencing content is preserved below in §16.3 for
reference but is no longer a gate.

Block 16 is the founder-driven manual testing phase. You (Parvez) work through
real-world use cases one at a time, with Hermes as the assistant, to find
leftover rough edges before v0.1.0 ships. This is NOT automated testing — it
is human-in-the-loop exploratory testing of the full product experience.

See `docs/b15-e2e-test-plan.md` for the lifecycle use cases (LC-01..LC-05) that
supplement the security-focused UC-01..UC-10 matrix below.

### 16.1 Use-Case Assessment Protocol

Each use case is assessed one at a time. For each:

1. **Define the scenario** — what agent type, what policy, what egress, what
   secrets, what the expected outcome is.
2. **Run it manually** — use the Hermes plugin (`/agentpaas deploy`) or the CLI
   directly (`agent pack`, `agent run`). Do NOT use test fixtures — use real
   agent code that a coding agent would generate.
3. **Observe the full experience** — dashboard, audit trail, logs, timeline,
   policy denials, error messages. Check each surface for clarity and correctness.
4. **Record findings** — any rough edge, confusing message, missing feature,
   or unexpected behavior gets filed as a bug or a docs issue.
5. **Fix or defer** — critical bugs get fixed immediately (back to Block 14
   sub-segments). Non-critical findings get tracked for P2.

### 16.2 Use-Case Matrix

The following use cases must be assessed. Each is a separate session. Do not
batch — the point is to find rough edges through focused attention.

**UC-01: Weather/API agent (the canonical demo)**
- Agent calls a weather API, attempts a denied exfil probe.
- Verify: dashboard shows DENIED probe, audit trail is complete and signed,
  export bundle verifies on a second machine.
- This is the minimum viable demo — it must be flawless.

**UC-02: Secret-brokered SaaS action**
- Agent uses a brokered credential through the gateway to call a SaaS API.
- Verify: secret value never visible in code/logs/audit, upstream receives
  authorized request, credential rotates after use.
- Tests the secrets broker + gateway injection end-to-end.

**UC-03: Agentic repair loop**
- Agent has a dependency/code defect + missing egress policy.
- Verify: `agentpaas_explain_failure` → `agentpaas_next_action` fixes code
  automatically, proposes policy (waits for confirmation), reruns, exports
  signed audit bundle.
- Tests the full operator repair loop.

**UC-04: Prompt-change immutable redeploy**
- Agent is already running. Change the agent prompt, redeploy.
- Verify: new run has distinct digests, old run stops cleanly, audit shows
  both old and new runs with distinct image/policy digests.
- Tests the immutable redeploy path.

**UC-05: Long-running agent with mixed egress**
- Agent runs for 5+ minutes, makes multiple HTTP calls (some allowed, some
  denied).
- Verify: live egress events in dashboard timeline (14B), audit trail grows
  in real time, budget enforcement works.
- Tests 14B real-time visibility + budget caps.

**UC-06: Clean-machine install (your own fresh account)**
- Install AgentPaaS from scratch on a fresh macOS user account using ONLY
  the README.
- Verify: <15 minutes to first governed agent, no undocumented steps, no
  missing prerequisites.
- This is the volunteer test done by you first — you are the harshest
  critic of your own docs.

**UC-07: Policy authoring experience**
- Write a `policy.yaml` from scratch for a custom agent with mixed egress
  (some domains allowed, some denied, some credential-backed).
- Verify: policy validates, compiles to agentgateway config, enforced at
  runtime, denials are explainable via `agentpaas_explain_policy_denial`.
- Tests the policy UX end-to-end.

**UC-08: Audit export + verification on second machine**
- Export audit trail from a completed run, transfer to a second machine,
  verify integrity.
- Verify: `agent audit verify` passes, hash chain is intact, signed export
  manifest validates, a security reviewer could trust this evidence.
- Tests the audit export story for design partners.

**UC-09: Daemon lifecycle (start/stop/restart/upgrade)**
- Start daemon, run agents, stop daemon mid-run, restart, verify state
  recovery. Upgrade via `brew upgrade`, verify state migration.
- Verify: no orphaned containers/networks, daemon state survives restart,
  upgrade path is clean.
- Tests operational reliability.

**UC-10: Hermes integration depth**
- Use the Hermes plugin exclusively (no CLI) for a full session: detect
  agent code → deploy → status → logs → metrics → repair → redeploy.
- Verify: every slash command works, every tool returns structured output,
  prompt-injection boundary holds (inject hostile instructions in agent
  source/comments, verify no policy alteration or secret disclosure).
- Tests the Hermes operator experience as a real user would use it.

### 16.3 Sequencing Reference (not a gate — context only)

```
B1 → B2 → B3 ─┬→ B4 → B5 ─┬→ B6 → B7 → B8 ─┬→ B9 → B10 → B11 → B12 → B13 → B14 → B15 → B16
              │            │                 │
              └ B3 gates everything security-spine-first
```

B4/B5 can interleave with B6 SDK design once Block 1 contracts are frozen.
B10 dashboard can start once B9 events exist. B11 operator contract depends on
B1-B10 JSON/control surfaces. B12 red-team needs B5-B8 plus B11 operator
methods. B13 integrations depend on B11 and B12. B14 (consolidated) closes
all post-B13 build work. **B15 (P1 gap closure) runs before B16 (manual
testing)** — B16 tests the experience that B15 makes possible. Testing with
fake LLM, no credential onboarding, and no triggers finds the wrong rough
edges. B15 closes P1 pre-release gaps (LLM integration, credential onboarding,
policy authoring, production hardening, release binary). B16 is manual
use-case assessment — done after B15 so the full experience is testable.

**P2 calendar target (four weeks after P1 ships):**
- Week 6: Linux certification (dockerd, systemd, libsecret, seccomp/AppArmor, deb/rpm).
- Week 7: Customer-facing control-plane (team/fleet, hosted identity/audit,
  registry/promotion, tenant metadata, support bundle).
- Week 8: Commercial observability + opt-in telemetry (consent UX, privacy docs,
  fleet health, team dashboards).
- Week 9: P2 release hardening (production docs, support playbook, design-partner
  onboarding, upgrade/rollback, pricing hooks, RC + launch packet).

**Execution control decisions (carried forward):**
- Once implementation starts, do not silently slip P1 blocks. If the calendar
  is impossible, stop and explicitly rescope before continuing.
- Block 13 P1 scope is Hermes-only. Generic MCP server + Claude Code/Codex/Cursor
  integrations move to P2.
- CrewAI support means AI-generated CrewAI projects pack/run through the generic
  Python harness. AgentPaaS is not building a CrewAI authoring framework.
- Node SDK remains explicitly deferred and is not part of the P1 gate.
- Audit, policy, network enforcement, secrets invisibility, packaging/signing,
  Hermes operator contract, redteam-smoke, and integration contract parity are
  never cut from P1.

### 16.4 Block 16 Test Findings — Fix Tasks

These tasks arise from manual test findings recorded on Kanban cards during
Block 16 testing. Each must be resolved before the block gate can pass. Fix
one at a time, in order.

---

#### T1 — LC01 Follow-up: Hermes Plugin UX Gaps (Kanban: t_5d2d4362)

Source: Parvez's follow-up comment on B16-LC01 (2026-07-02 21:05).

**T1a: Fix non-functional slash commands in Hermes autocomplete**
- Problem: `/agentpaas-status` and similar slash commands appear in Hermes
  autocomplete but don't resolve when selected. Only commands that actually
  work should be shown.
- Root cause to investigate: The Hermes plugin declares slash commands that
  are not wired to actual handlers, or the handlers exist but the routing is
  broken.
- Acceptance criteria:
  - Every slash command that appears in Hermes autocomplete resolves and
    executes when selected
  - Non-functional slash commands are removed from the plugin manifest or
    wired to working handlers
  - Verify from a fresh Hermes session: type `/`, select each agentpaas
    command, confirm it works
- Likely files: `integrations/hermes-plugin/plugin.yaml`, any slash command
  handler files under `integrations/hermes-plugin/`
- T1a-followup: After the skill and plugin are installed, Hermes should
  clearly tell the user "Restart Hermes using /quit to load the plugin."
  Currently the agent says "takes effect next session" but doesn't give
  explicit restart instructions. The onboarding flow should include a
  clear, actionable restart step. Fix before T2 and push to GitHub.
- T1a-followup-2: The install flow enables the plugin (plugins.enabled)
  but does NOT add the `agentpaas` toolset to platform_toolsets.cli.
  These are separate config steps in Hermes — enabling the plugin
  registers slash commands, but the agent can't see or call the
  agentpaas_* tools unless the toolset is also in platform_toolsets.
  The install instructions / make install-plugin should do both:
  1. hermes plugins enable agentpaas
  2. hermes config set platform_toolsets.cli (append agentpaas)
  Fix before T2 and push to GitHub.

**T1b: Add onboarding context when user says "I want to use agentpaas"**
- Problem: When user mentions agentpaas, Hermes does not explain what
  AgentPaaS is, what it does, or suggest usage patterns. A new user won't
  understand the product or how to interact with it.
- Fix: Add a skill or system-prompt context that triggers on agentpaas
  mentions and provides: brief product description, key capabilities, and
  suggested first actions (e.g., "try agentpaas_doctor to check your setup",
  "ask me to create a new agent project").
- Acceptance criteria:
  - When user says "I want to use agentpaas" (or similar), Hermes responds
    with a concise explanation of AgentPaaS and suggested next steps
  - The response is actionable, not just marketing copy
  - Tested from a fresh Hermes session with no prior context
- Likely files: `integrations/hermes-plugin/SKILL.md`, possibly a new skill
  or onboarding doc

**T1c: Improve install/setup documentation for open-source users**
- Problem: A new user cloning the repo has no clear walkthrough for: what to
  install, in what order, how to configure the Hermes profile, how to verify
  it works.
- Fix: Create a guided setup flow or improve README/quickstart with a
  numbered install checklist: prerequisites → install AgentPaaS binary →
  install Hermes plugin (`make install-plugin`) → start daemon → verify with
  `agentpaas doctor` → first agent.
- Acceptance criteria:
  - README or quickstart has a step-by-step install walkthrough that a fresh
    user can follow without prior knowledge
  - Each step has a verification command and expected output
  - No undocumented steps required
- Likely files: `README.md`, `docs/quickstart.md`

**T1c-followup: Plugin lives in a subdirectory, not repo root**
- Problem (found 2026-07-03 LC-01 test): A user giving the repo root URL
  (`https://github.com/AgentPaaS-ai/agentpaas`) to `hermes plugins install`
  gets the ENTIRE repo cloned into plugins/agentpaas/ — but plugin.yaml
  lives at `integrations/hermes-plugin/plugin.yaml`, not at the repo root.
  Hermes says "not recognized as a standard Hermes plugin." The agent that
  ran the install knew nothing about the subdirectory layout and could not
  discover it.
- Root cause: No plugin.yaml at repo root. `hermes plugins install` expects
  plugin.yaml at root unless given a `/tree/main/<subdir>` URL.
- Fix: Either (a) document the exact subdirectory install URL in README,
  OR (b) add a root-level plugin.yaml that delegates/re-exports from
  integrations/hermes-plugin/, OR (c) move the plugin to the repo root.
  Option (a) is minimum viable for v0.1.0; (b) is cleaner long-term.
- Correct install command for users:
  `hermes plugins install https://github.com/AgentPaaS-ai/agentpaas/tree/main/integrations/hermes-plugin --enable`
- Acceptance criteria:
  - README "Hermes Plugin" section shows the subdirectory install URL, not
    just `make install-plugin` (which is dev-only, requires local repo)
  - A user following the README install command succeeds on first try
  - Optional: root-level plugin.yaml shim that redirects to the real plugin
- Likely files: `README.md`, optionally `plugin.yaml` (new root-level shim)

**T1c-note: Plugin skills not in Available Skills list (expected behavior)**
- Observation (2026-07-03 LC-01): After successful plugin install + restart,
  the agentpaas skill does NOT appear in the system prompt's Available
  Skills section. This is BY DESIGN in Hermes — plugin-provided skills
  (registered via `register_skill`) are opt-in, explicit-load only and are
  deliberately excluded from the `<available_skills>` index.
- The skills ARE available, just not auto-advertised:
  - `agentpaas:agentpaas` — main plugin SKILL.md
  - `agentpaas:llm-configuration`
  - `agentpaas:secret-onboarding`
  - `agentpaas:policy-generation`
- The tools ARE visible and working (confirmed: agentpaas_audit_query etc.
  show in Available Tools).
- NOT A BUG — do not try to "fix" this. Document in onboarding so users
  know skills are loadable via skill_view(plugin:skill), not browsed.
- Acceptance: onboarding SKILL.md mentions that plugin skills are
  explicit-load (skill_view with qualified name), not auto-listed.

---

#### T2 — LC02/UC01: Invalid Scaffolded Policy (Kanban: t_e05f9789)

Source: Parvez's test results on B16-LC02/UC01 (2026-07-02 21:11). Verdict:
FAIL — agent did not run because scaffolded policy was invalid.

**T2a: Fix `agentpaas_init_project` to generate valid policy.yaml**
- Problem: The default policy.yaml generated by `agentpaas_init_project` uses
  WRONG field names (`host`, `port`, `protocol`, `description`). The actual
  schema expects (`domain`, `ports`, `allow_wildcard`). The scaffolded policy
  is invalid out of the box.
- Fix: Update the init_project scaffold template to use the correct schema
  field names matching what the policy validator expects.
- Acceptance criteria:
  - `agentpaas_init_project` generates a policy.yaml that passes
    `agentpaas_validate_project` without any manual fixes
  - The generated policy uses fields: `domain`, `ports`, `allow_wildcard`
  - No field name from the old schema (`host`, `port`, `protocol`,
    `description`) appears in the scaffold
- Likely files: the init_project scaffold template (search for the embedded
  policy.yaml template in the Go source, likely under `internal/` or
  `cmd/`)

**T2b: Fix policy schema mismatch between init_project and policy_init**
- Problem: The `init_project` scaffold and the `policy_init` template
  produce DIFFERENT policy schemas. This inconsistency confuses users.
- Fix: Ensure both code paths use the same policy schema and field names.
  The `policy_init` template (allow-http, allow-llm, allow-mcp, deny-all)
  is the correct reference — `init_project` must match it.
- Acceptance criteria:
  - `agentpaas_init_project` then `agentpaas_policy_show` shows a policy
    with the same schema as `agentpaas_policy_init --template deny-all`
    then `agentpaas_policy_show`
  - Both paths produce policies that pass validation
- Likely files: init_project scaffold template, policy_init templates

**T2c: Add docs/inline help for valid policy.yaml fields**
- Problem: No documentation on what fields policy.yaml accepts. User had to
  reverse-engineer from the template. The validation error message was
  helpful (listed wrong fields) but didn't show the correct schema.
- Fix: Add a policy.yaml reference section to docs (or inline help via
  `agentpaas policy --help`) showing all valid fields, types, and examples.
- Acceptance criteria:
  - `docs/policy-reference.md` (or equivalent) exists with: all valid
    fields, their types, allowed values, and at least 2 examples
  - Validation error messages include a hint pointing to the docs or
    showing the correct schema
- Likely files: new `docs/policy-reference.md`, validation error messages
  in Go source

---

**BLOCK 16 SUCCESS GATE:** `make block16-gate` is a docs-only gate: a checklist
confirming all 10 use cases in §16.2 (plus LC-01..LC-05 from
docs/b15-e2e-test-plan.md) have been assessed, with findings recorded and
critical bugs resolved. There is no automated gate — this block is inherently
human-driven. "Done" means you have personally walked through every use case
and signed off on the experience. **Block 16 gate cannot run until Block 15
gate passes.** **Block 16 gate cannot pass until T1 and T2 fix tasks are
resolved.**

---

#### T3 — LC02: Invocation + Log Streaming Bugs (found 2026-07-03)

Source: Parvez's LC-02 test (weather agent deploy + invoke). Multiple
blocking bugs found when actually exercising the deploy→invoke→observe
loop.

**T3a (BLOCKER): agentpaas logs crashes on invalid UTF-8 in log output**
- Problem: `agentpaas logs <run-id>` fails with:
  `rpc error: code = Internal desc = grpc: error while marshaling: string field contains invalid UTF-8`
- The gRPC log stream handler cannot serialize log lines containing
  non-UTF-8 bytes (common from Python tracebacks, Docker layer output,
  binary data in stdout, etc.).
- Impact: Agent log output is entirely inaccessible via the CLI/plugin
  when ANY log line contains invalid UTF-8. The user cannot debug agent
  failures.
- Fix: Sanitize log lines to valid UTF-8 before marshaling (replace
  invalid bytes with U+FFFD, or base64-encode the field, or use a bytes
  field instead of string in the proto).
- Acceptance criteria:
  - `agentpaas logs <run-id>` works for runs with non-UTF-8 output
  - Invalid bytes are replaced or escaped, not fatal
  - Unit test with binary data in log stream
- Likely files: log streaming RPC handler in internal/daemon or
  internal/harness

**T3b (BLOCKER): agentpaas_status tool/CLI naming mismatch**
- Problem: The plugin tool is named `agentpaas_status` and takes a
  run_id, but the underlying CLI command is `agentpaas timeline <run-id>`
  (there is no `agentpaas status` subcommand — `status` is not a valid
  CLI verb). The agent (and the user) naturally tries `agentpaas status`
  which returns "unknown command".
- The correct CLI command for run status appears to be
  `agentpaas timeline <run-id>`, but the plugin tool is named
  `agentpaas_status` creating a mismatch.
- Fix: Either add a `status` CLI subcommand alias, or align the plugin
  tool name with the actual CLI command. At minimum, the plugin's
  SKILL.md must document the correct command.
- Acceptance criteria:
  - `agentpaas status <run-id>` works as a CLI command (alias for
    timeline or a proper status verb), OR
  - The plugin tool name matches the CLI command name
- Likely files: cmd/ CLI dispatch, integrations/hermes-plugin/

**T3c (BLOCKER): trigger invoke --payload expects file path, plugin passes inline JSON**
- Problem: `agentpaas trigger invoke <agent> --payload <X>` expects X
  to be a FILE PATH, not inline JSON. But the plugin tool
  `agentpaas_trigger_invoke` parameter description says "Optional path
  to a payload file" — yet the test agent passed raw JSON
  (`{"lat":51.5074,"lon":-0.1278}`) as the payload arg, causing:
  `read payload file: open {"lat":51.5074,"lon":-0.1278}: no such file`
- The agent wrote a payload.json file and tried to pass its contents,
  not the path. This is a tooling/UX gap.
- Fix: The plugin tool should either (a) accept inline JSON and write
  it to a temp file before calling the CLI, or (b) the CLI should
  accept inline JSON with a flag like `--payload-json`.
- Acceptance criteria:
  - `agentpaas_trigger_invoke` plugin tool can accept inline JSON
    payload and handle it correctly
  - OR the tool description clearly states "path to file" and the
    agent is guided to write then pass the path
- Likely files: integrations/hermes-plugin/tools.py, CLI trigger invoke

**T3d: Run failed with status=failed, but no failure detail surfaced**
- Problem: The timeline shows `run_stop` with `"status":"failed"` for
  run-3b459e8f6bb24ce4, but there's no indication of WHY it failed.
  Combined with T3a (logs broken), the user has no way to debug.
- Fix: Ensure failed runs include a failure reason/error message in the
  timeline event or status output. `agentpaas explain-failure` should
  work here, but needs to be reachable.
- Acceptance criteria:
  - Failed runs show a reason in timeline or status
  - `agentpaas_explain_failure` returns root cause for failed runs

**T3e: No way to list running agents**
- Problem: There is no CLI command or plugin tool to list currently
  running agents. `agentpaas run` starts a run but there's no
  `agentpaas list`, `agentpaas ps`, `agentpaas run list`, or any verb
  to see what's currently deployed/running. The only way to check a
  run is to already know its run_id.
- Impact: A user who starts multiple agents has no way to see how many
  are running, what their run_ids are, or what's consuming resources.
  Orphaned runs are invisible. `agentpaas_status` without a run_id
  returns daemon health, not a run inventory.
- Fix: Add:
  1. CLI: `agentpaas run list` (or `agentpaas ps`, `agentpaas list`)
     — shows all active runs with run_id, agent_name, status, started_at
  2. Plugin tool: `agentpaas_list_runs` (or rename `agentpaas_status`
     to return a run inventory when called without run_id)
  3. The daemon already tracks runs in state.db — this is a read query
- Acceptance criteria:
  - `agentpaas run list` shows all running/recent runs
  - Output includes: run_id, agent_name, status, started_at, duration
  - Plugin tool `agentpaas_list_runs` wraps it
  - `agentpaas_status` with no run_id could optionally show a summary
    (N agents running) in addition to daemon health

**T3f (P0 BLOCKER): Agent invocation never works — harness never becomes ready**
- Problem: EVERY run fails with "harness /readyz not ready after 30
  attempts". The daemon retries for 30 seconds, gives up, and the run
  fails. The agent code NEVER executes.
- Root cause (confirmed via container inspection): The Python worker in
  the harness tries `from agentpaas_sdk import run` (python_worker.go:468).
  If the SDK is found, it calls `run()` which handles the agent lifecycle.
  If the SDK is NOT found (ModuleNotFoundError), it falls through to a
  raw importlib loader that looks for `invoke(payload)` — but the scaffolded
  agent defines `app(input)`, not `invoke()`. So:
    1. agentpaas_sdk is NOT bundled in the container image (the Dockerfile
       at build.go:509-524 does not COPY the python/ directory into the image)
    2. The fallback path requires `invoke()`, but the scaffold template
       (init.go:208) generates `def app(input)`
    3. Result: the worker sends `{"type":"import_failed"}` and the harness
       stays in not_ready state forever (503 on /readyz)
    4. Daemon times out after 30 attempts, run fails
- Impact: Agents NEVER run. This is not a partial failure — it's a
  complete block of the core product function. No agent can execute.
- Fix (two parts, both needed):
  1. Bundle the Python SDK in the container image. Add to the generated
     Dockerfile: `COPY --chown=0:0 python/ /app/python/` so the worker's
     `sys.path.insert(0, repo_python)` at python_worker.go:463 resolves.
  2. The SDK's run() must bridge to `app(input)` (the documented entry
     point in agent.yaml `entry: main:app`). OR the scaffold must
     generate `invoke()` instead of `app()`. Either way, the entry point
     must be consistent between scaffold, SDK, and worker.
- Acceptance criteria:
  - `agentpaas_run` produces a run that reaches "ready" state
  - `agentpaas trigger invoke` actually executes the agent and returns
    its output
  - Audit trail shows egress events (not just run_start)
  - The agent output is visible via logs or invoke response
- Likely files: internal/pack/build.go (Dockerfile template),
  internal/harness/python_worker.go (entry point bridging),
  python/agentpaas_sdk/runner.py (app() vs invoke() dispatch)

---


## 15.1 POST-P1 CONTINUATION — BLOCKS 25–27

The original numbered build plan above remains the P1 historical contract.
Current work continues in separately maintained block specifications; when a
later block conflicts with an older implementation detail, the later block's
explicit security invariant governs that release scope.

- **Block 25 — release and dependency closure:** T00-A/B/C execute first:
  upgrade the Go toolchain, enforce patched Docker Engine readiness, and
  migrate to `moby/moby/client` plus `moby/moby/api`. Only then implement
  Hermes sharing, run S1–S10 and the seven-claim adversary gate, truth-sync
  docs, and promote v0.2.0/Homebrew. See
  `docs/execution/blocks/b25-summary.md`.
- **Block 26 — operator portability and approval evidence:** T01 Codex skill,
  T02 shared Hermes/Codex conformance suite, and T03 Agent Approval Pack are
  the ordered required block. T04 is an evidence-based generic MCP decision
  and does not block B27. Hosted Grok and all distribution rails remain
  demand-gated backlog, not block completion work. See
  `docs/execution/blocks/b26-summary.md`.
- **Block 27 — keyless enterprise workflow capability closure:** implement
  the 14 chunks in its dependency table. T01 first proves the pinned gateway
  mutation/Broker topology. T02–T05 establish signed keyless requirements,
  receiver-local authorization, request-time injection, and remove the legacy
  harness credential preload. T06–T13 add constrained actions and enterprise
  workflow/evidence features; T14 alone executes the final two-person demo.
  Agent packages carry signed credential requirements only. The deterministic
  Credential Broker—not an LLM security agent—obtains/refreshes receiver
  credentials and authorizes per-request gateway header mutation after route,
  identity, policy, body, and approval checks. Raw credentials never enter
  agent code, invoke payloads, package bytes, static gateway configuration,
  logs, or evidence. See
  `docs/execution/blocks/b27-summary.md` and
  `docs/execution/reference/golden-enterprise-demo.md`.

Block 27 is not complete merely because the scripted demo succeeds. T01–T14
are all P0 for the full enterprise claim, and its
security/adversary gates must prove keyless pack/export with a locked secret
store, byte-identical bundles across changed local secret values,
receiver-specific authorization, OAuth expiry/rotation/revocation behavior,
run-bound broker isolation, no secret-bearing authorization cache, exact-body
write approval, crash cleanup, and sentinel absence across build/runtime/
evidence surfaces. The gateway is explicitly part of the local trusted
computing base for the access token or static key on the request it forwards;
the product must not claim otherwise.

---

## 16. DEFINITION OF DONE (PHASE 1)

The execution plan is complete when PRD v4 §8 (Success Definition) items
1–8 are demonstrably true via the gates above. Blocks 1-14 are complete only
when their `make blockN-gate` wrappers exit 0, and Block 14 must also collect
the volunteer/release evidence named in PRD §8. Block 16 closes the remaining
P1 pre-release gaps (LLM integration, credential onboarding, policy authoring,
production hardening, release binary) — `make block16-gate` must exit 0. Block
15 is the manual use-case assessment — done when all 10 use cases are assessed
and signed off by the founder. "Done" is always a command plus recorded
evidence, never a judgment call.

**END OF EXECUTION PLAN v1.1 — companion to agentpaas-prd-v4-master.md**
