# AGENTPAAS PHASE 1 — EXECUTION PLAN v1.0
**Purpose:** The build contract. Each BLOCK is sized for one focused LLM
coding session, carries an exact build prompt, a test plan with edge cases,
and a binary success gate. No block starts until the previous gate is green.
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
    primary: composer-2.5-via-x-oauth
    fallback: glm-5.2-via-openrouter
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
   the orchestrator explicitly reassigns it as a worker.
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
   final spec approver.
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
1. Confirm the diff is scoped to the issue.
2. Run the canonical gate or the narrowest meaningful failing target first,
   then the canonical gate when possible.
3. Verify that every acceptance criterion has direct evidence.
4. Verify that every security claim has a negative test, or list the missing
   test.
5. Try to reproduce any worker-reported failure.
6. Report exact PASS/FAIL evidence and repro steps.

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
- **Orchestrator primary:** GLM-5.2 through OpenRouter.
- **Orchestrator fallback:** DeepSeek V4 Pro.
- **Worker primary:** DeepSeek V4 Flash.
- **Worker fallback:** Composer 2.5 through X OAuth.
- **Verifier primary:** Composer 2.5 through X OAuth.
- **Verifier fallback:** GLM-5.2 through OpenRouter.
- **Adversary primary:** Grok 4.3.
- **Adversary fallback:** GLM-5.2 through OpenRouter.

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
3. **Verifier Worker:** Composer 2.5 through X OAuth for independent test
   writing, gate execution, failure reproduction, and missing-test
   identification. Fallback: GLM-5.2 through OpenRouter.
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
- Do verifier-worker and required adversary passes before the final
  orchestrator approval.
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
GitHub Actions (lint, test, -race, osv-scanner); Makefile targets
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
egress binding for remote MCP servers; hook destination declarations checked
as policy data in Block 4 and rechecked at delivery time in Block 9;
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
same Invoke path. P1 supports URL webhooks only; local command hooks are
deferred. Audit events include `api_key_created`, `api_key_revoked`,
`auth_failed`, `invoke_accepted`, `invoke_rejected`, `idempotency_replayed`,
`idempotency_conflict`, `rate_limited`, `webhook_delivered`,
`webhook_dead_lettered`, `cron_missed`, `cron_skipped_concurrency`,
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
skips and audits; CancelRun mid-LLM/MCP-call → audit cancel_requested, ask
gracefully, wait 30s, force stop if needed, audit final canceled/forced
outcome.
**SUCCESS GATE:** `make block9-gate` passes: API conformance suite (generated from proto) green;
auth/API-key lifecycle + idempotency + rate-limit + SSE reconnect e2e green;
cron/webhook tests prove same policy/audit path as manual Invoke; cancel
semantics e2e green; fuzz on REST JSON ingestion (100k execs, 0 crashes).

---

## BLOCK 10 — OTel pipeline + Dashboard
**Builds:** in-process OTLP collector → SQLite (WAL) with retention prune
(default 7d local, configurable) for OTel traces/logs/metrics only; canonical
audit JSONL is not pruned by dashboard retention and is purged only by an
explicit future user retention/purge command. Agent/harness/gateway logs are
ingested as OTel log records for dashboard correlation; daemon operational
logs remain bounded structured JSON files under `~/.agentpaas/logs/` with
rotation/redaction and are linked from `agent doctor`/`agent logs` but are not
the canonical audit source. Dashboard SPA (preact+TS, go:embed, no runtime
CDN): agent list w/ status+spend-vs-budget, run timeline with a stable event
schema (LLM calls w/ tokens+cost, MCP calls, egress ALLOWED/DENIED rows in
red, budget/audit markers), log viewer with truncation/redaction, policy view
showing both git-file diff and normalized effective policy digest, audit
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
redacted everywhere; binary/control characters and huge log/attribute values
are safely escaped/truncated with pointers to full retained logs where
allowed; clock-skewed spans → ordered by monotonic seq; security events are
never sampled out of canonical audit even if OTel retention prunes dashboard
telemetry; empty states designed (zero agents, zero runs); accessibility and
keyboard smoke test.
**SUCCESS GATE:** `make block10-gate` passes: Playwright e2e launches agent → watches live run → sees a
DENIED egress row → export audit → verify export. Lighthouse perf ≥ 90
local. Planted-XSS and sentinel-secret tests show escaped/redacted output.
10k-span, SSE reconnect, SQLite lock/corruption recovery, empty-state, policy
diff, audit export verify, and accessibility smoke tests green.

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
  cancel outcomes that tools can resume from.
