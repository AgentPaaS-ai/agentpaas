package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestBroker(t *testing.T) (*FileArtifactBroker, string) {
	t.Helper()
	root := t.TempDir()
	broker, err := NewFileArtifactBroker(root)
	if err != nil {
		t.Fatalf("NewFileArtifactBroker: %v", err)
	}
	return broker, root
}

func artifactStoragePath(root, workflowID, artifactID string) string {
	return filepath.Join(root, "artifacts", "transfer", workflowID, artifactID)
}

func testCommitReq(readerContents string, audience []string) CommitReq {
	return CommitReq{
		WorkflowID:       "wf-test",
		ProducerRunID:    "run-producer",
		ProducerAttemptID: "att-producer",
		ProducerTaskID:   "task-producer",
		LogicalRef:       "output.json",
		MediaType:        "application/json",
		Classification:   ClassificationInternal,
		Audience:         audience,
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
		Reader:           strings.NewReader(readerContents),
		MaxBytes:         1024 * 1024, // 1 MiB
	}
}

// ---------------------------------------------------------------------------
// 1. Commit + Authorize + Project happy path
// ---------------------------------------------------------------------------

func TestArtifactBroker_CommitAuthorizeProject_HappyPath(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	contents := `{"result": "verified"}`
	audience := []string{"consumer-logical-id"}

	// Commit
	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify ref fields
	if ref.ArtifactID == "" {
		t.Error("artifact_id must not be empty")
	}
	expectedDigest := sha256Hex([]byte(contents))
	if ref.Digest != expectedDigest {
		t.Errorf("digest mismatch: got %s, want %s", ref.Digest, expectedDigest)
	}
	if ref.WorkflowID != "wf-test" {
		t.Errorf("workflow_id: got %s, want wf-test", ref.WorkflowID)
	}
	if ref.ProducerRunID != "run-producer" {
		t.Errorf("producer_run_id: got %s", ref.ProducerRunID)
	}
	if ref.ByteSize != int64(len(contents)) {
		t.Errorf("byte_size: got %d, want %d", ref.ByteSize, len(contents))
	}
	if ref.LogicalRef != "output.json" {
		t.Errorf("logical_ref: got %s, want output.json", ref.LogicalRef)
	}
	if len(ref.Audience) != 1 || ref.Audience[0] != "consumer-logical-id" {
		t.Errorf("audience: got %v", ref.Audience)
	}

	// Authorize
	now := time.Now().UTC()
	if err := broker.AuthorizeRead(ctx, ref.ArtifactID, "consumer-logical-id", "wf-test", now); err != nil {
		t.Errorf("AuthorizeRead: %v", err)
	}

	// Project
	proj, err := broker.ProjectReadOnly(ctx, ref.ArtifactID, "consumer-logical-id", now)
	if err != nil {
		t.Fatalf("ProjectReadOnly: %v", err)
	}
	if proj.ArtifactID != ref.ArtifactID {
		t.Errorf("projected artifact_id: got %s, want %s", proj.ArtifactID, ref.ArtifactID)
	}
	if proj.Digest != expectedDigest {
		t.Errorf("projected digest: got %s, want %s", proj.Digest, expectedDigest)
	}
	if proj.LogicalRef != "output.json" {
		t.Errorf("projected logical_ref: got %s", proj.LogicalRef)
	}
	if proj.ByteSize != int64(len(contents)) {
		t.Errorf("projected byte_size: got %d", proj.ByteSize)
	}
	if proj.MediaType != "application/json" {
		t.Errorf("projected media_type: got %s", proj.MediaType)
	}
	if proj.ReadOnlyRoot == "" {
		t.Error("projected ReadOnlyRoot must not be empty")
	}
	if !strings.HasPrefix(proj.ReadOnlyRoot, broker.root) {
		t.Errorf("ReadOnlyRoot %s not under broker root %s", proj.ReadOnlyRoot, broker.root)
	}

	// Verify blob contents at projected path
	blobPath := filepath.Join(proj.ReadOnlyRoot, "blob")
	blobData, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("read projected blob: %v", err)
	}
	if string(blobData) != contents {
		t.Errorf("blob contents: got %q, want %q", string(blobData), contents)
	}

	// VerifyDigest
	if err := broker.VerifyDigest(ctx, ref.ArtifactID); err != nil {
		t.Errorf("VerifyDigest: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 2. Audience mismatch
// ---------------------------------------------------------------------------

func TestArtifactBroker_AudienceMismatch(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	contents := `{"data": "secret"}`
	audience := []string{"alice"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	now := time.Now().UTC()
	// bob is not in audience
	err = broker.AuthorizeRead(ctx, ref.ArtifactID, "bob", "wf-test", now)
	if err == nil {
		t.Error("expected audience mismatch error for bob")
	}

	// Also reject ProjectReadOnly for bob
	_, err = broker.ProjectReadOnly(ctx, ref.ArtifactID, "bob", now)
	if err == nil {
		t.Error("expected audience mismatch error in ProjectReadOnly for bob")
	}
}

// ---------------------------------------------------------------------------
// 3. Expired ref
// ---------------------------------------------------------------------------

func TestArtifactBroker_ExpiredRef(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	contents := `{"data": "expired"}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	req.ExpiresAt = time.Now().UTC().Add(-1 * time.Hour) // already expired
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	now := time.Now().UTC()
	err = broker.AuthorizeRead(ctx, ref.ArtifactID, "reader", "wf-test", now)
	if err == nil {
		t.Error("expected expired error")
	}

	_, err = broker.ProjectReadOnly(ctx, ref.ArtifactID, "reader", now)
	if err == nil {
		t.Error("expected expired error in ProjectReadOnly")
	}
}

// ---------------------------------------------------------------------------
// 4. Digest tamper on disk detected
// ---------------------------------------------------------------------------

func TestArtifactBroker_DigestTamper(t *testing.T) {
	broker, root := newTestBroker(t)
	ctx := context.Background()
	contents := `{"original": true}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Tamper with blob on disk
	artifactDir := artifactStoragePath(root, "wf-test", ref.ArtifactID)
	blobPath := filepath.Join(artifactDir, "blob")
	if err := os.WriteFile(blobPath, []byte(`{"tampered": true}`), 0o600); err != nil {
		t.Fatalf("tamper blob: %v", err)
	}

	// VerifyDigest must fail
	if err := broker.VerifyDigest(ctx, ref.ArtifactID); err == nil {
		t.Error("expected digest mismatch error after tamper")
	}

	// ProjectReadOnly must also fail
	now := time.Now().UTC()
	_, err = broker.ProjectReadOnly(ctx, ref.ArtifactID, "reader", now)
	if err == nil {
		t.Error("expected digest mismatch error in ProjectReadOnly after tamper")
	}
}

// ---------------------------------------------------------------------------
// 5. Path traversal logical_ref rejected
// ---------------------------------------------------------------------------

func TestArtifactBroker_PathTraversalRejected(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()

	traversalPaths := []string{
		"../etc/passwd",
		"..\\windows",
		"/etc/passwd",
		"./../../../secret",
		"foo/../../bar",
	}

	for _, lp := range traversalPaths {
		t.Run("ref="+lp, func(t *testing.T) {
			req := testCommitReq("data", []string{"reader"})
			req.LogicalRef = lp
			_, err := broker.Commit(ctx, req)
			if err == nil {
				t.Errorf("expected rejection for logical_ref %q", lp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. Symlink blob rejected (if create attempted)
// ---------------------------------------------------------------------------

func TestArtifactBroker_SymlinkBlobRejected(t *testing.T) {
	broker, root := newTestBroker(t)
	ctx := context.Background()
	contents := `{"legit": true}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Try to replace blob with a symlink
	artifactDir := artifactStoragePath(root, "wf-test", ref.ArtifactID)
	blobPath := filepath.Join(artifactDir, "blob")
	symlinkTarget := filepath.Join(root, "evil")

	if err := os.Remove(blobPath); err != nil {
		t.Fatalf("remove blob for symlink test: %v", err)
	}
	if err := os.Symlink(symlinkTarget, blobPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// VerifyDigest must reject symlink
	err = broker.VerifyDigest(ctx, ref.ArtifactID)
	if err == nil {
		t.Error("expected symlink rejection in VerifyDigest")
	}

	// ProjectReadOnly must also reject
	now := time.Now().UTC()
	_, err = broker.ProjectReadOnly(ctx, ref.ArtifactID, "reader", now)
	if err == nil {
		t.Error("expected symlink rejection in ProjectReadOnly")
	}
}

// ---------------------------------------------------------------------------
// 7. Hardlink rejected if Nlink > 1
// ---------------------------------------------------------------------------

func TestArtifactBroker_HardlinkRejected(t *testing.T) {
	broker, root := newTestBroker(t)
	ctx := context.Background()
	contents := `{"legit": true}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Create a hard link to the blob (raises Nlink)
	artifactDir := artifactStoragePath(root, "wf-test", ref.ArtifactID)
	blobPath := filepath.Join(artifactDir, "blob")
	hardlinkPath := filepath.Join(root, "hardlink-copy")

	if err := os.Link(blobPath, hardlinkPath); err != nil {
		t.Fatalf("create hardlink: %v", err)
	}
	defer func() { _ = os.Remove(hardlinkPath) }()

	// Verify Nlink > 1
	fi, err := os.Lstat(blobPath)
	if err != nil {
		t.Fatalf("lstat blob: %v", err)
	}
	if sysStat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if sysStat.Nlink <= 1 {
			t.Skip("filesystem does not support hardlink nlink detection")
		}
	}

	// VerifyDigest must reject hardlink
	err = broker.VerifyDigest(ctx, ref.ArtifactID)
	if err == nil {
		t.Error("expected hardlink rejection in VerifyDigest")
	}

	// ProjectReadOnly must also reject
	now := time.Now().UTC()
	_, err = broker.ProjectReadOnly(ctx, ref.ArtifactID, "reader", now)
	if err == nil {
		t.Error("expected hardlink rejection in ProjectReadOnly")
	}
}

// ---------------------------------------------------------------------------
// 8. Concurrent commit unique IDs
// ---------------------------------------------------------------------------

func TestArtifactBroker_ConcurrentCommitUniqueIDs(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()

	const concurrency = 10
	var wg sync.WaitGroup
	refs := make([]TransferableArtifactRef, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := testCommitReq(fmt.Sprintf("data-%d", idx), []string{"reader"})
			ref, err := broker.Commit(ctx, req)
			refs[idx] = ref
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent commit %d: %v", i, err)
		}
	}

	// All IDs must be unique
	seen := make(map[string]bool)
	for _, ref := range refs {
		if ref.ArtifactID == "" {
			t.Error("empty artifact_id")
		}
		if seen[ref.ArtifactID] {
			t.Errorf("duplicate artifact_id: %s", ref.ArtifactID)
		}
		seen[ref.ArtifactID] = true
	}

	if len(seen) != concurrency {
		t.Errorf("expected %d unique IDs, got %d", concurrency, len(seen))
	}
}

// ---------------------------------------------------------------------------
// 9. MaxBytes exceeded
// ---------------------------------------------------------------------------

func TestArtifactBroker_MaxBytesExceeded(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()

	// Commit with very small MaxBytes
	req := testCommitReq("this is more than 5 bytes", []string{"reader"})
	req.MaxBytes = 5
	_, err := broker.Commit(ctx, req)
	if err == nil {
		t.Error("expected MaxBytes exceeded error")
	}
}

// ---------------------------------------------------------------------------
// Additional: workflow ID mismatch in AuthorizeRead
// ---------------------------------------------------------------------------

func TestArtifactBroker_WorkflowMismatch(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	contents := `{"data": "wf-bound"}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	now := time.Now().UTC()
	// Wrong workflow ID
	err = broker.AuthorizeRead(ctx, ref.ArtifactID, "reader", "wf-other", now)
	if err == nil {
		t.Error("expected workflow mismatch error")
	}
}

// ---------------------------------------------------------------------------
// Additional: Re-commit same data produces different artifact IDs
// ---------------------------------------------------------------------------

func TestArtifactBroker_RecommitDifferentIDs(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	contents := `{"idempotent": false}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	ref1, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit 1: %v", err)
	}
	req2 := testCommitReq(contents, audience)
	ref2, err := broker.Commit(ctx, req2)
	if err != nil {
		t.Fatalf("Commit 2: %v", err)
	}

	if ref1.ArtifactID == ref2.ArtifactID {
		t.Error("re-commit produced same artifact_id — IDs must be unique")
	}
	// Same digest for same content
	if ref1.Digest != ref2.Digest {
		t.Errorf("same content should produce same digest: %s vs %s", ref1.Digest, ref2.Digest)
	}
}

// ---------------------------------------------------------------------------
// Additional: ReadOnlyRoot ensures blob is immutable via permissions
// ---------------------------------------------------------------------------

func TestArtifactBroker_ProjectReadOnlyRootIsImmutable(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	contents := `{"readonly": true}`
	audience := []string{"reader"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	now := time.Now().UTC()
	proj, err := broker.ProjectReadOnly(ctx, ref.ArtifactID, "reader", now)
	if err != nil {
		t.Fatalf("ProjectReadOnly: %v", err)
	}

	// The projected artifact dir should contain blob with 0400 perms
	blobPath := filepath.Join(proj.ReadOnlyRoot, "blob")
	fi, err := os.Lstat(blobPath)
	if err != nil {
		t.Fatalf("lstat blob: %v", err)
	}
	if fi.Mode().Perm()&0o200 != 0 {
		t.Errorf("blob should not be owner-writable after project, got %#o", fi.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// Additional: meta.json round trip
// ---------------------------------------------------------------------------

func TestArtifactBroker_MetaJSON_RoundTrip(t *testing.T) {
	broker, root := newTestBroker(t)
	ctx := context.Background()
	contents := `{"meta": "test"}`
	audience := []string{"alice", "bob"}

	req := testCommitReq(contents, audience)
	ref, err := broker.Commit(ctx, req)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Read meta.json from disk and verify
	artifactDir := artifactStoragePath(root, "wf-test", ref.ArtifactID)
	metaPath := filepath.Join(artifactDir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}

	var storedRef TransferableArtifactRef
	if err := json.Unmarshal(data, &storedRef); err != nil {
		t.Fatalf("unmarshal meta.json: %v", err)
	}

	if storedRef.ArtifactID != ref.ArtifactID {
		t.Error("meta.json artifact_id mismatch")
	}
	if storedRef.Digest != ref.Digest {
		t.Error("meta.json digest mismatch")
	}
	if storedRef.LogicalRef != ref.LogicalRef {
		t.Error("meta.json logical_ref mismatch")
	}
	if ref.MediaType != "application/json" {
		t.Error("meta.json media_type mismatch")
	}
	if storedRef.ByteSize != ref.ByteSize {
		t.Error("meta.json byte_size mismatch")
	}
	if len(storedRef.Audience) != 2 {
		t.Errorf("meta.json audience len: got %d, want 2", len(storedRef.Audience))
	}
}

// ---------------------------------------------------------------------------
// Additional: VerifyDigest on non-existent artifact
// ---------------------------------------------------------------------------

func TestArtifactBroker_VerifyDigest_NotFound(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()

	err := broker.VerifyDigest(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected not-found error")
	}
}

// ---------------------------------------------------------------------------
// Additional: AuthorizeRead on non-existent artifact
// ---------------------------------------------------------------------------

func TestArtifactBroker_AuthorizeRead_NotFound(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := broker.AuthorizeRead(ctx, "nonexistent-id", "reader", "wf-test", now)
	if err == nil {
		t.Error("expected not-found error")
	}
}

// ---------------------------------------------------------------------------
// Additional: empty reader (io.Reader nil)
// ---------------------------------------------------------------------------

func TestArtifactBroker_Commit_NilReader(t *testing.T) {
	broker, _ := newTestBroker(t)
	ctx := context.Background()

	req := CommitReq{
		WorkflowID:       "wf-test",
		ProducerRunID:    "run-1",
		ProducerAttemptID: "att-1",
		ProducerTaskID:   "task-1",
		LogicalRef:       "empty.txt",
		MediaType:        "text/plain",
		Classification:   ClassificationPublic,
		Audience:         []string{"reader"},
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
		Reader:           nil,
		MaxBytes:         1024,
	}
	_, err := broker.Commit(ctx, req)
	if err == nil {
		t.Error("expected error for nil reader")
	}
}
