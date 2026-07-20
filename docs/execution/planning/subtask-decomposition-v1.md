# AgentPaaS P1 Subtask Decomposition v1

Companion documents:
- `agentpaas-prd-v4-master.md`
- `agentpaas-execution-plan-v1.md`

Purpose: convert each execution block into PR-sized work items that an
orchestrator can hand to coding agents. Each item should become one GitHub
issue and usually one PR. Keep the existing rule: one behavioral claim per PR,
target under 500 changed production LOC plus tests, and no merge without
verifier evidence plus orchestrator ACCEPT.

## Universal Issue Contract

Every issue given to a worker must include:
- Block id and task id, for example `B4-T03`.
- Relevant PRD and execution-plan excerpts only.
- Exact files/directories in scope.
- Behavioral claim the PR is allowed to make.
- Non-goals and forbidden scope expansion.
- Required test-first target.
- Canonical gate wrapper, for example `make block4-gate`.
- Whether adversary review is required by the trigger matrix.
- Definition of Done copied into the PR.

Every verifier receives:
- The issue body and acceptance criteria.
- The worker diff and command output.
- The exact gate or narrow target to run.
- A request to answer only `PASS`, `FAIL` with repro, or `MISSING TESTS`.

Every orchestrator decision records:
- Accept/refine/reject.
- Evidence considered.
- Remaining risk and follow-up issues.
- Whether adversary was invoked or explicitly skipped.

## Task State Machine

`ready -> worker_active -> worker_done -> verifier_active -> verifier_done -> adversary_active? -> orchestrator_review -> approved -> merged`

Deferral paths:
- Worker cannot proceed: `needs_orchestrator`.
- Verifier finds defect: `defer_to_orchestrator` with repro.
- Adversary breaks claim: `blocked_security`.
- Same issue fails three worker attempts: orchestrator must split, rescope,
  change approach, or switch model.

## Block 1: Repo Bootstrap, Proto Contracts, CI Skeleton

Canonical gate: `make block1-gate`

### B1-T01 Repository Layout and Baseline Tooling

Goal: create the monorepo directory layout, `.gitignore`, `.gitattributes`,
LICENSE, SECURITY.md, CODEOWNERS placeholder, and baseline Go module.

Scope:
- Create the directories from execution-plan §0.3.
- Add Apache-2.0 license and security policy.
- Add Go module and minimal empty package layout where needed.

Non-goals:
- No daemon logic.
- No real runtime/policy/identity implementation.

Acceptance:
- `go test ./...` works on empty scaffold.
- `git diff --check` clean.
- Repo paths match the planned layout.

Verifier:
- Run `go test ./...` and inspect layout.

Adversary: not required.

### B1-T02 Proto Contract Authoring

Goal: author `api/trigger/v1/trigger.proto` and
`api/control/v1/control.proto` with stable packages, go_package options,
HTTP annotations, idempotency semantics, and operator/control methods.

Scope:
- Define Trigger API services and messages.
- Define Control API services and messages.
- Include enums, pagination, google.rpc.Status errors, run fields, and
  comments documenting REST/SSE mapping.

Non-goals:
- No server implementation.
- No CLI implementation.

Acceptance:
- Buf lint passes.
- Route table can be generated from annotations.
- Proto comments describe idempotency replay and conflict behavior.

Verifier:
- Run `make proto` after B1-T03 exists, or `buf lint` in narrow mode.
- Check methods listed in Block 1 are present.

Adversary: not required unless auth/security semantics are changed.

### B1-T03 Buf, Codegen, and Reproducibility

Goal: configure buf and grpc-gateway code generation with committed generated
Go code.

Scope:
- Add buf config, generation config, generated files, and Makefile proto
  target.
- Add codegen reproducibility check.
- Add buf breaking-change check fixture or script that fails on field
  renumbering.

Non-goals:
- No API implementation.

Acceptance:
- Two consecutive codegen runs are byte-identical.
- Deliberate generated-code drift is detected.
- Breaking-change test catches field renumbering.

Verifier:
- Run `make proto`.
- Run the reproducibility check.

Adversary: not required.

### B1-T04 Makefile and Canonical Gates

Goal: create the Makefile namespace and stable block gate wrappers.

Scope:
- Add `build`, `test`, `proto`, `lint`, `race`, `osv`, `e2e-network`,
  `redteam-smoke`, and `block1-gate` through `block15-gate`.
- Future block wrappers may fail with clear "not implemented until Block N"
  text until their block owns them.

Non-goals:
- Do not implement future block behavior.

Acceptance:
- `make block1-gate` wraps Block 1 checks.
- Every canonical wrapper exists and has a stable name.

Verifier:
- Run `make block1-gate`.
- Run a sample future wrapper and confirm intentional failure text.

Adversary: not required.

### B1-T05 CI Skeleton

Goal: add GitHub Actions for lint, test, race, proto, and OSV scanning.

Scope:
- Add workflows with macOS primary coverage where required.
- Ensure generated-code drift fails CI.
- Include placeholder jobs for later e2e gates without pretending they pass.

Non-goals:
- No release pipeline.

Acceptance:
- Local equivalents run through Makefile.
- CI config has no missing local target.

Verifier:
- Run `make lint test race proto osv` or available equivalents.
- Inspect workflow target parity.

Adversary: not required.

### B1-T06 Issue, PR, and Status Memory Templates

Goal: implement durable OWA memory templates and local fallback status files.

Scope:
- Add issue templates, PR template, `docs/status.md`,
  `docs/issues/`, `docs/prs/PR-000-template.md`, and
  `scripts/update-status-dashboard.sh`.
- Templates include model routing, attempt log, verifier evidence, adversary
  status, orchestrator decision, gate result, and next action.

Non-goals:
- No GitHub API automation required.

Acceptance:
- Status dashboard renders built/remaining/PR sections before GitHub exists.
- PR template contains Definition of Done.

Verifier:
- Run status update script.
- Check generated `docs/status.md` has required sections.

Adversary: not required.

## Block 2: Daemon Skeleton and CLI Plumbing

Canonical gate: `make block2-gate`

### B2-T01 Local Home Layout and Permissions

Goal: create `~/.agentpaas` layout with secure permissions and test overrides.

Scope:
- Implement home discovery using `AGENTPAAS_HOME`.
- Create `daemon.sock`, pid, logs, state, config, cache, tmp paths with
  required modes.
- Refuse broad home/socket permissions.

Non-goals:
- No launchd integration.
- No Docker checks beyond stubs.

Acceptance:
- Unit tests cover good, missing, stale, and bad-permission layouts.

Verifier:
- Run relevant package tests.

Adversary: required because socket/home permissions are a trust boundary.

### B2-T02 Unix Socket gRPC Daemon Skeleton

Goal: implement `agentpaasd` server skeleton with readiness handshake and
stub control handlers.

Scope:
- Bind only to unix socket.
- Add graceful shutdown and readiness state.
- Implement version/proto compatibility response.

Non-goals:
- No real pack/run/secret/audit behavior.

Acceptance:
- CLI can connect to daemon and receive structured stub responses.
- Daemon not ready causes bounded wait then actionable error.

Verifier:
- Run daemon unit tests and local daemon smoke.

Adversary: required for socket access/readiness edge cases.

### B2-T03 Cobra CLI Command Surface

Goal: implement `agent` CLI commands wired to Control API stubs.

Scope:
- Add daemon lifecycle commands, version, doctor v0, and stubs for planned
  control operations.
- Every user-visible command has `--json` where the contract requires it.

Non-goals:
- No real runtime logic.

Acceptance:
- `agent version` and `agent daemon status` include CLI version, daemon
  version, proto version, git commit, OS/arch, Docker context/API fields.

Verifier:
- Run CLI golden output tests for text and JSON.

Adversary: not required unless auth/socket semantics change.

### B2-T04 launchd and systemd Unit Generation

Goal: generate deterministic service files without requiring launchd/systemd
inside CI.

Scope:
- P1 launchd plist generation and lifecycle commands.
- P2 systemd user unit generator only, not Linux certification.

Non-goals:
- No privileged install.
- No root service.

Acceptance:
- Golden tests pass for macOS launchd and Linux systemd user files.
- Daemon refuses root unless `--allow-root-for-test`.

Verifier:
- Run golden tests.

Adversary: required for service/security posture.

### B2-T05 Doctor Checks

Goal: implement `agent doctor` v0 with actionable diagnostics.

Scope:
- Docker reachable/context/API version.
- Docker Desktop/Colima detection on macOS.
- Ports 7700/7717/7718 free.
- Socket/home perms.
- Daemon readiness and proto compatibility.

