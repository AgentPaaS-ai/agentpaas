# Block 15 — End-to-End Manual Test Plan

Status: DRAFT. Supersedes the UC-01..UC-10 matrix in the execution plan for
founder testing — those use cases are security/policy-focused; this plan adds
the full product lifecycle flow a real user experiences.

## 0. Prerequisites (run once before any UC)

- [ ] `agentpaas doctor` passes on this machine
- [ ] `agentpaas daemon start` — daemon up, socket reachable
- [ ] `agentpaas daemon status` returns healthy
- [ ] Hermes profile has the agentpaas plugin enabled and `AGENTPAAS_SOCKET_PATH` set
- [ ] Docker/colima running (`DOCKER_HOST` set)
- [ ] A throwaway agent project dir to deploy into

## 1. Lifecycle Use Cases (founder-driven, manual)

These are the five end-to-end flows a real user hits. Each is a separate
session. Do not batch.

### LC-01: Activate AgentPaaS from a Hermes profile
- **Scenario**: User is in a Hermes session and asks to use the agentpaas
  plugin and skills.
- **Steps**:
  1. From a fresh Hermes turn, say "I want to use agentpaas."
  2. Hermes should load the agentpaas skill, confirm the plugin is available,
     and report daemon/binary status.
  3. If daemon is down, Hermes should offer to start it.
- **Pass**: Hermes recognizes the plugin, tools are callable, daemon health
  is surfaced without the user touching the CLI.
- **Feasibility**: YES now (plugin + SKILL.md exist, 18 tools). Depends on
  daemon already running and socket env var set.

### LC-02: Build an agent, offer secure packaging, deploy
- **Scenario**: User says "build an agent that does X." Hermes writes the
  agent code, then asks whether the user wants it securely packaged with
  AgentPaaS. If yes, it packages and deploys to the local machine.
- **Steps**:
  1. Ask Hermes to build a simple HTTP-calling agent (e.g. fetch a public API).
  2. Hermes generates agent code in a project dir.
  3. Hermes asks: "Package and deploy this as a governed agent with AgentPaaS?"
  4. User confirms.
  5. Hermes calls `agentpaas_pack` then `agentpaas_run`.
  6. `agentpaas_status` shows the run as active.
- **Pass**: Agent is packaged, deployed, running, and status is visible — all
  driven from Hermes, no raw CLI.
- **Feasibility**: PARTIAL. Pack+run exist as separate tools (no single
  "deploy" verb despite SKILL.md mentioning `/agentpaas deploy`). The
  confirm-then-deploy flow is a Hermes orchestration behavior Hermes must
  perform, not an AgentPaaS feature. **Blocker: `agent.llm()` returns a
  hardcoded fake string (B15-T01). Agents that need LLM reasoning will not
  actually reason. HTTP-only agents work.**

### LC-03: Set triggers to launch the agent
- **Scenario**: User wants to launch the agent via (a) an API call through
  Hermes, (b) a Hermes event (for testing), or (c) a cron job.
- **Steps**:
  1. (a) API: configure an API-key trigger and invoke the agent via HTTP POST
     to the trigger server.
  2. (b) Event: register an event subscription and publish a test event that
     launches the agent.
  3. (c) Cron: register a cron schedule (e.g. every 2 min) targeting the agent.
- **Pass**: All three mechanisms launch the agent and produce a new run with
  audit events.
- **Feasibility**: BACKEND BUILT, SURFACE MISSING.
  - Trigger server (`internal/trigger/`) fully implements REST `/v1/trigger/invoke`
    with API-key auth (R18), SSE streaming, EventBus publish/subscribe, and
    CronScheduler with 5-field cron. BUT:
  - No CLI subcommand to create/manage triggers or cron schedules.
  - No plugin tool for triggers, events, or cron.
  - Schedules are only configurable programmatically via `CronConfig.Schedules`.
  - You can curl `/v1/trigger/invoke` directly to test (a), but (b) and (c)
    have no user-facing surface. **This is a real gap — likely needs a B15
    task or a B15 sub-block before v0.1.0.**

### LC-04: Agent output available in Hermes
- **Scenario**: After the agent runs, the user wants to see its output in
  Hermes.
