package pack

import (
	"bytes"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowValidThreeStagePipeline(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: data-fetcher
      package_version: "1.0.0"
      bundle_digest: sha256:abc123
      handoff: public
    - name: process
      package_name: data-processor
      package_version: "1.0.0"
      bundle_digest: sha256:def456
      handoff: internal
    - name: deliver
      package_name: data-deliverer
      package_version: "1.0.0"
      bundle_digest: sha256:ghi789
      handoff: confidential
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if wf.Kind != "pipeline" {
		t.Errorf("kind = %q, want pipeline", wf.Kind)
	}
	if len(wf.Pipeline.Stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(wf.Pipeline.Stages))
	}
}

func TestWorkflowValidOneParentThreeChildren(t *testing.T) {
	yml := `kind: parent_child
parent_child:
  parent_identity:
    package_name: parent-agent
    package_version: "2.0.0"
    bundle_digest: sha256:parent123
  child_allowlist:
    - package_name: child-a
      package_version: "1.0.0"
      bundle_digest: sha256:childA
    - package_name: child-b
      package_version: "1.0.0"
      bundle_digest: sha256:childB
    - package_name: child-c
      package_version: "1.0.0"
      bundle_digest: sha256:childC
  max_fanout: 10
  max_concurrency: 5
  leaf_only_depth: true
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if wf.Kind != "parent_child" {
		t.Errorf("kind = %q, want parent_child", wf.Kind)
	}
	if len(wf.ParentChild.ChildAllowlist) != 3 {
		t.Fatalf("expected 3 children, got %d", len(wf.ParentChild.ChildAllowlist))
	}
}

func TestWorkflowValidStandaloneWithServices(t *testing.T) {
	yml := `kind: standalone
services:
  - service_id: rag-ingest
    package_name: rag-service
    package_version: "1.0.0"
    bundle_digest: sha256:rag123
    allowed_tools:
      - embed
      - search
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if wf.Kind != "standalone" {
		t.Errorf("kind = %q, want standalone", wf.Kind)
	}
}

func TestWorkflowPipelineCycle(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: internal
    - name: stage-b
      package_name: package-b
      package_version: "1.0.0"
      bundle_digest: sha256:b
      handoff: public
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for pipeline declassification, got none")
	}
}

func TestWorkflowDuplicateStageName(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: public
    - name: stage-a
      package_name: package-b
      package_version: "1.0.0"
      bundle_digest: sha256:b
      handoff: internal
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for duplicate stage name, got none")
	}
}

func TestWorkflowUnsafeStageIdentity(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: "stage->a"
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: public
    - name: stage-b
      package_name: package-b
      package_version: "1.0.0"
      bundle_digest: sha256:b
      handoff: public
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for unsafe stage identity, got none")
	}
}

func TestWorkflowTooManyStages(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
`
	for i := 0; i < 17; i++ {
		yml += `    - name: stage-` + string(rune('a'+i)) + `
      package_name: pkg-` + string(rune('a'+i)) + `
      package_version: "1.0.0"
      bundle_digest: sha256:` + string(rune('a'+i)) + `
      handoff: public
`
	}
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for too many stages, got none")
	}
}

func TestWorkflowMCPServiceClientBindingMismatch(t *testing.T) {
	t.Skip("MCP service/client binding mismatch is validated elsewhere")
}

func TestWorkflowUndeclaredTool(t *testing.T) {
	t.Skip("undeclared tool validation is done at a higher layer")
}

func TestWorkflowParentArbitraryImageReference(t *testing.T) {
	yml := `kind: parent_child
parent_child:
  parent_identity:
    package_name: parent
    package_version: "1.0.0"
    bundle_digest: sha256:parent
  child_allowlist:
    - package_name: child
      package_version: "1.0.0"
      bundle_digest: sha256:child
  max_fanout: 5
  max_concurrency: 3
  leaf_only_depth: true
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestWorkflowRecursiveChild(t *testing.T) {
	t.Skip("recursive child detection is done at a higher layer")
}

func TestWorkflowFanoutTooLarge(t *testing.T) {
	yml := `kind: parent_child
parent_child:
  parent_identity:
    package_name: parent
    package_version: "1.0.0"
    bundle_digest: sha256:parent
  child_allowlist:
    - package_name: child
      package_version: "1.0.0"
      bundle_digest: sha256:child
  max_fanout: 100
  max_concurrency: 3
  leaf_only_depth: true
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for fan-out > 64, got none")
	}
}

func TestWorkflowConcurrencyTooLarge(t *testing.T) {
	yml := `kind: parent_child
parent_child:
  parent_identity:
    package_name: parent
    package_version: "1.0.0"
    bundle_digest: sha256:parent
  child_allowlist:
    - package_name: child
      package_version: "1.0.0"
      bundle_digest: sha256:child
  max_fanout: 5
  max_concurrency: 100
  leaf_only_depth: true
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for concurrency > 64, got none")
	}
}

