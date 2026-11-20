class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  # v0.5.1 source tag peeled SHA: 3a9a5e8d876a43a7da9d07b1d2625377b9d7c60d
  version "0.5.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.5.1/agentpaas_0.5.1_darwin_arm64.tar.gz"
      sha256 "421e82e1dfd3d40cfc688ad9ca1a5052fca48d6045bccf7dc1af4ba4183c1e7e"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.5.1/agentpaas_0.5.1_darwin_amd64.tar.gz"
      sha256 "433f58ec07d12e7b3d0db7163d836a037e1943553bd926f8bc7981e56f4573b9"
    end
  end

  def install
    bin.install "agentpaas"
    bin.install "agentpaasd"
    bin.install "agentpaas-mcp"
    bin.install "agentpaas-harness-linux"
    bin.install "agentpaas-harness-linux-amd64"
  end

  def post_install
    %w[
      agentpaas
      agentpaasd
      agentpaas-mcp
      agentpaas-harness-linux
      agentpaas-harness-linux-amd64
    ].each do |name|
      target = bin/name
      next unless target.exist?
      chmod "u+w", target
      system "/usr/bin/xattr", "-cr", target
      chmod 0555, target
    end
  end

  test do
    output = shell_output("#{bin}/agentpaas version")
    assert_match(/0\.5\.1/, output)
  end
end
