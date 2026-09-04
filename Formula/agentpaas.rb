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
      sha256 "bf5c482df677e90776093cba5c9d78054c7e65fb5af958febc16262af16c98b4"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.0/agentpaas_0.4.0_darwin_amd64.tar.gz"
      sha256 "27211c2c68ee7078502f606146e621b6f99c44590707a52fb489f7ddf2686d7a"
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
