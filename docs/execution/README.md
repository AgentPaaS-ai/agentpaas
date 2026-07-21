# AgentPaaS Execution Documentation

**Current shipped release:** v0.2.3
**Next planned prerelease:** v0.3.0-alpha.1 after B30
**Next planned stable release:** v0.3.0 — Agent Registry and Secure Delegation
after B32
**Current implementation state:** B1–B25 shipped; B26–B29 implemented on
the development branch; B30–B41 are execution-ready; B30 is next (T00 first:
synthetic MCP fail-closed closure).

This directory contains the detailed implementation records and block
contracts for AgentPaaS.

## Current source-of-truth order

Read these in order before executing v0.3–v0.5 release-train work:

1. [`Agentpaas-pitch.md`](../../Agentpaas-pitch.md) — product pitch, scope,
   approved decisions, and explicit deferred work.
2. [`docs/roadmap.md`](../roadmap.md) — shipped baseline, release scopes,
   dependencies, release contracts, and acceptance matrix.
3. The current block under [`blocks/`](blocks/) — binding task-level
   execution instructions, tests, gates, handoff, and pitfalls.
4. Every direct dependency block named in the current header — contracts and
   state that the current block must consume.
5. Existing code, tests, and release workflows — implementation reality.

When these disagree:

- Do not silently choose one.
- Preserve shipped v0.2.3 behavior.
- Treat the pitch/decision register and roadmap as the approved product
  boundary.
- Record the conflict and request a decision if resolving it would materially
  change scope or authority.

Historical PRDs, session logs, and superseded plans are useful context but are
not allowed to broaden the approved release scope.

## v0.3–v0.5 execution and release sequence

| Block | Release | Status | Outcome |
|---|---|---|---|
| [B26](blocks/b26-summary.md) | v0.3 | Implemented | Immutable deployment/alias, direct invocation/control, run/workflow/service/handoff/child contracts, protected local state |
| [B27](blocks/b27-summary.md) | v0.3 | Implemented | `agent.progress(...)`, authenticated progress/checkpoint journal, bounded artifacts, safe resume |
|| [B28](blocks/b28-summary.md) | v0.3 | Complete | Portable ports, Docker/Kubernetes conformance (5/10 steps), tenant proof, substrate decision: Kubernetes (Cloudflare rejected per D67) |
|| [B29](blocks/b29-summary.md) | v0.3 | Complete | Runtime profiles, real durable streaming/events/cursors, interactive waits, activation classes, efficiency baseline |
|| [B30](blocks/b30-summary.md) | v0.3 alpha | Execution-ready / prerelease | Direct durable invocation and long-run proof; close `v0.3.0-alpha.1`. T00 (P0, first): close the synthetic MCP success fallback — no-router managed MCP calls fail with a typed not-enabled error in production (2026-07-19 audit, risk R5) |
|| [B31](blocks/b31-summary.md) | v0.3 | Execution-ready (reduced per Fix 3) | Local package registry read API + promotion bit over installed/deployment stores; lifecycle/attestations/Agent Cards/schema matching deferred post-v0.5 |
| [B32](blocks/b32-summary.md) | v0.3 stable | Execution-ready / release | Logical A2A tasks/events, two-sided policy, encrypted artifacts, event completion; close `v0.3.0` |
| [B33](blocks/b33-summary.md) | v0.4 | Execution-ready | Real AgentPaaS-container MCP router over catalog/runtime directory, no synthetic success |
| [B34](blocks/b34-summary.md) | v0.4 | Execution-ready | Linear separate-container pipelines, atomic durable handoffs/artifacts, no Hermes relay |
| [B35](blocks/b35-summary.md) | v0.4 stable | Execution-ready / release | Bounded leaf-child spawn/join and parent collation; close `v0.4.0` |
| [B36](blocks/b36-summary.md) | v0.5 | Execution-ready | Model catalog, route compiler, profile eligibility, deterministic explainable selector |
| [B37](blocks/b37-summary.md) | v0.5 | Execution-ready | Failure normalization, exact replay, one automatic model recovery, streaming-safe failure semantics |
| [B38](blocks/b38-summary.md) | v0.5 | Execution-ready | One workflow LLM spend/token ledger and streaming reservation/reconciliation |
| [B39](blocks/b39-summary.md) | v0.5 | Execution-ready | Integrated leases, activation, event waits, guardrails, lifecycle controls, continuation, amendments |
| [B40](blocks/b40-summary.md) | v0.5 RC | Execution-ready / prerelease | Complete Hermes UX; close `v0.5.0-rc.1` |
| [B41](blocks/b41-summary.md) | v0.5 stable | Execution-ready / release | RR-01–RR-31 Golden proof, hardening, clean install, and stable `v0.5.0` release |