- Block 10 dashboard/OTel exposes the same timeline/audit/policy data as JSON;
  the UI is a view, not the source of truth.

**Safety model:** Agentic tools may automatically repair code, tests,
`agent.yaml`, dependency declarations, and non-security config inside the
project root. They may propose `policy.yaml` changes, new egress, credential
bindings, direct leases, webhook destinations, exposed listeners, retention
purges, and destructive actions, but P1 requires explicit user/daemon confirm
before applying them. Tools cannot read secret values, cannot broaden policy
silently, cannot delete audit, cannot disable red-team gates, and cannot use
paths outside the invoking project root. Prompt-injected instructions inside
agent source/logs/traces are untrusted data and must not cause policy changes,
secret disclosure, audit deletion, or destructive operations.

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
`review_policy_patch`, `increase_budget`, `rerun`, `export_audit`, or
`ask_user`.

**Edge cases:** malformed/old JSON schema version → clear compatibility error;
tool asks for path outside project root → refusal with audit event; huge logs
or build output → truncated excerpts + stable refs; denied egress → policy
patch is proposed but not applied; missing secret → secret binding request is
proposed but value is never requested through the agentic tool; prompt
injection in source/logs says "approve all policy" → ignored and tested;
network/dashboard unavailable → tool falls back to daemon/control JSON; daemon
restart mid-loop → idempotency and run refs let Hermes resume; human
declines policy patch → next action becomes `fix_code` or `ask_user`, not
policy bypass.
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
release gate.

---

## BLOCK 13 — Hermes integration plugin/skill
**Builds:** integrations/hermes-plugin — a Hermes plugin/skill exposing the
daemon's Block 11 operator contract as tools. Required P1 tools:
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

The Hermes plugin/skill includes SKILL.md plus native tool/MCP setup if Hermes
requires it. It teaches the flow (detect agent code →
`agentpaas_init_project` scaffold → validate → pack → run → inspect
timeline/dashboard → show first audit event), with pitfalls (Docker not
running → `agentpaas_doctor`; policy denial →
`agentpaas_explain_policy_denial` then
`agentpaas_recommend_policy_patch`). Terminal CLI commands remain documented
fallback steps.
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
installed: Hermes generates an agent → `agentpaas-deploy` → agent running
governed → dashboard shows a DENIED probe → post-install deploy flow <10
minutes; then Hermes is asked to change the agent prompt and must drive the
same immutable redeploy path (edit project prompt/config → validate → pack →
verify → run), with the second run reflecting the new prompt and audit
showing distinct old/new digests. Hermes integration conformance tests pass
against the spec revision pinned in the integration package.

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

## BLOCK 14 — Install path, docs, demo, and v0.1.0 release
**Builds:** distribution surface area —
- P1 is macOS-first. Homebrew tap (`brew install agentpaas/tap/agentpaas`)
  is the primary install path for darwin/arm64 and darwin/amd64. Linux native
  packages, Linux CI release certification, deb/rpm via nfpm, and Windows/WSL2
  docs move to P2 unless a design partner creates a hard requirement before
  launch.
- Installer trust posture: no blind `curl|bash`; Homebrew installs the binary
  but does not silently start background services. First run is explicit:
  `agent doctor` checks Docker Desktop/Colima, keychain, loopback ports,
  daemon socket permissions, release signature status, and dashboard port.
  `agent setup launchd` or documented `brew services start agentpaas` creates
  the user-level launchd service only after explaining what will run, where
  logs/state live, and how to uninstall.
