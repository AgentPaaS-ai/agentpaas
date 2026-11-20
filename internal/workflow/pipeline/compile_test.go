package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"gopkg.in/yaml.v3"
)

// fakeResolver is a test-only PackageResolver backed by a fixture map.
type fakeResolver struct {
	packages map[string]string // "name@version" -> resolved digest
}

func (f *fakeResolver) Resolve(packageName, packageVersion, declaredDigest string) (string, error) {
	key := packageName + "@" + packageVersion
	resolved, ok := f.packages[key]
	if !ok {
		return "", fmt.Errorf("package %s not installed", key)
	}
	if declaredDigest != "" && resolved != declaredDigest {
		// Return resolved digest; caller can check for mismatch.
		return resolved, nil
	}
	return resolved, nil
}

func makeFakeResolver(pkgs ...string) *fakeResolver {
	m := make(map[string]string)
	for i := 0; i+2 < len(pkgs); i += 3 {
		m[pkgs[i]+"@"+pkgs[i+1]] = pkgs[i+2]
	}
	return &fakeResolver{packages: m}
}

// twoStageWorkflowBytes returns the raw YAML for a valid 2-stage pipeline.
func twoStageWorkflowBytes() []byte {
	return []byte(`kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: fetcher
      package_version: "1.0.0"
      bundle_digest: sha256:aaa111
      handoff: public
      output_schema: example/raw/v1
    - name: process
      package_name: processor
      package_version: "1.0.0"
      bundle_digest: sha256:bbb222
      handoff: internal
      accepted_schemas:
        - example/raw/v1
`)
}

// parseWorkflowYAML parses raw YAML into WorkflowYAML.
func parseWorkflowYAML(data []byte) *pack.WorkflowYAML {
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil
	}
	return &wf
}

func TestCompilePipeline_TwoStageGolden(t *testing.T) {
	wf := parseWorkflowYAML(twoStageWorkflowBytes())
	if wf == nil {
		t.Fatal("parse failed")
	}
	resolver := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:aaa111",
		"processor", "1.0.0", "sha256:bbb222",
	)
	ctx := context.Background()
	snap, codes, err := CompilePipeline(ctx, wf, twoStageWorkflowBytes(), "", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) > 0 {
		t.Fatalf("unexpected codes: %v", codes)
	}
	if snap == nil {
		t.Fatal("snapshot is nil")
	}

	// Structural checks.
	if snap.SchemaVersion != SchemaVersionSnapshotV1 {
		t.Errorf("schema_version = %q, want %q", snap.SchemaVersion, SchemaVersionSnapshotV1)
	}
	if snap.Kind != "pipeline" {
		t.Errorf("kind = %q, want pipeline", snap.Kind)
	}
	if len(snap.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(snap.Stages))
	}
	if snap.Stages[0].Order != 0 {
		t.Errorf("stages[0].order = %d, want 0", snap.Stages[0].Order)
	}
	if snap.Stages[1].Order != 1 {
		t.Errorf("stages[1].order = %d, want 1", snap.Stages[1].Order)
	}
	if snap.Stages[0].Name != "fetch" {
		t.Errorf("stages[0].name = %q, want fetch", snap.Stages[0].Name)
	}
	if snap.Stages[0].BundleDigest != "sha256:aaa111" {
		t.Errorf("stages[0].bundle_digest = %q, want sha256:aaa111", snap.Stages[0].BundleDigest)
	}
	if snap.WorkflowYAMLDigest == "" {
		t.Error("workflow_yaml_digest is empty")
	}
	if snap.SnapshotDigest == "" {
		t.Error("snapshot_digest is empty")
	}
	if len(snap.NestedPackageDigests) != 2 {
		t.Errorf("nested_package_digests = %d entries, want 2", len(snap.NestedPackageDigests))
	}
}

