# M16.2 ADV — LangGraph library golden

SHA: 57f36f4 feat(m16.2): LangGraph library golden and pack reject
Branch: feat/m16.2-langgraph
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m16-2-langgraph
Lane: B. Claims this T: SC1, SC3, SC4, SC6.
Worker TestM162* left green and untouched. ADV tests compile and stay RED.

## BREAKS

- HIGH SC1 harness `internal/harness/m16_2_langgraph_adv_test.go:147` / `:151` `TestADV_M16_2_SC1_LangChainLangSmithKeysAbsentFromLangGraphWorkloadEnv` — `workerEnvOpenAI` / `isWorkloadOpenAIEnv` inherit `LANGCHAIN_API_KEY`, `LANGSMITH_API_KEY`, `LANGGRAPH_API_KEY`, `LANGGRAPH_CLOUD_API_KEY`, `LANGCHAIN_HUB_API_KEY`, `FIREWORKS_API_KEY`, `TOGETHER_API_KEY`, plus tracing flags/endpoints (`LANGCHAIN_TRACING_V2`, `LANGSMITH_TRACING`, `LANGCHAIN_ENDPOINT`, `LANGSMITH_ENDPOINT`, `LANGGRAPH_API_URL`, `LANGCHAIN_PROJECT`). Worker `TestM162WorkloadEnvOmitsProviderKey` only plants `OPENAI_API_KEY`/`OPENROUTER_API_KEY`. Advisors: Grok 4.6, Kimi K3, DeepSeek V4.1 Flash, gpt-5.6-luna.

- HIGH SC3 harness `internal/harness/m16_2_langgraph_adv_test.go:217` / `:246` `TestADV_M16_2_SC3_LangGraphDirectProviderAndLangSmithHostDenied` — rewritten worker env still names LangSmith/LangGraph-cloud hosts; `pydanticDirectHostDenied` / `loopbackDeniedProviderHosts` miss `api.smith.langchain.com`, `api.langchain.com`, `api.x.ai`, `inference-api.nousresearch.com`, `api.fireworks.ai`, `api.together.xyz`, IPv4/IPv6 literals, and suffix `api.openai.com.attacker.invalid`. `AGENTPAAS_EGRESS_DENY` is unread; deny list unused by `serveHTTP`. Worker `TestM162DirectProviderHostDenied` only checks openai/openrouter/anthropic/azure env substrings. Advisors: Grok 4.6, Kimi K3, DeepSeek V4.1 Flash, gpt-5.6-luna.

- MEDIUM SC3 harness `internal/harness/m16_2_langgraph_adv_test.go:294` `TestADV_M16_2_SC3_TrailingDotCaseAndIPBypassMustDeny` — trailing-dot / mixed-case LangSmith and x.ai hosts not denied. Advisors: Grok 4.6, gpt-5.6-luna.

- MEDIUM SC6 harness `internal/harness/m16_2_langgraph_adv_test.go:389` / `:430` / `:443` `TestADV_M16_2_SC6_LangGraphAuditShapeMatchesWeatherAgentReference` — ChatOpenAI `GET /v1/models` emits `egress_loopback_models` (not in weather-agent reference event set) and no additional weather-class `llm_result`/`egress_allowed|denied`. Worker `TestM162LangGraphAuditExportShape` treats models GET as success and only requires `llm_result` plus some egress event. Advisors: Grok 4.6, Kimi K3, DeepSeek V4.1 Flash, gpt-5.6-luna.

- HIGH SC3 pack `internal/pack/m16_2_langgraph_adv_test.go:156` / `:163` `TestADV_M16_2_SC3_LangGraphPolicyDefaultDenyAndAutoDeclare` — `ensureLLMProviderEgress` stamps `openrouter.ai` onto lock egress; `ValidateLLMEgress` returns nil while `policy.yaml` omits the provider domain. Worker `TestM162LangGraphBypassDenied` only greps `policy.yaml` text. Advisors: Grok 4.6, Kimi K3, gpt-5.6-luna.

- HIGH SC4 pack `internal/pack/m16_2_langgraph_adv_test.go:193` `TestADV_M16_2_SC4_PackRejectsUnderscoreAndPostgresRedisAliases` — `ValidateLangGraphLibraryDeps` accepts PEP 503 underscore aliases `langgraph_server`, `langgraph_api`, `langgraph_checkpoint`. Worker only greps hyphenated names. Advisors: Grok 4.6, gpt-5.6-luna.

- HIGH SC4 pack `internal/pack/m16_2_langgraph_adv_test.go:227` `TestADV_M16_2_SC4_PackRejectsPyprojectExtrasCliAndPoetry` — pack accepts `langgraph-cli`, `langgraph[server]`, `langgraph[checkpoint]`, `poetry.lock` `langgraph-cli`, `Pipfile` `langgraph-server`, `setup.py` `langgraph-checkpoint-postgres`. Worker scans only `requirements.txt`/`pyproject.toml`/`uv.lock`/`poetry.lock` for four literal strings and never extras/cli/Pipfile/setup.py. Advisors: Grok 4.6, Kimi K3, DeepSeek V4.1 Flash, gpt-5.6-luna.