Execute blocks in dependency order: B26 → B27 → B28 → B29 → B30 → B31 → B32
→ B33 → B34 → B35 → B36 → B37 → B38 → B39 → B40 → B41. Shared harness,
daemon, policy, protobuf, and Makefile integration must have one owner and be
serialized. Release closure occurs only at B30, B32, B35, B40, and B41. No
model-router block starts until the B28–B35 runtime/discovery/coordination
gates pass.

## Cumulative v0.3–v0.5 product boundary

The train exposes coherent usable boundaries rather than waiting for one
big-bang release:

| Stable version | Boundary |
|---|---|
| `v0.3.0` | Durable runtime, streaming/events/waits, activation, long-running invocation, private catalog, constrained capability resolution, secure A2A, and encrypted artifacts |
| `v0.4.0` | v0.3 plus governed MCP services, linear pipelines, and bounded parent/child workflows |
| `v0.5.0` | v0.4 plus deterministic routing/recovery, shared spend, integrated supervision/control, Hermes UX, and cumulative proof |

Cumulatively by v0.5, the product includes:

- Substrate-neutral signed packages and portable runtime/state/event/artifact/
  gateway/identity/secret/metering ports, proven on Docker plus a bounded
  local-Kubernetes slice (Cloudflare rejected per D67). Docker alone ships in
  v0.3.
- Versioned runtime profiles and admission-time compatibility for multi-role,
  structured, tool, reasoning, streaming, interactive, multimodal, and
  concurrency features.
- Backward-compatible buffered calls plus real durable model/invocation
  streaming, cursor replay, backpressure, safe partial-output handling, and
  one canonical terminal result.
- Durable inbox/wait/wakeup without polling, tmux, or an open observer.
- Tenant-private signed agent/MCP catalog, separate runtime directory, Hermes
  registration proposals, authorized promotion, constrained deterministic
  capability queries, and exact package/profile pins.
- A2A-compatible logical agent tasks/events, MCP tool/service calls, matching
  sender-egress/receiver-ingress policy, and encrypted brokered artifacts.
- Explicit `on_demand`, `warm`, and `resident` activation with ordinary agents
  scaling to zero and p50/p95/p99 platform overhead separated from provider/
  tool latency.
- Long-running multi-turn execution with no hidden platform lifetime ceiling,
  but explicit operator-approved active-time, cancellation, and pause controls.
- Immutable deployment versions and audited aliases, independently invokable
  through CLI/API by cron, Kubernetes, or another process with Hermes absent.
- Durable invocation idempotency, default-one top-level workflow concurrency per
  deployment, and visible overlap rejection without a hidden queue.
- AgentPaaS-managed MCP services in separate governed containers.
- Runtime-native linear pipelines with bounded durable handoffs and no Hermes
  data relay.
- Bounded parent/leaf-child fan-out/fan-in with parent-owned collation.
- Logical model routing.
- Aggregator candidates constrained to one model and a signed upstream
  allowlist.
- One bounded automatic model-call recovery per worker attempt.
- Multi-turn progress, checkpoint, artifact, and resume contract.
- One shared hard LLM spend/token limit across all workflow calls, model-using
  services, stages, parents, children, and attempts.
- Worker leases, stalls, accumulated active-time, loop guardrails, and fencing.
- One operator-directed failure continuation for an eligible standalone,
  pipeline-stage, or parent run; leaf children have call-level recovery only.
- Immediate cancel, safe-boundary pause/resume, provenance-linked new-run
  restart, and scoped append-only amendments to maximum active duration,
  current-attempt lease, and LLM-spend ceiling before terminal exhaustion.
- Deterministic `local-first` and `cloud-cost-first` patterns.
- Simple operational and cost evidence.
- Golden fixtures for weather, multi-turn reasoning/tool use, interactive
  streaming, MCP, pipeline, and orchestrator/worker/verifier/testing roles.

The cumulative v0.5 release does not include:

- A correctness verifier or LLM judge.
- Learned/predictive routing.
- Automatic context compaction or task decomposition.
- General DAGs/cycles/compensation, recursive children, pipeline-plus-child
  composition, or partial child-batch success.
- Multiple fallback cascades.
- Automatic OAuth reauthentication.
- Generic third-party runtime-orchestrator adapters; direct AgentPaaS
  deployment invocation is supported.
- Unconstrained registry federation, recursive swarms, direct machine/pod/
  session addressing, shared writable inter-agent mounts, correctness polling,
  or treating A2A/MCP as a second scheduler/trust authority.
- Production Kubernetes managed service, billing, or fleet control;
  v0.3 proves portability and records the Kubernetes substrate decision (D67)
  without shipping it.
- Destructive purge/retention automation.
- The superseded Codex Approval Pack or enterprise workflow mega-block.

