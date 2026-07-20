# package supervisor

## Purpose

`supervisor` owns post-admission run/attempt operational truth: claim leases,
track authenticated progress, enforce stalls, finalize verified results,
cancel/cleanup, and reconcile after daemon restart — all via CAS on durable
routedrun state.

## Key Types

| Type | Role |
|------|------|
| `Supervisor` | Lifecycle authority |
| `SupervisorOption` | `WithAuditLogger`, `WithTimer`, … |
| `DurableStore` | CAS subset of routedrun store APIs |
| `ResultStore` | Terminal result persistence |
| `ControlJournalFactory` / `ControlJournalHandle` | Terminal-event fence |
| `ProgressEvent` / `ResultEvent` / `CheckpointEvent` | Authenticated worker events |
| `ClaimOptions` | Attempt claim parameters |
| `GovernedOperationKind` | model / HTTP / MCP in-flight ops |
| `ReferenceWorker` / `ReferenceWorkerConfig` | Deterministic conformance worker |
| `attemptTracker` | Per-attempt liveness/stall bookkeeping (unexported) |

## Key Functions

| Symbol | Role |
|--------|------|
| `NewSupervisor` | Construct supervisor over stores/journal |
| `ClaimForRun` | Create/claim attempt + lease |
| `TrackProgress` | Accept HMAC-verified progress |
| `HandleCheckpoint` / `HandleResult` | Checkpoint and terminal result |
| `HandleModelStart/End`, `HandleHTTP*`, `HandleMCP*` | Governed op brackets |
| `CheckStall` | Stall evaluation |
| `Cancel` / `Finalize` / `Cleanup` | Terminal paths |
| `Reconcile` | Restart-safe recovery |
| `UnauthenticatedActivity` | Explicitly rejected non-auth activity |
| `ReferenceWorker.Run` | Multi-phase signed event simulation |

## Architecture

```
AdmitInvocation (routedrun)
        |
        v
Supervisor.ClaimForRun --> Attempt + Lease + control key
        |
        +-- worker progress (HMAC) --> TrackProgress
        +-- governed ops --> stall deadline extension
        +-- checkpoint --> SaveCheckpoint
        +-- verified result --> CAS terminal + ResultStore
        v
Cleanup (revoke lease, release resources)

Daemon restart --> Supervisor.Reconcile
```

Sentinel errors (`ErrLeaseMismatch`, `ErrInvalidHMAC`, `ErrNotVerifiedResult`,
…) are stable for `errors.Is` matching.

## Usage

```go
sup := supervisor.NewSupervisor(durable, results, journals, opts...)
attemptID, err := sup.ClaimForRun(ctx, runID, invocationID)
if err != nil {
    return err
}
if err := sup.TrackProgress(ctx, attemptID, progressEvent); err != nil {
    return err
}
return sup.HandleResult(ctx, attemptID, resultEvent)
```
