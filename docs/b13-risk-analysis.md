# Block 13 — In-Depth Risk Analysis

**Date:** 2026-06-25
**Purpose:** Identify shortcuts, broken items, production risks, and must-fix-before-Block-14 issues.

---

## 1. SHORTCUTS AND MOCKS

### 1.1 No Gateway Container — Egress "Denial" Is Network Isolation, Not Policy (HIGH RISK)

**What we built:** The Run handler creates an internal-only Docker network
(no default route) and attaches the agent container to it. There is NO
gateway container, NO agentgateway (Linux Foundation Rust gateway), and NO
policy enforcement layer.

**What the spec requires:** The PRD v4 locks egress enforcement as
topological — the agent container on `internal: true` network, the gateway
dual-homed (internal + egress networks), DNS only via gateway stub. Policy
is enforced by the gateway, not by network isolation alone.

**What actually happens:** When the agent calls `agent.http("GET",
"https://example.com")`, the HTTP request fails with a DNS error
(`dial tcp: lookup example.com on 127.0.0.11:53: server misbehaving`).
The harness records this as `egress_denied` with reason "http request
failed." But this is NOT a policy denial — it's network isolation. The
audit event says "denied" but it's really "unreachable."

**Why it matters:** The wedge is "policy-controlled, fully audited." If
the "denial" is just a dead network, there's no policy decision to audit.
A real buyer testing the product would notice that policy.yaml exists but
has no runtime effect. The demo shows "egress denied" but cannot show
"policy allowed this call but denied that one."

**Impact:** This is the single biggest gap between the product vision and
the implementation. The gateway container + agentgateway integration was
built in Block 5 topology tests (the e2e-network tests create dual-homed
gateways), but the daemon's Run handler doesn't use it.

**Fix:** This is Block 14 scope (14B real-time egress timeline depends on
gateway topology). But the Run handler should at minimum create a gateway
container on the egress network and connect the agent through it.

---

### 1.2 Fake LLM Handler — `agent.llm()` Returns a Hardcoded String (KNOWN GAP)

**What we built:** The harness `handleLLM` returns:
```
"text": "agentpaas fake llm response"
```
No real LLM API call is made. Token counting works (for budget), but the
response is meaningless.

**Impact:** Demo 3 (repair-loop) calls `agent.llm("Analyze this error...")`
and gets back "agentpaas fake llm response." The demo works mechanically
(the flow completes, audit events fire), but the LLM output is fake. A
real agent would need actual LLM access.

**Verdict:** Acceptable for P1. The harness is designed to broker LLM
calls (through the gateway, with credentials), but the actual LLM
provider integration is a later concern. The budget tracking and audit
plumbing are what matter for B13. Document clearly.

---

### 1.3 block13-gate Has No Docker E2E (MEDIUM RISK)

**What we built:** The gate runs unit tests (with mock Docker driver),
checks file existence, and validates Python syntax. It does NOT run the
real pack→run→invoke→stop→audit flow with Docker.

**What this means:** The "e2e governance verified" message in the gate is
misleading. We verified it manually (I ran it live during this session
and got a real `egress_denied` event), but CI won't catch a regression
in the Docker flow.

**The e2e test that exists:** `TestStop_IngestsHarnessAudit` writes a
fixture JSONL file to the harness audit path, then calls Stop, and
verifies the daemon ingests it. This tests the ingestion logic but uses a
mock Docker driver (`NewDockerRuntimeWithDriver`) — no real container,
no real harness, no real egress attempt.

**Fix:** Add a `AGENTPAAS_DOCKER_TESTS=1` gated e2e test that does the
full flow. This should be in block13-gate when Docker is available.

---

### 1.4 Invoke Result Discarded — No Run Status Tracking (BUG)

**What we built:** `invokeAgent` runs the POST to `/invoke`, captures
stdout/stderr/exitCode, then does `_ = stdout`. The result is discarded.
There is no run status field — every Stop publishes
`EventRunSucceeded`, even if the agent crashed or the invoke failed.

**What should happen:** The daemon should track run status (pending →
running → succeeded/failed). If invoke fails, the run should be marked
failed. The Stop handler should check the actual outcome.

**Impact:** The dashboard and audit always show "succeeded" regardless of
what happened. A buyer seeing all-green status for a crashed agent would
lose trust.