Non-goals:
- No Linux dockerd certification.

Acceptance:
- Induced failures return nonzero with exact issue and fix hint.

Verifier:
- Run `test/e2e/doctor_test.sh`.

Adversary: required.

### B2-T06 Structured Logging and Redaction

Goal: add slog JSON logging with secret-looking value redaction across CLI and
daemon.

Scope:
- Redact high-entropy/API-key-looking values in logs and errors.
- Add test fixtures with planted sentinel values.

Non-goals:
- No full data-loss-prevention engine.

Acceptance:
- Redaction tests prove planted values never appear in CLI or daemon logs.

Verifier:
- Run logging/redaction tests.

Adversary: required.

## Block 3: Identity Service and Audit Hash Chain

Canonical gate: `make block3-gate`

### B3-T01 KeyStore Interfaces and Fake Contract Tests

Goal: define narrow `KeyStore` and `IdentityIssuer` interfaces with reusable
contract tests.

Scope:
- Add in-memory fake keystore.
- Add interface-level tests for key create/load/sign/verify and permission
  failures.

Non-goals:
- No Keychain implementation yet.

Acceptance:
- Fake passes the same contract suite planned for local implementations.

Verifier:
- Run `go test ./internal/identity/...`.

Adversary: required.

### B3-T02 macOS Keychain and Encrypted File Keystore

Goal: implement P1 Keychain wrapper and explicit encrypted file-keystore
fallback.

Scope:
- Use `security(1)` wrapper for macOS.
- File fallback requires passphrase, 0600 files, and doctor warning.
- No plaintext fallback.

Non-goals:
- No Linux libsecret.

Acceptance:
- Locked/unavailable Keychain gives actionable error.
- Weak permissions and wrong passphrase fail closed.

Verifier:
- Run unit tests with fake and file store; macOS integration when available.

Adversary: required.

### B3-T03 Local CA, Package AID, and Workload Certs

Goal: manage local CA, daemon audit key, per-agent package identity keys, and
per-run workload cert issuance/renewal.

Scope:
- SPIFFE URI builder/verifier with configurable trust domain.
- 1h workload cert with renewal before expiry.
- Package identity key never enters run container.

Non-goals:
- No hosted identity service.

Acceptance:
- Expired cert rejected.
- Alternate hosted trust domain fixture passes without schema change.

Verifier:
- Run identity cert tests.

Adversary: required.

### B3-T04 Audit Record Canonicalization and Append-Only Writer

Goal: implement canonical JSONL audit chain with seq, prev_hash, record_hash,
and single daemon-owned writer.

Scope:
- Stable canonical JSON hash.
- Serialized appends with fsync.
- Guarded operations fail closed if append fails.

Non-goals:
- No export bundle yet.

Acceptance:
- Race test proves concurrent writers serialize without loss.
- fsync/write failure causes guarded operation failure.

Verifier:
- Run `go test ./internal/audit/... -race`.

Adversary: required.

### B3-T05 Checkpoints, Head Anchor, and Verification

Goal: insert signed checkpoint records and verify local chains against latest
head anchor.

Scope:
- Fixed cadence and export-time checkpoint support.
- Detect middle tamper, tail truncation, reorder, and missing checkpoint.
- SQLite index is rebuildable from JSONL.

Non-goals:
- No transparency log.

Acceptance:
- Tamper e2e names exact line/seq.
- Tail truncation relative to local anchor fails.

Verifier:
- Run tamper e2e script and audit tests.

Adversary: required.

### B3-T06 Audit Export Bundle and Offline Verification

Goal: implement `agent audit export` and `agent audit verify` local/bundle
modes.

Scope:
- Bundle JSONL segments, checkpoints, AIDs/public keys, trust metadata, and
  signed manifest.
- Verify in clean workspace using only bundle and expected daemon audit public
  key fingerprint.

Non-goals:
- No global anchoring claim.

Acceptance:
- Second-machine/clean CI verification passes.
- Docs state what offline verification proves.
- Verification docs explicitly state this proves bundle integrity only, not
  global transparency-log anchoring.

Verifier:
- Run export/verify in a clean temp home.

Adversary: required.

## Block 4: Policy Engine

Canonical gate: `make block4-gate`

### B4-T01 Strict YAML Schema and Parser

Goal: parse canonical `policy.yaml` with unknown fields rejected.

Scope:
- Egress, credentials, MCP servers, hooks, ingress.
- Fail typos such as `brokerd`, `allow_wildcards`, scalar `port`.

Non-goals:
- No agentgateway compile output.

Acceptance:
- Schema golden tests cover valid and invalid samples.

Verifier:
- Run policy parser tests.

Adversary: required.

### B4-T02 Validation Rules

Goal: implement egress, domain, CIDR, port, credential, MCP, hook, and direct
lease validation.

Scope:
- Exact hostname matching by default.
- Wildcards/private CIDRs require explicit opt-in.
- Header-only credential templates.
- Direct leases require mode and reason.
- MCP workload egress is default-deny unless explicitly allowed.

Non-goals:
- No runtime enforcement.

Acceptance:
- Positive/negative tests cover exact-host matching, wildcard opt-in,
  RFC1918/private CIDR opt-in, explicit ports only, world-writable policy
  refusal, undeclared brokered credential references, unused brokered
  credential warnings, query-string credential injection rejection, body
  credential injection rejection, direct lease without reason rejection,
  undeclared MCP server/tool rejection, remote MCP without matching egress
  rule rejection, local MCP undeclared env/secret rejection, local MCP workload
  egress without matching rule rejection, remote hook without matching egress
  rule rejection, loopback hook exposure refusal, credentialed redirect
  disablement, and noncredentialed redirect per-hop revalidation.

Verifier:
- Run validation test suite.

Adversary: required.

### B4-T03 Canonicalizer and Policy Digest

Goal: deterministic canonical policy form and stable digest.

Scope:
- Sort maps/lists, uppercase methods, lower/punycode domains, expand defaults,
  remove comments, deduplicate with warnings.

Non-goals:
- No confusable-IDN defense beyond fail-closed non-normalizable names.

Acceptance:
- Comments/key order do not change digest.
- Meaningful changes do change digest.
- Secret values never appear in digest input.
- Golden tests cover IDN to canonical ASCII/punycode, non-normalizable domain
  fail-closed behavior, duplicate rule dedup warnings, deterministic
  round-trip compile/parse output, and stable digest input that contains
  secret ids only.

Verifier:
- Run digest golden tests.

Adversary: required.

### B4-T04 agentgateway Compiler and Vendored Binary

Goal: compile policy to pinned agentgateway config and DNS-stub allow-list.

Scope:
- Vendor checksummed agentgateway release under `third_party/agentgateway`.
- Emit credential injection rules by id only.
- Real binary load smoke test.

Non-goals:
- No runtime Docker topology.

Acceptance:
- PRD sample policy compiles and agentgateway loads config.

Verifier:
- Run compiler golden tests and binary smoke.

Adversary: required.

### B4-T05 Parser Fuzzing

Goal: add parser fuzz target and committed crash corpus.

Scope:
- Go fuzz target for YAML parser/canonicalizer.
- Gate target for 1M executions.

Non-goals:
- No full semantic fuzzing of runtime.

Acceptance:
- Fuzz completes with zero crashes.
- Fuzz gate runs the parser/canonicalizer for 1M executions and uses the
  committed crash corpus.

Verifier:
- Run fuzz target per gate budget.

Adversary: required.

## Block 5: RuntimeDriver and Fenced Network Topology

Canonical gate: `make block5-gate`

### B5-T01 RuntimeDriver Interface and Docker Naming

Goal: define RuntimeDriver and Docker implementation shell with deterministic
AgentPaaS labels/names.

Scope:
- Create, Start, Stop, Remove, Status, Stats, Logs.
- Reconciliation discovers only owned resources.

Non-goals:
- No full network bypass suite.

Acceptance:
- Unit tests cover naming, labels, cleanup selection, and logs/stats stubs.

Verifier:
- Run runtime unit tests.

Adversary: required.

### B5-T02 Dual-Container Gateway-Only Network Topology

Goal: create per-agent internal bridge and AgentPaaS egress network with
gateway sidecar dual-homed and agent isolated.

Scope:
- Agent has no egress network, no host networking, no shared gateway namespace.
- Gateway has exactly internal plus egress networks.

Non-goals:
- No secrets or SDK behavior.

