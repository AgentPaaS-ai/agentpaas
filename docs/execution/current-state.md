# Current State — M7.5 HUMAN GATES; v0.3.6 shipped; cloud deploy blocked on R2

**M5–M7:** CLOSED eng
**CLI brew:** **v0.3.6** shipped (local goreleaser) — result/logs/invoke-token
**M7.5 eng:** GREEN (T01–T10 + ADV + residual SSRF). Cloud tests 497.
**Block OPEN** until D113:
- T11 cold-onboard (founder + external)
- T12 live CF soak

**Today (2026-08-01) session:**
- D1 remote migrations through 0015 applied
- `wrangler deploy` **BLOCKED**: CF R2 not enabled (API 10042) — founder must enable R2 in Dashboard
- cloud.agentpaas.ai still DOWN until deploy
- Weather demo already LLM-friendly summary in `answer`

**Next eng after human gates:** M8 run-record CLI verbs
