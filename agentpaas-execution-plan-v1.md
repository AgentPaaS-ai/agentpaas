# AGENTPAAS PHASE 1 — EXECUTION PLAN v1.0
**Purpose:** The build contract. Each BLOCK is sized for one focused LLM
coding session, carries an exact build prompt, a test plan with edge cases,
and a binary success gate. No block starts until the previous gate is green.
**Repo:** `github.com/agentpaas/agentpaas` (monorepo)
**Companion:** `agentpaas-prd-v4-master.md` (the WHY/spec; this is the HOW)

---

## 0. BUILD STRATEGY — HOW TO DRIVE LLMs ON THIS

### 0.1 Single-shot vs. multi-agent: the answer
Neither extreme. Use a **three-role loop per block**:
1. **Builder** (Claude Code / Codex / Hermes — one fresh session per block):
   receives ONLY: this plan's block section + the relevant PRD sections +
   the repo. Builds test-first.
2. **Spec reviewer** (separate fresh LLM session, different model if
   possible): receives the block spec + the diff. Question: "does the code
   do exactly what the spec says — nothing missing, nothing extra?"
   Output: PASS or a numbered defect list.
3. **Adversary** (separate fresh session): receives the SECURITY CLAIMS of
   the block and the code. Task: "write and run tests that break these
   claims." Any successful break = block fails, returns to Builder with the
   adversary's reproduction script.
You (founder) gate each block: review the three outputs, run the success
gate command yourself, merge. Mixture-of-agents is reserved for DESIGN
disputes (builder vs reviewer disagreement on approach), not routine
blocks — too slow/expensive for code.

### 0.1.1 Cost-effective LLM execution loop
Use the strongest available model for planning and architecture, then keep
execution PRs small enough for cheaper models to complete safely.

1. **Planner pass (strong model: ChatGPT 5.5 high + Codex).** For each block,
   produce: PR breakdown, public contracts, security invariants, expected
   tests, files likely touched, and non-goals. Output becomes GitHub issues.
2. **Executor pass (cheap model, e.g. DeepSeek flash-class).** One issue at a
   time. Context = issue body, relevant PRD/execution-plan sections, repo,
   failing test target. No architecture decisions unless the issue explicitly
   grants them.
3. **Verifier pass (cheap/different model).** Receives the issue, diff, and
   test output. Must answer: PASS or numbered defects. It cannot rewrite the
   implementation in the same pass.
4. **Adversary pass (cheap/different model for ordinary PRs; strong model for
   security PRs).** Writes or suggests negative tests against the claims in
   the issue. Any successful break blocks merge.
5. **Escalation rule.** Use the strong model only when: API/security contract
   changes, executor fails the same gate twice, reviewer and executor
   disagree, or the fix would broaden scope beyond the issue.

PR sizing rule: one behavioral claim per PR; target <500 changed production
LOC plus tests. If a PR needs more, split it before coding.

### 0.2 Standing rules for every Builder session (paste verbatim)
```
RULES (apply to every task in this block):
- TDD: failing test first, run it, implement, re-run.
- Go 1.24+, golangci-lint clean, go vet clean. No panics in library code.
- Every public function documented. Errors wrapped with context.
- No new dependency without listing name+license+reason in the PR body.
- All listeners bind 127.0.0.1 unless the spec says otherwise.
- Every security claim gets a NEGATIVE test (prove the bad path is blocked).
- Every user-visible operation must also expose a machine-readable JSON path
  suitable for Codex/Claude Code/Hermes. Human text output is a view, not the
  contract.
- Commit after every green test, conventional-commit messages.
- If the spec is ambiguous, STOP and emit "QUESTION:" — never guess.
- Done = this block's SUCCESS GATE command passes locally.
```

### 0.2.1 PR contract template
Every implementation PR must include:
- Linked issue / block id.
- User-facing behavior changed.
- Security claims changed or preserved.
- Tests added, including at least one negative test for any security claim.
- Commands run and exact result.
- Known limitations / follow-up issues.
- Definition of Done checklist copied from the issue.

No PR merges without: green CI, reviewer PASS, adversary PASS, and an updated
status dashboard.

### 0.2.2 Tracking and dashboard
Use GitHub from day 1, even while private.
- Local git is mandatory: `git init`, `main` protected by convention, feature
  branches per issue, conventional commits.
- GitHub is the recommended source of truth: Issues = work items, Pull
  Requests = execution units, GitHub Project "AgentPaaS P1" = dashboard.
- Required Project views: Board by status, Table by block, Roadmap by target
  week, PR Review queue, Security Gates.
- Required fields: Block, Area, Status, Priority, Model tier
  (`strong-plan|cheap-exec|cheap-review|strong-escalation`), Gate command,
  PR link, Owner, Target date.
- Required labels: `block:N`, `area:api|runtime|policy|identity|secrets|audit|docs`,
  `kind:plan|impl|test|security|docs`, `model:strong|model:cheap`,
  `status:ready|blocked|review|done`.
- `docs/status.md` is generated or refreshed before every merge and shows:
  built, remaining, active PRs, blocked items, latest gate results, and next
  recommended issue.
- Local-only fallback is temporary only: before the private GitHub repo is
  created, keep `docs/status.md`, `docs/prs/PR-000-template.md`, and one
  markdown issue per work item under `docs/issues/`. Move to GitHub before
  implementation PRs begin.

### 0.3 Repo layout (Block 1 creates this)
```
agentpaas/
├── cmd/agent/            # CLI main
├── cmd/agentpaasd/       # daemon main
├── cmd/harness/          # in-container PID 1
├── api/trigger/v1/trigger.proto
├── api/control/v1/control.proto
├── internal/
│   ├── runtime/          # RuntimeDriver iface + docker impl
│   ├── policy/           # parse, validate, compile → agentgateway cfg
│   ├── identity/         # local CA, agent keys, SVID issuance
│   ├── secrets/          # keychain broker, gateway injection, leases
│   ├── audit/            # hash-chain log, export, verify
│   ├── otel/             # collector, sqlite store
│   ├── events/           # bus, webhook delivery
│   ├── operator/         # agentic diagnostics, repair hints, JSON schemas
│   └── pack/             # build pipeline, sbom, sign, secret-scan
├── web/dashboard/        # SPA (preact/lit + TS, embedded via go:embed)
├── sdk/python/           # agentpaas-sdk
├── sdk/node/             # @agentpaas/sdk (deferred; not P1 gate)
├── integrations/
│   ├── mcp-server/       # the universal adapter
│   ├── claude-code-plugin/
│   └── hermes-skill/
├── test/e2e/
├── test/redteam/         # adversarial agent images + harness
├── third_party/agentgateway/  # pinned vendored release + checksum
├── scripts/
│   └── update-status-dashboard.sh
├── .github/
│   ├── workflows/
│   ├── ISSUE_TEMPLATE/
│   └── pull_request_template.md
└── docs/
    ├── status.md
    └── issues/           # local-only fallback until GitHub is live
```