- HIGH SC4 pack `internal/pack/m16_2_langgraph_adv_test.go:243` `TestADV_M16_2_SC4_PackRejectsCheckpointImportInEntrypoint` — pack accepts `from langgraph.checkpoint.memory import MemorySaver` + `compile(checkpointer=MemorySaver())` with only library `langgraph==0.2.28`. SC4/D150 is an image/checkpointer claim, not a requirements substring. Worker never scans `main.py` at pack time. Advisors: Grok 4.6, Kimi K3, DeepSeek V4.1 Flash, gpt-5.6-luna.

- MEDIUM IV pack `internal/pack/m16_2_langgraph_adv_test.go:335` `TestADV_M16_2_IV_UnpinnedOpenAISupplyChain` — golden `openai>=1.0` unpinned (host/SDK default drift). Advisors: Grok 4.6, Kimi K3.

- MEDIUM IV pack `internal/pack/m16_2_langgraph_adv_test.go:354` `TestADV_M16_2_IV_CredentialDeclaredButPolicyCredentialsEmpty` — `agent.yaml llm.credential: openrouter-key` with `policy.yaml credentials: []`. Advisors: Grok 4.6, Kimi K3.

## RECS

- Extend `isWorkloadOpenAIEnv` to strip LangGraph/LangChain/LangSmith/Fireworks/Together keys, tracing flags, and telemetry endpoints (`LANGCHAIN_*`, `LANGSMITH_*`, `LANGGRAPH_*`, `FIREWORKS_API_KEY`, `TOGETHER_API_KEY`). Dummy `OPENAI_API_KEY` stays.
- Make `AGENTPAAS_EGRESS_DENY` / `loopbackDeniedProviderHosts` a pre-dial matcher used on the HTTP path, not a list-length flag. Cover LangSmith/LangGraph-cloud, x.ai, nous, fireworks, together, trailing-dot, case, IP literals, and suffix bypass.
- Stop `ensureLLMProviderEgress` from punching SC3 default-deny on the LangGraph golden. `ValidateLLMEgress` must error when policy omits the provider domain instead of auto-declaring it onto lock egress.
- Normalize PEP 503 names (`-`/`_`) before SC4 match. Reject `langgraph-cli`, extras `langgraph[server]`/`langgraph[checkpoint]`, postgres/redis/aiosqlite checkpointers, Pipfile/setup.py, and entrypoint `langgraph.checkpoint` / `MemorySaver` / `checkpointer=`.
- Audit ChatOpenAI `GET /v1/models` as weather-class `egress_allowed`/`egress_denied` (or omit it from export) — do not introduce `egress_loopback_models` as a LangGraph-only event type.
- Pin `openai==…` in the golden. Bind `llm.credential` in `policy.yaml credentials`.

Do not apply these this T. ADV stays RED.

## CONFIRMED_SAFE

- Worker `TestM162WorkloadEnvOmitsProviderKey`: planted `OPENAI_API_KEY`/`OPENROUTER_API_KEY` stripped; dummy loopback key + `OPENAI_BASE_URL` 127.0.0.1 rewritten. Does not cover LangChain/LangSmith/LangGraph keys.
- Worker `TestM162DirectProviderHostDenied`: env no longer contains `api.openai.com`/`openrouter.ai`/`api.anthropic.com`/`openai.azure.com`; `pydanticDirectHostDenied` true for those four. Does not cover LangSmith/x.ai/IP/suffix.
- Worker `TestM162LangGraphAuditExportShape`: loopback chat emits `llm_result` with `provider`/`model` and an egress event with `destination`/`method`/`decision`; gateway sidecar and dummy key absent from audit JSON. Does not require weather-class models GET or forbid `egress_loopback_models`.
- Worker `TestM162PackRejectsLanggraphServer` / `TestM162PackRejectsCheckpointer`: hyphenated `langgraph-server`/`langgraph-api`/`langgraph.server`/`langgraph-checkpoint`/`langgraph-checkpoint-sqlite` and `uv.lock` checkpoint name rejected. Underscore aliases, extras, cli, Pipfile, setup.py, MemorySaver entrypoint not covered.
- Worker `TestM162LangGraphBypassDenied`: `policy.yaml` text contains `wttr.in` and does not contain openai/openrouter/anthropic/azure host strings. Does not call `ensureLLMProviderEgress`.
- Pack ADV `TestADV_M16_2_SC1_LangGraphGoldenArtifactsOmitProviderSecrets` held: golden `main.py`/`agent.yaml`/`policy.yaml`/`requirements.txt` have no live-key shape; `llm.credential` is a name.
- Pack ADV `TestADV_M16_2_SC6_LangGraphPackRecordsSameLLMEgressContractAsWeather` held: provider/model match weather analog; `wttr.in` allowed; `openrouter.ai` not in langgraph `policy.yaml`.
- Pack ADV `TestADV_M16_2_IV_LangSmithTelemetryHostsNotInPolicy` held: policy does not allow smith/langchain hosts; golden `main.py` does not enable langsmith tracing.
- Pack ADV SC4 hyphenated `langgraph-checkpoint-postgres`/`-redis`/`-aiosqlite` in `requirements.txt` already rejected by substring `langgraph-checkpoint` (underscore aliases still accepted).
- No live provider dial in ADV. Synthetic canaries only. `demo/langgraph-weather-agent` and `TestM162*` untouched.