- **Steps**:
  1. After a run completes (or while running), ask Hermes for the run output.
  2. Hermes calls `agentpaas_summarize_run`, `agentpaas_logs`,
     `agentpaas_get_run_timeline`, and `agentpaas_audit_query`.
  3. User sees structured output: summary, logs, timeline events, audit trail.
- **Pass**: All four return structured JSON Hermes can render to the user.
  Audit trail is signed and queryable.
- **Feasibility**: YES now. All four tools exist and wrap CLI subcommands.

### LC-05: Redeploy on changes (kill, remove, repackage, launch)
- **Scenario**: User wants changes to the agent. The currently running agent
  is killed and removed, the new agent is packaged and launched.
- **Steps**:
  1. Agent is running (from LC-02 or LC-03).
  2. User requests a change to the agent prompt/code.
  3. Hermes calls `agentpaas_stop <run-id>` (stops + removes container).
  4. Hermes calls `agentpaas_pack` with the updated project.
  5. Hermes calls `agentpaas_run` with the new image.
  6. `agentpaas_status` shows only the new run; old run is gone.
  7. Audit trail shows both runs with distinct digests.
- **Pass**: Old run stops cleanly (no orphaned container/network — R12
  reconciler), new run starts, audit distinguishes the two by digest.
- **Feasibility**: PARTIAL. Stop+pack+run all exist, but there is no single
  "redeploy" command and the plugin does not auto-detect "agent already
  running, stop first." Hermes must orchestrate stop→pack→run explicitly.
  Works but clunky. Reconciler handles crash-recovery orphans, not
  intentional redeploy.

## 2. Security/Policy Use Cases (from execution plan, retained)

These remain valid and should still be run, but they are secondary to the
lifecycle flows above. See execution plan §15.2: UC-01 (weather/API demo),
UC-02 (secret-brokered SaaS), UC-03 (agentic repair loop), UC-04 (prompt-change
redeploy — overlaps LC-05), UC-05 (long-running mixed egress), UC-06
(clean-machine install), UC-07 (policy authoring), UC-08 (audit export +
verify on second machine), UC-09 (daemon lifecycle), UC-10 (Hermes integration
depth — overlaps LC-01/02/04).

## 3. Gaps Found During Planning (block v0.1.0)

| Gap | Use case | Severity | Where to fix |
|-----|----------|----------|--------------|
| `agent.llm()` returns fake string | LC-02 | BLOCKER | B15-T01 |
| No trigger/cron CLI or plugin surface | LC-03 | BLOCKER for LC-03 | New B15 task or B15 sub-block |
| No single "deploy"/"redeploy" verb | LC-02, LC-05 | Polish | Plugin/SKILL update |
| No "rotate" secret CLI | UC-02 | Minor | B15-T05 |
| No event-trigger registration path | LC-03(b) | BLOCKER for LC-03(b) | New B15 task |

## 3a. B15-T01 Design Gaps (must resolve before implementation)

B15-T01 (LLM provider integration) has three gaps between the founder's
vision and the current plan/code. These must be decided before coding starts.

### Gap 1: Interactive provider selection at design time
- **Vision**: Plugin ASKS the user which LLM to use, then Hermes writes the
  choice into agent.yaml.
- **Plan says**: "Hermes decides the right LLM to integrate based on the
  agent's purpose." Hermes decides, not asks.
- **Fix needed**: Add an interactive step to the plugin where Hermes asks
  the user (or proposes and confirms) the provider + model. Write the
  choice to agent.yaml `llm.provider` + `llm.model`.

### Gap 2: Pre-deployment validation ("test before baking in")
- **Vision**: Hermes should test the LLM path in isolation before deploying
  the agent — verify the API key works, the provider responds, the egress
  path resolves.
- **Plan says**: Verification is "agent.llm() returns a real response from
  the configured provider." This tests at RUNTIME, inside the deployed
  container. No pre-deployment dry-run.