---

## BLOCK 1 — Repo bootstrap, proto contracts, CI skeleton
**Builds:** monorepo layout; both .proto files complete; buf lint+generate;
GitHub Actions (lint, test, -race, osv-scanner); Makefile targets
`build test proto e2e redteam`; SECURITY.md; Apache-2.0 LICENSE; local git
repo initialized; GitHub-ready issue/PR templates and status dashboard.
**Build prompt:** "Bootstrap the AgentPaaS monorepo per §0.3. Author
api/trigger/v1/trigger.proto: services Invoke, InvokeStream, GetRun,
CancelRun, ListRuns; messages carry agent_name, payload (bytes+content_type),
idempotency_key, run_id, RunStatus enum (PENDING/RUNNING/SUCCEEDED/FAILED/
CANCELLED/BUDGET_EXCEEDED), google.rpc.Status errors, Run fields
(run_id, agent_name, agent_version, status, created_at, started_at,
finished_at, error, budget_summary, policy_digest, image_digest), pagination
(page_size, page_token, next_page_token), and explicit idempotency semantics
(same key+same payload returns original run; same key+different payload
returns ALREADY_EXISTS/409). Add HTTP annotations for grpc-gateway routes and
document InvokeStream as REST SSE. Author control/v1/control.proto: Pack,
Run, Stop, Logs(stream), PolicyApply, SecretSet/Grant/Revoke, AuditQuery,
AuditExport, Doctor, ValidateAgentProject, SummarizeRun, ExplainFailure,
ExplainPolicyDenial, RecommendPolicyPatch, GetRunTimeline, NextAction. These
operator methods return stable JSON/protobuf payloads for coding-agent
clients; CLI/dashboard text is rendered from the same data. Use stable proto
package names
`agentpaas.trigger.v1` and `agentpaas.control.v1`, explicit `go_package`,
reserved field numbers/names when deleting, and committed generated code.
Wire buf + grpc-gateway codegen. CI as specified. Initialize local git,
create .gitignore/.gitattributes, .github issue templates, PR template,
CODEOWNERS placeholder, and docs/status.md plus
scripts/update-status-dashboard.sh. Apply standing RULES."
**Edge cases to test:** codegen reproducible (two runs byte-identical);
buf breaking-change check fires on a field renumber; generated code is
up-to-date in CI; HTTP annotation route table golden test; InvokeStream SSE
mapping documented; ListRuns pagination handles empty, exact-page, and
next-page cases; idempotency replay and payload-mismatch behavior covered in
proto/API conformance tests; PR template contains Definition of Done;
status dashboard renders built/remaining/PR sections even before GitHub is
connected; CI fails a deliberately bad branch for the right reason.
**SUCCESS GATE:** `make proto build test` green on macOS + ubuntu CI;
`scripts/update-status-dashboard.sh` updates `docs/status.md`; initial
GitHub issues/Project can be created from the block list OR local fallback
issues exist under `docs/issues/`.

---

## BLOCK 2 — Daemon skeleton + CLI plumbing (unix-socket gRPC)
**Builds:** agentpaasd lifecycle (start/stop/status; launchd plist +
systemd user unit generators), explicit local path layout under
`~/.agentpaas` (0700; `daemon.sock` 0600, `agentpaasd.pid`, `logs/`,
`state/`, `config/`, `cache/`, `tmp/`), unix socket gRPC server with
readiness handshake, control-API server with stub handlers; `agent` CLI
(cobra) wired to all control RPCs; daemon commands (`agent daemon install`,
`uninstall`, `start`, `stop`, `restart`, `status`); `agent version` and
`agent daemon status` show CLI version, daemon version, proto version, git
commit, OS/arch, Docker context, Docker API version; `agent doctor` v0
(Docker reachable? current Docker context? Docker Desktop/Colima/Linux
dockerd detection where possible? socket perms? ports 7700/7717/7718 free?
home dir perms? daemon ready? CLI/daemon proto compatible?); structured
logging (slog, JSON) with redaction enabled from day one. Dev/test
overrides: `AGENTPAAS_HOME`, `AGENTPAAS_SOCKET`,
`AGENTPAAS_DASHBOARD_PORT`, `AGENTPAAS_TRIGGER_REST_PORT`,
`AGENTPAAS_TRIGGER_GRPC_PORT`.
**Edge cases:** daemon not running → CLI clear error + start hint; daemon
process started but not ready → CLI waits with timeout then actionable
error; stale socket/pid/lock files → auto-recover only after proving no
live daemon owns them; two daemons race → flock prevents; user broadens
home/socket perms → daemon refuses to serve and says why; daemon run as
root → refuses unless `--allow-root-for-test`; SIGTERM → graceful drain of
in-flight RPCs; service file generation is deterministic and unit-tested
without requiring launchd/systemd inside CI; lifecycle e2e runs where the
host supports user services; Docker stopped/context missing/API too old →
doctor names the exact issue; port squatted → doctor names process/port
when the OS permits; log redaction masks high-entropy/API-key-looking
values in CLI and daemon logs.
**SUCCESS GATE:** `agent doctor` exits 0 on a healthy machine, nonzero with
actionable messages for each induced failure (docker stopped, port
squatted, bad socket perms, bad home perms, daemon not ready, CLI/daemon
version mismatch); `agent version` and `agent daemon status` print the
expected version/context fields; service-unit golden tests pass on macOS
and Linux; redaction test proves planted secret-looking values do not appear
in logs — scripted in test/e2e/doctor_test.sh and unit tests.

