# AgentPaaS post-install

After `hermes plugins install --force --enable` completes, resolve `<profile>`
from `HERMES_HOME`, `hermes profile`, or the installed path, then run:

```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/complete-install.py <profile>
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/verify-installed-state.py <profile>
```

The second command is a hard gate. If it exits non-zero, installation is not
complete; fix the reported state and rerun both commands. Do not reinstall.

Filesystem setup (toolset, full skill, and SOUL.md) completes on disk without
a restart. Slash commands and `agentpaas_*` tools require one Hermes session
reopen because plugins load at session start:

```text
/quit
hermes -p <profile>
```

AgentPaaS install finished on disk. Reopen this Hermes session once so slash
commands load. Do NOT reinstall. Do NOT ask for OpenRouter or other LLM/API
keys yet. After reopen, run `/agentpaas-doctor`.
