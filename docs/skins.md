# Host skins

Thin host support so a trial builder can author and operate AgentPaaS
from inside their own IDE. The skin is how you build and run the product,
not a second demo of one agent.

The supported host path in 0.5.0 is Hermes. This page keeps reference material for customers who build their own CLI wrapper.

From a shipped skin you can create, pack, deploy, invoke, and inspect any
component class the platform already productizes: agent, workflow, MCP
server, tool. Pack, policy.yaml, deploy/invoke as they exist today.

Weather is the first golden walk. It is not the ceiling.
This page does not claim an in-host weather pass.

## Inventory

Hermes host is the plugin, not an MCP config:

- `integrations/hermes-plugin/plugin.yaml`
- `docs/customer/trial/22-hermes-plugin.md`
- First-run walk: `docs/customer/trial/02-thirty-minute-path.md`

Do not change the Hermes first-run walk.

`agentpaas-mcp` is the D72 name for the single MCP integration artifact.
It is a thin stdio adapter over the `agentpaas` CLI. Not a new protocol
and not a per-host runtime.

What 0.4 brew actually installs (`Formula/agentpaas.rb`): `agentpaas`,
`agentpaasd`, `agentpaas-harness-linux`, `agentpaas-harness-linux-amd64`.
Until the next brew cut ships `agentpaas-mcp`, put it on PATH:

```
go build -o bin/agentpaas-mcp ./cmd/agentpaas-mcp
export PATH="$PWD/bin:$PATH"
```

`agentpaas cloud mcp call` talks to a *deployed* MCP server; it is not a
host skin.

## First run (one gesture per host)

`agentpaas-mcp` must be on PATH. Login and secrets stay in a normal
terminal. After the gesture, confirm with the doctor tool in the host.

### Cursor

Click the install link (or paste it into a browser with Cursor installed):

```
cursor://anysphere.cursor-deeplink/mcp/install?name=agentpaas&config=eyJjb21tYW5kIjoiYWdlbnRwYWFzLW1jcCIsImFyZ3MiOltdfQ==
```

Cursor prompts to install. Reload MCP if tools do not appear.

Paste fallback: copy `templates/hosts/cursor.mcp.json` into
`.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (user). Same wrap:
`command: agentpaas-mcp`, empty args.

Source: https://cursor.com/docs/mcp/install-links
(`cursor://anysphere.cursor-deeplink/mcp/install?name=$NAME&config=$BASE64_ENCODED_CONFIG`,
config is JSON.stringify of `{"command","args"}` then base64).

### Claude Code

```
claude mcp add agentpaas -- agentpaas-mcp
```

New session shows the tools. `claude mcp list` should show Connected.

Paste fallback: copy `templates/hosts/claude-code.mcp.json` into
`.mcp.json` (project) or merge the `mcpServers` object into
`~/.claude.json` (user). Restart the session.

Source: https://code.claude.com/docs/en/mcp-quickstart
(local stdio: `claude mcp add <name> -- <command>`).

### Grok Build

```
grok mcp add agentpaas -- agentpaas-mcp
```

Restart or `/mcps` refresh. Tools appear as `agentpaas__<tool>`.

Config-merge floor: merge `templates/hosts/grok.toml` into
`~/.grok/config.toml` (user) or `.grok/config.toml` (project).

Source: https://docs.x.ai/build/features/mcp-servers
(`grok mcp add <name> -- <command>` is the one-command add; toml is the floor).

All three wraps run `command: agentpaas-mcp` with empty args. No extra
env. No per-host flags. No second harness.

## CLI (working path on 0.4 brew)

```bash
brew tap AgentPaaS-ai/homebrew-tap
brew install agentpaas
export PATH="/opt/homebrew/bin:$PATH"
agentpaas version
agentpaas daemon start
agentpaas doctor
```

Mac (darwin/arm64). See `docs/customer/trial/21-install-macos.md`.
See the current install guide.

## Any productized class

Same verbs. Swap the class. Do not invent a sixth host story.

Agent (weather is the first golden walk, not the only agent):

```bash
agentpaas init ./my-agent --runtime python --noninteractive
# add policy.yaml / secrets as the 0.4 path already documents
agentpaas pack . --target linux/amd64
agentpaas cloud push --lock "$LOCK"
agentpaas cloud deploy latest
# invoke + logs/audit as in docs/customer/golden-path.md
```

Workflow (not a deployment; no slot). CLI after brew 0.5.0:

```bash
agentpaas cloud workflow create
agentpaas cloud workflow start
agentpaas cloud workflow instance
```

Detail: `docs/customer/trial/24-workflows.md`. Weather is not this path.

MCP server:

```bash
# agent.yaml kind: mcp_service (see integrations/hermes-plugin/SKILL.md)
agentpaas pack . --target linux/amd64
agentpaas cloud push --lock "$LOCK"
agentpaas cloud deploy latest --type mcp
```

Tool:

```bash
agentpaas pack . --target linux/amd64
agentpaas cloud push --lock "$LOCK"
agentpaas cloud deploy latest --type tool
```

Kinds: `docs/customer/trial/25-mcp-and-tools.md`. An agent calling a
deployed MCP or tool is still not a workflow.

If a class has no working pack to deploy to invoke path, that is a
platform gap: do not claim it.

## 0.5.0 support boundary

Hermes is the documented and supported host path for 0.5.0. The CLI remains the product surface. Customers can configure another host to invoke the CLI, but those host wrappers are customer customization and are not verified AgentPaaS integrations in this release.

The examples below are reference material for customers who choose to build their own wrapper. They are not a support claim or a release acceptance path.

## Out of scope

- Windows local runtime (D148b)
- New MCP protocol
- Changing the Hermes 0.4 walk
- Console policy editor (skins use CLI/MCP only)
- Per-host runtimes or per-host features
- Framework-specific integrations are outside the supported 0.5.0 host path
- Host-specific setup for Cursor, Claude Code, and Grok Build as a supported 0.5.0 path
- `agentpaas-mcp` as a supported 0.5.0 integration
