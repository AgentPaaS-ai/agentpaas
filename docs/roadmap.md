# AgentPaaS Roadmap

Post-P1 tasks identified during Block 16 manual testing. Ordered by priority.

## P0 — Security & Correctness

### Task: Egress policy must match configured LLM provider, not blanket-allow all

**Problem:** When an agent has `llm.provider: xai` in `agent.yaml`, the
`allow-llm` policy template grants egress to `api.openai.com`,
`api.anthropic.com`, AND `api.x.ai`. An agent configured for xAI should
only have egress to `api.x.ai`. Granting egress to providers the agent
doesn't use violates least-privilege and expands the attack surface.

Additionally, the `allow-llm` template hardcodes a stale
`openai-api-key` credential reference in the `credentials:` section,
which is noise for agents using a different provider.

**Found during:** B16-T6 Session 8 (2026-07-04)

**Root cause:**
- `internal/cli/policy_templates.go` — the `allow-llm` template lists
  all three providers unconditionally
- The onboarding flow (deploy skill) uses `allow-llm` without pruning
  to the configured provider

**Proposed fix (two layers):**
1. Make `allow-llm` template take a provider argument (or generate
   provider-specific templates: `allow-llm-xai`, `allow-llm-openai`,
   `allow-llm-anthropic`, `allow-llm-nous`)
2. Better: auto-derive egress from `agent.yaml`'s `llm:` section at pack
   time. If `llm.provider: xai` is set, the pack step should
   automatically add `api.x.ai:443` to egress (and warn if it's missing).
   The template should be a starting point, not the final word.

**Acceptance criteria:**
- An agent with `llm.provider: xai` has egress ONLY to `api.x.ai`
- An agent with `llm.provider: openai` has egress ONLY to `api.openai.com`
- Pack warns (or errors) if the configured LLM provider's domain is not
  in the egress policy
- No stale credential references in generated policy.yaml

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
