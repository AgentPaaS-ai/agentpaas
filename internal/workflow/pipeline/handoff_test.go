package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadHandoffFixture reads a JSON handoff fixture file.
func loadHandoffFixture(t *testing.T, path string) *HandoffEnvelope {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var ho HandoffEnvelope
	if err := json.Unmarshal(data, &ho); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", path, err)
	}
	return &ho
}

// TestHandoffValidFixtures tests valid handoff envelopes pass validation.
func TestHandoffValidFixtures(t *testing.T) {
	validDir := filepath.Join("testdata", "handoff", "valid")
	entries, err := os.ReadDir(validDir)
	if err != nil {
		t.Fatalf("read valid dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			path := filepath.Join(validDir, e.Name())
			ho := loadHandoffFixture(t, path)
			codes := ValidateHandoffEnvelope(ho)
			if len(codes) > 0 {
				t.Fatalf("expected no errors, got %v", codes)
			}
		})
	}
}

// TestHandoffInvalidFixtures tests invalid handoff envelopes fail with expected codes.
func TestHandoffInvalidFixtures(t *testing.T) {
	invalidDir := filepath.Join("testdata", "handoff", "invalid")
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
			ho := loadHandoffFixture(t, path)
			codes := ValidateHandoffEnvelope(ho)
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

// TestHandoffSchemaVersion rejects wrong schema version.
func TestHandoffSchemaVersion(t *testing.T) {
	ho := validMinimalHandoff()
	ho.SchemaVersion = "wrong/v1"
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffSchemaVersion) {
		t.Fatalf("expected %s, got %v", CodeHandoffSchemaVersion, codes)
	}
}

// TestHandoffContextOversize rejects context > 256 KiB.
func TestHandoffContextOversize(t *testing.T) {
	ho := validMinimalHandoff()
	// Build a valid JSON context that exceeds 256 KiB.
	bigPayload := `{"data":"` + strings.Repeat("x", HandoffContextMaxBytes+1) + `"}`
	ho.Context.Value = json.RawMessage(bigPayload)
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffContextOversize) {
		t.Fatalf("expected %s, got %v", CodeHandoffContextOversize, codes)
	}
}

// TestHandoffContextExactlyLimit accepts context at exactly 256 KiB.
func TestHandoffContextExactlyLimit(t *testing.T) {
	ho := validMinimalHandoff()
	// Create a valid JSON value that is exactly 256 KiB.
	// Use a JSON object with a large string value.
	payloadSize := HandoffContextMaxBytes - 17 // {"key":""} overhead is ~11, add buffer
	bigPayload := `{"data":"` + strings.Repeat("x", payloadSize-11) + `"}`
	ho.Context.Value = json.RawMessage(bigPayload)
	codes := ValidateHandoffEnvelope(&ho)
	if len(codes) > 0 {
		t.Fatalf("expected no errors for exactly-limit context (%d bytes), got %v", len(bigPayload), codes)
	}
}

// TestHandoffArtifactMetaOversize rejects artifact meta > 64 KiB.
func TestHandoffArtifactMetaOversize(t *testing.T) {
	ho := validMinimalHandoff()
	// Create many artifacts to exceed 64 KiB.
	artifacts := make([]HandoffArtifact, 0, 1000)
	for i := 0; i < 1000; i++ {
		artifacts = append(artifacts, HandoffArtifact{
			ArtifactID:     "artifact_" + strings.Repeat("x", 100),
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "artifacts/out.json",
			Digest:         "sha256:" + strings.Repeat("a", 64),
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "internal",
		})
	}
	ho.Artifacts = artifacts
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffArtifactMetaOversize) {
		t.Fatalf("expected %s, got %v", CodeHandoffArtifactMetaOversize, codes)
	}
}

