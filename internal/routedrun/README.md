# package routedrun

## Purpose

`routedrun` is the durable domain model for AgentPaaS routed runs. It defines
typed IDs, records, enums, state-transition rules, store contracts, and local
persistence used after invocation admission through attempt completion.

## Key Types

| Type | Role |
|------|------|
| `DeploymentID`, `InvocationID`, `RunID`, `AttemptID`, `WorkflowID`, … | Stable string ID types with SQL/JSON codecs |
| `DeploymentRecord`, `AliasRecord`, `InvocationReceipt` | Deployment and admission records |
| `RunRecord`, `AttemptRecord` | Run/attempt lifecycle with generations |
| `WorkflowRecord`, related node/service types | Workflow graph durable state |
| `TimeEnvelope` | Active-time / lease timing bounds for an attempt |
| `DeploymentStore`, `RunStore`, `WorkflowStore` | Store interfaces (CAS updates) |
| `LocalStore`, `MemoryStore` | Filesystem and in-memory implementations |
| `ArtifactWorkspace` / `ArtifactMetadata` | Validated artifact accept/list paths |
| `ControlJournal` helpers | Authenticated progress/control event journal |

## Key Functions

| Symbol | Role |
|--------|------|
| Transition validators | Enforce legal run/attempt/workflow status changes |
| `NewArtifactWorkspace` | Open a run-scoped artifact root |
| `(*ArtifactWorkspace).ValidateAndAccept` | Path-safe artifact intake with size caps |
| ID generators (`idgen`) | Create unique durable identifiers |
| WAL / migration helpers | Durable store recovery and schema evolution |
| `MarshalCanonical` / `UnmarshalStrict` | Deterministic JSON helpers |
| Resume/progress helpers | Continue attempts from checkpoints/progress |

## Architecture

```
AdmitInvocation (DeploymentStore)
        |
        v
  InvocationReceipt + workflow/run identity
        |
        v
  Attempt claim (RunStore) + TimeEnvelope
        |
        +-- progress / control journal
        +-- artifacts workspace
        +-- supervisor CAS transitions
        v
  Terminal result + ledger / cleanup
```

Persistence is generation-based: mutators take `expectedGeneration` and fail
on concurrent writers. Filesystem stores use structured directories under a
routed-run root supplied by the daemon.

## Usage

```go
store, err := routedrun.NewLocalStore(root)
if err != nil {
    return err
}
defer store.Close()

receipt, err := store.AdmitInvocation(ctx, req, depGeneration)
if err != nil {
    return err
}
run, err := store.GetRun(ctx, receipt.RunID)
```

Higher layers (`daemon`, `supervisor`) own orchestration; callers should treat
store CAS errors as concurrency conflicts and retry with fresh reads.
