package routedrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// Artifact workspace bounds (B27 spec)
// ---------------------------------------------------------------------------

const (
	ArtifactMaxPerFile   = int64(25 * 1024 * 1024) // 25 MiB
	ArtifactMaxTotal    = int64(100 * 1024 * 1024) // 100 MiB
	ArtifactMaxPathLen  = 512
	ArtifactMaxSegments = 8
	ArtifactDirEnvVar   = "AGENTPAAS_ARTIFACT_DIR"
	ArtifactMountPath   = "/workspace/artifacts"
)

var artifactSegRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ArtifactMetadata records the durable metadata for an accepted artifact.
type ArtifactMetadata struct {
	SchemaVersion    string `json:"schema_version"`
	ArtifactID       ArtifactID `json:"artifact_id"`
	RunID            RunID      `json:"run_id"`
	AttemptID        AttemptID  `json:"attempt_id"`

	// Relative path beneath the artifact root (POSIX).
	RelativePath string `json:"relative_path"`

	// Byte size.
	ByteSize int64 `json:"byte_size"`

	// SHA-256 hex digest.
	Digest string `json:"digest"`

	// Media type (best-effort, from file extension).
	MediaType string `json:"media_type,omitempty"`

	// Creating attempt.
	CreatingAttempt AttemptID `json:"creating_attempt"`

	// Last update timestamp.
	UpdatedAt time.Time `json:"updated_at"`
}

// ArtifactWorkspace manages the bounded artifact directory for a run.
type ArtifactWorkspace struct {
	root      string // host path: ~/.agentpaas/state/runs/<run_id>/artifacts/
	runID     RunID
	totalSize int64
	mu        struct {
		sync.Mutex
		metadata map[string]*ArtifactMetadata
	}
}

// NewArtifactWorkspace creates an artifact workspace manager for a run.
// The root is typically ~/.agentpaas/state/runs/<run_id>/artifacts/.
func NewArtifactWorkspace(root string, runID RunID) (*ArtifactWorkspace, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: empty artifact root", ErrInvalidArgument)
	}
	if err := mkdirProtected(root); err != nil {
		return nil, fmt.Errorf("create artifact dir: %w", err)
	}
	aw := &ArtifactWorkspace{
		root:  root,
		runID: runID,
	}
	aw.mu.metadata = make(map[string]*ArtifactMetadata)
	return aw, nil
}

// ValidateAndAccept validates, hashes, and accepts an artifact reference.
// The file must exist beneath the artifact root (no symlinks, no traversal).
// Returns the artifact metadata on success.
func (aw *ArtifactWorkspace) ValidateAndAccept(
	ctx context.Context,
	relPath string,
	attemptID AttemptID,
) (*ArtifactMetadata, error) {
	if err := validateArtifactRelPath(relPath); err != nil {
		return nil, err
	}

	// Resolve absolute path beneath root.
	absPath := filepath.Join(aw.root, filepath.Clean("/"+relPath))

	// Verify the resolved path is still beneath root.
	if !isBeneath(absPath, aw.root) {
		return nil, fmt.Errorf("%w: path escapes artifact root", ErrInvalidPath)
	}

	// Reject symlinks.
	info, err := os.Lstat(absPath)
	if err != nil {
		return nil, fmt.Errorf("%w: stat artifact: %v", ErrNotFound, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: symlink rejected", ErrSymlinkRejected)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrInvalidPath)
	}
	// Reject hard links (spec: "No symlinks, devices, sockets, FIFOs, absolute
	// paths, traversal, or hard links"). A hard link has Nlink > 1 — the file
	// is reachable from outside the artifact root, bypassing containment.
	if sysStat, ok := info.Sys().(*syscall.Stat_t); ok {
		if sysStat.Nlink > 1 {
			return nil, fmt.Errorf("%w: hard link rejected (nlink=%d)", ErrInvalidPath, sysStat.Nlink)
		}
	}

	// Per-file size limit.
	if info.Size() > ArtifactMaxPerFile {
		return nil, fmt.Errorf("%w: artifact %s is %d bytes (max %d)",
			ErrSizeCapExceeded, relPath, info.Size(), ArtifactMaxPerFile)
	}

	// Total size limit.
	aw.mu.Lock()
	defer aw.mu.Unlock()
	if aw.totalSize+info.Size() > ArtifactMaxTotal {
		return nil, fmt.Errorf("%w: total artifact size would exceed %d",
			ErrSizeCapExceeded, ArtifactMaxTotal)
	}

	// Hash the file.
	digest, err := hashFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("hash artifact: %w", err)
	}

	// Detect media type from extension.
	mediaType := detectMediaType(relPath)

	meta := &ArtifactMetadata{
		SchemaVersion:   CurrentSchemaVersion,
		ArtifactID:      ArtifactID(digest[:16]), // first 16 hex chars as ID
		RunID:           aw.runID,
		AttemptID:       attemptID,
		RelativePath:    relPath,
		ByteSize:        info.Size(),
		Digest:          digest,
		MediaType:       mediaType,
		CreatingAttempt: attemptID,
		UpdatedAt:       time.Now().UTC(),
	}

	aw.mu.metadata[relPath] = meta
	aw.totalSize += info.Size()

	return meta, nil
}

