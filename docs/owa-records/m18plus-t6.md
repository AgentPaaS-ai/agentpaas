# M18+ T6 — weather golden through Cursor CLI

Date: 2026-09-16 (PDT)
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t6-cursor-weather
Branch: feat/m18plus-t6-cursor-weather
HEAD at start: 46d2a43 (T5 Grok weather evidence already on this branch; not merged)

brew PATH mcp via Cursor CLI install (`cursor --add-mcp`); not in-host GUI; not T4 stdio; not T5 Grok; no GitHub 0.4.4; no tap push.

This T is Cursor CLI, not Grok, not Claude Code, not raw stdio. This T is not an in-host GUI claim. Dummy ping is not this story.
No nested hermes. No Cursor GUI / Grok / Claude Code founder sit. T5 grok was not re-run as a substitute. T4 stdio was not re-run as a substitute.
MCP tools named login/logout/secret* were not called.
Raw `/opt/homebrew/bin/agentpaas-mcp` JSON-RPC stdio was not used to drive weather.
`cursor agent login` was not run. `--api-key` was not passed.

Cursor: 3.19.19 (6496ea8a068aebfcd21990e70ff522e9abf10c80, arm64) at `/Applications/Cursor.app/Contents/Resources/app/bin/cursor`.
Cursor Agent CLI: 2026.09.15-d2fe57e.

## Binary (PATH=/opt/homebrew/bin:/usr/bin:/bin)

Must NOT be ~/.local/bin/agentpaas-mcp.

```
$ export PATH=/opt/homebrew/bin:/usr/bin:/bin
$ which agentpaas-mcp
/opt/homebrew/bin/agentpaas-mcp

$ agentpaas version
CLI: 0.4.4 | Proto: v1 | Commit: adf314591af12dcbd5e6d7e9963731358b93c7b3 | OS/Arch: darwin/arm64 | Docker: colima | Docker API: 29.5.2
```

Binary is a Homebrew link, not a host-local build:

```
$ ls -la /opt/homebrew/bin/agentpaas-mcp /opt/homebrew/bin/agentpaas
lrwxr-xr-x@ 1 pms88  admin  39 Sep 16 18:04 /opt/homebrew/bin/agentpaas -> ../Cellar/agentpaas/0.4.4/bin/agentpaas
lrwxr-xr-x@ 1 pms88  admin  43 Sep 16 18:04 /opt/homebrew/bin/agentpaas-mcp -> ../Cellar/agentpaas/0.4.4/bin/agentpaas-mcp
```

## Cursor install gesture

Primary: `cursor --add-mcp '{"name":"agentpaas","command":"agentpaas-mcp","args":[]}'` (CLI equivalent of deeplink / paste `templates/hosts/cursor.mcp.json`).
Floor paste into project `.cursor/mcp.json` or `~/.cursor/mcp.json` was not used; add succeeded (wrote user-profile settings). Cursor GUI was not opened for a human.

List before add:

```
$ cursor agent mcp list
No MCP servers configured (expected in .cursor/mcp.json or ~/.cursor/mcp.json)
```

list-tools before add:

```
$ cursor agent mcp list-tools agentpaas
Failed to list tools: Failed to load MCP 'agentpaas': MCP client "agentpaas" not found in config
```

Add:

```
$ cursor --add-mcp '{"name":"agentpaas","command":"agentpaas-mcp","args":[]}'
Added MCP servers: agentpaas
```

process exit 0.

User-profile block written (not invented). Cursor `--add-mcp` wrote `~/Library/Application Support/Cursor/User/settings.json`:

```
"mcp": {
	"servers": {
		"agentpaas": {
			"command": "agentpaas-mcp",
			"args": []
		}
	}
}
```

Matches `templates/hosts/cursor.mcp.json` (`command: agentpaas-mcp`, empty args). No `~/.cursor/mcp.json` exists. Worktree `.cursor/mcp.json` was not created.

## cursor agent mcp list / list-tools (doctor equivalent)

`cursor agent mcp list` reads `.cursor/mcp.json` or `~/.cursor/mcp.json`, not the user-profile `settings.json` block `--add-mcp` wrote.

List after add:

```
$ cursor agent mcp list
No MCP servers configured (expected in .cursor/mcp.json or ~/.cursor/mcp.json)
```

process exit 0.

list-tools after add:

```
$ cursor agent mcp list-tools agentpaas
Failed to list tools: Failed to load MCP 'agentpaas': MCP client "agentpaas" not found in config
```

process exit 1.

Stopped that step. Did not invent Connected. Did not paste `templates/hosts/cursor.mcp.json` (add did not refuse). Did not run `cursor agent mcp login` (MCP OAuth, not AgentPaaS login). Did not open Cursor GUI.

Doctor connectivity is not a weather-path pass. Dummy ping is not this story.

## Drive weather through Cursor-installed tools

Required: from demo/weather-agent, call Cursor-installed tools (names like agentpaas__doctor / doctor on server agentpaas): doctor, validate args=["."], pack --target linux/amd64, then push/deploy/invoke as far as the bound.
Do not call MCP login/logout/secret*. Do not bypass Cursor with raw agentpaas-mcp stdio. Do not pass `--api-key`. Do not run `cursor agent login`.

Cursor Agent CLI was not signed in. Stopped before `cursor agent --print`.

```
$ cursor agent status
Not logged in
```

```
$ cursor agent whoami
Not logged in
```

```
$ cursor agent about
About Cursor CLI

CLI Version         2026.09.15-d2fe57e
Latest              2026.09.15-d2fe57e (up to date)
Model               Auto
Subscription Tier   Unknown
OS                  darwin (arm64)
Terminal            tmux
Shell               zsh
User Email          Not logged in
```

Stopped. Did not run `cursor agent login` (would open browser / block). Did not pass `--api-key` / `CURSOR_API_KEY`. Did not pipe JSON-RPC to agentpaas-mcp. Did not edit the mcp binary. Did not invent tool results. Did not sit founder in Cursor GUI.

Weather path not reached:

- agentpaas__doctor / doctor: not called (cursor agent not signed in; mcp list did not see the server)
- agentpaas__validate: not called
- agentpaas__pack --target linux/amd64: not called
- cloud push/deploy/invoke: not called
- No ten_5e4aea6f. Demo ten_91ea8938 not queried this T (whoami not called through Cursor-installed MCP).

demo/weather-agent is present in this worktree (agent.yaml, main.py, policy.yaml, requirements.txt) but was not driven.

## Not done

- No weather golden through Cursor-installed tools (blocked on cursor agent sign-in; CLI mcp list did not see user-profile --add-mcp)
- No Grok claim
- No Claude Code claim
- No in-host GUI claim
- Not T4 stdio
- Not T5 Grok
- No git push / gh pr
- No GitHub 0.4.4 release or asset
- No tap push / retag / Formula publish
- No nested hermes
- cmd/agentpaas-mcp, Formula, .goreleaser.yaml, harness, cloud src, integrations/hermes-plugin, skins.md, templates not edited
- MCP login/logout/secret* not called
- Dummy ping not presented as the story
