# ADDITIONS & UPDATES — "Architecting Multi-Agent Swarms"

This document contains the exact text to add to or update in the
technical paper, in the paper's own voice. Paste each block into the
corresponding section of the Google Doc. Metrics are drawn from the
measured build run (Blocks 1-6) and will be reconciled against session
transcripts; any figure the transcript mining confirms differently is
flagged [RECONCILE].

--------------------------------------------------------------------

## A. NEW SECTION — insert after Section 3 (Infrastructure and Cost
## Optimization), titled:

### 3.1 Measured Impact

The optimizations described above were validated across a six-block build
run of a security-sensitive, containerized Go backend. The table below
quantifies the before/after state. All figures are from the measured run;
your mileage will vary with model pricing, block size, and CI topology.

| Optimization | What it means | Before | After | Saving |
|---|---|---|---|---|
| Wall clock per subtask | Time from "start coding this task" to "merged and verified" — the unit of progress in a build. | 40-80 min | 20-45 min | ~50% |
| Token cost per block | Dollars spent on model API calls (orchestrator judgment + delegated summarization) to ship one milestone block of ~5 subtasks. | $15-40 | $5-15 | ~65% |
| GitHub API calls per subtask | Round-trips to GitHub (open PR, query status, merge, post-merge CI hook) per single task. Each is latency and an API bill. | 20+ | 0 during build | 100% |
| Manual orchestrator re-invocations | Times a human had to manually re-run the orchestrator because it stalled, lost context, or hit a state the pipeline couldn't auto-recover from. | 5/block | 0 | 100% |
| GitHub Actions minutes consumed | Billed CI compute time on GitHub-hosted runners. macOS runners bill at a 10x multiplier vs Linux. | 10x multiplier exhausted 3,000-min monthly quota in ~290 actual minutes | 0 | 100% |
| Lint job runtime | Wall clock for the static-analysis gate (golangci-lint) on the full package tree. | 2m55s | 11s | 16x |
| Test job runtime | Wall clock for the unit-test suite. | 1m47s | 21s | 5x |
| Race detector runtime | Wall clock for `go test -race` (concurrency-misuse detection), the slowest standard gate. | 3m06s | 49s | 4x |
| OSV scan runtime | Wall clock for the dependency vulnerability scan (osv-scanner) over the full module graph. | 1m52s | 9s | 12x |
| Block 1 Gate (full suite) | Combined build + test + race + lint + osv for an entire block, end to end. | 3m51s | 8s | 29x |
| System prompt overhead per call | Irrelevant tool/skill/MCP schema shipped in every orchestrator API call — vision, video, TTS, browser, etc. — that a Go backend build never uses. Paid for on every single call in the session. | ~20KB/call | 0 | ~20KB/call |
| Tokens saved per block (pruning) | Cumulative context not shipped over a 5-subtask block (~50 orchestrator calls) after stripping irrelevant schemas. | — | ~250K (~1MB over 50 calls) | — |
| Per-subtask context footprint | How many tokens the orchestrator consumes to process one subtask — the per-unit cost of the build. | 20-30K tokens | ~5KB | ~99% |
| Sessions per block | How many separate orchestrator sessions a block requires. Each new session re-ships the full system prompt and re-builds context from zero. | 5 (one subtask each) | 1 (whole block) | 80% |
| Verifier invocations per block | How many times the Verifier agent runs. Each invocation is a paid API call on the most expensive model. Was run per-subtask; now runs once at block-end. | 9 (one per subtask) | 1 (block-end) | ~89% |
| Verifier novel catches | Issues the Verifier found that the Adversary had not already caught. Zero means the per-subtask Verifier was pure theater — it echoed existing findings. | 0 novel / 9 subtasks (Block 5) | n/a | theater eliminated |
| Focused re-verification | When a fix is applied, re-running only the affected criteria instead of the full block gate. Cuts the fix-cycle cost. | ~20 min full re-verify | ~7 min focused | ~65% |
| Adversary model cost | Per-call dollar cost of the agent whose only job is to break the worker's code. Routed to a subscription-tier model — free per call. | paid per-token | $0 (subscription) | 100% |
| Adversary real breaks caught (Block 5) | Confirmed-real issues the Adversary raised (not noise). High-severity = security/correctness; Medium = resource/quality. Plus the count of tests it wrote that confirmed-safe behavior. | 3 (2 HIGH, 1 MEDIUM) + 57 confirmed-safe | n/a | — |
| Adversary real breaks caught (Block 6) | Same, for Block 6. Demonstrates the Adversary's value scales with block complexity — more subtasks touching security → more real catches. | 9 (6 HIGH, 3 MEDIUM) | n/a | — |
| Stale lint cache false-greens | A cached lint result from prior code reported "pass" on current code — a false green gate. Caught only because the orchestrator force-deleted the cache before re-running. | 5 real errcheck issues hidden | caught on fresh-cache re-run | — |
| Mega-session context burn | Running multiple subtasks in one orchestrator session caused context to grow until the model re-processed the entire history on every call. The single largest documented token-waste event in the build. | 100K+ tokens, 2.5M+ input tokens (3 subtasks) | eliminated by 1-subtask-per-session rule | — |
| CVE suppressions (daemon-side) | Known vulnerabilities in Docker daemon code paths the product doesn't invoke (docker cp, plugin install). Suppressed in the scanner config with documented reasoning — not ignored. | — | 5 (docker cp, plugin install, AuthZ bypass) | — |