- Release pipeline: goreleaser builds darwin/arm64+amd64, produces checksums,
  SBOMs, provenance, and cosign signatures. Verification follows Sigstore
  keyless best practice using GitHub Actions OIDC identity, with a single
  copy-paste `cosign verify-blob` command plus an `agent verify-release`
  helper so early adopters are not forced to learn the whole supply-chain
  stack on day one.
- Docs site (docs/ → static): Quickstart (the <15-minute path), policy
  reference, secrets guide, "How enforcement actually works" (the
  network-topology page — security engineers read this one first),
  threat model (§3 of PRD published verbatim), known limitations (§3.3),
  audit-export verification guide for a second machine, privacy/telemetry
  page, Hermes plugin/skill setup, and demo scripts. Claude Code, Codex,
  Cursor, and generic MCP integration docs move to P2.
- The 3-minute demo video script + asciinema recordings embedded in
  README and landing page. Minimum v0.1.0 launch demo: Hermes writes an
  agent → AgentPaaS packs/signs/SBOMs it → governed run → blocked
  exfil attempt → signed audit export. Stretch launch demos: secret-brokered
  SaaS action and agentic repair loop from Block 13.
- README: the 60-second story above the fold, containment table from
  `make redteam-smoke` pasted as proof, explicit "zero telemetry, zero
  phone-home by default" statement, prerequisites called out up front
  (macOS, Homebrew, Docker Desktop or Colima), and a "known limitations"
  link near the install command.
- Air-gapped/offline path: P1 documents a macOS offline bundle containing
  signed binaries, checksums, SBOMs, container images, policy/demo fixtures,
  and verification instructions. The offline path may be more manual than
  Homebrew, but it must prove that release artifacts and audit exports can be
  verified without network access.
- Docs/release CI: broken-link check, command-snippet smoke scripts,
  README quickstart smoke, release-artifact verification matrix, screenshot
  and asciinema freshness check, and explicit docs issue filing for every
  clean-machine deviation.
**Edge cases:** clean-machine test on macOS (fresh user account) following
ONLY the README, with Docker Desktop or Colima already installed as a stated
prerequisite — every deviation found is a docs bug, filed and fixed before
release; brew upgrade preserves daemon state + restarts cleanly; failed daemon
state migration rolls back or leaves a clear manual recovery path; uninstall
(`agent uninstall`) removes launchd units, containers, networks, sockets, and
generated config, and says what it deliberately keeps (audit logs, keychain
items, offline bundles) and how to purge them; zero telemetry means no
analytics, update checks, crash reports, or usage pings leave the machine in
P1. Any future telemetry is separate, explicit opt-in only, and absent from
the launch demo path.
**SUCCESS GATE:** `make block14-gate` passes and release evidence shows two
volunteers (not you) each reach a running governed
agent in <15 minutes from the README on their own macOS machines after
installing Docker Desktop or Colima; the Hermes post-install deploy demo
completes in <10 minutes; at least one Block 13 differentiation demo is
recorded and embedded (two additional demos are stretch); `cosign verify-blob`
and `agent verify-release` are
documented-and-green on released artifacts; offline bundle verification is
documented-and-green; v0.1.0 tagged with goreleaser, all P1 CI gates (lint,
test, -race, fuzz corpus, e2e-network on macOS, Hermes integration conformance,
redteam-smoke 6/6, docs smoke) green on the tag.

---

## BLOCK 15 — Sequencing, founder calendar, and execution control
```
B1 → B2 → B3 ─┬→ B4 → B5 ─┬→ B6 → B7 → B8 ─┬→ B9 → B10 → B11 → B12 → B13 → B14
              │            │                 │
              └ B3 gates everything security-spine-first
```
B4/B5 can interleave with B6 SDK design once Block 1 contracts are frozen.
B10 dashboard can start once B9 events exist. B11 operator contract depends on
B1-B10 JSON/control surfaces. B12 red-team needs B5-B8 plus B11 operator
methods. B13 integrations depend on B11 and B12. B14 release/docs/demo closes
only after every P1 gate is green.