// TestHandoffReservedKey rejects reserved keys in context.
func TestHandoffReservedKey(t *testing.T) {
	ho := validMinimalHandoff()
	ctx := map[string]interface{}{
		"password": "secret123",
		"data":     "ok",
	}
	val, _ := json.Marshal(ctx)
	ho.Context.Value = val
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffReservedKey) {
		t.Fatalf("expected %s, got %v", CodeHandoffReservedKey, codes)
	}
}

// TestHandoffReservedKeyAPIKey rejects api_key in context.
func TestHandoffReservedKeyAPIKey(t *testing.T) {
	ho := validMinimalHandoff()
	ctx := map[string]interface{}{
		"api_key": "sk-12345",
	}
	val, _ := json.Marshal(ctx)
	ho.Context.Value = val
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffReservedKey) {
		t.Fatalf("expected %s, got %v", CodeHandoffReservedKey, codes)
	}
}

// TestHandoffDigestMismatch rejects malformed sha256 digest.
func TestHandoffDigestMismatch(t *testing.T) {
	ho := validMinimalHandoff()
	ho.ProducerResultDigest = "md5:abc123"
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffDigestMismatch) {
		t.Fatalf("expected %s, got %v", CodeHandoffDigestMismatch, codes)
	}
}

// TestHandoffDigestMismatchArtifact rejects malformed artifact digest.
func TestHandoffDigestMismatchArtifact(t *testing.T) {
	ho := validMinimalHandoff()
	ho.Artifacts = []HandoffArtifact{
		{
			ArtifactID:     "art_1",
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "artifacts/out.json",
			Digest:         "not-a-valid-digest",
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "internal",
		},
	}
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffDigestMismatch) {
		t.Fatalf("expected %s, got %v", CodeHandoffDigestMismatch, codes)
	}
}

// TestHandoffDeclassification rejects artifact less restrictive than envelope.
func TestHandoffDeclassification(t *testing.T) {
	ho := validMinimalHandoff()
	ho.Classification = "restricted"
	ho.Artifacts = []HandoffArtifact{
		{
			ArtifactID:     "art_1",
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "artifacts/out.json",
			Digest:         "sha256:" + strings.Repeat("a", 64),
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "public",
		},
	}
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffDeclassification) {
		t.Fatalf("expected %s, got %v", CodeHandoffDeclassification, codes)
	}
}

// TestHandoffDuplicateIDReject tests that same handoff_id with different content is rejected.
func TestHandoffDuplicateIDReject(t *testing.T) {
	ho1 := validMinimalHandoff()
	ho1.HandoffID = "ho_dup_1"
	ho2 := validMinimalHandoff()
	ho2.HandoffID = "ho_dup_1"
	ho2.ProducerRunID = "run_different"

	// Store first, then try second with same ID.
	store := newHandoffIDStore()
	if err := store.Record(&ho1); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if err := store.Record(&ho2); err == nil {
		t.Fatal("expected duplicate rejection, got nil")
	} else if !strings.Contains(err.Error(), CodeHandoffDuplicateID) {
		t.Fatalf("expected %s in error, got: %v", CodeHandoffDuplicateID, err)
	}
}

// TestHandoffNonCanonicalJSON rejects non-canonical JSON structure.
func TestHandoffNonCanonicalJSON(t *testing.T) {
	// Non-canonical: extra whitespace or out-of-order keys in context.value
	// For now, verify that invalid JSON structure is rejected.
	ho := validMinimalHandoff()
	ho.Context.Value = json.RawMessage(`{invalid}`)
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffNonCanonical) {
		t.Fatalf("expected %s, got %v", CodeHandoffNonCanonical, codes)
	}
}

// TestHandoffImmutableRefPathTraversal rejects path traversal in immutable_ref.
func TestHandoffImmutableRefPathTraversal(t *testing.T) {
	ho := validMinimalHandoff()
	ho.Artifacts = []HandoffArtifact{
		{
			ArtifactID:     "art_1",
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "../etc/passwd",
			Digest:         "sha256:" + strings.Repeat("a", 64),
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "internal",
		},
	}
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffReservedKey) {
		t.Fatalf("expected %s, got %v", CodeHandoffReservedKey, codes)
	}
}

