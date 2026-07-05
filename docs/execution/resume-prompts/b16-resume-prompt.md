# B16 Manual Testing — Resume Prompt (Session 6)

**Date:** 2026-07-04
**Session:** Continue T1-T6 manual testing from user perspective
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 2ec08b8 (all fixes pushed to origin/main)
**Profile:** agentpaas-test (model: grok-4.3 via xai-oauth, free)

## START HERE

The user is manually testing AgentPaaS Block 16 (T1-T10). Start from T1 with
a full clean slate. The agentpaas-test profile should be clean — no daemon,
no plugin, no SOUL.md, no skill pointer, no state.

## STATE AT SESSION END (Session 5→6)

### Code (all committed + pushed)
- HEAD: 2ec08b8
- Binaries: /usr/local/bin/agentpaas{,d} md5 8b3e383a... (macOS)
- Harness: /usr/local/bin/agentpaas-harness-linux md5 d24f4c4f... (rebuilt with xai fix)
- All commits on origin/main:
  - 2ca9036  B16-T5: Add cron payload support + fix gateway restart
  - 4348f5c  B16: Auto-ensure toolset in register()
  - fb67bb4  B16: reset-agentpaas-test.sh removes skills/agentpaas pointer
  - bccc6bb  B16: Fix syntax error in cron_add schema payload description
  - 0f1620c  B16-T6: Proactive LLM detection + pre-pack gate
  - 2ec08b8  B16-T6: Fix xai provider name mismatch (xiai→xai)

### T1-T5 STATUS: ALL PASS (verified in Session 5)

- T1: Plugin install — PASS (no gateway restart, clean install)
- T2: Doctor 6/6 — PASS (tools visible, daemon live)
- T3: Weather agent — PASS (build + invoke + exfil, real data, 403 block)
- T4: Immutable redeploy — PASS (different digests, V2 code live)
- T5: Trigger/cron — PASS (cron with payload, trigger invoke, cron removed)

### T6 STATUS: IN PROGRESS — BLOCKED ON HARNESS REBUILD

T6 tests LLM-brokered SaaS/API action. The agent built a question-llm-agent
that uses agent.llm() to call xAI grok-4.3. The agent correctly:
- Detected LLM need from user intent (proactive)
- Asked for provider/model/key BEFORE writing code
- Stored credential via agentpaas_secret_add
- Configured LLM via agentpaas_llm_configure
- Added api.x.ai:443 to egress policy
- Wrote agent code using agent.llm() SDK pattern

BUT: all invoke attempts failed with "unknown llm provider: xai".
Root cause: the LLM adapter was registered as "xiai" not "xai".
Fixed in 2ec08b8 — GetAdapter now accepts both "xai" and "xiai".
Harness binary rebuilt (d24f4c4f...).

The agent needs to be re-packed and re-run with the fixed harness binary.
Previous packed image (sha256:85e0f8df...) has the OLD harness baked in.

### BUGS FIXED THIS BLOCK (8 total, all in Session 5-6)

1. t2-stale-socket-autostart (029505c): daemon auto-start false-positive
2. t3-schema-payload-path (96e7ece): trigger_invoke payload schema
3. t3-fab-guardrail-weak (96e7ece): anti-fab rule in SOUL.md
4. t2-audit-chain-recovery (eff35f9): daemon crash-loop on broken audit
5. t4-empty-payload-warning (d22c6b9): trigger_invoke empty payload
6. gateway-restart-during-install (2ca9036): after-install.md now says don't
7. cron-add-missing-payload (2ca9036): added payload param to all 6 layers
8. schemas-syntax-error (bccc6bb): unescaped quotes in payload description
9. reset-missing-skill-pointer (fb67bb4): reset script now removes skills/agentpaas
10. auto-ensure-toolset (4348f5c): register() runs ensure-toolset.py
11. xai-provider-mismatch (2ec08b8): adapter was "xiai", skill uses "xai"

### XAI OAUTH TOKEN

The agentpaas-test profile uses xai-oauth (grok-4.3). The OAuth access token
(821 chars, from auth.json) works against api.x.ai/v1/chat/completions.
Token is written to /tmp/xai-token.txt for the test agent to read.
NOTE: OAuth tokens expire — may need re-extraction if stale.

## WHAT TO DO WHEN SESSION RESUMES

