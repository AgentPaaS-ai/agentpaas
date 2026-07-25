# B34-T02 Brief — Strict pipeline compilation

**Worktree:** `/Users/pms88/projects/ap-b34-t02`
**Branch:** `feat/b34-t02-pipeline-compile`
**Base:** main (includes T01)
**Spec:** `docs/execution/blocks/b34-summary.md` § T02
**Prior handoff:** `docs/owa-records/b34-t01.md`
**You are ap-worker. Implement + test + commit. Do NOT enable runtime scheduler (T04) or SDK (T03).**

## Goal

Turn validated `workflow.yaml` (pipeline kind) into one **immutable, policy-bound
compiled snapshot** with a deterministic digest. Runtime must be able to consume
the snapshot without re-reading mutable workflow source.

Also close T01 residuals:
1. `pack.ValidateWorkflowYAML` pipeline min stages = **2** (single source of truth)
2. Real `PIPELINE_UNDECLARED_MCP` via stage `mcp_services: []` vs workflow `services:`

## Hard constraints

- Do NOT enable `agentpaas_pipeline_not_enabled` flip / daemon scheduling.
- Do NOT implement SDK workflow_input/commit_handoff.
- Do NOT launch containers.
- Compilation is pure library + tests; optional pack/lock hook only if clean.
- Conventional commits. Write `docs/owa-records/b34-t02.md` when done.

## Design (implement in `internal/workflow/pipeline/`)

### Types (compile.go / snapshot.go)

```go
// CompiledPipelineSnapshot is the immutable execution plan (no generated IDs).
type CompiledPipelineSnapshot struct {
    SchemaVersion string `json:"schema_version"` // agentpaas.workflow.pipeline_snapshot/v1
    // Declarative only — no timestamps, no workflow_id, no run ids.
    Kind string `json:"kind"` // pipeline
    Stages []CompiledStage `json:"stages"`
    Services []CompiledServiceBinding `json:"services,omitempty"`
    Limits CompiledLimits `json:"limits"`
    // Digests of inputs that bound the snapshot.
    WorkflowYAMLDigest string `json:"workflow_yaml_digest"` // sha256 of canonical workflow bytes
    PolicyDigest string `json:"policy_digest,omitempty"`
    // NestedPackageDigests maps logical package name@version -> installed bundle digest
    NestedPackageDigests map[string]string `json:"nested_package_digests"`
    // SnapshotDigest is sha256 over canonical JSON of this struct with SnapshotDigest empty.
    SnapshotDigest string `json:"snapshot_digest"`
}

type CompiledStage struct {
    Order int `json:"order"`
    Name string `json:"name"`
    PackageName string `json:"package_name"`
    PackageVersion string `json:"package_version"`
    BundleDigest string `json:"bundle_digest"` // exact resolved
    HandoffClass string `json:"handoff"`
    OutputSchema string `json:"output_schema,omitempty"`
    AcceptedSchemas []string `json:"accepted_schemas,omitempty"`
    MCPServiceIDs []string `json:"mcp_service_ids,omitempty"` // resolved allowlist refs
    PolicyDigest string `json:"policy_digest,omitempty"` // per-stage if available
}

type CompiledServiceBinding struct {
    ServiceID string `json:"service_id"`
    PackageName string `json:"package_name"`
    PackageVersion string `json:"package_version"`
    BundleDigest string `json:"bundle_digest"`
    AllowedTools []string `json:"allowed_tools,omitempty"`
}

type CompiledLimits struct {
    MaxActiveDuration string `json:"max_active_duration,omitempty"`
    HandoffByteLimit int `json:"handoff_byte_limit,omitempty"`
    ArtifactLimit int `json:"artifact_limit,omitempty"`
    ActiveContainerLimit int `json:"active_container_limit,omitempty"`
    AggregateMaxTokens int `json:"aggregate_max_tokens,omitempty"`
    AggregateMaxLLMSpend string `json:"aggregate_max_llm_spend,omitempty"`
}
```

### PackageResolver interface (injectable for tests)