func TestCompilePipeline_ThreeStageGolden(t *testing.T) {
	raw := []byte(`kind: pipeline
pipeline:
  stages:
    - name: research
      package_name: research-agent
      package_version: "2.0.0"
      bundle_digest: sha256:r1r2r3r4
      handoff: public
      output_schema: example/notes/v1
    - name: draft
      package_name: draft-agent
      package_version: "1.5.0"
      bundle_digest: sha256:d1d2d3d4
      handoff: internal
      accepted_schemas:
        - example/notes/v1
      output_schema: example/draft/v1
    - name: review
      package_name: review-agent
      package_version: "3.0.0"
      bundle_digest: sha256:v1v2v3v4
      handoff: confidential
      accepted_schemas:
        - example/draft/v1
      output_schema: example/reviewed/v1
`)
	wf := parseWorkflowYAML(raw)
	if wf == nil {
		t.Fatal("parse failed")
	}
	resolver := makeFakeResolver(
		"research-agent", "2.0.0", "sha256:r1r2r3r4",
		"draft-agent", "1.5.0", "sha256:d1d2d3d4",
		"review-agent", "3.0.0", "sha256:v1v2v3v4",
	)
	ctx := context.Background()
	snap, codes, err := CompilePipeline(ctx, wf, raw, "", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) > 0 {
		t.Fatalf("unexpected codes: %v", codes)
	}
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if len(snap.Stages) != 3 {
		t.Fatalf("stages = %d, want 3", len(snap.Stages))
	}
	if snap.SnapshotDigest == "" {
		t.Error("snapshot_digest is empty")
	}
	if len(snap.NestedPackageDigests) != 3 {
		t.Errorf("nested_package_digests = %d, want 3", len(snap.NestedPackageDigests))
	}
}

func TestCompilePipeline_RecompileDeterministic(t *testing.T) {
	raw := twoStageWorkflowBytes()
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:aaa111",
		"processor", "1.0.0", "sha256:bbb222",
	)
	ctx := context.Background()

	snap1, _, err := CompilePipeline(ctx, wf, raw, "", resolver)
	if err != nil {
		t.Fatalf("first compile: %v", err)
	}

	// Re-parse to get fresh objects.
	wf2 := parseWorkflowYAML(raw)
	snap2, _, err := CompilePipeline(ctx, wf2, raw, "", resolver)
	if err != nil {
		t.Fatalf("second compile: %v", err)
	}

	if snap1.SnapshotDigest != snap2.SnapshotDigest {
		t.Errorf("digests differ: %s vs %s", snap1.SnapshotDigest, snap2.SnapshotDigest)
	}
}

func TestCompilePipeline_DifferentDigestChangesSnapshot(t *testing.T) {
	raw := twoStageWorkflowBytes()
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:aaa111",
		"processor", "1.0.0", "sha256:bbb222",
	)
	ctx := context.Background()
	snap1, _, _ := CompilePipeline(ctx, wf, raw, "", resolver)

	// Different workflow (different stage digest).
	raw2 := []byte(`kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: fetcher
      package_version: "1.0.0"
      bundle_digest: sha256:aaa111
      handoff: public
      output_schema: example/raw/v1
    - name: process
      package_name: processor
      package_version: "1.0.0"
      bundle_digest: sha256:ccc333
      handoff: internal
      accepted_schemas:
        - example/raw/v1
`)
	wf2 := parseWorkflowYAML(raw2)
	resolver2 := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:aaa111",
		"processor", "1.0.0", "sha256:ccc333",
	)
	snap2, _, _ := CompilePipeline(ctx, wf2, raw2, "", resolver2)

	if snap1.SnapshotDigest == snap2.SnapshotDigest {
		t.Error("snapshot digests should differ when stage digest changes")
	}
}

func TestCompilePipeline_ResolverMissingPackage(t *testing.T) {
	wf := parseWorkflowYAML(twoStageWorkflowBytes())
	// No packages registered.
	resolver := makeFakeResolver()
	ctx := context.Background()
	_, codes, _ := CompilePipeline(ctx, wf, twoStageWorkflowBytes(), "", resolver)
	if !ContainsCode(codes, CodePipelinePackageResolve) {
		t.Fatalf("expected %s, got %v", CodePipelinePackageResolve, codes)
	}
}

func TestCompilePipeline_ResolverDigestMismatch(t *testing.T) {
	wf := parseWorkflowYAML(twoStageWorkflowBytes())
	// Resolver returns different digest than declared.
	resolver := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:WRONG",
		"processor", "1.0.0", "sha256:bbb222",
	)
	ctx := context.Background()
	_, codes, _ := CompilePipeline(ctx, wf, twoStageWorkflowBytes(), "", resolver)
	if !ContainsCode(codes, CodePipelineDigestMismatch) {
		t.Fatalf("expected %s, got %v", CodePipelineDigestMismatch, codes)
	}
}

