# Block 33 — AgentPaaS-Container MCP Services

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Identity-source note (Fix 3, 2026-07-19):** B31 is reduced to a registry
read API + promotion bit. B33 consumes the promoted flag and `registry
show` for MCP service binding validation; the runtime-directory concept is
the B26 deployment/store health view, not a separate catalog directory.
Exact service digest pinning comes from B26 admission.
**Target release:** v0.4.0
**Depends on:** B32 complete; `make block32-gate` green
**Must complete before:** B34 and B35
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B33 proves that one AgentPaaS agent can call an approved MCP tool served by a
different AgentPaaS container without exposing either container directly or
using Hermes as a proxy.

At block completion:

- An AgentPaaS package can declare `kind: mcp_service` and a bounded set of MCP
  tools.
- A client agent declares a logical service binding and exact allowed tools.
- AgentPaaS starts the service in its own governed container and lease.
- The caller, caller gateway, service, and service gateway remain separately
  identifiable and cleanly reclaimable.
- The B31 catalog maps a logical service/capability to an exact approved
  package, while its runtime directory maps the selected version to trusted
  endpoint state. Python receives no raw container IP, Docker/Kubernetes DNS
  name, substrate address, or capability token.
- Calls use a workflow-scoped internal network and a per-binding unguessable
  capability enforced and stripped by trusted components.
- Caller and service policies are intersected. Caller credentials, egress,
  model routes, files, and other authority are never inherited by the service.
- MCP initialize, tool listing, tool calls, cancellation, and progress
  notifications conform to the selected protocol version. Buffered tool
  results remain valid; B29 ordered progress events are used when a service
  advertises streaming/progress support.
- Request/response size, timeout, cancellation, lease, tool allowlist, and
  concurrency bounds are enforced.
- Every call records caller run/attempt, service run/attempt, tool, input/output
  digests, status, timing, and policy evidence without raw sensitive bodies.
- Calls use the B32 two-sided communication edge, durable task/event state, and
  artifact broker; service completion never depends on endpoint polling.
- The current production-wiring gap and synthetic harness success fallback are
  removed for managed services. If routing is unavailable, the call fails
  closed.
- Service stop/recreate is idempotent so B39 can reach a fully `PAUSED` state
  with no live service capability and later resume against the same logical
  binding with a fresh lease/capability.

B33 does not implement sequential pipelines, parent/child agents, generic
third-party MCP packaging, arbitrary host MCP processes, public ingress, MCP
OAuth, or an MCP marketplace.

## Current baseline and reuse boundary

The repository already contains useful but incomplete pieces:

- `policy.MCPServer` and strict validation.
- `internal/mcpmanager.Manager`, `Lifecycle`, and `Router`.
- Stdio and HTTP JSON-RPC helpers.
- `agent.mcp(server_id, tool, input)`.
- Audit helpers and tool allowlists.

The production daemon does not construct and install the router in the
harness, the HTTP lifecycle sidecar does not prove a real service entrypoint,
the stdio response timeout is fixed at five seconds, and the harness can
return a synthetic `{ok: true}` result when no router exists.

B33 must evolve these pieces rather than create a second unrelated MCP stack,
but it must not preserve mock behavior as production functionality.

## Locked service and client contracts

### Service package

`agent.yaml` uses the B26 additive form:

```yaml
name: feedback-tools
kind: mcp_service
mcp_service:
  transport: streamable_http
  tools:
    - lookup_feedback
    - list_accounts
  max_concurrency: 4
```

The service’s `policy.yaml` declares its own credentials, outbound authority,
resource limits, active-time/lease behavior, and caller binding rules.

The Python SDK supports a minimal generated-service pattern:

```python
from agentpaas_sdk import agent

@agent.mcp_tool("lookup_feedback")
def lookup_feedback(arguments):
    return {"items": []}
```

Tool names are declared in signed configuration and registered in code. v0.4
requires exact set equality: every declared tool must register and every
registered tool must be declared; there are no optional or discovery-added
tools. Any mismatch fails readiness.

### Client binding

The workflow/client declaration names a logical binding:

```yaml
mcp_bindings:
  - id: feedback
    service: feedback-tools@1.0.0
    allowed_tools: [lookup_feedback]
```

Worker code continues to use:

```python
agent.mcp("feedback", "lookup_feedback", {"account_id": "a-1"})
```

