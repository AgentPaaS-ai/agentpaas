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
      sha256 "ba5ee5b8943c96620ebadd5ffb92ca3124a96dc89d41a75d00b7d1b370062f56"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.6/agentpaas_0.3.6_darwin_amd64.tar.gz"
      sha256 "b65828c7312b2587bf50b0fedc7a83531663db0ffa9ba11628505af3100e95d5"
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
