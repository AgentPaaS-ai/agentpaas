package routedrun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/fsutil"
)

// Store errors (sentinel).
var (
	ErrNotFound              = errors.New("routedrun: not found")
	ErrAlreadyExists         = errors.New("routedrun: already exists")
	ErrCASConflict           = errors.New("routedrun: generation conflict")
	ErrIdempotencyConflict   = errors.New("routedrun: idempotency conflict")
	ErrAlreadyRunning        = errors.New("routedrun: already running")
	ErrDeploymentInactive    = errors.New("routedrun: deployment inactive")
	ErrSymlinkRejected       = errors.New("routedrun: symlink rejected")
	ErrUnsafePermissions     = errors.New("routedrun: unsafe permissions")
	ErrSizeCapExceeded       = errors.New("routedrun: size cap exceeded")
	ErrInvalidPath           = errors.New("routedrun: invalid path component")
	ErrUnknownSchemaVersion  = errors.New("routedrun: unknown or unsupported schema version")
	ErrInvalidArgument       = errors.New("routedrun: invalid argument")
	ErrLeaseCallerSelected   = errors.New("routedrun: caller-selected lease id rejected")
	ErrJournalSequenceConflict = errors.New("routedrun: journal sequence conflict")
)

const (
	dirPerm  = os.FileMode(0o700)
	filePerm = os.FileMode(0o600)

	maxStateFileBytes  = 4 << 20 // 4 MiB
	maxLedgerLineBytes = 64 << 10
	maxStringBytes     = 64 << 10
	maxRecordsPerList  = 100_000
	maxInputJSONBytes  = 1 << 20
)

// persisted wraps a record with store-level generation for CAS when the
// domain type itself has no Generation field.
type persisted struct {
	SchemaVersion string          `json:"schema_version"`
	Generation    int64           `json:"generation"`
	Record        json.RawMessage `json:"record"`
}

func marshalPersisted(gen int64, v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	env := persisted{
		SchemaVersion: CurrentSchemaVersion,
		Generation:    gen,
		Record:        body,
	}
	return json.Marshal(env)
}

func unmarshalPersisted(data []byte, v any) (int64, error) {
	var env persisted
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, err
	}
	if env.SchemaVersion == "" {
		return 0, fmt.Errorf("routedrun: missing schema_version in envelope")
	}
	if env.SchemaVersion != CurrentSchemaVersion {
		// Callers may migrate first; fail closed here.
		if env.SchemaVersion > CurrentSchemaVersion || !isKnownSchema(env.SchemaVersion) {
			return 0, fmt.Errorf("%w: %s", ErrUnknownSchemaVersion, env.SchemaVersion)
		}
	}
	if err := json.Unmarshal(env.Record, v); err != nil {
		return 0, err
	}
	return env.Generation, nil
}

func isKnownSchema(v string) bool {
	return v == CurrentSchemaVersion
}

// mkdirProtected creates dir with 0700 and rejects symlinks.
func mkdirProtected(dir string) error {
	if err := rejectSymlinkPath(dir); err != nil && !os.IsNotExist(err) {
		// rejectSymlinkPath returns ErrSymlinkRejected or path errors.
		// For non-existent intermediate components, continue.
		if !errors.Is(err, ErrSymlinkRejected) {
			// Check parent chain; if leaf does not exist that's fine.
			if _, e := os.Lstat(dir); e != nil && !os.IsNotExist(e) {
				return err
			}
		} else {
			return err
		}
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("routedrun: mkdir %s: %w", dir, err)
	}
	// Re-check after create (TOCTOU): ensure not a symlink and mode is tight.
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlinkRejected, dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("routedrun: not a directory: %s", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		// Too permissive — fail closed after attempting fix.
		if err := os.Chmod(dir, dirPerm); err != nil {
			return fmt.Errorf("%w: %s mode %#o", ErrUnsafePermissions, dir, fi.Mode().Perm())
		}
		fi, err = os.Lstat(dir)
		if err != nil {
			return err
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%w: %s mode %#o", ErrUnsafePermissions, dir, fi.Mode().Perm())
		}
	}
	return nil
}

// atomicWriteFile writes data to path using temp file, fsync, rename, parent fsync.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if int64(len(data)) > maxStateFileBytes {
		return fmt.Errorf("%w: %d bytes", ErrSizeCapExceeded, len(data))
	}
	dir := filepath.Dir(path)
	if err := mkdirProtected(dir); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	// Refuse to replace a symlink path.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkRejected, path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("routedrun: create temp: %w", err)
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
		return fmt.Errorf("routedrun: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close() // best-effort close
		return fmt.Errorf("routedrun: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("routedrun: close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("routedrun: chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("routedrun: rename: %w", err)
	}
	cleanup = false
	if err := fsyncDir(dir); err != nil {
		return err
	}
	return nil
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("routedrun: open dir for fsync: %w", err)
	}
	defer func() { _ = d.Close() }() // best-effort close
	if err := d.Sync(); err != nil {
		return fmt.Errorf("routedrun: fsync dir: %w", err)
	}
	return nil
}

