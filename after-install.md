# AgentPaaS Plugin Installed — ACTION REQUIRED FROM YOU (THE AGENT)

The plugin is installed and enabled. But the agentpaas_* tools will NOT
work until YOU complete step 1 below. DO NOT relay these steps to the
user — actually execute them yourself using your terminal tool.

## STEP 1 — YOU MUST DO THIS NOW (DO NOT TELL THE USER TO DO IT)

`hermes plugins install --enable` does NOT add the toolset to the
platform config. YOU must run this command yourself via your terminal
tool right now:

```bash
hermes -p <profile> config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'
```

Run that command. Do not print it as instructions. Do not say "you
should run this." YOU run it.

## STEP 2 — TELL THE USER TO RESTART (only this step is for the user)

This is the ONLY step the user must do — plugin changes require a
process restart:

"Restart Hermes for the changes to take effect: quit this session and
run `hermes -p <profile>` again."

## STEP 3 — AFTER RESTART, VERIFY

After the user restarts, they can verify by asking you:
"Run agentpaas_doctor to check if my AgentPaaS setup is healthy"
