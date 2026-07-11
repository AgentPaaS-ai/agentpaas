# AgentPaaS Roadmap

Post-P1 tasks identified during manual testing and security review. Ordered by priority.

## v0.2.x execution sequence — Blocks 25–27

The executable source of truth is the block summaries; this roadmap does not
override their gates.

1. **B25 release closure:** T00 first upgrades the Go toolchain, enforces a
   patched Docker Engine, and migrates to supported Moby modules. Then complete
   Hermes sharing UX, S1–S10, the seven-claim adversary gate, documentation,
   and finally v0.2.0/Homebrew promotion.
2. **B26 platform proof:** build the Codex skill over JSON CLI, extract the
   shared Hermes/Codex safety conformance suite, and produce the deterministic
   Agent Approval Pack. Generic MCP is a nonblocking decision. Hosted Grok and
   distribution rails stay demand-gated.
3. **B27 enterprise proof:** follow its 14 dependency-ordered P0 chunks. Prove
   the gateway/Credential Broker topology first; build signed keyless receiver
   mappings/OAuth and remove harness credential preload; then add constrained
   approved actions, receipts, multi-LLM, portable MCP, data-flow/provider/
   report evidence, and only then run the Alice/Bob golden demo.

This order prevents demo/integration work from hardening the wrong credential
architecture and keeps publishable security claims behind executable gates.

## v0.1.2 — Security Claim Closure (Block 20, NEXT)

B20 executes before B19. It makes the current README/security claims true
before adding broader gateway-native governance. Spec:
`docs/execution/blocks/b20-summary.md`

### P0 — Claim closure

- **Credential invisibility:** user agent code receives only user payload;
  raw credentials never reach `agent.invoke`, stdout/stderr, invoke response,
  audit, Docker exec stdin, harness `/invoke`, or Python worker stdin.
- **Gateway credential injection:** B19 T1 is pulled forward. Credentials are
  resolved for gateway-side injection; harness/agent see credential IDs only.
- **Runtime artifact verification:** `Run` verifies signed lock, deployed
  immutability, image digest, and policy digest before Docker resources are
  created.
- **Full policy semantics:** method, port, credential binding, and CIDR
  behavior are enforced or rejected explicitly. No silent narrowing to hostname-only.
- **Audit completion:** harness audit records enter daemon audit chain on every
  terminal path.
- **Fail-closed inputs:** invalid trigger JSON, missing credentials, and
  unconfigured LLM calls fail before misleading runtime behavior.
- **Red-team gate:** every major README security claim maps to an adversary test.

### P1 — Truth sync and release hygiene

- Public docs and PRD align with implementation.
- Broken docs links are fixed and checked.
- Firewall/capability claims are either implemented fail-closed or documented
  as defense-in-depth.
- Demo/weather-agent and brew release state are made consistent with public UX.

## Post-B20 — AgentGateway Policy Integration (Block 19, DEFERRED)

Wire up agentgateway v1.3.0's native policy features after B20 passes. Spec:
`docs/execution/blocks/b19-summary.md`

### P0 after B20 — Gateway-Level Governance

- **LLM token budgets:** policy.yaml `llm_budget` section (max_tokens,
  max_tokens_per_request). Gateway enforces.
- **LLM rate limiting:** policy.yaml `llm_rate_limit` section
  (requests_per_minute, tokens_per_minute). Gateway enforces.
- **B19 manual testing:** Re-run B18 T1-T10 with gateway-level policies.

### P1 — Auth & Provider Locking

- **Ingress auth (JWT & API key):** policy.yaml `ingress_auth` section.
  Gateway validates trigger requests before forwarding to agent.
- **Egress OAuth (backend token refresh):** Gateway obtains and refreshes
  OAuth tokens for upstream LLM providers. Solves xAI 6h token expiry.
- **LLM provider locking:** Gateway enforces that LLM calls only go to
  the configured provider's endpoint (defense-in-depth beyond egress).

### P2 — Guardrails, Transformations, Observability

- **Guardrails:** policy.yaml `guardrails` section — regex blocking,
  OpenAI moderation, custom webhooks on LLM prompts/responses.
