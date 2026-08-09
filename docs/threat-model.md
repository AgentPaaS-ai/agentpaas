# Threat Model

This document captures the security posture and threat controls for
AgentPaaS P1. It is derived from the internal PRD security deep-dive.

For how controls map to runtime enforcement, see
[how-enforcement-works.md](how-enforcement-works.md). For accepted P1 gaps,
see [known-limitations.md](known-limitations.md).

---

# 3. SECURITY DEEP-DIVE (BULLETPROOFING ACTIONS)

## 3.1 Threat model (STRIDE-condensed)
| Threat | Vector | Control |
|---|---|---|
| Malicious/buggy AI-generated code exfiltrates secrets | outbound HTTP/DNS/raw socket | brokered credentials are not visible to agent code; internal-only network, gateway-only egress, DNS stub, default-deny, payload-hash audit |
| Secret theft from image/registry | keys baked into layers | keychain broker + gateway-side injection by default; direct leases explicit only; pack-time scanner |
| Prompt-injected agent calls unauthorized tools | MCP/tool call to non-approved server | MCP allow-list by server id + per-tool policy (P2: per-tool args constraints) |
| Container escape | kernel/runtime exploit | non-root, read-only rootfs, no shell, dropped capabilities (ALL), seccomp default profile, no privileged, pids-limit, memory/cpu caps |
| Supply chain (our deps) | compromised base image / dep | distroless pinned digests, SBOM on every artifact, `go mod verify`, dependabot, pinned vendored agentgateway with checksum |
| Supply chain (user deps) | typosquatted Python package | locked installs only (uv), SBOM surfaced in dashboard, osv-scanner advisory in `agentpaas pack` output |
| Trigger API abuse | replay / brute force | idempotency keys, constant-time key compare, rate limit, lockout+audit on repeated 401 |
| Audit tampering | attacker edits logs | canonical hash-chained JSONL, daemon-audit-key checkpoint signatures, local head anchor, signed export manifest, `agentpaas audit verify` |
| Daemon compromise | local privilege escalation | daemon runs as user (not root); socket 0600; no setuid; secrets only via OS keychain APIs |
| Malicious webhook targets | hook exfiltration channel | hook destinations are themselves policy-checked egress |
| Dashboard exposure | accidental 0.0.0.0 bind | loopback default; `--expose` refuses to start without API key + warns; CSRF tokens; strict CSP, no inline JS |
| Domain fronting | SNI ≠ Host | gateway cross-checks SNI/Host/DNS answer; mismatch = deny + audit |

## 3.1a Architecture invariant: one gateway per run

Every agent run receives its own dedicated gateway sidecar. A shared
gateway across agents or runs is prohibited. The per-run gateway keeps
isolation topological: agent containers on separate runs have no network
path to each other, to each other's gateways, or to each other's brokered
credentials. Cross-agent traffic uses workflow-scoped internal networks
between the relevant gateways with per-binding capabilities; it never
uses a shared gateway.

> **Scope note (2026-08-08):** §3.1 and §3.1a describe the LOCAL macOS runtime,
> where enforcement is topological (sidecar + internal network). For the
> AgentPaaS Cloud managed service, see §3.4 — the enforcement point and its
> assurance class differ by tier.

## 3.2 Hard security actions (all are execution-plan blocks)
1. P1 applies macOS Docker Desktop/Colima container hardening by default
   (non-root, read-only rootfs, no shell, dropped capabilities, seccomp where
   Docker exposes it, pids/memory/cpu caps). P2 adds certified Linux-native
   seccomp + AppArmor profiles.
2. Fuzz the policy compiler and the Trigger API (go-fuzz / protobuf fuzz).
3. `agentpaas doctor` verifies: docker version, network isolation actually
   holds (spins a canary container and proves no default route), keychain
   access, port collisions.
4. P1 integration test suite includes a fast red-team smoke gate that runs
   through the real pack/run/operator path and proves the core local release
   claims: default-deny egress, policy/credential misuse denial, brokered
   secret invisibility, host-access blocking smoke, resource containment
   smoke, and operator prompt-injection refusal. Full adversarial coverage is
   deferred to P2; P1 should be honest that this is release smoke proof, not a
   comprehensive pentest.
5. External pentest before GA tag; bug bounty (modest, scoped) at GA.
6. SLSA provenance for our own release artifacts; users can verify
   `agentpaas` binaries the same way we verify their agents.
