# B15-T05 Architecture Decisions

## Decision 1: Init Container Pattern — Option B (Pragmatic P1)

**Date:** 2026-06-30
**Status:** DECIDED (user-approved)
**Block:** B15-T05, MC4

### Context

The T05 spec (agentpaas-execution-plan-v1.md §15-T05) says:

> R17 init container pattern: Remove CAP_NET_ADMIN from the agent container
> entirely. Use an init container that programs iptables rules in a shared
> network namespace, then exits. The agent container never has NET_ADMIN.

Two options were evaluated:

**Option A — Full init container pattern (spec's literal intent):**
- Separate firewall-init container (new image with iptables) on the internal
  network, CapAdd NET_ADMIN, runs firewall_init.sh, sleeps to hold namespace
- Agent container joins via `--net=container:<init-id>` (Docker container
  network mode), CapDrop ALL, zero capabilities
- Docker inspect would show no NET_ADMIN on the agent container
- **Challenges:**
  - No iptables-capable image exists in the repo. The agent image is distroless
    python3-debian12, built dynamically in internal/pack/build.go
  - Would need a new Dockerfile (alpine + iptables) or a public image reference
  - Requires ContainerSpec.NetworkMode field + docker.go Create() changes to
    handle the `container:` network mode
  - Topology restructuring: init container lifecycle management in Run/Stop
  - The B14E research (docs/archive/build-sessions/2026-06/b14e/prompts/
    b14e-r17-research.md) explicitly marks network namespace sharing as P2

**Option B — Keep PID 1 capset-drop + add verification (pragmatic P1):**
- Current approach already works and is shipped (B14E):
  - Harness binary (PID 1, root) runs firewall_init.sh to program iptables
  - DropNetAdminCapability() in internal/harness/firewall_caps_linux.go uses
    the Linux capset syscall to remove CAP_NET_ADMIN from effective, permitted,
    and inheritable sets BEFORE starting the Python worker
  - The agent PROCESS (UID 64000) cannot flush iptables — it has no
    CAP_NET_ADMIN at runtime
- Docker inspect still shows NET_ADMIN in CapAdd, but the runtime process
    does not have the capability
- MC4 collapses into MC5: a Docker integration test proving the capset drop
    is effective end-to-end

### Decision

**Option B.** Keep the existing PID 1 capset-drop approach for P1.

### Rationale

1. **Security goal already met:** The threat model is "agent process cannot
   flush or modify iptables rules." The capset drop achieves this — the
   Python worker runs without CAP_NET_ADMIN in any capability set. An
   `iptables -F` attempt returns EPERM.

2. **Spec intent vs implementation:** The spec's intent ("the agent container
   never has NET_ADMIN") is about the agent PROCESS not having the capability,
   not about the Docker CapAdd metadata. The capset drop satisfies the
   security intent even though Docker inspect shows the capability in CapAdd.

3. **P1 vs P2 scope:** The B14E research marked network namespace sharing as
   P2. Building a new iptables image, modifying the Docker runtime driver,
   and restructuring the container topology is real P2 work. Shipping v0.1.0
   with the working capset approach is the right call.

4. **Complexity vs benefit:** Option A requires ~300 lines of new code
   (ContainerSpec.NetworkMode, docker.go Create changes, daemon Run/Stop
   handler changes, new Dockerfile, image build/publish). Option B requires
   ~50 lines (one integration test). The security benefit of Option A over
   Option B is marginal for P1 — both prevent iptables tampering.

### P2 Follow-Up

The full init container pattern (Option A) is tracked as P2:
- Build a minimal firewall-init image (alpine + iptables, ~5MB)
- Add ContainerSpec.NetworkMode to internal/runtime/driver.go
- Handle `container:` network mode in docker.go Create()
- Create init container in daemon Run handler, clean up in Stop
- Docker inspect shows no NET_ADMIN on agent container (cosmetic improvement)

This is filed in the P2 backlog (execution plan §15-P2-04 and the B14E
risk register R17 residual).

### Verification (MC5)

A Docker integration test (AGENTPAAS_DOCKER_TESTS=1) verifies that:
1. After the harness binary starts and DropNetAdminCapability() runs, the
   agent process (UID 64000) cannot execute `iptables -F` — it returns
   permission denied (exit code non-zero)
2. The iptables rules set by PID 1 persist (OUTPUT DROP policy remains)

This test closes the verification gap: we prove the capset drop works in
a real container, not just in code review.
