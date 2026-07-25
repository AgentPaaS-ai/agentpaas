# Current State — Block 34 in progress (T04 chunk1 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01 Freeze workflow/handoff conformance fixtures | DONE |
| T02 Strict pipeline compilation | DONE |
| T03 SDK input/handoff ops | DONE |
| T04 Durable linear scheduler | IN PROGRESS (chunk1 library CAS DONE; chunk2+ next) |
| T05–T09 | pending |

## T04 chunk1 evidence
- Controller: Seed/Claim/Ack/CommitSuccess/CommitFailure on MemoryStore
- 9 controller tests + full pipeline package race PASS
- docs/owa-records/b34-t04-chunk1.md
- No Docker/daemon wire yet

## Next
T04 chunk2: PAUSE_REQUESTED desired-state seam + no next launch; crash/idempotency matrix expansion
