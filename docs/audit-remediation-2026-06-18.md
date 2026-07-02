# Audit Remediation: 2026-06-18

Source audit:
`/Users/pms88/.codex/attachments/30e21bce-d140-4c2a-b6ea-591be55b9ff8/pasted-text.txt`

Scope: current `agentpaas-execution-plan-v1.md` and
`docs/agentpaas-subtask-decomposition-v1.md`.

## Preflight

- Audit accepted as current: it observed B1-B15 and 90 decomposition tasks.
- Post-remediation task count: 93.
- Reason for count change: B5-T04 was split into B5-T04a through B5-T04d.

## Findings

### M1: B5-T04 too broad

Status: fixed.

Changes:
- Split B5-T04 into:
  - B5-T04a Positive Path and External Canary Probes
  - B5-T04b Host, Loopback, and Docker Bridge Probes
  - B5-T04c Protocol and Namespace Bypass Probes
  - B5-T04d Topology Inspect, Restart, and Partial-Create Cleanup
- Re-scoped B5-T05 to daemon-crash reconciliation and secret-free debug
  output to avoid overlapping B5-T04d.

Verification:
- `rg -n "B5-T04" docs/agentpaas-subtask-decomposition-v1.md`
- `rg -c "^### B[0-9]+-T[0-9]+" docs/agentpaas-subtask-decomposition-v1.md`

### M2: B12 tasks need real pack/run plus Block 11-only path

Status: fixed.

Changes:
- Added an acceptance bullet to every B12 task requiring only real
  `agent pack`, real `agent run`, and real Block 11 operator methods.
- Explicitly disallowed synthetic harnesses, direct daemon shortcuts, and
  test-only enforcement paths.

Verification:
- `rg -n "real \\`agent pack\\`" docs/agentpaas-subtask-decomposition-v1.md`

### S1: Vague edge-case phrasing

Status: fixed for cited tasks.

Changes:
- Replaced B4-T02 broad acceptance with enumerated validation cases.
- Added B4-T03 canonicalizer/digest edge cases.
- Added B4-T05 exact fuzz execution/corpus requirement.
- Split overloaded B5-T04.
- Added explicit B7-T03/B7-T04 negative-test surfaces.
- Renamed and scoped B9-T08.
- Expanded B13-T04 prompt-injection source list.

Verification:
- `rg -n "all edge cases|all bypass|handles all|covers all" docs/agentpaas-subtask-decomposition-v1.md`

### S2: B11 contract freeze before B12/B13

Status: fixed.

Changes:
- Added B11-T01 acceptance requiring versioned schemas, evidence-ref fields,
  and confirmation protocol types to be committed and frozen before B12/B13
  issues are marked ready.
- Added `make block11-gate` as the prerequisite before B12/B13 workers consume
  the schemas.
- Added B11-T05 confirmation protocol fixture coverage.

Verification:
- `rg -n "frozen before any B12 or B13|confirmation protocol fixtures" docs/agentpaas-subtask-decomposition-v1.md`

### S3: B7 Docker inspect and image-layer negative tests

Status: fixed.

Changes:
- Added B7-T03 acceptance requiring raw brokered value absence from agent env,
  `/proc`, filesystem walks, daemon logs, gateway logs, CLI/dashboard errors,
  Docker inspect, compiled config files, build context, exported image layers,
  and packed artifacts.
- Added B7-T04 acceptance requiring direct-lease raw secret values absent from
  Docker inspect, config files, image layers, and packaged artifacts before
  the runtime tmpfs lease is mounted.

Verification:
- `rg -n "Docker inspect|image layers" docs/agentpaas-subtask-decomposition-v1.md`

### N1: B9-T08 scope

Status: fixed.

Changes:
- Renamed B9-T08 to `Control API REST/JSON Fuzzing`.
- Clarified Trigger API payload-size and idempotency behavior are covered by
  B9-T03, and broad Trigger API protocol fuzzing is not in P1.

Verification:
- `rg -n "Control API REST/JSON|Trigger API payload-size" docs/agentpaas-subtask-decomposition-v1.md`

### N2: B14 evidence artifact location

Status: fixed.

Changes:
- Added B14-T05 evidence path: `docs/release/volunteer-evidence/`.
- Added B14-T07 evidence path: `docs/release/offline-verification/`.

Verification:
- `rg -n "docs/release/volunteer-evidence|docs/release/offline-verification" docs/agentpaas-subtask-decomposition-v1.md`

## Additional Clarification

While applying the audit, B3-T06 was also tightened to restate that second
machine bundle verification proves bundle integrity only, not global
transparency-log anchoring.

## Final Checks

Run:

```sh
rg -c "^### B[0-9]+-T[0-9]+" docs/agentpaas-subtask-decomposition-v1.md
rg -n "all edge cases|all bypass|handles all|covers all" docs/agentpaas-subtask-decomposition-v1.md
git diff --check
```

## Follow-Up Audit: 2026-06-18

Source audit:
`/Users/pms88/.codex/attachments/7b4fec93-0674-4d09-9820-d624d0aff58c/pasted-text.txt`

Status: verified and tightened.

The follow-up audit found no MUST items and listed two SHOULD items:

- B11 contract-freeze wording.
- B14 volunteer evidence artifact location.

Both items were already present after the first remediation pass. To avoid
future ambiguity, the B11-T01 acceptance bullet now explicitly says
`Operator method surfaces, versioned JSON schemas, evidence-ref fields, and
confirmation protocol types`, and B14-T05 now names
`docs/release/volunteer-evidence/` in both Scope and Acceptance.
