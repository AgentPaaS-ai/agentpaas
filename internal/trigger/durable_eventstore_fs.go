package trigger

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/AgentPaaS-ai/agentpaas/internal/fsutil"
)

// eventRejectSymlinkPath ensures path (if it exists) is not a symlink and that
// its immediate parent is not a symlink. It mirrors routedrun.rejectSymlinkPath
// but is scoped to the event store to keep package-local sentinel errors.
func eventRejectSymlinkPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrEventStoreInvalidPath)
	}
	err := fsutil.RejectSymlinkPathAndParent(path)
	if err == nil {
		return nil
	}
	var se *fsutil.SymlinkError
	if errors.As(err, &se) {
		return fmt.Errorf("%w: %s", ErrEventStoreSymlink, se.Path)
	}
	return err
}

// eventRejectSymlinkLeaf fails if path exists and is a symlink.
func eventRejectSymlinkLeaf(path string) error {
	err := fsutil.RejectSymlinkLeaf(path)
	if err == nil {
		return nil
	}
	var se *fsutil.SymlinkError
	if errors.As(err, &se) {
		return fmt.Errorf("%w: %s", ErrEventStoreSymlink, se.Path)
	}
	return err
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
	defer func() { _ = f.Close() }() // best-effort close
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
	return fsutil.SafeID(id)
}
