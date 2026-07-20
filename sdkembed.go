// Package agentpaas embeds the Python SDK (python/agentpaas_sdk/) directly
// into the agentpaasd binary so that it is available at pack time regardless
// of how the binary was installed (brew, manual copy, release tarball).
//
// Without this, a brew-only install has no SDK on disk and every packed
// image fails at runtime with:
//
//	ModuleNotFoundError: No module named 'agentpaas_sdk'
//
// The go:embed directive must be in a .go file whose directory is a parent
// of the files to embed. We place this file at the repo root so it can
// embed python/agentpaas_sdk/**.
package agentpaas

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:python/agentpaas_sdk
var embeddedSDK embed.FS

// EmbeddedSDKDir returns the path that should be used as SDKDir when
// extracting the embedded SDK. Files are under "python/agentpaas_sdk".
const EmbeddedSDKPrefix = "python/agentpaas_sdk"

// HasEmbeddedSDK reports whether the binary contains the embedded SDK.
func HasEmbeddedSDK() bool {
	_, err := embeddedSDK.ReadDir(EmbeddedSDKPrefix)
	return err == nil
}

// EmbeddedSDKFiles returns the list of embedded SDK file paths
// (POSIX-style, relative to the embed root, e.g. "python/agentpaas_sdk/__init__.py").
// Excludes directories, __pycache__, and .pyc files. // intentionally ignored (reviewed)
func EmbeddedSDKFiles() ([]string, error) {
	var files []string
	err := fs.WalkDir(embeddedSDK, EmbeddedSDKPrefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) == ".pyc" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk embedded SDK: %w", err)
	}
	return files, nil
}

// ExtractEmbeddedSDK writes the embedded SDK files into dir, preserving
// the python/agentpaas_sdk/ directory structure. Returns the path to use
// as SDKDir (dir + "/python"). The caller MUST remove the temp dir when
// done.
func ExtractEmbeddedSDK(dir string) (string, error) {
	err := fs.WalkDir(embeddedSDK, EmbeddedSDKPrefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) == ".pyc" {
			return nil
		}
		data, err := embeddedSDK.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		// path is "python/agentpaas_sdk/..." — write it under dir.
		dst := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", dst, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("extract embedded SDK: %w", err)
	}
	return filepath.Join(dir, "python"), nil
}

// ExtractEmbeddedSDKToTemp creates a temporary directory, extracts the
// embedded SDK into it, and returns the SDKDir path. The caller MUST
// call the cleanup function when done with the SDK.
func ExtractEmbeddedSDKToTemp() (sdkDir string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "agentpaas-sdk-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir for SDK: %w", err)
	}
	sdkDir, err = ExtractEmbeddedSDK(tmp)
	if err != nil {
		_ = os.RemoveAll(tmp) // best-effort remove
		return "", nil, err
	}
	return sdkDir, func() { _ = os.RemoveAll(tmp) }, nil // best-effort remove
}
