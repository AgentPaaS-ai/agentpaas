package cloudclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// InvokeTokenStore persists deployment-scoped invoke tokens.
type InvokeTokenStore interface {
	Set(ctx context.Context, deploymentID, token string) error
	Get(ctx context.Context, deploymentID string) (string, error)
	Delete(ctx context.Context, deploymentID string) error
}

// ErrInvokeTokenNotFound indicates that no invoke token is stored for a
// deployment.
var ErrInvokeTokenNotFound = errors.New("invoke token not found")

// FileInvokeTokenStore stores deployment invoke tokens in a mode-0600 JSON
// file. The adjacent lock file serializes independent CLI processes.
type FileInvokeTokenStore struct {
	path string
	mu   sync.Mutex
}

// NewFileInvokeTokenStore creates a file-backed invoke-token store. The path
// must be absolute and must not point into a system directory.
func NewFileInvokeTokenStore(path string) (*FileInvokeTokenStore, error) {
	if err := validateInvokeTokenStorePath(path); err != nil {
		return nil, err
	}
	return &FileInvokeTokenStore{path: filepath.Clean(path)}, nil
}

// Set stores or rotates the token for a deployment.
func (s *FileInvokeTokenStore) Set(ctx context.Context, deploymentID, token string) error {
	if err := validateInvokeDeploymentID("invoke token store set", deploymentID); err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n\x00") {
		return errors.New("invoke token store set: token must be a non-empty single-line value")
	}

	return s.withLock(ctx, func() error {
		tokens, err := s.readUnlocked()
		if err != nil {
			return fmt.Errorf("invoke token store set: %w", err)
		}
		tokens[deploymentID] = token
		if err := s.writeUnlocked(tokens); err != nil {
			return fmt.Errorf("invoke token store set: %w", err)
		}
		return nil
	})
}

// Get returns the token for a deployment.
func (s *FileInvokeTokenStore) Get(ctx context.Context, deploymentID string) (string, error) {
	if err := validateInvokeDeploymentID("invoke token store get", deploymentID); err != nil {
		return "", err
	}

	var token string
	err := s.withLock(ctx, func() error {
		tokens, err := s.readUnlocked()
		if err != nil {
			return fmt.Errorf("invoke token store get: %w", err)
		}
		var ok bool
		token, ok = tokens[deploymentID]
		if !ok || token == "" {
			return fmt.Errorf("%w for deployment %q", ErrInvokeTokenNotFound, deploymentID)
		}
		return nil
	})
	return token, err
}

// Delete removes a deployment token. It is idempotent.
func (s *FileInvokeTokenStore) Delete(ctx context.Context, deploymentID string) error {
	if err := validateInvokeDeploymentID("invoke token store delete", deploymentID); err != nil {
		return err
	}

	return s.withLock(ctx, func() error {
		tokens, err := s.readUnlocked()
		if err != nil {
			return fmt.Errorf("invoke token store delete: %w", err)
		}
		if _, ok := tokens[deploymentID]; !ok {
			return nil
		}
		delete(tokens, deploymentID)
		if len(tokens) == 0 {
			return removeInvokeTokenFile(s.path)
		}
		if err := s.writeUnlocked(tokens); err != nil {
			return fmt.Errorf("invoke token store delete: %w", err)
		}
		return nil
	})
}

func (s *FileInvokeTokenStore) withLock(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureInvokeTokenParent(s.path); err != nil {
		return err
	}
	if err := checkInvokeTokenFile(s.path); err != nil {
		return err
	}

	lockPath := s.path + ".lock"
	if err := checkInvokeTokenFile(lockPath); err != nil {
		return fmt.Errorf("invoke token store lock: %w", err)
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("invoke token store lock: open lock file: %w", err)
	}
	defer func() { _ = lockFile.Close() }()
	if err := lockFile.Chmod(0o600); err != nil {
		return fmt.Errorf("invoke token store lock: set lock permissions: %w", err)
	}
	if err := acquireInvokeTokenLock(ctx, lockFile); err != nil {
		return err
	}
	defer func() { _ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN) }()

	// File lock is acquired before the in-process mutex. Keep this order in
	// every public method to avoid cross-process and in-process deadlocks.
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

