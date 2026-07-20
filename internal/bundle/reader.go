package bundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// Open opens a .agentpaas bundle file and validates its structure.
// Phase 1: stream-scan all tar headers, validate paths, extract the four
// metadata files (manifest, lock, policy, sbom) to memory.
// Phase 2: source/ and image/ entries are indexed for on-demand extraction
// via ExtractSource and ExtractImage. They are never extracted implicitly.
func Open(path string) (*Bundle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bundle: %w", err)
	}
	defer func() {
		if f != nil {
			_ = f.Close() // best-effort close
		}
	}()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() {
		if gzReader != nil {
			_ = gzReader.Close() // best-effort close
		}
	}()

	tr := tar.NewReader(gzReader)

	var (
		manifestJSON  []byte
		lockJSON      []byte
		policyYAML    []byte
		sbomJSON      []byte
		meta          = make(map[string]bundleMetaEntry)
		sourceMeta    []bundleMetaEntry
		imageMeta     []bundleMetaEntry
		seen          = make(map[string]bool)
		totalSize     int64
		entryCount    int
		manifestFound bool
	)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}

		entryCount++
		if entryCount > MaxEntries {
			return nil, &ErrCapExceeded{Cap: "entries", Got: int64(entryCount), Max: MaxEntries}
		}

		// Validate path.
		name := hdr.Name
		if err := validateEntryPath(name); err != nil {
			return nil, fmt.Errorf("open: %w", err)
		}

		// Reject duplicate paths.
		if seen[name] {
			return nil, &ErrPathRejected{Path: name, Reason: "duplicate path"}
		}
		seen[name] = true

		// Reject dangerous types.
		switch hdr.Typeflag {
		case tar.TypeSymlink:
			return nil, &ErrPathRejected{Path: name, Reason: "symlinks are not allowed"}
		case tar.TypeLink:
			return nil, &ErrPathRejected{Path: name, Reason: "hardlinks are not allowed"}
		case tar.TypeChar, tar.TypeBlock:
			return nil, &ErrPathRejected{Path: name, Reason: "device entries are not allowed"}
		case tar.TypeFifo:
			return nil, &ErrPathRejected{Path: name, Reason: "FIFO entries are not allowed"}
		}

		// Size caps.
		if hdr.Size > MaxSingleFileSize {
			return nil, &ErrCapExceeded{Cap: "single file", Got: hdr.Size, Max: MaxSingleFileSize}
		}
		totalSize += hdr.Size
		if totalSize > MaxTotalUncompressed {
			return nil, &ErrCapExceeded{Cap: "total uncompressed", Got: totalSize, Max: MaxTotalUncompressed}
		}

		// Metadata files get special size cap.
		if isMetadataFile(name) && hdr.Size > MaxMetadataFileSize {
			return nil, &ErrCapExceeded{Cap: name + " size", Got: hdr.Size, Max: MaxMetadataFileSize}
		}

		switch name {
		case ManifestPath:
			if hdr.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("manifest.json must be a regular file")
			}
			manifestJSON = make([]byte, hdr.Size)
			if _, err := io.ReadFull(tr, manifestJSON); err != nil {
				return nil, fmt.Errorf("read manifest.json: %w", err)
			}
			manifestFound = true
			continue
		case AgentLockPath:
			if hdr.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("agent.lock must be a regular file")
			}
			lockJSON = make([]byte, hdr.Size)
			if _, err := io.ReadFull(tr, lockJSON); err != nil {
				return nil, fmt.Errorf("read agent.lock: %w", err)
			}
			continue
		case PolicyPath:
			if hdr.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("policy.yaml must be a regular file")
			}
			policyYAML = make([]byte, hdr.Size)
			if _, err := io.ReadFull(tr, policyYAML); err != nil {
				return nil, fmt.Errorf("read policy.yaml: %w", err)
			}
			continue
		case SBOMPath:
			if hdr.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("sbom.spdx.json must be a regular file")
			}
			sbomJSON = make([]byte, hdr.Size)
			if _, err := io.ReadFull(tr, sbomJSON); err != nil {
				return nil, fmt.Errorf("read sbom.spdx.json: %w", err)
			}
			continue
		}

		// For source/ and image/ entries, store metadata for on-demand extraction.
		entry := bundleMetaEntry{
			Name:  name,
			Mode:  hdr.Mode,
			Size:  hdr.Size,
			IsDir: hdr.Typeflag == tar.TypeDir,
		}
		meta[name] = entry

		if strings.HasPrefix(name, SourcePrefix) {
			sourceMeta = append(sourceMeta, entry)
		} else if strings.HasPrefix(name, ImagePrefix) {
			imageMeta = append(imageMeta, entry)
		} else {
			return nil, &ErrPathRejected{Path: name, Reason: "entry is not in source/ or image/ prefix"}
		}

		// Skip the body — we'll extract on demand.
		if hdr.Size > 0 && !entry.IsDir {
			if _, err := io.Copy(io.Discard, io.LimitReader(tr, hdr.Size)); err != nil {
				return nil, fmt.Errorf("skip entry %s: %w", name, err)
			}
		}
	}

	// We must have found manifest.json.
	if !manifestFound {
		return nil, errors.New("bundle is missing manifest.json")
	}

	// Close gzip reader (we'll keep the file open for on-demand extraction).
	_ = gzReader.Close() // best-effort close
	gzReader = nil

	// Parse JSON metadata.
	var manifest Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest.json: %w", err)
	}

	var lock pack.AgentLock
	if err := json.Unmarshal(lockJSON, &lock); err != nil {
		return nil, fmt.Errorf("parse agent.lock: %w", err)
	}

	result := &Bundle{
		Manifest:   &manifest,
		Lock:       &lock,
		LockJSON:   lockJSON,
		PolicyYAML: policyYAML,
		SBOM:       sbomJSON,
		raw:        f,
		meta:       meta,
		sourceMeta: sourceMeta,
		imageMeta:  imageMeta,
	}
	f = nil // transfer ownership
	return result, nil
}