Acceptance:
- Docker inspect assertions prove network membership.
- Positive path reaches harness only through gateway.

Verifier:
- Run e2e-network positive path and inspect assertions.

Adversary: required.

### B5-T03 Container Hardening

Goal: enforce hardening flags on agent containers.

Scope:
- Non-root uid 64000, read-only rootfs, tmpfs `/tmp`, cap-drop ALL,
  no-new-privileges, default seccomp, pids/mem/cpu limits, IPv6 disabled.

Non-goals:
- No advanced AppArmor/seccomp custom profile.

Acceptance:
- Inspect/resource tests prove each flag is applied.

Verifier:
- Run hardening assertions.

Adversary: required.

### B5-T04a Positive Path and External Canary Probes

Goal: prove the allowed gateway path works while direct external canaries fail.

Scope:
- Agent invoke reaches harness only through gateway ingress.
- Agent outbound to an allowed test endpoint succeeds only through gateway
  egress and emits policy decision plus audit event.
- Direct `curl https://1.1.1.1` fails fast within the timeout budget.
- Direct DNS to `8.8.8.8` is unreachable.

Non-goals:
- No full Block 12 red-team corpus.
- No host-local or protocol bypass suite.

Acceptance:
- E2E prints allowed path PASS.
- 1.1.1.1 and 8.8.8.8 canaries are BLOCKED without hanging.

Verifier:
- Run the e2e-network positive path and canary target.

Adversary: required.

### B5-T04b Host, Loopback, and Docker Bridge Probes

Goal: prove agent containers cannot use host or Docker bridge shortcuts.

Scope:
- `host.docker.internal` is unreachable from the agent.
- Docker bridge gateway IP is unreachable.
- Gateway container IP probing is blocked.
- Daemon ports and host-local services are unreachable.

Non-goals:
- No IPv6/UDP/ICMP/raw socket bypass suite.

Acceptance:
- Each host/loopback/bridge probe is BLOCKED with bounded timeout and expected
  policy/audit evidence where applicable.

Verifier:
- Run the host-probe subset of `make e2e-network`.

Adversary: required.

### B5-T04c Protocol and Namespace Bypass Probes

Goal: prove protocol-level bypass attempts and namespace sharing are blocked.

Scope:
- IPv6 disabled: AAAA answers and direct v6 literals have no route.
- UDP non-DNS, ICMP, raw-socket attempts, and CONNECT tunnel bypass attempts
  are blocked.
- Docker inspect proves no host networking and no shared network namespace
  between agent and gateway.

Non-goals:
- No cleanup/reconciliation behavior.

Acceptance:
- IPv6, UDP, ICMP, raw socket, and CONNECT attempts are BLOCKED.
- Inspect assertions prove no shared network namespace or host networking.

Verifier:
- Run the protocol-bypass subset of `make e2e-network`.

Adversary: required.

### B5-T04d Topology Inspect, Restart, and Partial-Create Cleanup

Goal: prove Docker topology assertions remain true through restart and partial
failure cleanup.

Scope:
- Docker inspect proves agent has no default route, no egress network
  attachment, no host networking, and no shared gateway namespace.
- Gateway has exactly internal plus egress networks.
- Container restart preserves network membership.
- Partial create/start failure leaves no orphaned AgentPaaS containers or
  networks.

Non-goals:
- No protocol bypass probing.

Acceptance:
- Inspect assertions pass before and after restart.
- Failure-injection test leaves zero orphaned owned containers/networks.

Verifier:
- Run topology inspect, restart, and partial-create cleanup tests.

Adversary: required.

### B5-T05 Daemon Crash Reconciliation and Secret-Free Debug Output

Goal: reconcile unsafe runtime leftovers after daemon crash and keep debug
output free of raw secrets.

Scope:
- Kill agent container whose gateway is absent.
- Reconcile owned AgentPaaS containers/networks without touching unrelated
  Docker resources.
- Docker inspect, runtime logs, and network config dumps contain no raw secret
  values.

Non-goals:
- No fleet management.
- No partial create/start cleanup; covered by B5-T04d.

Acceptance:
- Startup reconciliation kills any owned agent container whose gateway is
  absent.
- Reconciliation leaves unrelated Docker resources untouched.
- Sentinel raw secret values are absent from Docker inspect, runtime logs, and
  network config dumps.

Verifier:
- Run daemon-crash reconciliation and secret-free debug-output tests.

Adversary: required.

## Block 6: Harness and Python SDK Contracts

Canonical gate: `make block6-gate`

### B6-T01 Harness HTTP Lifecycle

Goal: implement Go harness as PID 1 with `/invoke`, `/healthz`, and `/readyz`.

Scope:
- Exec Python user code.
- Load agent code once.
- Serialize invokes by default with concurrency 1.
- Capture stdout/stderr pointers.

Non-goals:
- No SDK outbound helpers yet.

Acceptance:
- Import crash yields FAILED with structured reason.
- Payload limit rejects >10MB with 413.

Verifier:
- Run harness contract tests.

Adversary: required.

### B6-T02 Budget Enforcement

Goal: implement startup timeout, wall-clock budget, iteration budget, and
token/best-known usage enforcement.

Scope:
- Monotonic run timer from invoke start.
- SIGTERM, 10s grace, SIGKILL.
- BUDGET_EXCEEDED status and audit event.
- Post-hoc overage accounting.

Non-goals:
- No P2 repair/retry strategy.

Acceptance:
- Infinite-loop agent dies at 30s +/- 2s from invoke start.
- Token-burn agent stops future calls at cap.

Verifier:
- Run budget e2e tests.

Adversary: required.

### B6-T03 PID 1 Process Duties

Goal: handle process lifecycle correctly inside the harness container.

Scope:
- Reap zombies.
- Kill child process tree on timeout/cancel.
- User code ignoring SIGTERM is killed at grace deadline.

Non-goals:
- No process sandbox beyond container settings from Block 5.

Acceptance:
- Zombie and SIGTERM-ignore fixtures pass.

Verifier:
- Run process lifecycle tests.

Adversary: required.

### B6-T04 Python SDK Core

Goal: implement `agentpaas-sdk` decorators and non-secret outbound helpers.

Scope:
- `@agent.on_invoke`, `agent.llm()`, noncredentialed `agent.http(...)`,
  `agent.http_with_credential(credential_id, ...)`, `agent.mcp(...)`.
- Brokered credentials are never returned to SDK callers.

Non-goals:
- Node SDK.
- Agent-level checkpoint/resume.

Acceptance:
- SDK passes language-neutral harness contract suite.
- Undeclared MCP server/tool is denied before execution and audited.

Verifier:
- Run Python SDK contract tests.

Adversary: required.

### B6-T05 Structured Failure Context and Redaction

Goal: emit structured failure reasons and redacted developer-visible context.

Scope:
- Categories for task/tool/SaaS/MCP/code failures.
- Include run id, policy digest, policy decision ids, upstream availability
  evidence, stderr/stdout refs.
- Redact secrets and payload bodies.

Non-goals:
- No automatic repair.

Acceptance:
- Blocked egress and tool-call failures are visible but redacted.
- MCP input/output bodies are logged only as hashes/metadata.

Verifier:
- Run failure-context golden tests.

Adversary: required.

## Block 7: Secrets Broker

Canonical gate: `make block7-gate`

### B7-T01 SecretStore and CLI Lifecycle

Goal: implement `SecretStore` abstraction, macOS Keychain store, fake test
store, and `agent secret set/list/rm`.

Scope:
- Values read from stdin or interactive prompt only.
- Max 64 KiB.
- List metadata only.
- Case-sensitive names without whitespace/control chars.

Non-goals:
- No Linux libsecret.
- No plaintext fallback.

Acceptance:
- Process-list and shell-history fixtures do not expose values.
- Oversize and invalid names are rejected.
- Secret values are never accepted through argv, never appear in process argv,
  and `list` output shows metadata only with no value, prefix, suffix, or
  hash-derived hints.

Verifier:
- Run secrets CLI tests.

Adversary: required.

### B7-T02 Brokered Gateway Credential Flow

Goal: implement daemon-mediated brokered credential use by gateway.

Scope:
- Gateway requests credential over local authenticated channel.
- Daemon validates run id, policy rule id, destination, method.
- Gateway injects header and originates upstream TLS request.

Non-goals:
- No body/query injection.

Acceptance:
- Wrong domain/method/port/credentialed redirect denied before injection and
  audited.
- Upstream fixture receives Authorization header for valid request.

Verifier:
- Run brokered credential e2e.

