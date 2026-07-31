# Current State — M7.5 IN PROGRESS

**M5:** CLOSED
**M6-lite:** CLOSED eng (no Stripe)
**M7:** CLOSED eng — triggers + ADV + OSS cloud invoke-token / invoke (07fd802)
**M7.5:** IN PROGRESS — Customer Readiness Regression
  Spec: `docs/execution/m7.5/block-spec.md`. Adversary required.
  T01 DONE (cloud 557e462). T02 DONE (ca986cb, 454 tests). T01–T03 DONE (output loop). Parallel: T04/T07 OSS, T08 RL, T10 preview vault.

**Next eng after M7.5:** M8 (rescoped — run-record CLI verbs only; depends M7.5)
**Human:** M6.5 Stripe GO; W1/UI plan; live CF with $ cap (T12)
**Process:** D112 deferrals→D-entries; D113 customer gates non-deferrable;
  D114 preview vault; D115 no commercial tenant-count gate
