package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// CompilePipeline turns a validated pipeline WorkflowYAML into a compiled,
// immutable snapshot with a deterministic digest. The caller supplies the raw
// workflow bytes (for reproducible digest) and an optional policy digest.
//
// Returns the compiled snapshot and a list of stable failure codes (empty on
// success). The snapshot is not persisted to disk by this function.
func CompilePipeline(
	ctx context.Context,
	wf *pack.WorkflowYAML,
	rawWorkflow []byte,
	policyDigest string,
	resolver PackageResolver,
) (*CompiledPipelineSnapshot, []string, error) {
	// 1. Validate declarative rules.
	codes := ValidatePipelineDeclarative(wf)
	if len(codes) > 0 {
		return nil, codes, nil
	}

	// 2. Require pipeline kind.
	if wf.Kind != pack.WorkflowKindPipeline {
		return nil, []string{CodePipelineUnsupportedShape}, nil
	}
	if wf.Pipeline == nil {
		return nil, []string{CodePipelineStageCount}, nil
	}

	stages := wf.Pipeline.Stages

	// 3. Build service ID set for MCP validation.
	serviceIDs := make(map[string]bool)
	for _, svc := range wf.Services {
		serviceIDs[svc.ServiceID] = true
	}

	// 4. Compile stages.
	compiledStages := make([]CompiledStage, 0, len(stages))
	nestedDigests := make(map[string]string)
	var prevOutputSchema string

	for i, stage := range stages {
		// Resolve package.
		resolvedDigest, err := resolver.Resolve(
			stage.PackageName, stage.PackageVersion, stage.BundleDigest,
		)
		if err != nil {
			// Check if it's a mismatch or not-installed.
			if resolvedDigest != "" && resolvedDigest != stage.BundleDigest {
				codes = append(codes, CodePipelineDigestMismatch)
				return nil, codes, nil
			}
			codes = append(codes, CodePipelinePackageResolve)
			return nil, codes, nil
		}

		// TOCTOU: if resolver returns a different digest than declared.
		if resolvedDigest != stage.BundleDigest {
			codes = append(codes, CodePipelineDigestMismatch)
			return nil, codes, nil
		}

		// MCP validation: every mcp_service must be in workflow services.
		var mcpIDs []string
		for _, mcpID := range stage.MCPServices {
			if !serviceIDs[mcpID] {
				codes = append(codes, CodePipelineUndeclaredMCP)
				return nil, codes, nil
			}
			mcpIDs = append(mcpIDs, mcpID)
		}

		// Schema adjacency re-check at compile time.
		if i > 0 && prevOutputSchema != "" {
			if len(stage.AcceptedSchemas) == 0 {
				codes = append(codes, CodePipelineSchemaMismatch)
				return nil, codes, nil
			}
			found := false
			for _, accepted := range stage.AcceptedSchemas {
				if accepted == prevOutputSchema {
					found = true
					break
				}
			}
			if !found {
				codes = append(codes, CodePipelineSchemaMismatch)
				return nil, codes, nil
			}
		}

		cs := CompiledStage{
			Order:           i,
			Name:            stage.Name,
			PackageName:     stage.PackageName,
			PackageVersion:  stage.PackageVersion,
			BundleDigest:    resolvedDigest,
			HandoffClass:    stage.Handoff,
			OutputSchema:    stage.OutputSchema,
			AcceptedSchemas: append([]string{}, stage.AcceptedSchemas...),
			MCPServiceIDs:   mcpIDs,
		}
		compiledStages = append(compiledStages, cs)

		// Track in nested digest map.
		key := stage.PackageName + "@" + stage.PackageVersion
		nestedDigests[key] = resolvedDigest

		prevOutputSchema = stage.OutputSchema
	}

	// 5. Compile service bindings.
	compiledServices := make([]CompiledServiceBinding, 0, len(wf.Services))
	for _, svc := range wf.Services {
		resolvedDigest, err := resolver.Resolve(
			svc.PackageName, svc.PackageVersion, svc.BundleDigest,
		)
		if err != nil {
			if resolvedDigest != "" && resolvedDigest != svc.BundleDigest {
				codes = append(codes, CodePipelineDigestMismatch)
				return nil, codes, nil
			}
			codes = append(codes, CodePipelinePackageResolve)
			return nil, codes, nil
		}
		if resolvedDigest != svc.BundleDigest {
			codes = append(codes, CodePipelineDigestMismatch)
			return nil, codes, nil
		}

		csb := CompiledServiceBinding{
			ServiceID:      svc.ServiceID,
			PackageName:    svc.PackageName,
			PackageVersion: svc.PackageVersion,
			BundleDigest:   resolvedDigest,
			AllowedTools:   append([]string{}, svc.AllowedTools...),
		}
		compiledServices = append(compiledServices, csb)

		key := svc.PackageName + "@" + svc.PackageVersion
		nestedDigests[key] = resolvedDigest
	}

	// 6. Compute workflow YAML digest.
	yamlDigest := sha256Hex(rawWorkflow)

	// 7. Build limits.
	limits := CompiledLimits{
		MaxActiveDuration:    wf.MaxActiveDuration,
		HandoffByteLimit:     wf.HandoffByteLimit,
		ArtifactLimit:        wf.ArtifactLimit,
		ActiveContainerLimit: wf.ActiveContainerLimit,
		AggregateMaxTokens:   wf.AggregateMaxTokens,
		AggregateMaxLLMSpend: wf.AggregateMaxLLMSpend,
	}

	// 8. Sort nested digest map keys for canonical output.
	sortedNestedDigests := make(map[string]string, len(nestedDigests))
	for k, v := range nestedDigests {
		sortedNestedDigests[k] = v
	}

	snapshot := &CompiledPipelineSnapshot{
		SchemaVersion:        SchemaVersionSnapshotV1,
		Kind:                 wf.Kind,
		Stages:               compiledStages,
		Services:             compiledServices,
		Limits:               limits,
		WorkflowYAMLDigest:   yamlDigest,
		PolicyDigest:         policyDigest,
		NestedPackageDigests: sortedNestedDigests,
	}

	// 9. Compute snapshot digest.
	digest, err := computeSnapshotDigest(snapshot)
	if err != nil {
		return nil, nil, fmt.Errorf("compute snapshot digest: %w", err)
	}
	snapshot.SnapshotDigest = digest

	return snapshot, nil, nil
}

// computeSnapshotDigest computes sha256 over canonical JSON with SnapshotDigest empty.
func computeSnapshotDigest(snapshot *CompiledPipelineSnapshot) (string, error) {
	// Make a copy with empty SnapshotDigest.
	clone := *snapshot
	clone.SnapshotDigest = ""
	// Sort nested digest map keys.
	clone.NestedPackageDigests = sortedMapCopy(snapshot.NestedPackageDigests)

	data, err := canonicalJSON(&clone)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

// canonicalJSON marshals v to JSON with sorted keys and no HTML escaping.
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder appends a newline; strip it for digest consistency.
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	// Re-parse and marshal with sorted keys to get deterministic output.
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("canonical JSON reparse: %w", err)
	}
	// Now marshal with sorted keys (json.Marshal sorts map keys).
	sorted, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return sorted, nil
}

// sortedMapCopy returns a copy with sorted key insertion (maps in Go marshal
// with sorted keys by default via json.Marshal, but we ensure determinism).
func sortedMapCopy(m map[string]string) map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

// sha256Hex returns the lowercase hex-encoded SHA-256 digest of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
