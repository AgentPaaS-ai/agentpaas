class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  version "0.3.5"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.5/agentpaas_0.3.5_darwin_arm64.tar.gz"
      sha256 "a7ccc77a19c9aa999a39029d5cd20232927fe7dac90f3e0d2cee8bcdd8363324"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.5/agentpaas_0.3.5_darwin_amd64.tar.gz"
      sha256 "77c864208766ce76ad05fe50c2216662a8e6ea5284ca9ec98da5d4772b5095e6"
    end
  end

  def install
    bin.install "agentpaas"
    bin.install "agentpaasd"
    bin.install "agentpaas-harness-linux"
  end

  test do
    output = shell_output("#{bin}/agentpaas version")
    assert_match(/0\.3\.5/, output)
  end
end