// Close releases resources held by the bundle.
func (b *Bundle) Close() error {
	if b.raw != nil {
		return b.raw.Close()
	}
	return nil
}

// ExtractSource extracts the source/ tree to destDir.
// destDir must be an existing directory. Files are written with their
// original modes. Extraction is safe: paths are validated against escape.
func (b *Bundle) ExtractSource(destDir string) error {
	return b.extractPrefix(destDir, SourcePrefix, b.sourceMeta)
}

// ExtractImage extracts the image/ tree to destDir.
func (b *Bundle) ExtractImage(destDir string) error {
	return b.extractPrefix(destDir, ImagePrefix, b.imageMeta)
}

// extractPrefix extracts entries matching a prefix to destDir using temp-dir + rename.
func (b *Bundle) extractPrefix(destDir, prefix string, entries []bundleMetaEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Create a temp dir for extraction.
	tmpDir, err := os.MkdirTemp("", "agentpaas-bundle-extract-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort remove

	// Reset file position and decompress.
	if _, err := b.raw.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek bundle: %w", err)
	}
	gzReader, err := gzip.NewReader(b.raw)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }() // best-effort close
	tr := tar.NewReader(gzReader)

	// Build set of paths to extract.
	extractPaths := make(map[string]bool)
	for _, e := range entries {
		extractPaths[e.Name] = true
	}

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		if !extractPaths[hdr.Name] {
			continue
		}

		relPath := strings.TrimPrefix(hdr.Name, prefix)
		if relPath == "" {
			// This is the prefix directory itself.
			continue
		}

		dstPath := filepath.Join(tmpDir, filepath.FromSlash(relPath))

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return fmt.Errorf("create dir %s: %w", relPath, err)
			}
			continue
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", relPath, err)
		}

		f, err := os.Create(dstPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", relPath, err)
		}
		if _, err := io.CopyN(f, tr, hdr.Size); err != nil {
			_ = f.Close() // best-effort close
			return fmt.Errorf("write %s: %w", relPath, err)
		}
		if err := f.Chmod(os.FileMode(hdr.Mode & 0o777)); err != nil {
			_ = f.Close() // best-effort close
			return fmt.Errorf("chmod %s: %w", relPath, err)
		}
		_ = f.Close() // best-effort close
	}

	// Move extracted files from tmpDir to destDir (atomic via rename for each file).
	entries2, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("read temp dir: %w", err)
	}
	for _, e := range entries2 {
		src := filepath.Join(tmpDir, e.Name())
		dst := filepath.Join(destDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", e.Name(), dst, err)
		}
	}

	return nil
}

// validateEntryPath validates a tar entry name against bundle rules.
func validateEntryPath(name string) error {
	// Reject empty names.
	if name == "" {
		return &ErrPathRejected{Path: name, Reason: "empty path"}
	}

	// Reject absolute paths.
	if filepath.IsAbs(name) {
		return &ErrPathRejected{Path: name, Reason: "absolute path not allowed"}
	}

	// Reject ".." components.
	parts := strings.Split(filepath.ToSlash(name), "/")
	for _, part := range parts {
		if part == ".." {
			return &ErrPathRejected{Path: name, Reason: "path must not contain '..'"}
		}
	}

	// Reject control characters.
	for _, r := range name {
		if r < 0x20 {
			return &ErrPathRejected{Path: name, Reason: "control character in path"}
		}
	}

	// Validate path is within allowed prefixes.
	if isMetadataFile(name) {
		return nil
	}
	if strings.HasPrefix(name, SourcePrefix) {
		return nil
	}
	if strings.HasPrefix(name, ImagePrefix) {
		return nil
	}
	if strings.HasPrefix(name, ExtraPrefix) {
		return nil
	}

	return &ErrPathRejected{Path: name, Reason: "entry not in allowed set (manifest.json, agent.lock, policy.yaml, sbom.spdx.json, source/**, image/**, extra/**)"}
}

func isMetadataFile(name string) bool {
	switch name {
	case ManifestPath, AgentLockPath, PolicyPath, SBOMPath:
		return true
	}
	return false
}
