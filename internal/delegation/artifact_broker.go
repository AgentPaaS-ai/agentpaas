package delegation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// ArtifactBroker interface
// ---------------------------------------------------------------------------

// ArtifactBroker manages digest-bound, audience-scoped artifact transfer.
// Committed artifacts are immutable; consumers receive read-only projected
// paths after authorization.
type ArtifactBroker interface {
	// Commit writes the artifact blob, records metadata, and returns an
	// immutable TransferableArtifactRef.
	Commit(ctx context.Context, req CommitReq) (TransferableArtifactRef, error)

	// AuthorizeRead checks that consumerLogicalID is in the artifact's
	// audience, the workflow matches, and the ref has not expired.
	AuthorizeRead(ctx context.Context, artifactID, consumerLogicalID, workflowID string, now time.Time) error

	// ProjectReadOnly verifies digest, audience, expiry, and returns a
	// ProjectedArtifact with a read-only host path under the broker store.
	ProjectReadOnly(ctx context.Context, artifactID, consumerLogicalID string, now time.Time) (ProjectedArtifact, error)

	// VerifyDigest re-hashes the blob and compares against the recorded digest.
	VerifyDigest(ctx context.Context, artifactID string) error
}

// CommitReq describes an artifact to commit.
type CommitReq struct {
	WorkflowID        string
	ProducerRunID     string
	ProducerAttemptID string
	ProducerTaskID    string
	LogicalRef        string
	MediaType         string
	Classification    Classification
	Audience          []string
	ExpiresAt         time.Time
	// Reader is the source data. May be nil (error). Read up to MaxBytes.
	Reader   io.Reader
	MaxBytes int64
}

// ProjectedArtifact is the read-only projection returned to a consumer.
// ReadOnlyRoot is a host path under the broker store — NEVER a peer
// container path.
type ProjectedArtifact struct {
	ArtifactID   string
	Digest       string
	LogicalRef   string
	ReadOnlyRoot string
	ByteSize     int64
	MediaType    string
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrArtifactNotFound       = errors.New("delegation: artifact not found")
	ErrArtifactAudienceDenied = errors.New("delegation: consumer not in audience")
	ErrArtifactExpired        = errors.New("delegation: artifact expired")
	ErrArtifactDigestMismatch = errors.New("delegation: digest mismatch")
	ErrArtifactSizeExceeded   = errors.New("delegation: artifact exceeds max bytes")
	ErrArtifactInvalidPath    = errors.New("delegation: invalid artifact path")
	ErrArtifactSymlink        = errors.New("delegation: symlink rejected")
	ErrArtifactHardlink       = errors.New("delegation: hard link rejected")
	ErrArtifactWorkflowMismatch = errors.New("delegation: workflow mismatch")
)

// ---------------------------------------------------------------------------
// FileArtifactBroker — disk-backed implementation
// ---------------------------------------------------------------------------

// FileArtifactBroker stores artifacts on a local filesystem under a root.
type FileArtifactBroker struct {
	root string
	mu   sync.Mutex
}

// NewFileArtifactBroker creates a broker rooted at rootPath.
func NewFileArtifactBroker(rootPath string) (*FileArtifactBroker, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("%w: root path is empty", ErrArtifactInvalidPath)
	}
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("broker: resolve root: %w", err)
	}
	// Pre-create the transfer root.
	transferRoot := filepath.Join(abs, "artifacts", "transfer")
	if err := os.MkdirAll(transferRoot, 0o700); err != nil {
		return nil, fmt.Errorf("broker: mkdir transfer root: %w", err)
	}
	return &FileArtifactBroker{root: abs}, nil
}

