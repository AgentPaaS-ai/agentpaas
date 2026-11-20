# package bundle

## Purpose

`bundle` produces and consumes portable `.agentpaas` archives: signed,
deterministic tar.gz packages of lock + policy + SBOM + source (+ optional
image) for offline inspect, install, and provenance workflows.

## Key Types

| Type | Role |
|------|------|
| `Manifest` / `ManifestContents` | Signed bundle descriptor with content digests |
| `ManifestPublisherInfo` | Publisher name, fingerprint, public key PEM |
| `BundleConfig` / `BundleResult` | Write inputs and write outcome |
| `Bundle` | Opened bundle with metadata + extract indexes |
| `VerifyReport` / `VerifyCheck` | Offline verification results |
| Cap constants | `MaxEntries`, `MaxSingleFileSize`, `MaxTotalUncompressed`, … |

## Key Functions

| Symbol | Role |
|--------|------|
| `Write` | Create deterministic signed `.agentpaas` tar.gz |
| `Open` | Hardened open + metadata load |
| `Verify` | Offline multi-check verification |
| `Inspect` helpers | Human/JSON inspect rendering |
| Extract source/image helpers | On-demand tree extraction after Open |
| Consent-card / chain lint helpers | Install UX and policy-chain checks |

## Architecture

```
pack outputs (AgentLock, policy.yaml, SBOM, source, optional OCI layout)
        |
        v
  bundle.Write  -->  manifest digests + ECDSA signature
        |
        v
  foo.agentpaas (sorted tar.gz)
        |
        +-- Open (caps, path validation)
        +-- Verify (signature + digest pins)
        +-- ExtractSource / ExtractImage
        v
  install / provenance / inspect CLI
```

## Usage

```go
res, err := bundle.Write(bundle.BundleConfig{
    ProjectDir:   dir,
    Manifest:     manifest,
    Lock:         lock,
    PolicyYAML:   policyYAML,
    SBOM:         sbom,
    PublisherKey: publisherKey,
}, outFile)
if err != nil {
    return err
}

b, err := bundle.Open(path)
if err != nil {
    return err
}
report, err := bundle.Verify(b)
```

CLI: `agent bundle inspect`, install-bundle, export paths.