---

## BLOCK 3 — Identity service + audit hash-chain (security spine first)
**Builds:** internal/identity — narrow interfaces for `KeyStore` and
`IdentityIssuer`, with P1 implementations backed by macOS Keychain
(`security(1)` wrapper), Linux libsecret, and an explicit encrypted
file-keystore fallback (0600 + passphrase; warned by doctor; no silent
plaintext fallback). Manage distinct local identities: local CA key,
daemon audit signing key, per-agent package identity keys, and per-run
workload key/cert. `agent pack` can mint/register an AID; `agent run` can
issue a 1h, auto-renewed SPIFFE-style workload cert
(`spiffe://local.agentpaas/agent/<name>/<ver>/run/<run_id>` in P1) for
gateway/harness-to-daemon mTLS. Trust-domain construction and verification
must be configurable so Phase 2 can issue hosted identities such as
`spiffe://tenant.agentpaas.ai/<tenant>/agent/<name>/<ver>/run/<run_id>`
without changing record schemas. Workload certs identify event sources but
never sign the canonical audit trail.

internal/audit — narrow interfaces for `AuditWriter`, `AuditAnchor`,
`AuditVerifier`, and `AuditExporter`, with P1 implementations backed by
append-only canonical JSONL where each record has `seq`, `prev_hash`, and
`record_hash` (SHA-256 over canonical JSON with `record_hash` omitted);
SQLite index derived from JSONL and rebuildable; single daemon-owned writer
that serializes appends and durably maintains a latest-head anchor. Signed
checkpoint records are inserted into the same chain at fixed cadence and at
export, signed by the daemon audit signing key over `{head_seq, head_hash,
previous_checkpoint_hash, created_at}`. Security-relevant actions fail
closed if their audit record cannot be appended. Add `agent audit verify`
with local and bundle modes; add `agent audit export` -> signed bundle
containing JSONL segments, checkpoints, AIDs/public keys, trust metadata,
and an export manifest signed by the daemon audit signing key. Record schema
must include `deployment_mode` (`local|hosted`) and optional hosted-context
fields (`tenant_id`, `project_id`, `region`, `runtime_provider`) so P2 can
reuse the same verification algorithm in AgentPaaS.ai.

**Edge cases (every one is a test):** tamper a middle line -> verify fails
naming the exact line/seq; truncate tail relative to the latest local head
anchor -> fail; reorder two lines -> fail; delete a checkpoint -> fail;
delete or corrupt the SQLite index -> verify reports/rebuilds from JSONL
without changing hashes; wall-clock moves backwards -> chain still valid
(monotonic seq is authoritative); expired workload cert rejected; workload
cert renewal happens before expiry; package identity key never appears in a
run container; 100k records verify < 5s; concurrent writers serialize
without loss (`-race`); audit append fsync/write failure makes the guarded
operation fail closed; keychain locked/unavailable -> clear error, no
silent plaintext fallback; explicit file-keystore fallback refuses weak
permissions and wrong passphrase; alternate trust domain URI builder/verifier
passes for a fake hosted tenant; audit bundle verification works from an
extracted bundle without reading `~/.agentpaas`; in-memory fake keystore and
fake audit anchor pass the same contract tests as local implementations.

**SUCCESS GATE:** `go test ./internal/identity/... ./internal/audit/...
-race` green; tamper-detection e2e script demonstrates all 4 tamper modes
caught; audit-head-anchor test proves tail truncation is caught locally;
export verifies on a second machine/clean CI workspace using only the
bundle and the expected daemon audit public-key fingerprint; docs for the
gate state that second-machine verification proves bundle integrity, not
global transparency-log anchoring.

---

## BLOCK 4 — Policy engine (parse → validate → compile to agentgateway)
**Builds:** internal/policy — one canonical human/LLM-friendly `policy.yaml`
for egress, credentials, MCP servers, hooks, and ingress; strict YAML schema
(unknown fields = error); validation (exact hostname matching by default,
no implicit subdomain matching, no wildcard domains unless
`allow_wildcard: true`, no private CIDRs unless `allow_private: true`); MCP server
declarations in `policy.yaml` with explicit server ids, transport
(`stdio|http`), command/endpoint, allowed tools, auth mode, minimal env, and
egress binding for remote MCP servers; hook destination declarations checked
as policy data in Block 4 and rechecked at delivery time in Block 9;
brokered credential bindings (`egress.allow[].credential` and MCP auth
references must point to `credentials.brokered[].id`) with header-only
injection templates; explicit direct-lease schema
(`credentials.direct_leases[]` requires mode+reason); canonicalizer that
sorts maps and unordered lists deterministically, uppercases HTTP methods,
lowercases and ASCII/punycode-normalizes domains, expands defaults, removes
comments, deduplicates equivalent rules with warnings, and emits a stable
policy digest (sha256 of canonical form) recorded for audit + agent.lock.
Compiler emits pinned agentgateway config + the DNS-stub allow-list +
credential-injection rules by id only. Vendor agentgateway release into
third_party/ with checksum verification in the build.
**Edge cases:** empty policy → deny-all config (valid, runs, nothing
egresses); `domain: example.com` does not allow `api.example.com`; wildcard
without `allow_wildcard: true` -> validation error; duplicate domains →
dedup warn; punycode/IDN domains → canonical ASCII form; confusable IDN
defense is deferred but non-normalizable names fail closed; port ranges
rejected (explicit ports only in P1); CIDR overlap with RFC1918 → require
`allow_private: true`; policy file world-writable → refuse to load; egress
rule references undeclared brokered credential id → validation error;
declared brokered credential not referenced by an egress/MCP rule →
validation warning; query-string or body credential injection → validation
error; direct lease without reason → validation error; compiled config and
canonical policy digest input never contain raw secret values; secret ids may
appear, secret values never may; undeclared MCP server id -> validation
error; MCP server with unspecified allowed tools -> deny all; remote MCP
server without matching egress allow rule -> validation error; local MCP
server receiving undeclared env/secret -> validation error; remote hook URL
without matching egress allow rule -> validation error; loopback hook URL
must be explicitly local and cannot be exposed to the agent container;
credentialed brokered request redirects are disabled by default;
noncredentialed redirects are re-evaluated against policy per hop; YAML key
order and comments do not affect digest; typos such as `credentials.brokerd`,
`allow_wildcards`, and scalar `port: 443` fail schema validation; round-trip:
compile(parse(x)) deterministic.
**Fuzzing:** go-fuzz on parser (mandatory; crash corpus committed).
**SUCCESS GATE:** unit + fuzz (1M execs, 0 crashes) green; golden-file
tests for compiler output; digest stability tests prove comments/key order do
not change the canonical digest while semantically meaningful changes do; a
sample policy.yaml from PRD §2.9 compiles to a config agentgateway actually
loads (smoke test runs the real binary).

