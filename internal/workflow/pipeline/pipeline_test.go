package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"gopkg.in/yaml.v3"
)

// TestPipelineValidFixtures tests that all valid fixtures pass B34 validation.
func TestPipelineValidFixtures(t *testing.T) {
	validDir := filepath.Join("testdata", "pipeline", "valid")
	entries, err := os.ReadDir(validDir)
	if err != nil {
		t.Fatalf("read valid dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no valid fixture files found")
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			path := filepath.Join(validDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}
			codes := ValidatePipelineYAML(data)
			if len(codes) > 0 {
				t.Fatalf("expected no errors, got %v", codes)
			}
		})
	}
}

// TestPipelineInvalidFixtures tests that invalid fixtures fail with expected codes.
func TestPipelineInvalidFixtures(t *testing.T) {
	invalidDir := filepath.Join("testdata", "pipeline", "invalid")
	manifestPath := filepath.Join(invalidDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string][]string
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	for fixtureName, expectedCodes := range manifest {
		t.Run(fixtureName, func(t *testing.T) {
			path := filepath.Join(invalidDir, fixtureName)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}
			codes := ValidatePipelineYAML(data)
			if len(codes) == 0 {
				t.Fatal("expected validation errors, got none")
			}
			for _, want := range expectedCodes {
				if !ContainsCode(codes, want) {
					t.Errorf("expected code %q not found in errors: %v", want, codes)
				}
			}
		})
	}
}

// TestPipelineZeroStages fails with PIPELINE_STAGE_COUNT.
func TestPipelineZeroStages(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineStageCount) {
		t.Fatalf("expected %s, got %v", CodePipelineStageCount, codes)
	}
}

// TestPipelineOneStage fails with PIPELINE_STAGE_COUNT (B34 rejects < 2).
func TestPipelineOneStage(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{Name: "only", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:abc", Handoff: "public"},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineStageCount) {
		t.Fatalf("expected %s, got %v", CodePipelineStageCount, codes)
	}
}

// TestPipelineSeventeenStages fails with PIPELINE_STAGE_COUNT.
func TestPipelineSeventeenStages(t *testing.T) {
	stages := make([]pack.PipelineStage, 17)
	for i := 0; i < 17; i++ {
		stages[i] = pack.PipelineStage{
			Name:           "s" + string(rune('a'+i%26)),
			PackageName:    "p",
			PackageVersion: "1.0.0",
			BundleDigest:   "sha256:abc",
			Handoff:        "public",
		}
	}
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: stages,
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineStageCount) {
		t.Fatalf("expected %s, got %v", CodePipelineStageCount, codes)
	}
}

// TestPipelineDuplicateNode fails with PIPELINE_DUPLICATE_NODE.
func TestPipelineDuplicateNode(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{Name: "dup", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:abc", Handoff: "public"},
				{Name: "dup", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:def", Handoff: "internal"},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineDuplicateNode) {
		t.Fatalf("expected %s, got %v", CodePipelineDuplicateNode, codes)
	}
}

// TestPipelineMutableRef fails with PIPELINE_MUTABLE_REF for empty digest.
func TestPipelineMutableRef(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{Name: "a", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "", Handoff: "public"},
				{Name: "b", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:abc", Handoff: "internal"},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineMutableRef) {
		t.Fatalf("expected %s, got %v", CodePipelineMutableRef, codes)
	}
}

// TestPipelineMutableTag fails with PIPELINE_MUTABLE_REF for "latest" tag.
func TestPipelineMutableTag(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{Name: "a", PackageName: "p", PackageVersion: "latest", BundleDigest: "sha256:abc", Handoff: "public"},
				{Name: "b", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:def", Handoff: "internal"},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineMutableRef) {
		t.Fatalf("expected %s, got %v", CodePipelineMutableRef, codes)
	}
}

// TestPipelineSchemaMismatch fails when adjacent schemas are incompatible.
func TestPipelineSchemaMismatch(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{
					Name: "producer", PackageName: "p", PackageVersion: "1.0.0",
					BundleDigest: "sha256:abc", Handoff: "public",
					OutputSchema: "example/output/v1",
				},
				{
					Name: "consumer", PackageName: "q", PackageVersion: "1.0.0",
					BundleDigest: "sha256:def", Handoff: "public",
					AcceptedSchemas: []string{"example/different/v1"},
				},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineSchemaMismatch) {
		t.Fatalf("expected %s, got %v", CodePipelineSchemaMismatch, codes)
	}
}

