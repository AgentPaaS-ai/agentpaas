package runtime

import (
	"strings"
	"testing"
)

func TestNegotiate_FailClosedOnMissingRequiredFeature(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureModelStreaming},
		OptionalFeatures: []string{FeatureReasoningControls},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
		OptionalFeatures: []string{FeatureReasoningControls},
	}
	_, err := Negotiate(declared, supported)
	if err == nil {
		t.Fatal("Negotiate must fail-closed when a required feature is missing from supported")
	}
	if !strings.Contains(err.Error(), FeatureModelStreaming) {
		t.Fatalf("error must mention the missing required feature %q; got %v", FeatureModelStreaming, err)
	}
}

func TestNegotiate_OptionalFeaturesSilentlyDroppedWhenUnsupported(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
		OptionalFeatures: []string{FeatureModelStreaming, FeatureReasoningControls},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureMultiRoleMessages},
		OptionalFeatures: []string{FeatureReasoningControls}, // model_streaming NOT supported
	}
	result, err := Negotiate(declared, supported)
	if err != nil {
		t.Fatalf("Negotiate must succeed when all required features are supported: %v", err)
	}
	// model_streaming was declared optional but unsupported → must be dropped.
	for _, f := range result.OptionalFeatures {
		if f == FeatureModelStreaming {
			t.Fatal("unsupported optional feature model_streaming must be silently dropped")
		}
	}
	// reasoning_controls was declared optional and supported → must be kept.
	foundReasoning := false
	for _, f := range result.OptionalFeatures {
		if f == FeatureReasoningControls {
			foundReasoning = true
		}
	}
	if !foundReasoning {
		t.Fatal("supported optional feature reasoning_controls must be kept in the negotiated profile")
	}
}

func TestNegotiate_AllRequiredPresentAllOptionalSupported(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureMultiRoleMessages},
		OptionalFeatures: []string{FeatureModelStreaming},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureMultiRoleMessages, FeatureProgress},
		OptionalFeatures: []string{FeatureModelStreaming, FeatureReasoningControls},
	}
	result, err := Negotiate(declared, supported)
	if err != nil {
		t.Fatalf("Negotiate must succeed: %v", err)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("negotiated profile must be valid: %v", err)
	}
	// Required features of the result must equal declared's required features
	// (all were supported).
	if !featureSetsEqual(result.RequiredFeatures, declared.RequiredFeatures) {
		t.Fatalf("negotiated required = %v; want %v", result.RequiredFeatures, declared.RequiredFeatures)
	}
}

func TestNegotiate_ResultVersionIsDeclaredVersion(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
	}
	supported := BaselineProfileV03()
	result, err := Negotiate(declared, supported)
	if err != nil {
		t.Fatalf("Negotiate must succeed: %v", err)
	}
	if result.Version != declared.Version {
		t.Fatalf("negotiated version = %q; want %q", result.Version, declared.Version)
	}
}

func featureSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, f := range a {
		seen[f] = true
	}
	for _, f := range b {
		if !seen[f] {
			return false
		}
	}
	return true
}
