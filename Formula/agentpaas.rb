class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  # version, url, and sha256 are placeholders filled by goreleaser at tag time.
  # The formula in this repo is a template for review; the published formula
  # lives in AgentPaaS-ai/homebrew-tap and is updated by the release workflow.
  version "0.3.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.0/agentpaas_0.3.0_darwin_arm64.tar.gz"
      sha256 "PLACEHOLDER_SHA256"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.3.0/agentpaas_0.3.0_darwin_amd64.tar.gz"
      sha256 "PLACEHOLDER_SHA256"
    end
  end

  def install
    bin.install "agentpaas"
    bin.install "agentpaasd"
    bin.install "agentpaas-harness-linux"
  end

  test do
    # The version check is flexible: when built with ldflags (Makefile / goreleaser),
    # the binary reports the stamped version. When built without ldflags, defaults
    # report "0.3.0-dev". The test accepts either.
    output = shell_output("#{bin}/agentpaas version")
    assert_match(/0\.3\.0/, output)
  end
end