---

### 1.5 Auto-Invoke Uses Detached Context — Orphan Risk (BUG)

**What we built:** The invoke goroutine uses
`context.WithTimeout(context.Background(), 2*time.Minute)`. This context
is NOT tied to the run lifecycle or the daemon lifecycle.

**Race condition:** If `Stop()` is called while `invokeAgent()` is polling
`/readyz`, the Stop removes the container. The next `rt.Exec()` call in
the invoke goroutine fails (container gone). The error is logged but the
goroutine may have already exec'd into a removed container.

More critically: if the daemon process is killed (not gracefully stopped),
the invoke goroutine dies but the container keeps running as an orphan.
There is no reconciliation or cleanup of orphaned containers on daemon
restart.

**Impact:** Orphaned containers accumulate. On daemon restart, they're
invisible to the new daemon (the `s.runs` map is in-memory, not
persisted).

---

## 2. BROKEN OR INCOMPLETE ITEMS

### 2.1 `stubControlServer` Name Is Misleading (CODE HYGIENE)

The production daemon's control handlers are on a type called
`stubControlServer`. The "stub" prefix was carried over from early Block 2
when they were actually stubs. They're now fully implemented production
handlers. The name is misleading but cosmetic — not a runtime bug.

### 2.2 Trigger Server Not Started in Local-First Mode

The daemon creates an `EventBus` but does not start the trigger API server
(gRPC :7718 / REST :7717). This means external invocations are impossible.
The auto-invoke via docker exec is the ONLY way an agent gets invoked.