func acquireInvokeTokenLock(ctx context.Context, file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			return fmt.Errorf("invoke token store lock: acquire lock: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *FileInvokeTokenStore) readUnlocked() (map[string]string, error) {
	if err := checkInvokeTokenFile(s.path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if tokens == nil {
		tokens = make(map[string]string)
	}
	return tokens, nil
}

func (s *FileInvokeTokenStore) writeUnlocked(tokens map[string]string) error {
	if err := ensureInvokeTokenParent(s.path); err != nil {
		return err
	}
	if err := checkInvokeTokenFile(s.path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token file: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	tmpFile, err := os.CreateTemp(dir, ".invoke-tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary token file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("set temporary token file permissions: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temporary token file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temporary token file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary token file: %w", err)
	}

	if err := checkInvokeTokenFile(s.path); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace token file: %w", err)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open token directory: %w", err)
	}
	if err := dirFile.Sync(); err != nil {
		_ = dirFile.Close()
		return fmt.Errorf("sync token directory: %w", err)
	}
	if err := dirFile.Close(); err != nil {
		return fmt.Errorf("close token directory: %w", err)
	}
	return nil
}

func removeInvokeTokenFile(path string) error {
	if err := checkInvokeTokenFile(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}

func validateInvokeTokenStorePath(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("invoke token store path must be absolute")
	}
	if strings.ContainsAny(path, "\r\n\x00") {
		return fmt.Errorf("invoke token store path contains a control character")
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == ".." {
			return fmt.Errorf("invoke token store path must not contain '..'")
		}
	}
	clean := filepath.Clean(path)
	for _, systemDir := range []string{"/etc", "/usr", "/bin", "/sbin", "/dev", "/proc", "/sys", "/root", "/lib", "/lib64"} {
		if clean == systemDir || strings.HasPrefix(clean, systemDir+string(filepath.Separator)) {
			return fmt.Errorf("invoke token store path is in a system directory")
		}
	}
	if clean == "/var" || strings.HasPrefix(clean, "/var"+string(filepath.Separator)) {
		if !strings.HasPrefix(clean, "/var/folders"+string(filepath.Separator)) && clean != "/var/folders" && !strings.HasPrefix(clean, "/private/var/folders"+string(filepath.Separator)) && clean != "/private/var/folders" {
			return fmt.Errorf("invoke token store path is in a system directory")
		}
	}
	return nil
}

func ensureInvokeTokenParent(path string) error {
	dir := filepath.Dir(path)
	cleanDir := filepath.Clean(dir)
	parts := strings.Split(filepath.ToSlash(cleanDir), "/")
	current := string(filepath.Separator)
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("invoke token store: create directory %s: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("invoke token store: inspect directory %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if !isAllowedInvokeTokenSystemSymlink(current) {
				return fmt.Errorf("invoke token store: refusing symlink parent %s", current)
			}
			resolvedInfo, statErr := os.Stat(current)
			if statErr != nil {
				return fmt.Errorf("invoke token store: inspect symlink target %s: %w", current, statErr)
			}
			info = resolvedInfo
		}
		if !info.IsDir() {
			return fmt.Errorf("invoke token store: parent %s is not a directory", current)
		}
	}
	return nil
}

func checkInvokeTokenFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("invoke token store: inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invoke token store: refusing symlink %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invoke token store: %s is not a regular file", path)
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("invoke token store: set permissions on %s: %w", path, err)
		}
	}
	return nil
}

func isAllowedInvokeTokenSystemSymlink(path string) bool {
	return runtime.GOOS == "darwin" && (path == "/var" || path == "/tmp")
}

// FakeInvokeTokenStore is an in-memory InvokeTokenStore for tests.
type FakeInvokeTokenStore struct {
	mu     sync.Mutex
	tokens map[string]string
}

// NewFakeInvokeTokenStore creates an empty in-memory invoke-token store.
func NewFakeInvokeTokenStore() *FakeInvokeTokenStore {
	return &FakeInvokeTokenStore{tokens: make(map[string]string)}
}

// Set stores or rotates a token in memory.
func (f *FakeInvokeTokenStore) Set(_ context.Context, deploymentID, token string) error {
	if err := validateInvokeDeploymentID("fake invoke token store set", deploymentID); err != nil {
		return err
	}
	if token == "" {
		return errors.New("fake invoke token store set: token must not be empty")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[deploymentID] = token
	return nil
}

// Get returns an in-memory token.
func (f *FakeInvokeTokenStore) Get(_ context.Context, deploymentID string) (string, error) {
	if err := validateInvokeDeploymentID("fake invoke token store get", deploymentID); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	token, ok := f.tokens[deploymentID]
	if !ok {
		return "", fmt.Errorf("%w for deployment %q", ErrInvokeTokenNotFound, deploymentID)
	}
	return token, nil
}

// Delete removes an in-memory token.
func (f *FakeInvokeTokenStore) Delete(_ context.Context, deploymentID string) error {
	if err := validateInvokeDeploymentID("fake invoke token store delete", deploymentID); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tokens, deploymentID)
	return nil
}

func IsInvokeTokenNotFoundErr(err error) bool {
	return errors.Is(err, ErrInvokeTokenNotFound)
}

var _ InvokeTokenStore = (*FileInvokeTokenStore)(nil)
var _ InvokeTokenStore = (*FakeInvokeTokenStore)(nil)