- **Request/response transformations:** Inject system prompts, remove
  headers, modify bodies before LLM calls.
- **Per-route timeouts & retry:** policy.yaml egress rules with timeout
  and retry config per domain.
- **Cost tracking & observability:** OTel metrics, per-run token/cost
  reports via `agctl costs`.
- **MCP tool access control:** Per-tool allow/deny lists for MCP servers.
- **Traffic splitting:** Canary/A-B testing for agent versions via
  gateway route splitting.

## P0 — Security & Correctness

### Task: Egress policy must match configured LLM provider, not blanket-allow all

**Status:** COMPLETE — Block 17 (2026-07-05)

**Problem:** When an agent has `llm.provider: xai` in `agent.yaml`, the
`allow-llm` policy template grants egress to `openrouter.ai` only (after
B16 fix). But there is no pack-time validation that the configured LLM
provider's domain is present in the egress policy. An agent with
`llm.provider: xai` could pack successfully without `api.x.ai` in its
egress policy, then fail at runtime when the gateway blocks the LLM call.

**Found during:** B16-T6 Session 8 (2026-07-04)

**Root cause:**
- `internal/cli/policy_templates.go` — the `allow-llm` template is static
- No pack-time or validate-time cross-check between `llm.provider` and
  egress policy domains
- The onboarding flow (deploy skill) uses `allow-llm` without ensuring
  the configured provider's domain is present

**Fix (Block 17):**
1. Add `llm.ProviderDomain(provider)` to map provider → egress domain
2. Add pack-time enforcement: if `llm.provider` is set, its domain MUST
   be in egress policy or pack fails with an actionable error
3. Add validate-time advisory warning (not error) for the same check
4. Add `--provider` flag to `policy init --template allow-llm` for
   provider-specific template generation

**Acceptance criteria:**
- An agent with `llm.provider: xai` has egress ONLY to `api.x.ai`
- An agent with `llm.provider: openai` has egress ONLY to `api.openai.com`
- Pack FAILS if the configured LLM provider's domain is not in egress policy
- `policy init --template allow-llm --provider xai` generates policy with
  only `api.x.ai:443`
- No stale credential references in generated policy.yaml

---

### Task: Secure secret ingestion flow (terminal-based, not through LLM context)

**Status:** COMPLETE — Block 17 (2026-07-05)

**Problem:** The `agentpaas_secret_add` Hermes tool accepts the API key
value as a tool parameter (`value` arg). When the Hermes agent calls this
tool, the key value enters the conversation context and is sent to the LLM
provider as part of the tool-call arguments. This violates the security
principle that API keys should never be exposed to the LLM.

**Found during:** Block 17 analysis (2026-07-05)

**Root cause:**
- `integrations/hermes-plugin/tools.py` `agentpaas_secret_add()` accepts
  `value` as a tool parameter
- The SKILL.md onboarding flow instructs the agent to call the tool with
  the value, rather than telling the user to run the CLI command directly

**Fix (Block 17):**
1. Update SKILL.md onboarding flow: instruct the USER to run
   `agentpaas secret add <name>` in their terminal, NOT have Hermes call
   the tool with the value
2. Hermes verifies the secret exists via `agentpaas_secret_list` (labels
   only, never values)
3. Update SOUL.md snippet to mention terminal-based secret ingestion
4. Keep the `value` parameter in the tool for backward compatibility but
   deprecate it for the standard onboarding flow

**Acceptance criteria:**
- SKILL.md onboarding flow instructs user to run terminal command
- Hermes only verifies secret existence via `agentpaas_secret_list`
- API key value never enters the Hermes conversation context during
  the standard onboarding flow
- Backward compatibility: existing `agentpaas_secret_add` tool still works

---

## P1 — Provider Extensibility

### Task: Add OpenRouter as a first-class LLM provider

**Problem:** OpenRouter (`openrouter.ai/api/v1/chat/completions`) is not
currently supported as a provider. Users wanting OpenRouter models
(e.g. `deepseek/deepseek-v4-flash`) must use the Nous Research provider
as a proxy, which has a ~15-minute token TTL and is fragile.