// Commit writes the artifact and returns its ref.
func (b *FileArtifactBroker) Commit(ctx context.Context, req CommitReq) (TransferableArtifactRef, error) {
	if req.Reader == nil {
		return TransferableArtifactRef{}, fmt.Errorf("%w: reader is nil", ErrArtifactInvalidPath)
	}
	if req.MaxBytes <= 0 {
		return TransferableArtifactRef{}, fmt.Errorf("%w: MaxBytes must be positive", ErrArtifactSizeExceeded)
	}
	if err := validateArtifactRefPath(req.LogicalRef); err != nil {
		return TransferableArtifactRef{}, fmt.Errorf("%w: %v", ErrArtifactInvalidPath, err)
	}
	if len(req.Audience) == 0 {
		return TransferableArtifactRef{}, fmt.Errorf("%w: audience is empty", ErrArtifactAudienceDenied)
	}
	// Forbid wildcard "*" — exact logical IDs only.
	for _, a := range req.Audience {
		if a == "*" {
			return TransferableArtifactRef{}, fmt.Errorf("%w: wildcard audience forbidden", ErrArtifactAudienceDenied)
		}
		if a == "" {
			return TransferableArtifactRef{}, fmt.Errorf("%w: empty audience entry", ErrArtifactAudienceDenied)
		}
	}
	if req.WorkflowID == "" {
		return TransferableArtifactRef{}, fmt.Errorf("%w: workflow_id is empty", ErrArtifactWorkflowMismatch)
	}

	// Generate unique artifact ID.
	artifactID, err := generateArtifactID()
	if err != nil {
		return TransferableArtifactRef{}, fmt.Errorf("Commit: generate id: %w", err)
	}

	artifactDir := filepath.Join(b.root, "artifacts", "transfer", req.WorkflowID, artifactID)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Create artifact directory.
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return TransferableArtifactRef{}, fmt.Errorf("Commit: mkdir artifact dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(artifactDir) // best-effort cleanup on error
		}
	}()

	// Write blob with streaming hash.
	blobPath := filepath.Join(artifactDir, "blob")
	digest, byteSize, err := writeBlobWithHash(blobPath, req.Reader, req.MaxBytes)
	if err != nil {
		return TransferableArtifactRef{}, fmt.Errorf("Commit: write blob: %w", err)
	}

	// Verify blob is a regular file (no symlinks, no hardlinks).
	if err := verifyRegularFile(blobPath); err != nil {
		return TransferableArtifactRef{}, fmt.Errorf("Commit: verify blob: %w", err)
	}

	// Write meta.json atomically.
	ref := TransferableArtifactRef{
		ArtifactID:        artifactID,
		Digest:            digest,
		WorkflowID:        req.WorkflowID,
		ProducerRunID:     req.ProducerRunID,
		ProducerAttemptID: req.ProducerAttemptID,
		ProducerTaskID:    req.ProducerTaskID,
		MediaType:         req.MediaType,
		ByteSize:          byteSize,
		Classification:    req.Classification,
		Audience:          req.Audience,
		ExpiresAt:         req.ExpiresAt,
		LogicalRef:        req.LogicalRef,
	}

	metaPath := filepath.Join(artifactDir, "meta.json")
	if err := atomicWriteJSON(metaPath, ref, 0o600); err != nil {
		return TransferableArtifactRef{}, fmt.Errorf("Commit: write meta: %w", err)
	}

	cleanup = false // success — keep artifacts on disk
	return ref, nil
}

