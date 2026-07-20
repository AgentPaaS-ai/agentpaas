# B29 Block Record — Agent Runtime Profiles, Durable Events, Streaming

**Status:** ALL 8 TASKS MERGED, block29-gate PASS
**Date:** 2026-07-19
**Base:** 836ca7a (B28 complete)
**Head:** e0a8e0c

## Task summary

| Task | Title | Worker | Duration | Status |
|------|-------|--------|----------|--------|
| T01 | Streaming/lifecycle characterization | deepseek-v4-pro | 12m42s | COMPLETE (755eb9d) |
| T02 | Runtime profile and normalized envelopes | glm-5.2 | 15m36s | COMPLETE (4a325e2) |
| T03 | Durable event store/outbox/subscriptions | glm-5.2 | 35m29s* | COMPLETE (c673ec2) |
| T04 | Governed model streaming | glm-5.2 | 29m24s* | COMPLETE (ec972da) |
| T05 | Interactive inbox and suspend/wake | glm-5.2 | 20m43s | COMPLETE |
| T06 | Activation policies | glm-5.2 | 8m27s | COMPLETE |
| T07 | Performance conformance harness | glm-5.2 | 21m38s | COMPLETE |
| T08 | Integrated adversary and gate | glm-5.2 | 17m10s* | COMPLETE (e0a8e0c) |

*T03, T04, T08 hit iteration budget (90/90) — committed work was complete,
orchestrator verified and merged.

## What B29 delivers

1. **Characterization tests** (T01): 29 tests freezing pre-B29 behavior
   (buffered LLM, synthetic InvokeStream, in-memory EventBus, cold-per-run).
2. **Runtime Profile** (T02): versioned schema with baseline v0.3 features,
   optional feature negotiation, fail-closed validation, no best-effort
   downgrade. ModelCallEnvelope with multi-role messages, reasoning controls,
   provider extensions.
3. **Durable Event Store** (T03): WAL-backed EventStore implementing the B28
   frozen port interface. Atomic outbox, cursors, replay, subscriber overflow
   handling (closes channel, never silently drops), cross-tenant isolation,
   restart proof.
4. **Governed Model Streaming** (T04): streaming event types, guardrail modes
   (buffered_release / incremental_release), streaming harness adapter with
   budget enforcement, backpressure (5s timeout), cancellation, partial output
   marked uncommitted. Python SDK llm_stream yielding governed StreamEvents.
5. **Interactive Inbox** (T05): durable inbox store, wake signals, approval
   protocol, disconnect/reconnect without polling, input content cannot expand
   authority.
6. **Activation Policies** (T06): on_demand/warm/resident validation,
   zero-authority idle state invariant, activation lifecycle transitions,
   default policy (on_demand, scale-to-zero).
7. **Performance Harness** (T07): timing spans (p50/p95/p99), resource
   metrics, perf harness, baseline JSON report.
8. **Adversary** (T08): 14 adversary tests (slow consumer, replay, forged
   cursor, cross-tenant, partial stream, cleanup) + block29-gate Makefile.

## Gate verification

`make block29-gate`: PASS
- B28 cumulative gate: PASS
- runtime/trigger/harness race tests: PASS
- adversary tests: 14/14 PASS
- golangci-lint: 0 issues (fresh cache)
- Python SDK tests: 32/32 PASS

## Remaining for block completion

- [ ] Architecture review (ap-thinker / Kimi-K3, interactive)
- [ ] Verifier (ap-verifier / grok-composer-2.5-fast)
- [ ] Human manual testing via ap-testing profile
- [ ] Push to GitHub
