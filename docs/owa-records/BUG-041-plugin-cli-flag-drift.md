# BUG-041 — Plugin CLI flag drift (export --project-dir, install --alias)

**Status:** OPEN  
**Severity:** P3 (UX / wasted tool calls)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (ap-testing session analysis)  

## Symptom

Plugin tools pass flags that the CLI doesn't support:

| Tool | Flag passed | CLI expects |
|------|-------------|------------|
| `agentpaas_export` | `--project-dir <path>` | positional `<path>` |
| `agentpaas_install` | `--alias <name>` | no such flag (alias set via `installed alias`) |

Each mismatch wastes 2–3 tool calls as the agent discovers the correct form.

## Fix

Sync plugin tool argument schemas to match CLI flag definitions. Or have plugin tools wrap CLI `--help` to discover flags dynamically.

## Evidence

- Session log: `agentpaas_export` failed twice with `unknown flag: --project-dir`
- Session log: `agentpaas_install` failed with `unknown flag: --alias`

## Related

- BUG-037 (PATH handoff), BUG-038 (fingerprint spaces) — same class: plugin ↔ CLI surface drift
