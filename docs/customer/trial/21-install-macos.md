# Install on macOS

Requires **AgentPaaS CLI 0.3.7+** (brew).

# Install on macOS

Mac required (darwin/arm64). Docker via Colima is expected for local pack/run.

```bash
# Brew install (see github.com/AgentPaaS-ai/agentpaas README for current cask)
brew tap AgentPaaS-ai/homebrew-tap
brew install agentpaas
export PATH="/opt/homebrew/bin:$PATH"
agentpaas version
agentpaas daemon start
agentpaas doctor
```

Brew post-install clears macOS quarantine on the installed binaries.

Expect doctor checks green. Keep the daemon running while packing and running agents.