This is acceptable for P1 local-first (the spec says "loopback-only unless
`--expose`"), but it should be documented that the trigger server exists
but isn't wired into daemon startup.

### 2.3 Stats Returns `errDockerNotImplemented`

`DockerRuntime.Stats()` returns a hardcoded "not yet implemented" error.
The dashboard's resource monitoring (CPU, memory) doesn't work. Agent
containers run without resource visibility.

### 2.4 CLI Doc Comments Say "not yet implemented" for Implemented Commands

`internal/cli/doc.go` says `pack`, `run`, `stop`, `logs`, `policy`,
`secrets`, `audit`, `validate`, `summarize`, `explain-failure`,
`explain-denial`, `recommend-patch`, `timeline`, `next-action` are all
"not yet implemented." They ARE implemented. The doc is stale.

### 2.5 No Policy Enforcement at Runtime

`PolicyApply` writes policy.yaml and compiles a gateway config, but the
gateway config is never consumed by a running gateway (because there is
no gateway container). Policy is parsed, validated, digested, and stored
— but never enforced.

---

## 3. PRODUCTION RISKS (RANKED BY SEVERITY)

### P0 — Would Break a Demo or First Impression

1. **No gateway = no real policy enforcement.** The product's entire value
   prop ("policy-controlled egress") doesn't work. Egress denial is just
   network isolation. A security buyer would immediately notice that
   policy.yaml has no runtime effect.

2. **Orphaned containers on daemon crash.** If the daemon dies, all running
   agent containers become invisible. No reconciliation on restart. The
   `s.runs` map is in-memory only.

3. **Run status always "succeeded".** Even crashed agents show green. A
   buyer watching the dashboard during a demo would see false success.

### P1 — Would Surface Under Load or Edge Cases

4. **Invoke/Stop race.** Calling Stop immediately after Run can race the
   invoke goroutine. The container gets removed while the invoke is still
   polling.

5. **No concurrent pack protection.** Two simultaneous `agent pack` calls
   for the same agent name would race on the deployment directory
   (RecordDeployment uses atomic staging-dir swap, but two packs could
   produce conflicting images).

6. **Hardcoded harness address (127.0.0.1:8080).** If a future container
   runs the harness on a different port, the auto-invoke breaks silently.

### P2 — Acceptable for P1 but Should Track

7. **Fake LLM.** `agent.llm()` returns garbage. Fine for plumbing, bad for
   real agent demos.

8. **Stats not implemented.** Dashboard resource monitoring doesn't work.

9. **CLI doc.go stale.** Says commands are unimplemented when they work.

---

## 4. MUST FIX BEFORE BLOCK 14

These are the items that should be addressed before starting Block 14
security hardening. They're either correctness bugs or things that make
14A's security work harder.

### Fix 1: Run Status Tracking (CRITICAL — do this first)

**Problem:** Every run shows "succeeded" regardless of outcome.

**Fix:** Add a `status` field to `trackedRun`. Set it to "running" on Run,
"failed" on invoke error, "succeeded"/"failed" on Stop based on actual
container exit code. Publish the correct event type.

**Effort:** ~1 micro-chunk (add field, set in 3 places, update Stop).

### Fix 2: Orphan Container Reconciliation on Daemon Start (CRITICAL)

**Problem:** Daemon crash → orphaned containers that the new daemon can't
see.

**Fix:** On daemon Start(), list all Docker containers with
`agentpaas/resource-type=agent` label. For any that aren't in the
`s.runs` map, either re-track them or clean them up (stop + remove +
remove network). This pattern exists in the Block 5 reconciliation tests.

**Effort:** ~2 micro-chunks.

### Fix 3: Invoke/Stop Synchronization (HIGH)

**Problem:** Stop can race the invoke goroutine.

**Fix:** Store a `context.CancelFunc` in `trackedRun`. On Stop, cancel
the invoke context before removing the container. The invoke goroutine
checks context cancellation and exits cleanly.

**Effort:** ~1 micro-chunk.

### Fix 4: Docker E2E Test in block13-gate (HIGH)

**Problem:** Gate doesn't test the real Docker flow. CI won't catch
regressions.

**Fix:** Add a `AGENTPAAS_DOCKER_TESTS=1` gated test (like block5-gate
does) that runs the full pack→run→stop→audit flow and asserts
`egress_denied` in the audit chain. Gate skips gracefully when Docker
isn't available.

**Effort:** ~2-3 micro-chunks (write test, wire into gate, verify).

### Fix 5: Rename `stubControlServer` to `controlServer` (LOW)

**Problem:** Misleading name. "stub" implies temporary.

**Fix:** Global rename. Mechanical change, no logic.

**Effort:** ~1 micro-chunk (sed + verify build).

### Fix 6: Stale CLI doc.go Comments (TRIVIAL)

**Fix:** Remove "not yet implemented" from the doc comments for commands
that are implemented.

**Effort:** 5 minutes.

---

## 5. WHAT'S NOT BROKEN (VALIDATED)

To be fair about what works:

- **Audit hash chain** — `AuditWriter.Append()` correctly re-chains
  harness records (assigns seq, prev_hash, recomputes record_hash). The
  chain is cryptographically sound. This is the most security-critical
  piece and it's correct.

- **Immutable deployment** — `RecordDeployment` uses atomic staging-dir
  swap with backup + restore on failure. `VerifyDeployedIntegrity` checks
  all digests. The prompt-change redeploy test proves distinct digests.

- **Concurrent run limit** — Enforced before Docker resources are created.
  Returns gRPC ResourceExhausted. Tested.

- **Plugin tool handlers** — All 17 tools are fully implemented with
  proper CLI shelling, JSON parsing, and error handling. 109 tests pass.

- **Confirmation protocol** — Trust-boundary actions require daemon
  confirmation. Self-confirm detection prevents the plugin from
  confirming its own actions.

- **Prompt-injection sanitizer** — Filters untrusted data from agent
  source/logs/external payloads. Wired into the handler pipeline.

- **Docker Exec** — Correctly implemented with stdcopy demux. The
  auto-invoke works against real Docker (verified live).

- **Harness audit appender** — Writes JSONL egress events at 5 decision
  points. FileAuditAppender is correct.

---

## SUMMARY

Block 13's plumbing is solid — audit chains, immutable deployments,
plugin tool wrappers, Docker exec, harness audit events all work
correctly. But the product layer on top has significant gaps:

1. **No real policy enforcement** (network isolation, not gateway)
2. **No run status tracking** (always "succeeded")
3. **No orphan cleanup** (crashed daemon = invisible containers)
4. **No Docker e2e in the gate** (manual-only verification)

Fixes 1-4 above should take 5-7 micro-chunks total (~1 focused session).
They're worth doing before Block 14 because 14A security hardening builds
on the assumption that the fundamentals work correctly, and right now the
run lifecycle has correctness bugs that would make security fixes fragile.
