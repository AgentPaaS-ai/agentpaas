# AgentPaaS Roadmap

Post-P1 tasks identified during Block 16 manual testing. Ordered by priority.

## P0 — Security & Correctness

### Task: Egress policy must match configured LLM provider, not blanket-allow all

**Status:** IN PROGRESS — Block 17 (2026-07-05)

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

**Status:** IN PROGRESS — Block 17 (2026-07-05)

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

## P2 — UX & Reliability

### Task: LLM credential auto-refresh for OAuth providers

**Problem:** xAI OAuth tokens expire (~6h TTL). When multiple Hermes
profiles share the same OAuth client, refreshing in one profile revokes
the token in others. Agents store a snapshot of the token, which goes
stale. There's no auto-refresh mechanism.

**Found during:** B16-T6 Session 8 (2026-07-04)

**Proposed fix:** Support a "refresh-on-invoke" credential type that
re-fetches the OAuth token from the Hermes auth store before each
container start. Alternatively, support a credential proxy that
intercepts 401s and retries with a refreshed token.

**Acceptance criteria:**
- An agent with an expired OAuth token doesn't fail permanently
- Token refresh happens transparently — no manual re-store needed

---

### Task: OAuth token exchange support (Codex, xOAuth, PKCE flows)

**Problem:** Some auth flows require token exchange — an authorization
code or refresh token is exchanged for an access token via a token
endpoint. Examples: Codex CLI integration, xOAuth, PKCE-based OAuth
flows. These are more complex than a static API key and cannot be
ingested via the simple `agentpaas secret add` stdin flow.

**Found during:** Block 17 analysis (2026-07-05)

**Proposed fix:** Support a credential type that performs token
exchange at credential-broker time. The agent.yaml specifies a token
endpoint, client ID, and refresh token (stored in keychain). The
gateway broker exchanges the refresh token for an access token before
injecting it into the agent container.

**Acceptance criteria:**
- `agentpaas secret add` supports a `--type oauth` mode that stores
  a refresh token + token endpoint metadata
- The gateway broker performs token exchange before container start
- Token exchange errors are surfaced with actionable guidance
- Works with PKCE-based OAuth flows (Codex, xOAuth)
