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
│   └── pack/             # build pipeline, sbom, sign, secret-scan
├── web/dashboard/        # SPA (preact/lit + TS, embedded via go:embed)
├── sdk/python/           # agentpaas-sdk
├── sdk/node/             # @agentpaas/sdk
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
AuditExport, Doctor. Use stable proto package names
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
container and the ingress/egress gateway sidecar. Per-agent `internal:
true` bridge; gateway sidecar dual-homed (internal bridge + egress
network); agent container never shares the gateway network namespace; agent
container hardening flags (non-root uid 64000, read-only rootfs, tmpfs
/tmp, cap-drop ALL, no-new-privileges, seccomp default profile, pids-limit
256, memory/cpu from agent.yaml); DNS of agent container pointed at gateway
stub IP only.
**Edge cases / negative tests (heart of the product — exhaustive):**
- canary on internal net: `curl https://1.1.1.1` → no route, fails fast
  (assert timeout behavior, not a hang)
- direct DNS to 8.8.8.8 → unreachable
- `host.docker.internal` → unreachable unless policy-allowed
- IPv6: no route (AAAA answers and direct v6 literals both dead)
- UDP egress (non-DNS) → blocked
- container restart preserves network membership
- daemon crash leaves no half-fenced agent: startup reconciliation kills
  any agent container whose gateway is absent
- agent container and gateway sidecar do not share a network namespace
- Docker Desktop vs colima vs Linux dockerd: topology holds on all three
**SUCCESS GATE:** `make e2e-network` runs the canary suite and prints a
table of 8 attack vectors, all BLOCKED, on macOS (Docker Desktop + colima)
and Linux CI.

---

## BLOCK 6 — Harness (cmd/harness) + SDK contracts
**Builds:** Go harness as container PID 1: exec user code per runtime
(python/node); HTTP contract (`POST /invoke`, `GET /healthz|readyz` on
localhost:8000 inside container); budget enforcement (max_iterations via
SDK count + harness-observed LLM-call count, wall-clock timer, token/USD
accounting from gateway-reported usage; breach → SIGTERM, 10s grace,
SIGKILL, status=BUDGET_EXCEEDED, audit event); checkpoint API (opaque
blob → daemon store); OTel emit. Python SDK (`agentpaas-sdk`): decorators
`@agent.on_invoke`, `agent.llm()` (OpenAI-compatible client preconfigured
to gateway), `agent.http(credential_id, ...)`,
`agent.mcp(server_id, tool, input)`.
Brokered credentials are never returned to SDK callers. `agent.secrets.file()`
exists only for explicit direct-lease compatibility mode and is discouraged
in generated code. Node SDK mirrors.
**Edge cases:** user code crashes on import → FAILED with stderr captured;
user code ignores SIGTERM → killed at grace deadline; zombie processes
reaped (PID 1 duty); invoke payload 50MB → rejected 413 (limit 10MB,
configurable); unicode/binary payloads round-trip; concurrent invokes →
serialized by default (`concurrency: 1`), explicit opt-up; budget race
(token usage reported after kill) → accounted post-hoc, audit shows
overage; wall-clock budget uses monotonic clock (immune to clock-set);
MCP call to undeclared server/tool is denied before execution and audited;
MCP tool input/output bodies are not logged, only hashes and metadata.
**SUCCESS GATE:** e2e: an infinite-loop agent with max_wall_clock=30s dies
at 30s±2s with BUDGET_EXCEEDED + audit event; a token-burn agent dies at
the token cap; both SDKs pass the same harness contract test suite.

---

## BLOCK 7 — Secrets broker
**Builds:** internal/secrets — `agent secret set/list/rm` (values read from
stdin/interactive prompt, NEVER argv so they never hit shell history or
process lists); brokered outbound credential flow (gateway sidecar requests
credential use from daemon/secrets broker over local authenticated channel;
daemon validates run id + policy rule id + destination + method; gateway
injects header/query/body field per policy and originates the upstream TLS
request; raw value is never sent to the agent container); direct lease flow
for compatibility (`file_lease` into
tmpfs 0400 owned by agent uid; `env_lease` opt-in, warned, discouraged);
revocation invalidates brokered use immediately and restarts affected
direct-lease agents. Audit guarantee: brokered injection emits
`secret_injected` with `visible_to_agent=false`; direct lease emits
`secret_leased` with `visible_to_agent=true`; SDK lease-helper reads emit
`secret_read`; P1 does not claim reliable per-open auditing for raw file
reads of a direct lease.
**Edge cases:** brokered secret referenced but not set → launch refuses
naming the missing secret; brokered credential used for wrong domain,
method, or port → denied before injection and audited; gateway crash cannot
dump secret in logs; keychain locked → actionable error, no plaintext
fallback; secret containing newlines/UTF-8/4KB length injects/round-trips
exactly; agent attempts `env`, `/proc`, filesystem walk, and `docker
inspect` to find a brokered sentinel secret → zero hits; compiled gateway
config and policy digest contain credential ids only, never values; direct
lease tmpfs file gone after `agent stop` (asserted); raw file read succeeds
for an explicit direct lease but is not claimed as a precise per-read audit
event.
**SUCCESS GATE:** negative suite green, including grep of full
`docker inspect`, gateway logs, compiled configs, exported image layers,
and agent filesystem/proc probes for a brokered sentinel secret → zero
hits; a real brokered OpenAI-style request receives the Authorization
header upstream while agent logs/proc/env never contain the key.