// GetMetadata returns the metadata for an accepted artifact.
func (aw *ArtifactWorkspace) GetMetadata(relPath string) (*ArtifactMetadata, error) {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	meta, ok := aw.mu.metadata[relPath]
	if !ok {
		return nil, fmt.Errorf("%w: artifact %s", ErrNotFound, relPath)
	}
	return meta, nil
}

// ListMetadata returns all accepted artifact metadata.
func (aw *ArtifactWorkspace) ListMetadata() []*ArtifactMetadata {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	out := make([]*ArtifactMetadata, 0, len(aw.mu.metadata))
	for _, m := range aw.mu.metadata {
		out = append(out, m)
	}
	return out
}

// TotalSize returns the total bytes of accepted artifacts.
func (aw *ArtifactWorkspace) TotalSize() int64 {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	return aw.totalSize
}

// Root returns the host filesystem root for the artifact directory.
func (aw *ArtifactWorkspace) Root() string {
	return aw.root
}

// RemoveUnreferenced removes files in the artifact dir that were never accepted
// into durable metadata. Called during fencing/finalization.
func (aw *ArtifactWorkspace) RemoveUnreferenced() error {
	aw.mu.Lock()
	accepted := make(map[string]bool, len(aw.mu.metadata))
	for k := range aw.mu.metadata {
		accepted[k] = true
	}
	aw.mu.Unlock()

	return filepath.Walk(aw.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(path)
			return nil
		}
		rel, err := filepath.Rel(aw.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !accepted[rel] {
			_ = os.Remove(path)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Path validation
// ---------------------------------------------------------------------------

// validateArtifactRelPath lexically validates a relative artifact path.
func validateArtifactRelPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: artifact path is empty", ErrInvalidPath)
	}
	if len(path) > ArtifactMaxPathLen {
		return fmt.Errorf("%w: path exceeds %d chars", ErrInvalidPath, ArtifactMaxPathLen)
	}
	if strings.Contains(path, "\\") {
		return fmt.Errorf("%w: backslash in path", ErrInvalidPath)
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: absolute path rejected", ErrInvalidPath)
	}
	segments := strings.Split(path, "/")
	if len(segments) > ArtifactMaxSegments {
		return fmt.Errorf("%w: exceeds %d segments", ErrInvalidPath, ArtifactMaxSegments)
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: empty or dot segment", ErrInvalidPath)
		}
		if !artifactSegRe.MatchString(seg) {
			return fmt.Errorf("%w: invalid segment %q", ErrInvalidPath, seg)
		}
	}
	return nil
}

// isBeneath checks if child is beneath parent (lexical check after cleaning).
func isBeneath(child, parent string) bool {
	cleanParent := filepath.Clean(parent)
	cleanChild := filepath.Clean(child)
	if cleanChild == cleanParent {
		return true
	}
	return strings.HasPrefix(cleanChild, cleanParent+string(os.PathSeparator))
}

// hashFile computes SHA-256 of a file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// detectMediaType returns a best-effort media type from the file extension.
func detectMediaType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "application/json"
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
