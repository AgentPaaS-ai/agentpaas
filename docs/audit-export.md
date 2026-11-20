# Audit Export and Verification

AgentPaaS maintains a tamper-evident audit trail for every governed action.
This guide covers exporting audit records and verifying integrity on a second
machine — the workflow a security reviewer uses to trust run evidence.

See [how-enforcement-works.md](how-enforcement-works.md) for what gets
audited and [threat-model.md](threat-model.md) for tamper controls.

## Overview

The daemon appends events to a hash-chained JSONL log. Each record links to
its predecessor via SHA-256 hashes over canonical JSON. You can export a
run's audit trail and verify the chain independently of the originating
machine.

## Export a run's audit trail

List recent audit entries to find the run ID:

```bash
agentpaas audit query
```

Export records for a specific run:

```bash
agentpaas audit query --run-id <run-id>
agentpaas audit export --output audit.jsonl
```

The export writes one JSON object per line (JSONL). Each record includes:

| Field | Description |
|---|---|
| `seq` | Monotonically increasing sequence number (1-based). |
| `prev_hash` | SHA-256 hex of the previous record's canonical JSON. |
| `record_hash` | SHA-256 hex of this record's canonical JSON. |
| `timestamp` | RFC 3339 event timestamp. |
| `event_type` | Event kind (e.g. egress decision, run lifecycle). |
| `actor` | Identity that triggered the event. |
| `payload` | Structured event data. |

Transfer `audit.jsonl` to the reviewer's machine by any secure channel (USB,
encrypted file share, etc.).

## Verify the hash chain

On the second machine (with `agentpaas` installed):

```bash
agentpaas audit verify --file audit.jsonl
```

A successful verification confirms:

- The genesis record (seq=1) has an empty `prev_hash`.
- Each subsequent record's `prev_hash` matches the previous record's
  `record_hash`.
- Each `record_hash` recomputes correctly from the record's canonical JSON
  (sorted map keys, `record_hash` field excluded from the hash input).

Example genesis record:

```json
{
  "seq": 1,
  "prev_hash": "",
  "record_hash": "a3f2...",
  "timestamp": "2026-06-26T12:00:00Z",
  "event_type": "run_started",
  "deployment_mode": "local",
  "actor": "agent",
  "payload": {}
}
```

Example chained record:

```json
{
  "seq": 2,
  "prev_hash": "a3f2...",
  "record_hash": "b7c1...",
  "timestamp": "2026-06-26T12:00:05Z",
  "event_type": "egress_denied",
  "deployment_mode": "local",
  "actor": "gateway",
  "payload": {"domain": "evil.example.com", "reason": "policy_denied"}
}
```

## What verification detects

| Tamper type | Detected? |
|---|---|
| Modified record content | Yes — `record_hash` mismatch |
| Reordered records | Yes — `prev_hash` chain break |
| Inserted middle record | Yes — chain break at insertion point |
| Truncated tail records | **No** in P1 — see limitation below |

## Signed export bundles

Full daemon exports may include a signed manifest and checkpoint signatures
bound to the daemon audit key fingerprint shown by `agentpaas doctor`. Bundle
verification checks checkpoint signatures in addition to the hash chain.

## P1 limitation: tail truncation

If an attacker removes the last N records from an exported JSONL file, the
remaining prefix chain is still valid. Detecting deletion requires signed
checkpoint anchors (P2). See
[known-limitations.md](known-limitations.md#hash-chain-record-deletion-detection).

## Typical review workflow

1. Operator completes a governed run and exports `audit.jsonl`.
2. Operator shares the file plus the `agentpaas doctor` key fingerprint.
3. Reviewer runs `agentpaas audit verify --file audit.jsonl` on a clean machine.
4. Reviewer inspects `egress_denied`, `egress_allowed`, and credential events
   in the payload fields to confirm policy compliance.

## Related docs

- [Quickstart](quickstart.md) — Step 10: check the audit trail
- [How enforcement works](how-enforcement-works.md) — what triggers audit events
- [Known limitations](known-limitations.md) — truncation undetectability
- [Threat model](threat-model.md) — audit tampering controls