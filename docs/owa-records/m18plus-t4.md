# M18+ T4 — weather golden through brew agentpaas-mcp stdio

Date: 2026-09-16 (PDT)
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t4-mcp-weather
Branch: feat/m18plus-t4-mcp-weather
HEAD: d3d1aae (T3 already on this branch; not merged)

brew PATH mcp; not in-host; no GitHub 0.4.4; no tap push.

This T is not an in-host claim. Dummy ping is not this story.
No nested hermes. No Cursor / Claude Code / Grok Build founder sit.
MCP tools named login/logout/secret* were not called.

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

## Drive tools/call on the brew mcp binary

stdio JSON-RPC. PATH=/opt/homebrew/bin:/usr/bin:/bin so LookPath finds brew `agentpaas`.
cwd: demo/weather-agent (so CLI sees agent.yaml).
MCP runCLI timeout in the existing binary is 60s. Binary was not edited.

### Session 1 — initialize, tools/list, doctor, validate (empty args), pack, cloud_whoami

```
$ export PATH=/opt/homebrew/bin:/usr/bin:/bin
$ cd /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t4-mcp-weather/demo/weather-agent
$ printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t4","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"doctor","arguments":{}}}' \
  '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"validate","arguments":{}}}' \
  '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"pack","arguments":{}}}' \
  '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"cloud_whoami","arguments":{}}}' \
  | /opt/homebrew/bin/agentpaas-mcp
```

Full stdout:

```
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2024-11-05","serverInfo":{"name":"agentpaas-mcp","version":"0.5.0-dev"}}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"description":"Run agentpaas doctor","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"doctor"},{"description":"Run agentpaas version","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"version"},{"description":"Run agentpaas init","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"init"},{"description":"Run agentpaas validate","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"validate"},{"description":"Run agentpaas pack","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"pack"},{"description":"Run agentpaas policy show","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"policy_show"},{"description":"Run agentpaas policy init","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"policy_init"},{"description":"Run agentpaas cloud whoami","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_whoami"},{"description":"Run agentpaas cloud push","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_push"},{"description":"Run agentpaas cloud deploy","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_deploy"},{"description":"Run agentpaas cloud invoke","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_invoke"},{"description":"Run agentpaas cloud result","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_result"},{"description":"Run agentpaas cloud logs","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_logs"},{"description":"Run agentpaas cloud status","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"cloud_status"},{"description":"Run agentpaas audit","inputSchema":{"properties":{"args":{"description":"Extra argv after the fixed command prefix","items":{"type":"string"},"type":"array"}},"type":"object"},"name":"audit"}]}}
{"jsonrpc":"2.0","id":3,"result":{"content":[{"text":"agentpaas doctor\n===============\nVersion:           ok\n                   0.4.4 (darwin/arm64)\nDocker CLI:        ok\n                   (29.8.0)\nDocker daemon:     ok\nmacOS Keychain:    ok\n                   (accessible)\nLinux harness:     ok\n                   (/opt/homebrew/bin/agentpaas-harness-linux)\nHome directory:    ok\n                   (/Users/pms88/.agentpaas, writable)\nskopeo (optional): ok\n                   (/opt/homebrew/bin/skopeo)\n\nOverall: 7/7 checks passed\n","type":"text"}]}}
{"jsonrpc":"2.0","id":4,"result":{"content":[{"text":"error: accepts 1 arg(s), received 0\n","type":"text"}],"isError":true}}
{"jsonrpc":"2.0","id":5,"result":{"content":[{"text":"Image: sha256:a0ef0787013d2822fb25d08d8d95c5ab27d9be361a0db184326af68d2f476710\nDigest: sha256:a0ef0787013d2822fb25d08d8d95c5ab27d9be361a0db184326af68d2f476710\nLock: /Users/pms88/.agentpaas/state/agents/weather-agent/agent.lock\nPush with: agentpaas cloud push --lock /Users/pms88/.agentpaas/state/agents/weather-agent/agent.lock\n","type":"text"}]}}
{"jsonrpc":"2.0","id":6,"result":{"content":[{"text":"Tenant: ten_91ea8938a5f2eaa4f77335feb53eabae\nTier: trial\nAgent limit: 19/100\nCPU minutes used: 64.08051666666667/1000\n","type":"text"}]}}
```

stderr: empty. process exit 0.

### tools/list names (from session 1 id=2)

doctor, version, init, validate, pack, policy_show, policy_init, cloud_whoami, cloud_push, cloud_deploy, cloud_invoke, cloud_result, cloud_logs, cloud_status, audit

tools/list includes doctor (not ping-only). ping is not in the list.

