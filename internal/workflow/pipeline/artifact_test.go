package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeSHA256(content []byte) string {
	h := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(h[:])
}

func newTestArtifact(opts ...func(*HandoffArtifact)) HandoffArtifact {
	a := HandoffArtifact{
		ArtifactID:     "test-artifact-001",
		OwnerNodeID:    "stage_a",
		OwnerRunID:     "run_1",
		ImmutableRef:   "output.json",
		MediaType:      "application/json",
		Classification: "internal",
	}
	for _, o := range opts {
		o(&a)
	}
	return a
}

// Test 1: Put/Get round-trip with digest computation.
func TestPutGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte(`{"result":"hello world"}`)
	meta := newTestArtifact()

	stored, err := store.Put(ctx, meta, content)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Digest should be computed.
	if stored.Digest == "" {
		t.Fatal("expected digest to be computed, got empty")
	}
	if !strings.HasPrefix(stored.Digest, "sha256:") {
		t.Fatalf("expected sha256: prefix, got %q", stored.Digest)
	}
	if stored.SizeBytes != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), stored.SizeBytes)
	}

	// Get should return same data.
	gotMeta, gotContent, err := store.Get(ctx, meta.ArtifactID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if gotMeta.Digest != stored.Digest {
		t.Fatalf("digest mismatch: %q != %q", gotMeta.Digest, stored.Digest)
	}
	if string(gotContent) != string(content) {
		t.Fatalf("content mismatch: %q != %q", string(gotContent), string(content))
	}
}

// Test 2: Put rejects path traversal.
func TestPutRejectsPathTraversal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()
	content := []byte("test")

	tests := []struct {
		name string
		ref  string
	}{
		{"dotdot", "../etc/passwd"},
		{"absolute", "/etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := newTestArtifact(func(a *HandoffArtifact) {
				a.ArtifactID = "bad-" + tt.name
				a.ImmutableRef = tt.ref
			})
			_, err := store.Put(ctx, meta, content)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.ref)
			}
			if !strings.Contains(err.Error(), CodeArtifactPathRejected) {
				t.Fatalf("expected %s in error, got: %v", CodeArtifactPathRejected, err)
			}
		})
	}
}

// Test 3: VerifyDigest detects mismatch after content tamper.
func TestVerifyDigestMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("original")
	meta := newTestArtifact()

	stored, err := store.Put(ctx, meta, content)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify should pass initially.
	if err := store.VerifyDigest(ctx, stored.ArtifactID); err != nil {
		t.Fatalf("initial VerifyDigest failed: %v", err)
	}

	// Tamper content directly in the store.
	store.mu.Lock()
	store.blobs[stored.ArtifactID].Content = []byte("tampered")
	store.mu.Unlock()

	// Verify should now fail.
	err = store.VerifyDigest(ctx, stored.ArtifactID)
	if err == nil {
		t.Fatal("expected VerifyDigest to fail after tampering, got nil")
	}
	if !strings.Contains(err.Error(), CodeArtifactDigestMismatch) {
		t.Fatalf("expected %s in error, got: %v", CodeArtifactDigestMismatch, err)
	}
}

// Test 4: Promote happy path with two artifacts.
func TestPromoteHappy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content1 := []byte(`{"step":"one"}`)
	content2 := []byte(`{"step":"two"}`)

	art1, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "art-1"
		a.ImmutableRef = "step1.json"
	}), content1)
	if err != nil {
		t.Fatalf("Put art1 failed: %v", err)
	}

	art2, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "art-2"
		a.ImmutableRef = "step2.json"
	}), content2)
	if err != nil {
		t.Fatalf("Put art2 failed: %v", err)
	}

	budget := &WorkflowArtifactBudget{
		MaxTotalBytes: 1 << 20, // 1 MiB
		MaxCount:      10,
	}

	promoted, err := PromoteHandoffArtifacts(ctx, store, budget, "stage_a", "run_1", []HandoffArtifact{art1, art2})
	if err != nil {
		t.Fatalf("PromoteHandoffArtifacts failed: %v", err)
	}
	if len(promoted) != 2 {
		t.Fatalf("expected 2 promoted, got %d", len(promoted))
	}
	if budget.UsedBytes != int64(len(content1)+len(content2)) {
		t.Fatalf("expected UsedBytes=%d, got %d", len(content1)+len(content2), budget.UsedBytes)
	}
	if budget.UsedCount != 2 {
		t.Fatalf("expected UsedCount=2, got %d", budget.UsedCount)
	}
}

