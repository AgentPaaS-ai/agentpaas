# Block 28 — Runtime Portability and Managed-PaaS Feasibility Gate

**Status:** COMPLETE — 2026-07-19. Substrate surface frozen per D67:
Kubernetes is the sole managed-substrate candidate, Cloudflare is rejected
(T05 will not be re-opened), and the optional service-mesh experiment is
cancelled. The Cloudflare references below describe the original approved
scope and are retained for historical record; D67 governs.

**Date:** 2026-07-18
**Target release:** v0.3.0 architecture gate; Docker remains the shipped runtime
**Depends on:** B26 and B27 complete; `make block27-gate` green
**Must complete before:** B29, B30, and every multi-container/routing block
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D34–D39, D54,
D55, D61, and D63–D65

## Outcome

B28 proves that the B26 deployment/run contracts and B27 progress/checkpoint/
artifact contracts are platform contracts rather than accidental Docker and
host-filesystem contracts. It does not ship Kubernetes or Cloudflare support.
It creates a bounded vertical proof and the replaceable ports required to keep
the local product, a future managed PaaS, and a customer-owned-cloud adapter on
one SDK/package/policy model.

At block completion:

- One immutable signed AgentPaaS package and manifest is admitted without
  source changes on the current Docker runtime, a local Kubernetes cluster,
  and a Cloudflare proof environment.
- Agent code and packages contain no Kubernetes YAML, Cloudflare Worker
  configuration, Docker Compose, pod address, container address, or provider-
  specific secret binding.
- Runtime, state, event, artifact, gateway, identity, secret, package, and
  metering ports have executable conformance tests.
- The Docker adapter remains the only supported v0.3 production adapter and
  passes the same contract used by both proofs.
- Kubernetes is evaluated first without a service mesh. An optional Istio
  ambient experiment measures mTLS/workload-identity value but cannot become
  an AgentPaaS product dependency or replace AgentPaaS semantic policy.
- Cloudflare is evaluated for arbitrary immutable agent images, durable
  lifecycle, tenant isolation, governed ingress/egress, credential invisibility,
  artifact persistence, event/wait behavior, metering, and economics.
- Two test tenants cannot list, invoke, signal, subscribe to, or read one
  another's deployments, tasks, events, artifacts, secrets, or usage.
- A signed comparison record chooses the first managed substrate or records
  exactly which feasibility fact is still missing. Convenience or local
  familiarity is not sufficient evidence.

B28 is deliberately a feasibility and architecture gate. It does not create a
production cluster, public cloud account hierarchy, billing product, public
registry, Terraform product, service-mesh dependency, or multi-cloud parity.

## Locked product boundary

AgentPaaS owns agent-domain state and authority:

- package/deployment identity and provenance;
- component capability, workflow/task, attempt, lease, and event state;
- signed communication policy, budgets, artifacts, credentials, audit, and
  metering attribution.

The substrate owns infrastructure mechanics:

- compute placement and sandbox start/stop;
- infrastructure networking and storage primitives;
- node lifecycle, image distribution, resource isolation, and autoscaling.

No substrate object becomes the public AgentPaaS API. Kubernetes Pods/Jobs,
Cloudflare Workers/Durable Objects/Containers, and Docker containers are
private adapter details.

## Required portable ports

Define small interfaces and contract fixtures for:

1. `WorkloadRuntime`: prepare, start, signal, fence, stop, inspect, and cleanup
   an exact image digest under a resource/activation policy.
2. `TransactionalStateStore`: compare-and-swap durable deployment, invocation,
   task, run, attempt, lease, and workflow state.
3. `EventStore`: append ordered durable events and subscribe after a cursor.
4. `ArtifactStore`: commit, authorize, stream/range-read, verify, retain, and
   delete scoped artifact objects without exposing backend credentials.
5. `IngressEnforcer` and `EgressEnforcer`: apply an immutable communication
   snapshot and return auditable decisions.
6. `IdentityIssuer` and `SecretBroker`: issue workload/task identity and apply
   credentials outside untrusted agent code.
7. `PackageStore`: resolve exact signed package/image digests without rebuild.
8. `MeteringSink`: attribute compute, memory, storage, network, model, and tool
   use to tenant/workflow/run/component.
9. `Clock` and `LeaseStore`: preserve B26 fencing and active-time semantics.

The interfaces describe required semantics, not the least-common-denominator
method names of Docker, Kubernetes, or Cloudflare.