---

## BLOCK 5 — RuntimeDriver + the fenced network topology
**Builds:** internal/runtime — RuntimeDriver interface (Create, Start,
Stop, Remove, Status, Stats, Logs) + Docker implementation. Network setup:
one logical agent deployment made of two containers: the agent/harness
container and the ingress/egress gateway sidecar. Both directions are
gateway-only: daemon/caller ingress goes through gateway before reaching the
harness, and agent outbound goes through gateway before reaching any
upstream. There are no direct daemon-to-harness calls, no agent-to-host
shortcuts, and no host networking in P1. Per-agent `internal: true` bridge;
dedicated AgentPaaS egress network; gateway sidecar dual-homed (internal
bridge + egress network); agent container never shares the gateway network
namespace and is never attached to the egress network; deterministic
AgentPaaS labels/names on all owned containers and networks for safe
reconciliation; cleanup on partial create failure. Agent container hardening
flags (non-root uid 64000, read-only rootfs, tmpfs /tmp, cap-drop ALL,
no-new-privileges, seccomp default profile, pids-limit 256, memory/cpu from
agent.yaml); DNS of agent container pointed at gateway stub IP only; IPv6
disabled for P1 agent networks. Rootless Docker is best-effort and explicitly
not a P1 gate; supported gates are Docker Desktop, Colima's Docker-compatible
socket, and Linux `dockerd`.
**Edge cases / negative tests (heart of the product — exhaustive):**
- positive path: agent invoke reaches harness only through gateway ingress;
  agent outbound to an allowed test endpoint succeeds only through gateway
  egress and emits the expected policy decision and audit event
- canary on internal net: `curl https://1.1.1.1` → no route, fails fast
  within a concrete timeout budget (target ≤2s; never hangs)
- direct DNS to 8.8.8.8 → unreachable
- `host.docker.internal`, Docker bridge gateway IP, gateway container IP,
  daemon ports, and any host-local service probes → unreachable from agent
- IPv6: no route (AAAA answers and direct v6 literals both dead)
- UDP egress (non-DNS), ICMP, raw-socket attempts, and CONNECT tunnel bypass
  attempts → blocked
- Docker inspect assertions prove agent has no default route, no egress
  network attachment, no host networking, no shared network namespace with
  gateway; gateway has exactly internal+egress networks
- resource assertions prove non-root uid, read-only rootfs, tmpfs /tmp,
  cap-drop ALL, no-new-privileges, seccomp default profile, pids-limit,
  memory, and CPU limits are actually applied
- container restart preserves network membership
- daemon crash leaves no half-fenced agent: startup reconciliation kills
  any agent container whose gateway is absent
- partial create/start failure leaves no orphaned AgentPaaS containers or
  networks after cleanup
- Docker inspect, runtime logs, and network config dumps contain no raw secret
  values and are suitable for debugging
- Docker Desktop vs colima vs Linux dockerd: topology holds on all three
**SUCCESS GATE:** `make e2e-network` runs the positive-path canary plus the
bypass suite and prints a table of allowed path PASS plus at least 12 attack
vectors, all BLOCKED, on macOS (Docker Desktop + colima) and Linux CI. The
gate docs explicitly state that Block 5 proves gateway-only network topology
and container hardening, not secrets, budgets, SDK behavior, or the full
Block 11 red-team suite.

---

## BLOCK 6 — Harness (cmd/harness) + Python SDK contracts
**Builds:** Go harness as container PID 1: exec Python user code; HTTP
contract (`POST /invoke`, `GET /healthz|readyz` on localhost:8000 inside
container); load agent code once per container and serialize invokes by
default (`concurrency: 1`) with explicit opt-up later. Budget enforcement:
`startup_timeout` covers import/readiness; `max_wall_clock` measures only
run receive-invoke → run finishes using a monotonic clock; `max_iterations`
means agent turns (each SDK-observed LLM/tool cycle counts, and each direct
`agent.llm()` call counts if no higher-level loop exists; P2 may use model
context-window health, performance drops, and turn-count guidance to adjust
repair/retry strategy); token/USD accounting uses gateway-reported best-known
usage and records post-hoc overage when provider usage arrives after
termination. Breach → SIGTERM,
10s grace, SIGKILL, status=BUDGET_EXCEEDED, audit event. OTel emit.
Python SDK (`agentpaas-sdk`): decorators `@agent.on_invoke`, `agent.llm()`
(OpenAI-compatible client preconfigured to gateway), noncredentialed
`agent.http(...)`, brokered `agent.http_with_credential(credential_id, ...)`,
and `agent.mcp(server_id, tool, input)`. Agent-level checkpoint/resume and
half-done job recovery are deferred to P2; P1 restarts failed runs from a
fresh container and records enough structured failure context for future
resume/repair loops. Audit-log checkpoints from Block 3 remain part of the
security model. Gateway policy can optionally deny noncredentialed HTTP calls
(`egress.require_credential_binding: true`), forcing all outbound HTTP
through named credential bindings. Brokered credentials are never returned to
SDK callers.
`agent.secrets.file()` exists only for explicit direct-lease compatibility
mode and is discouraged in generated code. Node SDK is deferred until after
the Python SDK and language-neutral harness contract are proven.
**Edge cases:** user code crashes on import → FAILED with stderr captured;
run fails for prompt/task/tool/SaaS/MCP/code reasons → structured failure
reason, stderr/stdout pointers, policy decision ids, and relevant upstream
availability evidence are reported to the control plane; failed runs are
safe to retry in a fresh container with prior failure context stored outside
the container; user code ignores SIGTERM → killed at grace deadline; zombie
processes reaped (PID 1 duty); invoke payload 50MB → rejected 413 (limit
10MB, configurable); unicode/binary payloads round-trip; budget race (token
usage reported after kill) → accounted post-hoc, audit shows overage;
blocked egress/tool calls are visible to the developer in CLI/dashboard logs
with reason, run id, policy digest, and strict secret/payload redaction; MCP
call to undeclared server/tool is denied before execution and audited; MCP
tool input/output bodies are not logged, only hashes and metadata. P2 note:
the control plane may use the structured failure context to decide whether to
modify an agent and retry in a fresh container until success; P1 only needs
the structured failure context needed to support that future loop.
**SUCCESS GATE:** e2e: an infinite-loop agent with max_wall_clock=30s dies
at 30s±2s from invoke start with BUDGET_EXCEEDED + audit event; a token-burn
agent stops future calls at the token cap using best-known usage and audits
any provider-reported overage; Python SDK passes the harness contract test
suite.

