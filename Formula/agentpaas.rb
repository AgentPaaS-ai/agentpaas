class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  # v0.5.0 source tag peeled SHA: 170b177ae8945bc5e0d31be14309f340a87439c1
  version "0.5.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.5.0/agentpaas_0.5.0_darwin_arm64.tar.gz"
      sha256 "2ec91a7a1b7933c165bc7c781eab8114ac3db6a8c70af6b5d2659027f5ae2c1a"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.5.0/agentpaas_0.5.0_darwin_amd64.tar.gz"
      sha256 "3db2b92cf1ff61eb2f6e2c74492f90cfdc2b82afeb54e6c8238a44d82822fe02"
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
    assert_match(/0\.5\.0/, output)
  end
end
