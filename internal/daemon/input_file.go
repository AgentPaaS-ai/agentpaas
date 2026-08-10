package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Container path for M13.8 local large input (read-only bind).
const agentpaasInputMountPath = "/agentpaas/input"

// preparedInputFile is a validated host file ready for RO bind-mount.
type preparedInputFile struct {
	HostPath string
	SHA256   string
	Size     int64
	Bind     string // host:container:ro
}

// prepareInputFilePath validates an absolute host path for M13.8 SC4:
// must be absolute, regular file, readable; returns RO bind + digest.
func prepareInputFilePath(path string) (*preparedInputFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("input_file_path is empty")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("input_file_path must be an absolute path")
	}
	// Reject path tricks before Stat.
	clean := filepath.Clean(path)
	if clean != path {
		path = clean
	}
	if strings.Contains(path, "\x00") {
		return nil, fmt.Errorf("input_file_path contains NUL")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("input_file_path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Resolve one level only; final target must still be a regular file.
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("input_file_path symlink: %w", err)
		}
		path = target
		info, err = os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("input_file_path: %w", err)
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input_file_path must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("input_file_path open: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, fmt.Errorf("input_file_path hash: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return &preparedInputFile{
		HostPath: path,
		SHA256:   sum,
		Size:     n,
		Bind:     fmt.Sprintf("%s:%s:ro", path, agentpaasInputMountPath),
	}, nil
}