---

## BLOCK 7 — Secrets broker
**Builds:** internal/secrets — `SecretStore` abstraction with P1
implementations for macOS Keychain, Linux libsecret, and an explicit fake
test store only; no silent plaintext fallback. `agent secret set/list/rm`
(values read from stdin/interactive prompt, NEVER argv so they never hit
shell history or process lists; max secret value size 64 KiB); `list` shows
metadata only (id, created_at, updated_at, last_used_at, referenced-by
policies/agents), never value, prefix, suffix, or hash-derived hints.
Secret store names are case-sensitive local-profile entries with no
whitespace/control characters; policy credential ids are policy-local stable
ids that bind egress/MCP rules to those store names.
Brokered outbound credential flow (gateway sidecar requests credential use
from daemon/secrets broker over local authenticated channel; daemon validates
run id + policy rule id + destination + method; gateway injects header field
per policy and originates the upstream TLS request; raw value is never sent
to the agent container). Direct lease flow for compatibility is file-only:
`file_lease` mounts a runtime tmpfs file 0400 owned by agent uid; P1 does
not support `env_lease`, and real secret files are never packaged into agent
images. Revocation invalidates brokered use immediately and restarts affected
direct-lease agents; direct-lease revocation cannot claw back a secret value
already visible to agent code. Audit guarantee: brokered injection emits
`secret_injected` with `visible_to_agent=false`; direct lease emits
`secret_leased` with `visible_to_agent=true`; SDK lease-helper reads emit
`secret_read`; P1 does not claim reliable per-open auditing for raw file
reads of a direct lease. Add a follow-up enterprise design issue for
corporate employee machines behind VPN: evaluate managed-vault/remote broker
patterns where enterprise secrets do not permanently reside on the employee
laptop, plus device posture, tenant policy to disable direct leases,
short-lived credential grants, revocation, and tenant-visible audit.
**Edge cases:** brokered secret referenced but not set → launch refuses
naming the missing secret; brokered credential used for wrong domain,
method, port, or credentialed redirect → denied before injection and audited;
noncredentialed redirects are rechecked against policy per hop; gateway crash
cannot dump secret in logs; keychain/libsecret locked or unavailable →
actionable error, no plaintext fallback; secret containing newlines/UTF-8/64
KiB length injects/round-trips exactly; oversize secret rejected before
storage; agent attempts `env`, `/proc`, filesystem walk, and `docker inspect`
to find a brokered sentinel secret → zero hits; compiled gateway config and
policy digest contain credential ids only, never values; generated files,
image layers, build context, and packed artifacts contain no real secret
values; direct lease tmpfs file gone after `agent stop` (asserted); raw file
read succeeds for an explicit direct lease but is not claimed as a precise
per-read audit event; CLI/dashboard/runtime errors redact secret values and
do not show value prefixes/suffixes.
**SUCCESS GATE:** negative suite green, including grep of full
process list, shell history fixture, `docker inspect`, gateway logs, compiled
configs, exported image layers, build context, packed artifacts, CLI/dashboard
errors, and agent filesystem/proc probes for a brokered sentinel secret →
zero hits; a real brokered OpenAI-style request receives the Authorization
header upstream while agent logs/proc/env never contain the key.

---

## BLOCK 8 — Packaging pipeline (`agent pack`)
**Builds:** internal/pack — framework detection for Python first (plain
Python, LangGraph, CrewAI markers; Node and custom Dockerfiles deferred),
buildkit image assembly
(distroless base by digest, locked deps via uv, harness as PID 1, non-root,
no shell),
gitleaks secret scan (fail-closed), syft SBOM (SPDX-json, attached as OCI
artifact in a local OCI layout plus Docker image by digest), local
key-backed cosign signing with the per-agent package identity key, and a
signed canonical `agent.lock` manifest. P1 does NOT use Sigstore keyless
OIDC/Fulcio signing for local packs; future release/enterprise flows may add
that separately. `agent.lock` includes schema version, agent name/version,
runtime/framework, platform, base image digest, harness version, build input
digest, image digest, SBOM digest, policy digest, package AID/public key,
signature bundle/referrer locations, and reproducibility metadata. The
lockfile itself is signed by the package identity key and is the exact review
unit consumed by `agent run` and future promotion.
**Edge cases:** no agent.yaml → offer `agent init` scaffold; dependency
conflict → surfaced verbatim, abort; 2GB build context → .agentpaasignore
honored, warn >100MB, with default excludes for `.git`, virtualenvs, caches,
`node_modules`, test outputs, and large local data; secret scan covers the
full source tree plus the effective build context, and a secret in either
path FAILs naming file:line; `--allow-secret-pattern` requires a successful
daemon audit append or aborts; rebuild without changes → identical image
digest (fixed timestamps, pinned base digest, locked deps, deterministic
tar order, `SOURCE_DATE_EPOCH`); local OCI layout missing/corrupt →
actionable repair; registry push is deferred; LangGraph and CrewAI example
repos pack without a custom Dockerfile.
**SUCCESS GATE:** 3 Python reference agents (plain-py, langgraph, crewai)
pack green; `agent verify agent.lock` and explicit offline
`cosign verify --key <AID pubkey>` pass for the image signature; lockfile
signature verifies; SBOM lists expected top-level deps; osv-scanner advisory
summary appears in `agent pack` output without failing on non-critical
findings; secret-scan e2e blocks a planted key in source, ignored source, and
build context; golden fixtures assert expected `agent.lock` fields. Node
packaging is a follow-on gate.