**Why:** OpenRouter is a standard OpenAI-compatible endpoint. Adding it
follows the same 9-file pattern as the Nous provider addition (commit
62b8e4c). The longer-term fix is a generic provider registry (below)
that eliminates the per-provider boilerplate.

**Proposed fix:**
1. Add OpenRouter adapter (`internal/llm/openrouter.go`) — copy `nous.go`,
   change endpoint to `openrouter.ai/api/v1/chat/completions`
2. Add to `endpoints.go`, `provider.go` GetAdapter/SupportedProviders
3. Update `providertest.go` with `testOpenRouter()`
4. Update `detectProviderFromName()` + help text in `control.go`
5. Add to plugin `tools.py` provider validation set
6. Update `schemas.py` descriptions
7. Update `SKILL.md` provider list + egress domains
8. Add `provider_test.go` expected provider count

**Acceptance criteria:**
- `agentpaas secret test <name> --provider openrouter` validates the key
- `agent.llm()` with `provider: openrouter` returns a real response
- Model names like `deepseek/deepseek-v4-flash` work

---

### Task: Generic provider registry (eliminate per-provider boilerplate)

**Problem:** Adding a new OpenAI-compatible provider requires changes in
9+ files (adapter, endpoints, provider.go, tests, CLI, plugin tools,
schemas, SKILL.md). This is hardcoded and fragile — every provider
addition repeats the same pattern.

**Proposed fix:** Config-driven provider discovery. A provider can be
added via agent.yaml or a providers config file without code changes:
```yaml
llm:
  provider: custom-openai-compat
  model: some-model
  credential: my-key
  endpoint: https://api.custom-provider.com/v1/chat/completions
```
The adapter auto-detects OpenAI-compatible APIs and routes accordingly.

**Acceptance criteria:**
- Adding a new OpenAI-compatible provider requires zero Go code changes
- Existing hardcoded providers (openai, anthropic, xai, nous) still work
- `agentpaas secret test` works with custom providers

---

## B27 P0 — Multi-Channel LLM Routing

### Task: Support multiple LLMs per agent with independent gates

**Status:** EXECUTION-PLANNED as B27 T09 (dependency-ordered after the
request-time Credential Broker and SaaS route constraints)

**Problem:** An agent currently has ONE `llm:` block in agent.yaml and
all policy gates (`llm_budget`, `llm_rate_limit`, `llm_provider_lock`,
`guardrails`) apply globally to that single LLM route. There is no
support for an agent that needs two or more LLMs — e.g. a cheap router
model for triage + an expensive model for generation, each with its own
budget, rate limit, provider lock, and guardrails.

**Current limitation (verified against schema.go + agent.yaml):**
- agent.yaml `llm:` is singular (one provider+model+credential)
- policy.yaml `llm_budget` / `llm_rate_limit` are single structs, not
  per-channel maps
- `agent.llm()` in the SDK has no channel/target parameter
- `llm_provider_lock.allowed_endpoints` is a single flat list
- An agent can call a 2nd LLM via `agent.http()` directly, but that
  bypasses ALL LLM gates (budget, rate-limit, guardrails, provider lock)

**Proposed fix:**
1. agent.yaml `llm:` becomes a list of named channels:
   ```yaml
   llm:
     channels:
       router: {provider: openrouter, model: deepseek-router, credential: or-key}
       writer: {provider: anthropic, model: claude-sonnet-4, credential: anthropic-key}
   ```
2. SDK `agent.llm(channel="writer", prompt=...)` routes to the named channel
3. policy.yaml gates become per-channel maps:
   ```yaml
   llm_budget:
     router: {max_tokens: 1000}
     writer: {max_tokens: 50000}
   llm_rate_limit:
     router: {requests_per_minute: 60}
     writer: {requests_per_minute: 5}
   llm_provider_lock:
     router: {allowed_endpoints: [...]}
     writer: {allowed_endpoints: [...]}
   guardrails:  # global still supported, plus per-channel override
     writer: [...]
   ```