func TestCompilePipeline_OneStageRejected(t *testing.T) {
	raw := []byte(`kind: pipeline
pipeline:
  stages:
    - name: only
      package_name: pkg
      package_version: "1.0.0"
      bundle_digest: sha256:abc
      handoff: public
`)
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver("pkg", "1.0.0", "sha256:abc")
	ctx := context.Background()
	_, codes, _ := CompilePipeline(ctx, wf, raw, "", resolver)
	if !ContainsCode(codes, CodePipelineStageCount) {
		t.Fatalf("expected %s, got %v", CodePipelineStageCount, codes)
	}
}

func TestCompilePipeline_SchemaMismatchAtCompile(t *testing.T) {
	raw := []byte(`kind: pipeline
pipeline:
  stages:
    - name: producer
      package_name: p
      package_version: "1.0.0"
      bundle_digest: sha256:aaa
      handoff: public
      output_schema: example/o/v1
    - name: consumer
      package_name: q
      package_version: "1.0.0"
      bundle_digest: sha256:bbb
      handoff: public
      accepted_schemas:
        - example/different/v1
`)
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver("p", "1.0.0", "sha256:aaa", "q", "1.0.0", "sha256:bbb")
	ctx := context.Background()
	_, codes, _ := CompilePipeline(ctx, wf, raw, "", resolver)
	if !ContainsCode(codes, CodePipelineSchemaMismatch) {
		t.Fatalf("expected %s, got %v", CodePipelineSchemaMismatch, codes)
	}
}

func TestCompilePipeline_UndeclaredMCPAtCompile(t *testing.T) {
	raw := []byte(`kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: pkg-a
      package_version: "1.0.0"
      bundle_digest: sha256:aaa
      handoff: public
      mcp_services:
        - undeclared-service
    - name: stage-b
      package_name: pkg-b
      package_version: "1.0.0"
      bundle_digest: sha256:bbb
      handoff: internal
`)
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver("pkg-a", "1.0.0", "sha256:aaa", "pkg-b", "1.0.0", "sha256:bbb")
	ctx := context.Background()
	_, codes, _ := CompilePipeline(ctx, wf, raw, "", resolver)
	if !ContainsCode(codes, CodePipelineUndeclaredMCP) {
		t.Fatalf("expected %s, got %v", CodePipelineUndeclaredMCP, codes)
	}
}

func TestCompilePipeline_SixteenStage(t *testing.T) {
	// Build 16-stage YAML programmatically.
	var b strings.Builder
	b.WriteString("kind: pipeline\n")
	b.WriteString("pipeline:\n")
	b.WriteString("  stages:\n")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "    - name: s%d\n", i)
		fmt.Fprintf(&b, "      package_name: pkg%d\n", i)
		fmt.Fprintf(&b, "      package_version: \"1.0.0\"\n")
		fmt.Fprintf(&b, "      bundle_digest: sha256:digest%d\n", i)
		fmt.Fprintf(&b, "      handoff: public\n")
	}
	raw := []byte(b.String())
	wf := parseWorkflowYAML(raw)
	if wf == nil {
		t.Fatal("parse failed")
	}

	pkgs := make([]string, 0, 16*3)
	for i := 0; i < 16; i++ {
		pkgs = append(pkgs, fmt.Sprintf("pkg%d", i), "1.0.0", fmt.Sprintf("sha256:digest%d", i))
	}
	resolver := makeFakeResolver(pkgs...)
	ctx := context.Background()
	snap, codes, err := CompilePipeline(ctx, wf, raw, "", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) > 0 {
		t.Fatalf("unexpected codes: %v", codes)
	}
	if len(snap.Stages) != 16 {
		t.Fatalf("stages = %d, want 16", len(snap.Stages))
	}
	if len(snap.NestedPackageDigests) != 16 {
		t.Errorf("nested_package_digests = %d, want 16", len(snap.NestedPackageDigests))
	}
}