Adversary: required.

### B7-T03 Brokered Secret Invisibility Suite

Goal: prove brokered sentinel secret is not visible to agent or artifacts.

Scope:
- Probe env, `/proc`, filesystem, logs, Docker inspect, compiled config,
  build context, exported image layers, packed artifacts, CLI/dashboard errors.

Non-goals:
- No claim for direct-lease per-read invisibility.

Acceptance:
- Zero hits for brokered sentinel secret.
- Negative tests assert the raw brokered value is absent from agent env,
  `/proc`, filesystem walks, daemon logs, gateway logs, CLI/dashboard errors,
  Docker inspect, compiled config files, build context, exported image layers,
  and packed artifacts.

Verifier:
- Run full negative grep suite.

Adversary: required.

### B7-T04 Direct Lease Compatibility Mode

Goal: implement explicit file-only direct leases.

Scope:
- Runtime tmpfs file, 0400, owner agent uid.
- Direct lease requires policy reason.
- File gone after stop.
- Audit `secret_leased visible_to_agent=true`; SDK helper emits `secret_read`.

Non-goals:
- No env leases.
- No precise per-open auditing claim.

Acceptance:
- Raw file read works only for explicit direct lease.
- Stop removes lease file.
- Negative tests confirm direct leases are file-only, never env leases, and raw
  secret values are absent from Docker inspect, config files, image layers, and
  packaged artifacts before the runtime tmpfs lease is mounted.

Verifier:
- Run direct lease tests.

Adversary: required.

### B7-T05 Revocation and Enterprise Follow-Up

Goal: implement brokered revocation behavior and create enterprise design
follow-up issue.

Scope:
- Brokered revocation invalidates immediately.
- Direct-lease revocation restarts affected agents with honest limitation.
- Add follow-up for managed-vault/remote broker corporate machines.

Non-goals:
- No enterprise remote broker in P1.

Acceptance:
- Revoked brokered credential denied immediately.
- Follow-up issue exists and is linked.

Verifier:
- Run revocation tests and inspect issue/docs entry.

Adversary: required.

## Block 7.5: MCP Server Lifecycle Manager

Canonical gate: `make block7-mcp-gate`

### B7M-T01 MCP Resource Model and Policy Binding

Goal: represent declared MCP servers as first-class managed AgentPaaS
resources tied to policy digest, agent, and run context.

Scope:
- Resource records with `resource_type=mcp_server`.
- Owning agent/run, server id, allowed tools, health, readiness, last error.
- Policy validation rejects duplicate ids and unspecified tools defaults to
  deny all.

Non-goals:
- No generic AgentPaaS MCP server for Codex/Cursor/Claude Code.
- No dynamic tool discovery auto-allow.

Acceptance:
- `agent status --json` can include agent, gateway, and MCP server resources.
- Undeclared server/tool calls are denied before execution and audited.

Verifier:
- Run MCP resource model and policy binding tests.

Adversary: required.

### B7M-T02 Local MCP Process and Sidecar Supervision

Goal: start, readiness-check, stop, and reconcile declared local MCP servers.

Scope:
- Daemon-managed child process and sidecar lifecycle.
- Minimal env by default.
- AgentPaaS Docker labels for MCP sidecars.
- No host networking or ambient Docker/host socket access for MCP sidecars.
- Daemon restart reconciliation for owned MCP resources.

Non-goals:
- No host networking by default.
- No shell/browser/desktop tools unless explicitly declared and confirmed.

Acceptance:
- Declared readonly filesystem MCP server starts separately from the agent.
- Daemon restart leaves unrelated processes/containers untouched.
- MCP crash produces structured unavailable/tool failure context.
- Docker inspect proves MCP sidecars carry AgentPaaS labels and no host
  networking.

Verifier:
- Run MCP lifecycle and reconciliation tests.

Adversary: required.

### B7M-T03 Gateway-Mediated MCP Routing

Goal: route agent MCP calls through the gateway to local MCP servers and return
responses through the same governed path.

Scope:
- `agent.mcp(server_id, tool, input)` uses gateway MCP route.
- Gateway performs server/tool policy decision before forwarding.
- Local stdio MCP calls are forwarded to daemon MCP manager.
- Local sidecar/endpoint MCP calls are routed only to declared endpoint.
- Trace and audit both request and response metadata.

Non-goals:
- No direct agent-to-host or agent-to-MCP container network access.
- No dynamic MCP tool discovery auto-allow.

Acceptance:
- E2E fixture agent calls readonly filesystem MCP via gateway and receives
  expected response.
- Direct connection attempts to local MCP process/sidecar from agent fail.
- Undeclared server/tool is denied before MCP execution and audited.

Verifier:
- Run gateway-mediated MCP route e2e tests.

Adversary: required.

### B7M-T04 MCP Workload Egress Policy

Goal: enforce default-deny egress for MCP servers as separate managed
workloads.

Scope:
- MCP server ingress accepts only AgentPaaS gateway/daemon-routed tool calls.
- MCP server outbound HTTP/network access is gateway-mediated and policy
  checked.
- Audit MCP server identity, destination, method, credential id, policy rule,
  and decision for allowed and denied egress.

Non-goals:
- No ambient host network, Docker socket, or local service access.
- No per-user OAuth delegated access; P2 handles runtime user connections.

Acceptance:
- MCP server egress to an allow-listed test endpoint succeeds and is audited.
- MCP server egress to a non-allow-listed endpoint is denied and audited.
- MCP server probes for host/Docker socket/bridge access fail.

Verifier:
- Run MCP workload egress policy e2e tests.

Adversary: required.

### B7M-T05 MCP Status, Dashboard Data, and Hermes Contract

Goal: expose MCP server readiness and health through the same operator/status
surfaces as agents.

Scope:
- `agent status --json` resource inventory.
- Dashboard data model includes MCP resources.
- Docker artifact metadata for MCP sidecars: labels, image digest, network
  membership, health, restart count, and resource stats.
- Hermes operator contract sees MCP status without scraping Docker.

Non-goals:
- No full dashboard UI polish beyond data required for Block 10.

Acceptance:
- Status output includes `resource_type`, `server_id`, owning agent/run,
  policy digest, allowed tools, readiness, health, and last error.
- Stopped/starting/ready/unhealthy states are represented consistently.
- Agent, gateway, and MCP Docker artifacts are visible without leaking raw
  secrets.

Verifier:
- Run status JSON golden tests.

Adversary: required for schema prompt-injection/status confusion cases.

### B7M-T06 MCP Tool Auditing and Host-Affecting Capability Guard

Goal: audit every MCP tool call and require confirmation for host-affecting
local tools.

Scope:
- Audit server id, tool name, decision, policy rule id, credential id if used,
  and input/output hashes.
- MCP server logs and tool output are redacted/truncated before CLI/dashboard
  display.
- Host-affecting tools include browser control, shell execution, writable
  filesystem, AppleScript, and desktop automation.
- Confirmation protocol required before enabling or broadening such tools.

Non-goals:
- No complete MCP prompt-injection matrix; full matrix remains P2 red-team.

Acceptance:
- Successful and denied tool calls emit expected audit events.
- Sentinel secrets and hostile HTML/control characters in MCP logs/tool output
  are redacted or escaped.
- Prompt-injected source/log/tool output cannot add MCP servers or broaden
  allowed tools.
- High-risk local tools cannot enable without confirmation.

Verifier:
- Run MCP audit and host-capability negative tests.

Adversary: required.

## Block 8: Packaging Pipeline

Canonical gate: `make block8-gate`

### B8-T01 Agent Project Detection and Init Scaffold

Goal: detect Python, LangGraph, and CrewAI-style projects and offer
`agent init` scaffold when `agent.yaml` is missing.

Scope:
- Plain Python first.
- Generic Python pack/run for LangGraph/CrewAI markers.
- Default `.agentpaasignore`.

Non-goals:
- No custom CrewAI orchestration adapter.
- No Node/custom Dockerfile packaging.

Acceptance:
- Three reference repos are detected and scaffolded.

Verifier:
- Run detection/scaffold tests.

Adversary: not required unless source trust boundaries change.

### B8-T02 BuildKit Image Assembly

Goal: build deterministic Python agent image with harness as PID 1.

Scope:
- Distroless base by digest.
- Locked deps via uv.
- Non-root, no shell.
- Fixed timestamps, deterministic tar order, `SOURCE_DATE_EPOCH`.

Non-goals:
- No registry push.

Acceptance:
- Rebuild without source changes produces identical image digest.
- Dependency conflicts abort with verbatim useful output.