### Session 1 notes

- doctor: 7/7 checks passed; Version 0.4.4 (darwin/arm64); harness /opt/homebrew/bin/agentpaas-harness-linux
- validate with empty arguments: CLI error `accepts 1 arg(s), received 0` (honest; validate requires `<project-path>`)
- pack: succeeded from cwd demo/weather-agent
  Image/Digest: sha256:a0ef0787013d2822fb25d08d8d95c5ab27d9be361a0db184326af68d2f476710
  Lock: /Users/pms88/.agentpaas/state/agents/weather-agent/agent.lock
- cloud_whoami: succeeded (not 401)
  Tenant: ten_91ea8938a5f2eaa4f77335feb53eabae
  Tier: trial
  Agent limit: 19/100
  CPU minutes used: 64.08051666666667/1000
  Not customer tenancy ten_5e4aea6f.

### Session 2 — validate extra argv, then cloud_deploy / cloud_invoke / audit

Adapter extra argv after the fixed prefix: `"arguments":{"args":["..."]}`.

```
$ export PATH=/opt/homebrew/bin:/usr/bin:/bin
$ cd /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t4-mcp-weather/demo/weather-agent
$ printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t4","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"validate","arguments":{"args":["."]}}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cloud_deploy","arguments":{"args":["--lock","/Users/pms88/.agentpaas/state/agents/weather-agent/agent.lock"]}}}' \
  '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"cloud_invoke","arguments":{}}}' \
  '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"audit","arguments":{"args":["verify"]}}}' \
  | /opt/homebrew/bin/agentpaas-mcp
```

Full stdout:

```
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2024-11-05","serverInfo":{"name":"agentpaas-mcp","version":"0.5.0-dev"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"text":"Project ready: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t4-mcp-weather/demo/weather-agent (runtime: python)\n","type":"text"}]}}
{"jsonrpc":"2.0","id":3,"result":{"content":[{"text":"Creating deployment…\nerror: cloud deploy: create deployment: image not admitted — run cloud push first (status 400)\n","type":"text"}],"isError":true}}
{"jsonrpc":"2.0","id":4,"result":{"content":[{"text":"error: accepts 1 arg(s), received 0\n","type":"text"}],"isError":true}}
{"jsonrpc":"2.0","id":5,"result":{"content":[{"text":"Audit chain verification FAILED\n- tail truncation: checkpoint seq=43 anchors head anchor seq=43 but audit chain only has 38 records (last seq=38)\nerror: audit chain verification failed: 1 issue(s)\n","type":"text"}],"isError":true}}
```

stderr: empty. process exit 0.

### Session 2 notes

- validate args=["."]: Project ready; runtime python (cwd demo/weather-agent)
- cloud_deploy --lock <pack lock>: honest FAIL — `image not admitted — run cloud push first (status 400)`
- cloud_invoke empty args: honest FAIL — `accepts 1 arg(s), received 0` (no deployment id; deploy did not succeed)
- audit verify: honest FAIL — local audit chain tail truncation (checkpoint seq=43 vs 38 records)
- cloud_push was not called. No bypass of MCP. No customer tenancy ten_5e4aea6f.

## CLI argv (for MCP extra args only)

PATH=/opt/homebrew/bin:/usr/bin:/bin. CLI help only; not a second MCP session.

```
$ export PATH=/opt/homebrew/bin:/usr/bin:/bin
$ agentpaas cloud push --help
Upload a locally built agent image to AgentPaaS Cloud and admit it for deployment. The image must have been packed with --target linux/amd64.

This command reads the agent.lock file, verifies its signature, and
streams a Docker save archive to the cloud API using your tenant token.
The control plane verifies the lockfile signature and admits the image for
deployment.

Admit rejects unsigned locks.

If --skip-registry is set, only the admission request is sent (no image
upload). Use --registry-ref to admit an image that is already available in
the cloud registry.

Usage:
  agentpaas cloud push [flags]

Examples:
  # Push and admit
  agentpaas cloud push --lock agent.lock

  # Push with admission only (no registry push)
  agentpaas cloud push --lock agent.lock --skip-registry

  # Admit an image already available in the cloud registry
  agentpaas cloud push --lock agent.lock --registry-ref registry.example.com/...

  # Push with explicit digest override
  agentpaas cloud push --lock agent.lock --digest sha256:...

Flags:
      --digest string         Override image digest (default: from lock.image_digest)
  -h, --help                  help for push
      --image string          Override local image reference (optional)
      --lock string           Path to agent.lock JSON (required)
      --platform string       Target platform (default: from lock.platform or linux/amd64)
      --registry-ref string   Cloud registry reference (optional)
      --skip-registry         Skip image upload; admission only

Global Flags:
      --home string     AgentPaaS home directory (default: $AGENTPAAS_HOME, else ~/.agentpaas)
      --json            Emit machine-readable JSON instead of human-readable text
      --socket string   Daemon Unix socket path (default: $AGENTPAAS_SOCKET, else <home>/daemon.sock)
```

