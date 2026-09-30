# Threat Model

This document describes the security boundaries, controls, and limits of AgentPaaS. For runtime behavior, see [how-enforcement-works.md](how-enforcement-works.md). For current capability limits, see [known-limitations.md](known-limitations.md).

## 3.1 Threat model (STRIDE-condensed)
| Threat | Vector | Control |
|---|---|---|
| Malicious/buggy AI-generated code exfiltrates secrets | outbound HTTP/DNS/raw socket | brokered credentials are not visible to agent code; internal-only network, gateway-only egress, DNS stub, default-deny, payload-hash audit |
| Secret theft from image/registry | keys baked into layers | keychain broker + gateway-side injection by default; direct leases explicit only; pack-time scanner |
| Prompt-injected agent calls unauthorized tools | MCP/tool call to non-approved server | MCP allow-list by server id + per-tool policy (P2: per-tool args constraints) |
| Container escape | kernel/runtime exploit | non-root, read-only rootfs, no shell, dropped capabilities (ALL), seccomp default profile, no privileged, pids-limit, memory/cpu caps |
| Supply chain (our deps) | compromised base image / dep | distroless pinned digests, SBOM on every artifact, `go mod verify`, dependabot, pinned vendored agentgateway with checksum |
| Supply chain (user deps) | typosquatted Python package | locked installs only (uv), SBOM surfaced in dashboard, osv-scanner advisory in `agentpaas pack` output |
| Trigger API abuse | replay / brute force | idempotency keys, constant-time key compare, lockout and audit on repeated 401 |
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

## Security limits

- AgentPaaS hardens containers. It does not claim protection from kernel zero-days.
- PII and outbound data controls use configured detectors and patterns. They are not semantic DLP.
- Local mode trusts the developer's machine.
- Cloud default-tier enforcement depends on the AgentPaaS control plane and gateway. The high-assurance tier adds substrate-enforced network policy.

---

## Cloud (AgentPaaS Cloud) enforcement and assurance class

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

**Assurance class.**

- **Local:** the agent has no network route except through its gateway. The network shape provides the boundary.
- **Cloud default tier:** the control plane and gateway enforce the per-instance policy. This tier does not claim substrate-enforced isolation.
- **Cloud high-assurance tier:** a paid, on-demand tier adds substrate-enforced network policy in a dedicated Kubernetes namespace.

**Cloud scope limits.** Cloud governs HTTP and HTTPS egress. Raw TCP, UDP, ICMP, and external agent-to-agent federation are outside the current scope.

Signed images, SBOMs, per-run identity, brokered credentials, default-deny egress, and hash-chained audit apply on every tier. The enforcement mechanism differs by tier.

---

## Ingress platform and egress gateway

The cloud webhook plane has two trust boundaries. Ingress verifies and admits a governed run. Egress remains the per-run gateway described above.

**Ingress.** Untrusted HTTP is verified and filtered before a run exists. HMAC is checked before admission. Signing secrets are encrypted at rest. Source filters are allow-lists. Cross-tenant identity is checked at admission, and reply destinations are pinned to the run.

**Egress.** Outbound traffic remains default-deny with brokered credentials and a host allow-list. The workload never holds an `xoxb-` value.

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