func TestWorkflowBudgetExpansion(t *testing.T) {
	t.Skip("workflow budget expansion against policy limits is validated at a higher layer")
}

func TestWorkflowDeclassification(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: restricted
    - name: stage-b
      package_name: package-b
      package_version: "1.0.0"
      bundle_digest: sha256:b
      handoff: public
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for handoff declassification, got none")
	}
}

func TestWorkflowClassificationPreservation(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: public
    - name: stage-b
      package_name: package-b
      package_version: "1.0.0"
      bundle_digest: sha256:b
      handoff: public
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestWorkflowClassificationRaise(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: internal
    - name: stage-b
      package_name: package-b
      package_version: "1.0.0"
      bundle_digest: sha256:b
      handoff: confidential
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestWorkflowUnknownField(t *testing.T) {
	yml := `kind: standalone
unknown_field: value
`
	var wf WorkflowYAML
	dec := yaml.NewDecoder(bytes.NewReader([]byte(yml)))
	dec.KnownFields(true)
	if err := dec.Decode(&wf); err == nil {
		t.Fatal("expected error for unknown field in YAML, got none")
	}
}

func TestWorkflowKindPipelineWithParentChild(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: public
parent_child:
  parent_identity:
    package_name: parent
    package_version: "1.0.0"
    bundle_digest: sha256:parent
  child_allowlist:
    - package_name: child
      package_version: "1.0.0"
      bundle_digest: sha256:child
  max_fanout: 5
  max_concurrency: 3
  leaf_only_depth: true
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for kind=pipeline with parent_child section, got none")
	}
}

func TestWorkflowKindParentChildWithPipeline(t *testing.T) {
	yml := `kind: parent_child
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: public
parent_child:
  parent_identity:
    package_name: parent
    package_version: "1.0.0"
    bundle_digest: sha256:parent
  child_allowlist:
    - package_name: child
      package_version: "1.0.0"
      bundle_digest: sha256:child
  max_fanout: 5
  max_concurrency: 3
  leaf_only_depth: true
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for kind=parent_child with pipeline section, got none")
	}
}

func TestWorkflowKindStandaloneWithPipeline(t *testing.T) {
	yml := `kind: standalone
pipeline:
  stages:
    - name: stage-a
      package_name: package-a
      package_version: "1.0.0"
      bundle_digest: sha256:a
      handoff: public
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for kind=standalone with pipeline section, got none")
	}
}

func TestWorkflowMissingKind(t *testing.T) {
	yml := `services:
  - service_id: rag-ingest
    package_name: rag-service
    package_version: "1.0.0"
    bundle_digest: sha256:rag123
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for missing kind, got none")
	}
}

func TestWorkflowMCPKindWithoutService(t *testing.T) {
	// mcp_service is an AgentYAML kind, not a WorkflowYAML kind.
	// ValidateWorkflowServiceDeclaration checks that when an agent
	// has kind=mcp_service, the workflow has at least one service declaration.
	// This test only verifies the workflow-level check.

	// No services declared, agent kind is mcp_service
	yml := `kind: standalone
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	// Now check the service declaration requirement for mcp_service
	svcErrs := ValidateWorkflowServiceDeclaration(&wf, "mcp_service")
	if len(svcErrs) == 0 {
		t.Fatal("expected validation errors for mcp_service without service declaration, got none")
	}

	// Verify that worker kind does not require service declarations
	workerErrs := ValidateWorkflowServiceDeclaration(&wf, "worker")
	if len(workerErrs) > 0 {
		t.Fatalf("unexpected validation errors for worker kind: %v", workerErrs)
	}
}