---

## BLOCK 9 — Trigger API + events/webhooks + cron
**Builds:** Trigger API serving (gRPC :7718 + grpc-gateway REST :7717,
loopback by default; Trigger API requires AgentPaaS API-key or mTLS auth even
on loopback; `--expose` refuses without an API key). API keys are AgentPaaS
Trigger API credentials for Codex/Hermes/Claude Code/local apps/CI callers to
invoke a packed agent under test or running locally; keys are shown once,
stored hashed, scoped by agent/action, revocable/rotatable, and audited.
REST CORS is deny-by-default; browser-originated local requests are not
trusted without explicit auth, and preflight/origin handling is covered by
tests.
Define stable caller IDs (`api_key:<id>`, `spiffe:<subject>`,
`system:cron:<agent>`, `local_user:<uid>`), token-bucket rate limits per
caller, durable idempotency table (key→run_id, 24h replay window,
canonical request hash over caller, agent, agent.lock digest, payload bytes,
content type, and API version), max invoke payload 1 MiB default with 413 and
"pass a reference/blob handle" guidance, InvokeStream (gRPC stream + REST
SSE for CLI/dashboard/coding-tool live progress), internal/events bus,
webhook delivery (HMAC-signed with timestamp/replay window, 3 retries exp
backoff, dead-letter to audit), and cron triggers from agent.yaml feeding the
same Invoke path. P1 supports URL webhooks only; local command hooks are
deferred. Audit events include `api_key_created`, `api_key_revoked`,
`auth_failed`, `invoke_accepted`, `invoke_rejected`, `idempotency_replayed`,
`idempotency_conflict`, `rate_limited`, `webhook_delivered`,
`webhook_dead_lettered`, `cron_missed`, `cron_skipped_concurrency`,
`cancel_requested`, `cancel_graceful`, and `cancel_forced`.
**Edge cases:** replayed idempotency key → same run_id, no second
execution; replay with DIFFERENT payload + same key → 409; burst 1000
invokes → rate-limited with Retry-After; malformed JSON → 400 with line;
browser POST from a random localhost origin without API key → 401/CORS deny;
oversized payload (>1 MiB default) → 413; daemon restart during idempotency
window preserves replay/409 behavior; SSE client reconnects with
Last-Event-ID, heartbeat, ordered event IDs, and no duplicate terminal event;
webhook target down → retries then dead-letter audit entry; webhook replay or
bad HMAC → rejected by receiver fixture; webhook target = non-allow-listed
domain → blocked by policy (hooks are egress too); cron uses 5-field syntax
only, local timezone by default, explicit timezone optional; cron during
daemon downtime → missed-run policy explicit (`skip` default, `catchup: 1`
opt-in); DST nonexistent local time skipped, repeated local time runs once;
cron fires while prior run active → `concurrency_policy: forbid` default
skips and audits; CancelRun mid-LLM/MCP-call → audit cancel_requested, ask
gracefully, wait 30s, force stop if needed, audit final canceled/forced
outcome.
**SUCCESS GATE:** API conformance suite (generated from proto) green;
auth/API-key lifecycle + idempotency + rate-limit + SSE reconnect e2e green;
cron/webhook tests prove same policy/audit path as manual Invoke; cancel
semantics e2e green; fuzz on REST JSON ingestion (100k execs, 0 crashes).

---

## BLOCK 10 — OTel pipeline + Dashboard
**Builds:** in-process OTLP collector → SQLite (WAL) with retention prune
(default 7d local, configurable) for OTel traces/logs/metrics only; canonical
audit JSONL is not pruned by dashboard retention and is purged only by an
explicit future user retention/purge command. Agent/harness/gateway logs are
ingested as OTel log records for dashboard correlation; daemon operational
logs remain bounded structured JSON files under `~/.agentpaas/logs/` with
rotation/redaction and are linked from `agent doctor`/`agent logs` but are not
the canonical audit source. Dashboard SPA (preact+TS, go:embed, no runtime
CDN): agent list w/ status+spend-vs-budget, run timeline with a stable event
schema (LLM calls w/ tokens+cost, MCP calls, egress ALLOWED/DENIED rows in
red, budget/audit markers), log viewer with truncation/redaction, policy view
showing both git-file diff and normalized effective policy digest, audit
search explicitly labeled as an indexed view + one-click signed export with
trust-anchor fingerprint, included sequence range, verification command, and
result status, live SSE event stream reusing Block 9 event IDs/heartbeat/
Last-Event-ID semantics. Cost estimates record provider, model, price-table
version, token counts, and `estimated=true`; P1 ships a built-in price table,
P2 allows user/tenant-modified price tables. Strict CSP, no inline JS, CSRF
token on mutating routes; loopback read-only dashboard may be unauthenticated,
exposed dashboard requires API key/session, and API keys are never stored in
browser localStorage.
**Edge cases:** 10k-span run renders (virtualized list); SQLite locked by
concurrent writer → dashboard reads stay snappy (WAL + read pool); SQLite
migration/WAL checkpoint/vacuum/prune/corruption recovery covered; dashboard
with daemon restarting → reconnects SSE gracefully using Last-Event-ID; XSS
attempt via agent-controlled log line / trace attribute → escaped (test with
planted `<script>` in agent output); sentinel secret in logs/spans/errors is
redacted everywhere; binary/control characters and huge log/attribute values
are safely escaped/truncated with pointers to full retained logs where
allowed; clock-skewed spans → ordered by monotonic seq; security events are
never sampled out of canonical audit even if OTel retention prunes dashboard
telemetry; empty states designed (zero agents, zero runs); accessibility and
keyboard smoke test.
**SUCCESS GATE:** Playwright e2e: launch agent → watch live run → see a
DENIED egress row → export audit → verify export. Lighthouse perf ≥ 90
local. Planted-XSS and sentinel-secret tests show escaped/redacted output.
10k-span, SSE reconnect, SQLite lock/corruption recovery, empty-state, policy
diff, audit export verify, and accessibility smoke tests green.

---

