package pipeline

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"gopkg.in/yaml.v3"
)

// unsupportedTopLevelKeys lists top-level YAML keys that are not supported
// in pipeline workflows. Detection is done via yaml.Node parse before unmarshal.
var unsupportedTopLevelKeys = map[string]bool{
	"branches":   true,
	"edges":      true,
	"cycle":      true,
	"compensate": true,
	"on_failure": true,
	"mcp_services": true,
	"host_path":  true,
}

// ValidatePipelineDeclarative validates a parsed pipeline workflow YAML against
// B34 conformance rules. Returns a list of stable failure codes.
// Non-pipeline kinds are skipped (return nil).
func ValidatePipelineDeclarative(wf *pack.WorkflowYAML) []string {
	if wf == nil || wf.Kind != pack.WorkflowKindPipeline {
		return nil
	}

	var codes []string

	if wf.Pipeline == nil {
		codes = append(codes, CodePipelineStageCount)
		return codes
	}

	stages := wf.Pipeline.Stages

	// Stage count: 2–16 for pipeline kind.
	if len(stages) < 2 || len(stages) > 16 {
		codes = append(codes, CodePipelineStageCount)
		if len(stages) < 2 {
			return codes // no further stage-level checks possible
		}
	}

	var stageNames = make(map[string]bool)
	serviceIDs := make(map[string]bool)
	for _, svc := range wf.Services {
		serviceIDs[svc.ServiceID] = true
	}
	prevRank := -1
	var prevOutputSchema string

	for i, stage := range stages {
		prefix := fmt.Sprintf("pipeline.stages[%d]", i)

		// Duplicate node name detection.
		if stage.Name != "" {
			if stageNames[stage.Name] {
				codes = append(codes, CodePipelineDuplicateNode)
				_ = prefix // used in diagnostic; codes pin the stable code
			}
			stageNames[stage.Name] = true
		}

		// Mutable ref detection (empty digest, "latest" tag).
		if stage.BundleDigest == "" || strings.TrimSpace(stage.BundleDigest) == "" {
			codes = append(codes, CodePipelineMutableRef)
		}
		if stage.PackageVersion == "latest" || stage.PackageVersion == "" {
			codes = append(codes, CodePipelineMutableRef)
		}

		// Host path detection in package name (must not look like a file path).
		if strings.Contains(stage.PackageName, "/") || strings.HasPrefix(stage.PackageName, ".") {
			codes = append(codes, CodePipelineUnsupportedShape)
		}

		// MCP undeclared check: each stage's mcp_services must reference
		// a service_id declared in the workflow's services list.
		for _, mcpID := range stage.MCPServices {
			if !serviceIDs[mcpID] {
				codes = append(codes, CodePipelineUndeclaredMCP)
				break // one undeclared is enough per stage
			}
		}

		// Handoff declassification check.
		rank := handoffClassificationRank(stage.Handoff)
		if rank == -1 {
			codes = append(codes, CodePipelineDeclassification)
		} else {
			if prevRank != -1 && rank < prevRank {
				codes = append(codes, CodePipelineDeclassification)
			}
			prevRank = rank
		}

		// Schema compatibility check (for stages after the first).
		if i > 0 && prevOutputSchema != "" {
			if len(stage.AcceptedSchemas) == 0 {
				codes = append(codes, CodePipelineSchemaMismatch)
			} else {
				found := false
				for _, accepted := range stage.AcceptedSchemas {
					if accepted == prevOutputSchema {
						found = true
						break
					}
				}
				if !found {
					codes = append(codes, CodePipelineSchemaMismatch)
				}
			}
		}

		// Track output schema for next stage.
		prevOutputSchema = stage.OutputSchema
	}

	return codes
}

// ValidatePipelineYAML parses raw YAML bytes and validates them as a pipeline workflow.
// Uses yaml.Node to detect unsupported top-level keys before unmarshal.
// Returns stable failure codes or nil on success.
func ValidatePipelineYAML(data []byte) []string {
	// Parse as yaml.Node to check for unsupported top-level keys.
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return []string{CodePipelineUnsupportedShape}
	}

	// Check top-level mapping keys for unsupported fields.
	var codes []string
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		top := root.Content[0]
		if top.Kind == yaml.MappingNode {
			for i := 0; i < len(top.Content); i += 2 {
				key := top.Content[i]
				if key.Kind == yaml.ScalarNode {
					if unsupportedTopLevelKeys[key.Value] {
						codes = append(codes, CodePipelineUnsupportedShape)
						break // one unsupported key is enough
					}
					// Also check for any key not known to WorkflowYAML struct.
					// We detect unsupported keys beyond the known set by
					// checking against a whitelist.
					if !isKnownTopLevelKey(key.Value) {
						codes = append(codes, CodePipelineUnsupportedShape)
						break
					}
				}
			}
		}
	}

	// Now unmarshal into WorkflowYAML for standard validation.
	var wf pack.WorkflowYAML
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&wf); err != nil {
		// KnownFields failure → unsupported shape or unknown field.
		if !containsCode(codes, CodePipelineUnsupportedShape) {
			codes = append(codes, CodePipelineUnsupportedShape)
		}
	}

	codes = append(codes, ValidatePipelineDeclarative(&wf)...)
	return codes
}

// knownTopLevelKeys is the set of valid top-level keys in a WorkflowYAML.
var knownTopLevelKeys = map[string]bool{
	"kind":                    true,
	"pipeline":                true,
	"parent_child":            true,
	"services":                true,
	"delegations":             true,
	"max_active_duration":     true,
	"handoff_byte_limit":      true,
	"artifact_limit":          true,
	"active_container_limit":  true,
	"aggregate_max_tokens":    true,
	"aggregate_max_llm_spend": true,
}

func isKnownTopLevelKey(k string) bool {
	return knownTopLevelKeys[k]
}

// ContainsCode checks if a code is present in the slice.
func ContainsCode(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

// containsCode is the internal lowercase alias.
func containsCode(codes []string, want string) bool {
	return ContainsCode(codes, want)
}

// handoffClassificationRank returns the rank of a handoff classification.
func handoffClassificationRank(c string) int {
	return classificationRank(c)
}
