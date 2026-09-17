# M18+ T7 — weather golden through Claude Code CLI

Date: 2026-09-16 (PDT)
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t7-claude-weather
Branch: feat/m18plus-t7-claude-weather
HEAD at start: d15c919 (T6 Cursor weather evidence already on this branch; not merged)

brew PATH mcp via Claude Code CLI install (`claude mcp add agentpaas -- agentpaas-mcp`); not in-host GUI; not T4 stdio; not T5 Grok; not T6 Cursor; no GitHub 0.4.4; no tap push.

This T is Claude Code CLI, not Grok, not Cursor, not raw stdio. This T is not an in-host GUI claim. Dummy ping is not this story.
No nested hermes. No Claude Code GUI / Grok / Cursor founder sit. T5 grok was not re-run as a substitute. T6 Cursor was not re-run as a substitute. T4 stdio was not re-run as a substitute.
MCP tools named login/logout/secret* were not called.
Raw `/opt/homebrew/bin/agentpaas-mcp` JSON-RPC stdio was not used to drive weather.
`claude auth login` / `claude setup-token` were not run. API keys were not passed.

Claude CLI: not found. `command -v claude` empty (exit 1). Not at `/opt/homebrew/bin/claude`, `/usr/local/bin/claude`, or `~/.local/bin/claude`. No `/Applications` Claude.app. No `~/.claude.json`. No `~/.claude`.

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

## Claude CLI lookup

Primary: `command -v claude`; common PATH `/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin`.

```
$ command -v claude
```

empty stdout; process exit 1.

```
$ PATH=/opt/homebrew/bin:/usr/bin:/bin which claude
```

empty stdout; process exit 1.

```
$ type claude
/bin/bash: line 41: type: claude: not found
```

```
$ ls -la /opt/homebrew/bin/claude /usr/local/bin/claude /Users/pms88/.local/bin/claude
ls: /Users/pms88/.local/bin/claude: No such file or directory
ls: /opt/homebrew/bin/claude: No such file or directory
ls: /usr/local/bin/claude: No such file or directory
```

```
$ ls /Applications | grep -i claude
```

empty stdout.

```
$ ls -la /Users/pms88/.claude.json /Users/pms88/.claude
ls: /Users/pms88/.claude: No such file or directory
ls: /Users/pms88/.claude.json: No such file or directory
```

Stopped lookup. Did not install Claude CLI. Did not open Claude Code GUI for a human.

## Claude T2 gesture

Primary: `claude mcp add agentpaas -- agentpaas-mcp` (skins.md / templates/hosts/claude-code.mcp.json).

Add:

```
$ claude mcp add agentpaas -- agentpaas-mcp
/bin/bash: line 62: claude: command not found
```

process exit 127.

List:

```
$ claude mcp list
/bin/bash: line 66: claude: command not found
```

process exit 127.

Help:

```
$ claude -h
/bin/bash: line 70: claude: command not found
```

process exit 127.

Stopped that step. Did not invent Connected. Did not paste `templates/hosts/claude-code.mcp.json` into project `.mcp.json` (claude CLI missing; add/list cannot prove install). Did not merge into `~/.claude.json` (file does not exist; would not be a Claude CLI proof). Did not open Claude Code GUI. Restart is not a GUI sit; CLI-only — CLI was not present.

Doctor connectivity is not a weather-path pass. Dummy ping is not this story.

## Doctor (or equivalent) against that server

Prefer driving the installed doctor tool through Claude. If Claude has `mcp get` / `mcp list-tools`, record that too.

Claude CLI missing. `claude mcp list` / `claude mcp get` / `claude mcp list-tools` not reachable.

```
$ claude mcp list
/bin/bash: line 66: claude: command not found
```

process exit 127.

Stopped. Did not invent Connected. Did not invent doctor tool output. Did not pipe JSON-RPC to agentpaas-mcp.

## Drive weather through Claude-installed tools

Required: from demo/weather-agent, call Claude-installed tools (names like mcp__agentpaas__doctor / agentpaas__doctor / doctor on server agentpaas): doctor, validate args=["."], pack --target linux/amd64, then push/deploy/invoke as far as the bound.
Do not call MCP login/logout/secret*. Do not bypass Claude with raw agentpaas-mcp stdio. Do not pass API keys. Do not run `claude auth login` / `claude setup-token`.

Claude CLI was not present. Stopped before `claude -p`.

```
$ claude -h
/bin/bash: line 70: claude: command not found
```

process exit 127.

Stopped. Did not run `claude auth login` / `claude setup-token` (would open browser / block). Did not pass API keys. Did not pipe JSON-RPC to agentpaas-mcp. Did not edit the mcp binary. Did not invent tool results. Did not sit founder in Claude Code GUI.

Weather path not reached:

- mcp__agentpaas__doctor / agentpaas__doctor / doctor: not called (claude CLI not found)
- agentpaas__validate args=["."]: not called
- agentpaas__pack --target linux/amd64: not called
- cloud push/deploy/invoke: not called
- No ten_5e4aea6f. Demo ten_91ea8938 not queried this T (whoami/status not called through Claude-installed MCP).

demo/weather-agent is present in this worktree (agent.yaml, main.py, policy.yaml, requirements.txt) but was not driven.

## Not done

- No weather golden through Claude-installed tools (blocked on missing claude CLI)
- No Grok claim
- No Cursor claim
- No in-host GUI claim
- Not T4 stdio
- Not T5 Grok
- Not T6 Cursor
- No git push / gh pr
- No GitHub 0.4.4 release or asset
- No tap push / retag / Formula publish
- No nested hermes
- cmd/agentpaas-mcp, Formula, .goreleaser.yaml, harness, cloud src, integrations/hermes-plugin, skins.md, templates not edited
- MCP login/logout/secret* not called
- Dummy ping not presented as the story