## BLOCK 10.5 — Agentic operator contract (Codex/Claude/Hermes-first P1)
**Purpose:** Make Codex, Claude Code, Hermes, Cursor, and similar agentic
development tools first-class operators of AgentPaaS, not screen-scrapers of
human CLI/dashboard output. P1 is a hands-off but secure local development
experience: a coding agent can scaffold, pack, run, inspect, diagnose, repair,
and re-run an agent on the user's machine, while sensitive boundary changes
remain explicit, reviewed, and audited.
**Builds:** internal/operator — a stable machine-readable diagnosis and
repair-hint layer consumed by CLI, dashboard, and Block 12 MCP integrations.
Add JSON-schema/protobuf contracts for: `ValidateAgentProject`,
`SummarizeRun`, `ExplainFailure`, `ExplainPolicyDenial`,
`RecommendPolicyPatch`, `GetRunTimeline`, and `NextAction`. All commands
that a human can use for pack/run/logs/status/policy/audit also support
`--json` with the same schema. Outputs include stable error categories
(`dependency_conflict`, `docker_unavailable`, `policy_denied`,
`missing_secret_binding`, `budget_exceeded`, `trigger_auth_failed`,
`harness_health_failed`, `agent_runtime_exception`,
`policy_validation_failed`, `network_sandbox_failed`, `secret_scan_failed`,
`package_verification_failed`, `dashboard_unavailable`) plus evidence refs
(run_id, audit seq range, policy rule id, span/log ids, redacted excerpts,
verification command).

The operator contract is the retroactive invariant for Blocks 1-10:
- Block 1 APIs/protos define stable machine-readable methods and error enums.
- Block 2 daemon lifecycle/doctor reports structured readiness and repair
  hints.
- Block 3 audit exposes query/export results as signed, verifiable machine
  data with trust-anchor fingerprints.
- Block 4 policy compiler emits structured denial reasons and safe patch
  proposals, never silent policy broadening.
- Block 5 network/runtime returns structured egress decisions and containment
  evidence.
- Block 6 harness/SDK emits run lifecycle, health, budget, and exception
  events in schemas that tools can reason over.
- Block 7 secrets broker exposes missing-binding/revocation/lease diagnostics
  without revealing secret values.
- Block 8 packaging returns signed `agent.lock`, SBOM, scan, advisory, and
  reproducibility results as JSON.
- Block 9 Trigger API uses stable caller ids, idempotency, SSE event ids, and
  cancel outcomes that tools can resume from.
- Block 10 dashboard/OTel exposes the same timeline/audit/policy data as JSON;
  the UI is a view, not the source of truth.

**Safety model:** Agentic tools may automatically repair code, tests,
`agent.yaml`, dependency declarations, and non-security config inside the
project root. They may propose `policy.yaml` changes, new egress, credential
bindings, direct leases, webhook destinations, exposed listeners, retention
purges, and destructive actions, but P1 requires explicit user/daemon confirm
before applying them. Tools cannot read secret values, cannot broaden policy
silently, cannot delete audit, cannot disable red-team gates, and cannot use
paths outside the invoking project root. Prompt-injected instructions inside
agent source/logs/traces are untrusted data and must not cause policy changes,
secret disclosure, audit deletion, or destructive operations.

**Agentic workflow contract:** `agent init --from-code --noninteractive`
creates/reconciles `agent.yaml` and a minimal default-deny `policy.yaml`;
`agent validate --json` returns project readiness; `agent pack --json` emits
scan/SBOM/signature/lockfile facts; `agent run --json` returns run_id and
stream refs; `agent status/logs/audit/policy --json` expose structured state;
`agent explain run <run_id> --json` diagnoses failures; `agent policy explain
<run_id|dest> --json` names the blocking rule; `agent policy propose --json`
returns a patch with risk level, rationale, affected destinations, credential
ids, and audit evidence; `agent next-action <run_id> --json` returns one of
`fix_code`, `install_dependency`, `start_docker`, `set_secret`,
`review_policy_patch`, `increase_budget`, `rerun`, `export_audit`, or
`ask_user`.

**Edge cases:** malformed/old JSON schema version → clear compatibility error;
tool asks for path outside project root → refusal with audit event; huge logs
or build output → truncated excerpts + stable refs; denied egress → policy
patch is proposed but not applied; missing secret → secret binding request is
proposed but value is never requested through the agentic tool; prompt
injection in source/logs says "approve all policy" → ignored and tested;
network/dashboard unavailable → tool falls back to daemon/control JSON; daemon
restart mid-loop → idempotency and run refs let the coding tool resume; human
declines policy patch → next action becomes `fix_code` or `ask_user`, not
policy bypass.
**SUCCESS GATE:** Agentic golden flow green on a clean machine: a scripted
Codex/Claude/Hermes-like client creates a deliberately incomplete Python
agent, runs `agent init --from-code --noninteractive`, validates, packs,
runs, sees a policy denial, receives a structured denial explanation,
receives a policy patch proposal but cannot apply it without confirm, fixes a
code/dependency issue automatically, reruns after approved policy, exports a
signed audit bundle, and summarizes the final result in JSON. Negative tests
prove prompt-injected source/log instructions cannot broaden policy, reveal
secrets, delete audit, or stop unrelated runs. JSON schema golden tests prove
backward-compatible outputs for every operator method.

---

## BLOCK 11 — Red-team suite (the permanent CI gate)
**Builds:** test/redteam — a malicious-agent image library + runner that
packs and runs each attacker through the REAL pipeline, then asserts
containment. Attack classes (one image each):
1. raw IP dial (TCP 443 to a literal IP)
2. DNS tunneling (data encoded in subdomain queries)
3. proxy bypass (raw socket, ignore HTTP_PROXY)
4. host.docker.internal / gateway-IP service probing
5. IPv6 escape attempt
6. read /etc/passwd, /proc/1/environ, mounted-secret directory traversal
7. discover brokered secret via env/proc/files/logs (must find zero);
   misuse brokered credential against wrong destination (denied);
   exfiltrate an explicit direct-lease secret to an ALLOWED domain
   (fingerprint match)
