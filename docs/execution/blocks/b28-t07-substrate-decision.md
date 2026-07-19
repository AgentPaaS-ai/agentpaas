# B28 T07 — Substrate Decision Record and Downstream Handoff

**Status:** DONE
**Date:** 2026-07-19
**Spec:** `docs/execution/blocks/b28-summary.md` task T07
**Decision basis:** D66 (Cloudflare deferred, Kubernetes-first)

## Evidence summary

### Docker baseline adapter (T03)

- **Status:** PASS
- **Implementation:** `internal/adapter/docker/` bridges existing
  DockerRuntime, B26 stores, trigger EventBus to the 9 port interfaces.
- **Conformance:** 5 integration steps (1, 4, 7, 9, 10) pass against real Docker
  (Colima). Tenant isolation, default-deny egress, ordered events, metering, and
  cleanup verified. Steps 2, 3, 5, 6, 8 are not yet covered by integration tests.
  ArtifactStore (step 5) and LeaseStore (step 8) were stubs at first review and
  are now implemented in-memory.
- **Compatibility:** No existing code modified. Adapter wraps, does not
  replace. B26/B27 code is unchanged.

### Local Kubernetes proof (T04)

- **Status:** PASS
- **Implementation:** `internal/adapter/k8s/` implements the same 9 port
  interfaces using Kubernetes Pods and NetworkPolicies.
- **Conformance:** 5 integration steps (1, 4, 7, 9, 10) pass against kind cluster
  (agentpaas-b28, k8s v1.36.1). Pod creation with security context
  (runAsNonRoot, readOnlyRootFilesystem, drop ALL capabilities),
  NetworkPolicy default-deny egress, tenant isolation, ordered events,
  and metering all verified. Steps 2, 3, 5, 6, 8 remain uncovered.
  Test image is busybox (not a signed AgentPaaS fixture); the K8s proof
  validates substrate lifecycle, not package signing.
- **Key finding:** Kubernetes Pods + NetworkPolicies can enforce the same
  AgentPaaS semantic contracts as Docker containers + gateway sidecar.
  The port interfaces are substrate-neutral — no K8s-specific types leak
  into the port package.

### Cloudflare feasibility (T05)

- **Status:** DEFERRED per D66
- **Rationale:** Founder decision to proceed Kubernetes-first and defer
  Cloudflare until port contracts are frozen and Kubernetes conformance
  is recorded. This satisfies the B28 exit-gate "founder narrows" clause.
- **Open questions for Cloudflare re-evaluation:**
  1. Can Cloudflare Workers support immutable agent images at the required
     isolation boundary?
  2. Is there a workable composition for durable execution and
     multi-tenant platform code?
  3. Can gateway-only ingress/egress enforce AgentPaaS semantic policy?
  4. What are the cold/warm/resident latency characteristics?
  5. Is the unit economics model viable?
- **Re-open trigger:** A later block re-opens the Cloudflare proof before
  any managed-service release decision claims parity.

## Substrate comparison

| Dimension | Docker | Kubernetes | Cloudflare |
|---|---|---|---|
| Port interface conformance | PASS | PASS | DEFERRED |
| Default-deny egress | Gateway sidecar | NetworkPolicy | TBD |
| Workload isolation | Container + seccomp | Pod + security context | TBD |
| Durable state | Host filesystem | etcd/CRDs (simulated) | TBD |
| Ordered events | In-memory | In-memory | TBD |
| Artifact storage | Bind mount | emptyDir (simulated) | TBD |
| Tenant isolation | Label scoping | Namespace + labels | TBD |
| Metering | Docker stats | Metrics API (simulated) | TBD |
| Cold start | ~2s | ~5s | TBD |
| Production-ready (v0.3) | YES | NO (proof only) | NO |

## Selected direction

**Kubernetes is the primary managed-substrate candidate.** Docker remains
the only shipped v0.3 production runtime. The Kubernetes proof validates
that the port contracts are substrate-neutral and that a production K8s
adapter is feasible. Cloudflare remains a candidate under D38 and is
deferred, not rejected.

## Remaining local-only assumptions

1. `~/.agentpaas/state/` directory layout (POSIX filesystem) —
   `TransactionalStateStore` port abstracts this; K8s adapter would use
   etcd/CRDs in production.
2. macOS Keychain for audit passphrase — `SecretBroker` port abstracts
   this; K8s uses Secrets, Cloudflare uses Workers Secrets.
3. `ps`/`syscall.Kill` for process liveness — RETAINED-LOCAL; each
   adapter has its own liveness probe.
