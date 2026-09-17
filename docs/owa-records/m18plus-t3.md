# M18+ T3 — local brew 0.4.4 with agentpaas-mcp

Date: 2026-09-16
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t3-brew-044
Branch: feat/m18plus-t3-brew-0.4.4-local
HEAD base: adf314591af12dcbd5e6d7e9963731358b93c7b3

local 0.4.4 only; no GitHub release; no tap push; live tap left at 0.4.2.

## Scope

- .goreleaser.yaml: fifth darwin binary agentpaas-mcp; archives.default.ids; homebrew_casks.binaries; post.install xattr list.
- Formula/agentpaas.rb: bin.install + post_install xattr names include agentpaas-mcp; test assert_match 0.4.2. Version/url/sha256 stay GitHub 0.4.2.
- Local Mac (arm64) brew formula 0.4.4 from file:// tarball. Live tap Casks/agentpaas.rb not edited.

## Staged tarball

```
VER=0.4.4
COMMIT=adf314591af12dcbd5e6d7e9963731358b93c7b3
SHA=5f13385df193ad172392a0917eb8bc226aa8314bacabb69d29303d3979505a1f
/tmp/agentpaas_0.4.4_darwin_arm64.tar.gz
contents: agentpaas agentpaasd agentpaas-mcp agentpaas-harness-linux agentpaas-harness-linux-amd64
```

Local formula: /tmp/agentpaas-0.4.4.rb (file:// tarball, version 0.4.4).

Homebrew 7.0.1-5-g66413cb rejects a path formula unless HOMEBREW_DEVELOPER=1, and maps class name from filename. Install used:

```
HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_DEVELOPER=1
cp /tmp/agentpaas-0.4.4.rb /tmp/agentpaas.rb
brew uninstall --cask agentpaas
brew uninstall --formula agentpaas || true
brew install --formula /tmp/agentpaas.rb
```

Keg: /opt/homebrew/Cellar/agentpaas/0.4.4. post_install MethodDeprecatedError (Homebrew 7 wants post_install_steps); binaries still linked. xattr -cr applied to cellar bins. No com.apple.quarantine.

## Proof (PATH=/opt/homebrew/bin:/usr/bin:/bin)

Must NOT be ~/.local/bin/agentpaas-mcp.

```
$ which agentpaas-mcp
/opt/homebrew/bin/agentpaas-mcp

$ agentpaas version
CLI: 0.4.4 | Proto: v1 | Commit: adf314591af12dcbd5e6d7e9963731358b93c7b3 | OS/Arch: darwin/arm64 | Docker: colima | Docker API: 29.5.2

$ which agentpaas
/opt/homebrew/bin/agentpaas

$ xattr -l /opt/homebrew/bin/agentpaas-mcp
com.apple.provenance: ^A^B
```

No com.apple.quarantine.

stdio handshake (full stdout):

```
$ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}' '{"jsonrpc":"2.0","method":"notifications/initialized"}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | /opt/homebrew/bin/agentpaas-mcp
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2024-11-05","serverInfo":{"name":"agentpaas-mcp","version":"0.5.0-dev"}}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"description":"Run agentpaas doctor","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"doctor"},{"description":"Run agentpaas version","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"version"},{"description":"Run agentpaas init","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"init"},{"description":"Run agentpaas validate","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"validate"},{"description":"Run agentpaas pack","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"pack"},{"description":"Run agentpaas policy show","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"policy_show"},{"description":"Run agentpaas policy init","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"policy_init"},{"description":"Run agentpaas cloud whoami","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_whoami"},{"description":"Run agentpaas cloud push","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_push"},{"description":"Run agentpaas cloud deploy","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_deploy"},{"description":"Run agentpaas cloud invoke","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_invoke"},{"description":"Run agentpaas cloud result","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_result"},{"description":"Run agentpaas cloud logs","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_logs"},{"description":"Run agentpaas cloud status","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_status"},{"description":"Run agentpaas audit","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"audit"}]}}
```

tools/list includes doctor (not ping-only).

brew list/info one-liner:

```
$ brew list --formula agentpaas | tr '\n' ' '; echo; brew info --formula --json=v2 agentpaas | python3 -c 'import json,sys; d=json.load(sys.stdin); f=d["formulae"][0]; print(f["name"], f["versions"]["stable"], "installed=", [i["version"] for i in f["installed"]], "linked=", f.get("linked_keg"))'
/opt/homebrew/Cellar/agentpaas/0.4.4/INSTALL_RECEIPT.json /opt/homebrew/Cellar/agentpaas/0.4.4/bin/agentpaasd /opt/homebrew/Cellar/agentpaas/0.4.4/bin/agentpaas-harness-linux /opt/homebrew/Cellar/agentpaas/0.4.4/bin/agentpaas /opt/homebrew/Cellar/agentpaas/0.4.4/bin/agentpaas-mcp /opt/homebrew/Cellar/agentpaas/0.4.4/bin/agentpaas-harness-linux-amd64 /opt/homebrew/Cellar/agentpaas/0.4.4/.brew/agentpaas.rb /opt/homebrew/Cellar/agentpaas/0.4.4/sbom.spdx.json
agentpaas 0.4.4 installed= ['0.4.4'] linked= 0.4.4
```

Live tap (untouched):

```
$ grep -E 'version ' /opt/homebrew/Library/Taps/agentpaas-ai/homebrew-tap/Casks/agentpaas.rb
  version "0.4.2"
```

## Not done

- No goreleaser release / GitHub 0.4.4 asset
- No git tag / retag v0.4.2
- No HOMEBREW_TAP_GITHUB_TOKEN
- No push to AgentPaaS-ai/homebrew-tap
- No gh pr / git push
- cmd/agentpaas-mcp/main.go, harness, daemon version.go, cloud, Hermes 0.4 walk docs untouched
