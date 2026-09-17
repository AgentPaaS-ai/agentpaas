# M18+ T5 — weather golden through Grok Build (grok CLI MCP)

Date: 2026-09-16 (PDT)
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t5-grok-weather
Branch: feat/m18plus-t5-grok-weather
HEAD at start: 8ddedcc (T4 brew mcp weather linux/amd64 pack evidence already on this branch; not merged)

brew PATH mcp via Grok CLI install (`grok mcp add`); not in-host; not T4 stdio; no GitHub 0.4.4; no tap push.

This T is Grok CLI, not raw stdio. This T is not an in-host claim. Dummy ping is not this story.
No nested hermes. No Cursor / Claude Code founder sit. T4 stdio was not re-run as a substitute.
MCP tools named login/logout/secret* were not called.
Raw `/opt/homebrew/bin/agentpaas-mcp` JSON-RPC stdio was not used to drive weather.

Grok CLI: 0.2.99 (b1b49ccb71a7) at `/Users/pms88/.local/bin/grok` -> `/Users/pms88/.grok/bin/grok`.

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

## Grok install gesture

Primary: `grok mcp add agentpaas -- agentpaas-mcp` (skins.md / templates/hosts/grok.toml).
Floor merge was not needed; add succeeded.

List before add:

```
$ grok mcp list
No MCP servers configured. Run `grok mcp add --help` to get started.
```

Add:

```
$ grok mcp add agentpaas -- agentpaas-mcp
Added stdio MCP server 'agentpaas' with command: agentpaas-mcp to user config
File modified: ~/.grok/config.toml
```

List after add:

```
$ grok mcp list
  agentpaas: agentpaas-mcp
```

User config block written (not invented):

```
[mcp_servers.agentpaas]
command = "agentpaas-mcp"
args = []
```

Matches templates/hosts/grok.toml.

## grok mcp doctor

```
$ grok mcp doctor agentpaas

MCP Doctor

  Config sources
    ~/.grok/config.toml                      1 server
    ~/.claude.json                           not found
    .mcp.json                                not found
    grok.com                                 skipped (auth expired — run `grok login`)

  agentpaas (stdio: agentpaas-mcp)
    ✓ command found (/opt/homebrew/bin/agentpaas-mcp)
    ✓ server started (0.0s)
    ✓ handshake OK (protocol 2024-11-05)
    ✓ 15 tools discovered

Found 1 healthy, 0 failing.
```

`grok inspect` from demo/weather-agent also showed:

```
  MCP Servers (1)
  └ agentpaas (stdio)  config
```

15 tools discovered. Dummy ping is not this story. Doctor connectivity is not a weather-path pass.

## Drive weather through Grok-installed tools

Required: from demo/weather-agent, call Grok-installed tools (names like agentpaas__doctor): doctor, validate args=["."], pack --target linux/amd64, then push/deploy/invoke as far as the bound.
Do not call MCP login/logout/secret*. Do not bypass grok with raw agentpaas-mcp stdio.

Grok CLI was not signed in. Headless `-p` refused before any MCP tool call.

```
$ export PATH=/Users/pms88/.local/bin:/Users/pms88/.grok/bin:/opt/homebrew/bin:/usr/bin:/bin
$ cd /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t5-grok-weather/demo/weather-agent
$ grok --cwd /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t5-grok-weather/demo/weather-agent \
  --always-approve --no-subagents --no-plan --max-turns 6 \
  --output-format json \
  --verbatim \
  -p "Call the MCP tool agentpaas__doctor with empty arguments. Print the raw tool result. Do not call login, logout, or secret tools. Do not run the agentpaas shell CLI."
{"type":"error","message":"Not signed in. To authenticate without a browser, run:\n  grok login --device-code\n\nAlternatively, set the XAI_API_KEY environment variable or run `grok login` on a machine with a browser."}
Error: Not signed in. To authenticate without a browser, run:
  grok login --device-code

Alternatively, set the XAI_API_KEY environment variable or run `grok login` on a machine with a browser.
```

process exit 1.

Stopped. Did not run `grok login` / `--device-code` (would block). Did not set XAI_API_KEY. Did not pipe JSON-RPC to agentpaas-mcp. Did not edit the mcp binary. Did not invent tool results.

Weather path not reached:

- agentpaas__doctor: not called (grok -p not signed in)
- agentpaas__validate: not called
- agentpaas__pack --target linux/amd64: not called
- cloud push/deploy/invoke: not called
- No ten_5e4aea6f. Demo ten_91ea8938 not queried this T (whoami not called through grok).

## Not done

- No weather golden through Grok-installed tools (blocked on grok CLI sign-in)
- No Cursor claim
- No Claude Code claim
- No in-host claim
- Not T4 stdio
- No git push / gh pr
- No GitHub 0.4.4 release or asset
- No tap push / retag / Formula publish
- No nested hermes
- cmd/agentpaas-mcp, Formula, .goreleaser.yaml, harness, cloud src, integrations/hermes-plugin, skins.md, templates not edited
- MCP login/logout/secret* not called
- Dummy ping not presented as the story
