# R32 steps 3–6 — Manual test plan (pre-v0.3.0 tag)

**Purpose:** Prove the B32/R32 “Agent Registry and Secure Delegation” operator claims that the golden share loop does **not** cover.  
**Normative source:** `docs/execution/blocks/b32-summary.md` § R32 items 3–6.  
**Also maps to:** short checklist M3–M6 in `docs/execution/blocks/b32-manual-testing.md`.  
**Prerequisite:** Golden half 1–12 PASS (or equivalent); `0.3.0-dev` RC on PATH; doctor 7/7; Docker/colima up.  
**Profile:** `ap-testing` preferred; orchestrator verifies disk only.  
**Rules:** stop on bug; no workarounds; disk truth (audit, invoke-response, task events); do not weaken assertions.

---

## Why this was not executed in the plan you just ran

| Fact | Detail |
|------|--------|
| It **was** in the short checklist | `b32-manual-testing.md` **M3–M6** = delegate surface, artifact transfer, wait/wake, negative paths. |
| The **long** plan deprioritized it | `b32-v0.3.0-manual-test-plan.md` line ~35: full multi-agent `agent.delegate` is **library/harness-proven**, “not yet a first-class Hermes two-weather-agents golden path”; Phase 15 I–K marked DEFER by design of *that* document. |
| Founder steering mid-run | You asked to drop verbose ops paste and match **v0.2.3 golden loop** (install → weather → modify → export → receive). Orchestrator followed that spine (phases 1–12 + registry). |
| Product UX gap | No Hermes one-liner “make A call B.” Path is: two packages + `workflow.yaml` `delegations:` + pack snapshot + SDK `agent.delegate` inside a run. Phase 14 already showed bare workflow pack is not a friendly CLI. |
| Risk doc honesty | `docs/b32-risk-analysis.md`: east-west multi-container live path thin; task store memory-only across daemon restart. |

So: **not omitted from R32 product requirements** — **sidelined in the human plan you executed** in favor of the share loop. Tagging without 3–6 means release notes must not claim the full R32 six-step quickstart.

---

## R32 claims under test

| Step | Claim (paraphrase) | Manual proof |
|------|-------------------|--------------|
| **3** | Delegate a task to another agent by **logical** identity; no host/port/token in agent code or response | Parent run calls `agent.delegate(capability=…)`; child runs; response/task JSON has no IP/localhost/network_alias/capability_token |
| **4** | One digest-verified artifact, read-only, scoped **audience** | Producer commits artifact; authorized audience can project/read; wrong audience denied |
| **5** | Disconnect / cursor; terminal task event without polling loops as correctness | Parent can resume from event cursor; terminal delivered (at-least-once OK if deduped) |
| **6** | Undeclared delegation denied + audited; audience-mismatched artifact read denied + audited | Stable deny codes; harness/daemon audit evidence |

**Automated baseline (must stay green):**

```bash
export PATH="$HOME/projects/agentpaas/bin:/tmp/agentpaas-rc-prefix/bin:$PATH"
cd ~/projects/agentpaas
make block32-gate          # long; or at least:
go test ./internal/delegation/... ./internal/harness/... -count=1 -run 'B32|Delegat|Artifact'
```

Gate PASS ≠ operator quickstart PASS. This doc is the operator path.

---

## Tiering (so you know when to stop)

| Tier | What | Tag implication |
|------|------|-----------------|
| **A** | `block32-gate` + adversary B32 green | Minimum for “library + gate proven” wording |
| **B** | Single-machine **two-package** pack/run with `delegations:` + `agent.delegate` happy path + undeclared deny | Required to claim R32 step 3 + 6a in quickstart |
| **C** | Artifact audience allow + deny + optional tamper | Required for step 4 + 6b |
| **D** | Wait/wake after parent disconnect / harness restart | Required for step 5; may hit known memory-store limit — document if blocked |

**GO full R32 quickstart:** A+B+C+D green (or D explicitly waived in release notes with residual risk).  
**GO narrow v0.3.0:** A + golden share + registry; notes say A2A operator path = gate-proven, quickstart 3–6 follow-up.

---

# HALF 0 — Prep (host shell)

```bash
export PATH="/tmp/agentpaas-rc-prefix/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/bin:/bin:$PATH"
# or repo bin after make build-all
hash -r
agentpaas version          # 0.3.0-dev
agentpaas doctor           # 7/7
cd ~/projects/agentpaas && make build-all
```

Identity + secret (publisher machine is fine for this proof):

```bash
agentpaas identity show || agentpaas identity init --name r32-manual
# openrouter-key if callees need LLM; child can be pure-http for simpler proof
agentpaas secret list
```

