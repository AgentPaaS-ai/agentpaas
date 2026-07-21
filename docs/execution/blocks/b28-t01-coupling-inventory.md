# B28 T01 — Coupling Inventory and Frozen Fixtures

**Status:** DONE — 2026-07-19
**Date:** 2026-07-19
**Spec:** `docs/execution/blocks/b28-summary.md` task T01

## Goal (from spec)

> Every Docker/filesystem/process assumption mapped to a port or explicitly
> retained as local behavior.

Each finding is tagged with a port from `b28-summary.md:71-88` or marked
`RETAINED-LOCAL`. Ports referenced:

- `WorkloadRuntime` — prepare, start, signal, fence, stop, inspect, cleanup
- `TransactionalStateStore` — CAS durable deployment/invocation/task/run/attempt/lease/workflow state
- `EventStore` — append ordered durable events, subscribe after cursor
- `ArtifactStore` — commit, authorize, stream/range-read, verify, retain, delete
- `IngressEnforcer` / `EgressEnforcer` — apply immutable communication snapshot, auditable decisions
- `IdentityIssuer` / `SecretBroker` — issue workload/task identity, apply credentials outside untrusted code
- `PackageStore` — resolve exact signed package/image digests without rebuild
- `MeteringSink` — attribute compute/memory/storage/network/model/tool use
- `Clock` / `LeaseStore` — preserve B26 fencing and active-time semantics

## Findings

### A. Docker Engine API coupling → `WorkloadRuntime`

The `RuntimeDriver` interface (`internal/runtime/driver.go:209-271`) is
already a substrate-neutral abstraction. The leak is the single concrete
implementation `DockerRuntime` (`internal/runtime/docker.go:34-37`) and its
consumers:

| Site | Coupling | Port |
|---|---|---|
| `internal/runtime/docker.go:34` | `DockerRuntime` is the only `RuntimeDriver` impl; imports `github.com/docker/docker/client` | `WorkloadRuntime` |
| `internal/runtime/docker.go:28-30` | `GatewayImage = "ghcr.io/agentgateway/agentgateway:v1.3.0"` pinned; gateway is a Docker container, not a workload abstraction | `WorkloadRuntime` |
| `internal/runtime/naming.go` | Network/container names use Docker label conventions (`agentpaas.run-id`, etc.) | `WorkloadRuntime` (labels must be port-internal) |
| `internal/runtime/reconcile.go` | `ListContainers(labelFilters...)` — Docker-specific label filter semantics | `WorkloadRuntime` |
| `internal/daemon/control_handlers.go:1884` | `getOrCreateRuntime()` returns `*runtime.DockerRuntime` (concrete, not interface) | `WorkloadRuntime` |
| `internal/daemon/control_handlers.go:985` | `checkDockerEngineVersion(ctx, rt *runtime.DockerRuntime)` — hardcoded Docker Engine version gate | `WorkloadRuntime` |
| `internal/daemon/control_handlers.go:803-824` | Container spec built with Docker-specific `Image`, `NetworkIDs`, `Binds`, `CapAdd` directly | `WorkloadRuntime` |
| `internal/daemon/control_handlers.go:775-801` | Bind-mounts (`agentBinds`) constructed as Docker `host:container` strings; audit dir, credentials file, journal key/dir, artifact dir all assume Docker bind semantics | `WorkloadRuntime` + `ArtifactStore` |
| `internal/daemon/control_handlers.go:847-861` | `trackedRun` struct holds `ContainerID`, `NetworkID`, `GatewayID` directly as Docker IDs | `WorkloadRuntime` |

**Implication:** `RuntimeDriver` already exists but (a) has no second
implementation and (b) is not split into `WorkloadRuntime` + the other 8 ports.
The ContainerSpec, ContainerStatus, NetworkSpec types are Docker-shaped. B28
T02 must define a substrate-neutral `WorkloadRuntime` and either adapt
`RuntimeDriver` to it or replace it.

### B. Docker socket / client discovery → `WorkloadRuntime` (RETAINED-LOCAL for macOS)

| Site | Coupling | Port / Disposition |
|---|---|---|
| `internal/dockerclient/dockerclient.go:1-200` | Resolves Docker socket path, handles Colima/Docker Desktop contexts, exports `DOCKER_HOST` | `WorkloadRuntime` — adapter-internal |
| `cmd/agentpaasd/main.go:38-43` | `dockerclient.ExportHostToEnv()` — propagates resolved Docker endpoint to child processes | RETAINED-LOCAL (macOS-specific; each substrate has its own bootstrap) |
| `internal/export/image.go:69` | "hard-codes /var/run/docker.sock and cannot follow Colima contexts" — known defect | Fix-in-place; not a B28 port |
| `internal/doctor/checks.go:328-394` | `CheckDockerDesktop()` pgrep's for `colima` and `com.docker.docker` processes | RETAINED-LOCAL (doctor is a local CLI diagnostic; substrate portability means each adapter ships its own doctor probe) |
| `internal/cli/doctor.go:121` | "Docker daemon not responding (is Docker Desktop / Colima running?)" | RETAINED-LOCAL |

