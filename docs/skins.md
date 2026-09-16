# Host skins

Thin host support so a trial builder can author and operate AgentPaaS
from inside their own IDE. The skin is how you build and run the product,
not a second demo of one agent.

Buyer: the trial builder in Cursor or Codex (this cut). Not a
ChatGPT-wrapper user.

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
There is no `agentpaas-mcp` binary on that formula. `agentpaas cloud mcp
call` talks to a *deployed* MCP server; it is not a host skin.

If `agentpaas-mcp` is not on PATH, use the CLI path below. Do not invent
a server. Do not claim a live MCP-connect pass until that binary exists.
That absence is a platform gap, not an M18 gap.

## First run (same five steps in every skin)

Only the host config block differs.

1. Open the host (Cursor or Codex).
2. Connect to `agentpaas-mcp` with the host snippet, **or** install the CLI.
3. Scaffold a component from template.
4. Pack.
5. Deploy, then invoke, then inspect the run/audit trail.

### 2a. MCP snippet (when `agentpaas-mcp` is on PATH)

Cursor: copy `templates/hosts/cursor.mcp.json` into `.cursor/mcp.json`
(project) or `~/.cursor/mcp.json` (user). Reload MCP servers.

Codex: merge `templates/hosts/codex.toml` into `~/.codex/config.toml`.
Restart Codex.

Both snippets run `command: agentpaas-mcp` with empty args. No extra env.
No per-host flags.

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

Ships Cursor + Codex first. ChatGPT desktop, Grok Build, and Claude Code
wait if the five-host set misses about a week.

## Out of scope

- Windows local runtime (D148b)
- New MCP protocol
- Changing the Hermes 0.4 walk
- Console policy editor (skins use CLI/MCP only)
- Per-host runtimes or per-host features
- Framework goldens (Pydantic / LangGraph / CrewAI stay M16)