Hermes is the required release-train user-facing authoring/testing/packaging/deployment
environment and an optional later lifecycle/inspection client. Hermes uses
AgentPaaS skills and the AgentPaaS deployment API; AgentPaaS then operates the
artifact independently. The runtime remains the authority for exact model
selection, catalog promotion/resolution, policy, credentials, communication
edges, events/artifacts, current time/spend ceilings, attempts, services,
stage transitions, child scheduling, handoffs, activation, and fencing. Hermes
is neither a runtime dependency nor the message bus.

## Execution discipline

For every task inside a block:

1. Read the entire block and its direct dependencies.
2. Confirm prior block gates and handoff records.
3. Add failing contract tests before implementation.
4. Implement the smallest behavior that satisfies the approved contract.
5. Run focused unit, race, integration, and adversary tests.
6. Re-run the cumulative block gate.
7. Record exact commands, outputs, skipped manual gates, risks, and the next
   unblocked task.

Do not:

- Change a golden expectation merely to match a regression.
- Convert unavailable live/manual evidence into PASS.
- Use live model prose as a correctness oracle.
- Add a provider, endpoint, credential, time/spend authority, or cloud
  permission outside signed policy or the explicit administrative amendment
  contract.
- Claim savings, globally optimal routing, or verified correctness.
- Publish outside the approved B30/B32/B35/B40/B41 release checkpoints.

## Approval checkpoints

Three evidence-backed decision classes remain open through B41:

1. The real local/cloud model roster used in public defaults and examples,
   including its price-validity/freshness policy.
2. The final model-call timeout, stall timeout, attempt lease, maximum active
   duration, and recovery margin.
3. Any public latency SLO, managed activation guarantee, or default warm-pool
   policy after cold/cached/warm/resident baselines exist. Every release may
   ship with truthful measured evidence and no latency SLA.

B41 must produce live comparison/calibration evidence and stop for explicit
founder approval before publishing those defaults or claims.

Each stable closure block—B32, B35, and B41—must stop before creating or
pushing its exact tag. B30 and B40 must do the same for their prerelease tags.
Approval of the plan or an earlier block is not release authorization.

Every stable release closure must include cumulative compatibility/migration
proof, a clean install and upgrade, signed checksummed SBOM-bearing artifacts,
stable Homebrew verification, truth-synced docs and known limitations, one
reproducible quickstart/demo, zero open P0/P1 defects, explicit approval for
the exact commit, and post-publish public verification. Patch releases are
reserved for compatible fixes and security updates.

## Block completion minimum

Each block defines its exact cumulative gate. At minimum, run applicable:

```text
go build ./...
go test ./... -count=1
go test -race ./...
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
osv-scanner scan -r .
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
make golden-fast
```

Docker, egress, red-team, slow golden, live-provider, clean-machine, and
release gates run where the block requires them. A task is not complete when a
required gate is merely documented for later.

## Directory guide

```text
docs/execution/
├── README.md
├── blocks/
│   ├── b16-summary.md ... b25-summary.md   shipped implementation records
│   ├── b26-summary.md ... b27-summary.md   implemented v0.3 foundations
│   └── b28-summary.md ... b41-summary.md   active v0.3–v0.5 train plans
├── planning/                               historical/master PRDs and plans
├── prompts/
│   └── block-startup-prompt.md             orchestrator startup template
├── reference/                              operational and E2E references
├── golden-loop-test.md                     public release lifecycle gate
├── resume-prompts/                         historical session handoffs
└── archive/                                retired build records
```

Relevant shipped references:

- [`planning/prd-v4-master.md`](planning/prd-v4-master.md)
- [`planning/execution-plan-v1.md`](planning/execution-plan-v1.md)
- [`golden-loop-test.md`](golden-loop-test.md)
- [`reference/credential-onboarding.md`](reference/credential-onboarding.md)
- [`reference/e2e-test-plan.md`](reference/e2e-test-plan.md)

The old B26 “Codex portability/Approval Pack” and B27 enterprise OAuth/MCP
mega-block are superseded. Ideas from them remain later-roadmap possibilities, not
hidden prerequisites.

## Starting a new execution session

1. Confirm the repository branch, worktree status, and current tag.
2. Read the pitch/decision register, roadmap, and target block end to end.
3. Inspect current code/tests named under the block’s “Likely files.”
4. Run the prior block gate before changing code.
5. Create a task-scoped branch/worktree using the repository’s current
   collaboration convention.
6. Start with T01 and preserve task order unless the block explicitly permits
   parallel work.
7. Use the block’s handoff format after every task.

For the release train, B26 and B27 are complete and B28 is the next executable block. Do
not begin B29–B41 by reconstructing or bypassing the B26–B28 contracts.