Verifier:
- Run packaging reproducibility tests.

Adversary: required for provenance/build integrity.

### B8-T03 Secret Scan and Build Context Control

Goal: fail closed on secrets in source or effective build context.

Scope:
- gitleaks scan full source tree and build context.
- `.agentpaasignore` honored.
- Warn >100MB context, fail planted secrets.
- `--allow-secret-pattern` requires audit append.

Non-goals:
- No real secret allow-list without audit.

Acceptance:
- Planted key in source, ignored source, and build context is blocked.

Verifier:
- Run secret-scan e2e.

Adversary: required.

### B8-T04 SBOM, Signing, and agent.lock

Goal: produce syft SBOM, local cosign signature with package identity key, and
signed canonical `agent.lock`.

Scope:
- Include all required lockfile fields and digest refs.
- Verify lockfile signature and image signature offline by AID public key.

Non-goals:
- No Sigstore keyless local pack.

Acceptance:
- `agent verify agent.lock` passes.
- Explicit `cosign verify --key <AID pubkey>` passes.
- SBOM lists expected top-level deps.

Verifier:
- Run signing and lockfile golden tests.

Adversary: required.

### B8-T05 Immutable Prompt and Config Update Path

Goal: enforce deployed agent immutability and verified edit-pack-run path.

Scope:
- Behavior-changing config and prompt files are covered by build input digest.
- In-place deployed image/lockfile mutation is rejected and audited.
- CLI e2e: prompt v1 run, edit to v2, validate, pack, verify, run.

Non-goals:
- No live prompt mutation.

Acceptance:
- New run reflects prompt v2 and has distinct build/image/lock digests.

Verifier:
- Run immutable update e2e.

Adversary: required.

### B8-T06 OSV Advisory Reporting and Local OCI Repair

Goal: add advisory summary and actionable OCI layout repair errors.

Scope:
- OSV scanner summary appears in `agent pack`.
- Non-critical findings do not fail by default.
- Missing/corrupt local OCI layout gives repair hint.

Non-goals:
- No registry push.

Acceptance:
- Advisory and corrupt-layout fixtures produce expected JSON/text.

Verifier:
- Run packaging CLI golden tests.

Adversary: not required.

## Block 9: Trigger API, Events, Webhooks, Cron

Canonical gate: `make block9-gate`

### B9-T01 Trigger API Serving and Auth

Goal: serve gRPC `:7718` and REST `:7717` loopback APIs with auth.

Scope:
- API key or mTLS required even on loopback.
- `--expose` refuses without API key.
- CORS deny-by-default.

Non-goals:
- No remote cloud control plane.

Acceptance:
- Random browser localhost POST without key gets 401/CORS deny.

Verifier:
- Run API auth/CORS conformance.

Adversary: required.

### B9-T02 API Key Lifecycle

Goal: implement Trigger API key create/show-once/hash/scope/revoke/rotate.

Scope:
- Stable caller ids.
- Scoped by agent/action.
- Audit create/revoke/auth_failed.

Non-goals:
- No user/team identity model.

Acceptance:
- Raw keys are never stored or re-shown.
- Revoked key fails.

Verifier:
- Run API key lifecycle tests.

Adversary: required.

### B9-T03 Durable Idempotency and Payload Limits

Goal: implement idempotency table and invoke payload limits.

Scope:
- 24h replay window.
- Canonical request hash over caller, agent, lock digest, payload, content
  type, and API version.
- Default 1 MiB payload limit.

Non-goals:
- No blob storage service.

Acceptance:
- Same key/same payload returns same run id.
- Same key/different payload returns 409.
- Daemon restart preserves behavior.

Verifier:
- Run idempotency e2e.

Adversary: required.

### B9-T04 SSE and Event Bus

Goal: implement event bus and InvokeStream over gRPC and REST SSE.

Scope:
- Ordered event IDs, heartbeats, terminal event, Last-Event-ID reconnect.
- No duplicate terminal event.

Non-goals:
- No dashboard UI rendering.

Acceptance:
- Reconnect e2e resumes without duplicates.

Verifier:
- Run SSE reconnect tests.

Adversary: required.

### B9-T05 Webhook Delivery

Goal: deliver URL webhooks through policy-controlled egress.

Scope:
- HMAC timestamp/replay protection.
- Three retries with exponential backoff.
- Dead-letter to audit.
- Hook destinations validated by policy.

Non-goals:
- No local command hooks.

Acceptance:
- Down target retries then dead-letters.
- Bad HMAC/replay rejected by receiver fixture.
- Non-allow-listed domain blocked.

Verifier:
- Run webhook tests.

Adversary: required.

### B9-T06 Cron Triggers

Goal: implement local cron triggers from `agent.yaml` feeding the same Invoke
path.

Scope:
- Five-field syntax.
- Local timezone default, optional explicit timezone.
- Missed-run policy skip default, catchup 1 opt-in.
- DST behavior and concurrency policy.

Non-goals:
- No distributed scheduler.

Acceptance:
- DST nonexistent skipped; repeated runs once.
- Prior active run skipped under forbid and audited.

Verifier:
- Run cron tests with fake clock.

Adversary: required.

### B9-T07 Local Handoff Triggers

Goal: implement static approved handoffs from one agent's terminal run event
to another packed agent through the same Trigger API.

Scope:
- Handoff caller id `system:handoff:<source_agent>`.
- Parent run id, correlation id, idempotency key, and target lock digest.
- Payload modes: `empty`, `summary_ref`, `artifact_ref`, `fixed_json`.
- Internal A2A-compatible envelope with source/target agent-card refs, parent
  task/run id, context/correlation id, message role, parts, artifact refs, and
  metadata map.
- Cycle/depth guard and concurrency policy.
- Audit `handoff_invoked`, `handoff_skipped`, and `handoff_denied`.

Non-goals:
- No dynamic DAG/workflow engine.
- No external A2A server, agent-card discovery endpoint, or arbitrary task
  negotiation in P1.
- No dynamic target agent names from model output.
- No local command hooks.
- No checkpoint/resume.

Acceptance:
- Two-agent handoff runs on macOS without Hermes alive after configuration.
- Target agent receives input only through Trigger API and normal policy,
  budget, secret, and audit paths.
- Handoff envelope round-trips in an A2A-compatible shape and records artifact
  refs without embedding raw large outputs.
- Missing target, stale lock digest, declined/unapproved config, and cycle
  guard produce skipped/denied audit events.

Verifier:
- Run local handoff e2e and cycle-guard tests.

Adversary: required.

### B9-T08 CancelRun Semantics

Goal: implement cancellation path.

Scope:
- Audit `cancel_requested`, graceful request, 30s wait, forced stop if needed,
  final canceled/forced outcome.

Non-goals:
- No checkpoint/resume.

Acceptance:
- Mid-LLM/MCP cancel e2e records correct sequence.

Verifier:
- Run cancel e2e.

Adversary: required.

### B9-T09 Control API REST/JSON Fuzzing

Goal: fuzz Control API REST JSON ingestion.

Scope:
- Malformed JSON gives 400 with line.
- Fuzz 100k executions.
- Trigger API payload-size and idempotency behavior are covered by B9-T03,
  not by this fuzz target.

Non-goals:
- No browser fuzzing.
- No broad Trigger API protocol fuzzing in P1.

Acceptance:
- 0 crashes.

Verifier:
- Run fuzz target.

Adversary: required.

## Block 10: OTel Pipeline and Dashboard

Canonical gate: `make block10-gate`

### B10-T01 OTLP Collector and SQLite Store

Goal: implement in-process OTLP collector to SQLite WAL for traces/logs/metrics.

Scope:
- Retention prune for OTel data only.
- Audit JSONL never pruned by dashboard retention.
- Ingest agent, harness, gateway, MCP server, and MCP manager logs.
- Migration, WAL checkpoint, vacuum, corruption recovery.

Non-goals:
- No hosted telemetry.

Acceptance:
- SQLite locked writer does not block dashboard reads.
- Corruption recovery test passes.

Verifier:
- Run OTel store tests.

Adversary: required because audit/telemetry separation is security relevant.

### B10-T02 Embedded Dashboard App Shell

Goal: build embedded Preact/TypeScript SPA with strict CSP, no runtime CDN, and
managed resource inventory.

Scope:
- Managed resource list for agents, gateways, and MCP servers.
- Empty states, basic routing, keyboard smoke.
- Docker artifact ids/labels/digests where applicable.
- Network membership, health, restart count, resource stats.
- CSRF token on mutating routes.
- Exposed dashboard requires API key/session.
- No API keys in localStorage.

