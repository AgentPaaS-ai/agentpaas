# AgentPaaS Checkpoint

Date/time: 2026-06-12 00:10:39 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the final whole-plan consistency pass requested after
the Block 15 sequencing review and before Block 1 implementation.

The review covered:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- latest checkpoint context, especially
  `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-12_00-03-18_PDT.md`

Latest committed checkpoint entering this review:
- `260e56f docs: close block 15 sequencing review`

## Review Outcome

The plan is now internally consistent and implementation-ready after the small
documentation fixes in this checkpoint.

Locked strategy remains intact:
- P1 is macOS-first OSS/demo delivery, not the full customer-facing commercial
  release.
- P1 has zero telemetry.
- P1 target is week 4/5.
- P2 is the first customer-facing release track over the following four weeks.
- Blocks 1-14 are product/release build blocks.
- Block 15 is sequencing, founder calendar, and execution control.
- Former Block 10.5 is now Block 11.
- Block 12 is redteam-smoke.
- Block 13 is MCP server + Claude Code plugin + Hermes native MCP skill.
- Block 14 is install/docs/demo/release.
- No Block 13 integration items are skipped for P1.
- CrewAI is only a generated Python input shape through the generic Python
  harness, not a custom adapter.
- Node SDK, Linux, WSL2, deb/rpm, systemd, libsecret, customer-facing
  fleet/team/commercial features, and telemetry remain P2 or later.

## Findings Fixed

1. **Gate command ambiguity before implementation.**
   The execution plan said every gate command lives in the Makefile, but several
   block success gates used prose or raw commands instead of canonical Make
   targets, and Block 1 still listed `redteam` while later release gates use
   `redteam-smoke`.

   Fix:
   - Added §0.2.2a with canonical `make blockN-gate` wrappers for Blocks 1-15.
   - Updated every block success gate to cite its canonical wrapper.
   - Clarified that `make block12-gate` wraps `make redteam-smoke`.
   - Clarified Block 15's pre-Block-1 docs-only gate and post-Block-1
     `make block15-gate`.
   - Updated the Phase 1 Definition of Done to require command exit 0 plus
     recorded evidence.

2. **Stale P1 Linux diagnostic wording.**
   Block 2's doctor prompt still grouped Docker Desktop, Colima, and Linux
   `dockerd` together in a way that could be misread as a P1 Linux support gate.

   Fix:
   - Reworded Block 2 so Docker Desktop/Colima are the macOS P1 check and Linux
     `dockerd` is reported as P2/not-a-P1-gate.

3. **Stale Node hint in the P1 architecture diagram.**
   The PRD component diagram still said user code execution was "Py / Node",
   while Node SDK/package support is explicitly deferred.

   Fix:
   - Changed the diagram to "Python P1".

4. **Installer trust wording.**
   The PRD technology table still said "brew/curl install", while Block 14
   explicitly rejects blind `curl|bash`.

   Fix:
   - Changed the table to "simple Homebrew/macOS install".

5. **MCP configuration shape inconsistency.**
   PRD §2.7.2 said MCP servers were declared in `mcp.yaml`, while the normative
   policy schema and examples use `policy.yaml` with `mcp_servers`.

   Fix:
   - Reworded the MCP client policy text to declare local/remote MCP servers in
     `policy.yaml`'s `mcp_servers` section.

6. **Stale PRD red-team cross-reference.**
   PRD §8 pointed to `§3.2.4`, but the red-team statement lives in §3.2 item 4.

   Fix:
   - Corrected the reference to `§3.2 item 4`.

## Review Notes

No blocking inconsistencies remain in the active PRD or execution plan.

Remaining search hits were reviewed and are intentional:
- "Former Block 10.5 is now Block 11" appears only as a renumbering note or as
  PRD subsection `2.10.5`, not as live block numbering.
- "monthly" appears only in post-P2 channel work.
- "weekly-active-runtime" appears only as a future opt-in telemetry note outside
  the P1 launch path.
- Linux, WSL2, deb/rpm, systemd, libsecret, fleet/team/commercial features, and
  telemetry are consistently marked P2 or later.
- Older checkpoint files before 00:03 may contain historical pre-renumbering
  language, but the latest checkpoint and active plans supersede them.

## Verification

Commands run:
- `git log --oneline -5` confirmed `260e56f docs: close block 15 sequencing review`
  as HEAD entering the pass.
- `rg` scans for stale block numbering, Linux P1 wording, redteam 10/10,
  telemetry assumptions, generic MCP tool names, Node/Linux/P2 scope drift, and
  Makefile gate consistency.
- `git diff --check` passed before this checkpoint was written.

Next concrete Block 1 action:
- Create the monorepo skeleton and Makefile namespace, starting with
  `make block1-gate` wrapping `make proto build test`, then author the trigger
  and control protobuf contracts from Block 1.
