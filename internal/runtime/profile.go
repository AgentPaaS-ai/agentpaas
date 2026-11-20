package runtime

import (
	"errors"
	"fmt"
	"strings"
)

// RuntimeProfileVersion is the current Agent Runtime Profile schema version.
const RuntimeProfileVersion = "0.3"

// Baseline v0.3 required features. Every conforming runtime must support all
// of these.
const (
	// FeatureStructuredIO is bounded structured input and output.
	FeatureStructuredIO = "structured_io"
	// FeatureMultiRoleMessages enables system, developer, user, assistant,
	// and tool roles in the normalized model-call envelope.
	FeatureMultiRoleMessages = "multi_role_messages"
	// FeatureBufferedModelCalls guarantees the legacy agent.llm(prompt)
	// buffered compatibility wrapper.
	FeatureBufferedModelCalls = "buffered_model_calls"
	// FeatureProgress covers semantic progress, checkpoints, artifacts,
	// cancellation, and deadlines.
	FeatureProgress = "progress"
	// FeatureGovernedHTTP constrains outbound HTTP and MCP calls under policy.
	FeatureGovernedHTTP = "governed_http"
	// FeatureOrderedRunEvents provides ordered durable run events.
	FeatureOrderedRunEvents = "ordered_run_events"
)

// Negotiated optional features. A package may declare any subset of these as
// required or optional; admission uses set inclusion and never best-effort
// downgrade.
const (
	// FeatureModelStreaming streams response output deltas.
	FeatureModelStreaming = "model_streaming"
	// FeatureToolCallStreaming streams tool-call argument deltas.
	FeatureToolCallStreaming = "tool_call_streaming"
	// FeatureStructuredOutput requests a structured JSON-schema output.
	FeatureStructuredOutput = "structured_output"
	// FeatureReasoningControls exposes reasoning effort, token accounting,
	// and provider-approved reasoning summaries (never raw chain-of-thought).
	FeatureReasoningControls = "reasoning_controls"
	// FeatureInteractiveInbox supports durable external waits via an inbox.
	FeatureInteractiveInbox = "interactive_inbox"
	// FeatureMultimodalArtifactParts allows multimodal artifact parts.
	FeatureMultimodalArtifactParts = "multimodal_artifact_parts"
	// FeatureConcurrentCalls bounds concurrent model/tool calls.
	FeatureConcurrentCalls = "concurrent_calls"
	// FeatureProviderExtensions permits namespaced, policy-reviewed provider
	// extension metadata.
	FeatureProviderExtensions = "provider_extensions"
)

// RuntimeProfile declares the version and feature set a package, deployment,
// model, runtime, or component supports or requires.
type RuntimeProfile struct {
	// Version is the runtime profile schema version (e.g. "0.3").
	Version string
	// RequiredFeatures are features the declarer cannot run without. Admission
	// fails closed if any are absent in the supported profile.
	RequiredFeatures []string
	// OptionalFeatures are features the declarer can use if supported, but
	// does not require. They must not overlap with RequiredFeatures.
	OptionalFeatures []string
}

// BaselineProfileV03 returns the baseline v0.3 runtime profile containing
// every required baseline feature and no optional features.
func BaselineProfileV03() RuntimeProfile {
	return RuntimeProfile{
		Version: RuntimeProfileVersion,
		RequiredFeatures: []string{
			FeatureStructuredIO,
			FeatureMultiRoleMessages,
			FeatureBufferedModelCalls,
			FeatureProgress,
			FeatureGovernedHTTP,
			FeatureOrderedRunEvents,
		},
	}
}

// Validate checks the profile is well-formed:
//   - version is non-empty;
//   - required features is non-empty;
//   - optional features do not overlap with required features.
func (p RuntimeProfile) Validate() error {
	if strings.TrimSpace(p.Version) == "" {
		return errors.New("runtime profile: version must be non-empty")
	}
	if len(p.RequiredFeatures) == 0 {
		return errors.New("runtime profile: required features must be non-empty")
	}
	requiredSet := make(map[string]struct{}, len(p.RequiredFeatures))
	for _, f := range p.RequiredFeatures {
		if strings.TrimSpace(f) == "" {
			return errors.New("runtime profile: required feature must be non-empty")
		}
		requiredSet[f] = struct{}{}
	}
	for _, f := range p.OptionalFeatures {
		if strings.TrimSpace(f) == "" {
			return errors.New("runtime profile: optional feature must be non-empty")
		}
		if _, ok := requiredSet[f]; ok {
			return fmt.Errorf("runtime profile: feature %q is both required and optional", f)
		}
	}
	return nil
}

// supportedFeatureSet returns the set of all features a profile supports
// (required union optional).
func (p RuntimeProfile) supportedFeatureSet() map[string]struct{} {
	out := make(map[string]struct{}, len(p.RequiredFeatures)+len(p.OptionalFeatures))
	for _, f := range p.RequiredFeatures {
		out[f] = struct{}{}
	}
	for _, f := range p.OptionalFeatures {
		out[f] = struct{}{}
	}
	return out
}

// IsCompatible reports whether every required feature in declared is present
// in supported (set inclusion over supported's required+optional features).
// Marketing names or silent best-effort downgrade are forbidden: if any
// required feature is absent, this returns false.
func IsCompatible(declared, supported RuntimeProfile) bool {
	supportedSet := supported.supportedFeatureSet()
	for _, f := range declared.RequiredFeatures {
		if _, ok := supportedSet[f]; !ok {
			return false
		}
	}
	return true
}
