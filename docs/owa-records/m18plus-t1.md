# M18+ T1 — agentpaas-mcp CLI adapter

Date: 2026-09-16
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t1-mcp-adapter
Branch: feat/m18plus-t1-mcp-adapter
HEAD after commit: b59abaa
Verdict: PASS

## Scope

Replace ping-only dummy `agentpaas-mcp` with a thin stdio adapter over the
`agentpaas` CLI. Not pack/deploy itself. Not a per-host runtime. Not a new
MCP protocol.

## go test ./cmd/agentpaas-mcp/...

```
ok  	github.com/AgentPaaS-ai/agentpaas/cmd/agentpaas-mcp	0.199s
```

## Build

```
mkdir -p bin && go build -o bin/agentpaas-mcp ./cmd/agentpaas-mcp
```

Binary: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t1-mcp-adapter/bin/agentpaas-mcp

## Handshake

```
python3 /tmp/m18plus-t1-handshake.py "$PWD/bin/agentpaas-mcp"
```

```
BIN /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t1-mcp-adapter/bin/agentpaas-mcp
INIT {"id": 1, "jsonrpc": "2.0", "result": {"capabilities": {"tools": {}}, "protocolVersion": "2024-11-05", "serverInfo": {"name": "agentpaas-mcp", "version": "0.5.0-dev"}}}
LIST {"id": 2, "jsonrpc": "2.0", "result": {"tools": [{"description": "Run agentpaas doctor", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "doctor"}, {"description": "Run agentpaas version", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "version"}, {"description": "Run agentpaas init", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "init"}, {"description": "Run agentpaas validate", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "validate"}, {"description": "Run agentpaas pack", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "pack"}, {"description": "Run agentpaas policy show", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "policy_show"}, {"description": "Run agentpaas policy init", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "policy_init"}, {"description": "Run agentpaas cloud whoami", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_whoami"}, {"description": "Run agentpaas cloud push", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_push"}, {"description": "Run agentpaas cloud deploy", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_deploy"}, {"description": "Run agentpaas cloud invoke", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_invoke"}, {"description": "Run agentpaas cloud result", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_result"}, {"description": "Run agentpaas cloud logs", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_logs"}, {"description": "Run agentpaas cloud status", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "cloud_status"}, {"description": "Run agentpaas audit", "inputSchema": {"properties": {"args": {"description": "Extra argv after the fixed command prefix", "items": {"type": "string"}, "type": "array"}}, "type": "object"}, "name": "audit"}]}}
DOCTOR {"id": 3, "jsonrpc": "2.0", "result": {"content": [{"text": "agentpaas doctor\n===============\nVersion:           ok\n                   0.4.2 (darwin/arm64)\nDocker CLI:        ok\n                   (29.8.0)\nDocker daemon:     ok\nmacOS Keychain:    ok\n                   (accessible)\nLinux harness:     ok\n                   (/opt/homebrew/bin/agentpaas-harness-linux)\nHome directory:    ok\n                   (/Users/pms88/.agentpaas, writable)\nskopeo (optional): ok\n                   (/opt/homebrew/bin/skopeo)\n\nOverall: 7/7 checks passed\n", "type": "text"}]}}
TOOLS ['doctor', 'version', 'init', 'validate', 'pack', 'policy_show', 'policy_init', 'cloud_whoami', 'cloud_push', 'cloud_deploy', 'cloud_invoke', 'cloud_result', 'cloud_logs', 'cloud_status', 'audit']
DOCTOR_TEXT_BEGIN
agentpaas doctor
===============
Version:           ok
                   0.4.2 (darwin/arm64)
Docker CLI:        ok
                   (29.8.0)
Docker daemon:     ok
macOS Keychain:    ok
                   (accessible)
Linux harness:     ok
                   (/opt/homebrew/bin/agentpaas-harness-linux)
Home directory:    ok
                   (/Users/pms88/.agentpaas, writable)
skopeo (optional): ok
                   (/opt/homebrew/bin/skopeo)

Overall: 7/7 checks passed

DOCTOR_TEXT_END
RESULT PASS
SERVER_VERSION 0.5.0-dev
```
