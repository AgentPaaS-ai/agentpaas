package runtime

import (
	"fmt"
	"sort"
)

// Negotiate computes the effective runtime profile for a declared profile
// against a supported profile.
//
//   - Every REQUIRED feature in declared must be present in supported (set
//     inclusion over supported's required+optional features). If any is
//     missing, Negotiate returns an error (fail-closed). Marketing names or
//     silent best-effort downgrade are forbidden.
//   - Optional features in declared that are not supported are silently
//     dropped.
//   - The returned profile has declared's version and required features, and
//     the intersection of declared's optional features with supported's
//     required+optional features.
func Negotiate(declared, supported RuntimeProfile) (RuntimeProfile, error) {
	supportedSet := supported.supportedFeatureSet()

	// Fail-closed on any missing required feature.
	var missing []string
	for _, f := range declared.RequiredFeatures {
		if _, ok := supportedSet[f]; !ok {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return RuntimeProfile{}, fmt.Errorf(
			"runtime profile negotiation: unsupported required features: %v",
			missing,
		)
	}

	// Keep only supported optional features; silently drop the rest.
	negotiatedOptional := make([]string, 0, len(declared.OptionalFeatures))
	for _, f := range declared.OptionalFeatures {
		if _, ok := supportedSet[f]; ok {
			negotiatedOptional = append(negotiatedOptional, f)
		}
	}

	return RuntimeProfile{
		Version:          declared.Version,
		RequiredFeatures: append([]string(nil), declared.RequiredFeatures...),
		OptionalFeatures: negotiatedOptional,
	}, nil
}
