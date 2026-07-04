# AgentPaaS Plugin Installed — Complete Setup

The plugin is now installed and enabled. Two more steps are REQUIRED
before the agentpaas_* tools will work:

## Step 1: Add the agentpaas toolset (REQUIRED)

The `hermes plugins install --enable` command does not automatically
add the plugin's toolset to the platform configuration. You MUST run
this command to make the tools available:

```bash
hermes -p <profile> config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'
```

If the profile already has custom toolsets, append "agentpaas" to the
existing list instead of replacing it.

## Step 2: Restart required

Plugin and toolset changes load at process startup, not mid-session.
The user must restart Hermes for the changes to take effect:

```
Quit this session and run: hermes -p <profile>
```

## Step 3: Verify

After restart, verify the tools are available by asking:
"Run agentpaas_doctor to check if my AgentPaaS setup is healthy"