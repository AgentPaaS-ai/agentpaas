# Blocks B6–B21 — Consolidated Historical Summary

Compact record of completed foundation blocks. Individual B6–B21 summaries were
retired; B22+ summaries remain under `docs/execution/blocks/`.

---

## B6 — Agent Harness & Python SDK
**Gate:** `make block6-gate` — PASS  
Built the in-container harness HTTP lifecycle (`/healthz`, `/readyz`, `/invoke`),
per-invoke wall/iteration/token budget enforcement, Linux PID-1 process reaper,
Python SDK core with RPC back-channel, and structured failure context with
redaction.  
**Key packages:** `internal/harness/`, `python/agentpaas_sdk/`  
**Adversary:** 9 breaks found and fixed across T01/T02/T05.

## B7 — Secrets Brokering & MCP Lifecycle
**Gate:** `make block7-gate` — PASS (2026-06-21)  
SecretStore + Keychain lifecycle, brokered gateway credential flow, invisibility
suite (agent never sees raw secrets), direct-lease compatibility, revocation
hooks. MCP track (B7M): resource model, process/sidecar supervision, gateway
routing, egress policy, status reporting, tool audit + host-affecting capability
guard.  
**Key packages:** `internal/secrets/`, `internal/mcpmanager/`

## B8 — Packaging Pipeline (`agent pack`)
**Gate:** `make block8-gate` — PASS  
Project detect/init scaffold, BuildKit image assembly, fail-closed secret scan
and build-context control, SBOM + cosign signing + `agent.lock`, immutable
prompt/config deploy path (TOCTOU fix), OSV advisory reporting and local OCI
repair.  
**Key packages:** `internal/pack/`

## B9 — Trigger API, Events, Webhooks, Cron
**Gate:** `make block9-gate` — PASS  
Trigger gRPC (:7718) + REST (:7717), API-key lifecycle, durable idempotency and
payload limits, SSE event bus (auth on stream), webhook delivery, cron triggers,
local handoff, CancelRun semantics, REST/JSON fuzzing.  
**Key packages:** `internal/trigger/`

## B10 — Observability Dashboard
**Gate:** `make block10-gate` — PASS  
OTLP collector + SQLite store, embedded SPA shell, run timeline + live SSE, log
viewer redaction/XSS defense, policy diff + audit export UI, cost/budget
display, perf/accessibility gate.  
**Key packages:** `internal/otel/`, `internal/dashboard/`

## B11 — Hermes Operator Contract
**Gate:** `make block11-gate` — PASS  
Operator schemas and error categories, CLI JSON parity, noninteractive
validate/init, explain-failure + next-action, policy patch proposal with
confirmation boundary, prompt-injection and path-boundary tests, 14-step Hermes
golden flow.  
**Key packages:** `internal/operator/`, `internal/daemon/operator_handlers.go`,
`internal/cli/`

## B12 — P1 Red-Team Smoke Gate
**Gate:** `make block12-gate` — PASS  
Red-team runner + report format; fixtures for default-deny egress, gateway/
credential misuse, brokered secret invisibility, host access/resource
containment, operator prompt-injection.  
**Key packages:** `test/redteam/`

## B13 — Hermes Integration Plugin
**Gate:** `make block13-gate` — PASS (2026-06-25)  
Hermes plugin skeleton (18 tools), reconcile-from-source, policy init templates,
e2e deploy acceptance (daemon auto-invoke via docker exec), slash commands,
SKILL.md, demo agents. Closed the deploy→invoke gap (BUG 7d).  
**Key packages:** `integrations/hermes-plugin/`, `internal/runtime/` (Exec)

## B14 — Security Remediation, Gateway, Policy, Release
**Gate:** `make block14-gate` — PASS (2026-06-26)  
Run status + orphan reconciliation + invoke/stop sync; plugin path allow-list,
binary verification, DLP, anti-fabrication, audit genesis checks; gateway
container in Run path with real-time egress; install docs, Homebrew formula,
release workflows.  
**Key packages:** `internal/runtime/`, `internal/daemon/`, `internal/policy/`,
`Formula/agentpaas.rb`

