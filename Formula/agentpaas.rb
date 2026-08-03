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
      sha256 "43cf3d84d921a3e2ee2a1e166b4bced13dfa759569da719ea57cf7ccf5a006f8"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.6/agentpaas_0.3.6_darwin_amd64.tar.gz"
      sha256 "41c51374dc65e156e451ff111305bad6ed8219c6fb1e69e57c55b8c50bec059c"
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
