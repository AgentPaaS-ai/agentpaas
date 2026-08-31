class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  version "0.4.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.0/agentpaas_0.4.0_darwin_arm64.tar.gz"
      sha256 "aee1eea947be49b0d2598892b61a59f6774dfcdaf52c15f8bdca57d06a839f3f"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.0/agentpaas_0.4.0_darwin_amd64.tar.gz"
      sha256 "156c6e9466a2e9150f483c6198e2919613f0ed623cf8a43781993c01689e3528"
    end
  end

  def install
    bin.install "agentpaas"
    bin.install "agentpaasd"
    bin.install "agentpaas-harness-linux"
    bin.install "agentpaas-harness-linux-amd64"
  end

  test do
    output = shell_output("#{bin}/agentpaas version")
    assert_match(/0\.4\.0/, output)
  end
end
