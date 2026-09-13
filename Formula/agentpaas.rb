class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  version "0.4.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.1/agentpaas_0.4.1_darwin_arm64.tar.gz"
      sha256 "22a3d01464d7896e202f75fad23868d1964d2497431e9f7a8983dbb1f9264b78"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.1/agentpaas_0.4.1_darwin_amd64.tar.gz"
      sha256 "f01eafdf3ec6ee34896cf4992ba372ec767a00c7f35b32a365489e410d6a6fa0"
    end
  end

  def install
    bin.install "agentpaas"
    bin.install "agentpaasd"
    bin.install "agentpaas-harness-linux"
    bin.install "agentpaas-harness-linux-amd64"
  end

  def post_install
    %w[
      agentpaas
      agentpaasd
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
    assert_match(/0\.4\.1/, output)
  end
end