// AuthorizeRead checks authorization.
func (b *FileArtifactBroker) AuthorizeRead(ctx context.Context, artifactID, consumerLogicalID, workflowID string, now time.Time) error {
	ref, _, err := b.loadMeta(artifactID, workflowID)
	if err != nil {
		return err
	}
	if now.After(ref.ExpiresAt) {
		return fmt.Errorf("%w: artifact %s expired at %s", ErrArtifactExpired, artifactID, ref.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if ref.WorkflowID != workflowID {
		return fmt.Errorf("%w: expected %s, got %s", ErrArtifactWorkflowMismatch, workflowID, ref.WorkflowID)
	}
	if !stringInSlice(consumerLogicalID, ref.Audience) {
		return fmt.Errorf("%w: %s not in audience", ErrArtifactAudienceDenied, consumerLogicalID)
	}
	return nil
}

// ProjectReadOnly returns a read-only artifact projection.
func (b *FileArtifactBroker) ProjectReadOnly(ctx context.Context, artifactID, consumerLogicalID string, now time.Time) (ProjectedArtifact, error) {
	ref, artifactDir, err := b.loadMeta(artifactID, "")
	if err != nil {
		return ProjectedArtifact{}, err
	}

	// Re-verify with the correct workflow from meta.
	if err := b.AuthorizeRead(ctx, artifactID, consumerLogicalID, ref.WorkflowID, now); err != nil {
		return ProjectedArtifact{}, err
	}

	// Verify blob integrity.
	blobPath := filepath.Join(artifactDir, "blob")
	if err := verifyRegularFile(blobPath); err != nil {
		return ProjectedArtifact{}, err
	}
	actualDigest, err := hashFileAt(blobPath)
	if err != nil {
		return ProjectedArtifact{}, fmt.Errorf("ProjectReadOnly: hash blob: %w", err)
	}
	if actualDigest != ref.Digest {
		return ProjectedArtifact{}, fmt.Errorf("%w: expected %s, got %s", ErrArtifactDigestMismatch, ref.Digest, actualDigest)
	}

	// Make blob read-only.
	if err := os.Chmod(blobPath, 0o400); err != nil {
		return ProjectedArtifact{}, fmt.Errorf("ProjectReadOnly: chmod blob: %w", err)
	}

	return ProjectedArtifact{
		ArtifactID:   ref.ArtifactID,
		Digest:       ref.Digest,
		LogicalRef:   ref.LogicalRef,
		ReadOnlyRoot: artifactDir,
		ByteSize:     ref.ByteSize,
		MediaType:    ref.MediaType,
	}, nil
}

// VerifyDigest re-hashes the blob and compares to the stored digest.
func (b *FileArtifactBroker) VerifyDigest(ctx context.Context, artifactID string) error {
	ref, artifactDir, err := b.loadMeta(artifactID, "")
	if err != nil {
		return err
	}
	blobPath := filepath.Join(artifactDir, "blob")
	if err := verifyRegularFile(blobPath); err != nil {
		return err
	}
	actualDigest, err := hashFileAt(blobPath)
	if err != nil {
		return fmt.Errorf("VerifyDigest: hash blob: %w", err)
	}
	if actualDigest != ref.Digest {
		return fmt.Errorf("%w: expected %s, got %s", ErrArtifactDigestMismatch, ref.Digest, actualDigest)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// loadMeta reads meta.json for a given artifact and workflow.
// If workflowID is empty, searches under the artifacts/transfer/ tree.
func (b *FileArtifactBroker) loadMeta(artifactID, workflowID string) (TransferableArtifactRef, string, error) {
	var artifactDir string
	if workflowID != "" {
		artifactDir = filepath.Join(b.root, "artifacts", "transfer", workflowID, artifactID)
	} else {
		// Search for the artifact under all workflow dirs.
		var found string
		transferRoot := filepath.Join(b.root, "artifacts", "transfer")
		entries, err := os.ReadDir(transferRoot)
		if err != nil {
			return TransferableArtifactRef{}, "", fmt.Errorf("%w: read transfer root: %w", ErrArtifactNotFound, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			candidate := filepath.Join(transferRoot, entry.Name(), artifactID)
			if fi, err := os.Lstat(candidate); err == nil && fi.IsDir() {
				// Verify no symlink traversal.
				if fi.Mode()&os.ModeSymlink != 0 {
					return TransferableArtifactRef{}, "", fmt.Errorf("%w: artifact dir is symlink", ErrArtifactSymlink)
				}
				artifactDir = candidate
				found = entry.Name()
				_ = found // used for clarity
				break
			}
		}
		if artifactDir == "" {
			return TransferableArtifactRef{}, "", fmt.Errorf("%w: artifact %s", ErrArtifactNotFound, artifactID)
		}
	}

	// Verify artifactDir is a real directory, not a symlink.
	fi, err := os.Lstat(artifactDir)
	if err != nil {
		return TransferableArtifactRef{}, "", fmt.Errorf("%w: stat artifact dir: %w", ErrArtifactNotFound, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return TransferableArtifactRef{}, "", fmt.Errorf("%w: artifact dir is symlink", ErrArtifactSymlink)
	}
	if !fi.IsDir() {
		return TransferableArtifactRef{}, "", fmt.Errorf("%w: artifact path is not a directory", ErrArtifactNotFound)
	}

	metaPath := filepath.Join(artifactDir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return TransferableArtifactRef{}, "", fmt.Errorf("%w: read meta: %w", ErrArtifactNotFound, err)
	}
	var ref TransferableArtifactRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return TransferableArtifactRef{}, "", fmt.Errorf("%w: parse meta: %w", ErrArtifactNotFound, err)
	}
	return ref, artifactDir, nil
}

// writeBlobWithHash writes data from r to path, computing SHA-256 on the fly.
// Rejects if total bytes > maxBytes. The file is created with 0o600 and fsynced.
func writeBlobWithHash(path string, r io.Reader, maxBytes int64) (digest string, byteSize int64, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("open blob: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(path) // best-effort remove
		}
	}()

	h := sha256.New()
	var written int64

	// Read in bounded chunks.
	buf := make([]byte, 32*1024) // 32KB chunks
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if written+int64(n) > maxBytes {
				_ = f.Close() // best-effort close
				return "", 0, fmt.Errorf("%w: %d > %d", ErrArtifactSizeExceeded, written+int64(n), maxBytes)
			}
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				_ = f.Close() // best-effort close
				return "", 0, fmt.Errorf("write blob: %w", writeErr)
			}
			if _, hashErr := h.Write(buf[:n]); hashErr != nil {
				_ = f.Close() // best-effort close
				return "", 0, fmt.Errorf("hash blob: %w", hashErr)
			}
			written += int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = f.Close() // best-effort close
			return "", 0, fmt.Errorf("read source: %w", readErr)
		}
	}

	// fsync file.
	if err := f.Sync(); err != nil {
		_ = f.Close() // best-effort close
		return "", 0, fmt.Errorf("fsync blob: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", 0, fmt.Errorf("close blob: %w", err)
	}
	cleanup = false

	// fsync parent directory.
	dir := filepath.Dir(path)
	if err := fsyncDir(dir); err != nil {
		return "", 0, fmt.Errorf("fsync blob dir: %w", err)
	}

	digest = hex.EncodeToString(h.Sum(nil))
	return digest, written, nil
}

// atomicWriteJSON writes v as JSON to path atomically.
func atomicWriteJSON(path string, v interface{}, mode os.FileMode) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return atomicWriteFile(path, data, mode)
}

// atomicWriteFile writes data to path using temp file, fsync, rename, parent fsync.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Reject if path is a symlink.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrArtifactSymlink, path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath) // best-effort remove
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // best-effort close
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close() // best-effort close
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	cleanup = false

	// fsync parent directory for durability.
	if err := fsyncDir(dir); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}

