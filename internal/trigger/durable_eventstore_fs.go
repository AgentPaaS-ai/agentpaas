package trigger

import (
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

// eventRejectSymlinkPath ensures path (if it exists) is not a symlink and that
// its immediate parent is not a symlink. It mirrors routedrun.rejectSymlinkPath
// but is scoped to the event store to keep the trigger package self-contained.
func eventRejectSymlinkPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrEventStoreInvalidPath)
	}
	cleaned := filepath.Clean(path)
	if err := eventRejectSymlinkLeaf(cleaned); err != nil {
		return err
	}
	parent := filepath.Dir(cleaned)
	if parent != cleaned {
		if err := eventRejectSymlinkLeaf(parent); err != nil {
			return err
		}
	}
	return nil
}

// eventRejectSymlinkLeaf fails if path exists and is a symlink.
func eventRejectSymlinkLeaf(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrEventStoreSymlink, path)
	}
	return nil
}

// eventMkdirProtected creates dir with 0700 and rejects symlinks, mirroring
// routedrun.mkdirProtected. It re-checks permissions after creation (TOCTOU).
func eventMkdirProtected(dir string) error {
	if dir == "" {
		return fmt.Errorf("%w: empty dir", ErrEventStoreInvalidPath)
	}
	if err := eventRejectSymlinkPath(dir); err != nil && !os.IsNotExist(err) {
		if !errors.Is(err, ErrEventStoreSymlink) {
			if _, e := os.Lstat(dir); e != nil && !os.IsNotExist(e) {
				return err
			}
		} else {
			return err
		}
	}
	if err := os.MkdirAll(dir, eventDirPerm); err != nil {
		return fmt.Errorf("trigger: mkdir %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrEventStoreSymlink, dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("trigger: not a directory: %s", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, eventDirPerm); err != nil {
			return fmt.Errorf("%w: %s mode %#o", ErrEventStoreUnsafePerm, dir, fi.Mode().Perm())
		}
		fi, err = os.Lstat(dir)
		if err != nil {
			return err
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%w: %s mode %#o", ErrEventStoreUnsafePerm, dir, fi.Mode().Perm())
		}
	}
	return nil
}

// eventReadFileStrict reads a file after symlink and permission checks. It
// caps the read at maxBytes to prevent OOM during recovery.
func eventReadFileStrict(path string, maxBytes int64) ([]byte, error) {
	if err := eventRejectSymlinkPath(path); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrEventStoreSymlink, path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("trigger: not a regular file: %s", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s mode %#o", ErrEventStoreUnsafePerm, path, fi.Mode().Perm())
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %s size %d", ErrEventStoreTooLarge, path, fi.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %s", ErrEventStoreTooLarge, path)
	}
	return data, nil
}

// eventSafeID sanitizes an ID for use as a single path component. Rejects
// empty, traversal, separators, and control characters by hashing the input
// into a stable, safe component. Mirrors routedrun.safeID.
func eventSafeID(id string) string {
	if id == "" || id == "." || id == ".." {
		return "_invalid"
	}
	if strings.ContainsAny(id, `/\\`) {
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