---

## BLOCK 8 — Packaging pipeline (`agent pack`)
**Builds:** internal/pack — framework detection (plain Python, LangGraph,
CrewAI markers, Node), buildkit image assembly (distroless base by digest,
locked deps via uv / npm ci, harness as PID 1, non-root, no shell),
gitleaks secret scan (fail-closed), syft SBOM (SPDX-json, attached as OCI
artifact), cosign sign with the agent identity key, emit `agent.lock`
(image digest + SBOM digest + policy digest + identity pubkey).
**Edge cases:** no agent.yaml → offer `agent init` scaffold; dependency
conflict → surfaced verbatim, abort; 2GB build context → .agentpaasignore
honored, warn >100MB; secret in source → FAIL naming file:line;
`--allow-secret-pattern` logged into the audit trail; rebuild without
changes → identical image digest (reproducibility); LangGraph and CrewAI
example repos pack without a Dockerfile; `--dockerfile` escape hatch still
gets harness + hardening layered on (or refuses with clear limits).
**SUCCESS GATE:** 4 reference agents (plain-py, langgraph, crewai, node)
pack green; `cosign verify` passes; SBOM lists expected top-level deps;
secret-scan e2e blocks a planted key.

---

## BLOCK 9 — Trigger API + events/webhooks + cron
**Builds:** Trigger API serving (gRPC :7718 + grpc-gateway REST :7717,
loopback; `--expose` path requires API key, refuses otherwise);
idempotency (key→run_id replay window 24h); rate limit (token bucket
per caller); InvokeStream (gRPC stream + REST SSE); internal/events bus,
webhook delivery (HMAC-signed, 3 retries exp backoff, dead-letter to
audit); cron triggers from agent.yaml feeding the same Invoke path.
**Edge cases:** replayed idempotency key → same run_id, no second
execution; replay with DIFFERENT payload + same key → 409; burst 1000
invokes → rate-limited with Retry-After; malformed JSON → 400 with line;
oversized payload → 413; webhook target down → retries then dead-letter
audit entry; webhook target = non-allow-listed domain → blocked by policy
(hooks are egress too); cron during daemon downtime → missed-run policy
explicit (`skip` default, `catchup: 1` opt-in); DST transition → cron uses
local tz with documented behavior; CancelRun mid-LLM-call → graceful then
forced.
**SUCCESS GATE:** API conformance suite (generated from proto) green;
idempotency + rate-limit + SSE e2e green; fuzz on REST JSON ingestion
(100k execs, 0 crashes).

---

## BLOCK 10 — OTel pipeline + Dashboard
**Builds:** in-process OTLP collector → SQLite (WAL) with retention prune
(default 7d local, configurable); dashboard SPA (preact+TS, go:embed, no
runtime CDN): agent list w/ status+spend-vs-budget, run timeline (LLM
calls w/ tokens+cost, MCP calls, egress ALLOWED/DENIED rows in red),
log viewer, policy view w/ git-diff, audit search + one-click signed
export, live SSE event stream. Strict CSP, no inline JS, CSRF token on
mutating routes.
**Edge cases:** 10k-span run renders (virtualized list); SQLite locked by
concurrent writer → dashboard reads stay snappy (WAL + read pool);
dashboard with daemon restarting → reconnects SSE gracefully; XSS attempt
via agent-controlled log line / trace attribute → escaped (test with
planted `<script>` in agent output); clock-skewed spans → ordered by
monotonic seq; empty states designed (zero agents, zero runs).
**SUCCESS GATE:** Playwright e2e: launch agent → watch live run → see a
DENIED egress row → export audit → verify export. Lighthouse perf ≥ 90
local. Planted-XSS test shows escaped output.

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
`agentpaas_audit_query`). This single adapter is the universal wedge: any
MCP-speaking coding agent (Claude Code, Codex, Cursor, Hermes via
native-mcp) gets "deploy what you just built, safely" as a verb.
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