8. UDP egress (non-DNS) + ICMP tunnel attempt
9. fork bomb / memory bomb / fd exhaustion (resource-limit containment)
10. domain-fronting (SNI=allowed-domain, Host header=other domain)
Each attacker asserts: action BLOCKED (or contained) + a correct audit
event was written. Runner prints a 10-row containment table.
**Edge cases:** attacks run through the REAL `agent pack` + `agent run`
pipeline (no test shortcuts); each attacker also verifies its own audit
event exists with correct dest/verdict fields; suite is rerun on every
agentgateway version bump and every RuntimeDriver change; a NEW attack
class added to this library never removes an old one.
**SUCCESS GATE:** `make redteam` prints 10/10 BLOCKED on macOS (Docker
Desktop + colima) and Linux CI. Permanent gate: every future release must
show 0 escapes; a single escape is a release blocker, no exceptions.

---

## BLOCK 12 — Integrations: MCP server + Claude Code plugin + Hermes skill
**Builds:** integrations/mcp-server — an MCP server exposing the daemon's
control API as tools (`agentpaas_pack`, `agentpaas_run`, `agentpaas_stop`,
`agentpaas_logs`, `agentpaas_status`, `agentpaas_policy_show`,
`agentpaas_audit_query`, `agentpaas_validate_project`,
`agentpaas_summarize_run`, `agentpaas_explain_failure`,
`agentpaas_explain_policy_denial`, `agentpaas_recommend_policy_patch`,
`agentpaas_next_action`). These are thin wrappers over the Block 10.5
operator contract, not a separate behavior surface. This single adapter is
the universal wedge: any MCP-speaking coding agent (Claude Code, Codex,
Cursor, Hermes via native-mcp) gets "deploy what you just built, safely" as a
verb plus the diagnosis loop needed to fix what it just built.
Then two thin first-party packagings of it:
- **claude-code-plugin:** plugin manifest + slash command `/deploy-agent`
  + a PostToolUse hint surfacing "run this under AgentPaaS?" after agent
  code is written. Distributed via plugin marketplace repo.
- **hermes-skill:** SKILL.md teaching the flow (detect agent code → `agent
  init` scaffold → pack → run → open dashboard → show first audit event),
  with pitfalls (Docker not running → `agent doctor`; policy denial →
  `agent policy explain <dest>`).
Security stance: the MCP server talks ONLY to the loopback daemon socket;
it never accepts remote connections; destructive tools (stop, secret ops)
require the daemon's confirm flag so a prompt-injected coding agent cannot
silently kill or exfiltrate.
**Edge cases:** MCP client passes a path outside the project root → tool
refuses (path allow-list = invoking project dir); daemon down → tool
returns actionable error, not a hang; concurrent pack requests for the
same agent → second queues with a message; tool output > 50KB (huge build
logs) → truncated with a pointer to `agent logs`; prompt-injection test:
a hostile instruction embedded in agent source comments must not cause
the MCP tools to alter policy or reveal secrets (negative test in CI).
**SUCCESS GATE:** scripted e2e on a clean machine: Claude Code session
generates a weather agent → `/deploy-agent` → agent running governed →
dashboard shows a DENIED probe → total flow < 10 minutes. Same flow green
via Hermes skill. MCP conformance tests pass against the spec revision
pinned in agent.lock.

---

## BLOCK 13 — Install path, docs, demo, and v0.1.0 release
**Builds:** distribution surface area —
- Homebrew tap (`brew install agentpaas/tap/agentpaas`) + Linux install
  script that is NOT `curl|bash`-blind: it downloads, prints the checksum
  + cosign verify command, and asks before executing (we sell trust;
  the installer must model it). deb/rpm via nfpm. goreleaser pipeline:
  darwin/arm64+amd64, linux/arm64+amd64, all binaries cosign-signed,
  SBOMs attached to the GitHub release.
- Docs site (docs/ → static): Quickstart (the <15-minute path), policy
  reference, secrets guide, "How enforcement actually works" (the
  network-topology page — security engineers read this one first),
  threat model (§3 of PRD published verbatim), audit-export verification
  guide for a second machine.
- The 3-minute demo video script + asciinema recordings embedded in
  README and landing page (Claude Code writes agent → pack → run →
  blocked exfil attempt → signed audit export).
- README: the 60-second story above the fold, containment table from
  `make redteam` pasted as proof, explicit "zero telemetry" statement.
**Edge cases:** clean-machine test on macOS (fresh user account) and
Ubuntu 24.04 container following ONLY the README — every deviation found
is a docs bug, filed and fixed before release; brew upgrade preserves
daemon state + restarts cleanly; uninstall (`agent uninstall`) removes
launchd/systemd units, containers, networks, and says what it deliberately
keeps (audit logs, keychain items) and how to purge them; air-gapped
install documented (offline image bundle).
**SUCCESS GATE:** two volunteers (not you) each reach a running governed
agent in < 15 minutes from the README on their own machines; `cosign
verify` documented-and-green on released artifacts; v0.1.0 tagged with
goreleaser, all CI gates (lint, test, -race, fuzz corpus, e2e-network,
redteam 10/10) green on the tag.

---

## 14. BLOCK SEQUENCING + PARALLELISM
```
B1 → B2 → B3 ─┬→ B4 → B5 ─┬→ B6 → B7 → B8 ─┬→ B9 → B10 → B11 → B12 → B13
              │            │                 │
              └ B3 gates everything security-spine-first
B4/B5 can interleave with B6 SDK design (contracts frozen in B1 protos).
B10 dashboard can start once B9 events exist; B11 red-team needs B5–B8.
```
Estimated calendar (nights/weekends, LLM-driven, one block ≈ 1–2 focused
sessions + review): B1–B5 ≈ 3 weekends, B6–B8 ≈ 3 weekends, B9–B11 ≈ 3
weekends, B12–B13 ≈ 2 weekends. ~11 weekends end to end; cut scope by
deferring Node SDK and CrewAI adapter if slipping, never by cutting
red-team or audit blocks.

## 15. DEFINITION OF DONE (PHASE 1)
The execution plan is complete when PRD v4 §8 (Success Definition) items
1–6 are demonstrably true via the gates above, and item 7 (5 design
partners) is in motion. Every gate command is in the Makefile; "done"
is always a command exiting 0, never a judgment call.

**END OF EXECUTION PLAN v1.0 — companion to agentpaas-prd-v4-master.md**