// Test 5: Budget exhaustion when MaxTotalBytes is small.
func TestPromoteBudgetExhausted(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content1 := []byte(strings.Repeat("x", 100))
	art1, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "big-1"
	}), content1)

	content2 := []byte(strings.Repeat("y", 200))
	art2, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "big-2"
	}), content2)

	budget := &WorkflowArtifactBudget{
		MaxTotalBytes: 150, // only fits first artifact
		MaxCount:      10,
	}

	_, err := PromoteHandoffArtifacts(ctx, store, budget, "stage_a", "run_1", []HandoffArtifact{art1, art2})
	if err == nil {
		t.Fatal("expected budget exhausted error, got nil")
	}
	if !strings.Contains(err.Error(), CodeArtifactBudgetExceeded) {
		t.Fatalf("expected %s in error, got: %v", CodeArtifactBudgetExceeded, err)
	}
}

// Test 6: Promote rejects owner mismatch.
func TestPromoteOwnerMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("test")
	art, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "owner-test"
		a.OwnerNodeID = "stage_a"
		a.OwnerRunID = "run_1"
	}), content)

	// Wrong node.
	_, err := PromoteHandoffArtifacts(ctx, store, nil, "stage_b", "run_1", []HandoffArtifact{art})
	if err == nil {
		t.Fatal("expected owner mismatch for wrong node, got nil")
	}
	if !strings.Contains(err.Error(), CodeArtifactOwnerMismatch) {
		t.Fatalf("expected %s, got: %v", CodeArtifactOwnerMismatch, err)
	}

	// Wrong run.
	_, err = PromoteHandoffArtifacts(ctx, store, nil, "stage_a", "run_999", []HandoffArtifact{art})
	if err == nil {
		t.Fatal("expected owner mismatch for wrong run, got nil")
	}
	if !strings.Contains(err.Error(), CodeArtifactOwnerMismatch) {
		t.Fatalf("expected %s, got: %v", CodeArtifactOwnerMismatch, err)
	}
}

// Test 7: BuildROProjection writes content as read-only files.
func TestBuildROProjection_ReadOnlyFiles(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte(`{"data":"hello"}`)
	art, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "proj-1"
		a.ImmutableRef = "data.json"
	}), content)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	tmpDir := t.TempDir()
	proj, err := BuildROProjection(ctx, store, tmpDir, []HandoffArtifact{art})
	if err != nil {
		t.Fatalf("BuildROProjection failed: %v", err)
	}
	defer func() { _ = proj.Cleanup() }()

	// Check projection dir exists.
	if _, err := os.Stat(proj.Dir); os.IsNotExist(err) {
		t.Fatal("projection dir does not exist")
	}

	// Check file exists inside Dir.
	entries, err := os.ReadDir(proj.Dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no files in projection dir")
	}
	filePath := filepath.Join(proj.Dir, entries[0].Name())
	gotContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("readfile: %v", err)
	}
	if string(gotContent) != string(content) {
		t.Fatalf("content mismatch: %q != %q", string(gotContent), string(content))
	}

	// Check it is read-only (0400).
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("file is world-writable: %o", info.Mode().Perm())
	}

	// Check binds.
	if len(proj.Plan.Binds) != 1 {
		t.Fatalf("expected 1 bind, got %d", len(proj.Plan.Binds))
	}
	if !strings.HasSuffix(proj.Plan.Binds[0], ":ro") {
		t.Fatalf("bind must end with :ro, got %q", proj.Plan.Binds[0])
	}
	if proj.Plan.MountRoot != "/agentpaas/incoming-artifacts" {
		t.Fatalf("expected MountRoot /agentpaas/incoming-artifacts, got %q", proj.Plan.MountRoot)
	}
}

// Test 8: BuildROProjection rejects unsafe refs (path traversal).
func TestBuildROProjection_RejectsUnsafeRef(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("test")
	art, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "unsafe-1"
		a.ImmutableRef = "ok.json"
	}), content)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Corrupt the artifact's immutable_ref to point outside.
	badArt := art
	badArt.ImmutableRef = "../etc/passwd"

	tmpDir := t.TempDir()
	_, err = BuildROProjection(ctx, store, tmpDir, []HandoffArtifact{badArt})
	if err == nil {
		t.Fatal("expected rejection of unsafe ref, got nil")
	}
	if !strings.Contains(err.Error(), CodeArtifactPathRejected) {
		t.Fatalf("expected %s, got: %v", CodeArtifactPathRejected, err)
	}
}