The worker cannot override service package, version, endpoint, network,
credential, timeout above policy, capability, or caller identity.
The immutable workflow deployment resolves the service reference to one exact
installed package version/digest before admission; readiness and restart never
re-resolve an alias.

### Protocol subset

v0.4 supports the required JSON-RPC subset over MCP Streamable
HTTP:

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`
- bounded progress notifications for a tool call when declared by the service

The service uses bounded request/response bodies. B29/B32 may transport ordered
progress events, but no server-initiated sampling, roots, elicitation, arbitrary
reverse callback, or second source of task truth is added in v0.4. Unknown
methods fail explicitly.

Protocol version support is pinned and reported. Do not silently accept an
unknown future version.

## Locked network and identity model

1. Every caller run keeps its existing private agent network.
2. A workflow-scoped internal service network is created with no external
   route.
3. The trusted caller gateway/service proxy is attached to the caller private
   network and service network. The untrusted caller Python process is not
   given a directly reachable service endpoint.
4. The MCP service container or its trusted ingress proxy is attached to the
   service network. Its egress remains governed through its own gateway and
   policy.
5. The registry assigns a random network alias and per-binding capability.
   Neither is accepted from workflow/user input.
6. The capability is delivered only to trusted harness/gateway state, required
   on the exact MCP route, and stripped before Python/tool dispatch.
7. Generic `agent.http()`, raw proxied HTTP, another run, or a guessed alias
   cannot reach the service route without the binding capability.
8. Service identity is checked against workflow ID, service binding, package
   digest, run ID, attempt/lease, and registered tool envelope.
9. Revoking either caller or service lease immediately blocks new calls and
   cancels in-flight calls within the B30 operation deadline.

If the current gateway cannot enforce an internal exact-route capability,
implement one narrowly scoped trusted service proxy. Do not attach all agent
containers to one shared network as a shortcut.

## Locked authority and data rules

- Effective tool authority is the intersection of:
  - signed workflow binding;
  - client policy allowlist;
  - service package declared tools;
  - service policy caller rules;
  - active caller and service leases.
- The caller sends JSON object arguments only.
- Default maximum request is 256 KiB and response is 1 MiB; both are
  configurable only downward in the signed binding for v0.4.
- Raw arguments/results are visible to caller/service code as required for the
  call but are not copied into ordinary audit, route decisions, checkpoints,
  or logs. Evidence uses canonical digests and bounded redacted metadata.
- Caller credential values, environment, artifact root, model-route
  capability, and lease token are never forwarded.
- Service credentials are resolved from the service’s own policy and local
  credential mapping.
- The service cannot issue a model/HTTP/MCP call after its lease is fenced.
- A protocol success is operational success, not verification of the tool’s
  factual or business result.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Freeze current MCP behavior and close synthetic-success claims | B32 | production-wiring negative and current conformance fixtures recorded |
| 2 | T02 Implement service package/SDK/runner contract | T01, B26 | declared tools register, readiness validates, undeclared tools fail |
| 3 | T03 Implement durable service registry and lifecycle | T02 | separate service run/container/lease survives client disconnect and reconciles |
| 4 | T04 Build workflow-scoped network and service capability path | T03 | raw east-west and generic HTTP bypass tests fail closed |
| 5 | T05 Wire real MCP protocol router end to end | T02–T04 | initialize/list/call reaches actual service code; no synthetic response |
| 6 | T06 Enforce time, size, concurrency, cancellation, and lease controls | T05 | boundary/race/flood matrix passes |
| 7 | T07 Persist evidence, health, restart, and cleanup | T03–T06 | durable reports and reconciliation contain no secret/body leakage |
| 8 | T08 Cross-container reference proof | T01–T07 | client and service in distinct containers complete approved tool call |
| 9 | T09 Block gate and adversary review | T01–T08 | `make block33-gate` passes |

## T01 — Characterize and close the current MCP gap

**Note (2026-07-19 audit, risk R5):** the production fail-closed half of this
task moved to B30 T00. By the time B33 executes, the no-router path already
returns a typed `agentpaas_mcp_service_not_enabled` error in production, with
the synthetic response confined to explicit test mode. What remains here is
characterization, reuse/replace decisions, and wiring the real router (T05).

### Goal

Prevent tests and docs from treating allowlist validation or a mock result as
a working production MCP service.

### Required work

1. Add a production-path test proving no code currently calls
   `SetRouter(...)` outside tests.
2. Add a test proving the no-router harness branch returns a synthetic result.
3. Mark these as baseline failures for managed service mode.
4. Freeze current policy, manager, router, lifecycle, audit, and SDK tests.
5. Inventory fixed five-second stdio timeout and any HTTP client timeout.
6. Define reuse/replace decisions for each current component in the handoff.

### Tests to write first

- Managed service call with no real router must fail.
- Current synthetic payload is detected and forbidden.
- Existing external HTTP/stdio MCP compatibility fixtures remain recorded.
- Tool allowlist bypass and body redaction tests remain green.

### Exit gate

The team has an executable distinction between MCP schema/mock support and a
real AgentPaaS-container service call.

## T02 — Implement the MCP service package and SDK runner

### Goal

Turn an AgentPaaS-generated service package into a protocol-conformant tool
server without exposing a Python network listener.

### Required work

1. Add `Agent.mcp_tool(name)` registration with strict safe tool IDs.
2. Add a service runner mode selected only by signed `kind: mcp_service`.
3. Keep the trusted harness as the network/protocol endpoint; dispatch bounded
   calls to Python over the existing private worker protocol.
4. Validate code registrations exactly equal the signed service declaration.
   Missing and extra registrations both fail import/readiness; v0.4 has no
   optional-tool marker or discovery subset.
5. Validate arguments are JSON objects and results are JSON-serializable,
   bounded, and free of reserved control fields.
6. Serialize each tool initially or enforce declared maximum concurrency with
   deterministic bounds and race tests.
7. Map Python exception to bounded MCP error without traceback/secret leakage.
8. Returning from one tool call does not terminate the service runner.
9. Service shutdown stops accepting calls, drains/cancels in-flight work
   within lease/active-time controls, and writes terminal evidence.

### Tests to write first

- Register/list/call one and multiple tools.
- Duplicate/unsafe tool names plus both missing-declared and extra-registered
  tool-set mismatches.
- Invalid args/result, oversized body, deep JSON, control-field smuggling.
- Tool exception and secret sentinel redaction.
- Concurrency bound and shutdown race.
- Worker cannot read capability or raw network endpoint.

### Exit gate

The service harness can answer real MCP requests by dispatching approved tools
to Python, and no Python HTTP server is required.

## T03 — Implement the durable service registry and lifecycle

### Goal

Manage a service as a first-class workflow member with its own run, lease, and
container rather than an anonymous sidecar.

### Required work

1. Implement the B31 catalog/runtime-directory binding with generation/CAS
   updates; do not create a second service registry.
2. Service states:
   - `DECLARED`
   - `STARTING`
   - `READY`
   - `UNHEALTHY`
   - `FENCED`
   - `STOPPING`
   - `STOPPED`
   - `FAILED`
3. Resolve exact installed service package/version/digest before resources.
4. Create service run/attempt/lease and labels containing workflow and service
   IDs.
5. Start its own agent/service container and egress gateway/resources.
6. Readiness requires:
   - harness ready;
   - protocol initialize response;
   - signed tool envelope match;
   - active lease;
   - registry generation match.
7. Define service lifetime: workflow-scoped by default; stop after no remaining
   bound clients or workflow terminal state.
8. Reconcile daemon restart by discovering labelled resources, revoking
   ambiguous leases, and never trusting stale registry endpoint data.
9. Prevent two active containers from claiming the same service generation.

### Tests to write first

- Full state transition matrix.
- Duplicate start idempotency and concurrent start race.
- Package/tool/digest mismatch.
- Service crash before/after readiness.
- Restart with ready/stale/orphan service.
- Client disconnect does not accidentally stop a still-bound service.
- Workflow terminal cleanup.

### Exit gate

The registry is the source of truth and every active service endpoint maps to
one live, leased, digest-matched container.

## T04 — Implement the protected internal service network

### Goal

Permit the exact approved MCP route without granting arbitrary east-west
container connectivity.

### Required work

1. Extend runtime labels and network lifecycle for workflow service networks.
2. Create the internal network before service/client gateway attachment.
3. Add required attach/detach support to `RuntimeDriver` if current create-time
   attachment is insufficient; fake and Docker drivers must remain in parity.
4. Generate random network aliases and binding capabilities in trusted code.
5. Compile exact service path, method, workflow, binding, and capability
   checks into gateway/proxy configuration.
6. Strip capability and internal routing headers before service Python
   dispatch and before any audit/error rendering.
7. Keep caller Python off the service network or otherwise prove raw socket
   bypass is impossible.
8. Deny service access from another workflow, another client, generic HTTP,
   wrong tool path, wrong method, guessed alias, or stale capability.
9. Remove network only after all service/client gateway attachments are
   stopped; reconcile orphan network labels after crash.

### Tests to write first

- Approved route succeeds.
- Caller raw socket/direct HTTP denied.
- `agent.http()` and `http_with_credential()` denied to service route.
- Wrong/missing/replayed/cross-workflow capability denied.
- Capability stripped before service.
- Alias/endpoint/capability absent from Python env, result, logs, audit,
  checkpoint, artifact, and errors.
- Network attach/detach/start/stop crash matrix.

### Exit gate

Only the trusted MCP path can reach the service route, and the internal network
does not become a general container mesh.

## T05 — Wire the real MCP router end to end

### Goal

Connect `agent.mcp()` to the actual registered service and protocol response.

### Required work

1. Construct and install the real `mcpmanager.Router` in production durable
   harness setup.
2. Add an AgentPaaS-service transport resolver backed by the registry; do not
   accept raw endpoint from payload.
3. Perform initialize/version negotiation and signed tool-list comparison at
   readiness.
4. Route `tools/call` with canonical request ID and active caller/service
   lease metadata.
5. Use B30 effective operation deadline instead of the fixed five-second
   timeout.
6. Reject synthetic success whenever a binding is managed. Remove the fallback
   or restrict it to explicit test doubles that cannot enter production build.
7. Preserve external HTTP/stdio MCP only where existing policy supports it;
   do not silently reinterpret those as AgentPaaS services.
8. Map protocol, service, lease, policy, timeout, cancellation, and crash
   errors to stable typed failures.
9. Audit one call once; avoid duplicate router+harness events.

### Tests to write first

- SDK -> caller harness -> gateway/proxy -> service harness -> Python tool ->
  result.
- Initialize/list/call protocol goldens.
- Unknown version/method/request ID/tool.
- Real tool value proves no synthetic response.
- Router unavailable, registry stale, and service crash fail closed.
- External stdio/HTTP compatibility tests.
- Exact one audit record/correlation chain.

### Exit gate

A managed MCP call can be traced to real service code in another container and
cannot succeed without the registry/router path.

## T06 — Enforce operation bounds and lease cancellation

### Goal

Prevent a slow, malicious, or overloaded service from escaping workflow
limits.

### Required work

1. Compute effective call deadline from binding timeout, caller lease, service
   lease, and workflow active-time remaining.
2. Enforce request and response byte/depth bounds before dispatch/return.
3. Enforce per-service and per-caller concurrency.
4. Use bounded queues; reject overload rather than accumulating unbounded
   goroutines/memory.
5. Cancel in-flight call when caller or service lease is revoked.
6. Ensure a service heartbeat alone cannot extend caller lease or workflow
   active-time ceiling.
7. Record timeout/overload/cancel reason and timing without raw bodies.
8. Define no automatic cross-service fallback in v0.4.

### Tests to write first

- Minus/at/plus-one request, response, deadline, and concurrency bounds.
- Slow tool below timeout succeeds; over timeout cancels.
- Caller/service/workflow active-time precedence.
- Queue flood and disconnect storm.
- Lease revoke during call; late result discarded.
- Memory/goroutine/file descriptor bounded under repeated calls.

### Exit gate

No MCP call outlives either active lease or the workflow envelope, and
overload remains bounded.

## T07 — Persist evidence, health, restart, and cleanup

### Goal

Make cross-container calls diagnosable and crash-safe.

### Required work

1. Persist sanitized service lifecycle and call records in workflow/run
   ledgers.
2. Include caller/service run, attempt and lease IDs; binding/tool; input and
   output digests; protocol version; timing; status/reason; evidence refs.
3. Expose service readiness/health and recent bounded failures through status,
   summarize, timeline, and operator schema.
4. Reconcile calls in flight during daemon restart as unknown/cancelled; do
   not fabricate tool completion or reissue automatically.
5. Clean service containers, gateways, network attachments, network, control
   capabilities, and temp credentials idempotently.
6. Preserve service audit order relative to caller progress/checkpoint.

### Tests to write first

- Lifecycle/call report fixtures.
- Restart before dispatch, during tool, after result before commit.
- Late result after restart/fence.
- Duplicate cleanup and orphan discovery.
- Raw arguments/results/secrets/capabilities absent from evidence.
- Timeline ordering and bounded event volume.

### Exit gate

After any fault, AgentPaaS can state whether the call committed, failed, or is
unknown without replaying it or leaking its body.

## T08 — Cross-container MCP reference proof

### Required topology

- One client agent container and gateway.
- One MCP service agent container and gateway/proxy.
- Distinct run and attempt IDs and distinct policies.
- One workflow-scoped service network.
- No host port/public ingress.

### Reference scenario

1. Hermes authors, tests, packages, and deploys the immutable client/service
   workflow; the snapshot pins both package digests. Hermes exits before
   invocation.
2. Invoke the deployment through public CLI and authenticated API paths.
3. Service exposes `lookup_feedback` using deterministic fixture data.
4. Client calls it through `agent.mcp("feedback", ...)`.
5. Client uses the returned structured value in a later local/model phase.
6. Client writes an artifact and returns success.
7. Evidence proves exact service package, tool, call digest, result digest,
   container separation, lease state, and cleanup.
8. Run the shared B26 admission-conformance suite for this MCP-client topology:
   exact/alias pinning, same caller/key replay, changed intent conflict, alias
   movement, inactive future admission, default-one overlap/no queue,
   configured-safe concurrency, and paused-slot resume reacquisition.

### Required negative companion

- Undeclared tool.
- Undeclared service.
- Generic HTTP bypass.
- Cross-workflow caller.
- Service crash and timeout.
- Lease revoke during tool.

### Exit gate

The positive and all negatives pass three consecutive clean Docker runs with
zero orphan resources; the complete B26 MCP-topology admission matrix passes
with Hermes absent before each invocation.

## T09 — Block gate and adversary review

### Required `make block33-gate`

```text
make block32-gate
go test ./internal/mcpmanager/... ./internal/harness/... ./internal/daemon/... -count=1 -race
go test ./internal/runtime/... ./internal/routedrun/... ./internal/policy/... ./internal/pack/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

