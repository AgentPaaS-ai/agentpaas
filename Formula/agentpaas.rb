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
      sha256 "3852abd6622c864c3f91dcf7a287ff7349248823aa0a06aba5a526c354b2d483"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.4.1/agentpaas_0.4.1_darwin_amd64.tar.gz"
      sha256 "8ef516dedd5bdebfd0d4b4ec3b24f1a7e8268341ba2231a5db7c891d4560d290"
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
