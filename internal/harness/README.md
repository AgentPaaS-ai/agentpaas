# package harness

## Purpose

`harness` is the in-container agent supervisor. It serves a loopback HTTP
lifecycle API, runs the Python worker, enforces budgets/rlimits, reaps
children, integrates progress journals, and streams model I/O under guardrails.

## Key Types

| Type | Role |
|------|------|
| `Config` | Listen addr, agent path, timeouts, journal/credential sidecars, rlimits |
| `Server` | HTTP lifecycle server |
| `InvokeResponse` / `ErrorResponse` | Invoke success/failure envelopes |
| `FailureContext` | Structured failure detail for operators |
| Budget types | Wall-clock / envelope-derived invoke budgets |
| Progress journal writer types | HMAC progress records (B27) |
| Stream adapter types | Model stream delta handling and terminal events |

## Key Functions

| Symbol | Role |
|--------|------|
| `NewServer` / server listen helpers | Start lifecycle HTTP API |
| Import/invoke/terminate handlers | Worker lifecycle |
| Budget derivation | TimeEnvelope vs legacy 120s fallback |
| Process reaper / process-group kill | Zombie and cancel hygiene |
| Firewall helpers | Platform-specific egress capability hooks |
| Guardrail / streaming helpers | Response filtering and stream terminal rules |
| RPC server (Unix) | In-container LLM/MCP credential-mediated calls |
| `LoadRunBundle` | Download, hash, and unpack a run bundle before the worker starts |
| `FullRehashDefault` | Whether a restored tree re-hashes every file when the switch is unset |

## Run bundle

When `AGENTPAAS_BUNDLE_URL`, `AGENTPAAS_BUNDLE_SHA256`, and `AGENTPAAS_BUNDLE_SIZE` are all set, the harness downloads that tar, hashes it while reading, and refuses on a size or hash mismatch before writing `/app`. Redirects are not followed. Unpack allows only `app/**` and `agentpaas/requirements.lock`, rejects links and `..`, and caps unpacked bytes. A successful unpack writes `/app/.agentpaas-bundle-manifest.json` (per-file sha256 plus the bundle digest and size) and keeps the verified tar at `/app/.agentpaas-bundle.tar`, then the worker starts as before. A refusal writes one JSON line to stderr (`status`, `reason`, `detail`) and exits non-zero without starting the worker. With those variables unset, startup is unchanged.

If that manifest is already present, the harness does not download again. It checks the manifest digest and size against the expected values and refuses on a mismatch (`bundle_tree_digest_mismatch`). An unreadable, non-regular, or swapped manifest is `bundle_tree_manifest_invalid` and does not fall through to a download. A missing manifest is a fresh tree only when `/app` is absent or empty: `/app` with any entry and no manifest is `bundle_tree_manifest_invalid`, and nothing is downloaded over it. After a download, `/app` must hold only the bundle files, the manifest, and the kept tar; any other entry is `bundle_tree_rehash_mismatch` and the worker does not start. `AGENTPAAS_BUNDLE_REHASH` selects a full re-hash. Unset, the switch is on (`FullRehashDefault`). `0`, `false`, `no`, or `off` skips the file re-hash and checks only the digest and size. A full re-hash does not trust file hashes stored in the tree: those lines are writable by the same uid. It keeps the verified bundle at `/app/.agentpaas-bundle.tar` and requires that file's sha256 to equal the expected digest (the value the platform took from the signed lock). Every bundle file must match those bytes, and `/app/.agentpaas-bundle-manifest.json` must be the manifest that re-hash just produced. A changed, extra, or missing file, or a manifest rewritten to agree with a changed file, is `bundle_tree_rehash_mismatch`. The image harness binary under `/agentpaas` is not a bundle file.

`AGENTPAAS_BAKE=1` (or `--bake`) does the bundle load and the rest of startup except the worker start, then reports ready on `/health`, `/healthz`, and `/readyz` and waits. `AGENTPAAS_BAKE=0` wins over `--bake`. Bake is not an HTTP route; a request cannot enter it, and `/invoke` during bake returns `bake_mode` without starting Python. `make base-image` (alias `make py311-r1`) builds `agentpaas/py311-r1`: pinned distroless base, linux/amd64 harness, empty `/app` owned by uid 64000, `USER 64000:64000`, entrypoint `/agentpaas/harness`. `/agentpaas` stays root-owned. `/agentpaas/requirements.lock` is an empty file owned by 64000 so unpack can overwrite it.

## Architecture

```
Docker container
  /harness (PID 1)
       |
       +-- HTTP 127.0.0.1:8080  /health /import /invoke /terminate
       +-- Python worker (agent entry)
       +-- optional Unix RPC for LLM/MCP via gateway
       +-- progress journal file (HMAC)
       +-- credentials sidecar (deleted after load)
       v
  stdout/stderr + audit records tailed by daemon
```

## Usage

```go
srv, err := harness.NewServer(harness.Config{
    Addr:          "127.0.0.1:8080",
    AgentPath:     "/app",
    DurablePath:   true,
    CPUQuotaSeconds: 30,
    MaxPIDs:         16,
    JournalPath:     journalPath,
    JournalKeyPath:  keyPath,
})
// listen and serve until terminate
```

Binary entry: `cmd/harness`. Images embed the harness as the container entrypoint.
