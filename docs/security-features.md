# Security features (full list)

The README keeps the five that matter most for a first read. Everything
else lives here.

| Feature | What it stops |
|---|---|
| Default-deny egress | Calls to any endpoint you did not approve |
| Container isolation | Non-root (UID 64000), read-only rootfs, no shell, caps dropped except NET_ADMIN when the egress firewall is on, seccomp profile |
| Credential brokering | Secrets never reach agent code; gateway injects them at request time |
| Internal-only network | No internet route except via gateway; DNS stub only resolves approved domains |
| Tamper-evident audit | Hash-chained JSONL + signed checkpoints. In-chain edits, reorders, and inserts fail verification. Cutting the last N records still leaves a valid prefix; full coverage needs external checkpoint anchoring (P2). |
| Signed audit export | Portable signed bundle, checkable on another machine |
| Signed images | Each agent image cosign-signed with a per-agent identity key |
| SBOM on every artifact | Software bill of materials at pack time |
| Domain fronting block | Gateway cross-checks SNI / Host / DNS; mismatch is deny |
| DNS exfiltration block | DNS only via gateway stub; raw-IP dialing blocked |
| Pack-time secret scan | Build context scanned for leaked secrets before image creation |
| Budget enforcement | Token, wall-clock, and iteration limits kill runaway agents |
| Locked dependency installs | `uv` lockfiles only; no free-floating package names |
| Publisher identity | ECDSA P-256 keypair per publisher; bundles signed; fingerprint check via TOFU |
| Signed bundles | `.agentpaas` file: deterministic tar.gz with lock, policy, SBOM, source; cosign-signed image optional |
| Provenance chains | Each pack/fork appends a signed entry; chain verifies end to end; 32-entry cap |
| Policy deltas | Forked bundles show egress / credential / MCP tool changes per hop |
| Tamper-evident install | Post-install checks on lock signature, image digest, policy digest, source digest |
| Consent card | Receiver sees publisher, policy summary, provenance, and egress lints before install |
| Fork and redistribute | `agentpaas fork <ref> <dir>` makes an editable project; re-pack adds a `forked` provenance entry |

## Red-team smoke

Six attack fixtures through the real pack → run → gateway → audit path:

| Attack | Result |
|---|---|
| Network exfiltration | Blocked (gateway policy; topology isolation is primary) |
| DNS exfiltration | Blocked (internal network isolation) |
| File system access | Restricted (UID 64000, bind mounts) |
| Privilege escalation | Blocked (non-root container) |
| Process escape | Mitigated (Docker isolation, no new privileges) |
| Secret leakage | Prevented (Keychain broker, no env passthrough) |

Related docs:

- [How enforcement works](how-enforcement-works.md)
- [Threat model](threat-model.md)
- [Audit export](audit-export.md)
- [Trust model](trust-model.md)
