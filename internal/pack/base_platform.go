package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// knownMultiArchManifests maps known multi-arch index digests to their
// platform-specific manifest digests. Used as a fallback when dynamic
// resolution via buildx is unavailable.
var knownMultiArchManifests = map[string]map[string]string{
	// gcr.io/distroless/python3-debian12 (multi-arch index)
	"sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f": {
		"linux/amd64": "sha256:87406cabca8cc6846b5ea08bd9b25960b4e29b845f76c1af0e3f4a0affd1d7b4",
		"linux/arm64": "sha256:630eb311a5eafad7a7b1cd28e9db312e87a975cc63b6bfca884b4572ace0ab16",
	},
}

// ResolveBaseImagePlatform resolves a base image reference to a single-platform
// digest when the ref refers to a multi-arch index. If the ref is already
// platform-specific or platform is empty, it is returned unchanged.
//
// Resolution order:
//  1. Dynamic: docker buildx imagetools inspect (parses the manifest list to
//     find the matching platform entry).
//  2. Static fallback: knownMultiArchManifests map for known distroless digests.
//  3. Best-effort: return the original ref when neither method can resolve.
func ResolveBaseImagePlatform(ctx context.Context, baseImage, platform string) (string, error) {
	if platform == "" || baseImage == "" {
		return baseImage, nil
	}

	// If the base image doesn't have a pinned digest, we can't resolve it.
	if !strings.Contains(baseImage, "@sha256:") {
		return baseImage, nil
	}

	// 1. Dynamic resolution via buildx.
	if resolved, resolvedOK, err := resolveViaBuildx(ctx, baseImage, platform); err != nil {
		return "", fmt.Errorf("resolve base image platform: %w", err)
	} else if resolvedOK {
		return resolved, nil
	}

	// 2. Static fallback map.
	digest := extractDigest(baseImage)
	if resolved, ok := resolveFromMap(digest, platform); ok {
		return replaceDigest(baseImage, resolved), nil
	}

	// 3. Best-effort: return original.
	return baseImage, nil
}

// extractDigest returns the @sha256:... portion from an image ref, or empty.
func extractDigest(ref string) string {
	if idx := strings.LastIndex(ref, "@sha256:"); idx >= 0 {
		return strings.TrimPrefix(ref[idx:], "@")
	}
	return ""
}

// replaceDigest replaces the digest portion of an image ref.
// e.g. "gcr.io/distroless/python3-debian12@sha256:abc" + "sha256:def"
// → "gcr.io/distroless/python3-debian12@sha256:def"
func replaceDigest(ref, newDigest string) string {
	idx := strings.LastIndex(ref, "@sha256:")
	if idx < 0 {
		return ref
	}
	return ref[:idx] + "@" + newDigest
}

// resolveFromMap checks the known multi-arch map for a matching platform digest.
func resolveFromMap(digest, platform string) (string, bool) {
	platformMap, ok := knownMultiArchManifests[digest]
	if !ok {
		return "", false
	}
	resolved, ok := platformMap[platform]
	return resolved, ok
}

// buildxManifestList is the JSON structure returned by
// `docker buildx imagetools inspect <ref> --format '{{json .Manifest}}'`.
type buildxManifestList struct {
	MediaType string               `json:"mediaType"`
	Manifests []buildxManifestDesc `json:"manifests"`
}

type buildxManifestDesc struct {
	MediaType string          `json:"mediaType"`
	Digest    string          `json:"digest"`
	Platform  buildxPlatform  `json:"platform"`
}

type buildxPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

// resolveViaBuildx runs buildx imagetools inspect to dynamically resolve a
// multi-arch index to a single-platform manifest digest. Returns (resolved, true, nil)
// on success, ("", false, nil) when buildx is unavailable or the image is not
// a manifest list, and ("", false, error) on genuine failures.
func resolveViaBuildx(ctx context.Context, baseImage, platform string) (string, bool, error) {
	// Parse the requested platform into os/arch.
	parts := strings.SplitN(platform, "/", 3)
	if len(parts) < 2 {
		return "", false, nil
	}
	targetOS, targetArch := parts[0], parts[1]
	targetVariant := ""
	if len(parts) > 2 {
		targetVariant = parts[2]
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "docker", "buildx", "imagetools", "inspect",
		baseImage, "--format", "{{json .Manifest}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// buildx unavailable or command failed; return false so caller falls back.
		return "", false, nil
	}

	var ml buildxManifestList
	if err := json.Unmarshal(output, &ml); err != nil {
		return "", false, nil
	}

	// If there are no manifests, it's not a multi-arch index.
	if len(ml.Manifests) == 0 {
		return "", false, nil
	}

	// Search for a matching platform entry.
	for _, m := range ml.Manifests {
		if m.Platform.OS == targetOS && m.Platform.Architecture == targetArch {
			if targetVariant == "" || m.Platform.Variant == targetVariant {
				return replaceDigest(baseImage, m.Digest), true, nil
			}
		}
	}

	// No matching platform found in the manifest list.
	return "", false, nil
}
