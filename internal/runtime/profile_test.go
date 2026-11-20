package runtime

import (
	"strings"
	"testing"
)

func TestRuntimeProfileValidate_RejectsEmptyVersion(t *testing.T) {
	p := RuntimeProfile{
		Version:           "",
		RequiredFeatures:  []string{FeatureStructuredIO},
		OptionalFeatures:  []string{FeatureModelStreaming},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate must reject empty version")
	}
}

func TestRuntimeProfileValidate_RejectsEmptyRequiredFeatures(t *testing.T) {
	p := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: nil,
		OptionalFeatures: []string{FeatureModelStreaming},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate must reject empty required features")
	}
}

func TestRuntimeProfileValidate_RejectsOverlappingRequiredAndOptional(t *testing.T) {
	p := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureModelStreaming},
		OptionalFeatures: []string{FeatureModelStreaming},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate must reject a feature that is both required and optional")
	}
}

func TestRuntimeProfileValidate_AcceptsBaseline(t *testing.T) {
	p := BaselineProfileV03()
	if err := p.Validate(); err != nil {
		t.Fatalf("baseline v0.3 profile must validate: %v", err)
	}
}

func TestIsCompatible_TrueWhenAllRequiredPresent(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureMultiRoleMessages},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureMultiRoleMessages, FeatureProgress},
	}
	if !IsCompatible(declared, supported) {
		t.Fatal("all required features are present in supported; must be compatible")
	}
}

func TestIsCompatible_TrueWhenSupportedHasExtras(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
		OptionalFeatures: []string{FeatureModelStreaming, FeatureReasoningControls},
	}
	if !IsCompatible(declared, supported) {
		t.Fatal("supported has every required feature plus extras; must be compatible")
	}
}

func TestIsCompatible_FalseWhenRequiredFeatureMissing(t *testing.T) {
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO, FeatureModelStreaming},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
	}
	if IsCompatible(declared, supported) {
		t.Fatal("a required feature is missing from supported; must be incompatible")
	}
}

func TestIsCompatible_NoBestEffortDowngradeForModelStreaming(t *testing.T) {
	// Spec: marketing names or silent best-effort downgrade are forbidden.
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureModelStreaming},
	}
	supported := BaselineProfileV03() // baseline does NOT include model_streaming
	if IsCompatible(declared, supported) {
		t.Fatal("request for model_streaming against a profile that doesn't list it must return false")
	}
}

func TestBaselineProfileV03_ContainsAllBaselineFeatures(t *testing.T) {
	p := BaselineProfileV03()
	want := []string{
		FeatureStructuredIO,
		FeatureMultiRoleMessages,
		FeatureBufferedModelCalls,
		FeatureProgress,
		FeatureGovernedHTTP,
		FeatureOrderedRunEvents,
	}
	for _, w := range want {
		found := false
		for _, got := range p.RequiredFeatures {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("baseline v0.3 must require %q; got %v", w, p.RequiredFeatures)
		}
	}
	if p.Version != "0.3" {
		t.Fatalf("baseline version = %q; want 0.3", p.Version)
	}
}

func TestIsCompatible_RequiredInOptionalCountsAsSupported(t *testing.T) {
	// A feature listed as optional in supported still satisfies a required
	// feature in declared — set inclusion is over the union of supported's
	// required+optional features.
	declared := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureModelStreaming},
	}
	supported := RuntimeProfile{
		Version:          "0.3",
		RequiredFeatures: []string{FeatureStructuredIO},
		OptionalFeatures: []string{FeatureModelStreaming},
	}
	if !IsCompatible(declared, supported) {
		t.Fatal("model_streaming is in supported's optional set; must satisfy the required feature")
	}
}

func TestRuntimeProfileValidate_ErrorMentionsField(t *testing.T) {
	p := RuntimeProfile{Version: "", RequiredFeatures: []string{FeatureStructuredIO}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for empty version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("error must mention version; got %v", err)
	}
}
