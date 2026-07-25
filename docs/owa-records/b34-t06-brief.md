# B34-T06 — Artifact transfer + provenance (library)

**IMPLEMENT B34-T06 NOW. Do not ask questions. RED GATE must pass before WORKER_DONE.**

**Worktree:** `/Users/pms88/projects/ap-b34-t06`
**Branch:** `feat/b34-t06-artifacts`
**Spec:** docs/execution/blocks/b34-summary.md § T06
**PRE:** `git rev-parse HEAD | tee /tmp/b34-t06-pre.sha`

## Goal

Move large outputs via immutable artifact refs without hidden state copy.
Library + filesystem fixture tests. No live Docker required. No admission flip.

## Package

```
internal/workflow/pipeline/
  artifact_store.go      # Memory/FS artifact blob store for tests + Promote
  artifact_project.go    # ProjectArtifactsRO — materialize RO binds
  artifact_validate.go   # ValidateHandoffArtifacts against store + aggregates
  artifact_test.go       # RED GATE tests
```

Reuse `HandoffArtifact` and `isSafeRef` / digest checks from handoff.go / handoff_validate.go.
Reuse `routedrun.ArtifactRef` where converting to durable refs helps.

## API

```go
type ArtifactBlob struct {
  Ref HandoffArtifact // metadata
  // Content only in store, never in handoff context
}

type ArtifactStore interface {
  // Put commits bytes under artifact_id; computes sha256 digest if Ref.Digest empty.
  // Rejects path traversal in ImmutableRef, symlinks if writing via path helper.
  Put(ctx context.Context, meta HandoffArtifact, content []byte) (HandoffArtifact, error)
  Get(ctx context.Context, artifactID string) (meta HandoffArtifact, content []byte, err error)
  VerifyDigest(ctx context.Context, artifactID string) error
}

type MemoryArtifactStore struct { ... }

// WorkflowArtifactBudget tracks aggregate across stages.
type WorkflowArtifactBudget struct {
  MaxTotalBytes int64 // default e.g. 64<<20 for tests
  MaxCount      int
  UsedBytes     int64
  UsedCount     int
}

func (b *WorkflowArtifactBudget) Account(size int64) error // CodeArtifactBudgetExceeded

// PromoteHandoffArtifacts validates each HandoffArtifact against store:
// - exists, digest matches content, owner node/run match producer
// - classification not declassified vs envelope
// - isSafeRef
// - budget.Account each
// Returns durable list of promoted refs (immutable).
func PromoteHandoffArtifacts(ctx, store ArtifactStore, budget *WorkflowArtifactBudget, producerNode, producerRun string, arts []HandoffArtifact) ([]HandoffArtifact, error)

// ProjectPlan is RO bind list for next stage (no host path leak into handoff).
type ProjectPlan struct {
  // Container binds host:container:ro — host is under a workflow-owned projection root
  Binds []string
  // MountRoot is reserved SDK path e.g. /agentpaas/incoming-artifacts (container path)
  MountRoot string
}

// BuildROProjection creates temp dir under baseDir, writes verified content as files
// named by safe basename of ImmutableRef, returns ProjectPlan with :ro binds.
// Rejects symlink, device, path escape. Deletes projection on Cleanup().
func BuildROProjection(ctx, store ArtifactStore, baseDir string, arts []HandoffArtifact) (*ArtifactProjection, error)

type ArtifactProjection struct {
  Plan ProjectPlan
  Dir  string
}
func (p *ArtifactProjection) Cleanup() error

// ProvenanceChain walks handoff artifacts back to producer attempt IDs.
func ProvenanceChain(arts []HandoffArtifact) []string // owner run IDs in order
```

## Errors / codes

Add to codes.go if missing:
- `ARTIFACT_DIGEST_MISMATCH`
- `ARTIFACT_NOT_FOUND`
- `ARTIFACT_PATH_REJECTED`
- `ARTIFACT_BUDGET_EXCEEDED`
- `ARTIFACT_SYMLINK_REJECTED`
- `ARTIFACT_OWNER_MISMATCH`

## Tests (must PASS -race)

1. `TestPutGetRoundTrip` — content + digest sha256:
2. `TestPutRejectsPathTraversal` — `../etc/passwd`, absolute `/etc/passwd`
3. `TestVerifyDigestMismatch` — tamper content after put
4. `TestPromoteHappy` — put two arts, promote, budget counts
5. `TestPromoteBudgetExhausted` — MaxTotalBytes small → fail before all
6. `TestPromoteOwnerMismatch` — wrong OwnerNodeID
7. `TestBuildROProjection_ReadOnlyFiles` — files exist, content matches; chmod bits not world-writable
8. `TestBuildROProjection_RejectsUnsafeRef` — does not write outside Dir
9. `TestProjectionCleanup` — Dir removed
10. `TestMultiMegabyteHandoff` — 2MB blob in store, context stays small; promote+project OK
11. `TestProvenanceChain` — refs list producer runs
12. `TestClassificationNoDeclassify` — confidential envelope cannot promote public artifact if already validated in handoff; if separate check, enforce here

Keep existing handoff_test path traversal green.

## Do NOT

- Docker e2e
- T07 cancel
- Flip admission
- Put artifact bytes into HandoffEnvelope.Context

## Docs

docs/owa-records/b34-t06.md + update current-state when green

## RED GATE

```bash
cd /Users/pms88/projects/ap-b34-t06
PRE=$(cat /tmp/b34-t06-pre.sha)
rg -n "PromoteHandoffArtifacts|BuildROProjection|MemoryArtifactStore|ARTIFACT_" internal/workflow/pipeline/
go test ./internal/workflow/pipeline/ -count=1 -run 'Artifact|Promote|Projection|Provenance|PutGet|Budget' -v
go test ./internal/workflow/pipeline/ -race -count=1
test -f docs/owa-records/b34-t06.md
git log --oneline ${PRE}..HEAD
```

START NOW. T06 only.
