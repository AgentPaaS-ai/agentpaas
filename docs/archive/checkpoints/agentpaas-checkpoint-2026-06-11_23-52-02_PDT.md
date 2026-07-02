# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:52:02 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 13 install/docs/demo/release review and the
follow-up clarification that Phase 1 is not the full customer-facing
commercial release. P1 is now explicitly scoped as a macOS-first OSS/demo
delivery used to prove the wedge, create credible demos, publish verifiable
open-source artifacts, and gather design-partner feedback without telemetry.
P2 is the first customer-facing release track.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-52-02_PDT.md`

Latest committed checkpoint entering this review:
- `a1622f6 docs: close block 12 integration review`

## Block 13 Review Outcome

Block 13 now frames v0.1.0 as a trusted OSS/demo release rather than a
customer-facing commercial launch.

Decisions locked:
- P1 is macOS-first.
- P1 primary install path is Homebrew for darwin/arm64 and darwin/amd64.
- Linux native packages, Linux CI release certification, deb/rpm packaging,
  Windows/WSL2 docs, and Linux libsecret/systemd support move to P2 unless a
  design partner creates a hard pre-launch requirement.
- Docker Desktop or Colima is an explicit prerequisite for the P1 README
  activation gate.
- Homebrew installs the binary but does not silently start background
  services.
- First run is explicit through `agent doctor`, then `agent setup launchd` or
  documented `brew services start agentpaas`.
- Release verification follows Sigstore keyless best practice using GitHub
  Actions OIDC, with a documented `cosign verify-blob` command and easier
  `agent verify-release` helper.
- P1 includes a macOS air-gapped/offline verification bundle.
- P1 has zero telemetry: no analytics, update checks, crash reports, usage
  pings, or automatic adoption metrics leave the machine.
- Any future telemetry must be separate, explicit opt-in, and absent from the
  P1 demo path.
- P1 requires one recorded Block 12 differentiation demo; the secret-brokered
  SaaS action and agentic repair loop are stretch launch demos.
- P2 is the first customer-facing release track: Linux certification,
  fleet/team management, enterprise packaging, support posture, and commercial
  observability.

## Added To Block 13

Block 13 now includes:
- macOS-first release scope.
- No blind `curl|bash` installer posture.
- explicit first-run `agent doctor` checks for Docker Desktop/Colima,
  keychain, loopback ports, daemon socket permissions, release signature
  status, and dashboard port.
- user-visible launchd setup semantics.
- goreleaser darwin artifacts, checksums, SBOMs, provenance, and cosign
  signatures.
- docs for quickstart, policy, secrets, enforcement topology, threat model,
  known limitations, audit verification, privacy/telemetry, Claude Code plugin
  setup, Hermes native MCP setup, and demo scripts.
- README requirements: 60-second story, `make redteam-smoke` containment proof,
  zero telemetry statement, prerequisites, and known limitations link.
- macOS offline bundle with signed binaries, checksums, SBOMs, container
  images, policy/demo fixtures, and verification instructions.
- docs/release CI: broken-link check, command-snippet smoke scripts, README
  quickstart smoke, release-artifact verification matrix, screenshot/asciinema
  freshness check, and docs issue filing for clean-machine deviations.
- uninstall and upgrade edge cases, including daemon-state migration rollback
  or manual recovery path.

## PRD Alignment

The PRD now mirrors the Block 13 decisions:
- Product definition says P1 is OSS/demo proof and P2 is the first
  customer-facing release track.
- P1 non-goals exclude Linux-native, Windows-native, and WSL2 support.
- Local runtime conventions are macOS Docker Desktop/Colima only in P1.
- launchd is P1; systemd is P2.
- macOS Keychain is P1; Linux libsecret is P2.
- P1 hardening is macOS Docker Desktop/Colima container hardening; certified
  Linux seccomp/AppArmor profiles are P2.
- GTM metrics no longer depend on automatic telemetry.
- Phase 1 success criteria now require two clean macOS volunteer machines, P1
  redteam-smoke 6/6, post-install Claude Code and Hermes deploy demos under
  10 minutes, and offline bundle verification.

## Current Open Items

Recommended next item:
Decide whether the current planning pass is complete enough to begin Block 1
implementation, or whether to do one final must-ship/can-slip pass across the
whole execution plan before coding.

Other open items still relevant:
1. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
2. Policy schema reference: required/optional fields, defaults, unknown
   fields, wildcard behavior, CIDR/private-network behavior, credential
   binding behavior, MCP declarations.
3. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
4. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
5. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:52:02 PDT. We
are reviewing the execution plan before implementation. Git is initialized
and checkpoints are committed. Latest commit should be the checkpoint closeout
after Block 13. P1 is now explicitly macOS-first OSS/demo delivery with zero
telemetry; P2 is the first customer-facing release track. Next, decide whether
to start Block 1 implementation or do a final must-ship/can-slip pass across
the whole plan."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
