# Block 7 — OWA Records

## Table of Contents

- [B7-T01: SecretStore and CLI Lifecycle](#b7-t01)
- [B7-T02: Brokered Gateway Credential Flow](#b7-t02)
- [B7-T03: Brokered Secret Invisibility Suite](#b7-t03)
- [B7-T04: Direct Lease Compatibility Mode](#b7-t04)
- [B7-T05: Revocation and Enterprise Follow-Up](#b7-t05)
- [B7M-T01: MCP Resource Model and Policy Binding](#b7m-t01)
- [B7M-T02: Local MCP Process and Sidecar Supervision](#b7m-t02)
- [B7M-T03: Gateway-Mediated MCP Routing](#b7m-t03)
- [B7M-T04: MCP Workload Egress Policy](#b7m-t04)
- [B7M-T05: MCP Status, Dashboard Data, and Hermes Contract](#b7m-t05)
- [B7M-T06: MCP Tool Auditing and Host-Affecting Capability Guard](#b7m-t06)
- [Block 7 Block-End Verification Report](#verification) — verification record

---

# B7-T01: SecretStore and CLI Lifecycle

## Worker
- Model: gpt-5.5 via Codex CLI (local mode)
- Branch: feat/b7-t01-secretstore-cli (merged)
- Commits: 18024ef, f23018e, bdac11f, dd159b6, ee01ce4
- Files: internal/secrets/store.go, keychain.go, keychain_test.go, store_test.go, doc.go; internal/cli/control.go, root.go, cli_test.go; Makefile
- Tests added: 13 (store_test.go + keychain_test.go)
- Status: complete — all 10 acceptance criteria met

## What was built
- `SecretStore` interface (Set/Get/List/Delete/TouchLastUsed) + `SecretMeta` struct
- `FakeKeyStore` (in-memory, the only test store — no plaintext fallback)
- `KeychainStore` (macOS `security` CLI, all tests guarded by GOOS + AGENTPAAS_KEYCHAIN_TESTS)
- `agent secret set/list/rm` CLI (set reads stdin only, never argv; list shows metadata only; max 64 KiB; case-sensitive names, rejects whitespace/control/format chars)
- Makefile `block7-gate` runs `go test -race -count=1 ./internal/secrets/...`

## Gate (local, fresh cache)
- build: PASS
- test -race: PASS (secrets 1.227s, cli 1.233s)
- lint: 0 issues

## Adversary (grok-4.3, agentpaas-adversary)
Breaks found: 1
- BREAK #1 (MEDIUM): Unicode zero-width/format characters (U+200B, U+200C, U+200D, U+FEFF) bypassed ValidateSecretName because `unicode.IsSpace`/`unicode.IsControl` don't cover category Cf.
  - Test: TestAdversary_B7_T01_UnicodeHomoglyphsAndControls
  - Fix: added `unicode.Is(unicode.Cf, r)` check (commit dd159b6)
  - Verified: adversary test passes after fix

Confirmed-safe: argv value acceptance, plaintext fallback, value leakage in list/errors, keychain test guards, RM idempotency, concurrent access, oversize boundary, empty stdin, LastUsedAt semantics.

## Fix workers
1. dd159b6 — reject unicode format/zero-width chars in secret names (adversary break)
2. ee01ce4 — errcheck fix in adversary test (unchecked store.Set/Get returns)

## Verifier
Deferred to block-end (see docs/owa-records/b7-block-end.md)

## Merge
- Merge commit: 100760a (B7-T01: SecretStore and CLI Lifecycle)
- Merged to local main, worktree pruned, branch deleted

---

# B7-T02: Brokered Gateway Credential Flow

## Worker
- Model: gpt-5.5 via Codex CLI (local mode)
- Branch: feat/b7-t02-brokered-credflow (merged)
- Commits: 017a509, 4026388, 224482d, 24698a4
- Files: internal/secrets/broker.go, broker_test.go, gateway.go; internal/audit/event_types.go
- Tests added: 10
- Status: complete — all 14 acceptance criteria met

## What was built
- `Broker` type with `RequestCredential(ctx, runID, policyRuleID, destination, method) (CredentialInjection, error)`
- Validates: active run, policy rule, credential reference, destination (domain+port), method
- `CredentialInjection` with redacted String() (no raw value in any string representation)
- Gateway helper for upstream TLS requests with redirect policy (credentialed redirect denied, non-credentialed rechecked per hop)
- `secret_injected` audit event with `visible_to_agent=false`
- Wrong domain/method/port/credentialed redirect denied before injection and audited
- Missing secret returns actionable error naming credential id only

## Gate (local, fresh cache)
- build: PASS
- test -race: PASS (secrets 1.566s, audit 12.873s)
- lint: 0 issues

## Adversary (grok-4.3, agentpaas-adversary)
Breaks found: 1
- BREAK #1 (MEDIUM): Header name injection via brokered credential policy — Credential.Header containing \r\n (CRLF) was accepted into CredentialInjection.HeaderName without sanitization. HTTP header injection risk.
  - Test: TestAdversary_B7_T02_HeaderInjectionViaPolicy
  - Fix: added RFC 7230 token validation for header names, reject CRLF/control chars (commit 224482d)
  - Test updated to expect rejection (commit 24698a4)
  - Verified: adversary test passes after fix

Confirmed-safe: secret leakage in strings/errors/audit, domain bypasses (IDN/subdomain/trailing dot/case), method case/unusual, credentialed redirect follow, port precision, rule spoofing, runID validation, concurrent races, missing secret error, audit completeness.

## Fix workers
1. 224482d — reject CRLF/control chars in brokered header names (adversary break)
2. 24698a4 — update adversary test to expect rejection (test expectation update)

## Verifier
Deferred to block-end (see docs/owa-records/b7-block-end.md)

## Merge
- Merge commit: 63e69bd (B7-T02: Brokered Gateway Credential Flow)
- Merged to local main, worktree pruned, branch deleted

---

# B7-T03: Brokered Secret Invisibility Suite

## Worker
- Model: gpt-5.5 via Codex CLI (local mode)
- Branch: feat/b7-t03-invisibility (merged)
- Commits: c1dcab4, cc24913, 63bdd3d, 5d6c2c6
- Files: internal/secrets/invisibility_b7_t03_test.go, adversary_b7_t03_test.go, broker.go (json tags), gateway.go (response redaction)
- Tests added: 11 (invisibility) + adversary suite
- Status: complete — all 15 acceptance criteria met

## What was built
- 11 negative-test probes: agent environment, process args, filesystem walk, daemon logs, gateway logs, CLI errors, compiled config, audit events, Docker inspect (gated), process list, shell history fixture
- Sentinel value: BROKERED_SENTINEL_7f3a9c2e1b8d4f6a_NOT_VISIBLE_ANYWHERE
- All probes assert zero hits for the sentinel across every tested surface

## Gate (local, fresh cache)
- build: PASS
- test -race: PASS (secrets 1.409s)
- lint: 0 issues

## Adversary (grok-4.3, agentpaas-adversary)
Breaks found: 3
- BREAK #1 (HIGH): CredentialInjection leaks sentinel via json.Marshal — no json:"-" tag on HeaderValue. json.Marshal(inj) outputs the raw secret.
  - Fix: added json:"-" tags to HeaderName and HeaderValue (commit 63bdd3d)
- BREAK #2 (HIGH): Same as #1 but for *CredentialInjection (pointer marshal)
  - Fix: same json:"-" tags (commit 63bdd3d)
- BREAK #3 (MEDIUM): HTTP response body echoes brokered sentinel — echo test fixture reflects Authorization header (with sentinel) into response body, gateway returns it to caller.
  - Fix: gateway now redacts injected credential value from final upstream response bodies before returning (commit 63bdd3d)
  - Verified: all 3 adversary tests pass after fix

Confirmed-safe: false-negative tests, error stack traces, context leak, goroutine stack, metrics/spans, race between inject and clear, test output leak.

## Fix workers
1. 63bdd3d — redact credential from json marshal and http response body (3 adversary breaks)
2. 5d6c2c6 — errcheck and SA1029 lint fixes in adversary test

## Verifier
Deferred to block-end (see docs/owa-records/b7-block-end.md)

## Merge
- Merge commit: 0b9e493 (B7-T03: Brokered Secret Invisibility Suite)
- Merged to local main, worktree pruned, branch deleted

---

# B7-T04: Direct Lease Compatibility Mode

## Worker
- Model: gpt-5.5 via Codex CLI (local mode)
- Branch: feat/b7-t04-direct-lease (merged)
- Commits: 30591fd, b4073eb, 13a0f04, 57c31fe, 5c61e1b
- Files: internal/secrets/lease.go, lease_test.go, adversary_b7_t04_test.go; internal/audit/event_types.go
- Tests added: 11 + adversary regression suite
- Status: complete — all acceptance criteria met

## What was built
- `DirectLease` type with `Lease(ctx, runID, credentialID, policyRuleID) (LeaseHandle, error)`
- `LeaseHandle` with FilePath, redacted String()/json (no secret value exposure)
- `ReadLease` SDK helper that reads the lease file and emits `secret_read` audit event
- `Cleanup` removes the lease file (file gone after stop)
- Direct lease requires policy reason (`file_lease` type — opt-in, not default)
- File mode 0400, symlink protection (os.Lstat)
- `secret_leased` audit event with `visible_to_agent=true`
- No env leases (env_lease rejected)
- Raw file read only for explicit direct lease (non-lease files rejected by ReadLease)

## Gate (local, fresh cache)
- build: PASS
- test -race: PASS (secrets 1.483s)
- lint: 0 issues

## Adversary (grok-4.3, agentpaas-adversary)
Breaks found: 0
All claims hold. All attacks confirmed-safe:
- Secret value in lease handle (no leak)
- File permissions (exactly 0400)
- Symlink attack (rejected by os.Lstat)
- Symlink at creation (rejected)
- Cleanup race (no data race)
- Env leak (no env vars set)
- File not removed (Cleanup works)
- Multiple leases same secret (no interference)
- Path traversal (rejected)
- ReadLease on non-lease file (rejected)
- Audit event value leak (no value in events)
- Concurrent lease creation (no race)

Adversary added: regression test for "require direct lease reason" (commit 57c31fe, in lease_test.go) + adversary_b7_t04_test.go regression suite (commit 5c61e1b).

## Verifier
Deferred to block-end (see docs/owa-records/b7-block-end.md)

## Merge
- Merge commit: 91ddeef (B7-T04: Direct Lease Compatibility Mode)
- Merged to local main, worktree pruned, branch deleted

---

# B7-T05: Revocation and Enterprise Follow-Up

## Subtask
B7-T05: Revocation and Enterprise Follow-Up

## Scope
Implement brokered credential revocation behavior and create an enterprise
design follow-up document for managed-vault/remote broker patterns.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7-t05-revocation
- Commits: 08242e6 (tests), 96dde0c (implementation)
- Files changed:
  - internal/secrets/broker.go — Revoke, IsRevoked, RestartAffectedAgents, revocation check in RequestCredential
  - internal/secrets/revocation_test.go — 7 tests covering all acceptance criteria
  - docs/enterprise-follow-up.md — enterprise design follow-up (managed-vault, device posture, tenant policy, short-lived grants, revocation, audit)

## Implementation
- Broker.Revoke(ctx, credentialID): marks credential as revoked in in-memory
  map (sync.RWMutex protected).
- Broker.RequestCredential: checks revocation BEFORE store.Get. Revoked
  credentials denied with audit event (status="denied", reason="revoked",
  agent_id, run_id).
- Broker.IsRevoked(credentialID): returns revocation state.
- Broker.RestartAffectedAgents(ctx, credentialID): returns run IDs with
  active direct leases for the credential. Does NOT restart agents (daemon's
  job). Honest limitation documented: cannot claw back secret already visible
  to agent code.
- docs/enterprise-follow-up.md: design follow-up for corporate employee
  machines behind VPN (managed-vault/remote broker, device posture, tenant
  policy to disable direct leases, short-lived grants, revocation, audit).

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: c0d8fa2 (adversary break tests)
- Tests: 5 attacks in internal/secrets/adversary_b7_t05_test.go

### Breaks Found: 2

| # | Attack | Severity | Fix Commit | Verification |
|---|--------|----------|------------|--------------|
| 1 | Revocation denial audit payload missing agent_id/run_id | Medium | 2606b92 | TestAdversary_B7T05_RevocationDenialAuditMissingAgentID PASS (agent_id present) |
| 2 | Direct SecretStore.Get bypasses revocation (store ref held outside broker) | Low (architectural) | 2a9f4a5 | TestAdversary_B7T05_DirectStoreGetBypassesRevocation PASS (documented as design boundary; broker is sole credential path, store ref unexported) |

### Safe: 3
- Revoke non-existent credential: no-op, no panic ✓
- RestartAffectedAgents: correctly returns only active leased runs ✓
- Revocation per-broker instance: no cross-instance leak ✓

## Fix Worker
- Model: GPT-5.5 via Codex CLI (existing worktree)
- Commits: 2606b92 (audit enrichment), 2a9f4a5 (broker sole-path documentation + store ref unexport)
- All adversary tests pass after fix.

## Gate
- go build ./... : PASS
- go test -race -count=1 ./internal/secrets/... : PASS (1.460s)
- golangci-lint run ./internal/secrets/... : 0 issues
- All 5 adversary tests: PASS

## Verifier
See docs/owa-records/b7-block-end.md (deferred to block-end verification)

## Merge
- Merge commit: 882a113 (B7-T05: Revocation and Enterprise Follow-Up)
- Strategy: --no-ff
- 4 files changed, 454 insertions(+), 29 deletions(-)

---

# B7M-T01: MCP Resource Model and Policy Binding

## Subtask
B7M-T01: MCP Resource Model and Policy Binding

## Scope
Represent declared MCP servers as first-class managed AgentPaaS resources
tied to policy digest, agent, and run context. Create internal/mcpmanager
package with Resource type, Manager, policy validation, and status reporting.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7m-t01-mcp-resource-model
- Commits: 6 (docs, tests, manager, policy schema, canonical form, audit event)
- Files changed:
  - internal/mcpmanager/doc.go — package doc
  - internal/mcpmanager/manager.go — Resource type, Manager (Validate, IsToolAllowed, DenyToolCall, Status, policy digest)
  - internal/mcpmanager/manager_test.go — 15 tests
  - internal/policy/schema.go — MCPServer: added AuthMode, EgressBinding fields
  - internal/policy/canonical.go — CanonicalMCPServer: includes Transport, AllowedTools, AuthMode (no secret leak)
  - internal/audit/event_types.go — EventTypeMCPToolDenied

## Implementation
- Resource type: resource_type="mcp_server", owning agent/run, server ID,
  transport, allowed tools, health, readiness, last error, policy digest.
- Manager.Validate: rejects duplicate IDs, empty transport, stdio without
  command, http without URL/endpoint.
- Manager.IsToolAllowed: deny-all default for empty AllowedTools, exact
  case-sensitive match for allowed tools, false for undeclared servers.
- Manager.DenyToolCall: emits audit event with agent_id, run_id, server_id,
  tool, policy_rule_id.
- Policy digest: SHA-256 of canonical JSON (deterministic, excludes secrets
  and EgressBinding value).
- Manager.Status: returns []Resource with initial state (readiness="stopped",
  health="unknown", last error="").

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: d6c7493 (adversary regression tests)
- Tests: 10 attacks in internal/mcpmanager/adversary_b7m_t01_test.go

### Breaks Found: 0
All 10 attacks confirmed safe:
1. UndeclaredServerBypass: SAFE — IsToolAllowed returns false
2. DenyAllDefault: SAFE — empty AllowedTools denies all
3. CaseSensitiveToolMatch: SAFE — exact case match only
4. PolicyDigestDeterministicAndNoLeak: SAFE — deterministic, no EgressBinding leak
5. ValidationBypass: SAFE — rejects all invalid configs
6. CanonicalMCPServerEgressLeak: SAFE — EgressBinding excluded
7. DenyToolCallMissingFields: SAFE — all required fields present
8. StatusLeakSecrets: SAFE — no secret/credential leak
9. RaceValidateIsToolAllowed: SAFE — no race
10. TransportEmptyBypass: SAFE — empty transport rejected

## Gate
- go build ./... : PASS
- go test -race -count=1 ./internal/mcpmanager/... : PASS (1.273s)
- go test -race -count=1 ./internal/policy/... : PASS (3.286s)
- golangci-lint run ./internal/mcpmanager/... : 0 issues
- golangci-lint run ./internal/policy/... : 0 issues
- All 10 adversary tests: PASS

## Verifier
See docs/owa-records/b7-block-end.md (deferred to block-end verification)

## Merge
- Merge commit: f283a64 (B7M-T01: MCP Resource Model and Policy Binding)
- Strategy: --no-ff
- 7 files changed, 604 insertions(+), 23 deletions(-)

---

# B7M-T02: Local MCP Process and Sidecar Supervision

## Subtask
B7M-T02: Local MCP Process and Sidecar Supervision

## Scope
Start, readiness-check, stop, and reconcile declared local MCP servers.
Stdio servers run as daemon-managed child processes. HTTP servers run as
Docker sidecar containers. No host networking for MCP sidecars.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7m-t02-mcp-lifecycle
- Commits: e24f487 (tests), aae5442 (implementation)
- Files changed:
  - internal/mcpmanager/lifecycle.go — Lifecycle type (Start/Stop/StopAll/CheckReadiness/IsRunning/CrashContext)
  - internal/mcpmanager/lifecycle_test.go — 14 tests
  - internal/mcpmanager/manager.go — updated for lifecycle integration
  - internal/runtime/naming.go — ResourceTypeMCP="mcp", MCP container prefix, LabelMCPServerID
  - internal/runtime/reconcile.go — ReconcileAfterCrash extended for MCP, ReconcileMCPServers
  - internal/runtime/reconcile_test.go — MCP reconciliation tests

## Implementation
- Lifecycle.Start: stdio → child process with minimal env (PATH + declared only);
  http → Docker sidecar with AgentPaaS labels, no host networking
- Lifecycle.CheckReadiness: stdio → process alive check; http → container status check
- Lifecycle.Stop/StopAll: SIGTERM→SIGKILL for stdio; Stop+Remove for Docker
- CrashContext: structured failure context (server ID, transport, exit code, error, recoverable)
- ReconcileAfterCrash: extended to remove orphaned MCP containers (agent gone)
- ReconcileMCPServers: discovers MCP-labeled containers for lifecycle reconciliation

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: d8cd8b5 (adversary regression tests)
- Tests: 9 attacks in internal/mcpmanager/adversary_b7m_t02_test.go

### Breaks Found: 0
All 9 attacks confirmed safe:
1. Stdio env inheritance: SAFE — minimal env only
2. Host networking: SAFE — rejected for HTTP sidecars
3. Docker socket/host access: SAFE
4. Stop zombie/leak: SAFE — SIGTERM→SIGKILL works
5. StopAll completeness: SAFE — all stopped
6. CrashContext secret leak: SAFE — no secrets in Error field
7. Concurrent Start/Stop race: SAFE — no data race
8. Reconcile unrelated containers: SAFE — only owned MCP touched
9. Double-start: SAFE — prevented

Note: adversary observed Error field uses raw err strings (potential future
contract gap if errors contain secrets) — not a current break.

## Gate
- go build ./... : PASS
- go test -race -count=1 ./internal/mcpmanager/... : PASS (1.346s)
- go test -race -count=1 ./internal/runtime/... : PASS (1.401s)
- golangci-lint run ./internal/mcpmanager/... : 0 issues
- golangci-lint run ./internal/runtime/... : 0 issues
- All 9 adversary tests: PASS

## Verifier
See docs/owa-records/b7-block-end.md (deferred to block-end verification)

## Merge
- Merge commit: 3e5b97f (B7M-T02: Local MCP Process and Sidecar Supervision)
- Strategy: --no-ff
- 7 files changed, 1329 insertions(+), 8 deletions(-)

---

# B7M-T03: Gateway-Mediated MCP Routing

## Subtask
B7M-T03: Gateway-Mediated MCP Routing

## Scope
Route agent MCP tool calls through the gateway to local MCP servers and
return responses through the same governed path.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7m-t03-mcp-routing
- Commits: 64c1abe (router), 9877c51 (harness delegation)
- Files: internal/mcpmanager/router.go, router_test.go, lifecycle.go (StdioPipes), internal/harness/rpc_server.go (handleMCP)

## Implementation
- Router.CallTool: checks IsToolAllowed, routes to stdio (stdin/stdout JSON-RPC)
  or http (POST to endpoint), audits request/response
- JSON-RPC 2.0 protocol with request-ID matching for stdio
- Lifecycle.StdioPipes: exposes process stdin/stdout for routing
- Harness handleMCP: delegates to Router when configured, preserves legacy mock mode
- Audit: server_id, tool, input_hash (SHA-256), output_hash (SHA-256), timing_ms, agent_id, run_id

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: 913ef6c (adversary tests)

### Breaks Found: 2
| # | Attack | Severity | Fix Commit | Verification |
|---|--------|----------|------------|--------------|
| 1 | UnboundedHTTPResponseBody: io.ReadAll with no LimitReader → memory exhaustion | High | 0d6cd73 | TestAdversary_B7M_T03_UnboundedHTTPResponseBody PASS (1MiB limit) |
| 2 | StdioDecodeTimeoutDesync: uncancellable json.Decode, pipe state polluted after timeout | Medium | 389435d | TestAdversary_B7M_T03_StdioDecodeTimeoutDesync PASS (request-ID matching + pipe cleanup) |

### Safe: 4
- NilRouterManagerSafe: nil guards prevent panic
- LegacyMockStillWorks: router==nil path preserved
- ConcurrentStdioSerialized: Router.mu serializes stdio I/O
- AuditAllFieldsAndHashed: all 7 fields present, input/output always SHA-256 hex

## Fix Worker
- Commits: 0d6cd73 (HTTP 1MiB limit), 389435d (serialized stdio with request-ID matching)
- All adversary tests pass after fix.

## Gate
- go build ./... : PASS
- go test -race -count=1 ./internal/mcpmanager/... : PASS (2.769s)
- go test -race -count=1 ./internal/harness/... : PASS (15.753s)
- golangci-lint run ./internal/mcpmanager/... ./internal/harness/... : 0 issues
- All 6 adversary tests: PASS

## Merge
- Merge commit: B7M-T03: Gateway-Mediated MCP Routing
- Strategy: --no-ff
- 5 files changed, 920 insertions(+), 7 deletions(-)

---

# B7M-T04: MCP Workload Egress Policy

## Subtask
B7M-T04: MCP Workload Egress Policy

## Scope
Enforce default-deny egress for MCP servers as separate managed workloads.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7m-t04-mcp-egress
- Commits: 509d552 (implementation), 206815c (adversary fix)
- Files: internal/mcpmanager/egress.go, egress_test.go, internal/policy/schema.go

## Implementation
- EgressPolicy.CheckEgress: validates destination, method, port against rules
- Default-deny: servers with no rules deny all outbound
- Host access (localhost, 127.x, ::1, localhost.localdomain, localhost4/6) denied
- Docker socket (unix:///var/run/docker.sock) denied
- Link-local (169.254.x.x) denied
- Hardened: wildcard * rejected, Port=0 deny, empty Methods deny
- Audit: server_id, destination, method, credential_id, policy_rule_id, decision
- policy.EgressRule: added MCPServerID field

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: 81c5ac2 (adversary tests)

### Breaks Found: 5
| # | Attack | Fix Commit | Verification |
|---|--------|------------|--------------|
| 1 | NilEgressPolicy panic | 206815c | nil guard added |
| 2 | WildcardDefeatsDefaultDeny (* allows all) | 206815c | wildcard rejected |
| 3 | DNSBypassLocalhostAlias | 206815c | expanded localhost check |
| 4 | PortZeroAnyPort | 206815c | Port=0 = deny |
| 5 | EmptyMethodsAllowsAll | 206815c | empty Methods = deny |

### Safe: 5
- DockerSocketDifferentPath: SAFE
- URLUserinfoBypass: SAFE (denied)
- PrivateNetworkWithoutCIDR: SAFE (blocked)
- IPv6LoopbackDenied: SAFE (blocked)
- AuditNoCredentialLeak: SAFE (ID only)

## Gate
- go test -race -count=1 ./internal/mcpmanager/... : PASS (2.828s)
- golangci-lint run ./internal/mcpmanager/... : 0 issues
- All 10 adversary tests: PASS

## Merge
- Merge commit: B7M-T04: MCP Workload Egress Policy
- Strategy: --no-ff
- 4 files changed, 848 insertions(+), 6 deletions(-)

---

# B7M-T05: MCP Status, Dashboard Data, and Hermes Contract

## Subtask
B7M-T05: MCP Status, Dashboard Data, and Hermes Contract

## Scope
Expose MCP server readiness and health through the same operator/status surfaces
as agents. Status report includes resource inventory, lifecycle-derived readiness/health,
Docker sidecar metadata (labels, image digest, network membership, health, restart count),
and redacted AgentPaaS ownership labels.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7m-t05-mcp-status
- Commits: e95d462 (status report), 09f6c49 (CLI hook), 72e5a0e (lint fix)
- Files changed:
  - internal/mcpmanager/status.go — StatusReport, MCPSidecarInfo, GenerateStatusReport,
    MCPStatusJSON, applyLifecycleState, collectSidecars, buildSidecarInfo,
    stateFromContainerStatus, agentpaasLabels, networkNames
  - internal/mcpmanager/status_test.go — 10 tests

## Implementation
- GenerateStatusReport: aggregates Manager.Status() resources + Docker sidecar metadata
- MCPSidecarInfo: server_id, container_id, image_digest, labels, networks, health,
  readiness, restart_count, memory_bytes, cpu_percent, last_error
- agentpaasLabels: whitelist filter (LabelManagedBy, LabelResourceType, LabelRunID,
  LabelMCPServerID) + value sanitization (control char escape, truncate to 128 chars)
- collectSidecars: filters on ALL three ownership labels (MCPServerID + ManagedBy +
  ResourceType) — rejects spoofed containers missing ownership labels
- stateFromContainerStatus: complete mapping (Running→ready/healthy,
  Paused→starting/unknown, Stopped/Removed→stopped/failed, Unknown→unhealthy/unknown)
- Nil driver → sidecar info omitted (stdio-only deployments)
- Nil manager → empty report
- Timestamps: UTC (time.Now().UTC())

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: 9e208d3 (adversary break tests), a9bfb27 (fix)

### Breaks Found: 4 (all fixed)
| # | Attack | Fix |
|---|--------|-----|
| 1 | SpoofedContainerWithoutOwnershipLabels — collectSidecars filtered only on LabelMCPServerID | Added LabelManagedBy + LabelResourceType to ListContainers filter |
| 2 | AllowedLabelValuesNotRedacted — label values copied verbatim (secret leak) | Added sanitizeLabelValue (control char escape, truncate to 128) |
| 3 | IncompleteStateMapping — Paused/Unknown defaulted to unhealthy/failed | Added explicit Paused→starting/unknown, Unknown→unhealthy/unknown |
| 4 | ContainerIDMismatchViaLabelSpoof — no ownership validation beyond label | Fixed by ownership filter (#1); known risk: daemon-set labels trusted |

### Safe (confirmed by adversary)
- UTC timestamps consistent
- Nil manager/driver handled safely
- Non-whitelisted secret labels redacted
- Concurrent GenerateStatusReport safe
- Partial driver errors set LastError without aborting report

## Gate
- go build ./... : PASS
- go test -race -count=1 ./internal/mcpmanager/... : PASS (2.899s)
- go test -race -count=1 ./internal/cli/... : PASS
- golangci-lint run ./internal/mcpmanager/... : 0 issues
- golangci-lint run ./internal/cli/... : 0 issues
- All 4 adversary tests: PASS (asserting fixed behavior)

## Verifier
See docs/owa-records/b7-block-end.md (deferred to block-end verification)

## Merge
- Merge commit: 51bfbac (B7M-T05: MCP Status, Dashboard Data, and Hermes Contract)
- Strategy: --no-ff
- 3 files changed, 800 insertions(+)

## Known Risks
- A container with valid AgentPaaS ownership labels and a matching MCPServerID is
  accepted without further validation. In production, labels are daemon-set, so
  spoofing requires daemon access. Documented in adversary test.
- CLI control.go did not contain an existing status command or daemon wiring, so
  the CLI change was reverted (unused helper functions). GenerateStatusReport is
  available as a reusable API; CLI wiring deferred to Block 10 (dashboard).

---

# B7M-T06: MCP Tool Auditing and Host-Affecting Capability Guard

## Subtask
B7M-T06: MCP Tool Auditing and Host-Affecting Capability Guard

## Scope
Audit every MCP tool call with full metadata (decision, policy rule id, credential id,
input/output hashes). Redact/truncate MCP tool output before display. Classify
host-affecting tools and require explicit confirmation before enabling them.

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Branch: feat/b7m-t06-mcp-audit
- Commits: 399af8a (implementation), be653f8 (output sanitization fix)
- Files changed:
  - internal/audit/event_types.go — EventTypeMCPToolCall = "mcp_tool_call"
  - internal/mcpmanager/audit.go — AuditToolCall, AuditToolDenied
  - internal/mcpmanager/capability.go — ClassifyTool, IsHostAffecting, patterns
  - internal/mcpmanager/capability_test.go — 13 tests
  - internal/mcpmanager/redact.go — RedactToolOutput, sanitizeJSONValue, sentinel patterns
  - internal/mcpmanager/manager.go — ConfirmTool, IsToolConfirmed, RequiresConfirmation
  - internal/mcpmanager/router.go — CallTool (confirmation check, audit, redacted return)

## Implementation
- AuditToolCall: emits mcp_tool_call with server_id, tool, decision, policy_rule_id,
  credential_id, input_hash, output_hash, timing_ms, host_affecting, agent_id, run_id
- AuditToolDenied: emits mcp_tool_denied with symmetric fields (credential_id,
  input_hash, timing_ms included for audit consistency)
- ClassifyTool: pattern-based — shell/exec/bash/terminal, browser/playwright/puppeteer,
  filesystem/write_file/edit_file, applescript/osascript, desktop/mouse/keyboard/screen/cua
- Manager.ConfirmTool: marks host-affecting tool as confirmed
- Manager.RequiresConfirmation: true if host-affecting AND not confirmed
- RedactToolOutput: sentinel secret redaction (sk-, sk_live_, AKIA, ghp_, gho_, ghs_,
  -----BEGIN, PRIVATE KEY, xoxb-, xoxp-), control char escape, truncate to 4096 chars
- sanitizeJSONValue: recursively sanitizes map KEYS and values, nested slices
- sentinelSecretPatternsList: returns fresh slice (immutable, not mutable var)
- Router.CallTool: IsToolAllowed → AuditToolDenied; RequiresConfirmation →
  AuditToolDenied; route; AuditToolCall; return redactToolOutputValue(result)

## Adversary
- Model: grok-4.3 via agentpaas-adversary profile
- Commit: 7f0dc06 (adversary tests), 77b1fa2 (fix)

### Breaks Found: 3 (all fixed)
| # | Attack | Fix |
|---|--------|-----|
| 1 | Redaction misses secrets in map KEYS (only values sanitized) | sanitizeJSONValue now sanitizes both keys and values in map[string]any |
| 2 | AuditToolDenied omits credential_id/input_hash/output_hash/timing_ms | Added parameters to AuditToolDenied, updated all call sites, symmetrized payload |
| 3 | sentinelSecretPatterns is mutable var (package-level mutation risk) | Converted to sentinelSecretPatternsList() function returning fresh slice |

### Safe (confirmed by adversary — 13 attacks safe)
- Pattern/unicode/control evasion of ClassifyTool (substring match catches variants)
- TOCTOU race between RequiresConfirmation and call (no data race under -race)
- Denied calls do not reach MCP server (routing blocked before routeStdio/routeHTTP)
- Control characters in nested structures escaped (sanitizeJSONValue recurses)
- Hostile HTML with embedded control chars escaped
- confirmedTools map safe under concurrent access (sync.RWMutex)
- Prompt-injected tool output cannot add MCP servers or broaden tools (read-only)
- Audit events include all 11 required fields
- redactToolOutputValue preserves JSON types for downstream parsing
- Empty/zero tool name and server ID do not panic
- No raw output leaked into audit event (output_hash is a hash only)

## Gate
- go build ./... : PASS
- go test -race -count=1 ./internal/mcpmanager/... : PASS (2.902s)
- go test -race -count=1 ./internal/audit/... : PASS
- golangci-lint run ./internal/mcpmanager/... : 0 issues
- golangci-lint run ./internal/audit/... : 0 issues
- All 3 adversary tests: PASS (asserting fixed behavior)

## Verifier
See docs/owa-records/b7-block-end.md (deferred to block-end verification)

## Merge
- Merge commit: 5a5111e (B7M-T06: MCP Tool Auditing and Host-Affecting Capability Guard)
- Strategy: --no-ff
- 8 files changed, 765 insertions(+), 43 deletions(-)

---

# Block 7 Block-End Verification Report

## Verifier
- Model: z-ai/glm-5.2 (GLM-5.2 Flagship) via agentpaas-verifier profile
- Session: 20260621_172903_ba82c4
- Date: 2026-06-21
- Scope: Full block gate + all adversary tests on merged main + cross-subtask integration review

## Block 7 Scope
Two tracks:
- **Block 7 (secrets/revocation):** B7-T01 SecretStore, B7-T02 Brokered Gateway,
  B7-T03 Invisibility Suite, B7-T04 Direct Lease, B7-T05 Revocation
- **Block 7.5 (MCP Server Lifecycle Manager):** B7M-T01 Resource Model,
  B7M-T02 Lifecycle Supervision, B7M-T03 Gateway Routing, B7M-T04 Egress Policy,
  B7M-T05 Status Reporting, B7M-T06 Audit + Capability Guard

## Gate Results
- build: PASS (exit 0, no errors)
- test: PASS (13 packages, 0 fail)
- race: PASS (exit 0)
- lint: PASS (0 issues, fresh-cache golangci-lint)
- osv: PASS (0 issues — 7 Docker daemon-side CVEs filtered: docker cp race / archive
  trojan, fixed in Docker Engine 29.5.1, not SDK client vulns, AgentPaaS does not use
  docker cp; requires Docker Engine >=29.5.1)

## Adversary Tests
- B7 tests: 44 run, 44 pass, 0 fail (internal/secrets)
- B7M tests: 55 run, 55 pass, 0 fail (internal/mcpmanager)
- Total: 99/99 pass

## Cross-Subtask Integration Review

### 1. [PASS] Router uses Manager + Lifecycle correctly
- router.go:64 `r.manager.IsToolAllowed(serverID, tool)` (T01 policy check)
- router.go:68 `r.manager.RequiresConfirmation(serverID, tool)` (T06 host-affecting gate via T01 confirmation)
- router.go:108 `r.lifecycle.StdioPipes(serverID)` (T02 stdio pipes for routing)
- router.go:126 `r.lifecycle.CrashContext(serverID)` (T02 structured crash context)
- router.go:86 `r.routeHTTP(...)` for http transport

### 2. [PASS] Status reflects Lifecycle states (both stdio + http)
- status.go:55 `manager.Status()` (T01)
- status.go:56-58 `applyLifecycleState` for ALL resources (no transport filter — covers stdio AND http)
- status.go:88 `lifecycle.IsRunning` + :94 `lifecycle.CrashContext` (T02)
- status.go:65 `collectSidecars` for Docker http sidecar metadata (image digest, restart count, networks, stats)

### 3. [PASS] Audit fires from Router on allowed + denied
- Denied: router.go:65 (undeclared), :69 (host_affecting_unconfirmed), :74 (server not found) → AuditToolDenied
- Allowed: router.go:93 → AuditToolCall with "allowed" + RedactToolOutputHash
- audit.go: AuditToolDenied / AuditToolCall defined with full metadata (decision, policy_rule_id, credential_id, input_hash, output_hash, timing_ms, host_affecting)

### 4. [PASS] Egress applies to MCP sidecars independently of agent egress
- egress.go EgressPolicy is in mcpmanager package, keyed by MCPServerID
- Separate from agent egress which is secrets.Broker.ValidateEgress (broker.go:210)
- Default-deny: nil policy → error; no rules for server → "no egress rules for MCP server"
- Docker socket URL blocked; localhost/link-local/private host denied; sidecars use non-host network

### 5. [PASS] Revocation blocks Gateway requests
- broker.go:131 `if b.IsRevoked(credentialID)` inside RequestCredential → denyWithReason "revoked"
- gateway.go:52 `g.broker.RequestCredential(...)` called for credentialed requests — revoked credential returns error, no injection
- broker.go:174 `Revoke` marks credential in revoked map; :182 `IsRevoked` reads it
- broker.go:192 `RestartAffectedAgents` returns activeDirectLeases[credentialID] ∩ activeRuns (sorted)

### 6. [PASS] All adversary test files on main
- B7 (5): internal/secrets/adversary_b7_t01..t04_test.go, adversary_b7_t05 (named TestAdversary_B7T05_*)
- B7M (6): internal/mcpmanager/adversary_b7m_t01..t06_test.go

## Additional Integration Notes
- T02 ReconcileAfterCrash / ReconcileMCPServers live in internal/runtime/reconcile.go (orphan cleanup at runtime/Docker layer, removes MCP sidecars + agents after daemon crash)
- T06 RedactToolOutput (redact.go) sanitizes output on allowed path + map keys redacted, control chars escaped, sentinel secret patterns replaced, HTML escaped, truncated to 4KiB
- T01 Manager.Register (manager.go:90) creates Resource with PolicyDigest (computePolicyDigest:108, strips URL userinfo) + AllowedTools copy
- T03/T04 Gateway (gateway.go) credentialed redirect denied before re-injection, redirect following disabled via CheckRedirect=ErrUseLastResponse, credential redacted from response body
- T04 DirectLease (lease.go): file_lease only (not env_lease — P1), lease file mode 0400, chown to agent UID, symlink/path-traversal rejection, LeaseHandle.String/GoString/MarshalJSON all redact FilePath

## OWA Records Completeness
11/11 present:
- B7: b7-t01.md, b7-t02.md, b7-t03.md, b7-t04.md, b7-t05.md
- B7M: b7m-t01.md, b7m-t02.md, b7m-t03.md, b7m-t04.md, b7m-t05.md, b7m-t06.md

## Post-Build Audit Table

| Subtask | Worker | Adversary | Breaks | Fix | Gate | OWA Record |
|---------|--------|-----------|--------|-----|------|------------|
| B7-T01 | PASS | PASS | — | — | PASS | ✓ |
| B7-T02 | PASS | PASS | — | — | PASS | ✓ |
| B7-T03 | PASS | PASS | — | — | PASS | ✓ |
| B7-T04 | PASS | PASS | — | — | PASS | ✓ |
| B7-T05 | PASS | PASS | — | — | PASS | ✓ |
| B7M-T01 | PASS | PASS | — | — | PASS | ✓ |
| B7M-T02 | PASS | PASS | 0 breaks | — | PASS | ✓ |
| B7M-T03 | PASS | PASS | 2 breaks | fixed | PASS | ✓ |
| B7M-T04 | PASS | PASS | 5 breaks | fixed | PASS | ✓ |
| B7M-T05 | PASS | PASS | 4 breaks | fixed | PASS | ✓ |
| B7M-T06 | PASS | PASS | 3 breaks | fixed | PASS | ✓ |

## Verdict

**VERIFY PASS**

All gates green, all 99 adversary tests pass, all 6 cross-subtask integration points
confirmed by reading merged code, all 11 OWA records present. No blockers found.
