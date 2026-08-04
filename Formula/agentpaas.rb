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
      sha256 "7d25de940812fc6b9962bec243569a4cf5d58318418f96f0a66ceedead023cb3"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.6/agentpaas_0.3.6_darwin_amd64.tar.gz"
      sha256 "9b7f92fa763f82378f45f46657828db298e133ceea08af406647b0fb83285a2b"
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
    assert_match(/0\.3\.6/, output)
  end
end
