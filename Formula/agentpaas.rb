class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  version "0.3.7"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.7/agentpaas_0.3.7_darwin_arm64.tar.gz"
      sha256 "61bcb2e35523800588376cec7a945739895e2445ddbc329625cd16d9f944b06c"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.7/agentpaas_0.3.7_darwin_amd64.tar.gz"
      sha256 "1d5c3c55a443b4e8837967c016b1aaa526f9952ea5587e232281e819efbf92f5"
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
    assert_match(/0\.3\.7/, output)
  end
end
