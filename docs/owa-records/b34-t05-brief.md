# B34-T05 — Stage containers + authority isolation (library-first)

**IMPLEMENT B34-T05 NOW. Do not ask questions. Do not claim complete until RED GATE passes.**

**Worktree:** `/Users/pms88/projects/ap-b34-t05`
**Branch:** `feat/b34-t05-stage-containers`
**Base:** main @ 11e7de4 (T04 complete)
**Spec:** docs/execution/blocks/b34-summary.md § T05
**PRE:** `git rev-parse HEAD | tee /tmp/b34-t05-pre.sha`

## Goal

Prove a pipeline is multi-container execution with independent authority — not
in-process function chaining. **Library + mock RuntimeDriver first.** Live
Docker e2e is nice-to-have if Colima is up; do not block the task on flaky
Docker. Do **not** flip `agentpaas_pipeline_not_enabled` admission unless
required for a unit test that still fails closed in production path.

## Package layout

```
internal/runtime/naming.go          # ADD pipeline stage label constants + helper
internal/workflow/pipeline/
  stage_spec.go                     # BuildStageContainerSpec, StageLaunchPlan
  stage_authority.go                # IntersectStageAuthority
  stage_launcher.go                 # RuntimeStageLauncher (uses runtime.RuntimeDriver)
  stage_fence.go                    # FenceStage — stop/remove prior stage containers
  stage_isolation_test.go           # RED GATE tests (mock driver)
```

Keep FakeLauncher working for T04 tests. New RuntimeStageLauncher is optional path.

## Labels (required — no secrets)

Add to `internal/runtime/naming.go` (or pipeline if cleaner, but prefer runtime for Docker discoverability):

```go
LabelWorkflowID     // may already exist = "agentpaas.workflow_id"
LabelNodeID         = "agentpaas.node_id"
LabelAttemptID      = "agentpaas.attempt_id"
LabelPackageDigest  = "agentpaas.package_digest"
LabelPolicyDigest   = "agentpaas.policy_digest"
LabelLeaseGeneration = "agentpaas.lease_generation"
LabelPipelineStage  = "agentpaas.pipeline_stage" // "true"
LabelStageOrder     = "agentpaas.stage_order"
```

```go
func PipelineStageLabels(workflowID, nodeID, runID, attemptID, packageDigest, policyDigest string, leaseGen int64, stageOrder int) map[string]string
```

Must include LabelManagedBy=agentpaas, LabelResourceType=agent, LabelRunID, and all pipeline fields.
**Reject** any label value containing newline or NUL (reuse existing sanitization if present).
**Never** put credential values, tokens, or secret env values into labels.

## Stage container spec builder

```go
type StageLaunchRequest struct {
  WorkflowID, NodeID, RunID, AttemptID string
  StageOrder int
  PackageDigest, PolicyDigest, Image string
  LeaseGeneration int64
  NetworkID string          // stage-private internal net (required non-empty for isolation tests)
  ReadOnlyArtifactBinds []string // optional; must be ":ro" suffix
  WritableWorkDirBind string     // optional single RW bind unique per stage
  Env []string                 // must NOT include secret-looking KEY= where value is credential
  CPUQuota, MaxPIDs int64
  MemoryLimitBytes int64
}

// BuildStageContainerSpec returns runtime.ContainerSpec with:
// - Labels = PipelineStageLabels(...)
// - NetworkIDs = []string{req.NetworkID} exactly one
// - Binds = RO artifacts + optional unique RW workdir
// - no CapAdd NET_ADMIN unless explicitly justified (default empty CapAdd)
func BuildStageContainerSpec(req StageLaunchRequest) (runtime.ContainerSpec, error)
```

Validation errors (stable):
- empty Image, empty NetworkID, empty RunID/NodeID/WorkflowID
- any bind that is RW and path-collides with another stage's pattern (document rule: workdir must contain nodeID or runID)
- label sanitization failure

## Authority intersection

```go
type StageAuthority struct {
  AllowHosts []string
  AllowMCP   []string
  MaxActiveMs int64
  MaxLLMSpend string // decimal; empty ok
  NetworkEgress bool
}

// IntersectStageAuthority returns intersection of workflow-level and stage-package authority.
// Hosts/MCP: set intersection. MaxActiveMs: min of positive. NetworkEgress: AND.
// Never copies prior stage authority wholesale.
func IntersectStageAuthority(workflow, stage StageAuthority) StageAuthority
```