// Test 9: Cleanup removes projection directory.
func TestProjectionCleanup(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("test")
	art, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "cleanup-1"
		a.ImmutableRef = "file.txt"
	}), content)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	tmpDir := t.TempDir()
	proj, err := BuildROProjection(ctx, store, tmpDir, []HandoffArtifact{art})
	if err != nil {
		t.Fatalf("BuildROProjection failed: %v", err)
	}

	// Verify dir exists.
	if _, err := os.Stat(proj.Dir); os.IsNotExist(err) {
		t.Fatal("projection dir should exist before cleanup")
	}

	// Cleanup.
	if err := proj.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify dir is gone.
	if _, err := os.Stat(proj.Dir); !os.IsNotExist(err) {
		t.Fatal("projection dir should not exist after cleanup")
	}
}

// Test 10: Multi-megabyte handoff stays in store, not context.
func TestMultiMegabyteHandoff(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	// 2 MiB blob.
	size2MB := 2 * 1024 * 1024
	bigContent := make([]byte, size2MB)
	for i := range bigContent {
		bigContent[i] = byte(i % 256)
	}

	art, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "big-blob"
		a.ImmutableRef = "large.bin"
	}), bigContent)
	if err != nil {
		t.Fatalf("Put big blob failed: %v", err)
	}

	if art.SizeBytes != int64(size2MB) {
		t.Fatalf("expected SizeBytes=%d, got %d", size2MB, art.SizeBytes)
	}

	// Context size check: artifact ref itself is small (< 1KB).
	refSize := len(art.ArtifactID) + len(art.ImmutableRef) + len(art.Digest) + len(art.Classification)
	_ = refSize // just verifying it's small
	if refSize > 1024 {
		t.Fatalf("artifact ref metadata is too large: %d bytes", refSize)
	}

	// Promote should work.
	budget := &WorkflowArtifactBudget{
		MaxTotalBytes: int64(size2MB) + 1,
		MaxCount:      10,
	}
	promoted, err := PromoteHandoffArtifacts(ctx, store, budget, "stage_a", "run_1", []HandoffArtifact{art})
	if err != nil {
		t.Fatalf("PromoteHandoffArtifacts failed: %v", err)
	}
	if len(promoted) != 1 {
		t.Fatalf("expected 1 promoted, got %d", len(promoted))
	}

	// Build projection with large blob.
	tmpDir := t.TempDir()
	proj, err := BuildROProjection(ctx, store, tmpDir, []HandoffArtifact{art})
	if err != nil {
		t.Fatalf("BuildROProjection with 2MB blob failed: %v", err)
	}
	defer func() { _ = proj.Cleanup() }()

	entries, err := os.ReadDir(proj.Dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in projection, got %d", len(entries))
	}
}

// Test 11: ProvenanceChain returns producer run IDs.
func TestProvenanceChain(t *testing.T) {
	arts := []HandoffArtifact{
		{ArtifactID: "a1", OwnerRunID: "run_1"},
		{ArtifactID: "a2", OwnerRunID: "run_2"},
		{ArtifactID: "a3", OwnerRunID: "run_3"},
		{ArtifactID: "a4", OwnerRunID: ""}, // empty run ID skipped
	}

	chain := ProvenanceChain(arts)
	if len(chain) != 3 {
		t.Fatalf("expected 3 run IDs, got %d: %v", len(chain), chain)
	}
	expected := []string{"run_1", "run_2", "run_3"}
	for i, want := range expected {
		if chain[i] != want {
			t.Fatalf("chain[%d]: expected %q, got %q", i, want, chain[i])
		}
	}
}

// Test 12: Classification - confidential envelope rejects public artifact.
func TestClassificationNoDeclassify(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("test")
	// Create artifact with "public" classification.
	art, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "pub-art"
		a.ImmutableRef = "data.json"
		a.Classification = "public"
	}), content)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Create envelope with "confidential" classification containing the public artifact.
	envelope := validMinimalHandoff()
	envelope.Classification = "confidential"
	envelope.Artifacts = []HandoffArtifact{art}

	codes := ValidateHandoffEnvelope(&envelope)

	// The public artifact is less restrictive than the confidential envelope.
	if !ContainsCode(codes, CodeHandoffDeclassification) {
		t.Fatalf("expected %s when public artifact in confidential envelope, got %v",
			CodeHandoffDeclassification, codes)
	}
}