```go
type PackageResolver interface {
    // Resolve returns the installed signed bundle digest for name+version.
    // Must fail if not installed, digest mismatch vs declared, or mutable tag.
    Resolve(ctx context.Context, packageName, packageVersion, declaredDigest string) (resolvedDigest string, err error)
}
```

For tests: fake resolver that returns declared digest if non-empty and matches
a fixture map; rejects missing/mismatch.

Optional: thin adapter over `internal/registry` if a clean lookup already exists
— prefer interface + fake first so compile tests stay unit-level.

### CompilePipeline(ctx, wf *pack.WorkflowYAML, rawWorkflow []byte, policyDigest string, resolver PackageResolver) (*CompiledPipelineSnapshot, []string codes, error)

Steps:
1. Run `ValidatePipelineDeclarative` / ValidatePipelineYAML on inputs; fail closed with codes.
2. Require kind pipeline and 2–16 stages.
3. For each stage: Resolve package; require resolved digest == declared bundle_digest
   (TOCTOU: if resolver returns different digest → code / error `PIPELINE_DIGEST_MISMATCH` or reuse MUTABLE_REF / new stable code `PIPELINE_PACKAGE_RESOLVE`).
4. For each service binding: Resolve package similarly.
5. Schema adjacency (already in T01 validator) — re-check at compile.
6. MCP: each stage MCPServices id must be in wf.Services; copy into CompiledStage.MCPServiceIDs (do not grant).
7. Canonicalize stage order 0..n-1; sort nested digest map keys when hashing.
8. Compute WorkflowYAMLDigest = sha256 of raw workflow bytes (or canonical YAML if you define one — document choice; prefer hash of caller-supplied bytes for reproducibility).
9. Compute SnapshotDigest over canonical JSON (json.Encoder SetEscapeHTML false + sorted map keys). Empty SnapshotDigest field during hash.
10. Return snapshot; do not persist to disk in T02 unless you add an optional `WriteSnapshot(dir)` helper used only by tests.

### Determinism
- Same inputs → byte-identical canonical JSON and same SnapshotDigest.
- No time.Now in snapshot.
- Generated IDs never enter snapshot.

### pack residuals (required)
1. `validatePipelineStages`: min **2** stages (error text "at least 2").
2. `PipelineStage.MCPServices []string` yaml mcp_services
3. ValidatePipelineDeclarative: real UNDECLARED_MCP
4. Fix undeclared_mcp fixture + manifest to expect PIPELINE_UNDECLARED_MCP
5. pack test: one-stage pipeline rejected

### Tests (compile_test.go)
- Golden two-stage and three-stage compile digests (hardcode expected snapshot_digest after first run, or compare two compiles equal + structural fields).
- Recompile identical → equal SnapshotDigest and equal canonical bytes.
- Change one stage package digest → different SnapshotDigest.
- Resolver missing package → fail.
- Resolver returns different digest than declared → fail (TOCTOU).
- Schema mismatch still fails at compile.
- Unsupported shape negatives still fail.
- NestedPackageDigests contains every stage + service package key.
- Max 16-stage compiles.
- pack one-stage rejected + UNDECLARED_MCP real test.

## Acceptance

```bash
cd /Users/pms88/projects/ap-b34-t02
go test ./internal/workflow/pipeline/... -count=1
go test -race ./internal/workflow/pipeline/...
go test ./internal/pack/ -count=1 -run 'Workflow|MCPFeedback|Pipeline'
go test ./internal/daemon/ -count=1 -run 'Pipeline|NotEnabled'
# pack min-2:
go test ./internal/pack/ -count=1 -run 'OneStage|TooMany|ThreeStage'
test -f docs/owa-records/b34-t02.md
git log --oneline main..HEAD
```

Daemon still pipeline_not_enabled.

## Out of scope
T03 SDK, T04 scheduler, T05 containers, block34-gate, enabling pipeline runtime.

## Handoff docs/owa-records/b34-t02.md
Status DONE, SHAs, snapshot schema_version, how digest is computed, tests, gaps for T03/T04, next T03.