### C. Host filesystem paths → `TransactionalStateStore` / `EventStore` / `ArtifactStore`

The `HomePaths` struct (`internal/home/home.go:19-46`) hardcodes a
host-filesystem layout under `~/.agentpaas`. This is portable across macOS /
Linux but assumes a POSIX filesystem with `0700` permissions and a Unix
domain socket.

| Site | Coupling | Port |
|---|---|---|
| `internal/home/home.go:19-46` | All state paths derive from `Home` (`~/.agentpaas`) — assumes POSIX fs + Unix socket | `TransactionalStateStore` |
| `internal/home/home.go:397` | `exec.Command("ps", "-p", pid, ...)` — host process check (assumes `ps`) | RETAINED-LOCAL (per-substrate liveness probe) |
| `internal/home/home.go:441` | `os.FindProcess(pid)` + `syscall.Kill` — host PID-based liveness | RETAINED-LOCAL |
| `internal/daemon/control_handlers.go:771-772` | `journalHostPath = filepath.Join(s.homePaths.State, "runs", runID, "journals", ...)` — journal on host fs | `TransactionalStateStore` |
| `internal/daemon/control_handlers.go:798-800` | `artifactDir` bind-mounted into container as `/workspace/artifacts` — Docker bind semantics | `ArtifactStore` |
| `internal/secrets/lease.go:58` | `os.MkdirTemp("", "agentpaas-leases-*")` — host tmpdir for lease files | `LeaseStore` |
| `internal/audit/passphrase.go:74-86` | `exec.Command("security", ...)` — **macOS Keychain** for audit passphrase | `SecretBroker` (adapter-internal) |

**Implication:** The `~/.agentpaas` layout and Unix-socket daemon control
plane can remain local-adapter-internal. But the durable state the daemon
writes (deployment records, invocation records, runs, attempts, leases,
events, artifacts, journals) must move behind `TransactionalStateStore`,
`EventStore`, and `ArtifactStore` interfaces. Kubernetes and Cloudflare will
back those interfaces with etcd/ConfigMaps/CRDs and Durable Objects/KV
respectively.

### D. Process / signal coupling → `WorkloadRuntime`

