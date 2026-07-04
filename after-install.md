# AgentPaaS Plugin Installed ✓

## Required next steps (don't skip these!)

### 1. Add the agentpaas toolset

The plugin is enabled, but its tools won't be visible to the agent until you add
the `agentpaas` toolset to your profile's platform toolsets:

```bash
hermes config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'
```

If you already have custom toolsets, append `"agentpaas"` to your existing list.

### 2. Restart Hermes

Run `/quit` in your Hermes session, then relaunch:

```bash
hermes -p <your-profile>
```

Plugins and toolsets load at process startup — not mid-session.

### 3. Verify after restart

```bash
hermes tools list | grep agentpaas    # should show ~30 tools
```

Ask the agent: "Run agentpaas_doctor to check setup health"
