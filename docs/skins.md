# Host skins

Thin host support so a trial builder can author and operate AgentPaaS
from inside their own IDE. The skin is how you build and run the product,
not a second demo of one agent.

Buyer: the trial builder in Cursor, Codex, ChatGPT desktop, Grok Build,
or Claude Code. Not a ChatGPT-wrapper user.

From a shipped skin you can create, pack, deploy, invoke, and inspect any
component class the platform already productizes: agent, workflow, MCP
server, tool. Pack, policy.yaml, deploy/invoke as they exist today.

Weather is the first golden walk (the 0.4 path). It is not the ceiling.

## Inventory (this worktree, 0.4)

Hermes 0.4 host is the plugin, not an MCP config:

- `integrations/hermes-plugin/plugin.yaml`
- `docs/customer/trial/22-hermes-plugin.md`
- First-run walk: `docs/customer/trial/02-thirty-minute-path.md`

Do not change that Hermes walk.

`agentpaas-mcp` is the D72 name for the single MCP integration artifact.
This cut wraps that name in host config. It is not a new protocol and not
a per-host runtime.

What 0.4 brew actually installs (`Formula/agentpaas.rb`): `agentpaas`,
`agentpaasd`, `agentpaas-harness-linux`, `agentpaas-harness-linux-amd64`.
There is no `agentpaas-mcp` on that 0.4.2 formula. Build the dummy locally:

```
go build -o bin/agentpaas-mcp ./cmd/agentpaas-mcp
# or: make build  (also writes bin/agentpaas-mcp)
```

The dummy speaks MCP stdio (initialize / tools/list / ping→pong) so a host
can prove connect. It is not pack/deploy and not a per-host runtime.
`agentpaas cloud mcp call` talks to a *deployed* MCP server; it is not a
host skin.

## First run (same five steps in every skin)

Only the host config block differs.

1. Open the host (Cursor, Codex, ChatGPT desktop, Grok Build, or Claude Code).
2. Connect to `agentpaas-mcp` with the host snippet, **or** install the CLI.
3. Scaffold a component from template.
4. Pack.
5. Deploy, then invoke, then inspect the run/audit trail.

### 2a. MCP snippet (when `agentpaas-mcp` is on PATH)

Cursor: copy `templates/hosts/cursor.mcp.json` into `.cursor/mcp.json`
(project) or `~/.cursor/mcp.json` (user). Reload MCP servers.

Codex: merge `templates/hosts/codex.toml` into `~/.codex/config.toml`.
Restart Codex.

ChatGPT desktop: copy `templates/hosts/chatgpt-desktop.mcp.json`. Same
wrap (`command: agentpaas-mcp`, empty args). ChatGPT desktop Connectors /
Developer Mode talks to remote HTTPS MCP servers and does not launch a
local stdio command. Do not invent a bridge or URL runtime. If the app
exposes a stdio command/args field, this is the wrap. Otherwise use the
CLI path (2b). Do not claim a ChatGPT MCP-connect pass.

Grok Build: merge `templates/hosts/grok.toml` into `~/.grok/config.toml`
(user) or `.grok/config.toml` (project). Restart Grok / refresh MCP.

Claude Code: copy `templates/hosts/claude-code.mcp.json` into `.mcp.json`
(project) or merge the `mcpServers` object into `~/.claude.json` (user).
Restart the Claude Code session.

All five snippets run `command: agentpaas-mcp` with empty args. No extra
env. No per-host flags.

### 2b. CLI (working path on 0.4 brew)

```bash
brew tap AgentPaaS-ai/homebrew-tap
brew install agentpaas
export PATH="/opt/homebrew/bin:$PATH"
agentpaas version
agentpaas daemon start
agentpaas doctor
```

Mac (darwin/arm64). See `docs/customer/trial/21-install-macos.md`.
Do not change the Hermes 0.4 walk.

### 3-5. Any productized class

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

Workflow (not a deployment; no slot). CLI after brew 0.4.0:

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
platform gap, not an M18 gap: do not claim it.

## This cut (D148a)

Ships all five hosts: Cursor, Codex, ChatGPT desktop, Grok Build, Claude
Code. Same wrap. Live MCP-connect is still blocked on the missing
`agentpaas-mcp` brew binary (platform gap). CLI is the working first-run.

## Out of scope

- Windows local runtime (D148b)
- New MCP protocol
- Changing the Hermes 0.4 walk
- Console policy editor (skins use CLI/MCP only)
- Per-host runtimes or per-host features
- Framework goldens (Pydantic / LangGraph / CrewAI stay M16)