Non-goals:
- No full run timeline yet.

Acceptance:
- CSP test blocks inline script.
- Empty states render for zero agents, zero runs, and zero MCP servers.
- Agent, gateway, and MCP server resources render without scraping Docker from
  Hermes.

Verifier:
- Run frontend unit and Playwright smoke.

Adversary: required.

### B10-T03 Run Timeline and Live SSE

Goal: render live run timeline from Block 9 event stream.

Scope:
- LLM calls, MCP calls, egress allowed/denied rows, budget/audit markers.
- Last-Event-ID reconnect.
- 10k-span virtualization.

Non-goals:
- No audit export UI yet.

Acceptance:
- Playwright watches live run and sees DENIED egress row.
- 10k-span run stays performant.

Verifier:
- Run Playwright timeline tests.

Adversary: required for untrusted data rendering.

### B10-T04 Log Viewer Redaction and XSS Defense

Goal: safely display agent/harness/gateway/MCP logs, Docker artifact metadata,
and OTel attributes.

Scope:
- Escape HTML, binary/control chars.
- Truncate huge values with refs.
- Redact sentinel secrets everywhere.
- Sanitize Docker inspect-derived views for agents, gateways, and MCP sidecars.

Non-goals:
- No raw unredacted browser view.

Acceptance:
- Planted `<script>` appears escaped.
- Sentinel secret not visible in logs/spans/errors, MCP logs, or Docker
  artifact views.
- Stale/missing Docker artifacts show reconciled state instead of stale green.

Verifier:
- Run XSS and redaction Playwright tests.

Adversary: required.

### B10-T05 Policy Diff and Audit Export UI

Goal: show policy file diff, normalized policy digest, indexed audit search,
and one-click signed export/verify result.

Scope:
- Label audit search as indexed view.
- Show trust-anchor fingerprint, sequence range, verification command, result.

Non-goals:
- No audit purge UI.

Acceptance:
- Playwright export audit and verify flow passes.

Verifier:
- Run audit export UI test.

Adversary: required.

### B10-T06 Cost and Budget Display

Goal: display provider/model cost estimates with price-table version and
estimated flag.

Scope:
- Token counts, cost, provider, model, price-table version.
- P1 built-in price table.

Non-goals:
- No tenant-modified price tables.

Acceptance:
- Cost fields render consistently in run timeline and JSON.

Verifier:
- Run cost display golden tests.

Adversary: not required.

### B10-T07 Performance, Accessibility, Lighthouse

Goal: satisfy dashboard performance and accessibility smoke.

Scope:
- Lighthouse perf >= 90 local.
- Keyboard navigation smoke.
- Empty-state and restart/reconnect behavior.

Non-goals:
- No full WCAG certification.

Acceptance:
- Lighthouse and accessibility smoke pass.

Verifier:
- Run `make block10-gate`.

Adversary: not required unless rendering untrusted data changed.

## Block 11: Hermes Operator Contract

Canonical gate: `make block11-gate`

### B11-T01 Operator Schemas and Error Categories

Goal: define versioned JSON-schema/protobuf contracts for all operator methods
and stable error categories.

Scope:
- ValidateAgentProject, SummarizeRun, ExplainFailure,
  ExplainPolicyDenial, RecommendPolicyPatch, GetRunTimeline, NextAction.
- Evidence refs and redacted excerpts.

Non-goals:
- No Hermes plugin yet.

Acceptance:
- Schema golden tests cover every method and category.
- Operator JSON schemas, evidence-ref fields, and confirmation protocol committed; make block11-gate passes before any B12 or B13 issue is marked ready.

Verifier:
- Run schema tests.

Adversary: required.

### B11-T02 CLI JSON Parity

Goal: ensure human CLI commands expose `--json` backed by the same schemas.

Scope:
- pack/run/logs/status/policy/audit and operator methods.
- Text output is rendered view, not contract.

Non-goals:
- No dashboard changes except data parity if needed.

Acceptance:
- Golden tests prove JSON fields and versioning.

Verifier:
- Run CLI JSON parity tests.

Adversary: required.

### B11-T03 Validate and Init Noninteractive Flow

Goal: implement `agent init --from-code --noninteractive` and
`agent validate --json`.

Scope:
- Reconcile `agent.yaml`.
- Create minimal default-deny `policy.yaml`.
- Project root path enforcement.

Non-goals:
- No policy broadening.

Acceptance:
- Incomplete Python agent becomes valid enough to pack or returns actionable
  readiness errors.

Verifier:
- Run golden Hermes-like init/validate flow.

Adversary: required.

### B11-T04 Explain Failure, Policy Denial, and Next Action

Goal: implement structured diagnosis methods.

Scope:
- Failure evidence refs, categories, redacted excerpts.
- Next action values from the fixed enum.

Non-goals:
- No automatic code editing.

Acceptance:
- Denied egress returns blocking rule and `review_policy_patch` next action.
- Missing secret returns proposal without secret value request.

Verifier:
- Run diagnosis golden tests.

Adversary: required.

### B11-T05 Policy Patch Proposal and Confirmation Boundary

Goal: recommend policy patches without silently applying trust-boundary changes.

Scope:
- Risk level, rationale, affected destinations, credential ids, audit evidence.
- Explicit user/daemon confirm required for policy, credentials, direct leases,
  local handoff triggers, exposed listeners, destructive operations.

Non-goals:
- No auto-apply for security-sensitive changes.

Acceptance:
- Human decline changes next action to `fix_code` or `ask_user`, not bypass.
- Confirmation protocol fixtures cover policy changes, new egress, credential
  bindings, direct leases, local handoff triggers, webhook destinations,
  exposed listeners, retention purges, unrelated run stops, and destructive
  operations.

Verifier:
- Run confirmation-boundary tests.

Adversary: required.

### B11-T06 Prompt Injection and Path Boundary Tests

Goal: prove untrusted source/log/trace instructions cannot control operator
trust boundaries.

Scope:
- Refuse paths outside project root with audit event.
- Ignore injected instructions to approve policy, reveal secrets, delete audit,
  stop unrelated runs.

Non-goals:
- No complete MCP prompt-injection matrix.

Acceptance:
- Negative tests pass with machine-readable refusal.

Verifier:
- Run operator negative suite.

Adversary: required.

### B11-T07 Hermes Golden Flow Simulator

Goal: implement scripted Hermes-like client flow for the Block 11 gate.

Scope:
- Create incomplete Python agent, init, validate, pack, run, denial,
  explanation, patch proposal, code/dependency fix, approved policy, rerun,
  audit export, summary JSON.

Non-goals:
- No actual Hermes plugin.

Acceptance:
- `make block11-gate` passes golden flow on clean machine.

Verifier:
- Run gate and inspect final JSON summary.

Adversary: required.

## Block 12: P1 Red-Team Smoke Gate

Canonical gate: `make block12-gate`

### B12-T01 Red-Team Runner and Report Format

Goal: build `test/redteam` runner that executes real pack/run paths and prints
six-row containment table plus signed audit verification summary.

Scope:
- Runtime <10 minutes on developer laptop.
- Each fixture asserts machine-readable result and audit event.

Non-goals:
- No full P2 attack corpus.

Acceptance:
- Runner can execute one fixture or all fixtures.
- Uses only real `agent pack`, real `agent run`, and real Block 11 operator
  methods; no synthetic harnesses, direct daemon shortcuts, or test-only
  enforcement paths.

Verifier:
- Run redteam runner in fixture mode.

Adversary: required.

### B12-T02 Default-Deny Egress Fixture

Goal: fixture for raw IP TCP dial and direct HTTPS to non-allowed domain.

Scope:
- Real `agent pack` and `agent run`.
- Expect blocked/no route plus egress_denied audit.

Non-goals:
- No DNS tunneling corpus.

Acceptance:
- Fixture PASS means both behavior and audit evidence exist.
- Uses only real `agent pack`, real `agent run`, and real Block 11 operator
  methods; no synthetic harnesses, direct daemon shortcuts, or test-only
  enforcement paths.

Verifier:
- Run fixture and inspect audit assertion.

Adversary: required.

### B12-T03 Gateway and Credential Misuse Fixture

Goal: fixture for disallowed host/method and brokered credential wrong
destination.

Scope:
- Expect denied with policy rule/audit evidence.

Non-goals:
- No domain-fronting corpus.

