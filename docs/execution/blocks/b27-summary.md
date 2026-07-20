# Block 27 — SDK Progress, Checkpoint, and Artifact Protocol

**Status:** IMPLEMENTED — retained as the binding completion record
**Date:** 2026-07-16
**Last reconciled:** 2026-07-18
**Target release:** v0.3.0
**Depends on:** B26 complete and `make block26-gate` green
**Must complete before:** B30, B39, and B40
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D33

**Post-block compatibility:** Approved decisions D34–D65 apply prospectively.
B28/B29 must expose these completed progress/checkpoint/artifact records through
portable tenant-aware event/artifact ports; B32 adds inter-agent artifact
grants without weakening B27 path, digest, lease, or checkpoint invariants.

## Outcome

B27 gives AgentPaaS-generated workers one small, portable recovery contract:

```python
agent.progress(
    phase="...",
    completed_work=[...],
    remaining_work=[...],
    artifact_references=[...],
    last_committed_action="...",
    safe_to_resume=True,
)
```

At block completion:

- A worker can heartbeat and describe semantic progress.
- Safe progress becomes a durable checkpoint tied to workflow, node, run,
  attempt, and lease.
- A run has a bounded artifact workspace that survives between attempts.
- Artifact references are path-validated, hashed, and recorded without
  placing artifact contents in checkpoints.
- The daemon receives authenticated progress records and updates B26 state.
- A resumed attempt can obtain the latest checkpoint through the first
  `agent.progress(...)` response.
- The same checkpoint can later serve as an operator-requested pause boundary;
  B27 never claims that freezing an arbitrary container is resumable.
- Existing agents that never call `agent.progress(...)` still run unchanged,
  but are honestly reported as lacking safe whole-worker resume.

B27 does not implement watchdogs, automatic model failover, or continuation.
B30 consumes this protocol for long-running supervision; B39 consumes it for
routed continuation.

## Locked SDK contract

### Method signature

The Python SDK method is:

```python
def progress(
    phase: str,
    *,
    completed_work: list[str] | None = None,
    remaining_work: list[str] | None = None,
    artifact_references: list[str] | None = None,
    last_committed_action: str | None = None,
    safe_to_resume: bool = False,
) -> dict[str, Any]:
    ...
```

All arguments except `phase` are keyword-only.

The returned object contains:

```text
recorded
workflow_id
node_id
run_id
attempt_id
checkpoint_id
lease_expires_at
resume_checkpoint
resume_reason
```

`resume_checkpoint` is absent or `null` on an initial attempt. On a resumed
attempt it contains the latest accepted semantic checkpoint. Returning it on
every successful progress call is acceptable; the stable checkpoint ID lets
worker code avoid applying it twice.

`resume_reason` is absent on an initial attempt and otherwise is trusted enum
`failure_continuation|operator_pause_resume`. Trigger input and worker code
cannot set it.

### Heartbeat versus checkpoint

- Every valid `agent.progress(...)` call is a heartbeat/activity event.
- A durable resume checkpoint is created only when `safe_to_resume=True`.
- `safe_to_resume=True` requires non-empty `last_committed_action` and at
  least one non-empty `completed_work` entry. `remaining_work` may be empty at
  a final committed boundary.
- A heartbeat with `safe_to_resume=False` never becomes a claimed safe resume
  point.

The runtime validates only this form, bounds, artifact integrity, and lease
state. It does not judge whether the worker’s checkpoint description is
truthful or semantically sufficient; `safe_to_resume` remains an explicit
worker assertion.

### Completion and failure

- Returning from `@agent.on_invoke` remains successful worker completion.
- Raising an exception remains worker failure.
- `agent.progress(...)` does not mark the task complete.
- AgentPaaS does not infer semantic correctness from a checkpoint.

### Bounds

Use fixed v0.3 safety bounds:

- `phase`: 1–128 UTF-8 characters.
- `completed_work`: at most 50 entries, 1,024 characters each.
- `remaining_work`: at most 50 entries, 1,024 characters each.
- `artifact_references`: at most 32 entries.
- `last_committed_action`: at most 1,024 characters.
- Serialized semantic checkpoint: at most 64 KiB.