1. Verify clean slate: no daemon, no plugin, no SOUL.md, no skill pointer
2. If not clean, run: cd ~/projects/agentpaas && bash scripts/reset-agentpaas-test.sh
   Then: rm -f ~/.hermes/profiles/agentpaas-test/SOUL.md
   Then: rm -rf ~/.hermes/profiles/agentpaas-test/skills/agentpaas
3. Tell user to restart: hermes -p agentpaas-test

### T1-T5 (re-run to verify fixes work clean)

T1: "Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas"
- Verify: no gateway restart, plugin installs clean
- After install: /quit, relaunch, /quit, relaunch (2 restarts for toolset)
- Verify: tools visible (agentpaas_doctor works as tool, not CLI fallback)

T2: "Run agentpaas_doctor to check if my AgentPaaS setup is healthy"
- Verify: 6/6 checks, daemon live

T3: "Build a weather agent that fetches current weather for a city. Use wttr.in as the weather API. Pack and run it, then invoke it with city 'Folsom'."
- Verify: real weather data in invoke-response.json
- Exfil: "Now test exfiltration: modify the weather agent to also try to send data to evil.example.com and repack and run it."
- Verify: 403 blocked, explain-denial shows default_deny

T4: "Build a simple echo agent... change to add [V2] prefix... repack..."
- Verify: different digests, V2 code live

T5: "Set up a cron schedule for the echo agent that invokes it every 5 minutes with payload {"text": "cron test"}."
- Verify: cron created with payload (payload fix working)
- Then: "Invoke the echo agent via the trigger API with payload {"text": "trigger test"}"
- Verify: trigger invoke returns correct echo
- Clean up: remove cron schedule

### T6: LLM-brokered SaaS/API action (AUTOMATED — ALL PROVIDERS)

T6 must be completed AUTONOMOUSLY by the agent (me), not manually by the user.
The user wants to test one model through EACH provider to confirm the full
credential → egress → LLM call chain works for all of them.

## AVAILABLE CREDENTIALS (already in agentpaas-test .env)

```
XAI_API_KEY         — xAI (api.x.ai)
GOOGLE_API_KEY      — Google Gemini (generativelanguage.googleapis.com)
GEMINI_API_KEY      — Google Gemini (duplicate)
OPENROUTER_API_KEY  — OpenRouter (openrouter.ai)
GLM_API_KEY         — Z.AI / GLM (api.z.ai)
```

Also available: xAI OAuth token from auth.json (821 chars, Bearer token,
works as API key against api.x.ai). See extraction code at bottom.

NOTE: XAI_API_KEY in .env has no credits (team has 0 balance). Use the
OAuth token instead — it works and is free via subscription.

## CURRENTLY SUPPORTED PROVIDERS (in provider.go GetAdapter)

- openai → api.openai.com:443 (adapter exists)
- anthropic → api.anthropic.com:443 (adapter exists)
- xai → api.x.ai:443 (adapter exists, fixed in 2ec08b8)

## NOT YET SUPPORTED (need adapter or use OpenAI-compatible mode)

- google/gemini → generativelanguage.googleapis.com:443 (NO adapter)
- openrouter → openrouter.ai:443 (NO adapter, but OpenAI-compatible API)
- zai/glm → api.z.ai:443 (NO adapter, but OpenAI-compatible API)

## T6 EXECUTION PLAN (autonomous, do all of this yourself)

### Phase 1: Verify keys independently

For EACH provider, extract the key from .env and make a direct curl/Python
HTTP call to the provider's API to confirm the key works BEFORE testing
through AgentPaaS. This isolates key issues from AgentPaaS issues.

Providers to test:
1. xai (use OAuth token from auth.json, not XAI_API_KEY which has no credits)
2. openrouter (OpenAI-compatible, use OPENROUTER_API_KEY)
3. google/gemini (use GOOGLE_API_KEY or GEMINI_API_KEY)
4. zai/glm (use GLM_API_KEY, OpenAI-compatible)

For each: make a trivial chat completion call, confirm 200 + real response.

### Phase 2: Test through AgentPaaS

For each provider that passed Phase 1:
1. Store the key via agentpaas_secret_add
2. Configure LLM via agentpaas_llm_configure (provider, model, credential)
3. Build a simple question-llm-agent that uses agent.llm()
4. Add the provider's domain to egress policy
5. Pack, run, invoke with "What is the capital of France?"
6. Verify real LLM response in invoke-response.json
7. Check harness-audit.jsonl for egress_allowed event