Founder calendar target (rough, week-by-week progress, not a relaxed gate):
- **Week 1:** B1-B3 green. Repo/protos/CI, daemon/CLI skeleton, identity and
  audit spine. Output: local CLI talks to daemon; audit hash chain and signed
  export skeleton exist.
- **Week 2:** B4-B8 green. Policy compiler, fenced macOS Docker
  Desktop/Colima runtime, harness/Python SDK, secrets broker, packaging.
  Output: a plain Python/LangGraph/CrewAI-generated project can pack into a
  signed artifact and run behind default-deny egress.
- **Week 3:** B9-B12 green. Trigger API/events/cron, dashboard, operator
  contract, redteam-smoke. Output: governed run is observable, machine-readable
  operator loop works, and 6/6 smoke proof is release-blocking.
- **Week 4:** B13-B14 green. Hermes plugin/skill plus install/docs/demo/release
  path. Output: post-install Hermes demo completes in <10 minutes; README
  clean-machine path works on macOS.
- **Week 5:** P1 release buffer only. Bug fixes, volunteer clean-machine
  verification, offline bundle verification, video/asciinema polish, tag
  v0.1.0. If Week 4 is clean, ship in Week 4; if not, Week 5 is the cap.

P2 calendar target after P1 (four additional weeks):
- **Week 6:** Linux certification track: Linux `dockerd`, systemd, libsecret,
  CI runners, seccomp/AppArmor profiles, deb/rpm packaging.
- **Week 7:** Customer-facing control-plane foundations: team/fleet model,
  hosted identity/audit abstractions, registry/promotion flow, tenant/project
  metadata, support bundle.
- **Week 8:** Commercial observability and opt-in telemetry: explicit consent
  UX, payload contract, privacy docs, fleet health, upgrade/error reporting,
  dashboard views for teams.
- **Week 9:** P2 customer release hardening: production docs, support playbook,
  design-partner onboarding, upgrade/rollback, pricing hooks, release
  candidate and customer-facing launch packet.

Execution control decisions:
- The plan is now 15 blocks: 14 product/release build blocks plus this founder
  calendar/control block. Former Block 10.5 is now Block 11.
- Once implementation starts, do not silently slip P1 blocks. If the calendar
  is impossible, stop and explicitly rescope before continuing.
- Block 13 P1 scope is Hermes-only: Hermes plugin/skill, contract parity gate,
  prompt-injection boundaries, and post-install <10-minute demo are required.
  Generic MCP server distribution plus Claude Code, Codex, and Cursor
  integrations move to P2.
- The extra Block 13 demo recordings beyond the minimum launch video are launch
  asset prioritization, not skipped product functionality.
- CrewAI is the Python multi-agent framework. P1 support means an AI-generated
  CrewAI project packs/runs through the generic Python harness and examples;
  AgentPaaS is not building a CrewAI authoring framework or custom orchestration
  layer.
- Node SDK remains explicitly deferred and is not part of the P1 gate.
- Audit, policy, network enforcement, secrets invisibility, packaging/signing,
  Hermes operator contract, redteam-smoke, and integration contract parity are
  never cut from P1.

**SUCCESS GATE:** `make block15-gate` passes once the Makefile exists, proving
the plan/checkpoint consistency checks still have no stale block numbering,
P1/P2 scope drift, missing gate commands, or cut security invariants. Before
Block 1 creates the Makefile, the equivalent docs-only gate is this whole-plan
review, `git diff --check`, and a committed checkpoint.

## 16. DEFINITION OF DONE (PHASE 1)
The execution plan is complete when PRD v4 §8 (Success Definition) items
1–8 are demonstrably true via the gates above. Blocks 1-14 are complete only
when their `make blockN-gate` wrappers exit 0, and Block 14 must also collect
the volunteer/release evidence named in PRD §8. Block 15 is the sequencing
control gate. "Done" is always a command plus recorded evidence, never a
judgment call.

**END OF EXECUTION PLAN v1.0 — companion to agentpaas-prd-v4-master.md**
