class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.1.0/agentpaas_0.1.0_darwin_arm64.tar.gz"
      # SHA256 is a placeholder — goreleaser fills the real checksum during the first release (v0.1.0 tag push).
      sha256 "PLACEHOLDER_SHA256"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.1.0/agentpaas_0.1.0_darwin_amd64.tar.gz"
      # SHA256 is a placeholder — goreleaser fills the real checksum during the first release (v0.1.0 tag push).
      sha256 "PLACEHOLDER_SHA256"
    end
  end

  def install
    bin.install "agent"
    bin.install "agentpaasd"
    bin.install "agentpaas-harness-linux"
  end

  test do
    assert_match "0.1.0", shell_output("#{bin}/agent version")
  end
end