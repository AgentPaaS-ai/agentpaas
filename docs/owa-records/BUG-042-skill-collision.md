# BUG-042 — Skill collision: two agentpaas-build SKILL.md files in profile

**Status:** OPEN  
**Severity:** P3 (UX / blocks skill loading)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (ap-testing session analysis)  

## Symptom

`skill_view("agentpaas-build")` fails with:

```
Ambiguous skill name 'agentpaas-build': 2 candidates —
  ~/.hermes/profiles/ap-testing/skills/agentpaas/SKILL.md
  ~/.hermes/profiles/ap-testing/skills/agentpaas/agentpaas-build-skills/SKILL.md
```

Agent wastes 6+ tool calls trying to resolve the collision (skill_manage, skill_view retries).

## Fix

- Ensure only one `agentpaas-build` skill per profile.
- Remove duplicate `agentpaas-build-skills/` subdirectory or merge into the parent skill.
- `make install-plugin` should not create a second skill pointer if one exists.

## Evidence

- Error log: `Skill name collision for 'agentpaas-build': 2 candidates`
- Session: 6 failed skill_manage / skill_view calls

## Related

- Plugin install creates skill pointer; if a nested skill dir also exists, collision