## Portable conformance scenario

Each adapter proof runs the same fixture:

1. Register tenant A and tenant B.
2. Install one exact signed fixture image and manifest for tenant A.
3. Admit an idempotent asynchronous invocation and return durable IDs.
4. Start the sandbox with default-deny ingress/egress and no raw credential.
5. Emit authenticated progress, create a checkpoint, and commit an artifact.
6. Perform one allowed brokered HTTP call and deny one undeclared call.
7. Deliver an ordered terminal event after client disconnect/reconnect.
8. Restart or kill the worker, reconcile once, fence stale authority, and
   recover the durable state without duplicate completion.
9. Verify tenant B cannot discover or access any tenant-A object.
10. Attribute resource and external-call use to tenant A, then clean every
    ephemeral workload resource.

The deterministic fixture uses no live LLM. Provider latency must not hide
platform lifecycle defects.

## Substrate decision rule

Cloudflare becomes the first managed substrate only if the proof establishes:

- programmatic deployment of customer-built immutable images at the required
  isolation boundary;
- a workable composition for durable execution and multi-tenant platform
  code, including any separation imposed by product namespaces;
- gateway-only ingress/egress and agent-invisible credential application;
- durable events, artifacts, state, cancellation, restart, and usage
  attribution under documented limits;
- acceptable cold/warm behavior and a credible unit-economics model.

If it fails one of those requirements and Kubernetes passes, Kubernetes is the
primary managed backend and Cloudflare may remain an edge/gateway adapter. If
Cloudflare passes, Kubernetes remains the customer-owned-cloud/other-cloud
adapter. If neither passes, later runtime blocks stop until the violated
AgentPaaS contract is repaired.

## Task sequence

| Task | Name | Primary result |
|---|---|---|
| T01 | Coupling inventory and frozen fixtures | Every Docker/filesystem/process assumption mapped to a port or explicitly retained local behavior |
| T02 | Portable port contracts | Interfaces, fakes, tenant-aware identifiers, and conformance suite |
| T03 | Docker baseline adapter | Current behavior passes the portable slice without weakening v0.2.3 compatibility |
| T04 | Local Kubernetes proof | Exact package runs with default-deny networking, sandbox/resource policy, durable state/events/artifacts, and cleanup |
| T05 | Cloudflare feasibility proof | Exact package exercises the same lifecycle and records unsupported/composed product constraints honestly |
| T06 | Isolation, metering, and performance comparison | Two-tenant adversary proof plus cold/cached/warm measurements and cost inputs |
| T07 | Substrate decision record and downstream handoff | Signed evidence, chosen direction or explicit blocker, and locked B29/B30 assumptions |

## Required tests

- Interface contract tests run against fakes and every available adapter.
- Exact image/package digest is identical across proofs; no cloud rebuild.
- Alias movement cannot change an admitted exact deployment.
- Duplicate admission/start/result delivery creates one run and terminal state.
- Stale lease cannot emit events, commit artifacts, or call egress.
- Agent code cannot reach substrate control APIs or raw object-store secrets.
- Default-deny network policy fails closed when enforcement cannot be installed.
- Cross-tenant list/get/invoke/subscribe/download attempts fail and are audited.
- Cold image, cached-cold, warm sandbox, and cleanup timing are recorded where
  the substrate supports them; unsupported modes are labelled, not simulated.
- Optional mesh removal leaves AgentPaaS behavior correct; enabling it may add
  transport protection but no new product authority.

## Exit gate

`make block28-gate` must include:

- full B27 cumulative gate;
- portable port unit/race/adversary tests;
- mandatory Docker conformance;
- recorded local Kubernetes conformance;
- recorded Cloudflare proof or an explicit external-feasibility blocker;
- two-tenant isolation and metering evidence;
- a reviewable substrate comparison and downstream contract handoff.

A skipped Kubernetes or Cloudflare proof is **NO-GO** for B29 unless the
founder explicitly narrows the approved portability decision in the decision
register. B28 completion is not authorization to publish a cloud service.

## Handoff to B29 and B30

Record:

- exact interface versions and fixture digests;
- every remaining local-only assumption;
- adapter lifecycle timing breakdowns;
- Cloudflare/Kubernetes feasibility findings and unresolved limits;
- the selected event, artifact, identity, and metering seams;
- confirmation that B29 may build streaming/events and B30 may build
  long-running execution without selecting a substrate-specific public API.