// TestPipelineDeclassification fails when handoff classification weakens.
func TestPipelineDeclassification(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{Name: "a", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:abc", Handoff: "restricted"},
				{Name: "b", PackageName: "q", PackageVersion: "1.0.0", BundleDigest: "sha256:def", Handoff: "public"},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if !ContainsCode(codes, CodePipelineDeclassification) {
		t.Fatalf("expected %s, got %v", CodePipelineDeclassification, codes)
	}
}

// TestPipelineUndeclaredMCP validates code constant exists.
func TestPipelineUndeclaredMCP(t *testing.T) {
	// The undeclared MCP code fires when a stage references an MCP service not in the services list.
	// Stage-level MCP binding check is a future feature; for now verify the code catalog.
	if CodePipelineUndeclaredMCP != "PIPELINE_UNDECLARED_MCP" {
		t.Fatal("code constant mismatch")
	}
}

// TestPipelineUnsupportedShape loads branch_edges fixture and expects UNSUPPORTED_SHAPE.
func TestPipelineUnsupportedShape(t *testing.T) {
	path := filepath.Join("testdata", "pipeline", "invalid", "branch_edges.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	codes := ValidatePipelineYAML(data)
	if !ContainsCode(codes, CodePipelineUnsupportedShape) {
		t.Fatalf("expected %s, got %v", CodePipelineUnsupportedShape, codes)
	}
}

// TestLegacyStandalonePackValidationStillGreen ensures standalone workflows still pass pack validation.
func TestLegacyStandalonePackValidationStillGreen(t *testing.T) {
	yml := `kind: standalone
services:
  - service_id: feedback
    package_name: mcp-feedback-service
    package_version: "0.1.0"
    bundle_digest: sha256:placeholder
    allowed_tools:
      - lookup_feedback
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if errs := pack.ValidateWorkflowYAML(&wf); len(errs) > 0 {
		t.Fatalf("standalone workflow should pass pack validation: %v", errs)
	}
}

// TestLegacyMCPFeedbackFixtureStillGreen loads the actual B33 MCP feedback client
// workflow.yaml fixture from disk and verifies it passes pack validation.
func TestLegacyMCPFeedbackFixtureStillGreen(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "test", "e2e", "fixtures", "mcp-feedback-client", "workflow.yaml")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read MCP feedback fixture: %v", err)
	}
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("unmarshal MCP feedback fixture: %v", err)
	}
	if errs := pack.ValidateWorkflowYAML(&wf); len(errs) > 0 {
		t.Fatalf("MCP feedback fixture should pass pack validation: %v", errs)
	}
}

// TestKindPipelineValidationRejectsNonPipeline ensures non-pipeline kinds are skipped.
func TestKindPipelineValidationRejectsNonPipeline(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindStandalone,
	}
	codes := ValidatePipelineDeclarative(wf)
	if len(codes) > 0 {
		t.Fatalf("non-pipeline kind should not be validated: %v", codes)
	}
}

// TestPipelineSchemaCompatible validates adjacent schema match.
func TestPipelineSchemaCompatible(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{
					Name: "producer", PackageName: "p", PackageVersion: "1.0.0",
					BundleDigest: "sha256:abc", Handoff: "public",
					OutputSchema: "example/output/v1",
				},
				{
					Name: "consumer", PackageName: "q", PackageVersion: "1.0.0",
					BundleDigest: "sha256:def", Handoff: "public",
					AcceptedSchemas: []string{"example/output/v1"},
				},
			},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if len(codes) > 0 {
		t.Fatalf("expected no errors, got %v", codes)
	}
}

// TestPipelineWithMCPServiceDeclared validates pipeline with declared MCP services.
func TestPipelineWithMCPServiceDeclared(t *testing.T) {
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: []pack.PipelineStage{
				{Name: "a", PackageName: "p", PackageVersion: "1.0.0", BundleDigest: "sha256:abc", Handoff: "public"},
				{Name: "b", PackageName: "q", PackageVersion: "1.0.0", BundleDigest: "sha256:def", Handoff: "internal",
					OutputSchema: "s/v1", AcceptedSchemas: []string{"s/v1"}},
			},
		},
		Services: []pack.ServiceBinding{
			{ServiceID: "lookup", PackageName: "svc", PackageVersion: "1.0.0", BundleDigest: "sha256:svc"},
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if len(codes) > 0 {
		t.Fatalf("expected no errors, got %v", codes)
	}
}

// TestPipelineSixteenStages validates the maximum allowed stages.
func TestPipelineSixteenStages(t *testing.T) {
	stages := make([]pack.PipelineStage, 16)
	for i := 0; i < 16; i++ {
		name := "s" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		stages[i] = pack.PipelineStage{
			Name:           name,
			PackageName:    "p",
			PackageVersion: "1.0.0",
			BundleDigest:   "sha256:abc",
			Handoff:        "public",
		}
	}
	wf := &pack.WorkflowYAML{
		Kind: pack.WorkflowKindPipeline,
		Pipeline: &pack.PipelineConfig{
			Stages: stages,
		},
	}
	codes := ValidatePipelineDeclarative(wf)
	if len(codes) > 0 {
		t.Fatalf("expected no errors for 16 stages, got %v", codes)
	}
}
