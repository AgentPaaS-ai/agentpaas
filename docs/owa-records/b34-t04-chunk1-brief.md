# B34-T04 Chunk 1 — Linear pipeline controller CAS (no Docker)

**Worktree:** `/Users/pms88/projects/ap-b34-t04`
**Branch:** `feat/b34-t04-scheduler`
**Spec:** docs/execution/blocks/b34-summary.md § T04 items 1–3, 5–6 (library only)
**Priors:** T01–T03 done on main
**Chunk scope only. Do NOT implement container launch, daemon wire, pause full matrix, or T05+.**

## Goal

Implement a pure-Go **linear pipeline controller** that advances durable node/run
state via CAS with exactly-once logical effects for:

1. Admit-time fixed nodes already exist (simulate with store seed helpers)
2. Claim READY → LAUNCHING (create attempt/lease records in store; no Docker)
3. Mark ACTIVE (optional if NodeStatus has it) when "launch acknowledged"
4. On stage success + staged handoff candidate: **atomic** commit
   terminal stage result + handoff + next node READY (or workflow terminal
   on final stage)
5. Crash between logical effects must be replay-safe (idempotent commit)
6. At most one active/launching node for linear pipelines

No container driver. Fake clock optional. Use `routedrun.MemoryStore` (and
LocalStore if easy).

## Package

Prefer `internal/workflow/pipeline/controller.go` (+ controller_test.go)
reusing T01/T02 types. Or `internal/workflow/pipeline/scheduler/` if cleaner.

## Core API (suggested)

```go
type Controller struct {
    Store routedrun.Store // or narrower interface
    // Clock, IDGen injectable
}

// SeedPipelineWorkflow creates workflow + N nodes: stage0 READY, rest PENDING,
// with precreated run IDs. Used by tests and later admission.
func SeedPipelineWorkflow(...) (WorkflowID, []NodeID, error)

// ClaimNextReady CAS-claims the earliest READY node → LAUNCHING, creates
// attempt+lease with stable idempotency key workflow|node|generation.
// Returns nil,nil if nothing to claim or PAUSE_REQUESTED desired state.
func (c *Controller) ClaimNextReady(ctx, workflowID) (*Claim, error)

// AcknowledgeRunning moves LAUNCHING → RUNNING/ACTIVE for the claimed node.
func (c *Controller) AcknowledgeRunning(ctx, claim) error

// CommitStageSuccess atomically:
// - validates handoff candidate (or allows nil for final stage)
// - marks node+run SUCCEEDED
// - CommitHandoff if non-final
// - marks next node READY OR workflow SUCCEEDED if final
// - fences prior lease generation
// Must be idempotent if called twice with same success.
func (c *Controller) CommitStageSuccess(ctx, req StageSuccess) error

// CommitStageFailure marks node/run failed and workflow failed; no next READY.
func (c *Controller) CommitStageFailure(ctx, req StageFailure) error
```

Adapt names to existing routedrun enums/store methods. Read:
- internal/routedrun/interfaces.go
- internal/routedrun/memorystore.go (CreateNode, UpdateNode, CommitHandoff, CAS)
- internal/routedrun/enums.go NodeStatus*
- internal/routedrun/transitions.go
- internal/routedrun/localstore.go NestedPackageDigests stage meta (admission pattern)

If store lacks a needed atomic multi-record txn, implement controller-level
idempotency keys + generation CAS so double-apply is safe even if multi-step.

## Stable codes / errors

Reuse pipeline codes where relevant. Add controller errors as typed values:
`ErrNothingReady`, `ErrCASConflict`, `ErrHandoffMissing`, `ErrInvalidTransition`.

## Tests (must pass)

1. Two-stage happy path: claim0 → ack → success+handoff → claim1 → ack → final success → workflow SUCCEEDED; one handoff stored.
2. Three-stage happy path.
3. Double ClaimNextReady while stage launching: second claim no-ops or conflict; still one launching.
4. Double CommitStageSuccess identical: idempotent.
5. CommitStageSuccess without handoff on non-final → fail, no next READY.
6. CommitStageFailure stage0 → workflow failed, stage1 still PENDING never READY.
7. Claim after final: nothing ready.
8. CAS conflict simulation: stale generation update fails closed.
9. Restart simulation: re-seed store from memory snapshot mid-flight; ClaimNextReady does not duplicate completed stage.

No Docker. No daemon.

## Also

- Update docs/owa-records/b34-t04-chunk1.md handoff
- Conventional commits
- Do not touch Python SDK unless needed
- Do not enable pipeline in daemon

## Red gate

```bash
cd /Users/pms88/projects/ap-b34-t04
rg -n "CommitStageSuccess|ClaimNextReady" internal/workflow/pipeline/
go test ./internal/workflow/pipeline/... -count=1
go test -race ./internal/workflow/pipeline/... -count=1
test -f docs/owa-records/b34-t04-chunk1.md
git log --oneline main..HEAD
```

START NOW. Chunk 1 only.