Tests: different adjacent stages → different AllowHosts; workflow host not in stage → dropped.

## RuntimeStageLauncher

```go
type RuntimeStageLauncher struct {
  Driver runtime.RuntimeDriver // interface from driver.go
  // optional: previous container IDs by launch key for fence
  mu sync.Mutex
  active map[string]runtime.ContainerID // launchKey -> container
}

// EnsureLaunch implements StageLauncher:
// 1. If job already has container recorded and Inspect says running → return nil
// 2. Build StageLaunchRequest from job metadata (extend StageLaunchJob with optional Image/NetworkID/digests fields OR store side table)
// 3. Create+Start via Driver
// 4. Record container ID on job or side map
//
// For tests: use mock driver from runtime/driver_test.go patterns (copy minimal mock into pipeline_test or export).
```

**Extend StageLaunchJob** with optional fields (zero-value safe for old tests):
```go
Image, NetworkID, PackageDigest, PolicyDigest string
StageOrder int
ContainerID string // set after launch
```

FakeLauncher ignores new fields.

## Fence previous stage

```go
// FenceStage stops and removes containers labeled with workflowID+nodeID (or by recorded ID).
// Must be called before launching next stage in a higher-level Advance helper OR from tests.
func (l *RuntimeStageLauncher) FenceStage(ctx, workflowID, nodeID string) error
```

```go
// AdvanceWithFence: Fence previous node (if any) then ReconcileOnce — test helper in pipeline package.
func AdvanceWithFence(ctx, rec *Reconciler, launcher *RuntimeStageLauncher, wfID, prevNodeID string) (*Claim, error)
```

## Isolation invariants (tests with mock driver)

1. `TestBuildStageContainerSpec_LabelsComplete` — all required labels present; no secret keys in labels
2. `TestBuildStageContainerSpec_SingleNetwork` — exactly one NetworkID
3. `TestBuildStageContainerSpec_RejectsSharedWritableBind` — two stages cannot share same RW bind path
4. `TestIntersectStageAuthority_DropsExtraHosts`
5. `TestRuntimeStageLauncher_EnsureLaunchIdempotent` — second EnsureLaunch does not Create twice (mock counts Create calls)
6. `TestFenceStage_StopsPriorBeforeNext` — after fence, mock shows Stop+Remove called on prior ID; next Create uses different NetworkID
7. `TestTwoStageSpecs_NoSharedNetworkOrRWVolume` — stage0 and stage1 specs have distinct NetworkIDs and distinct RW binds
8. `TestLabelsRejectSecrets` — Env may have non-secret; Labels never contain values matching `sk-` or `api_key` patterns if mistakenly passed (builder strips secret-like env from being copied to labels)

Optional if Docker available (`AGENTPAAS_DOCKER_TESTS=1`):
9. Real two-container create with labels — skip if no docker

## Do NOT

- Remove agentpaas_pipeline_not_enabled gate
- Implement full T06 artifact content transfer (only RO bind plumbing OK)
- Implement T07 cancel matrix
- Import moby only via existing runtime package
- Break T04 tests (`go test ./internal/workflow/pipeline/ -race`)

## Docs

- `docs/owa-records/b34-t05.md` handoff
- Update `docs/execution/current-state.md` when green

## RED GATE

```bash
cd /Users/pms88/projects/ap-b34-t05
PRE=$(cat /tmp/b34-t05-pre.sha)

rg -n "PipelineStageLabels|BuildStageContainerSpec|IntersectStageAuthority|FenceStage|RuntimeStageLauncher" internal/

go test ./internal/workflow/pipeline/ -count=1 -run 'Stage|Authority|Fence|BuildStage|RuntimeStage|Isolation|Labels' -v
go test ./internal/workflow/pipeline/ -race -count=1
go test ./internal/runtime/ -count=1 -run 'Label|PipelineStage' -v  # if labels added there
test -f docs/owa-records/b34-t05.md
git log --oneline ${PRE}..HEAD
```

Conventional commits `feat(b34-t05):` / `test(b34-t05):`

START NOW. T05 only. WORKER_DONE only after RED GATE green.
