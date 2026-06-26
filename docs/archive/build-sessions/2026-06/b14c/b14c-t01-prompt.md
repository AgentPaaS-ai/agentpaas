# Block 14C-T01: Goreleaser Config + Homebrew Formula

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. We need to
set up the release pipeline for v0.1.0.

The project has 3 binaries:
- `agent` (cmd/agent/) — the main CLI
- `agentpaasd` (cmd/agentpaasd/) — the daemon
- `harness` (cmd/harness/) — the in-container harness (NOT distributed as host binary)

Only `agent` and `agentpaasd` are released as host binaries. The harness is
built for linux/arm64 and embedded in agent containers during pack.

## What to Implement

### 1. Create `.goreleaser.yaml`

```yaml
project_name: agentpaas

before:
  hooks:
    - go mod tidy

builds:
  - id: agent
    main: ./cmd/agent
    binary: agent
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X github.com/parvezsyed/agentpaas/internal/version.Version={{.Version}}
      - -X github.com/parvezsyed/agentpaas/internal/version.Commit={{.Commit}}
      - -X github.com/parvezsyed/agentpaas/internal/version.Date={{.Date}}

  - id: agentpaasd
    main: ./cmd/agentpaasd
    binary: agentpaasd
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X github.com/parvezsyed/agentpaas/internal/version.Version={{.Version}}
      - -X github.com/parvezsyed/agentpaas/internal/version.Commit={{.Commit}}
      - -X github.com/parvezsyed/agentpaas/internal/version.Date={{.Date}}

archives:
  - id: default
    format: tar.gz
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
    files:
      - LICENSE
      - README.md
      - demo/**/*

checksum:
  name_template: 'checksums.txt'
  algorithm: sha256

sboms:
  - id: default
    artifacts: archive

signs:
  - id: cosign
    cmd: cosign
    signature: "${artifact}.sig"
    certificate: "${artifact}.pem"
    args:
      - sign-blob
      - "--output-signature=${signature}"
      - "--output-certificate=${certificate}"
      - "${artifact}"
    artifacts: all

brews:
  - name: agentpaas
    homepage: https://github.com/AgentPaaS-ai/agentpaas
    description: "Governed, local-first runtime for AI-generated agents"
    license: MIT
    folder: Formula
    repository:
      owner: AgentPaaS-ai
      name: homebrew-tap
      token: "{{ .Env.GITHUB_TOKEN }}"
    install: |
      bin.install "agent"
      bin.install "agentpaasd"
    test: |
      system "#{bin}/agent", "version"

changelog:
  sort: asc
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^chore:'

release:
  github:
    owner: AgentPaaS-ai
    name: agentpaas
  draft: true
  prerelease: auto
  name_template: "v{{.Version}}"
```

IMPORTANT: Check the actual version package path. Read internal/version/ or
internal/cli/version.go to find the correct ldflags import path. If the version
package doesn't have Version/Commit/Date variables, check how version is currently
handled and adjust.

### 2. Create Homebrew Formula template

Create `Formula/agentpaas.rb` (this will be in the tap repo, but having it in
the main repo documents the formula):

```ruby
class Agentpaas < Formula
  desc "Governed, local-first runtime for AI-generated agents"
  homepage "https://github.com/AgentPaaS-ai/agentpaas"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.1.0/agentpaas_0.1.0_darwin_arm64.tar.gz"
      sha256 "PLACEHOLDER_SHA256"
    end
    on_intel do
      url "https://github.com/AgentPaaS-ai/agentpaas/releases/download/v0.1.0/agentpaas_0.1.0_darwin_amd64.tar.gz"
      sha256 "PLACEHOLDER_SHA256"
    end
  end

  def install
    bin.install "agent"
    bin.install "agentpaasd"
  end

  test do
    assert_match "0.1.0", shell_output("#{bin}/agent version")
  end
end
```

### 3. Create GitHub Actions release workflow

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write
  packages: write
  id-token: write

jobs:
  release:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Install cosign
        uses: sigstore/cosign-installer@v3

      - name: Install syft (SBOM)
        uses: anchore/sbom-action@v0

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Constraints

- Only build for darwin (macOS-first). Linux support is P2.
- The harness binary is NOT released (it's built during pack for linux/arm64).
- Check the actual version package path before writing ldflags.
- The Homebrew formula uses PLACEHOLDER SHA256 — real checksums come from goreleaser.
- Do NOT tag v0.1.0 yet (that's done manually after verification).
- Run `go build ./...` to verify the project still builds.
- Verify `.goreleaser.yaml` is valid YAML.
