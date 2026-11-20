package pipeline

// ---------------------------------------------------------------------------
// Stage authority
// ---------------------------------------------------------------------------

// StageAuthority defines the authority scope for a single pipeline stage.
// This is derived from the policy and package configuration and represents
// what the stage container is allowed to do at runtime.
type StageAuthority struct {
	// AllowHosts is the set of host patterns this stage may contact.
	AllowHosts []string

	// AllowMCP is the set of MCP server names this stage may invoke.
	AllowMCP []string

	// MaxActiveMs is the maximum active duration in milliseconds.
	MaxActiveMs int64

	// MaxLLMSpend is a decimal string representing the maximum LLM spend.
	// Empty means no cap.
	MaxLLMSpend string

	// NetworkEgress indicates whether this stage may access external networks.
	NetworkEgress bool
}

// ---------------------------------------------------------------------------
// IntersectStageAuthority
// ---------------------------------------------------------------------------

// IntersectStageAuthority returns the intersection of workflow-level and
// stage-package authority. This ensures no stage receives more authority than
// the workflow allows, and each stage's authority is independently scoped.
//
// Rules:
//   - AllowHosts: set intersection
//   - AllowMCP: set intersection
//   - MaxActiveMs: min of positive values (0 = no limit, skipped in min)
//   - MaxLLMSpend: workflow wins if set; stage wins if workflow empty
//   - NetworkEgress: logical AND (both must allow)
//
// Never copies prior stage authority wholesale — each stage is independently
// intersected with the workflow authority.
func IntersectStageAuthority(workflow, stage StageAuthority) StageAuthority {
	result := StageAuthority{
		NetworkEgress: workflow.NetworkEgress && stage.NetworkEgress,
	}

	// Hosts: set intersection.
	result.AllowHosts = intersectStrings(workflow.AllowHosts, stage.AllowHosts)

	// MCP: set intersection.
	result.AllowMCP = intersectStrings(workflow.AllowMCP, stage.AllowMCP)

	// MaxActiveMs: min of positive values. If both 0, result is 0.
	result.MaxActiveMs = minPositive(workflow.MaxActiveMs, stage.MaxActiveMs)

	// MaxLLMSpend: workflow value takes precedence if set.
	if workflow.MaxLLMSpend != "" {
		result.MaxLLMSpend = workflow.MaxLLMSpend
	} else {
		result.MaxLLMSpend = stage.MaxLLMSpend
	}

	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// intersectStrings returns the set intersection of a and b, preserving
// order from a for determinism.
func intersectStrings(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	// Build a lookup from b.
	bm := make(map[string]struct{}, len(b))
	for _, s := range b {
		bm[s] = struct{}{}
	}

	var out []string
	for _, s := range a {
		if _, ok := bm[s]; ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// minPositive returns the minimum of a and b, treating 0 as "no limit"
// (skipped). If one is 0 and the other is positive, returns the positive.
// If both are 0, returns 0.
func minPositive(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
