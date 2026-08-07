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
      sha256 "93fe61de6a129bdb00086747f0447ad102332ad0d5b16cf2e44a1e098145fc7d"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.6/agentpaas_0.3.6_darwin_amd64.tar.gz"
      sha256 "2df349fa3495d4128b7a8103319022f7459ac48a6ebdca69fe086753c201122e"
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
