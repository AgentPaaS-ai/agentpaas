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
      sha256 "5d424bfdc10aa16c383fb6f6298c3d24e7fe0a5cd8da759eb6e0de2a9a041386"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.7/agentpaas_0.3.7_darwin_amd64.tar.gz"
      sha256 "932b43eb24cf077defc5c72015684f5fc0cdc146e9a9b32c3e780ddbad3773f1"
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