| Site | Coupling | Port |
|---|---|---|
| `cmd/agentpaasd/main.go:77-84` | `signal.Notify(SIGTERM, SIGINT, SIGHUP)` — daemon lifecycle on host | RETAINED-LOCAL (daemon is always host-process) |
| `cmd/harness/main.go:55` | `signal.Notify(SIGTERM, SIGINT)` inside container — container process | `WorkloadRuntime` (signal is one of the port's operations: "signal, fence, stop") |
| `internal/harness/process_reaper.go:38` | `signal.Notify(SIGCHLD)` — reaps zombie children inside container | `WorkloadRuntime` (container-internal) |
| `internal/harness/python_worker.go:424` | `syscall.Kill(-pid, SIGTERM)` — kills Python worker process group | `WorkloadRuntime` |
| `internal/runtime/docker.go` (Exec method) | `cli.ContainerExecAttach` — Docker exec into running container | `WorkloadRuntime` |

### E. Resource limits → `WorkloadRuntime` (RETAINED-LOCAL for application logic, port for enforcement)

| Site | Coupling | Port |
|---|---|---|
| `internal/harness/python_worker.go:477-479` | `("RLIMIT_CPU", 30), ("RLIMIT_NPROC", 0)` — Linux rlimits hardcoded | `WorkloadRuntime` (enforcement), spec-level policy is portable |
| `internal/harness/firewall_caps_linux.go:14-38` | `DropNetAdminCapability()` — Linux capset syscall | `WorkloadRuntime` (container-internal; each adapter maps to its own mechanism) |
| `internal/harness/firewall.go:24` | `InitEgressFirewall` — iptables OUTPUT rules inside container | `EgressEnforcer` (the policy is portable; the iptables implementation is adapter-internal) |
| `internal/daemon/control_handlers.go:809-811` | `agentSpec.CapAdd = []string{"NET_ADMIN"}` — Docker capability add | `EgressEnforcer` |

**Implication:** `b30-summary.md:359-369` already calls for removing the
`RLIMIT_CPU=30` / `RLIMIT_NPROC=0` hardcoded values. B28 does not remove them
but defines the `WorkloadRuntime` resource-control surface so B30 can replace
them with policy-derived container limits.

### F. Network topology → `EgressEnforcer` / `IngressEnforcer`

The current topology (internal-only network + egress network + gateway
sidecar) is Docker-specific but the *policy* is not.

| Site | Coupling | Port |
|---|---|---|
| `internal/daemon/control_handlers.go` (CreateNetwork calls) | Creates `internal` and `egress` Docker networks per run | `WorkloadRuntime` (network provisioning is adapter-internal) |
| `internal/daemon/control_handlers.go:775` | Gateway container on both networks, agent on internal only | `EgressEnforcer` (policy: agent cannot reach internet directly) |
| `internal/harness/rpc_server.go:437-456` | `AGENTPAAS_GATEWAY_URL` env var rewrites LLM URLs to gateway — assumes gateway is a reachable HTTP proxy | `EgressEnforcer` (the rewrite is portable; the gateway container is adapter-internal) |
| `internal/policy/smoke_test.go:98` | `filepath.Join(os.TempDir(), "agentpaas-agentgateway-cache")` — gateway config cache on host fs | `EgressEnforcer` (adapter-internal cache) |

**Implication:** The gateway sidecar is a Docker implementation detail. The
portable contract is: "agent egress is default-deny; an `EgressEnforcer`
applies an immutable communication snapshot and returns auditable decisions."
Kubernetes may use a NetworkPolicy + sidecar or a service-mesh adapter;
Cloudflare may use Workers bindings. The `AGENTPAAS_GATEWAY_URL` rewrite
pattern is portable across adapters that expose an HTTP proxy.

### G. Identity and secrets → `IdentityIssuer` / `SecretBroker`

| Site | Coupling | Port |
|---|---|---|
| `internal/identity/issuer.go:15` | `LocalIdentityIssuer` implements `IdentityIssuer` interface — already abstracted | `IdentityIssuer` (interface exists) |
| `internal/identity/keystore.go:131-134` | `IdentityIssuer` interface defined | `IdentityIssuer` |
| `internal/audit/passphrase.go:74-86` | `exec.Command("security", ...)` — **macOS Keychain** for audit passphrase | `SecretBroker` (adapter-internal; Kubernetes uses Secrets, Cloudflare uses Workers Secrets/KV) |
| `internal/daemon/control_handlers.go:776-778` | Credentials file bind-mounted into container, deleted after load | `SecretBroker` |
| `internal/harness/rpc_server.go:250-286` | `LoadCredentials(path)` — reads JSON sidecar file, deletes it | `SecretBroker` (adapter-internal application mechanism) |

**Implication:** `IdentityIssuer` already exists as an interface with a local
implementation. B28 must add `SecretBroker` as the portable abstraction for
credential application (file bind-mount on Docker, Kubernetes Secret mount,
Workers secret binding).

### H. Package / image handling → `PackageStore`

| Site | Coupling | Port |
|---|---|---|
| `internal/pack/registry.go:12-131` | `dockerclient.New()` — pushes images to local Docker daemon's image store | `PackageStore` (Docker daemon IS the package store currently) |
| `internal/pack/build.go:22-118` | `dockerclient.New()` — builds images via Docker daemon | `PackageStore` (build is adapter-internal; resolution must be portable) |
| `internal/install/image.go:71-92` | `skopeo copy` + `docker image inspect` — assumes Docker daemon present | `PackageStore` |
| `internal/daemon/control_handlers.go:813-823` | `imageRef = "sha256:" + imageDigest` or `pack.LocalImageRef(...)` — resolves image in local Docker store | `PackageStore` |
| `internal/install/materialize.go:88-195` | `os.MkdirTemp` staging dir + `docker-daemon:` skopeo copy — host fs + Docker | `PackageStore` + `TransactionalStateStore` |

**Implication:** `PackageStore` must abstract "resolve exact signed
package/image digest without rebuild." Docker resolves from local image
store; Kubernetes resolves from a registry; Cloudflare resolves from its
image registry. The B22/B23 signed bundle format and digest verification
remain portable; only the image-resolution backend changes.

### I. Metering → `MeteringSink`

| Site | Coupling | Port |
|---|---|---|
| `internal/runtime/docker.go` (Stats method) | `cli.ContainerStats` — Docker stats API for CPU/memory/PIDs | `MeteringSink` (adapter-internal metric source) |
| `internal/harness/rpc_server.go:522-528` | `auditLLMResult(provider, model, inputTokens, outputTokens, ...)` — LLM usage to audit | `MeteringSink` (audit is one sink; need a metering interface) |
| `internal/llm/provider.go:38-44` | `EstimateCost(provider, model, inputTokens, outputTokens)` — cost estimation | `MeteringSink` (cost attribution) |

**Implication:** No metering interface exists today. B28 must add
`MeteringSink` and wire it to the existing audit + stats + cost-estimate
sources. Kubernetes and Cloudflare will back it with their own metric sources.

### J. Clock and lease → `Clock` / `LeaseStore`

| Site | Coupling | Port |
|---|---|---|
| `internal/secrets/lease.go:58` | `os.MkdirTemp("", "agentpaas-leases-*")` — lease files on host tmpdir | `LeaseStore` |
| `internal/trigger/idempotency.go:21` | `DefaultIdempotencyTTL = 24 * time.Hour` — time.Duration constants | `Clock` (time semantics are portable; storage backend is not) |
| `internal/daemon/control_handlers.go` (timeout constants) | Fixed 60s/2m/5m/120s layers — `b30-summary.md:79-90` table | `Clock` (B30 removes fixed constants; B28 defines the Clock port) |

## Summary table

| Port | Current state | Work required in T02 |
|---|---|---|
| `WorkloadRuntime` | `RuntimeDriver` interface exists; `DockerRuntime` is only impl; spec types are Docker-shaped | Define substrate-neutral `WorkloadRuntime`; adapt or replace `RuntimeDriver`; split network provisioning from enforcement |
| `TransactionalStateStore` | State is host files under `~/.agentpaas/state/`; `trackedRun` is in-memory | Define CAS interface; back with local BoltDB/JSON first; map existing state files |
| `EventStore` | B27 progress journal is durable (HMAC'd JSONL on host fs); trigger event bus is in-memory | Extract `EventStore` from journal writer; make trigger bus durable |
| `ArtifactStore` | B27 artifact workspace is a bind-mounted host directory with path/quota/digest validation | Define `ArtifactStore` interface; current bind-mount becomes Docker adapter's impl |
| `IngressEnforcer` / `EgressEnforcer` | Gateway sidecar container + iptables + `AGENTPAAS_GATEWAY_URL` rewrite; `EgressFirewallEnabled` flag | Define `EgressEnforcer` interface; gateway/iptables become Docker adapter impl |
| `IdentityIssuer` / `SecretBroker` | `IdentityIssuer` interface exists with `LocalIdentityIssuer`; secrets are file bind-mount + macOS Keychain for passphrase | Define `SecretBroker`; existing file-mount becomes Docker adapter impl |
| `PackageStore` | `dockerclient.New()` + `skopeo copy` + local Docker image store | Define `PackageStore`; Docker image store becomes Docker adapter impl |
| `MeteringSink` | No interface; audit + Docker stats + `EstimateCost` are scattered | Define `MeteringSink`; wire existing sources to it |
| `Clock` / `LeaseStore` | Host tmpdir lease files; `time.Hour` constants; fixed timeout layers | Define `Clock` (monotonic + UTC) and `LeaseStore` interfaces; current tmpdir becomes local adapter impl |

## RETAINED-LOCAL behaviors

These are explicitly retained as local-adapter behavior and are NOT promoted
to ports:

1. `cmd/agentpaasd/main.go` signal handling and `DOCKER_HOST` export — the
   daemon is always a host process; each substrate has its own bootstrap.
2. `internal/doctor/checks.go` Docker Desktop / Colima detection — doctor is
   a local CLI diagnostic; each adapter ships its own doctor probe.
3. `internal/home/home.go` `ps` / `os.FindProcess` / `syscall.Kill` for
   daemon PID liveness — host process management is per-substrate.
4. `internal/audit/passphrase.go` macOS Keychain access — the audit
   passphrase source is adapter-internal; Kubernetes uses Secrets,
   Cloudflare uses Workers Secrets.
5. `internal/harness/firewall_caps_linux.go` Linux capset syscall —
   container-internal hardening; each adapter maps to its own mechanism.
6. `internal/harness/process_reaper.go` SIGCHLD reaping — container-internal.

## Frozen fixtures

Per `b28-summary.md:107-108`: "The deterministic fixture uses no live LLM.
Provider latency must not hide platform lifecycle defects."

The conformance fixture (T02) will use:
- The existing `AGENTPAAS_TEST_FAKE_LLM=1` path (`internal/harness/rpc_server.go:330`)
  which returns `"agentpaas fake llm response"` — no provider call, no tokens.
- A deterministic HTTP fixture (e.g., httpbin.org/status/200 or a local
  httptest server) for the "one allowed brokered HTTP call" step.
- A deterministic checkpoint/artifact commit (B27 progress API).

No LLM judge, no expected prose, no provider latency. The fixture is
identical across Docker, Kubernetes, and (deferred) Cloudflare adapters.

## Next: T02

Define the 9 portable port interfaces in a new `internal/port` package with
fakes and a conformance suite. The conformance suite runs the fixture from
the "Portable conformance scenario" section of `b28-summary.md:92-108`.