### Required adversary matrix

- Synthetic no-router success reaches production.
- Raw service endpoint/IP/DNS/capability supplied by worker.
- Generic HTTP/raw socket bypass.
- Cross-workflow or stale capability reuse.
- Service registers undeclared tool.
- Caller invokes undeclared tool.
- Caller credential or authority inherited by service.
- Capability appears in Python, logs, audit, checkpoint, artifact, or error.
- Oversized/deep request or response.
- Fixed five-second timeout remains on managed path.
- Queue/concurrency exhaustion.
- Late result accepted after either lease revoked.
- Daemon restart replays a tool call.
- Service/network/container orphan.

### Block success gate

B33 is complete only when:

1. `make block33-gate` passes.
2. The production daemon installs a real managed-service router.
3. The managed path has no synthetic success fallback.
4. Client and service run in separate governed containers/policies/leases.
5. Only the exact capability-protected MCP path reaches the service.
6. MCP protocol, time, size, concurrency, cancellation, and lease bounds pass.
7. Restart never blindly replays an in-flight tool call.
8. Evidence is complete, bounded, and secret-free.
9. Exact service identity and the full B26 admission-conformance matrix pass
   through the public CLI/API with Hermes absent before start.
10. B34 is the next unblocked block.

## Handoff record required after every task

Append:

- Task/date/commit.
- Existing component reused or replaced.
- Schema/SDK/protocol decision.
- Files changed.
- Tests added first.
- Exact commands and PASS/FAIL output.
- Topology and capability evidence.
- Adversary/cleanup result.
- Compatibility impact.
- Open risks.
- Next task unblocked.

## Pitfalls

- An allowlist test is not a real MCP call.
- Never return a synthetic success when the router is absent.
- Do not put all agents on one shared network.
- Do not expose service IPs or capability tokens to Python.
- Do not inherit caller credentials or policy into the service.
- Do not use a fixed five-second timeout for long tools.
- Do not treat protocol success as semantic correctness.
- Do not broaden this block into a generic MCP marketplace or host-process
  integration.