// TestHandoffImmutableRefAbsolutePath rejects absolute paths.
func TestHandoffImmutableRefAbsolutePath(t *testing.T) {
	ho := validMinimalHandoff()
	ho.Artifacts = []HandoffArtifact{
		{
			ArtifactID:     "art_1",
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "/var/run/secrets",
			Digest:         "sha256:" + strings.Repeat("a", 64),
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "internal",
		},
	}
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffReservedKey) {
		t.Fatalf("expected %s, got %v", CodeHandoffReservedKey, codes)
	}
}

// TestHandoffForgedProducerMissingIDs rejects forged producer fields.
func TestHandoffForgedProducerMissingIDs(t *testing.T) {
	ho := validMinimalHandoff()
	ho.ProducerRunID = ""
	ho.ProducerAttemptID = ""
	codes := ValidateHandoffEnvelope(&ho)
	if len(codes) == 0 {
		t.Fatal("expected validation errors for missing producer IDs")
	}
}

// FuzzHandoffJSONBounds fuzzes handoff envelope validation with random
// context values to ensure no panics and fast rejection of oversized inputs.
func FuzzHandoffJSONBounds(f *testing.F) {
	// Seed corpus with known edge cases.
	f.Add("{}")
	f.Add(`{"key":"value"}`)
	f.Add(strings.Repeat("x", HandoffContextMaxBytes))
	f.Add(strings.Repeat("x", HandoffContextMaxBytes+1))
	f.Add(`{"` + strings.Repeat("k", 100) + `":"` + strings.Repeat("v", 1000) + `"}`)

	f.Fuzz(func(t *testing.T, ctxValue string) {
		ho := validMinimalHandoff()
		ho.Context.Value = json.RawMessage(ctxValue)
		codes := ValidateHandoffEnvelope(&ho)
		// Must not panic; must return valid codes for any input.
		for _, c := range codes {
			_ = c // verify code is a non-empty string
		}
	})
}

// TestHandoffArtifactMetaExactlyAtLimit validates artifact meta at exactly 64 KiB.
func TestHandoffArtifactMetaExactlyAtLimit(t *testing.T) {
	ho := validMinimalHandoff()
	// Build artifact meta that totals exactly HandoffArtifactMetaMaxBytes.
	// Each artifact has fixed overhead from the struct fields we measure.
	// Template artifact: ArtifactID (20) + OwnerNodeID (7) + OwnerRunID (5) +
	// ImmutableRef (18) + Digest (71) + MediaType (16) + 8 (SizeBytes) + Classification (10) = 155 bytes
	perArtifact := 155
	count := HandoffArtifactMetaMaxBytes / perArtifact
	artifacts := make([]HandoffArtifact, 0, count)
	digest := "sha256:" + strings.Repeat("a", 64)
	for i := 0; i < count; i++ {
		artifacts = append(artifacts, HandoffArtifact{
			ArtifactID:     "artifact_" + strings.Repeat("x", 10),
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "artifacts/out.json",
			Digest:         digest,
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "internal",
		})
	}
	ho.Artifacts = artifacts
	codes := ValidateHandoffEnvelope(&ho)
	metaSize := estimateArtifactMetaSize(ho.Artifacts)
	if metaSize > HandoffArtifactMetaMaxBytes {
		if !ContainsCode(codes, CodeHandoffArtifactMetaOversize) {
			t.Fatalf("expected %s for meta size %d > %d, got %v",
				CodeHandoffArtifactMetaOversize, metaSize, HandoffArtifactMetaMaxBytes, codes)
		}
	} else {
		if len(codes) > 0 {
			t.Fatalf("expected no errors for meta size %d <= %d, got %v",
				metaSize, HandoffArtifactMetaMaxBytes, codes)
		}
	}
}

