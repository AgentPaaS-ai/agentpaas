# Current State — As of Block 30

**Read this first** when starting work on AgentPaaS. Detailed block specs live
under `docs/execution/blocks/`; product decisions in `Agentpaas-pitch.md`
(private, gitignored); public posture in `README.md` and `docs/trust-model.md`.

**Shipped release:** v0.2.3 (B1–B25)  
**Development head:** B26–B30 implemented; B31 next  
**Gates:** `make block30-gate` (PASS on B30-specific work); golden-fast 19/19
when publisher identity + Hermes profile are present (G44/G47 are env setup,
not code regressions)

---

## What exists in the codebase

### Binaries (`cmd/`)
- `agentpaasd` — local daemon (control API, run lifecycle, audit, dashboard)
- `agentpaas` — CLI (init, policy, pack, run, secret, identity, bundle, trust, …)
- `harness` — in-container agent harness
- `agent` — agent-side helper

### Core packages (`internal/`)
| Area | Package(s) | Role |
|------|------------|------|
| Policy | `policy` | Parse/validate/compile policy.yaml → gateway config |
| Pack / supply chain | `pack`, `bundle`, `export`, `install`, `trust` | Build, sign, export `.agentpaas`, install, TOFU trust |
| Identity | `identity` | Local CA, AID, publisher keys, SPIFFE helpers, keystores |
| Secrets | `secrets` | Keychain/file store, broker, leases, gateway injection |
| Runtime | `runtime`, `adapter/docker`, `adapter/k8s` | Docker (shipped) + K8s adapter ports |
| Ports | `port` | Substrate-neutral contracts + conformance fakes |
| Harness / SDK | `harness` + `python/agentpaas_sdk` | Invoke path, budgets, firewall caps, progress RPC |
| Daemon / CLI | `daemon`, `cli`, `operator` | Control handlers, operator schema, UX |
| Trigger | `trigger` | Invoke, cron, webhook, SSE, durable event store |
| Audit | `audit` | Hash-chained JSONL, checkpoints, SQLite index, verify |
| Durable run | `routedrun`, `supervisor` | Deployments, attempts, leases, journals, supervisor |
| MCP | `mcpmanager` | Local MCP lifecycle (production router still B33) |
| Observability | `otel`, `dashboard`, `logging` | OTLP store, embedded UI, redaction |
| Misc | `home`, `service`, `doctor`, `llm`, `money`, `naming`, `fsutil` | Paths, launchd/systemd, health, providers, money math |

### Integrations
- `integrations/hermes-plugin/` — Hermes tools, SKILL.md, sanitizer, tests
- `demo/weather-agent`, `demo/governed-weather` — reference agents
- `test/golden`, `test/redteam`, `test/compat/v0.2.3` — regression suites

### Key types / features (mental model)
- **Legacy sync path (v0.2.3):** pack → run container+gateway → invoke → audit
- **Durable path (B26–B30):** immutable deployment + alias, invocation IDs,
  attempts/leases, progress journals (HMAC), semantic checkpoints, artifacts,
  active-time ledger, supervisor lifecycle (not fully daemon-wired yet)
- **Sharing (B21–B25):** publisher identity, trust store, signed bundle,
  consent install, fork provenance chains
- **Enforcement:** internal-only Docker network + gateway allowlist; credentials
  brokered (not in agent payload); iptables firewall = defense-in-depth

---

## Gate status

| Check | Status | Notes |
|-------|--------|-------|
| B26–B29 gates | Complete on dev branch | Contracts + profiles + ports |
| `block30-gate` | PASS (B30 codepaths) | Supervisor, liveness, reference worker, longevity (fake clock) |
| Real-time Docker long-run | Deferred | `block28-long` / 6m+20-turn and 30m soak need `AGENTPAAS_DOCKER_TESTS=1` before R30 prerelease |
| Golden-fast | 19/19 with setup | G44 needs Hermes profile; G47 needs `agentpaas identity init` |
| Supervisor ↔ daemon invoke | Not wired | Supervisor package tested in isolation (B30 R4) |

---

## Known deferred items

Authoritative scratch list was `docs/pre-b31-issue-list.md` (removed as stale);
substance lives in `docs/known-limitations.md` and recent block risk notes.

High-signal deferrals:
1. **Real-time Docker long-run proofs** not in default gate (B30 R1)
2. **Supervisor not in daemon invoke path** (B30 R4)
3. **B28 adapter gaps** — partial integration steps, K8s NetworkPolicy/signal/
   Prepare field drops, Docker Fence no-op
4. **B29** — streaming buffer caps, outbox dead code, inbox persist ordering,
   activation egress fence on warm→idle
5. **B27** — artifact ValidateAndAccept not fully production-wired; heartbeat
   fsync policy
6. **govulncheck** — 5 moby/moby findings, upstream Fixed-in N/A
7. **Product gaps** until later blocks: registry promotion (B31), A2A (B32),
   MCP router (B33), pipelines/children (B34–B35), model router/spend (B36–B38)

---

## Crypto security findings (top 5)

From `docs/pre-b31-crypto-security-review.md` (no CRITICAL remote break):

1. **C-01 HIGH** — Checkpoint `ComputeDigest` omits identity/sequence fields
   (`attempt_id`, `run_id`, `lease_id`, …); local retarget/reorder risk
2. **C-02 HIGH** — Pack writes AID private key to temp files for cosign import
   with empty password
3. **C-03 HIGH** — Identity keychain `security(1)` calls lack timeout; key
   material on argv
4. **C-04 MEDIUM** — Custom RFC 6979 ECDSA in bundle writer (maintenance /
   low-S risk)
5. **C-05 MEDIUM** — FileKeyStore symlink check is target-only; parent-component
   symlink swap gap

Prefer fixing C-01/C-02 if B31 touches resume or pack signing.

---

## Next block: B31

**Spec:** `docs/execution/blocks/b31-summary.md`  
**Theme:** Local package registry **read API** + **promotion bit** over existing
B23 installed-agent store and B26 deployment store (reduced scope).  
**Out of B31:** six-state lifecycle, attestations, Agent Cards, capability-schema
matching (post-v0.5).  
**Depends on:** B30 complete / `make block30-gate` green.

### Suggested read order for a new agent
1. This file  
2. `docs/known-limitations.md`  
3. `docs/execution/blocks/b31-summary.md` (if implementing B31)  
4. `docs/execution/blocks/b6-b21-summary.md` (history) + `b26`–`b30` summaries as needed  
5. Package `doc.go` / `README.md` under the package you touch  
6. `docs/pre-b31-crypto-security-review.md` if touching crypto/signing/checkpoints  