// fsyncDir fsyncs the directory at dir for durability.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir for fsync: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}

// verifyRegularFile checks that path is a regular file, not a symlink or hardlink.
func verifyRegularFile(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: stat: %w", ErrArtifactNotFound, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", ErrArtifactSymlink, path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrArtifactInvalidPath, path)
	}
	// Reject hard links (Nlink > 1).
	if sysStat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if sysStat.Nlink > 1 {
			return fmt.Errorf("%w: %s has nlink=%d", ErrArtifactHardlink, path, sysStat.Nlink)
		}
	}
	return nil
}

// hashFileAt computes SHA-256 of the file at path.
// Uses the open fd for post-stat to avoid TOCTOU.
func hashFileAt(path string) (string, error) {
	preStat, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	// TOCTOU defense: verify file didn't change during hashing.
	postStat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("fstat: %w", err)
	}
	if preStat.Size() != postStat.Size() || !preStat.ModTime().Equal(postStat.ModTime()) {
		return "", fmt.Errorf("%w: file changed during hashing", ErrArtifactDigestMismatch)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// generateArtifactID generates a cryptographically random artifact ID.
func generateArtifactID() (string, error) {
	// Reuse the ID generation pattern from ids.go.
	return generateID("artf-")
}

// stringInSlice returns true if s is in ss.
func stringInSlice(s string, ss []string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// Compile-time check: FileArtifactBroker implements ArtifactBroker.
var _ ArtifactBroker = (*FileArtifactBroker)(nil)
