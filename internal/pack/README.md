# package pack

## Purpose

`pack` turns an agent project directory into a reproducible, signed deployable
unit: OCI image + `agent.lock` + SBOM + policy sidecar metadata, with scans and
provenance support.

## Key Types

| Type | Role |
|------|------|
| `BuildConfig` / `BuildResult` | Deterministic image build inputs/outputs |
| `BuildFile` | Collected source file in build context |
| `AgentYAML` / `LLMConfig` | Parsed agent.yaml |
| `RuntimeType` | `python`, `langgraph`, `crewai`, … |
| `DetectionResult` | Runtime/project detection outcome |
| `AgentLock` | Signed lockfile / review unit |
| `IgnoreMatcher` | `.agentpaasignore` rules |
| `AdvisoryReport` / `AdvisoryFinding` | OSV scanner results |
| `LineageFile` / `LineageParent` | Fork/lineage metadata |

## Key Functions

| Symbol | Role |
|--------|------|
| `DetectProject` / `LoadAgentYAML` | Inspect project and agent.yaml |
| `InitScaffold` / `InitFromCode` | Create or reconcile project files |
| `BuildImage` | Build digest-pinned OCI image with harness PID 1 |
| `ComputeBuildInputDigest` | Canonical source digest |
| `CollectBuildFiles` / `CreateBuildContext` | Symlink-safe context collection |
| `ResolveDependencies` | Lock deps with uv |
| `ScanAdvisories` | OSV advisory scan over SBOM |
| Lock/sign/verify helpers | Produce and check `AgentLock` |
| `ValidateLLMEgress` | Pack-time provider/egress consistency |
| Secret/provenance/lineage helpers | Scan and record supply-chain metadata |

## Architecture

```
project dir
  agent.yaml + source + policy.yaml + .agentpaasignore
        |
        v
  Detect / validate LLM egress / ignore / secret scan
        |
        v
  Resolve deps (uv) -> multi-stage Docker build
        |
        v
  Image digest + SBOM + build_input_digest
        |
        v
  Sign AgentLock (package identity key)
        |
        v
  Deployed dir / registry / bundle export
```

## Usage

```go
cfg := pack.BuildConfig{
    ProjectDir: dir,
    ImageTag:   "agentpaas/demo:0.1.0",
    HarnessPath: harnessBin,
}
result, err := pack.BuildImage(ctx, cfg)
if err != nil {
    return err
}
fmt.Println(result.ImageDigest, result.BuildInputDigest)
```

CLI entry: `agent pack` (via `internal/cli` → daemon `Pack` RPC or local path).
