---
name: OWA Build Task
about: AgentPaaS OWA loop task (Orchestrator-Worker-Verifier-Adversary)
title: "B<N>-T<NN>: <task title>"
labels: []
---

## Task: B<N>-T<NN>

**Block:** N
**Gate:** `make blockN-gate`
**Adversary:** Required / Not required

### Goal
<task description>

### Scope
- <what to build>

### Non-goals
- <what not to build>

### Acceptance Criteria
- [ ] <criterion 1>
- [ ] <criterion 2>

### Build Session Routing
```yaml
build_session:
  block: N
  orchestrator: {primary: z-ai/glm-5.2, fallback: deepseek-v4-pro}
  worker: {primary: deepseek-v4-flash, fallback: composer-2.5}
  verifier: {primary: composer-2.5, fallback: z-ai/glm-5.2}
  adversary: {primary: grok-4.3, fallback: z-ai/glm-5.2}
  adversary_required: true|false
```

### Attempt Log
Every role pass must record an attempt log:
```yaml
attempt: 1
role: worker|verifier|adversary|orchestrator
model: <id>
fallback_used: true|false
result: pass|fail|blocked|needs_orchestrator|accepted|refine|reject
gate: <command or null>
commands_run: [...]
files_touched: [...]
next_recommendation: continue|retry_worker|switch_fallback|split_issue|rescope|invoke_adversary|founder_decision
```