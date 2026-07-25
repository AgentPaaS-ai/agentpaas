# Current State — Block 34 in progress (T07c1 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01–T06 | DONE |
| T07 Failure/cancel/pause/idempotency | IN PROGRESS (c1 fail+cancel DONE; c2 resume next) |
| T08 Deploy invoke + operator inspection | pending |
| T09 block34-gate + adversary | pending |

## Evidence
- T07c1: CancelWorkflow, late success reject, FailureReason; fault_test 11 cases; -race PASS
- docs/owa-records/b34-t07-chunk1.md
- Admission still gated

## Next
T07c2: Resume after PAUSED; pause/cancel races; repeated resume