func TestCompilePipeline_WithServices(t *testing.T) {
	raw := []byte(`kind: pipeline
pipeline:
  stages:
    - name: query
      package_name: query-agent
      package_version: "1.0.0"
      bundle_digest: sha256:q1q2q3q4
      handoff: public
      mcp_services:
        - lookup
    - name: enrich
      package_name: enrich-agent
      package_version: "1.0.0"
      bundle_digest: sha256:e1e2e3e4
      handoff: internal
services:
  - service_id: lookup
    package_name: lookup-service
    package_version: "1.0.0"
    bundle_digest: sha256:svc111
`)
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver(
		"query-agent", "1.0.0", "sha256:q1q2q3q4",
		"enrich-agent", "1.0.0", "sha256:e1e2e3e4",
		"lookup-service", "1.0.0", "sha256:svc111",
	)
	ctx := context.Background()
	snap, codes, err := CompilePipeline(ctx, wf, raw, "", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) > 0 {
		t.Fatalf("unexpected codes: %v", codes)
	}
	if len(snap.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(snap.Services))
	}
	if snap.Services[0].ServiceID != "lookup" {
		t.Errorf("service_id = %q, want lookup", snap.Services[0].ServiceID)
	}
	if snap.Stages[0].MCPServiceIDs == nil || len(snap.Stages[0].MCPServiceIDs) != 1 ||
		snap.Stages[0].MCPServiceIDs[0] != "lookup" {
		t.Errorf("stage[0] MCPs = %v, want [lookup]", snap.Stages[0].MCPServiceIDs)
	}
	// Nested digests should contain the service package too.
	svcKey := "lookup-service@1.0.0"
	if _, ok := snap.NestedPackageDigests[svcKey]; !ok {
		t.Errorf("service key %q missing from nested_package_digests", svcKey)
	}
}

func TestCompilePipeline_NotPipelineKind(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindStandalone,
	}
	ctx := context.Background()
	resolver := makeFakeResolver()
	_, codes, _ := CompilePipeline(ctx, wf, nil, "", resolver)
	if !ContainsCode(codes, CodePipelineUnsupportedShape) {
		t.Fatalf("expected %s for non-pipeline, got %v", CodePipelineUnsupportedShape, codes)
	}
}

func TestCompilePipeline_NilPipeline(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
	}
	ctx := context.Background()
	resolver := makeFakeResolver()
	_, codes, _ := CompilePipeline(ctx, wf, nil, "", resolver)
	if !ContainsCode(codes, CodePipelineStageCount) {
		t.Fatalf("expected %s for nil pipeline, got %v", CodePipelineStageCount, codes)
	}
}

func TestCompilePipeline_PolicyDigest(t *testing.T) {
	wf := parseWorkflowYAML(twoStageWorkflowBytes())
	resolver := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:aaa111",
		"processor", "1.0.0", "sha256:bbb222",
	)
	ctx := context.Background()
	snap, _, err := CompilePipeline(ctx, wf, twoStageWorkflowBytes(), "sha256:policyAAA", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.PolicyDigest != "sha256:policyAAA" {
		t.Errorf("policy_digest = %q, want sha256:policyAAA", snap.PolicyDigest)
	}
}

func TestCompilePipeline_LimitsPropagated(t *testing.T) {
	raw := []byte(`kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: fetcher
      package_version: "1.0.0"
      bundle_digest: sha256:aaa111
      handoff: public
      output_schema: example/raw/v1
    - name: process
      package_name: processor
      package_version: "1.0.0"
      bundle_digest: sha256:bbb222
      handoff: internal
      accepted_schemas:
        - example/raw/v1
max_active_duration: 10m
handoff_byte_limit: 65536
artifact_limit: 5
active_container_limit: 3
aggregate_max_tokens: 1000000
aggregate_max_llm_spend: "5.00"
`)
	wf := parseWorkflowYAML(raw)
	resolver := makeFakeResolver(
		"fetcher", "1.0.0", "sha256:aaa111",
		"processor", "1.0.0", "sha256:bbb222",
	)
	ctx := context.Background()
	snap, _, err := CompilePipeline(ctx, wf, raw, "", resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Limits.MaxActiveDuration != "10m" {
		t.Errorf("max_active_duration = %q, want 10m", snap.Limits.MaxActiveDuration)
	}
	if snap.Limits.HandoffByteLimit != 65536 {
		t.Errorf("handoff_byte_limit = %d, want 65536", snap.Limits.HandoffByteLimit)
	}
	if snap.Limits.ArtifactLimit != 5 {
		t.Errorf("artifact_limit = %d, want 5", snap.Limits.ArtifactLimit)
	}
	if snap.Limits.ActiveContainerLimit != 3 {
		t.Errorf("active_container_limit = %d, want 3", snap.Limits.ActiveContainerLimit)
	}
	if snap.Limits.AggregateMaxTokens != 1000000 {
		t.Errorf("aggregate_max_tokens = %d, want 1000000", snap.Limits.AggregateMaxTokens)
	}
	if snap.Limits.AggregateMaxLLMSpend != "5.00" {
		t.Errorf("aggregate_max_llm_spend = %q, want 5.00", snap.Limits.AggregateMaxLLMSpend)
	}
}
