# AgentPaaS Demo Matrix — P1 Differentiation

Three demo scenarios that showcase AgentPaaS's core value proposition:
turning AI-generated agent code into governed, audited, policy-controlled
workloads.

## Demo 1: Governed Weather/API Agent

**Wedge:** AI-generated agent attempts allowed API + denied exfil probe.
Dashboard shows policy denial and signed audit evidence.

**Agent:** `governed-weather/` — attempts a legitimate weather API call
and an exfiltration probe. On the internal-only network, both are denied
by network isolation. The audit chain records both as `egress_denied`
with distinct destinations.

**Run:**
```sh
# Bundle SDK into demo agent
cp -r ../../python/agentpaas_sdk governed-weather/python/

# Pack + run
agent pack demo/governed-weather
agent run governed-weather
# Wait for invoke, then:
agent stop <run-id>
agent audit query --run-id <run-id> --json
```

**Expected audit:** 2× `egress_denied` events (weather API + exfil probe).

## Demo 2: Secret-Brokered SaaS Action

**Wedge:** Agent uses a brokered credential through the harness. Secret
value is never visible to code or logs, but upstream fixture receives the
authorized request.

**Agent:** `secret-saas/` — calls `agent.http_with_credential()` with
credential_id `crm-api-key`. The harness matches the declared credential
and injects the Authorization header. An uncredentialed call is also
attempted (denied by harness: "credential not declared").

**Run:**
```sh
cp -r ../../python/agentpaas_sdk secret-saas/python/
agent pack demo/secret-saas
agent run secret-saas
```

**Expected audit:** `egress_denied` with reason "credential not declared"
for the uncredentialed call; `egress_denied` (network) for the credentialed
call (since the internal network blocks all external HTTP).

## Demo 3: Agentic Repair Loop

**Wedge:** Agent has a dependency/code defect and missing egress policy.
MCP `next_action` recommends fixes, proposes policy patches, and the
agent reruns with exported signed audit.

**Agent:** `repair-loop/` — exercises LLM calls (budget-tracked),
iteration recording, egress attempts (denied), and MCP tool calls
(undeclared → denied). The operator dashboard's `next_action` /
`explain_failure` tools can be used to diagnose and recommend fixes.

**Run:**
```sh
cp -r ../../python/agentpaas_sdk repair-loop/python/
agent pack demo/repair-loop
agent run repair-loop
```

**Expected audit:** LLM token usage, iteration records, `egress_denied`,
`mcp_denied` events.

## SDK Bundling

All demo agents require the AgentPaaS SDK bundled locally. From the
repo root:

```sh
for demo in demo/*/; do
  mkdir -p "$demo/python"
  cp -r python/agentpaas_sdk "$demo/python/"
done
```

The pack Dockerfile copies project files to `/app/`. The harness finds
the SDK via `pythonPackagePath()` which walks from cwd looking for
`python/agentpaas_sdk/`. Do NOT list `agentpaas_sdk` in requirements.txt
— the distroless base image has no pip.