Optional clean agents dir if leftover state confuses you — **do not** need full nuclear if golden loop already passed; use a fresh project dirs under `/tmp/r32-a2a/`.

```bash
mkdir -p /tmp/r32-a2a
rm -rf /tmp/r32-a2a/worker /tmp/r32-a2a/parent
```

---

# HALF 1 — Step 3: logical delegate (Tier B)

## Goal

Parent agent invokes child by **binding id** only. No network endpoints in agent code or invoke/task payloads.

## 1A. Build a minimal **worker** (callee)

Short paste for ap-testing (or implement via CLI yourself):

```
Create project /tmp/r32-a2a/worker — a tiny agent that on invoke returns
{"status":"OK","echo": <payload>, "role":"worker"}.
No LLM required if possible. policy: deny-all egress or empty egress.
Pack, run, promote as worker@… 
Stop after registry show promoted=true.
```

**Verify (you):**

```bash
agentpaas registry list
agentpaas registry promote <worker-ref>   # if not already
# note package digest for workflow.yaml
agentpaas registry show <worker-ref>
```

## 1B. Build **parent** with workflow delegations

Parent `main.py` must use SDK only:

```python
# conceptual — skill/worker writes real file
from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    h = agent.delegate(
        capability="r32.echo",   # MUST match binding_id in workflow.yaml
        message={"text": "ping-from-parent"},
        idempotency_key=payload.get("idempotency_key") or "r32-manual-1",
    )
    # drain events / result per TaskHandle API in SDK
    ...
    return {"status": "OK", "child_task": h.task_id, ...}
```

`workflow.yaml` (shape from pack tests / b32-summary):

```yaml
kind: standalone   # or parent_child if required by pack in this build — use what ValidateWorkflowYAML accepts
delegations:
  - binding_id: r32.echo
    package_name: <worker name>
    package_version: "<ver>"
    bundle_digest: "sha256:..."   # from registry show / lock
    max_data_class: internal
    artifact_audience:
      - parent
```

Pack parent **with** workflow that embeds communication snapshot.

**Paste:**

```
Create /tmp/r32-a2a/parent that delegates capability r32.echo to the promoted worker
using agent.delegate only (no hosts/ports). Include workflow.yaml delegations.
Pack and run parent. Invoke parent with {"ping":true}.
Show invoke-response and confirm no IP/localhost/capability_token in response or task JSON.
```

**Verify (disk):**

```bash
# latest parent run
find ~/.agentpaas/state/runs -name invoke-response.json -mmin -30 | tail -3
# must NOT match:
rg -n '127\.0\.0\.1|localhost|network_alias|capability_token|0\.0\.0\.0' \
  ~/.agentpaas/state/runs/run-*/invoke-response.json \
  ~/.agentpaas/state/runs/run-*/  2>/dev/null | head
agentpaas audit query --limit 30 2>/dev/null || true
```

**PASS step 3:** child effect observed (echo/result); logical capability only; leak grep clean; audit shows delegation path (not raw peer dial from agent).

**If pack cannot wire delegations to a live second container:** record **BLOCKED** with exact error → either fix before tag or narrow release notes (do not mark PASS).

---

## 1C. Idempotent retry (step 3 extra)

```
Invoke parent twice with the SAME idempotency_key. Report both task ids.
```

**PASS:** same task id (or documented equivalent).  
**FAIL:** two distinct tasks for same key without documented reason.

---

# HALF 2 — Step 6a: undeclared delegation denied (Tier B)

## Goal

Parent code calls a capability **not** in signed `delegations:` → deny + audit.

**Setup:** keep parent image that only declares `r32.echo`.  
Temporarily change **only running code** is wrong — must be **repack** with code calling `agent.delegate("r32.undeclared", …)` while workflow still only has `r32.echo`, **or** use a second parent project.

**Paste:**

```
Pack a parent whose workflow allows only r32.echo but code calls agent.delegate("r32.undeclared", ...).
Run and invoke. Expect failure with stable denial, not hang. Show audit.
```

**Verify:**

```bash
# invoke-response status ERROR or structured deny
# audit: delegation denied / undeclared binding (exact event name from build)
rg -n 'denial|denied|undeclared|delegat' ~/.agentpaas/state/audit.jsonl | tail -20
rg -n 'denied|denial' ~/.agentpaas/state/runs/run-*/harness-audit/*.jsonl | tail -20
```

**PASS:** non-zero/error result; stable code/reason; audit row; no child side effects.  
**FAIL:** success, hang, or silent drop.

---

# HALF 3 — Step 4: artifact transfer (Tier C)

## Goal

