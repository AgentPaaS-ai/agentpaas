# M0 B30 Operator Soak — GATE PASS

**Date:** 2026-07-29
**HEAD:** cd74d56 (main after ff-merge feat/m0-b30-soak)
**Gate:** `make block30-soak-gate` EXIT:0

## Evidence (local docs/execution/m0/ — D90 not git-added)

| Run | wall_seconds | turns | pass | notes |
|-----|-------------:|------:|:----:|-------|
| real-daemon-restart-1 | 1831.55 | 1809 | true | SC1/SC2 OK |
| real-daemon-restart-2 | 1831.43 | 1808 | true | SC1/SC2 OK |
| real-daemon-restart-3 | 1832.01 | 1809 | true | SC1/SC2 OK |
| real-worker-sigkill-1 | 55.68 | 155 | true | docker kill 137 cid f9511f62cbea |
| real-worker-sigkill-2 | 57.20 | 154 | true | docker kill 137 cid fa1d1b04b10d |
| real-worker-sigkill-3 | 52.53 | 154 | true | docker kill 137 cid 8342ac7a6f1d |

Log: /tmp/block30-soak-gate-full.log

## Key fixes on branch
- Supervisor daemon wiring (ClaimForRun + Reconcile)
- Durable TimeEnvelope → urlopen + daemon + harness session timeouts (not StallTimeoutMs)
- Real operator tests (agentpaasd PID + docker kill)
- PID kill scoped to test daemon
- Inter-soak Docker cleanup race

## Adversary
`go test ./internal/supervisor/ -run TestAdversary_B30` — 19/19 PASS

## Remaining (founder)
Founder manual: re-run `make block30-soak-gate` or Hermes long agent + kill -9 daemon mid-run.