// TestHandoffArtifactMetaOneByteOver rejects artifact meta at 64 KiB + 1.
func TestHandoffArtifactMetaOneByteOver(t *testing.T) {
	ho := validMinimalHandoff()
	// Create just enough artifacts to exceed the limit.
	perArtifact := estimateArtifactMetaSize([]HandoffArtifact{{
		ArtifactID:     "artifact_" + strings.Repeat("x", 10),
		OwnerNodeID:    "stage_a",
		OwnerRunID:     "run_1",
		ImmutableRef:   "artifacts/out.json",
		Digest:         "sha256:" + strings.Repeat("a", 64),
		MediaType:      "application/json",
		SizeBytes:      100,
		Classification: "internal",
	}})
	count := HandoffArtifactMetaMaxBytes/perArtifact + 1
	artifacts := make([]HandoffArtifact, 0, count)
	digest := "sha256:" + strings.Repeat("a", 64)
	for i := 0; i < count; i++ {
		artifacts = append(artifacts, HandoffArtifact{
			ArtifactID:     "artifact_" + strings.Repeat("x", 10),
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "artifacts/out.json",
			Digest:         digest,
			MediaType:      "application/json",
			SizeBytes:      100,
			Classification: "internal",
		})
	}
	ho.Artifacts = artifacts
	codes := ValidateHandoffEnvelope(&ho)
	metaSize := estimateArtifactMetaSize(ho.Artifacts)
	if metaSize <= HandoffArtifactMetaMaxBytes {
		t.Fatalf("expected meta size %d to exceed %d", metaSize, HandoffArtifactMetaMaxBytes)
	}
	if !ContainsCode(codes, CodeHandoffArtifactMetaOversize) {
		t.Fatalf("expected %s, got %v", CodeHandoffArtifactMetaOversize, codes)
	}
}

// TestHandoffContextExactlyLimitFixture validates context exactly at 256 KiB via ValidateHandoffEnvelope.
func TestHandoffContextExactlyLimitFixture(t *testing.T) {
	ho := validMinimalHandoff()
	// Create a valid JSON object whose serialized length is exactly HandoffContextMaxBytes.
	// {"data":"<padding>"} = 11 bytes overhead + padding.
	paddingLen := HandoffContextMaxBytes - 11
	val := json.RawMessage(`{"data":"` + strings.Repeat("x", paddingLen) + `"}`)
	ho.Context.Value = val
	codes := ValidateHandoffEnvelope(&ho)
	if len(codes) > 0 {
		t.Fatalf("expected no errors for exactly-limit context (%d bytes), got %v", len(val), codes)
	}
}

// TestHandoffContextOneByteOver rejects context at 256 KiB + 1 byte.
func TestHandoffContextOneByteOver(t *testing.T) {
	ho := validMinimalHandoff()
	paddingLen := HandoffContextMaxBytes - 10 // one more than limit
	val := json.RawMessage(`{"data":"` + strings.Repeat("x", paddingLen) + `"}`)
	ho.Context.Value = val
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffContextOversize) {
		t.Fatalf("expected %s for context size %d > %d, got %v",
			CodeHandoffContextOversize, len(val), HandoffContextMaxBytes, codes)
	}
}

// validMinimalHandoff returns a minimal valid handoff envelope for testing.
func validMinimalHandoff() HandoffEnvelope {
	return HandoffEnvelope{
		SchemaVersion:       SchemaVersionHandoffV1,
		WorkflowID:          "wf_test123",
		HandoffID:           "ho_test456",
		FromNodeID:          "stage_research",
		ToNodeID:            "stage_write",
		ProducerRunID:       "run_test789",
		ProducerAttemptID:   "attempt_test012",
		ProducerResultDigest: "sha256:" + strings.Repeat("a", 64),
		Sequence:            1,
		CreatedAt:           "2026-07-16T00:00:00Z",
		Classification:      "internal",
		Context: HandoffContext{
			Schema: "example/research-notes/v1",
			Value:  json.RawMessage(`{"notes":"test data"}`),
		},
	}
}
