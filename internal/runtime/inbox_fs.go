package runtime

// Filesystem helpers for the durable inbox store. These mirror the
// trigger package's event-store fs helpers (symlink rejection, protected
// mkdir, strict read, safe ID hashing) but are scoped to the runtime package
// to keep it self-contained and avoid an import cycle (runtime must not
// import trigger).

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// inboxRejectSymlinkPath ensures path (if it exists) is not a symlink and that
// its immediate parent is not a symlink.
func inboxRejectSymlinkPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrInboxInvalidPath)
	}
	cleaned := filepath.Clean(path)
	if err := inboxRejectSymlinkLeaf(cleaned); err != nil {
		return fmt.Errorf("inbox reject symlink path: %w", err)
	}
	parent := filepath.Dir(cleaned)
	if parent != cleaned {
		if err := inboxRejectSymlinkLeaf(parent); err != nil {
			return fmt.Errorf("inbox reject symlink path: %w", err)
		}
	}
	return nil
}

// inboxRejectSymlinkLeaf fails if path exists and is a symlink.
func inboxRejectSymlinkLeaf(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inbox reject symlink leaf: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrInboxSymlink, path)
	}
	return nil
}

// inboxMkdirProtected creates dir with 0700 and rejects symlinks, mirroring
// trigger.eventMkdirProtected. It re-checks permissions after creation
// (TOCTOU).
func inboxMkdirProtected(dir string) error {
	if dir == "" {
		return fmt.Errorf("%w: empty dir", ErrInboxInvalidPath)
	}
	if err := inboxRejectSymlinkPath(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		if !errors.Is(err, ErrInboxSymlink) {
			if _, e := os.Lstat(dir); e != nil && !errors.Is(e, os.ErrNotExist) {
				return err
			}
		} else {
			return err
		}
	}
	if err := os.MkdirAll(dir, inboxDirPerm); err != nil {
		return fmt.Errorf("runtime: mkdir %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inbox mkdir protected: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrInboxSymlink, dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("runtime: not a directory: %s", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, inboxDirPerm); err != nil {
			return fmt.Errorf("%w: %s mode %#o", ErrInboxUnsafePerm, dir, fi.Mode().Perm())
		}
		fi, err = os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("inbox mkdir protected: %w", err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%w: %s mode %#o", ErrInboxUnsafePerm, dir, fi.Mode().Perm())
		}
	}
	return nil
}

// inboxReadFileStrict reads a file after symlink and permission checks. It
// caps the read at maxBytes to prevent OOM during recovery.
func inboxReadFileStrict(path string, maxBytes int64) ([]byte, error) {
	if err := inboxRejectSymlinkPath(path); err != nil {
		return nil, fmt.Errorf("inbox read file strict: %w", err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inbox read file strict: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrInboxSymlink, path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("runtime: not a regular file: %s", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s mode %#o", ErrInboxUnsafePerm, path, fi.Mode().Perm())
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %s size %d", ErrInboxTooLarge, path, fi.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("inbox read file strict: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("inbox read file strict: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %s", ErrInboxTooLarge, path)
	}
	return data, nil
}

// inboxSafeID sanitizes an ID for use as a single path component. Rejects
// empty, traversal, separators, and control characters by hashing the input
// into a stable, safe component. Mirrors trigger.eventSafeID.
func inboxSafeID(id string) string {
	if id == "" || id == "." || id == ".." {
		return "_invalid"
	}
	if strings.ContainsAny(id, `/\`) {
		sum := sha256.Sum256([]byte(id))
		return "h-" + hex.EncodeToString(sum[:16])
	}
	for _, r := range id {
		if r < 32 || r == 127 || !unicode.IsPrint(r) {
			sum := sha256.Sum256([]byte(id))
			return "h-" + hex.EncodeToString(sum[:16])
		}
	}
	if len(id) > 200 {
		sum := sha256.Sum256([]byte(id))
		return "h-" + hex.EncodeToString(sum[:16])
	}
	return id
}

// newBufioWriter is a thin wrapper so the inbox WAL rewriter can use a
// bufio.Writer without the inbox.go file importing bufio directly (keeps
// the import surface narrow). Not strictly necessary but keeps inbox.go's
// imports stable across edits.
func newBufioWriter(w *os.File) *bufio.Writer { return bufio.NewWriter(w) }