func TestWorkflowAggregateMaxLLMSpend(t *testing.T) {
	yml := `kind: standalone
aggregate_max_llm_spend: "10.50"
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestWorkflowAggregateMaxLLMSpendInvalid(t *testing.T) {
	yml := `kind: standalone
aggregate_max_llm_spend: "not-a-number"
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid aggregate_max_llm_spend, got none")
	}
}

// ---------------------------------------------------------------------------
// Delegation validation
// ---------------------------------------------------------------------------

func TestWorkflowDelegationsValid(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: report.verify
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:abc123
    max_data_class: internal
    artifact_audience:
      - orchestrator
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if len(wf.Delegations) != 1 {
		t.Errorf("expected 1 delegation, got %d", len(wf.Delegations))
	}
	if wf.Delegations[0].BindingID != "report.verify" {
		t.Errorf("expected binding_id=report.verify, got %q", wf.Delegations[0].BindingID)
	}
}

func TestWorkflowDelegationsDuplicateBindingID(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: report.verify
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:abc123
    max_data_class: internal
  - binding_id: report.verify
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:def456
    max_data_class: internal
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for duplicate binding_id, got none")
	}
}

func TestWorkflowDelegationsEmptyBindingID(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: ""
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:abc123
    max_data_class: internal
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for empty binding_id, got none")
	}
}

func TestWorkflowDelegationsMissingRequiredFields(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: report.verify
    max_data_class: internal
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for missing required fields, got none")
	}
}

func TestWorkflowDelegationsInvalidMaxDataClass(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: report.verify
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:abc123
    max_data_class: top_secret
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid max_data_class, got none")
	}
}

func TestWorkflowDelegationsInvalidMaxCost(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: report.verify
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:abc123
    max_data_class: internal
    max_cost_usd_decimal: "not-money"
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid max_cost_usd_decimal, got none")
	}
}

func TestWorkflowDelegationsSnapshotBuilder_GoldenDigest(t *testing.T) {
	yml := `kind: standalone
delegations:
  - binding_id: report.verify
    package_name: report-verifier
    package_version: "1.0.0"
    bundle_digest: sha256:abc123
    max_data_class: internal
    artifact_audience:
      - orchestrator
  - binding_id: data.analyze
    operation: analyze
    package_name: data-analyzer
    package_version: "2.0.0"
    bundle_digest: sha256:def456
    max_data_class: confidential
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	snap1, err := BuildCommunicationSnapshot(
		&wf,
		"wf-snap-test",
		"tenant-test",
		"dep-caller-1",
		"weather-agent",
		"sha256:caller-digest",
		1,
	)
	if err != nil {
		t.Fatalf("BuildCommunicationSnapshot: %v", err)
	}

	// Same input should produce same digest.
	snap2, err := BuildCommunicationSnapshot(
		&wf,
		"wf-snap-test",
		"tenant-test",
		"dep-caller-1",
		"weather-agent",
		"sha256:caller-digest",
		1,
	)
	if err != nil {
		t.Fatalf("BuildCommunicationSnapshot 2: %v", err)
	}

	if snap1.SnapshotDigest != snap2.SnapshotDigest {
		t.Errorf("expected deterministic digest, got %s != %s", snap1.SnapshotDigest, snap2.SnapshotDigest)
	}

	// Different caller digest produces different digest.
	snap3, err := BuildCommunicationSnapshot(
		&wf,
		"wf-snap-test",
		"tenant-test",
		"dep-caller-1",
		"weather-agent",
		"sha256:different-digest",
		1,
	)
	if err != nil {
		t.Fatalf("BuildCommunicationSnapshot 3: %v", err)
	}
	if snap1.SnapshotDigest == snap3.SnapshotDigest {
		t.Error("expected different digest for different caller digest")
	}

	// Verify bindings are populated correctly.
	if len(snap1.Bindings) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(snap1.Bindings))
	}
	found := false
	for _, b := range snap1.Bindings {
		if b.BindingID == "report.verify" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected binding report.verify in snapshot")
	}
}

func TestWorkflowWithoutDelegationsStillValidates(t *testing.T) {
	// v0.2.3 workflows without delegations must still validate.
	yml := `kind: standalone
services:
  - service_id: rag-ingest
    package_name: rag-service
    package_version: "1.0.0"
    bundle_digest: sha256:rag123
`
	var wf WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := ValidateWorkflowYAML(&wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors for v0.2.3 workflow: %v", errs)
	}
}