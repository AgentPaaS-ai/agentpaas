class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # Published install path is the Homebrew cask in AgentPaaS-ai/homebrew-tap
  # (goreleaser updates Casks/agentpaas.rb). This Formula is the in-repo mirror.
  version "0.4.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.2/agentpaas_0.4.2_darwin_arm64.tar.gz"
      sha256 "0c0a0689b7d4971b4cc01189f89a75b6408aea60107ed0ec61e81fae467a6ef9"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.2/agentpaas_0.4.2_darwin_amd64.tar.gz"
      sha256 "1cf8c4e88067c3c04c267cb89a6fc65760dd63af6deee70bf27ddc1c4f1b6003"
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
