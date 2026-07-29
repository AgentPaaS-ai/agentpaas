# M0 status (orch) 2026-07-29 ~13:02

**Branch:** feat/m0-b30-soak @ f288bff
**Gate:** RUNNING (2nd full attempt) pid 82820
**Log:** /tmp/block30-soak-gate-full.log

## Prior full attempt
- r1 PASS 1831s / r2 PASS 1830s / r3 FAIL SIGTERM@10s (cleanup race)
- fix f288bff: defer cleanup + inter-run Makefile cleanup + per-run evidence

## Fixes on branch
- c7cfc9c harness session timeout active+lease not stall
- e02443b+ daemon outer timeouts
- d6070cd PID kill scoped
- f288bff inter-soak cleanup race

## Remaining after gate PASS
adversary SC, truth-sync, merge main, founder manual
