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
