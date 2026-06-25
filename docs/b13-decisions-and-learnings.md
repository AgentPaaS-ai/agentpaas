# Block 13 — Decisions, Learnings, and Build Log

**Block:** B13 — Hermes Integration Plugin
**Status:** COMPLETE — `make block13-gate` passes
**Date range:** Multiple sessions, June 2026
**Commits:** 50+ commits across B13 (T01–T09 + BUG 6/7 series)

This document is the authoritative reference for every significant decision,
bug, workaround, and learning from Block 13. It exists so a future reader
(you, a teammate, or an AI) can trace WHY each choice was made, not just
WHAT changed.

---

## Table of Contents

1. [Architecture Decisions](#1-architecture-decisions)
2. [The BUG Series — Root Causes and Fixes](#2-the-bug-series--root-causes-and-fixes)
3. [Non-Obvious Technical Learnings](#3-non-obvious-technical-learnings)
4. [OWA Build Loop Notes](#4-owa-build-loop-notes)
5. [Subtask Completion Log](#5-subtask-completion-log)
6. [What Was Deferred to Block 14](#6-what-was-deferred-to-block-14)

---

## 1. Architecture Decisions

### 1.1 Hermes Plugin, Not MCP Server (Locked Early)

**Decision:** B13 ships as a single Hermes plugin (`integrations/hermes-plugin/`),
not an MCP server or a bare skill.

**Rationale:** The PRD v4 master and the execution plan both lock this. The
plugin wraps the Block 11 operator contract (17 tools) as Hermes tool
handlers that shell out to the `agent` CLI with `--json`. This avoids:

- Growing the Hermes core tool schema (every core tool ships on every API call)
- Re-implementing operator logic in Python (DRY — all logic lives in the Go CLI)
- A separate MCP server that needs its own lifecycle (deferred to P2)

**Trade-off:** Claude Code / Codex / Cursor don't get a native integration
in P1. That's intentional — the generic MCP server (`agentpaas-mcp`) is P2.
P1 is Hermes-only because Hermes is the primary coding agent used for
AgentPaaS development.

**Commit:** `e07f643` — "lock Hermes plugin spec"

---

### 1.2 Docker Exec Invoke Pattern (Not Trigger Server)

**Decision:** The daemon auto-invokes the agent after container start via
`docker exec`, not via the trigger API server (gRPC :7718).

**Context — the invoke gap (BUG 7d):** The harness HTTP server binds to
`127.0.0.1:8080` INSIDE the container. The daemon starts the container but
had no way to reach that loopback address from the host. Without an invoke,
`SetInvoke()` never fires, the agent's `@on_invoke` handler never runs,
`agent.http()` is never called, and no egress audit events are produced.

**Options considered:**

| Option | Description | Verdict |
|--------|-------------|---------|
| A. Docker exec | Daemon execs `python3 -c "urllib.request..."` inside the container to POST to `/invoke` | **CHOSEN** — simplest, no extra services, matches industry pattern |
| B. Trigger server | Start the trigger API (gRPC :7718) alongside the daemon | Rejected — adds a server lifecycle for P1 local-first mode; the trigger server is designed for remote invocation, not local auto-invoke |
| C. In-process test | Go integration test that drives the full flow | Good for CI but not the production path |

**Why docker exec is the right pattern:**

- The harness binds to loopback only (security: the agent API is unreachable
  from outside the container). Docker exec reaches the container's network
  namespace directly — no port forwarding, no exposure.
- This matches the industry standard. AWS Bedrock AgentCore uses
  `InvokeAgentRuntimeCommand` for the same purpose (execute a command inside
  a running container session to trigger agent code). ZenML Kitaru's
  `DockerSandbox` and OpenAI Sandboxes use the same control-plane-invokes-
  sandbox architecture.
- The `invokeAgent` method polls `/readyz` (up to 30 attempts, 1s apart)
  then POSTs `/invoke` with a 60s timeout. It runs in a goroutine after
  container start — non-blocking. Errors are logged, not fatal.

**Exec on RuntimeDriver interface:** Added `Exec(ctx, id, cmd) → (stdout,
stderr string, exitCode int, err error)` to the `RuntimeDriver` interface.
This is a permanent, broadly useful addition — any container runtime needs
exec capability for orchestration. Implemented on `DockerRuntime` using
`ContainerExecCreate`/`ContainerExecAttach` with `stdcopy.StdCopy` for
stream demuxing.

**Commit:** `13e489c`

---

### 1.3 Harness Audit via Mounted Volume (Not RPC Streaming)

**Decision:** The harness writes audit events (egress decisions, MCP calls)
to a JSONL file on a mounted volume (`/audit/harness-audit.jsonl`). The
daemon reads this file after the run stops and ingests records into its own
hash-chained audit chain.

**Rationale:** Simpler than real-time RPC streaming. The harness runs inside
an isolated container with only a Unix socket RPC to the daemon. Real-time
streaming would require either:

- A persistent RPC connection from daemon → harness (complex lifecycle)
- A tail-and-publish pattern (deferred to Block 14B)

For P1, post-run ingestion is sufficient. The dashboard timeline shows
egress events after the run completes. Block 14B adds real-time streaming.

**Implementation:**

1. `internal/harness/file_appender.go` — `FileAuditAppender` writes flat
   JSONL records (no hash chain — the daemon re-chains them on ingestion).
2. `cmd/harness/main.go` — wires `AGENTPAAS_AUDIT_PATH` env var → appender.
3. `internal/harness/rpc_server.go` — `auditEgressDecision()` emits
   `egress_allowed` / `egress_denied` events at 5 points in `handleHTTP`.
4. `internal/daemon/control_handlers.go` — Run handler mounts the audit
   volume; Stop handler calls `ingestHarnessAudit()`.

**Commits:** `3236c1a`, `901acd9`, `319ccee`

---

### 1.4 Concurrent Run Limit (max 3)

**Decision:** The daemon enforces a maximum of 3 concurrent agent runs.
Exceeding this returns gRPC `ResourceExhausted`.

**Rationale:** Prevents resource exhaustion on a single machine. Each agent
container consumes memory, CPU, and PIDs. The limit is configurable but
defaults to 3 for P1 local-first deployments.

**Commit:** `a479a78`

---

### 1.5 Execution Plan v1.1 Restructure

**Decision:** Consolidated post-B13 work into a single Block 14 with three
sub-segments (14A security, 14B egress timeline, 14C release). Removed the
old calendar/sequencing block as a gate. Block 15 is manual use-case
assessment.

**Rationale:** The original plan had separate blocks for each post-B13
concern, creating a long sequential chain. Consolidating them into one block
with sub-gates allows parallel work and faster delivery.

**Commit:** `a479a78`

---

## 2. The BUG Series — Root Causes and Fixes

Block 13 uncovered a series of integration bugs. Each is documented here
with root cause, symptom, and fix — so the same class of bug doesn't recur.

### BUG 6: Exec Format Error (Container Binary Mismatch)

**Symptom:** Container starts, immediately exits with `exec format error`.

**Root cause:** The harness binary baked into the container image was
compiled for `darwin/arm64` (the host), but the container runs
`linux/arm64` (colima/Docker Desktop containers are Linux internally).

**Fix:** Cross-compile the harness for the container target at pack time:
```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/agentpaas-harness-linux ./cmd/harness
```
The shipped binaries (`agent`, `agentpaasd`) stay darwin/arm64 — Mac-only
product. The internal cross-compile is NOT a Linux release; it's how every
Go project that puts a binary in a container works.

**Commit:** `dc457a8`

**Pitfall:** Don't conflate "cross-compile for the container" with "support
Linux as a platform." The user locked P1 = Mac-only. The container always
runs Linux internally — that's unavoidable and is NOT a Linux release.

---

### BUG 7a: Missing Audit Events

**Symptom:** `agent audit query` returns empty — no pack/run/stop events.

**Root cause:** The daemon's control handlers (Pack, Run, Stop) didn't call
`recordAudit()`. The audit writer and index existed but were never invoked.

**Fix:** Added `recordAudit()` calls to Pack, Run, and Stop handlers with
relevant payload (agent_name, image_digest, run_id, container_id, network).

**Commit:** `8a3f68a`

---

### BUG 7b: Dashboard Resource Listing Crash

**Symptom:** Dashboard crashes when listing Docker containers.

**Root cause:** The dashboard tried to list containers directly via the Docker
client, but the daemon hadn't abstracted this behind the runtime driver. A nil
pointer on `d.dockerRT` caused the crash.

**Fix:** Implemented `DockerResourceManager` that delegates to the runtime
driver's `ListContainers` method. Also fixed a goroutine race on `d.dashboard`
between `Start()` and `Stop()` — both read/nilled the field without holding
`d.mu`.

**Commits:** `3f52177`, `a15ceca`

---

### BUG 7c: Daemon Startup Crashes

**Series of crashes on fresh installs:**

1. **Nil ProcessState panic** — daemon checked `os.Process.Pid` before the
   process was set. Fixed with nil guards. (`247181c`)
2. **home.Ensure() not called** — `agentpaasd` called `daemon.New()` before
   ensuring the home directory existed. Fresh install → crash. Fixed by
   calling `home.Ensure()` first. (`9095b68`)
3. **CLI relative paths** — daemon received relative project paths from the
   CLI but couldn't resolve them (daemon runs in its own working directory).
   Fixed by resolving to absolute in the CLI before sending. (`35ebfad`)
4. **Unimplemented RPC handlers** — 10 control RPC handlers were stubs
   returning "not implemented." Implemented Pack/Run/Stop/Logs/Policy/
   Secret/Audit. (`cb77041`)
5. **Dashboard server not wired** — dashboard server existed but wasn't
   started by the daemon. Fixed in daemon startup. (`7ae6545`)
6. **Daemon stdout/stderr blocking** — daemon wrote to stdout, causing the
   CLI to hang waiting for EOF. Fixed by redirecting to a log file. (`ce99da2`)
7. **Goroutine leaks** — Start success path and Stop timeout path both
   leaked goroutines. Fixed with proper context cancellation. (`8a6146a`,
   `0410bc2`)

---

### BUG 7d: DENIED Probe (The Big One)

**Goal:** Verify that an agent attempting HTTP egress on an internal-only
network produces an `egress_denied` audit event.

**This was the hardest bug in Block 13.** It spanned 5 sub-steps across
multiple sessions:

#### Step 1-2: Harness Audit Appender + Egress Events

**Problem:** The harness `cfg.Audit` was nil — no audit appender wired. The
`handleHTTP` method in `rpc_server.go` didn't emit audit events for egress
attempts.

**Fix:**
- Created `internal/harness/file_appender.go` — writes JSONL to a file path.
- `cmd/harness/main.go` wires `AGENTPAAS_AUDIT_PATH` → `FileAuditAppender`.
- `rpc_server.go` `auditEgressDecision()` emits `egress_allowed`/`egress_denied`
  at 5 points: invalid request, credential not declared, HTTP failed,
  response read failed, HTTP success.
- Added `Binds []string` field to `ContainerSpec` for volume mounts.

**Commit:** `3236c1a`

---

#### Step 3: Mount Audit Volume in Run Handler

**Problem:** The harness writes to `/audit/harness-audit.jsonl` inside the
container, but no volume was mounted there.

**Fix:** Run handler creates host dir `{state}/runs/{runID}/harness-audit/`,
chmod 0777 (to defeat umask — see learnings), adds bind mount
`{hostAuditDir}:/audit`, sets `AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl`.

**Commit:** `901acd9`

---

#### Step 4: Ingest Harness Audit on Stop

**Problem:** After the run, the harness audit JSONL file existed on the host
but its records were never ingested into the daemon's audit chain.

**Fix:** Stop handler calls `ingestHarnessAudit(runID, auditDir)` which:
1. Reads the harness audit JSONL
2. Parses each record (flat, no hash chain)
3. Appends each to the daemon's audit chain via `auditWriter.Append(record)`
4. Rebuilds the SQLite audit index

The daemon re-chains the flat harness records into the hash-chained audit log.

**Commit:** `319ccee`

---

#### Step 5: SDK Bundling + Agent Path

Two sub-bugs discovered during e2e:

**5a. SDK not found in container:** The distroless container base
(`gcr.io/distroless/python3-debian12`) has NO pip. The `agentpaas_sdk`
cannot be pip-installed at runtime. The harness finds the SDK via
`pythonPackagePath()` which walks from cwd (`/app`) looking for
`python/agentpaas_sdk/`.

**Fix:** Copy the SDK into the test agent project before packing:
```sh
mkdir -p /tmp/agentpaas-e2e-agent/python
cp -r python/agentpaas_sdk /tmp/agentpaas-e2e-agent/python/
```
Do NOT list `agentpaas_sdk` in requirements.txt — it won't pip-install and
isn't needed there. The SDK is a thin RPC client that sends JSON over a Unix
socket to the Go harness (PID 1).

**5b. Wrong agent path:** The harness defaults `AGENTPAAS_AGENT_PATH` to
`/agent/main.py`, but the pack Dockerfile copies project files to `/app/`.

**Fix:** Daemon passes `AGENTPAAS_AGENT_PATH=/app/main.py` in the container
env explicitly.

**Commits:** `528bc5b` (agent path), SDK bundling documented in skill

---

#### Step 6: The Invoke Gap (Final Blocker)

**Problem:** Even after all the above, no egress audit events appeared.
Root cause: the harness HTTP server listens on `127.0.0.1:8080` INSIDE the
container, but nothing ever POSTs to `/invoke`. The daemon starts the
container and returns a run_id, but the agent's `@on_invoke` handler never
fires, so `agent.http()` is never called.

**Fix:** See section 1.2 above — docker exec auto-invoke.

**Commit:** `13e489c`

---

### Cosign Signing Series

Multiple bugs in the image signing pipeline:

1. **SEC1 private key format** — cosign keys in SEC1 format weren't handled
   by the pack lock signer. Fixed by detecting and converting. (`f5244cb`)
2. **PKCS8 key format** — further key format normalization. (`8a60233`)
3. **v3 signing-config flag** — cosign v3 changed the signing config flag
   name. Updated the pack command. (`e4f0343`)
4. **DOCKER_HOST propagation** — the pack subprocess didn't inherit
   `DOCKER_HOST`, so it couldn't reach colima's Docker daemon. Fixed by
   propagating the env var. (`e4f0343`)
5. **Local registry ref** — the run handler used the wrong image reference
   format for the local registry. Fixed to use `localhost:5001/...`. (`8a60233`)

---

## 3. Non-Obvious Technical Learnings

These are platform-specific gotchas that cost significant debugging time.
Each is documented in the `agentpaas-build-rhythm` skill so future sessions
don't re-derive them.

### 3.1 Colima virtiofs Only Mounts /Users

Colima's virtiofs mounts ONLY `/Users/<user>`. A Docker bind mount from host
`/tmp/agentpaas-e2e-home/...` resolves to the VM's `/tmp` (empty), not
macOS `/tmp`. The symptom looks identical to a permissions error, but chmod
doesn't help because you're chmodding the wrong directory.

**Fix:** ALWAYS put `AGENTPAAS_HOME` under `~/` (e.g.
`~/agentpaas-e2e-home`), NEVER `/tmp`.

### 3.2 MkdirAll Umask Defeats Mode

`os.MkdirAll(path, 0o777)` does NOT produce a 0777 directory. The process
umask (typically 022 on macOS/Linux) masks it to 0755. AgentPaaS containers
run as UID 64000 (non-root). A host directory bind-mounted into the container
at 0755 lets UID 64000 read/execute (it's "other" on the host) but NOT write.

The fix is ALWAYS: follow `MkdirAll` with an explicit `os.Chmod(path, 0o777)`
to defeat umask. Do NOT assume MkdirAll's mode argument is the final mode.

This applies to any Docker bind mount from a host dir into a container that
runs as a different UID. Check `ls -la` on the host dir — if it's 0755
(drwxr-xr-x) and the container runs non-root, this is your bug.

**Commit:** `abb6362`, `90515a5`

### 3.3 Harness AGENTPAAS_AGENT_PATH Defaults Wrong

The harness binary (`cmd/harness/main.go`) defaults `AGENTPAAS_AGENT_PATH`
to `/agent/main.py`, but the pack Dockerfile copies project files to `/app/`.
The daemon must pass `AGENTPAAS_AGENT_PATH=/app/main.py` in the container env
or the agent import fails with FileNotFoundError.

**Commit:** `528bc5b`

### 3.4 Harness Never Auto-Invoked

The harness is an RPC server that waits for invoke requests. The daemon
starts the container but does NOT invoke the agent. There is no `agent
invoke` CLI command. For e2e verification, either:
- The daemon auto-invokes via docker exec (our chosen approach)
- The trigger server (gRPC :7718) is started and invokes
- A Go integration test drives the flow in-process

Also: the SDK's `agent.http()` requires `self._rpc` to be set
(python/agentpaas_sdk/agent.py:62), which happens in runner.py AFTER import.
So egress calls at import time fail with "SDK RPC is not connected". Egress
must happen inside an invoke handler.

### 3.5 SDK RPC Timing — Egress Must Be Inside Invoke

The SDK's `agent.http()` requires `self._rpc` to be set, which happens in
`runner.py:33` AFTER import. Egress calls at module top level fail with
"SDK RPC is not connected." All egress must happen inside the `@on_invoke`
handler.

### 3.6 ST1005 Lint: Lowercase Error Strings

golangci-lint's ST1005 rule flags capitalized error strings. Workers running
`make lint` will lowercase error strings to pass (e.g., "Docker socket not
found" → "docker socket not found"). Do NOT revert these as "cosmetic" —
they're legitimate lint fixes. Before reverting any worker change, check
whether `make lint` still passes without it.

**Commit:** `90515a5`

### 3.7 DeployedAgent File Layout

`pack.LoadDeployedAgent` does NOT read a single JSON file. It reads
individual files from `state/agents/<name>/`:
- `agent.lock` (JSON: AgentLock struct)
- `image.digest` (plain text)
- `source_digest` (plain text)
- `deployed_at` (RFC3339Nano timestamp)
- `agent.lock.sha256` (lockfile hash)

When writing test fixtures, create these files individually. Writing a single
`deployed.json` will NOT work.

### 3.8 Docker Exec Stream Demux

Docker exec output is multiplexed — stdout and stderr are interleaved in a
single stream with frame headers. Use
`stdcopy.StdCopy(stdoutBuf, stderrBuf, hijacked.Reader)` from
`github.com/docker/docker/pkg/stdcopy` to demux. Without this, output appears
corrupted with binary frame headers.

---

## 4. OWA Build Loop Notes

Block 13 was built using the Orchestrator-Worker-Adversary (OWA) pattern:

- **Orchestrator:** z-ai/glm-5.2 via z.ai direct API. Plans, dispatches,
  reviews, merges. Does NOT edit code directly.
- **Workers:** grok-composer-2.5-fast via Grok CLI ($0 subscription tier).
  Dispatched on specific micro-chunks.
- **Adversary:** grok-4.3 via xai-oauth ($0). Writes break-tests against
  security claims.
- **Verifier:** GLM-5.2 block-end check.

**Key OWA learnings from B13:**

1. **Worker changes beyond scope:** Workers run `make lint` / `make test` and
   may fix pre-existing violations. Do NOT blindly revert extra changes.
   Check `git diff` on extra files — if they're lint/formatting fixes, keep
   them.

2. **Adversary → fix → adversary spiral:** An adversary break should be fixed
   in ONE micro-chunk. If the fix itself needs adversary verification, that's
   a new micro-chunk — checkpoint first.

3. **SDK packaging investigations are turn sinks:** If the test agent can't
   find the SDK, don't trace `pythonPackagePath()` and the Dockerfile. The
   fix is always: `cp -r python/agentpaas_sdk <agent-project>/python/`. This
   is documented to prevent re-derivation.

4. **Delegation concurrency budget:** GLM-5.2 has 10 concurrent request
   slots. Orchestrator (1) + delegate_task subagents (≤5) + verifier (1) =
   ≤7 total, leaving 3 slots headroom. Workers and adversary use different
   providers and don't count.

---

## 5. Subtask Completion Log

| Subtask | Description | Status | Key Commits |
|---------|-------------|--------|-------------|
| T01 | Hermes plugin skeleton + 17 tool manifest | DONE | `2697de7`, `0dd8322` |
| T02 | Schema-generated tool wrappers + contract-parity gate | DONE | `9d69c3b`, `630bfba` |
| T03 | Confirmation protocol for trust-boundary actions | DONE | `e6f078c`, `09d7ac3` |
| T04 | Prompt-injection boundary — sanitizer + negative tests | DONE | `a4fbffa`, `d906f28` |
| T05 | E2E deploy acceptance (BUG 7d DENIED probe) | DONE | `3236c1a`→`13e489c` |
| T06 | Prompt-change immutable redeploy | DONE (tested) | `update_e2e_test.go` |
| T07 | Demo matrix fixtures (3 scenarios) | DONE | `ac54377` |
| T08 | /agentpaas slash commands | DONE | `bd5afc9` |
| T09 | Bundled SKILL.md + plugin.yaml | DONE | `bd5afc9` |
| Gate | `make block13-gate` | PASS | `bd5afc9` |

**Adversary tests converted to regressions:** B13-T01 through T04 each had
adversary break-tests written against them. All were hardened and converted
to regression tests that run in CI.

**Plugin test suite:** 109 tests covering tool handlers, sanitizer, contract
parity, confirmation protocol, and adversary regression. All pass.

---

## 6. What Was Deferred to Block 14

| Item | Block | Rationale |
|------|-------|-----------|
| Real-time egress timeline (dashboard SSE) | 14B | Requires harness → daemon streaming; post-run ingestion is sufficient for P1 |
| Path allow-list (GAP-1) | 14A | Plugin currently allows any path; security hardening |
| Binary verification | 14A | Defense-in-depth on the harness binary |
| Output cap on tool results | 14A | Large outputs can flood context |
| Generic MCP server | P2 | Claude Code/Codex/Cursor integration |
| Linux platform support | Later | P1 is Mac-only (locked) |
| Install path (brew, docs) | 14C | Release engineering |
| Demo video assets | 14C | Marketing/release prep |

---

## Appendix: Full B13 Commit Chronology

```
e07f643 docs(b13): lock Hermes plugin spec
2697de7 B13-T01: Hermes plugin skeleton and tool manifest
0d4f9c6 fix(b13-t01): harden handlers, CLI resolver, and schemas
75a522f test(b13-t01): convert adversary break-tests to regression tests
0dd8322 B13-T01: Hermes plugin skeleton (17 tools + register(ctx) + tests)
9d69c3b B13-T02: schema-generated tool wrappers + contract-parity gate
2b00ecd test(b13-t02): adversary break-tests
73cce53 fix(b13-t02): address adversary breaks
630bfba B13-T02: schema-generated tool wrappers (merged)
e6f078c B13-T03: confirmation protocol for trust-boundary actions
7879b51 test(b13-t03): adversary break-tests
fee35d9 fix(b13-t03): harden confirmation protocol
09d7ac3 B13-T03: confirmation protocol (merged)
a4fbffa B13-T04: prompt-injection boundary — sanitizer + negative tests
e3b665e test(b13-t04): adversary break-tests
4df7484 fix(b13-t04): wire sanitizer into handler pipeline
d906f28 B13-T04: prompt-injection boundary (merged)
4510133 fix(b13): env var overwrite + success-path test
247181c fix(b13): daemon startup nil-ProcessState panic (merged)
9095b68 fix(b13): home.Ensure() before daemon.New()
35ebfad fix(b13): resolve CLI relative paths to absolute
cb77041 fix(b13): implement 10 unimplemented control RPC handlers
7ae6545 fix(b13): wire dashboard server into daemon startup
ce99da2 fix(b13): redirect daemon stdout/stderr to log file
8a6146a fix(b13): goroutine leak on daemon start success path
0410bc2 fix(b13): goroutine leak on daemon stop timeout path
d0e4635 fix(b13): dedupe AGENTPAAS_HOME/SOCKET env vars
03aa436 fix(b13): add missing harness binary main entry point
ce17e43 fix(b13): dashboard server.go errors import + grace period
f2054d3 fix(b13): centralize Docker endpoint resolution
096b467 Merge fix/b13-deploy-blockers
f5244cb fix(b13): resolve harness binary + SEC1 private keys
a9db1ba fix(b13): cosign v3 signing-config flag
e4f0343 fix(b13): cosign v3 + local registry + DOCKER_HOST
bd0eb96 Merge fix/b13-pack-sign
8a60233 fix(b13): cosign PKCS8 key format + run registry ref
28d0862 Merge fix/b13-key-run
dc457a8 fix(b13): cross-compile harness linux/arm64 (BUG 6)
8c44dc7 Merge fix/b13-harness-linux
8a3f68a fix(b13): wire audit event recording into pack/run/stop (BUG 7a)
3e8443a Merge fix/b13-audit-events
3f52177 fix(b13): DockerResourceManager for dashboard (BUG 7b)
a4545b8 Merge fix/b13-resource-mgr
a15ceca fix(b13): close goroutine race on d.dashboard
3236c1a fix(b13): wire harness audit appender + egress events + Binds
a479a78 feat(b13): concurrent run limit + execution plan v1.1
901acd9 fix(b13): mount audit volume in Run handler (BUG 7d Step 3)
0ec15e0 Merge fix/b13-audit-volume
319ccee fix(b13): ingest harness audit JSONL on Stop (BUG 7d Step 4)
2a24a34 Merge fix/b13-audit-ingest
abb6362 fix(b13): audit dir 0777 for non-root UID (BUG 7d e2e)
90515a5 fix(b13): chmod 0777 + lowercase docker error strings
528bc5b fix(b13): set AGENTPAAS_AGENT_PATH=/app/main.py
d0113af docs(b13): resume prompt — invoke gap is the blocker
13e489c fix(b13): docker Exec + auto-invoke harness (BUG 7d final)
ac54377 feat(b13): demo matrix fixtures
bd5afc9 feat(b13): slash commands + SKILL.md + block13-gate
813b843 docs(b13): block-end report — COMPLETE
```