// Test: empty budget (nil) is allowed in Promote.
func TestPromoteNilBudget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	art, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "nb-1"
	}), []byte("test"))

	promoted, err := PromoteHandoffArtifacts(ctx, store, nil, "stage_a", "run_1", []HandoffArtifact{art})
	if err != nil {
		t.Fatalf("PromoteHandoffArtifacts with nil budget failed: %v", err)
	}
	if len(promoted) != 1 {
		t.Fatalf("expected 1 promoted, got %d", len(promoted))
	}
}

// Test: Put with pre-set digest that matches content.
func TestPutWithCorrectDigest(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("hello world")
	correctDigest := makeSHA256(content)

	meta := newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "pre-digest"
		a.Digest = correctDigest
	})

	stored, err := store.Put(ctx, meta, content)
	if err != nil {
		t.Fatalf("Put with correct digest failed: %v", err)
	}
	if stored.Digest != correctDigest {
		t.Fatalf("expected digest %q, got %q", correctDigest, stored.Digest)
	}
}

// Test: Put with wrong digest rejects.
func TestPutRejectsWrongDigest(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("hello world")
	wrongDigest := "sha256:" + strings.Repeat("0", 64)

	meta := newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "wrong-digest"
		a.Digest = wrongDigest
	})

	_, err := store.Put(ctx, meta, content)
	if err == nil {
		t.Fatal("expected error for wrong digest, got nil")
	}
	if !strings.Contains(err.Error(), CodeArtifactDigestMismatch) {
		t.Fatalf("expected %s, got: %v", CodeArtifactDigestMismatch, err)
	}
}

// Test: artifact classified as "restricted" in "internal" envelope is OK (more restrictive).
func TestClassificationMoreRestrictiveOK(t *testing.T) {
	envelope := validMinimalHandoff()
	envelope.Classification = "internal"
	digest := "sha256:" + strings.Repeat("a", 64)
	envelope.Artifacts = []HandoffArtifact{
		{
			ArtifactID:     "art_restricted",
			OwnerNodeID:    "stage_a",
			OwnerRunID:     "run_1",
			ImmutableRef:   "data.bin",
			Digest:         digest,
			MediaType:      "application/octet-stream",
			SizeBytes:      100,
			Classification: "restricted",
		},
	}

	codes := ValidateHandoffEnvelope(&envelope)
	if ContainsCode(codes, CodeHandoffDeclassification) {
		t.Fatalf("restricted artifact in internal envelope should NOT trigger declassification, got %v", codes)
	}
}

// Test: VerifyDigest on non-existent artifact.
func TestVerifyDigestNotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	err := store.VerifyDigest(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent artifact")
	}
	if !strings.Contains(err.Error(), CodeArtifactNotFound) {
		t.Fatalf("expected %s, got: %v", CodeArtifactNotFound, err)
	}
}

// Test: Get non-existent artifact.
func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	_, _, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent artifact")
	}
	if !strings.Contains(err.Error(), CodeArtifactNotFound) {
		t.Fatalf("expected %s, got: %v", CodeArtifactNotFound, err)
	}
}

// Test: Promote rejects unsafe immutable_ref.
func TestPromoteRejectsUnsafeRef(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	content := []byte("test")
	art, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "unsafe-promote"
		a.ImmutableRef = "ok.json"
	}), content)

	badArt := art
	badArt.ImmutableRef = "../etc/passwd"

	_, err := PromoteHandoffArtifacts(ctx, store, nil, "stage_a", "run_1", []HandoffArtifact{badArt})
	if err == nil {
		t.Fatal("expected error for unsafe immutable_ref in Promote")
	}
	if !strings.Contains(err.Error(), CodeArtifactPathRejected) {
		t.Fatalf("expected %s, got: %v", CodeArtifactPathRejected, err)
	}
}

// Test: Put with empty ArtifactID.
func TestPutRejectsEmptyArtifactID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	meta := newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = ""
	})

	_, err := store.Put(ctx, meta, []byte("test"))
	if err == nil {
		t.Fatal("expected error for empty artifact_id")
	}
	if !strings.Contains(err.Error(), CodeArtifactPathRejected) {
		t.Fatalf("expected %s, got: %v", CodeArtifactPathRejected, err)
	}
}