Control characters, invalid UTF-8, values matching registered secret
fingerprints/configured test sentinels, and oversized values fail with a typed
SDK/RPC error. Error output is redacted.

### Artifact workspace

The runtime mounts a dedicated run-level workspace:

```text
/workspace/artifacts
```

Workers reference artifacts by relative path, for example:

```text
customer-feedback-report.md
charts/themes.json
```

Artifact references use POSIX `/` separators, at most 512 characters and
eight path segments. Each segment matches
`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. Reject empty/dot segments, backslashes,
Unicode/confusables, and case-fold collisions so behavior is stable on the
default macOS filesystem.

The host store is:

```text
~/.agentpaas/state/runs/<run_id>/artifacts/
```

v0.3 limits:

- 25 MiB per artifact.
- 100 MiB total per run.
- Artifact references resolve to regular files only; directories may exist
  only as parents and are not checkpointed as one opaque artifact.
- No symlinks, devices, sockets, FIFOs, absolute paths, traversal, or hard
  links.
- Artifact metadata records relative path, bytes, media type when known,
  SHA-256 digest, creating attempt, and last update.

The artifact contents are not copied into checkpoint JSON, logs, model
prompts, or audit payloads.

The limits apply to files accepted into the durable artifact set. The runtime
must also monitor the mounted tree, reject/stop quota abuse, and remove
unreferenced files during fencing/finalization; it must not imply that the
artifact quota is a general container-disk quota.

## Authenticated progress journal

The untrusted Python worker can access its writable mounts. Progress used for
leases and recovery must therefore not be accepted from an unsigned file that
agent code could forge.

B27 creates a per-attempt authenticated progress journal:

1. The daemon generates a random attempt journal key.
2. The daemon persists it in a daemon-private `0600` attempt-secret file
   outside every worker mount. It is never serialized into run/attempt JSON.
   This lets restart reconciliation authenticate journal records already on
   disk.
3. The key is delivered only to the trusted harness through a one-time
   protected startup file descriptor or file before the Python worker starts.
4. The harness closes/removes that startup material and never passes the key
   to the child process or SDK.
5. The daemon removes the durable key only after the attempt is terminal,
   every accepted journal record is durably ingested, and the terminal journal
   digest is committed to audit. Crash before deletion is idempotently
   reconciled.
6. Each progress record contains schema version, workflow ID, node ID, run ID,
   attempt ID, lease ID, monotonic sequence, timestamp, semantic checkpoint
   digest, artifact metadata digest, and HMAC.
7. The daemon tailer verifies sequence and HMAC before updating durable state
   or liveness.
8. Invalid, replayed, reordered, or late records fail closed and generate an
   audit event.

The journal key is an internal attempt-authentication value, not a provider
credential. It must still be protected from logs, environment output, agent
code, and artifacts.

B27 extends the B26 host layout with:

```text
~/.agentpaas/state/runs/<run_id>/
  journals/<attempt_id>.jsonl
  attempt-secrets/<attempt_id>.journal-key
