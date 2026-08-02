class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  version "0.3.6"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.6/agentpaas_0.3.6_darwin_arm64.tar.gz"
      sha256 "d5862c35559d5ce33bd4e04e98b0de23f265125e1479b26e1b3a508463627f66"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.6/agentpaas_0.3.6_darwin_amd64.tar.gz"
      sha256 "bc8e7b96937940c8843cd1d78462399109c6b7c40c4c4e4a8edbef05323aee7e"
    end
  end

  def install
    bin.install "agentpaas"
    bin.install "agentpaasd"
    bin.install "agentpaas-harness-linux"
  end

  test do
    output = shell_output("#{bin}/agentpaas version")
    assert_match(/0\.3\.6/, output)
  end
end
