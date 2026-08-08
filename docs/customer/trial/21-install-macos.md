# Install on macOS

Mac required (darwin/arm64). Docker via Colima is expected for local pack/run.

```bash
# Brew install (see github.com/AgentPaaS-ai/agentpaas README for current cask)
export PATH="/opt/homebrew/bin:$PATH"
agentpaas version
# If macOS blocks the binary:
xattr -cr "$(brew --prefix)/bin/agentpaas" "$(brew --prefix)/bin/agentpaasd" 2>/dev/null || true
agentpaas daemon start
agentpaas doctor
```

Expect doctor checks green. Keep the daemon running while packing and running agents.