`--lock` is required. No other flags are required. MCP extra argv used: `["--lock", "<pack lock>"]`. `--skip-registry` / `--registry-ref` / `--digest` were not added.

```
$ agentpaas cloud invoke --help
Invoke a deployment with a deployment invoke token.

Use --token or AGENTPAAS_CLOUD_INVOKE_TOKEN for the invoke token. This
command never uses the tenant cloud login token for the invoke request.

Large inputs (M13.8): --input-file uploads via POST /v1/inputs (tenant
login required) and attaches input_ref. --input-url attaches a URL ref.
--input-sha256 and --input-size-bytes are optional; the CLI does not
invent them.

Usage:
  agentpaas cloud invoke <deployment_id> [flags]

Flags:
      --body string             JSON request body (default "{}")
      --body-file string        Read JSON request body from a file, or - for stdin
  -h, --help                    help for invoke
      --input-file string       Upload local file as input_ref (M13.8; requires cloud login)
      --input-sha256 string     Optional SHA-256 hex for --input-url
      --input-size-bytes int    Optional size_bytes for --input-url (default -1)
      --input-url string        Attach URL input_ref (sha256 and size_bytes optional)
      --token string            Deployment invoke token
      --wait                    Wait for a terminal run result
      --wait-timeout duration   Maximum time to wait when --wait is set (default 10m0s)

Global Flags:
      --home string     AgentPaaS home directory (default: $AGENTPAAS_HOME, else ~/.agentpaas)
      --json            Emit machine-readable JSON instead of human-readable text
      --socket string   Daemon Unix socket path (default: $AGENTPAAS_SOCKET, else <home>/daemon.sock)
```

Required positional: `<deployment_id>` (1 arg). Not invoked in session 3 (no deployment/workflow id).

### Session 3 — initialize, cloud_push --lock, audit (deploy/invoke not reached)

One stdio session on `/opt/homebrew/bin/agentpaas-mcp`. Sequential on the same stdin: initialize + initialized + cloud_push, then audit. cloud_deploy and cloud_invoke were not sent because push failed and did not report image already present. Pack/whoami/doctor/validate were not redone. No MCP login/secret tools. Tenant remains ten_91ea8938a5f2eaa4f77335feb53eabae from session 1 whoami (not ten_5e4aea6f).

```
$ export PATH=/opt/homebrew/bin:/usr/bin:/bin
$ cd /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t4-mcp-weather/demo/weather-agent
$ printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t4","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"cloud_push","arguments":{"args":["--lock","/Users/pms88/.agentpaas/state/agents/weather-agent/agent.lock"]}}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"audit","arguments":{"args":["verify"]}}}' \
  | /opt/homebrew/bin/agentpaas-mcp
```

Full stdout:

```
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2024-11-05","serverInfo":{"name":"agentpaas-mcp","version":"0.5.0-dev"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"text":"error: cloud push: platform must be linux/amd64 for cloud, got \"darwin/arm64\"\n","type":"text"}],"isError":true}}
{"jsonrpc":"2.0","id":3,"result":{"content":[{"text":"Audit chain verification FAILED\n- tail truncation: checkpoint seq=43 anchors head anchor seq=43 but audit chain only has 38 records (last seq=38)\nerror: audit chain verification failed: 1 issue(s)\n","type":"text"}],"isError":true}}
```

stderr: empty. process exit 0.

### Session 3 notes

- cloud_push args=["--lock", pack lock]: honest FAIL — `error: cloud push: platform must be linux/amd64 for cloud, got "darwin/arm64"` (lock from session 1 pack; pack was not redone)
- cloud_deploy: not called (push did not succeed; image not already present)
- cloud_invoke: not called (no deployment/workflow id)
- audit verify: honest FAIL — local audit chain tail truncation (checkpoint seq=43 vs 38 records)
- No bypass of MCP. No customer tenancy ten_5e4aea6f.

## Not done

- No git push / gh pr
- No GitHub 0.4.4 release or asset
- No tap push / retag / Formula publish
- No nested hermes
- cmd/agentpaas-mcp, Formula, .goreleaser.yaml, harness, cloud src, integrations/hermes-plugin, Hermes 0.4 walk not edited
- Not an in-host claim