- **Fix needed**: Add a `agentpaas secret test <name>` or
  `agentpaas doctor --llm <provider>` command that makes a trivial LLM
  call (e.g. "say OK") with the brokered credential, OUTSIDE the container,
  before pack/run. Fail fast with a clear error if the key is wrong, the
  provider is unreachable, or the egress policy doesn't allow the provider
  domain. Same pattern for any third-party API key.

### Gap 3: LLM routing architecture (RPC vs gateway-proxied egress)
- **Current code**: `agent.llm()` is a harness RPC method
  (internal/harness/rpc_server.go:168) that returns a hardcoded fake
  string. It does NOT go through the gateway.
- **Plan says**: "The gateway sidecar resolves the provider credential from
  Keychain and proxies the LLM call, attaching the API key."
- **Conflict**: The plan describes gateway-proxied egress, but the code has
  LLM as a direct harness RPC. These are different paths.
- **Decision needed**:
  - **Option A (RPC)**: Keep LLM as harness RPC. Add real provider calls
    inside the harness (resolve credential from Keychain broker, make HTTP
    call to provider). Simpler. LLM calls get budget/audit via the harness
    but do NOT go through the gateway egress audit/policy path.
  - **Option B (Unified egress)**: Route LLM through the gateway as
    credentialed HTTP egress, same as any third-party API. `agent.llm()`
    becomes sugar over `agent.http_with_credential` to the provider API.
    Full audit, policy enforcement, credential broker — all reused. LLM is
    just another egress destination. More aligned with "all pathways baked
    in before deployment."
- **Recommendation**: Option B. It unifies LLM and third-party API access
  under one security model. The secrets broker, gateway proxy, audit chain,
  and policy engine already handle credentialed HTTP egress. LLM should not
  be special.

### Gap 4: Generic third-party credential pre-deployment validation
- **Vision**: If the agent needs to access any third-party service (x, SaaS
  APIs, etc.), all keys and pathways must be validated before deployment.
- **Current state**: The secrets broker (internal/secrets/broker.go) handles
  credential injection for HTTP egress. Policy.yaml supports credentials[]
  with brokered access. B15-T05 adds secret add/list/remove/rotate CLI.
- **Missing**: No pre-deployment "validate all credential paths resolve"
  step. You find out a key is broken at runtime, inside the container.
- **Fix needed**: Extend the Gap 2 solution to ALL credential types, not
  just LLM. `agentpaas secret test <name>` should work for any brokered
  credential — make a trivial authenticated call to the target service,
  verify it works, before deploying the agent.

### Summary: what B15-T01 should deliver (revised)

1. Interactive provider selection in the plugin (ask user, write to agent.yaml)
2. `agentpaas secret test <name>` — pre-deployment credential validation
   for LLM AND third-party API keys (makes a trivial authenticated call,
   verifies the path before pack/run)
3. LLM routing decision: Option B (unified egress) — route LLM calls
   through the gateway as credentialed HTTP, reusing the existing broker +
   audit + policy infrastructure. Deprecate the special agent.llm() RPC
   path or make it thin sugar over http_with_credential.
4. agent.yaml schema: `llm.provider`, `llm.model`, and the credential
   binding (which Keychain secret to use for this provider)
5. Budget enforcement on LLM calls (token counting — already in harness,
   needs to apply to gateway-proxied calls too)
6. Audit events for LLM calls (provider, model, token count, cost,
   allowed/denied — already supported by the egress audit path)

## 4. Recommended Order

1. LC-01 (activation) — smoke test the plugin surface.
2. LC-04 (output) — verify observability tools before relying on them.
3. LC-02 (build+deploy) — HTTP-only agent (LLM is fake). Confirms pack+run.
4. LC-05 (redeploy) — confirms stop+repackage+launch loop.
5. LC-03 (triggers) — only after trigger surface gap is closed, OR test
   (a) via raw curl to `/v1/trigger/invoke` as a stopgap.
6. Then the security UCs from the execution plan.

## 5. Definition of Done for Block 15

- All LC-01..LC-05 pass (or gaps filed as issues with clear owners).
- All UC-01..UC-10 from the execution plan assessed (pass or filed).
- Every gap in §3 either fixed or explicitly deferred to P2 with rationale.
- Clean-machine install (UC-06) completed on a fresh account in <15 min
  using only README.