## INDEPENDENT_VECTORS

Orch named SC1/SC3/SC4/SC6 and analog surfaces. These are additional LangGraph-block vectors invented this T (not orch-listed, not worker TestM162*):

1. Grok 4.6 — LangSmith/LangChain tracing env (`LANGCHAIN_TRACING_V2`, `LANGSMITH_ENDPOINT`) as a prompt+key exfil channel independent of `OPENAI_API_KEY` scrub. Attacked: BREAK (SC1).
2. Grok 4.6 — PEP 503 underscore `langgraph_server` / `langgraph_checkpoint` vs hyphen deny list. Attacked: BREAK (SC4).
3. Grok 4.6 — `egress_loopback_models` as a LangGraph-only audit class vs weather `llm_result`/`egress_*`. Attacked: BREAK (SC6).
4. Kimi K3 — suffix-match bypass `api.openai.com.attacker.invalid` against `HasSuffix` host checks. Attacked: BREAK (SC3 harness deny list).
5. Kimi K3 — encoded/canary key in graph-state echo (declared; not a separate RED file this T because env inheritance already proved the secret is in the process). Held as follow-on: workload still inherits the raw env so a node can copy it into `WeatherState`.
6. Kimi K3 — `policy.yaml credentials: []` with declared `llm.credential`. Attacked: BREAK (IV).
7. DeepSeek V4.1 Flash — IPv4/IPv6 literal pre-dial (`104.18.0.1`, `2606:4700::1`) vs hostname blocklist. Attacked: BREAK (SC3).
8. DeepSeek V4.1 Flash — core `langgraph.checkpoint.memory.MemorySaver` import with no extra package. Attacked: BREAK (SC4).
9. DeepSeek V4.1 Flash — denied-path audit shape (models GET not weather-class). Attacked: BREAK (SC6).
10. gpt-5.6-luna — `langgraph[server]` / `langgraph[checkpoint]` extras and `langgraph-cli` in pyproject/poetry/Pipfile/setup.py. Attacked: BREAK (SC4).
11. gpt-5.6-luna — `ensureLLMProviderEgress` auto-declare punching default-deny on the packed lock (worker only grepped policy text). Attacked: BREAK (SC3 pack).
12. gpt-5.6-luna — unpinned `openai>=1.0` supply-chain / SDK default-host drift. Attacked: BREAK (IV).

Not implemented this T (still independent, for round 2): DoH-then-IP connect, checkpoint sqlite under `/tmp`, CompiledGraph pickle with bound key, sitecustomize/`.pth` injecting server extra, HTTP redirect from `wttr.in` to provider host.

## ADVISOR_SCORECARD

- Grok 4.6 (xai-oauth high) — unique HIGH: LangSmith env inheritance (SC1), underscore PEP 503 aliases (SC4), `egress_loopback_models` shape (SC6). unique MEDIUM: trailing-dot LangSmith/x.ai (SC3). unique IVs: tracing-env exfil, underscore aliases, models-event class. Verdict: BREAK.

- Kimi K3 (Nous high) — unique HIGH: suffix-bypass host (SC3). unique MEDIUM: empty policy credentials (IV). unique IVs: suffix bypass, graph-state echo follow-on, credentials-empty. Verdict: BREAK.

- DeepSeek V4.1 Flash (Nous high) — unique HIGH: IP-literal deny miss (SC3), MemorySaver core import (SC4). unique MEDIUM: none beyond shared SC6. unique IVs: IP literals, in-core checkpointer, denied-path audit. Verdict: BREAK.

- gpt-5.6-luna (openai-codex max) — unique HIGH: extras/cli/unscanned manifests (SC4), auto-declare lock punch (SC3 pack). unique MEDIUM: unpinned openai (IV). unique IVs: extras/cli/Pipfile/setup.py, auto-declare, openai pin. Verdict: BREAK.

Mix unchanged (KEEP ALL FOUR). No hermes. No product patch this T.