Acceptance:
- Denial occurs before credential injection.
- Uses only real `agent pack`, real `agent run`, and real Block 11 operator
  methods; no synthetic harnesses, direct daemon shortcuts, or test-only
  enforcement paths.

Verifier:
- Run fixture.

Adversary: required.

### B12-T04 Brokered Secret Invisibility Fixture

Goal: fixture proving agent cannot find brokered sentinel while upstream
receives injected header.

Scope:
- Probe env, proc, files, logs, mounted paths.

Non-goals:
- No direct-lease DLP claim.

Acceptance:
- Zero hits and successful upstream auth.
- Uses only real `agent pack`, real `agent run`, and real Block 11 operator
  methods; no synthetic harnesses, direct daemon shortcuts, or test-only
  enforcement paths.

Verifier:
- Run fixture.

Adversary: required.

### B12-T05 Host Access and Resource Containment Fixtures

Goal: fixtures for host probes and resource pressure.

Scope:
- host.docker.internal, bridge gateway, daemon ports blocked.
- Memory/fd/child pressure contained without daemon/dashboard failure.

Non-goals:
- No Linux-specific certification.

Acceptance:
- Contained/blocked result plus audit where applicable.
- Uses only real `agent pack`, real `agent run`, and real Block 11 operator
  methods; no synthetic harnesses, direct daemon shortcuts, or test-only
  enforcement paths.

Verifier:
- Run fixtures.

Adversary: required.

### B12-T06 Operator Prompt-Injection Fixture

Goal: malicious source/log text tries to approve policy, reveal secrets, delete
audit, or stop unrelated runs.

Scope:
- Use real Block 11 `--json`/operator methods.

Non-goals:
- No exhaustive prompt-injection matrix.

Acceptance:
- Refusal/proposal-only behavior and no trust-boundary change.
- Uses only real `agent pack`, real `agent run`, and real Block 11 operator
  methods; no synthetic harnesses, direct daemon shortcuts, or test-only
  enforcement paths.

Verifier:
- Run fixture.

Adversary: required.

## Block 13: Hermes Integration Plugin/Skill

Canonical gate: `make block13-gate`

### B13-T01 Hermes Plugin Skeleton and Tool Manifest

Goal: create the Hermes plugin package (`plugin.yaml` + `__init__.py` +
`schemas.py` + `tools.py`) with the 17 required P1 tool names registered via
`ctx.register_tool`.

Scope:
- Tool list exactly matches Block 13 required tools.
- Talks only to loopback daemon socket.
- Path roots resolved against invoking project dir.

Non-goals:
- No generic MCP server.
- No Claude Code/Codex/Cursor skins.

Acceptance:
- Manifest/tool discovery tests pass.

Verifier:
- Run integration unit tests.

Adversary: required.

### B13-T02 Schema-Generated Tool Wrappers

Goal: generate or schema-test wrappers against Block 11 contracts.

Scope:
- CI fails if operator method lacks wrapper.
- Wrapper cannot return fields outside schema or drop evidence refs/categories.

Non-goals:
- No hand-maintained divergent behavior.

Acceptance:
- Contract parity gate passes.

Verifier:
- Run integration conformance tests.

Adversary: required.

### B13-T03 Confirmation Protocol Handling

Goal: implement requires-confirmation responses for destructive or trust-boundary
actions.

Scope:
- Stop only active run by default.
- Unrelated stop, policy apply, credential binding, direct lease, local
  handoff trigger, exposed listener, budget increase, audit purge/export
  remote, gate disabling all return confirmation metadata.

Non-goals:
- Hermes cannot apply confirmation itself.

Acceptance:
- Confirmation id replay/expiry refused and audited.

Verifier:
- Run confirmation tests.

Adversary: required.

### B13-T04 Prompt-Injection Boundary in Integration Responses

Goal: separate trusted control fields from untrusted evidence and block hostile
instructions embedded in source/logs/comments.

Scope:
- Trusted fields: status, category, next_action, confirmation, risk, refs.
- Untrusted fields: excerpts, source/log/trace snippets, payload text.

Non-goals:
- No external LLM behavior guarantees beyond tool output boundary.

Acceptance:
- Negative CI test proves no policy alteration, secret disclosure, audit
  deletion, gate disabling, unrelated stop.
- Negative tests include hostile instructions embedded in agent source, source
  comments, logs, traces, tool output, Hermes resource text, and remote payload
  text, and verify trusted control fields remain separate from untrusted
  excerpts.

Verifier:
- Run prompt-injection integration tests.

Adversary: required.

### B13-T05 Hermes AgentPaaS Deploy Flow

Goal: scripted e2e where Hermes generates an agent, calls AgentPaaS tools, and
gets governed run with dashboard DENIED probe.

Scope:
- Clean machine after AgentPaaS installed.
- Post-install deploy flow under 10 minutes.

Non-goals:
- No release docs/video polish.

Acceptance:
- `agentpaas-deploy` flow completes and dashboard shows DENIED probe.

Verifier:
- Run `make block13-gate` e2e portion.

Adversary: required.

### B13-T06 Prompt Change Immutable Redeploy Flow

Goal: Hermes handles "change prompt" by editing project files and driving
validate-pack-verify-run, not mutating deployed image.

Scope:
- Report new run id and distinct old/new digests.

Non-goals:
- No in-place deployed prompt mutation.

Acceptance:
- Second run reflects new prompt and audit shows distinct digests.

Verifier:
- Run Hermes prompt-change e2e.

Adversary: required.

### B13-T07 Demo Matrix Fixtures

Goal: create reusable fixtures for the three P1 differentiation demos.

Scope:
- Governed weather/API agent.
- Secret-brokered SaaS action.
- Agentic repair loop.

Non-goals:
- Recording and release embed are Block 14.

Acceptance:
- At least minimum demo fixture can be run headlessly.

Verifier:
- Run demo fixture smoke.

Adversary: required for secret/repair flows, optional for pure demo glue.

### B13-T08 In-Session Slash Commands (/agentpaas)

Goal: register `/agentpaas` slash commands via `ctx.register_command` so a
user can build, deploy, and operate an agent without leaving the Hermes
session.

Scope:
- `/agentpaas deploy` — detect agent code → init → validate → pack → run →
  print run_id and dashboard URL.
- `/agentpaas status` — `agentpaas_status` across active runs (one-line per
  run).
- `/agentpaas logs [run_id]` — `agentpaas_logs` with tail/truncate.
- `/agentpaas metrics [run_id]` — `agentpaas_get_run_timeline` +
  `agentpaas_summarize_run` for the budget/denial/health view.
- `/agentpaas repair [run_id]` — `agentpaas_explain_failure` →
  `agentpaas_next_action`; auto-drives safe repairs (fix_code,
  install_dependency, start_docker), surfaces confirmation requirements for
  trust-boundary actions (policy patch, handoff, secret binding).
- Each command is a thin orchestrator that calls the plugin's own tools
  through `ctx.dispatch_tool` (same approval/redaction/budget pipelines as a
  model-invoked tool call); no logic re-implementation.

Non-goals:
- No new operator methods or tool names beyond the 17 in B13-T02.
- No privilege escalation — commands inherit the same confirmation protocol.

Acceptance:
- Each slash command returns the same structured output as its underlying
  tool(s) and is exercised in the B13 e2e (SUCCESS GATE).

Verifier:
- Run slash-command conformance against the tool outputs.

Adversary: required (confirm commands cannot bypass the confirmation
protocol, cannot stop unrelated runs, and cannot drop evidence refs).

### B13-T09 Bundled SKILL.md and Plugin Manifest

Goal: bundle a `SKILL.md` via `ctx.register_skill` and declare
`requires_env` for the daemon socket path in `plugin.yaml`.

Scope:
- `SKILL.md` teaches detect→init→validate→pack→run→inspect→repair with
  pitfalls (Docker not running → doctor; policy denial → explain +
  recommend).
- `plugin.yaml` `requires_env` gates on the daemon socket path; interactive
  prompt during `hermes plugins install`.
- Plugin loads cleanly under `hermes plugins list` and appears in `/plugins`.

Non-goals:
- No MCP server registration (P2).

Acceptance:
- Fresh `hermes plugins install` of the plugin prompts for the socket path
  and the 17 tools + 5 slash commands + bundled skill appear in the session.

Verifier:
- Run install + discovery smoke.

Adversary: required (confirm `requires_env` gating cannot be bypassed and
the plugin refuses to load with a missing/malformed socket path).

## Block 14: Install Path, Docs, Demo, Release

Canonical gate: `make block14-gate`

