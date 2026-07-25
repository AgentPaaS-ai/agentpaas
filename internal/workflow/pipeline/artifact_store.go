package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ArtifactBlob pairs artifact metadata with content.
// Content is only in the store, never in handoff context.
type ArtifactBlob struct {
	Ref     HandoffArtifact
	Content []byte
}

// ArtifactStore is the interface for artifact blob storage.
type ArtifactStore interface {
	// Put commits bytes under artifact_id; computes sha256 digest if Ref.Digest empty.
	// Rejects path traversal in ImmutableRef, symlinks if writing via path helper.
	Put(ctx context.Context, meta HandoffArtifact, content []byte) (HandoffArtifact, error)
	// Get retrieves artifact metadata and content by artifact_id.
	Get(ctx context.Context, artifactID string) (HandoffArtifact, []byte, error)
	// VerifyDigest checks that the stored content matches the stored digest.
	VerifyDigest(ctx context.Context, artifactID string) error
}

// MemoryArtifactStore is an in-memory implementation of ArtifactStore for tests and Promote.
type MemoryArtifactStore struct {
	mu    sync.Mutex
	blobs map[string]*ArtifactBlob
}

// NewMemoryArtifactStore creates a new in-memory artifact store.
func NewMemoryArtifactStore() *MemoryArtifactStore {
	return &MemoryArtifactStore{
		blobs: make(map[string]*ArtifactBlob),
	}
}

// Put stores content and computes digest if not already set.
func (s *MemoryArtifactStore) Put(_ context.Context, meta HandoffArtifact, content []byte) (HandoffArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if meta.ArtifactID == "" {
		return HandoffArtifact{}, fmt.Errorf("%s: artifact_id is required", CodeArtifactPathRejected)
	}

	// Validate immutable_ref path safety.
	if meta.ImmutableRef != "" {
		if !isSafeRef(meta.ImmutableRef) {
			return HandoffArtifact{}, fmt.Errorf("%s: unsafe immutable_ref %q", CodeArtifactPathRejected, meta.ImmutableRef)
		}
		// Reject path traversal specifically.
		if strings.Contains(meta.ImmutableRef, "..") || strings.HasPrefix(meta.ImmutableRef, "/") {
			return HandoffArtifact{}, fmt.Errorf("%s: path traversal in immutable_ref %q", CodeArtifactPathRejected, meta.ImmutableRef)
		}
	}

	// Compute digest if empty.
	digest := meta.Digest
	if digest == "" {
		h := sha256.Sum256(content)
		digest = "sha256:" + hex.EncodeToString(h[:])
	}

	result := HandoffArtifact{
		ArtifactID:     meta.ArtifactID,
		OwnerNodeID:    meta.OwnerNodeID,
		OwnerRunID:     meta.OwnerRunID,
		ImmutableRef:   meta.ImmutableRef,
		Digest:         digest,
		MediaType:      meta.MediaType,
		SizeBytes:      int64(len(content)),
		Classification: meta.Classification,
	}

	// Compute and validate digest matches.
	h := sha256.Sum256(content)
	expectedDigest := "sha256:" + hex.EncodeToString(h[:])
	if meta.Digest != "" && meta.Digest != expectedDigest {
		return HandoffArtifact{}, fmt.Errorf("%s: provided digest %q does not match content digest %q",
			CodeArtifactDigestMismatch, meta.Digest, expectedDigest)
	}

	result.Digest = expectedDigest
	result.SizeBytes = int64(len(content))

	s.blobs[meta.ArtifactID] = &ArtifactBlob{
		Ref:     result,
		Content: make([]byte, len(content)),
	}
	copy(s.blobs[meta.ArtifactID].Content, content)

	return result, nil
}

// Get retrieves artifact metadata and content.
func (s *MemoryArtifactStore) Get(_ context.Context, artifactID string) (HandoffArtifact, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blob, ok := s.blobs[artifactID]
	if !ok {
		return HandoffArtifact{}, nil, fmt.Errorf("%s: artifact %q not found", CodeArtifactNotFound, artifactID)
	}
	return blob.Ref, blob.Content, nil
}

// VerifyDigest checks that stored content matches stored digest.
func (s *MemoryArtifactStore) VerifyDigest(_ context.Context, artifactID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blob, ok := s.blobs[artifactID]
	if !ok {
		return fmt.Errorf("%s: artifact %q not found", CodeArtifactNotFound, artifactID)
	}

	h := sha256.Sum256(blob.Content)
	expected := "sha256:" + hex.EncodeToString(h[:])
	if blob.Ref.Digest != expected {
		return fmt.Errorf("%s: stored digest %q does not match content digest %q",
			CodeArtifactDigestMismatch, blob.Ref.Digest, expected)
	}
	return nil
}

// ---------------------------------------------------------------------------
// FS helpers for projection writing
// ---------------------------------------------------------------------------

// artifactPutToFS writes artifact content to a file under the given directory,
// using the safe basename of immutable_ref. Rejects path traversal.
func artifactPutToFS(dir string, art HandoffArtifact, content []byte) (string, error) {
	if art.ImmutableRef == "" {
		return "", fmt.Errorf("%s: empty immutable_ref", CodeArtifactPathRejected)
	}
	if !isSafeRef(art.ImmutableRef) {
		return "", fmt.Errorf("%s: unsafe immutable_ref %q", CodeArtifactPathRejected, art.ImmutableRef)
	}

	base := filepath.Base(filepath.Clean(art.ImmutableRef))
	if base == "." || base == ".." || base == "" {
		return "", fmt.Errorf("%s: invalid basename from immutable_ref %q", CodeArtifactPathRejected, art.ImmutableRef)
	}

	// Ensure the resolved path is beneath dir.
	target := filepath.Join(dir, base)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("abs dir: %w", err)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("abs target: %w", err)
	}
	if !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) && absTarget != absDir {
		return "", fmt.Errorf("%s: path escape from %q to %q", CodeArtifactPathRejected, dir, target)
	}

	// Reject writing through a symlink (symlink escape protection).
	// Lstat does not follow symlinks, so ModeSymlink is only set on the link itself.
	if fi, lerr := os.Lstat(target); lerr == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s: target %q is a symlink; refusing to follow", CodeArtifactSymlinkRejected, target)
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	return target, os.WriteFile(target, content, 0o400)
}