4. Gateway compiles separate routes per channel with independent gates
5. Backward compat: singular `llm:` block still works (treated as default channel)

**Acceptance criteria:**
- agent.yaml with 2+ LLM channels packs and runs
- Each channel has independent budget/rate-limit/provider-lock enforcement
- `agent.llm("router", ...)` and `agent.llm("writer", ...)` hit different providers
- Audit records which channel each LLM call used
- Existing single-LLM agents work unchanged

**Found during:** T11 manual testing (user question, 2026-07-09)

---

## P1 — Three-Layer LLM Token Budget Enforcement

### Task: Pass max_tokens to LLM API + inject system prompt guidance

**Status:** COMPLETE (2026-07-11, Bug 020) — LLM adapters support max_tokens via variadic parameter. Harness reads max_tokens_per_request from budget config and passes to BuildRequest (rpc_server.go:354).

**Problem:** Three enforcement layers exist but only one works:

1. **API max_tokens parameter (MISSING)** — LLM adapters (`BuildRequest` in
   openrouter.go, openai.go, anthropic.go, xai.go, nous.go) omit `max_tokens`
   from the JSON request body entirely. OpenAI-compatible APIs support
   `max_tokens` as a hard server-side cap — the LLM literally stops generating
   at that limit. This is the strongest enforcement and it's not wired.

2. **System prompt guidance (MISSING)** — `Transformations.RequestTransform.
   InjectSystemPrompt` exists in policy schema but isn't wired to the harness
   LLM call. A system prompt like "Keep your response concise, under N tokens"
   guides the model to self-limit before hitting the hard API cap, producing
   cleaner output (no mid-sentence cutoff).

3. **Harness BudgetEnforcer post-hoc counter (FIXED — Bug 019)** — Records
   tokens after the LLM call completes. Kills the process if total exceeds
   budget. Weakest layer — the LLM has already generated and returned the full
   response before this fires.

**Proposed fix:**

1. **Wire max_tokens_per_request to the LLM adapter:**
   - Change `BuildRequest` signature to accept `maxTokens int`:
     `BuildRequest(ctx, model, prompt, credentialValue string, maxTokens int)`
   - When `maxTokens > 0`, add `"max_tokens": maxTokens` to the request body
   - Harness `handleLLM` reads `max_tokens_per_request` from budget config
     and passes it to `BuildRequest`
   - Anthropic adapter: use `max_tokens` (already hard-codes 1024)

2. **Wire system prompt injection from Transformations:**
   - If `transformations.request.inject_system_prompt` is set, prepend it
     as a system message in the LLM request:
     `{"role": "system", "content": inject_system_prompt + "\n\nKeep your response under N tokens."}`
   - Auto-generate system prompt from `llm_budget.max_tokens_per_request`:
     `"Keep your response concise. Limit to approximately N tokens."`

3. **Three-layer enforcement chain:**
   - `max_tokens_per_request: 50` → system prompt says "keep under 50 tokens"
     → API body has `"max_tokens": 50` → LLM stops at 50 → harness records 50
     → budget counter at 50/100 → agent can make one more call

**Acceptance criteria:**
- LLM request body includes `"max_tokens": N` when `max_tokens_per_request` is set
- System prompt with token guidance is sent when budget is configured
- LLM response is truncated by the API (not by post-hoc kill)
- Existing agents without budget work unchanged (max_tokens omitted)
- Audit shows the max_tokens value in the LLM request

**Found during:** T11 manual testing (user observation, 2026-07-09)

---

## P1 — Per-Egress-Route Gate Isolation

### Task: Independent rate limiting, content filtering, and budgets per egress destination

**Status:** PROPOSED (2026-07-09, found during T11 manual testing)

**Problem:** An agent may call multiple non-LLM HTTP endpoints, but
only LLM traffic gets rate limiting, guardrails, and budget enforcement.
Per-egress-rule fields already exist for `methods`, `credential`,
`timeout`, and `retry` (verified against schema.go + compiler.go), but
there is no per-egress-route rate limiting, content filtering, or
request/response budgeting for non-LLM HTTP traffic.