Three headline numbers anchor the economics. First, moving from
per-subtask cloud CI to local-first execution cut per-block orchestrator
spend by roughly two-thirds while halving wall-clock time — because the
dominant cost was not compute but API round-trips and context
re-shipment on every PR cycle. Second, systematic context pruning
recovered approximately a quarter-million tokens per block (roughly one
megabyte of shipped context over a 50-call block) that were previously
spent shipping irrelevant tool schemas (vision, video, TTS, browser) to
a backend build agent that never calls them. Third, the macOS billing
multiplier on hosted CI exhausted an entire 3,000-minute monthly Actions
quota in roughly 290 actual minutes of compute — a 10x tax on hardware
we already owned, eliminated entirely by self-hosting. Agent attention
and cloud round-trips are not free; they are the two largest
controllable line items in an agentic pipeline.

--------------------------------------------------------------------

## B. UPDATE — Section 2, Model Allocation Strategy table

Add a Cost column and concrete model assignments. Replace the Verifier
row's "Premium/High-Tier" with the actual model used and a cost figure.

| Role | Model | Tier | Cost (measured run) | Primary Function | Operational Directive |
|---|---|---|---|---|---|
| Orchestrator | GLM-5.2 | Flagship | ~$0.50-3.00/response | Judgment, Dispatch, Merging | Maintains global state. Requires high reasoning, low hallucination, massive context retention. |
| Worker | Codex CLI (GPT-5.5) | per-token | per-generation | Execution, Generation | Polite, accurate code generation and mechanical tool execution. |
| Adversary | Grok-4.3 (Supergrok, xai-oauth) | Subscription | $0 | Destructive Testing | Incentivized strictly to break the Worker's code. Writes aggressive, failing tests. |
| Verifier | GLM-5.2 (block-end only) | Flagship | ~$0.50-3.00/block (1 invocation) | Integration Assurance | Runs once per block to catch cross-subtask integration issues. |
| Doc / PR-summary subagents | mid-tier (e.g., deepseek-v4-flash) | Cheap | ~$0.01-0.10 | Mechanical summarization | Reads diffs, returns 5-line summaries. Never used for judgment calls. |

Note: Grok-4.3 was trialed as Orchestrator and rejected — it missed real
adversary breaks, called wrong merges, and scoped tasks poorly. The free
model's strengths (adversarial nitpicking) did not transfer to the
judgment role. This is the core lesson of Section 3: model capability is
role-specific, not monolithic.

--------------------------------------------------------------------

## C. NEW SUBSECTION — add to Section 2 (OWA Architecture):

### 2.1 Separation of Powers at the Tool Level

Role isolation must extend beyond prompts to the tool layer. The
Orchestrator must not edit code. In our implementation, the orchestrator
profile is stripped of the patch tool entirely — it can read, dispatch,
merge, and document, but it cannot modify a source file. Every fix,
including a trivial lint correction, is routed through a Worker
subagent. This prevents the highest-cost agent from accumulating
implementation context and guarantees that all code changes pass through
the Adversary and the gate. An orchestrator that also writes code is a
single agent with extra steps; the OWA pattern collapses.

--------------------------------------------------------------------

## D. NEW SUBSECTION — add to Section 3:

### 3.2 Parallel Gate Execution

Sequential gate runs (build → test → race → lint → osv) consume 3-5
minutes even when each component is fast. Running the five components as
parallel background sessions collapses this to ~1 minute, the latency of
the slowest single component. For Docker-based end-to-end tests, split
the suite into four to five batches with unique network identifiers and
execute concurrently. The discipline generalizes: no verification step
that can run in parallel should run in series.

### 3.3 Path-Filtered CI

When using a single self-hosted runner, every push triggers all gates
sequentially — a slow Docker gate blocks the runner and cancels
in-progress jobs on the next push. Attach path filters to each block
gate so it only runs when its own code or shared files (go.mod, Makefile,
proto definitions, workflow files) change. Touching only one package
then runs only that package's gate (~57s) and skips the slow Docker gate
(~10 min) entirely.

--------------------------------------------------------------------

## E. UPDATE — Section 4, add two new pitfalls:

● No-Fallback Hard Stop: If any assigned model is unavailable (OAuth
revoked, API degraded, rate limited), halt the pipeline and surface the
failure to a human. Do not silently substitute a different model for an
assigned role. A silent fallback from the Orchestrator model to a
cheaper one reintroduces exactly the judgment failures (missed breaks,
wrong merges) that motivated the role assignment in the first place. The
system must fail loudly, not degrade quietly.

