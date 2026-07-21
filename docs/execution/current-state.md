# Current State — As of Block 31

**Read this first** when starting work on AgentPaaS. Detailed block specs live
under `docs/execution/blocks/`; product decisions in `Agentpaas-pitch.md`
(private, gitignored); public posture in `README.md` and `docs/trust-model.md`.

**Shipped release:** v0.2.3 (B1–B25)  
**Development head:** B26–B31 implemented; B32 next  
**Gates:** `make block31-gate` PASS (MAKE_EXIT=0); golden-fast 19/19 with
publisher identity + Hermes profile present

---

## What exists in the codebase

### Binaries (`cmd/`)
- `agentpaasd` — local daemon (control API, run lifecycle, audit, dashboard)
- `agentpaas` — CLI (init, policy, pack, run, secret, identity, bundle, trust, registry, …)
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
| Registry | `registry` | Joined install+deployment view, promote/demote, workflow promotion gate |
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
- **Registry (B31):** `registry list/show/promote/demote`; daemon ListRegistry/
  ShowRegistry; workflow must name promoted packages at pack/deploy admission
- **Sharing (B21–B25):** publisher identity, trust store, signed bundle,
  consent install, fork provenance chains
- **Enforcement:** internal-only Docker network + gateway allowlist; credentials
  brokered (not in agent payload); iptables firewall = defense-in-depth

---

## Gate status

| Check | Status | Notes |
|-------|--------|-------|
| B26–B30 gates | Complete on dev branch | Contracts + profiles + ports + supervisor |
| `block31-gate` | PASS | Registry, promotion, daemon RPC, adversary, golden-fast 19/19 |
| Real-time Docker long-run | Deferred | `block28-long` before R30 / pre-v0.3.0 |
| Supervisor ↔ daemon invoke | Not wired | Supervisor package tested in isolation (B30 R4) |

---

## Known deferred items

High-signal deferrals:
1. **Real-time Docker long-run proofs** not in default gate (B30 R1)
2. **Supervisor not in daemon invoke path** (B30 R4)
3. **B28 adapter gaps** — partial integration steps, K8s NetworkPolicy/signal/
   Prepare field drops, Docker Fence no-op
4. **B29** — streaming buffer caps, outbox dead code, inbox persist ordering,
   activation egress fence on warm→idle
5. **B27** — artifact ValidateAndAccept not fully production-wired; heartbeat
   fsync policy
6. **govulncheck** — 5 moby/moby findings, upstream Fixed-in N/A; migrate Docker
   SDK before v0.4.0
7. **Product gaps** until later blocks: A2A (B32), MCP router (B33),
   pipelines/children (B34–B35), model router/spend (B36–B38)
8. **B31 NOTES:** LocalStore read-path orphan tmp cleanup (R2); pack visibility
   for non-installed workflow refs (F9)

---

## Crypto security findings (top 5)

From `docs/pre-b31-crypto-security-review.md` (no CRITICAL remote break):

1. **C-01 HIGH** — Checkpoint `ComputeDigest` omits identity/sequence fields
2. **C-02 HIGH** — Pack writes AID private key to temp files for cosign import
   with empty password
3. **C-03 HIGH** — Identity keychain `security(1)` calls lack timeout; key
   material on argv
4. **C-04 MEDIUM** — Custom RFC 6979 ECDSA in bundle writer
5. **C-05 MEDIUM** — FileKeyStore symlink check is target-only

B31 fixed lock capability omission (F3): capabilities now in lockCanonicalMap.

---

## Next block: B32

**Spec:** `docs/execution/blocks/b32-summary.md`  
**Depends on:** B31 complete / `make block31-gate` green.

### Suggested read order for a new agent
1. This file  
2. `docs/known-limitations.md`  
3. `docs/execution/blocks/b32-summary.md` (if implementing B32)  
4. `docs/execution/blocks/b31-summary.md` + review-notes for registry handoff  
5. `docs/execution/blocks/b6-b21-summary.md` (history) + `b26`–`b30` as needed  
6. Package `doc.go` / `README.md` under the package you touch  