7. Security disclosure policy + SECURITY.md from the first public commit.
8. CVE response SLA stated publicly: critical < 48h patch for the runtime.

## 3.3 What we explicitly do NOT claim in P1 (honesty = trust)
- Not a sandbox against kernel 0-days (we harden containers; we are not gVisor).
  P2 option: gVisor/Kata runtime class for high-assurance mode.
- Outbound data-loss prevention is fingerprint-based, not semantic, in P1.
- P1 red-team coverage is a fast smoke gate for demo/release-critical claims,
  not a full adversarial research corpus. P2 adds DNS tunneling, proxy bypass
  variants, IPv6/UDP/ICMP/domain-fronting depth, direct-lease exfil/DLP,
  SBOM/signature tamper, full MCP prompt-injection matrix, fuzzed operator
  payloads, and permanent red-team gating on every runtime/gateway change.
- Local mode trusts the developer's machine; we protect against the AGENT,
  not against the user.

---

## 3.4 Cloud (AgentPaaS Cloud) enforcement and assurance class

The cloud managed service runs the same signed OCI images on Cloudflare, but
the enforcement mechanism and the strength of the guarantee differ from the
local sidecar model in §3.1a. This section exists so the cloud claims are
never overstated against the local ones.

**Enforcement mechanism (cloud default tier).** Each run is a Cloudflare
Container (VM-per-instance). Egress is enforced by a control-plane
message-carrier: the run's compiled `allowed_hosts` is pushed onto the
container instance at admission (per-instance egress), and an outbound
handler in the Workers runtime — outside the workload trust boundary —
injects brokered credentials just-in-time. Agent code never holds a
credential value. The platform fail-closed default denies non-HTTP/S ports
and pins DNS, so if the carrier is absent the container has no internet.

**Assurance class — the honest distinction.**

- **Local (§3.1a): topological.** "No egress path exists except through the
  gateway." Proven once, structurally, by the network shape.
- **Cloud default tier: control-plane-enforced.** "Every egress path that
  exists terminates at a carrier we control, which applies policy before any
  bytes leave." This is enforced by OUR code and is proven correct per
  release by verifier + adversary testing. There is no substrate backstop on
  the routing decision, so a carrier bug is a potential bypass. This is the
 accepted assurance debt of the Cloudflare-only data plane.
 - **Cloud high-assurance tier (paid, on-demand): substrate-enforced.**
 For tenants who require it, workloads run in a dedicated Kubernetes
 namespace with a Cilium FQDN NetworkPolicy floor compiled from the signed
 policy.yaml and enforced by the kernel, independent of AgentPaaS
 control-plane code. This restores the topological guarantee and answers
 "who enforces the enforcer" with "the cluster, verifiably."

**Cloud scope limits (accepted).** HTTP/S egress only — no non-HTTP
protocol governance. No external A2A federation. Both are deliberate scope
decisions, revivable on demand, and are not represented as capabilities.

**What does NOT differ by tier.** Signed images, SBOM, per-run identity,
brokered credentials invisible to agent code, default-deny egress, and the
hash-chained audit log hold on every tier. Only the enforcement mechanism's
assurance class changes.

---

## Phase 2 Threats: secure agent sharing

Phase 2 adds secure agent sharing: bundles, publisher identities, and
provenance chains. The following adversaries are specific to the sharing
surface. The P1 STRIDE table above still applies.

| # | Adversary | Attack | Control |
|---|-----------|--------|---------|
| A2 | Impersonator | "This bundle is from the publisher" with an attacker-controlled key | TOFU pinning + out-of-band fingerprint verification; key-change hard fail for known publishers (no silent acceptance of a new key for a known publisher fingerprint). Trust store persists verified fingerprints so subsequent bundles from the same publisher are recognized without re-prompting. |
| A11 | Stolen publisher key | Attacker signs a malicious bundle with the publisher's real private key | **Out of scope for v0.2.0.** AgentPaaS v0.2.0 has no revocation mechanism; a stolen key is outside the current trust boundary. Revocation-list support is planned. The docs acknowledge this limitation explicitly. Until revocation ships, users must treat the publisher keypair as a long-lived credential and protect it accordingly — export an encrypted backup and store it securely. |

For what publisher signatures do and do not prove, see
[trust-model.md](trust-model.md).