### Phase 3: Fix bugs

If any provider fails through AgentPaaS but passed independently:
- Check if adapter exists (provider.go GetAdapter)
- If no adapter: either add one or use the openai adapter with a custom
  base_url (OpenAI-compatible providers: openrouter, zai/glm, gemini)
- If adapter exists: check provider name mismatch (like the xai/xiai bug)
- Fix, rebuild binaries (make build-all + make build-harness-linux),
  install to /usr/local/bin/, restart daemon, repack agent, re-run

### Phase 4: Test failure paths

After all providers work:
- Test with wrong API key → verify clear error message
- Test with wrong domain in egress → verify 403 blocked
- Test with missing credential → verify clear error

### Phase 5: Stop

When all providers pass and failure paths are verified, stop and report
results to the user. Do NOT proceed to T7-T10 without user direction.

## MODELS TO USE (one per provider)

- xai: grok-4.3 (via OAuth token)
- openrouter: google/gemini-2.5-flash (or any free model)
- google/gemini: gemini-2.0-flash (or gemini-1.5-flash)
- zai/glm: glm-4-flash (or glm-4)

## KEY EGRESS DOMAINS

- xai: api.x.ai:443
- openrouter: openrouter.ai:443
- google: generativelanguage.googleapis.com:443
- zai: api.z.ai:443 (or open.bigmodel.cn:443)

## IMPORTANT NOTES

- The harness binary is BAKED INTO the Docker image during pack. After
  any Go code change, rebuild BOTH:
    make build-all && sudo cp bin/agentpaas bin/agentpaasd /usr/local/bin/
    make build-harness-linux && sudo cp bin/agentpaas-harness-linux /usr/local/bin/
  Then restart daemon, REPACK the agent (old image has old harness), re-run.
- OAuth tokens expire. If xai OAuth token fails, re-extract from auth.json.
- The agentpaas-test profile runs grok-4.3 via xai-oauth. This is the
  Hermes session model — it is NOT the same as the AgentPaaS agent's LLM.
  The AgentPaaS agent runs in a container and calls the LLM API directly
  via the harness, using credentials from Keychain.

## T-CARD REFERENCE

- T6: B16-UC02 — Secret-brokered SaaS/API action (LLM through egress)
- T7: B16-UC03 — Agentic repair loop
- T8: B16-UC05 — Long-running mixed-egress agent
- T9: B16-UC06 — Clean-machine install under 15 minutes
- T10: B16-UC07 — Policy authoring from scratch

## OPEN TASKS (post-B16)

1. **Generic provider registry**: Make LLM provider registry generic so new
   OpenAI-compatible providers don't need code changes. Auto-discover from
   base_url + auth header pattern.
2. **t2-invoke-spawns-new-run**: trigger invoke starts a NEW container
   instead of invoking the already-running agent.
3. **t3-checkpoint-loop**: daemon crashes on restart with corrupted checkpoint key.
4. **post-b16-rich-input**: large files, Drive/cloud links, online URLs, videos, images.

## KEY SKILLS TO LOAD

- agentpaas-autonomous-testing (test flow, pitfalls, verification)
- cost-aware-model-selection (every session start)

## CREDENTIAL FLOW (for reference)

```
agentpaas_secret_add → macOS Keychain
                         ↓
daemon buildInvokePayload() reads Keychain at invoke time
                         ↓
harness payload: {llm: {provider, model, credential}, credentials: [{id, header, value}]}
                         ↓
harness RPC handleLLM() uses credential value to call LLM API
                         ↓
egress gateway enforces domain allowlist (api.x.ai:443)
```

The agent code uses `agent.llm(prompt=...)` from the SDK. The daemon injects
the credential at runtime from Keychain. Agent code should NOT read API keys
from env vars or hardcode them.

## XAI OAUTH TOKEN EXTRACTION (if /tmp/xai-token.txt is stale)

```python
import json
with open('/Users/pms88/.hermes/profiles/agentpaas-test/auth.json') as f:
    auth = json.load(f)
tokens = auth['providers']['xai-oauth']['tokens']
at = tokens.get('access_token', '')
with open('/tmp/xai-token.txt', 'w') as f:
    f.write(at)
```

Token works against api.x.ai/v1/chat/completions with model grok-4.3.