// readFileStrict reads a file after symlink and permission checks.
func readFileStrict(path string, maxBytes int64) ([]byte, error) {
	if err := rejectSymlinkPath(path); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrSymlinkRejected, path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("routedrun: not a regular file: %s", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s mode %#o", ErrUnsafePermissions, path, fi.Mode().Perm())
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %s size %d", ErrSizeCapExceeded, path, fi.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // best-effort close
	// Cap read.
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %s", ErrSizeCapExceeded, path)
	}
	return data, nil
}

// rejectSymlinkPath ensures path (if it exists) is not a symlink, and that its
// immediate parent directory (if it exists) is not a symlink. It does not walk
// all the way to the filesystem root: on macOS /var is a symlink to
// /private/var, which is a normal system layout and must not fail closed.
//
// Callers that manage a store root should additionally use rejectSymlinkInRoot
// so every component under the store root is checked.
func rejectSymlinkPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	err := fsutil.RejectSymlinkPathAndParent(path)
	if err == nil {
		return nil
	}
	var se *fsutil.SymlinkError
	if errors.As(err, &se) {
		return fmt.Errorf("%w: %s", ErrSymlinkRejected, se.Path)
	}
	return err
}

// rejectSymlinkInRoot walks every path component from root to path (inclusive)
// and fails if any is a symlink. path must be under root.
func rejectSymlinkInRoot(root, path string) error {
	err := fsutil.RejectSymlinkInRoot(root, path)
	if err == nil {
		return nil
	}
	var pe *fsutil.PathEscapesError
	if errors.As(err, &pe) {
		return fmt.Errorf("%w: path %s escapes root %s", ErrInvalidPath, pe.Path, pe.Root)
	}
	var se *fsutil.SymlinkError
	if errors.As(err, &se) {
		return fmt.Errorf("%w: %s", ErrSymlinkRejected, se.Path)
	}
	return err
}

// safeID sanitizes an ID for use as a single path component.
// Rejects empty, traversal, separators, and control characters.
func safeID(id string) string {
	return fsutil.SafeID(id)
}

// escapeAlias converts an alias string into a safe path component.
func escapeAlias(alias string) string {
	if alias == "" {
		return "_empty"
	}
	// Percent-encode path-significant characters without using path packages that follow links.
	var b strings.Builder
	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == '/':
			b.WriteString("__")
		default:
			_, _ = fmt.Fprintf(&b, "_%04x", r) // best-effort write
		}
	}
	s := b.String()
	if s == "." || s == ".." {
		return "_dot"
	}
	if len(s) > 200 {
		sum := sha256.Sum256([]byte(alias))
		return "a-" + hex.EncodeToString(sum[:16])
	}
	return s
}

// idempotencyPathKey hashes caller+key for path safety.
func idempotencyPathKey(caller, key string) string {
	sum := sha256.Sum256([]byte(caller + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

func appendJSONL(path string, line []byte) error {
	if len(line) > maxLedgerLineBytes {
		return fmt.Errorf("%w: line length %d", ErrSizeCapExceeded, len(line))
	}
	if err := rejectSymlinkPath(path); err != nil && !os.IsNotExist(err) {
		if errors.Is(err, ErrSymlinkRejected) {
			return err
		}
	}
	dir := filepath.Dir(path)
	if err := mkdirProtected(dir); err != nil {
		return err
	}
	// Append with O_APPEND; fsync after write. Not fully atomic multi-line,
	// but each line is durable after Sync.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // best-effort close
	// Enforce permissions on open.
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		_ = f.Close() // best-effort close
		return fmt.Errorf("%w: %s mode %#o", ErrUnsafePermissions, path, fi.Mode().Perm())
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

// cleanupOrphanTemps removes orphaned .tmp-* files under root (incomplete writes).
func cleanupOrphanTemps(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkRejected, path)
		}
		if !info.IsDir() && strings.HasPrefix(info.Name(), ".tmp-") {
			_ = os.Remove(path) // best-effort remove
		}
		return nil
	})
}

// checkStringCap fails if s exceeds maxStringBytes.
func checkStringCap(field, s string) error {
	if len(s) > maxStringBytes {
		return fmt.Errorf("%w: field %s length %d", ErrSizeCapExceeded, field, len(s))
	}
	return nil
}