## B15 — P1 Completion (Pre-Release Gap Closure)
**Gate:** `make block15-gate` — PASS (2026-07-02) → v0.1.0 feature-complete  
Credential onboarding CLI, `agent.llm()` over unified gateway egress, policy
authoring + pack-time validation, full trigger/cron/event plugin surface (29
tools), production hardening (RFC1918 tightening, Rekor retry, checkpoint key
encryption, capset drop), goreleaser v2.  
**Key packages:** `internal/llm/`, `internal/cli/`, `internal/secrets/`

## B16 — Manual Testing & Bug Fixes
**Status:** COMPLETE (2026-07-03–04); v0.1.0 tag moved  
Full lifecycle manual test (plugin→onboard→policy→pack→run→invoke→audit). P0
fixes: embed Python SDK in binary, pack timeout, ExecWithStdin framing, surface
Docker build errors. Security/UX: xAI name fix, provider-scoped egress, secret
whitespace trim, OpenRouter/Nous providers, actionable LLM errors.  
**Key files:** `sdkembed.go`, `internal/llm/`, `internal/runtime/docker.go`

## B17 — LLM Egress Auto-Derivation + Secure Secret Ingestion
**Status:** COMPLETE (2026-07-05)  
Pack-time auto-derive LLM egress from `agent.yaml` provider; fail/warn if
provider domain missing from policy. Secret ingestion moved to terminal
`agentpaas secret add` (stdin) so keys never enter Hermes/LLM tool-call context.
SKILL.md / SOUL.md / golden G06 updated.  
**Key packages:** `internal/pack/llm_egress.go`, `integrations/hermes-plugin/`

## B18 — Rigorous Manual Test Plan (First-Time User)
**Status:** COMPLETE — shipped before v0.2.3  
Clean T1–T10 first-time-user simulation on agentpaas-test profile. Fixed
policy-already-exists friction, SDK-not-on-PyPI scaffold guidance, run-target
resolution vs project basename, weather demo real-HTTP pattern, brew binary lag
mitigation.  
**Key outcomes:** manual-testing discipline; docs/SKILL truth fixes

## B19 — AgentGateway Policy Integration
**Status:** COMPLETE — shipped before v0.2.3 (after B20)  
Wired agentgateway-native governance: credential injection at gateway, LLM token
budgets, rate limiting, provider locking, ingress auth, egress OAuth hooks,
guardrails, transformations, timeouts/retry, observability, MCP tool
allow/deny. Compiler emits gateway config; harness path remains fallback.  
**Key packages:** `internal/policy/compiler.go`, `internal/policy/schema.go`

## B20 — Security Claim Closure
**Status:** COMPLETE — ran before B19; v0.2.3 baseline  
Closed claim/implementation gaps: credentials never on agent payload/env/stdin;
runtime verifies signed lock/policy before Docker resources; full egress
method/port/credential semantics; harness audit ingested into daemon chain;
fail-closed invalid input / missing secrets / fake LLM in production; README
red-team evidence; honest scoping of firewall as defense-in-depth.  
**Key packages:** `internal/daemon/`, `internal/runtime/`, `internal/harness/`,
`internal/secrets/`, `test/redteam/`

## B21 — Publisher Identity, Trust Store, Provenance Schema
**Status:** COMPLETE — shipped in v0.2.x (2026-07-06)  
Publisher keypair lifecycle (distinct from package AIDs), trust store with TOFU
pinning, lock schema v2 (publisher block + signature + provenance array),
provenance verify library, pack signs v2 when identity exists, fingerprint
display. No bundle/install yet (B22/B23).  
**Key packages:** `internal/identity/`, `internal/trust/`, `internal/pack/lock.go`,
`internal/pack/provenance.go`

---

## After B21
- **B22–B25:** bundle format, verified install/consent, fork/provenance chains,
  Hermes sharing UX → **v0.2.x shipped**
- **B26–B30:** durable contracts, progress/checkpoints, portability, profiles/
  streaming, long-run supervisor → **implemented on development branch**
- See per-block summaries `b22-summary.md` … `b41-summary.md` and
  `docs/execution/current-state.md` for forward context.