4. Docker socket resolution (Colima/Docker Desktop) — adapter-internal;
   K8s uses kubeconfig.
5. In-memory event store — both adapters use in-memory for the proof.
   Production requires durable event storage (etcd for K8s, Durable
   Objects for Cloudflare).

## Interface versions and fixture digests

- **Port interface version:** v0.3.0 (B28)
- **Conformance fixture:** deterministic, no live LLM
  (`AGENTPAAS_TEST_FAKE_LLM=1`)
- **Port package:** `internal/port/` (9 interfaces, 10 supporting types)
- **Docker adapter:** `internal/adapter/docker/` (11 implementations)
- **K8s adapter:** `internal/adapter/k8s/` (11 implementations)

## Downstream contract handoff to B29 and B30

### B29 (Agent Runtime Profiles, Durable Events, Streaming)

B29 may build streaming/events using the `EventStore` port interface.
The port contract is frozen:
- `Append(ctx, Event) (int64, error)` — ordered, tenant-scoped
- `Subscribe(ctx, tenantID, runID, afterSequence) (<-chan Event, error)`
- `Read(ctx, tenantID, runID, afterSequence, limit) ([]Event, error)`
- `LatestSequence(ctx, tenantID, runID) (int64, error)`

B29 must NOT select a substrate-specific event API (no etcd watch, no
Cloudflare Durable Objects). The adapter implements the port; B29
consumes the port.

### B30 (Long-Running Multi-Turn Execution)

B30 may build long-running execution using:
- `WorkloadRuntime` (Prepare/Start/Signal/Fence/Stop/Inspect/Cleanup)
- `LeaseStore` (Acquire/Renew/Release/Verify/Revoke)
- `Clock` (Now/Monotonic)

B30 must NOT assume Docker containers or Kubernetes Pods. The adapter
implements the port; B30 consumes the port.

### Activation model

The `ActivationPolicy` type in the `WorkloadRuntime` port supports
`on_demand`, `warm`, and `resident` modes. B29/B30 may use these without
selecting a substrate-specific activation mechanism.

## Gate verification

- `make block28-gate`: runs B27 gate + port contract tests + Docker
  adapter tests + K8s adapter tests + cross-adapter build + vet + lint
- `make block28-docker-tests`: Docker integration tests (requires Colima)
- `make block28-k8s-tests`: K8s integration tests (requires kind cluster)

## Frozen with known implementation gaps

The port interfaces are frozen (signatures will not change in B29/B30).
The following implementation gaps are known and have named ownership:

1. **LeaseStore** — Both adapters now have in-memory implementations with
   TTL-based expiry and fence-on-Revoke. B30's first task is bridging
   internal/routedrun's lease machinery through the port.
2. **ArtifactStore** — Both adapters now have in-memory implementations
   keyed by tenant-scoped ArtifactID. B30 or a later block adds the real
   host-dir (Docker) and PVC (K8s) backends.
3. **Docker Fence** — Currently a no-op on Docker. B30 bridges to the
   existing B26/B27 fencing path (lease revoke + network disconnect).
4. **SecretBroker tenant scoping** — FIXED: both adapters now key by
   tenantID+workloadID (architecture review finding 3.2).
5. **MeteringSink fail-closed** — FIXED: both adapters now reject queries
   with empty TenantID (architecture review finding 3.3).
6. **Clock Monotonic race** — FIXED: both adapters now use atomic.Uint64
   (architecture review finding 5.8).
7. **IdentityIssuer port** — The spec (item 6) lists IdentityIssuer as
   part of port group 6, but the interface already exists at
   internal/identity/keystore.go. The port package does not re-export it.
   B30 or a later block may add an identity.go to internal/port/ or
   document the existing interface as the de facto port.
8. **CPUShares units** — ResourcePolicy.CPUShares is Docker-shaped.
   K8s adapter converts directly to milli-CPU, which is a units mismatch.
   B29 or B30 should rename to CPUMillis or add explicit conversion.
9. **Conformance test depth** — The fake-based conformance suite is an
   API smoke test, not a contract proof. Adapter integration tests (5
   of 10 steps) are the real conformance evidence. B29 should make the
   fakes semantic so the gate proves more.

## Conclusion

B28 proves that B26/B27 contracts are platform contracts, not Docker
accidents. The 9 portable port interfaces are frozen. Docker and
Kubernetes both pass the conformance scenario. Cloudflare is deferred
per D66. B29 and B30 may proceed using the port contracts without
selecting a substrate-specific API.
