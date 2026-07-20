// Package fsutil provides shared filesystem path safety helpers.
package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ErrSymlink indicates a path component is a symbolic link.
var ErrSymlink = errors.New("symlink path component rejected")

// ErrPathEscapes indicates a path escapes a required root directory.
var ErrPathEscapes = errors.New("path escapes root")

// SymlinkError carries the offending symlink path.
type SymlinkError struct {
	Path string
}

func (e *SymlinkError) Error() string {
	if e == nil {
		return ErrSymlink.Error()
	}
	return fmt.Sprintf("%s: %s", ErrSymlink.Error(), e.Path)
}

func (e *SymlinkError) Is(target error) bool {
	return target == ErrSymlink
}

// PathEscapesError carries path/root when a path escapes its root.
type PathEscapesError struct {
	Path string
	Root string
}

func (e *PathEscapesError) Error() string {
	if e == nil {
		return ErrPathEscapes.Error()
	}
	return fmt.Sprintf("%s: path %s escapes root %s", ErrPathEscapes.Error(), e.Path, e.Root)
}

func (e *PathEscapesError) Is(target error) bool {
	return target == ErrPathEscapes
}

// RejectSymlinkLeaf fails if path exists and is a symlink. A missing path is OK.
func RejectSymlinkLeaf(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return &SymlinkError{Path: path}
	}
	return nil
}

// RejectSymlinkPathAndParent ensures path (if it exists) is not a symlink, and
// that its immediate parent directory (if it exists) is not a symlink.
//
// It does not walk all the way to the filesystem root: on macOS /var is a
// symlink to /private/var, which is a normal system layout and must not fail
// closed. Callers that manage a store root should additionally use
// RejectSymlinkInRoot so every component under the store root is checked.
func RejectSymlinkPathAndParent(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	cleaned := filepath.Clean(path)
	if err := RejectSymlinkLeaf(cleaned); err != nil {
		return err
	}
	parent := filepath.Dir(cleaned)
	if parent != cleaned {
		if err := RejectSymlinkLeaf(parent); err != nil {
			return err
		}
	}
	return nil
}

// RejectSymlinkInRoot walks every path component from root to path (inclusive)
// and fails if any is a symlink. path must be under root.
func RejectSymlinkInRoot(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return &PathEscapesError{Path: path, Root: root}
	}
	if err := RejectSymlinkLeaf(root); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return &PathEscapesError{Path: path, Root: root}
		}
		cur = filepath.Join(cur, part)
		if err := RejectSymlinkLeaf(cur); err != nil {
			return err
		}
	}
	return nil
}

// MissingMode controls how missing path components are treated during a walk.
type MissingMode int

const (
	// MissingFail rejects any missing component.
	MissingFail MissingMode = iota
	// MissingAllowLeaf allows only the final component to be missing.
	MissingAllowLeaf
	// MissingAllowAll allows any component to be missing.
	MissingAllowAll
)

// WalkOptions configures RejectSymlinkWalk.
type WalkOptions struct {
	// RequireAbsolute requires the cleaned path to be absolute.
	RequireAbsolute bool
	// RejectDotDot rejects a ".." path segment in the cleaned path.
	RejectDotDot bool
	// ResolveAbs runs filepath.Abs before walking.
	ResolveAbs bool
	// Missing controls missing-component policy.
	Missing MissingMode
	// SkipVolumeRootSymlinks skips symlink rejection for components whose
	// parent is the filesystem root (e.g. allow macOS /var -> /private/var).
	// Uses an upward walk matching historical pack.detect behavior.
	SkipVolumeRootSymlinks bool
}

// RejectSymlinkWalk walks path components and rejects symlink components
// according to opts.
func RejectSymlinkWalk(path string, opts WalkOptions) error {
	cleanPath := filepath.Clean(path)
	if opts.ResolveAbs {
		absPath, err := filepath.Abs(cleanPath)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		cleanPath = absPath
	}
	if opts.RequireAbsolute && !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("path %s must be absolute", path)
	}
	if opts.RejectDotDot && HasDotDotPathSegment(cleanPath) {
		return fmt.Errorf("path %s must not contain dot-dot path segments", path)
	}
	if opts.SkipVolumeRootSymlinks {
		return rejectSymlinkWalkUp(cleanPath, opts.Missing)
	}
	return rejectSymlinkWalkDown(cleanPath, opts.Missing)
}

func rejectSymlinkWalkUp(absPath string, missing MissingMode) error {
	current := absPath
	for {
		parent := filepath.Dir(current)
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 && filepath.Dir(parent) != parent {
				return &SymlinkError{Path: current}
			}
		} else if errors.Is(err, fs.ErrNotExist) {
			if missing == MissingFail {
				return fmt.Errorf("inspect %s: %w", current, err)
			}
		} else {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		if parent == current {
			return nil
		}
		current = parent
	}
}

func rejectSymlinkWalkDown(cleanPath string, missing MissingMode) error {
	volume := filepath.VolumeName(cleanPath)
	rest := strings.TrimPrefix(cleanPath, volume)
	if rest == "" {
		return nil
	}
	separator := string(os.PathSeparator)
	current := volume
	if strings.HasPrefix(rest, separator) {
		current += separator
		rest = strings.TrimPrefix(rest, separator)
	}
	parts := strings.Split(rest, separator)
	components := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			components = append(components, part)
		}
	}
	for i, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
				isLeaf := i == len(components)-1
				switch missing {
				case MissingAllowAll:
					continue
				case MissingAllowLeaf:
					if isLeaf {
						return nil
					}
					return err
				default:
					return err
				}
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &SymlinkError{Path: current}
		}
	}
	return nil
}

// HasDotDotPathSegment reports whether path contains a ".." segment.
func HasDotDotPathSegment(path string) bool {
	for _, component := range strings.Split(path, string(os.PathSeparator)) {
		if component == ".." {
			return true
		}
	}
	return false
}

// SafeID sanitizes an ID for use as a single path component.
// Rejects empty, traversal, separators, and control characters by hashing
// unsafe input into a stable, safe component.
func SafeID(id string) string {
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
