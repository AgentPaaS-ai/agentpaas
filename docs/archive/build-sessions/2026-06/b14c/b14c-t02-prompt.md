# Block 14C-T02: README Rewrite for v0.1.0 Release

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. The current
README.md is the planning README — it describes the project structure and
decomposition process. We need a complete rewrite for the v0.1.0 release that
follows the execution plan spec.

## What to Implement

Rewrite `README.md` to follow the spec:

### Structure (from execution plan)

1. **60-second story above the fold**
   - What AgentPaaS is in one sentence
   - The core value: governed, local-first runtime for AI agents
   - Key differentiators: policy enforcement, signed audit trail, zero telemetry

2. **Prerequisites**
   - macOS (darwin/arm64 or darwin/amd64)
   - Homebrew
   - Docker Desktop or Colima

3. **Quick Install**
   ```bash
   brew install agentpaas/tap/agentpaas
   agent doctor
   ```

4. **60-second Quickstart**
   ```bash
   # Start the daemon
   agent daemon start

   # Create a governed agent
   agent init my-agent
   cd my-agent

   # Pack it (builds container image)
   agent pack

   # Apply a policy
   agent policy apply --file policy.yaml

   # Run it (governed execution)
   agent run my-agent

   # Check the audit trail
   agent audit list --run <run-id>

   # View in dashboard
   open http://localhost:8090
   ```

5. **How Enforcement Works**
   - Brief explanation of the gateway topology (agent on internal network,
     gateway dual-homed, HTTP_PROXY routing)
   - Link to docs for deep dive

6. **Containment Table** (from redteam-smoke)
   - Table showing what's blocked and how

7. **Zero Telemetry Statement**
   - "Zero telemetry, zero phone-home by default"
   - No analytics, update checks, crash reports, or usage pings

8. **Known Limitations**
   - Link to docs/known-limitations.md
   - Brief list of P1 accepted limitations

9. **Repository Layout** (keep from existing README)

10. **License** (MIT)

### Key Messages

- **Governed**: Every agent action is policy-checked and audited
- **Local-first**: Runs entirely on your machine. No cloud dependency.
- **Observable**: Dashboard, audit trail, OTel traces
- **Verifiable**: Signed audit exports, cosign-signed releases, SBOMs

### Containment Table (example)

```
| Attack Vector         | Status     | How                          |
|-----------------------|------------|------------------------------|
| Network exfiltration  | Blocked    | Gateway policy enforcement   |
| DNS exfiltration      | Blocked    | Internal network isolation   |
| File system access    | Restricted | Container UID 64000, bind mounts |
| Privilege escalation  | Blocked    | Non-root container           |
| Process escape        | Mitigated  | Docker isolation, no new privs |
| Secret leakage        | Prevented  | Keychain broker, no env passthrough |
```

## Constraints

- Keep the repository layout section from the existing README.
- Do NOT remove links to PRD or execution plan (put them in a "Development" section).
- The README should be readable in a terminal (not overly formatted).
- Keep it under 200 lines — concise, not exhaustive.
- Link to docs/ for deep dives.
- The install command uses `brew install agentpaas/tap/agentpaas` (the tap doesn't
  exist yet, but the formula is in T01).
- Do NOT include demo video links (not recorded yet).