**Current per-egress (schema.go EgressRule):**
- `methods []string` — per-rule method restriction ✓
- `credential string` — per-rule credential binding ✓
- `timeout string` — per-rule, compiled to gateway route ✓
- `retry *RetryConfig` — per-rule (max_attempts, backoff, max_backoff) ✓

**Missing per-egress (LLM-only or nonexistent):**
- `llm_rate_limit` — LLM calls only; no HTTP egress rate limiting
- `guardrails` — LLM prompts/responses only; no HTTP content filtering
- `llm_budget` — LLM calls only; no per-route request budget
- No per-egress-rule request body inspection / response filtering

**Proposed fix:**
1. Add optional per-egress-rule gate fields to EgressRule:
   ```yaml
   egress:
     - domain: api.stripe.com
       ports: [443]
       methods: [GET, POST]
       credential: stripe-key
       timeout: 10s
       retry: {max_attempts: 3, backoff: exponential, max_backoff: 30s}
       rate_limit:                    # NEW
         requests_per_minute: 30
       response_filter:               # NEW — regex/JSON-path content filtering
         - pattern: "(?i)(card|cvv|ssn)"
           action: redact
       max_request_bytes: 1048576     # NEW — per-route body budget
   ```
2. Compiler emits `localRateLimit` policies per non-LLM route (same
   mechanism already used for LLM routes)
3. Response filter runs at gateway level — strips/redacts matched
   patterns before response reaches agent container
4. Per-route body budget rejects requests exceeding byte limit
5. Audit records which egress route each call used + which gate fired

**Acceptance criteria:**
- Two egress rules with different `rate_limit` values enforce independently
- Response content matching `response_filter` patterns is redacted before
  reaching agent code
- Per-route `max_request_bytes` rejects oversized requests
- Existing agents without per-egress gates work unchanged (fields optional)
- Audit shows per-route gate enforcement events

**Found during:** T11 manual testing (user question, 2026-07-09)

---

## P2 — UX & Reliability

### Task: LLM credential auto-refresh for OAuth providers

**Status:** SUPERSEDED by B27 T04–T05. Do not implement refresh-on-invoke or
container-start credential loading.

**Problem:** xAI OAuth tokens expire (~6h TTL). When multiple Hermes
profiles share the same OAuth client, refreshing in one profile revokes
the token in others. Agents store a snapshot of the token, which goes
stale. There's no auto-refresh mechanism.

**Found during:** B16-T6 Session 8 (2026-07-04)

**Planned fix:** receiver-local OAuth bindings keep refresh material in
Keychain. The deterministic Credential Broker refreshes pre-expiry or once
after an eligible 401 and authorizes per-request gateway header mutation.
Tokens never enter agent/harness/container configuration.

**Acceptance criteria:**
- An agent with an expired OAuth token doesn't fail permanently
- Token refresh happens transparently — no manual re-store needed

---

### Task: OAuth token exchange support (Codex, xOAuth, PKCE flows)

**Status:** SUPERSEDED by B27 T04–T05. Codex operator integration in B26 is
separate from SaaS OAuth account binding.

**Problem:** Some auth flows require token exchange — an authorization
code or refresh token is exchanged for an access token via a token
endpoint. Examples: Codex CLI integration, xOAuth, PKCE-based OAuth
flows. These are more complex than a static API key and cannot be
ingested via the simple `agentpaas secret add` stdin flow.

**Found during:** Block 17 analysis (2026-07-05)

**Planned fix:** provider-neutral authorization uses external-browser PKCE/
loopback or supported device flow. Signed packages declare logical provider,
scope, audience, and tenant requirements; receiver-local client/refresh
material stays in Keychain. The Broker exchanges/refreshes and authorizes one
gateway request after signed route and identity checks.

**Acceptance criteria:**
- terminal-only `agentpaas auth` commands create and manage OAuth bindings
- the Broker performs exchange/refresh at authorization time, never by loading
  credentials into an agent or container
- Token exchange errors are surfaced with actionable guidance
- Works with PKCE-based OAuth flows (Codex, xOAuth)