// Test: ProvenanceChain with no artifacts returns empty list.
func TestProvenanceChainEmpty(t *testing.T) {
	chain := ProvenanceChain(nil)
	if len(chain) != 0 {
		t.Fatalf("expected empty chain, got %v", chain)
	}

	chain = ProvenanceChain([]HandoffArtifact{})
	if len(chain) != 0 {
		t.Fatalf("expected empty chain, got %v", chain)
	}
}

// Test: Put with empty content works (zero-length file).
func TestPutEmptyContent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	meta := newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "empty-content"
	})

	stored, err := store.Put(ctx, meta, []byte{})
	if err != nil {
		t.Fatalf("Put empty content failed: %v", err)
	}
	if stored.SizeBytes != 0 {
		t.Fatalf("expected SizeBytes=0, got %d", stored.SizeBytes)
	}
	if stored.Digest == "" {
		t.Fatal("expected digest for empty content")
	}

	// Verify should pass.
	if err := store.VerifyDigest(ctx, stored.ArtifactID); err != nil {
		t.Fatalf("VerifyDigest for empty content failed: %v", err)
	}
}

// Test: WorkflowArtifactBudget MaxCount exceeded.
func TestBudgetMaxCountExceeded(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	art1, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "count-1"
	}), []byte("a"))

	art2, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "count-2"
	}), []byte("b"))

	budget := &WorkflowArtifactBudget{
		MaxTotalBytes: 1 << 20,
		MaxCount:      1,
	}

	_, err := PromoteHandoffArtifacts(ctx, store, budget, "stage_a", "run_1", []HandoffArtifact{art1, art2})
	if err == nil {
		t.Fatal("expected budget count exceeded error")
	}
	if !strings.Contains(err.Error(), CodeArtifactBudgetExceeded) {
		t.Fatalf("expected %s, got: %v", CodeArtifactBudgetExceeded, err)
	}
}

// Test: ValidateHandoffArtifacts catches bad digests and unsafe refs.
func TestValidateHandoffArtifacts(t *testing.T) {
	arts := []HandoffArtifact{
		{ArtifactID: "a1", Digest: "md5:abc", ImmutableRef: "ok.json"},
		{ArtifactID: "a2", Digest: "sha256:" + strings.Repeat("a", 64), ImmutableRef: "../bad"},
	}

	codes := ValidateHandoffArtifacts(arts)
	if len(codes) != 2 {
		t.Fatalf("expected 2 validation errors, got %d: %v", len(codes), codes)
	}
	if !ContainsCode(codes, CodeArtifactDigestMismatch) {
		t.Fatalf("expected %s, got %v", CodeArtifactDigestMismatch, codes)
	}
	if !ContainsCode(codes, CodeArtifactPathRejected) {
		t.Fatalf("expected %s, got %v", CodeArtifactPathRejected, codes)
	}
}

// Test: Promote with artifact not in store.
func TestPromoteArtifactNotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	art := newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "not-stored"
	})

	_, err := PromoteHandoffArtifacts(ctx, store, nil, "stage_a", "run_1", []HandoffArtifact{art})
	if err == nil {
		t.Fatal("expected error for artifact not found")
	}
	if !strings.Contains(err.Error(), CodeArtifactNotFound) {
		t.Fatalf("expected %s, got: %v", CodeArtifactNotFound, err)
	}
}

// Test: BuildROProjection with empty baseDir.
func TestBuildROProjectionEmptyBaseDir(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	art, _ := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "bd-1"
	}), []byte("test"))

	_, err := BuildROProjection(ctx, store, "", []HandoffArtifact{art})
	if err == nil {
		t.Fatal("expected error for empty baseDir")
	}
	if !strings.Contains(err.Error(), CodeArtifactPathRejected) {
		t.Fatalf("expected %s, got: %v", CodeArtifactPathRejected, err)
	}
}

// Test: Put with safe ref containing valid path segments passes.
func TestPutSafeRefs(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	safeRefs := []string{
		"artifacts/output.json",
		"results/data.csv",
		"stage_a/file.txt",
		"a/b/c/d/e/f/g/h/i.json",
	}

	for _, ref := range safeRefs {
		meta := newTestArtifact(func(a *HandoffArtifact) {
			a.ArtifactID = fmt.Sprintf("safe-%s", strings.ReplaceAll(ref, "/", "-"))
			a.ImmutableRef = ref
		})
		_, err := store.Put(ctx, meta, []byte("ok"))
		if err != nil {
			t.Fatalf("Put(%q) failed: %v", ref, err)
		}
	}
}

