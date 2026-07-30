package pack

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveBaseImagePlatform_NoPlatform(t *testing.T) {
	baseImage := "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
	result, err := ResolveBaseImagePlatform(context.Background(), baseImage, "")
	if err != nil {
		t.Fatalf("ResolveBaseImagePlatform() error = %v", err)
	}
	if result != baseImage {
		t.Fatalf("ResolveBaseImagePlatform() = %q, want unchanged %q", result, baseImage)
	}
}

func TestResolveBaseImagePlatform_EmptyBaseImage(t *testing.T) {
	result, err := ResolveBaseImagePlatform(context.Background(), "", "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveBaseImagePlatform() error = %v", err)
	}
	if result != "" {
		t.Fatalf("ResolveBaseImagePlatform() = %q, want empty", result)
	}
}

func TestResolveBaseImagePlatform_KnownMap_LinuxAmd64(t *testing.T) {
	baseImage := "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
	result, err := ResolveBaseImagePlatform(context.Background(), baseImage, "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveBaseImagePlatform() error = %v", err)
	}
	expected := "gcr.io/distroless/python3-debian12@sha256:87406cabca8cc6846b5ea08bd9b25960b4e29b845f76c1af0e3f4a0affd1d7b4"
	if result != expected {
		t.Fatalf("ResolveBaseImagePlatform() = %q, want %q", result, expected)
	}
}

func TestResolveBaseImagePlatform_KnownMap_LinuxArm64(t *testing.T) {
	baseImage := "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
	result, err := ResolveBaseImagePlatform(context.Background(), baseImage, "linux/arm64")
	if err != nil {
		t.Fatalf("ResolveBaseImagePlatform() error = %v", err)
	}
	expected := "gcr.io/distroless/python3-debian12@sha256:630eb311a5eafad7a7b1cd28e9db312e87a975cc63b6bfca884b4572ace0ab16"
	if result != expected {
		t.Fatalf("ResolveBaseImagePlatform() = %q, want %q", result, expected)
	}
}

func TestResolveBaseImagePlatform_UnknownDigest_ReturnsOriginal(t *testing.T) {
	// An unknown digest that's not in our map and buildx won't be available in CI.
	baseImage := "gcr.io/unknown/image@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	result, err := ResolveBaseImagePlatform(context.Background(), baseImage, "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveBaseImagePlatform() error = %v", err)
	}
	if result != baseImage {
		t.Fatalf("ResolveBaseImagePlatform() = %q, want unchanged %q", result, baseImage)
	}
}

func TestResolveBaseImagePlatform_NoDigest_TagOnly(t *testing.T) {
	// A base image with only a tag (no @sha256:) should be returned unchanged.
	baseImage := "python:3.11-slim"
	result, err := ResolveBaseImagePlatform(context.Background(), baseImage, "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveBaseImagePlatform() error = %v", err)
	}
	if result != baseImage {
		t.Fatalf("ResolveBaseImagePlatform() = %q, want unchanged %q", result, baseImage)
	}
}

func TestExtractDigest(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"digest present", "gcr.io/distroless/python3-debian12@sha256:abcdef123456", "sha256:abcdef123456"},
		{"no digest", "python:3.11-slim", ""},
		{"empty", "", ""},
		{"multiple at signs", "registry.io/foo@sha256:abc@sha256:def", "sha256:def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDigest(tt.ref)
			if got != tt.want {
				t.Fatalf("extractDigest(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestReplaceDigest(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		newDigest string
		want      string
	}{
		{
			"replace existing digest",
			"gcr.io/distroless/python3-debian12@sha256:olddigest123",
			"sha256:newdigest456",
			"gcr.io/distroless/python3-debian12@sha256:newdigest456",
		},
		{
			"no existing digest",
			"python:3.11-slim",
			"sha256:newdigest456",
			"python:3.11-slim",
		},
		{
			"empty ref",
			"",
			"sha256:newdigest456",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceDigest(tt.ref, tt.newDigest)
			if got != tt.want {
				t.Fatalf("replaceDigest(%q, %q) = %q, want %q", tt.ref, tt.newDigest, got, tt.want)
			}
		})
	}
}

func TestResolveFromMap(t *testing.T) {
	// Known digest with known platforms.
	digest := "sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
	if got, ok := resolveFromMap(digest, "linux/amd64"); !ok {
		t.Fatal("resolveFromMap(known digest, linux/amd64): not found")
	} else if !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("resolveFromMap returned invalid digest: %q", got)
	}

	// Unknown platform for known digest.
	if _, ok := resolveFromMap(digest, "windows/amd64"); ok {
		t.Fatal("resolveFromMap(known digest, windows/amd64): unexpectedly found")
	}

	// Unknown digest.
	if _, ok := resolveFromMap("sha256:deadbeef", "linux/amd64"); ok {
		t.Fatal("resolveFromMap(unknown digest): unexpectedly found")
	}
}

func TestBuildxManifestListParsing(t *testing.T) {
	// Test JSON unmarshaling of the buildx manifest list format.
	const buildxOutput = `{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:abc123","platform":{"architecture":"amd64","os":"linux"}},{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:def456","platform":{"architecture":"arm64","os":"linux","variant":"v8"}}]}`

	var ml buildxManifestList
	if err := json.Unmarshal([]byte(buildxOutput), &ml); err != nil {
		t.Fatalf("json.Unmarshal(buildx output) error = %v", err)
	}
	if len(ml.Manifests) != 2 {
		t.Fatalf("got %d manifests, want 2", len(ml.Manifests))
	}
	if ml.Manifests[0].Digest != "sha256:abc123" {
		t.Fatalf("manifest[0].Digest = %q, want sha256:abc123", ml.Manifests[0].Digest)
	}
	if ml.Manifests[0].Platform.OS != "linux" || ml.Manifests[0].Platform.Architecture != "amd64" {
		t.Fatalf("manifest[0].Platform = %+v", ml.Manifests[0].Platform)
	}
	if ml.Manifests[1].Platform.Variant != "v8" {
		t.Fatalf("manifest[1].Platform.Variant = %q, want v8", ml.Manifests[1].Platform.Variant)
	}
}