```

Both directories are `0700`; files are `0600`. Neither path is mounted into
the artifact workspace. `attempt-secrets` is never mounted. The journal may be
mounted at a harness path; direct Python mutation is still treated as
possible, untrusted, and detectable through HMAC/sequence verification. Only
the one-time harness key delivery is exposed to the container, and it is
removed before Python starts.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Python SDK progress contract | B26 | SDK unit/adversary tests and legacy tests pass |
| 2 | T02 Harness progress RPC and authenticated journal | T01 | signed records, dedupe, bounds, and redaction pass |
| 3 | T03 Daemon journal ingestion and checkpoint persistence | T02 | authenticated live ingestion updates B26 store |
| 4 | T04 Durable artifact workspace and metadata | T03 | path, quota, digest, retry-mount tests pass |
| 5 | T05 Resume checkpoint delivery | T03, T04 | resumed harness returns latest safe checkpoint |
| 6 | T06 Reference worker pattern and Hermes authoring fixture | T01–T05 | generated worker emits valid checkpoints without extra APIs |
| 7 | T07 Block gate and adversary review | T01–T06 | `make block27-gate` passes |

## T01 — Implement the Python SDK progress contract

### Goal

Expose the approved recovery API without changing existing invoke, LLM, HTTP,
or MCP behavior.

### Required work

1. Add `Agent.progress(...)` with the locked signature.
2. Normalize `None` collections to empty lists without sharing mutable
   defaults.
3. Validate types and bounds before making RPC.
4. Generate an opaque per-call progress event ID for idempotent retries.
5. Call RPC method `progress`.
6. Map new typed RPC errors:
   - `INVALID_PROGRESS`
   - `CHECKPOINT_REJECTED`
   - `ARTIFACT_REJECTED`
   - `LEASE_EXPIRED`
7. Preserve existing `BudgetExceeded` behavior.
8. Add docstrings and examples that:
   - Emit progress after a committed phase.
   - Do not include secrets or raw artifact contents.
   - Set `safe_to_resume=True` only after side effects are committed and
     understood.
   - Treat `resume_checkpoint` as explicit input, not hidden memory.
9. Do not add a required `agent.resume()` or second checkpoint API.

### Likely files

- `python/agentpaas_sdk/agent.py`
- `python/agentpaas_sdk/_rpc.py`
- `python/agentpaas_sdk/__init__.py`
- `python/agentpaas_sdk/tests/test_agent.py`
- new focused progress/adversary tests

### Tests to write first

- Full argument forwarding.
- Keyword-only enforcement.
- Empty/minimum valid heartbeat.
- Safe checkpoint field requirements.
- Every bound and wrong-type negative.
- Progress event ID uniqueness.
- Typed RPC error mapping.
- Resume checkpoint response shape.
- Existing SDK test suite unchanged.

### Exit gate

The SDK surface is stable, documented, and independently testable with a fake
RPC client.

## T02 — Add harness progress RPC and authenticated journal

### Goal

Accept progress only through the trusted SDK-to-harness channel and produce
authenticated records for the daemon.

### Required work

1. Extend the harness RPC dispatcher with `progress`.
2. Add run, attempt, lease, lease-expiry, journal path, and journal-key
   metadata to the trusted invoke state.
3. Load journal metadata before starting Python and strip it from child
   environment/config.
4. Revalidate every SDK field in Go; never trust Python validation alone.
5. Redact or reject registered secret fingerprints and configured test
   sentinels before persistence; do not claim semantic secret detection.
6. Validate referenced paths lexically at RPC time; T04 performs filesystem
   validation and hashing.
7. Create an append-only journal writer with:
   - Monotonic sequence.
   - Event ID deduplication.
   - Canonical JSON.
   - HMAC-SHA-256.
   - fsync after each safe checkpoint; bounded batching is allowed for
     heartbeat-only events only if a crash test proves no false checkpoint.
8. Emit a normal harness audit event containing only non-secret summary and
   the authenticated journal sequence/digest.
9. Return current attempt metadata and the B39-provided resume checkpoint.
10. Reject progress after the invoke ends or when the local lease expiry has
    passed.

### Likely files

- `internal/harness/rpc_server.go`
- new `internal/harness/progress.go`
- harness config/startup files
- `cmd/harness/main.go`
- harness tests

### Tests to write first

- Valid heartbeat and checkpoint journal records.
- HMAC known-vector and canonicalization tests.
- Duplicate event ID is idempotent.
- Reordered sequence rejected.
- Missing/invalid journal key fails startup before Python.
- Journal key absent from worker environment, stdout, stderr, audit, and
  errors.
- Late/post-invoke progress rejected.
- Concurrent progress calls serialize correctly.
- Fuzz progress parameters.

### Exit gate

Only the harness can produce an accepted journal record, and the Python
process cannot obtain the signing key through supported interfaces.

## T03 — Ingest progress and persist checkpoints in the daemon

### Goal

Convert authenticated journal records into durable B26 state and live events.

### Required work

1. Extend the current audit tailer or add a dedicated progress tailer that:
   - Reads appended records incrementally.
   - Verifies HMAC and strict monotonic sequence.
   - Verifies run/attempt/lease identity against the store.
   - Rejects records after lease revocation.
2. Persist heartbeat/activity timestamps on the attempt.
3. Persist the latest semantic progress for every valid record.
4. For `safe_to_resume=True`:
   - Generate a checkpoint ID.
   - Store canonical checkpoint JSON atomically under `checkpoints/`.
   - Store its digest and reference on the attempt.
   - Never mutate an existing checkpoint.
5. Publish sanitized progress/checkpoint events to:
   - Daemon audit chain.
   - Event bus/timeline.
   - Operator attempt report.
6. Preserve ordering between progress and terminal attempt state.
7. On malformed/tampered journal:
   - Stop accepting new progress for that attempt.
   - Record `progress_journal_invalid`.
   - Mark resume capability `none` until a later valid design explicitly
     repairs it; do not guess.
8. Keep heartbeat ingestion low overhead; a worker may report every few
   seconds.

### Likely files

- `internal/daemon/audit_tailer.go` or new progress tailer
- `internal/daemon/control_handlers.go`
- `internal/routedrun/**`
- `internal/trigger/eventbus.go`
- operator timeline/summarize handlers

### Tests to write first

- Valid live append updates attempt state.
- Safe checkpoint file/digest/reference.
- Heartbeat-only event does not create checkpoint.
- Tampered HMAC, sequence gap, duplicate, and replay.
- Record for wrong run/attempt/lease.
- Terminal/progress race.
- Daemon restart resumes reading without double-applying.
- Audit and timeline contain sanitized summary only.

### Exit gate

A checkpoint is durable on the host before the daemon reports it as available,
and a forged worker-written journal line cannot extend liveness or create a
checkpoint.

## T04 — Add the bounded artifact workspace

### Goal

Preserve explicit work products between attempts without mounting arbitrary
host paths.

### Required work

1. Create the run-level artifact directory before an attempt container.
2. Bind it to `/workspace/artifacts` read-write.
3. Expose only the non-secret path constant
   `AGENTPAAS_ARTIFACT_DIR=/workspace/artifacts` to the Python worker.
4. On each progress record with artifact references:
   - Normalize relative paths.
   - Open beneath the artifact root without following symlinks.
   - Verify regular-file type and reject links.
   - Enforce per-file and total quota.
   - Hash the file and store metadata.
5. Reject references to files still changing during hash; the worker must
   commit/close them before checkpoint.
6. Use the same artifact directory on a later attempt only after B39 fences
   the old attempt. B27 provides the mount helper and tests with sequential
   simulated attempts.
7. Exclude artifact contents from trigger payload, checkpoint JSON, logs,
   model-call replay, and ordinary audit.
8. Include artifact digests in the checkpoint so resume code can detect
   unexpected mutation.
9. Monitor total tree usage while an attempt is active and fail the attempt on
   quota abuse. On fencing/finalization, delete files that were never accepted
   into durable artifact metadata.

### Likely files

- `internal/routedrun/artifacts.go`
- daemon container/mount construction
- runtime bind validation helpers
- artifact tests

### Tests to write first

- Nested regular file happy path.
- Absolute/traversal/symlink/hard-link/device/FIFO/socket negatives.
- File replacement race during hash.
- Per-file and total quota.
- Unicode normalization/collision.
- Artifact changed after checkpoint produces digest mismatch.
- Sequential attempt remount sees the same committed file.
- Secret sentinel content is not copied to metadata or logs.

### Exit gate

Only bounded files beneath the run artifact root can be referenced, and their
identity is protected by digest.

## T05 — Deliver the latest safe checkpoint to a resumed attempt

### Goal

Define the resume data path B39 will activate without injecting checkpoint
state into user trigger payloads.

### Required work

1. Add optional trusted `resume_checkpoint` and `resume_reason` fields to
   harness invoke state.
2. Load the checkpoint from the B26 store by ID and verify:
   - Run and prior attempt relationship.
   - Checkpoint digest.
   - Artifact digests.
   - Policy/image/catalog snapshot compatibility.
3. Pass it to the harness, not the Python trigger payload.
4. Return it from `agent.progress(...)`.
5. Include:
   - Checkpoint ID and source attempt.
   - Semantic fields.
   - Artifact metadata.
   - Mechanical summary required for safe continuation.
6. Never include:
   - Hidden reasoning.
   - Raw provider response fragments.
   - Credential values.
   - Raw artifact contents.
   - Provider cache/continuation IDs.
7. A missing, invalid, or incompatible checkpoint returns
   `CHECKPOINT_UNAVAILABLE` and no resume claim.

### Tests to write first

- Initial attempt returns no resume checkpoint.
- Simulated resumed/replacement attempt returns the latest accepted safe
  checkpoint.
- Latest heartbeat-only progress is ignored for resume.
- Tampered checkpoint/artifact digest rejected.
- Policy/image/catalog mismatch rejected.
- Trigger payload cannot spoof resume checkpoint.
- Trigger payload cannot spoof resume reason; an operator-pause resume uses
  the same validated checkpoint without resetting recovery counters.
- Checkpoint response size and secret bounds.

### Exit gate

The protocol is ready for B39 continuation while no B27 path independently
starts another attempt.

## T06 — Add the reference worker and Hermes authoring fixture

### Goal

Prove the contract is practical for a general worker without building the
final public demo.

### Required work

1. Add a small deterministic reference worker that:
   - Reads a local fixture.
   - Executes at least three phases.
   - Writes one artifact.
   - Calls `agent.progress(...)` after each committed phase.
   - Restores from a supplied resume checkpoint when present.
   - Returns a final structured result.
2. Keep model calls fake in this block.
3. Add a generated-worker code fixture used by future Hermes tests.
4. Add authoring guidance to the existing AgentPaaS skill source, clearly
   marked as an internal v0.3 contract until B40 enables the user flow.
5. Show the safe pattern:

```python
resume = agent.progress(phase="starting").get("resume_checkpoint")
if resume:
    restore_explicit_state(resume)

# commit work
agent.progress(
    phase="themes_complete",
    completed_work=["theme analysis"],
    remaining_work=["write report"],
    artifact_references=["themes.json"],
    last_committed_action="wrote themes.json",
    safe_to_resume=True,
)
```

6. Do not teach any control client to continue or pause/resume a run yet; B39
   activates those transitions.

### Tests to write first

- Reference worker completes from scratch.
- Simulated second invocation skips committed work and resumes explicitly.
- It does not repeat its committed artifact action.
- It handles no resume checkpoint.
- Generated fixture uses only the approved SDK method.

### Exit gate

The contract is understandable and sufficient for one realistic phased worker
without framework-specific checkpoint machinery.

## T07 — Block gate and adversary review

### Required `make block27-gate`

Run:

```text
make block26-gate
go test ./internal/harness/... ./internal/routedrun/... ./internal/daemon/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Add a deterministic container test for artifact mount and authenticated
progress ingestion when Docker is available.

### Required adversary matrix

- Direct worker write of a forged progress record.
- Journal key discovery through env, `/proc`, stdout/stderr, exception, audit,
  or artifact.
- HMAC replay, truncation, reordering, and sequence reset.
- Oversized and deeply nested checkpoint.
- Secret/API-key/private-key checkpoint content.
- Artifact absolute path, traversal, symlink swap, hard-link escape, device,
  and quota abuse.
- Checkpoint/artifact digest mismatch.
- Trigger payload resume spoofing.
- Progress after invoke completion.
- Concurrent progress calls.
- Unsafe `safe_to_resume=True` with no committed action.

### Block success gate

B27 is complete only when:

1. `make block27-gate` passes.
2. Existing agents run without progress calls.
3. Progress and safe checkpoints are durably distinct.
4. The daemon accepts only authenticated journal records.
5. Artifact references are bounded and digest-verified.
6. A simulated resumed attempt receives the latest safe checkpoint through
   `agent.progress(...)`.
7. No second mandatory SDK recovery API was introduced.
8. No public claim says whole-worker retry works before B39.

## Handoff record required after every task

Append:

- Task ID and date.
- SDK/schema decisions.
- Files changed.
- Tests added first.
- Exact commands and PASS output.
- Journal/artifact adversary result.
- Compatibility impact.
- Open risks.
- Next task unblocked.

## Pitfalls

- Heartbeat is not automatically a safe checkpoint.
- `safe_to_resume=True` is a worker assertion, not proof of correctness.
- Never put raw artifacts in checkpoint JSON.
- Do not mount the user’s project or home directory as the artifact workspace.
- Do not trust a worker-writable unsigned file for liveness.
- Do not pass the journal key to Python through environment or RPC.
- Do not make resume checkpoint part of user trigger payload.
- Do not implement B30’s long-running watchdog or B39’s routed continuation
  inside this block.
