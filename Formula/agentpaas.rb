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
      sha256 "1a0edb0801759981bbd2da80c0e1d6a2887a1cc13972576be3e24e2ed8c23e11"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.0/agentpaas_0.4.0_darwin_amd64.tar.gz"
      sha256 "80f6409ccf9885085cd1e01a5d5fbef77a2a12a1b14ac17a3327fa05a8e24fb7"
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
    assert_match(/0\.4\.1/, output)
  end
end