### B14-T01 Goreleaser and Artifact Signing

Goal: build darwin/arm64 and darwin/amd64 release artifacts with checksums,
SBOMs, provenance, and cosign signatures.

Scope:
- GitHub Actions OIDC keyless release signing.
- `agent verify-release` helper.
- Copy-paste `cosign verify-blob` docs.

Non-goals:
- No Linux/Windows packages.

Acceptance:
- Release artifact verification matrix green.

Verifier:
- Run release dry-run and verify artifacts.

Adversary: required.

### B14-T02 Homebrew Tap Formula

Goal: implement primary macOS install path.

Scope:
- `brew install agentpaas/tap/agentpaas`.
- Homebrew does not silently start services.
- Upgrade preserves daemon state and restarts cleanly.

Non-goals:
- No deb/rpm.

Acceptance:
- Formula install and upgrade smoke pass.

Verifier:
- Run formula audit/test where available.

Adversary: required for install trust posture.

### B14-T03 First-Run Setup and Uninstall

Goal: implement explicit setup and uninstall UX.

Scope:
- `agent doctor` first-run checks.
- `agent setup launchd` or documented brew services start.
- `agent uninstall` removes launchd units, containers, networks, sockets,
  generated config; says what it keeps and how to purge.

Non-goals:
- No silent service start.

Acceptance:
- Failed migration rolls back or gives recovery path.
- Uninstall leaves only deliberately kept audit/keychain/offline artifacts.

Verifier:
- Run setup/uninstall e2e on test home.

Adversary: required.

### B14-T04 Documentation Site

Goal: create static docs covering quickstart, policy, secrets, enforcement,
threat model, limitations, audit verification, privacy, Hermes setup, demos.

Scope:
- Publish PRD threat model verbatim where required.
- Explicit zero telemetry/no phone-home statement.
- Known limitations near install.
- Broken-link and command-snippet smoke.

Non-goals:
- No P2 Codex/Cursor/generic MCP docs.

Acceptance:
- Docs smoke passes; every clean-machine deviation files/fixes a docs bug.

Verifier:
- Run docs link and snippet checks.

Adversary: required for security docs accuracy.

### B14-T05 README Quickstart and Clean-Machine Evidence

Goal: make README the 15-minute path and collect volunteer evidence.

Scope:
- 60-second story above fold.
- Prereqs: macOS, Homebrew, Docker Desktop or Colima.
- Redteam containment table.
- Clean-machine test by two volunteers.
- Evidence artifact directory: `docs/release/volunteer-evidence/`.

Non-goals:
- No Linux promise.

Acceptance:
- Two volunteers each reach governed agent in <15 minutes following README.
- Evidence committed under `docs/release/volunteer-evidence/`, including
  command transcripts, timing, environment notes, screenshots or asciinema
  links, and volunteer attestation.

Verifier:
- Verify evidence logs/screenshots and commands.

Adversary: not required unless claims change.

### B14-T06 Demo Recordings and Launch Assets

Goal: produce 3-minute minimum demo and optionally stretch demos.

Scope:
- Hermes writes agent -> pack/sign/SBOM -> governed run -> blocked exfil ->
  signed audit export.
- Embed asciinema/video in README and landing page.
- At least one Block 13 differentiation demo recorded.

Non-goals:
- No marketing site rebuild unless needed.

Acceptance:
- Screenshot/asciinema freshness check passes.

Verifier:
- Run demo scripts and asset freshness checks.

Adversary: not required.

### B14-T07 Offline Bundle

Goal: document and verify macOS offline bundle.

Scope:
- Signed binaries, checksums, SBOMs, container images, policy/demo fixtures,
  verification instructions.
- Prove release artifacts and audit exports verify without network.

Non-goals:
- No polished enterprise offline installer.

Acceptance:
- Offline verification documented and green.
- Evidence committed under `docs/release/offline-verification/`, including
  verification commands, artifact checksums, environment notes, and network
  isolation/air-gap notes.

Verifier:
- Run offline verification in network-disabled or isolated environment.

Adversary: required.

### B14-T08 v0.1.0 Tag Gate

Goal: close release with all P1 gates green on tag.

Scope:
- lint, test, race, fuzz corpus, e2e-network on macOS, Hermes conformance,
  redteam-smoke 6/6, docs smoke.

Non-goals:
- No deferred P2 certification.

Acceptance:
- `make block14-gate` passes and v0.1.0 tag evidence exists.

Verifier:
- Run or inspect CI for tag.

Adversary: required only if release evidence changes security claims.

## Block 15: Sequencing, Calendar, Execution Control

Canonical gate: `make block15-gate`

### B15-T01 Plan Consistency Checker

Goal: implement checks for stale block numbering, missing gate commands, P1/P2
scope drift, and cut security invariants.

Scope:
- Parse execution plan and decomposition docs.
- Confirm gates `block1-gate` through `block15-gate`.
- Confirm never-cut P1 invariants are still named.

Non-goals:
- No natural-language perfect proof.

Acceptance:
- Checker fails on seeded stale block number or missing gate.

Verifier:
- Run `make block15-gate`.

Adversary: not required.

### B15-T02 Status Dashboard Automation

Goal: keep `docs/status.md` or GitHub-backed dashboard aligned with active
issues and PRs.

Scope:
- Show built, remaining, active PRs, blockers, latest gates, next recommended
  issue.
- Link to GitHub rather than duplicate full history once GitHub exists.

Non-goals:
- No custom project-management app.

Acceptance:
- Status refresh is required before merge.

Verifier:
- Run update script and inspect status output.

Adversary: not required.

### B15-T03 Issue Dependency Graph

Goal: encode dependency ordering for orchestrator scheduling.

Scope:
- B1 before all.
- B2 before local control-plane e2e.
- B3 gates security spine.
- B4/B5 before meaningful governed run.
- B6/B7/B8 before redteam runtime fixtures.
- B11 before B12 operator fixture and B13.
- B14 only after B1-B13.

Non-goals:
- No auto-scheduler required.

Acceptance:
- Project view or local issue metadata prevents blocked items from starting.

Verifier:
- Inspect generated issue metadata.

Adversary: not required.

## Suggested Delivery Waves

Wave 1: foundation
- B1-T01 through B1-T06
- B2-T01 through B2-T06
- B3-T01 through B3-T06

Wave 2: governed runtime core
- B4-T01 through B4-T05
- B5-T01 through B5-T05, including B5-T04a through B5-T04d
- B6-T01 through B6-T05
- B7-T01 through B7-T05
- B8-T01 through B8-T06

Wave 3: operation and proof
- B9-T01 through B9-T08
- B10-T01 through B10-T07
- B11-T01 through B11-T07
- B12-T01 through B12-T06

Wave 4: integration and release
- B13-T01 through B13-T07
- B14-T01 through B14-T08
- B15-T01 through B15-T03

## Worker Prompt Skeleton

```text
You are the Worker for <TASK_ID>. Implement only this issue.

Context:
- PRD excerpts: <paste only relevant excerpts>
- Execution-plan excerpts: <paste block excerpt and universal rules>
- Current repo state: <paths/tests>

Behavioral claim:
<one sentence>

In scope:
<bullets>

Out of scope:
<bullets>

Required approach:
- TDD: write the failing test first.
- Keep production changes under the issue scope.
- No architecture changes unless explicitly named.
- No new dependency without license/reason in PR body.

Acceptance criteria:
<bullets>

Gate:
<narrow target first>, then <make blockN-gate when block is ready>

Return:
- Diff summary
- Files changed
- Tests added
- Commands run with exact results
- Known risks
```

## Verifier Prompt Skeleton

```text
You are the Verifier for <TASK_ID>. Do not approve the spec. Verify evidence.

Inputs:
- Issue body and acceptance criteria
- Worker diff
- Worker command output
- Expected gate: <command>

Tasks:
- Run the expected gate or the narrowest relevant target.
- Add or request missing tests if a claim lacks evidence.
- Try the named edge cases from the issue.

Return one:
- PASS with exact command output summary
- FAIL with numbered defects and repro commands
- MISSING TESTS with the specific missing assertions
```

## Orchestrator Review Skeleton

```text
Decision for <TASK_ID>: ACCEPT | REFINE | REJECT

Evidence:
- Worker summary:
- Verifier result:
- Adversary result, if invoked:
- Gate:

Spec judgment:
- Behavioral claim satisfied?
- Scope stayed contained?
- Security invariants preserved?
- Follow-up issues required?

Merge decision:
- Approved to merge only if all required evidence is green.
```
