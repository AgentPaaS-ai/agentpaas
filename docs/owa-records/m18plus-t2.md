# M18+ T2 — one-gesture host install paths

Date: 2026-09-16
Worktree: /Users/pms88/projects/agentpaas/worktrees/oss/ap-m18plus-t2-host-install
Branch: feat/m18plus-t2-host-install
Verdict: files on disk (docs/templates). No in-host founder walk.

## Scope

Docs + templates only. Cursor deeplink, `claude mcp add`, `grok mcp add`.
No brew retag. No Formula edit. No Go. No Hermes 0.4 walk change.
No founder sitting in a host.

## Live host docs (orch, 2026-09-16)

- Cursor install links: https://cursor.com/docs/mcp/install-links
  `cursor://anysphere.cursor-deeplink/mcp/install?name=$NAME&config=$BASE64_ENCODED_CONFIG`
  Config is JSON.stringify of command/args, then base64. Paste fallback remains
  `templates/hosts/cursor.mcp.json` (mcp.json `mcpServers` wrap).
- Claude Code local stdio: https://code.claude.com/docs/en/mcp-quickstart
  `claude mcp add <name> -- <command>` (stdio is default; no --transport).
  Paste fallback remains `templates/hosts/claude-code.mcp.json`.
- Grok Build: https://docs.x.ai/build/features/mcp-servers
  `grok mcp add <name> -- <command>` is the one-command add (SPEC: use it
  when it exists). Toml merge is the floor (`templates/hosts/grok.toml`).

## Deeplink (computed, not invented)

JSON: `{"command":"agentpaas-mcp","args":[]}`
Link: `cursor://anysphere.cursor-deeplink/mcp/install?name=agentpaas&config=eyJjb21tYW5kIjoiYWdlbnRwYWFzLW1jcCIsImFyZ3MiOltdfQ==`

## Files

- docs/skins.md (first-run rewritten for three hosts)
- templates/hosts/cursor.install-link.txt (new)
- templates/hosts/grok.toml (comment: grok mcp add primary)
- templates/hosts/cursor.mcp.json (unchanged wrap)
- templates/hosts/claude-code.mcp.json (unchanged wrap)

## Not claimed

- Weather golden inside Cursor / Claude Code / Grok Build
- Brew ships agentpaas-mcp (0.4.2 formula still four binaries)
- Codex / ChatGPT desktop as this-cut hosts