Producer commits artifact; consumer with audience can read; digest binds content.

**Preferred path:** extend worker to write a small artifact via SDK/workspace commit used by B27/B32, parent receives reference and reads via authorized project API — **only if** exposed on SDK for agents.

If agent-level APIs are thin, operator path may be:

```bash
# Use package tests as operator witness (not a substitute for quickstart, but evidence):
cd ~/projects/agentpaas
go test ./internal/delegation/ -count=1 -run 'Artifact|Audience' -v
```

**Manual operator (when SDK supports):**

1. Parent delegates work that returns an artifact ref.  
2. Parent (or sibling binding) projects artifact for audience `parent`.  
3. Bytes match digest.

**Paste (if SDK ready):**

```
Delegate a task that produces a small text artifact. Parent must read it only via
authorized artifact projection. Show digests and that raw shared paths are not used.
```

**PASS:** read succeeds for allowed audience; content matches digest.  
**BLOCKED:** no agent-reachable commit/project → document + rely on gate tests; **cannot** claim R32 step 4 in public quickstart.

---

# HALF 4 — Step 6b: audience mismatch (Tier C)

**Paste:**

```
Attempt to read/project the same artifact as a wrong audience (or second agent not in audience).
Expect denial and audit. Do not weaken policy to pass.
```

**Verify:** deny + audit; no plaintext leak to unauthorized run dir.

**PASS / BLOCKED** same rules as step 4.

---

# HALF 5 — Step 5: wait/wake + disconnect (Tier D)

## Goal

Parent does not need a live HTTP long-poll as correctness; terminal event is durable enough to resume after disconnect.

**Happy path paste:**

```
Parent delegates a slow child (sleep 10–30s or multi-step). Parent should wait via
task events/cursor APIs, not a busy loop on status HTTP if avoidable.
Show event sequences and terminal event once.
```

**Disconnect proof (stronger):**

```bash
# While parent is waiting on child:
# 1) note run_id / task_id / last event cursor from logs
# 2) stop parent container OR restart agentpaasd (know residual: memory task store)
agentpaas daemon stop; sleep 2; agentpaas daemon start
# 3) resume wait from cursor or re-attach per product docs
```

**PASS:** terminal event observed once (dup OK if idempotent); parent reaches terminal without “poll forever.”  
**EXPECTED LIMIT (document, not silent PASS):** B32 risk — task store **not** durable across daemon restart. If restart loses tasks, **FAIL for step 5 durability claim** but may **PASS in-process disconnect** (parent container recycle with daemon up). Record which.

**Duplicate delivery:**

```
If test harness can redeliver terminal event, parent must not double-apply side effects
(or must dedupe by sequence).
```

---

# HALF 6 — Leak & negative sweep (all steps)

After any successful delegate:

```bash
# responses + task records
rg -n 'capability_token|network_alias|127\.0\.0\.1|/var/run|Bearer ' \
  ~/.agentpaas/state/runs/run-*/invoke-response.json \
  ~/.agentpaas/state/routed/runs/*/  2>/dev/null | head -40
```

**PASS:** no matches (except benign docs). Any token/alias in agent-visible JSON = **FAIL**.

---

# Sign-off sheet (fill after run)

```
Date:
main SHA:
agentpaas version:

Tier A block32-gate:     PASS/FAIL
Step 3 logical delegate: PASS/FAIL/BLOCKED
Step 3 idempotency:      PASS/FAIL/BLOCKED
Step 6a undeclared deny: PASS/FAIL/BLOCKED
Step 4 artifact allow:   PASS/FAIL/BLOCKED
Step 6b audience deny:   PASS/FAIL/BLOCKED
Step 5 wait/wake:        PASS/FAIL/BLOCKED (in-process / cross-daemon?)
Leak sweep:              PASS/FAIL

P0/P1 open:
GO full R32 quickstart claim? YES/NO
GO narrow v0.3.0 (share+registry+gates only)? YES/NO
```

---

## Suggested order of work (half-day)

1. Tier A gate (background)  
2. Worker + parent delegate happy path (step 3)  
3. Undeclared deny (6a)  
4. Artifact allow + deny if SDK allows (4, 6b)  
5. Wait/wake in-process (5); only then try daemon restart  
6. Sign-off  

## Hermes pastes — keep short

Do **not** dump this whole file into ap-testing. One half at a time, same style as golden loop.

---

## Related bugs / residuals

- BUG-037 PATH — use abs CLI in pastes if needed  
- B32 risk: memory task store vs step 5 cross-daemon  
- Phase 14: workflow pack UX weak — this plan may hit same wall; treat BLOCKED as release-note honesty, not “test skipped quietly”