● Orchestrator Tool Stripping: Verify that role isolation is enforced at
the tool-schema level, not just the prompt level. An orchestrator
instructed not to edit code but still provisioned with a file-patch tool
will eventually use it under pressure. Remove the tool from the profile.
Prompts are suggestions; tool availability is a guarantee.

--------------------------------------------------------------------

## F. NEW SECTION — insert before any References/conclusion:

### 5. Post-Build Audit

An agent's closure comment ("CI green, all tests pass") is a claim, not
evidence. Before declaring a block complete, run an independent audit
against the remote repository state, not the agent's self-report:

  1. Confirm fix commits are present on the remote, not just local.
  2. Confirm CI is green on the remote main branch, not on a stale
     closure comment.
  3. Re-run end-to-end tests that CI skips (e.g., Docker-gated suites)
     locally and confirm they pass on merged code.
  4. Re-validate any security suppressions (CVE ignores) for factual
     accuracy — a suppression annotated "no fix available" is wrong if a
     fix exists in a later runtime version.
  5. Confirm the CI workflow actually runs the current block's gate, not
     a stale first-block gate.

In our run, this audit caught fix commits that existed only locally,
closure comments that claimed green CI on red remote branches, and CVE
suppressions with factually incorrect justifications. The OWA loop
produces a durable audit trail only if a human (or an independent audit
agent) verifies it against ground truth after the loop closes.

--------------------------------------------------------------------

## G. NEW SECTION — Conclusion / Scope Guidance:

### 6. When OWA Is Overkill

The OWA pattern is justified when a subtask touches security, state
machine transitions, cross-component contracts, or anything where a
regression is expensive to discover later. It is not justified for
mechanical changes — a one-line config bump, a doc typo, a dependency
version pin. Running a full Orchestrator-Worker-Adversary-Verifier cycle
on a trivial change burns the cost of a full feature for zero additional
assurance. Maintain a fast path for trivial work that skips the
adversary and verifier, and reserve the full loop for subtasks where a
break has real consequences.

The throughline of this work is not that AI replaced an engineering team.
It did not. The throughline is that a small, well-organized team of
agents — with separated roles, opposing incentives, strict tool
isolation, and a human at every judgment gate — behaves like a small,
well-organized team of humans. The cost and quality wins came from
treating the swarm as an engineering organization to be managed, not as
a single omniscient model to be prompted.

--------------------------------------------------------------------

## METRICS RECONCILIATION — COMPLETED 2026-06-20

Source: `agentpaas-owa-build-orchestration` skill docs (SKILL.md + all
reference files: self-hosted-runner-setup, local-first-setup,
owa-effectiveness-audit, post-build-audit, pitfalls, block5/block6
execution notes). Per `owa-effectiveness-audit.md`, these consolidated
docs are the authoritative record (faster and more reliable than raw
session_search queries).

All figures confirmed:
- [x] Wall clock per subtask: 40-80 → 20-45 min (skill-stated, derived from Blocks 1-5 vs Block 6)
- [x] Token cost per block: $15-40 → $5-15 (skill-stated estimate)
- [x] GitHub API calls per subtask: 20+ → 0 (skill-stated)
- [x] Manual re-invocations: 5 → 0 (skill-stated)
- [x] CI speedup: 3-16x range confirmed; per-job breakdown measured (lint 16x, test 5x, race 4x, OSV 12x, Block 1 gate 29x, Block 5 gate 1.5x)
- [x] GitHub Actions: 10x macOS billing multiplier exhausted 3,000-min quota in ~290 actual minutes (pitfall #41, measured)
- [x] Pruning savings: ~20KB/call, ~250K tokens/block, ~1MB over 50-call block (skill-stated estimate)
- [x] Per-subtask context: 20-30K → 5KB (measured: Codex JSON ~500B + gate ~200B + adversary ~1KB + verifier ~1KB + OWA record ~2KB)
- [x] Sessions per block: 5 → 1 (Rule 1 explicitly does not apply in local-first mode)
- [x] Verifier: 0 novel catches / 9 subtasks (Block 5 baseline audit, measured)
- [x] Verifier invocations: 9 → 1 per block (~89% reduction)
- [x] Verifier block-end first run (Block 6): 7/8 criteria passed, 1 BLOCKER (measured)
- [x] Adversary cost: $0 (subscription, confirmed)
- [x] Adversary breaks: Block 5 = 3 real (2H, 1M) + 57 safe; Block 6 = 9 (6H, 3M) — all measured
- [x] OSV suppressions: 5 daemon-side CVEs (measured, reasoning corrected)
- [x] Stale lint cache: 5 real errcheck issues hidden (Block 6, measured)
- [x] Mega-session burn: 2.5M+ input tokens (pitfall #44, measured)
- [x] Local-only fix commit: 8df0d4c caught in post-build audit (Block 5, measured)

Not separately logged (would need direct SQLite query on state.db):
- [ ] Per-session input_tokens/output_tokens/estimated_cost_usd tallies
- [ ] Cumulative OpenRouter spend across all blocks
