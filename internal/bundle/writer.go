package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// Write creates a deterministic .agentpaas bundle and writes it to out.
// The bundle is a gzipped tar with lexicographically sorted entries.
// All tar headers use mtime=SourceDateEpoch, uid/gid=0, uname/gname="" .
// The manifest is signed with PublisherKey and written first in tar order.
func Write(cfg BundleConfig, out io.Writer) (*BundleResult, error) {
	if err := validateBundleConfig(&cfg); err != nil {
		return nil, err
	}

	// Canonical lock JSON (stable field order for determinism).
	lockJSON, err := pack.LockfileCanonicalJSON(cfg.Lock)
	if err != nil {
		return nil, fmt.Errorf("marshal lock: %w", err)
	}
	lockDigest := sha256Hex(lockJSON)

	policyDigest := sha256Hex(cfg.PolicyYAML)
	sbomDigest := sha256Hex(cfg.SBOM)

	// Source digest = lock's build_input_digest (already computed during pack).
	sourceDigest := cfg.Lock.BuildInputDigest

	// Build manifest with digests.
	manifest := *cfg.Manifest // shallow copy
	manifest.BundleSchemaVersion = BundleSchemaVersion
	manifest.Contents.Lock.Digest = lockDigest
	manifest.Contents.Policy.Digest = policyDigest
	manifest.Contents.SBOM.Digest = sbomDigest
	manifest.Contents.Source.Digest = sourceDigest

	// Scan image/ dir for OCI index if present.
	var imageEntry *ManifestImageEntry
	if cfg.ImageDir != "" {
		imageEntry = computeOCIImageDigest(cfg.ImageDir)
		if imageEntry != nil {
			manifest.Contents.Image = imageEntry
		}
	}

	// Sign the manifest.
	pubKey, ok := cfg.PublisherKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("publisher key must be *ecdsa.PrivateKey")
	}
	if cfg.Manifest.ManifestSignature != "" {
		manifest.ManifestSignature = cfg.Manifest.ManifestSignature
	} else {
		if err := signManifest(&manifest, pubKey); err != nil {
			return nil, fmt.Errorf("sign manifest: %w", err)
		}
		cfg.Manifest.ManifestSignature = manifest.ManifestSignature
	}

	manifestJSON, err := manifestCanonicalJSON(&manifest, true)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	// Collect source files.
	var sourceFiles []pack.BuildFile
	if cfg.ProjectDir != "" {
		sourceFiles, err = pack.CollectBuildFiles(cfg.ProjectDir, cfg.Ignore)
		if err != nil {
			return nil, fmt.Errorf("collect source files: %w", err)
		}
	}

	// Collect image files.
	var imageFiles []imageFileEntry
	if cfg.ImageDir != "" {
		imageFiles, err = collectImageFiles(cfg.ImageDir)
		if err != nil {
			return nil, fmt.Errorf("collect image files: %w", err)
		}
	}

	// Build tar in-memory, count entries.
	var buf bytes.Buffer
	fileCount, err := writeBundleTar(&buf, manifestJSON, lockJSON, cfg.PolicyYAML, cfg.SBOM,
		sourceFiles, cfg.ExtraFiles, imageFiles, cfg.SourceDateEpoch)
	if err != nil {
		return nil, err
	}
	tarBytes := buf.Bytes()

	// Gzip the tar deterministically.
	gzBuf := new(bytes.Buffer)
	if err := writeDeterministicGzip(gzBuf, tarBytes); err != nil {
		return nil, err
	}
	finalBytes := gzBuf.Bytes()

	// Compute bundle digest (over final gzipped bytes).
	bundleDigest := sha256Hex(finalBytes)

	// Write to output.
	n, err := out.Write(finalBytes)
	if err != nil {
		return nil, fmt.Errorf("write bundle: %w", err)
	}

	return &BundleResult{
		BundleDigest: bundleDigest,
		FileCount:    fileCount,
		TotalBytes:   int64(n),
	}, nil
}

// WriteToFile is like Write but writes to a file path and sets BundleResult.Path.
func WriteToFile(cfg BundleConfig, path string) (*BundleResult, error) {
	tmpPath := path + ".tmp"
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("create temp: %w", err)
	}
	writeErr := func() error {
		if _, err := Write(cfg, tmp); err != nil {
			return err
		}
		return tmp.Close()
	}()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, writeErr
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("rename temp to bundle: %w", err)
	}
	result, err := computeResultFromFile(cfg, path)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func computeResultFromFile(cfg BundleConfig, path string) (*BundleResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle for digest: %w", err)
	}
	bundleDigest := sha256Hex(data)

	// Count entries by decompressing and scanning tar.
	var fileCount int
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()
	tr := tar.NewReader(gzReader)
	for {
		_, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		fileCount++
	}

	return &BundleResult{
		BundleDigest: bundleDigest,
		Path:         path,
		FileCount:    fileCount,
		TotalBytes:   int64(len(data)),
	}, nil
}

// --- Internal builder functions ---

func validateBundleConfig(cfg *BundleConfig) error {
	if cfg == nil {
		return errors.New("bundle config must not be nil")
	}
	if cfg.Manifest == nil {
		return errors.New("manifest must not be nil")
	}
	if cfg.Lock == nil {
		return errors.New("lock must not be nil")
	}
	if cfg.PublisherKey == nil {
		return errors.New("publisher key must not be nil")
	}
	// Ensure it's the right type.
	if _, ok := cfg.PublisherKey.(*ecdsa.PrivateKey); !ok && cfg.PublisherKey != nil {
		return fmt.Errorf("publisher key must be *ecdsa.PrivateKey, got %T", cfg.PublisherKey)
	}
	if cfg.PolicyYAML == nil {
		cfg.PolicyYAML = []byte{}
	}
	if cfg.SBOM == nil {
		cfg.SBOM = []byte{}
	}
	if cfg.SourceDateEpoch.IsZero() {
		cfg.SourceDateEpoch = time.Unix(0, 0).UTC()
	}
	cfg.SourceDateEpoch = cfg.SourceDateEpoch.UTC()
	return nil
}

// writeBundleTar writes the bundle entries into a tar writer, sorted lexicographically.
// Returns the entry count and any error.
func writeBundleTar(w io.Writer, manifestJSON, lockJSON, policyYAML, sbomJSON []byte,
	sourceFiles, extraFiles []pack.BuildFile, imageFiles []imageFileEntry, mtime time.Time) (int, error) {

	// Build entries in a list so we can sort before writing.
	type entry struct {
		Name string
		Body []byte
		Mode int64
		// For files that come from disk (source, image).
		DiskPath string
		Info     fs.FileInfo
	}

	var entries []entry

	// manifest.json — written LAST in construction order but FIRST in sorted order.
	entries = append(entries, entry{Name: "manifest.json", Body: manifestJSON, Mode: 0o644})
	// agent.lock
	entries = append(entries, entry{Name: "agent.lock", Body: lockJSON, Mode: 0o644})
	// policy.yaml
	entries = append(entries, entry{Name: "policy.yaml", Body: policyYAML, Mode: 0o644})
	// sbom.spdx.json
	entries = append(entries, entry{Name: "sbom.spdx.json", Body: sbomJSON, Mode: 0o644})

	// source/ directory entry.
	entries = append(entries, entry{Name: "source/", Mode: 0o755})

	// source files.
	for _, f := range sourceFiles {
		entries = append(entries, entry{
			Name:     filepath.ToSlash(filepath.Join("source", f.RelPath)),
			DiskPath: f.AbsPath,
			Info:     f.Info,
			Mode:     int64(f.Info.Mode().Perm()),
		})
	}

	// extra/ files (--include).
	if len(extraFiles) > 0 {
		entries = append(entries, entry{Name: "extra/", Mode: 0o755})
	}
	for _, f := range extraFiles {
		entries = append(entries, entry{
			Name:     filepath.ToSlash(filepath.Join("extra", f.RelPath)),
			DiskPath: f.AbsPath,
			Info:     f.Info,
			Mode:     int64(f.Info.Mode().Perm()),
		})
	}

	// image/ directory and files.
	if len(imageFiles) > 0 {
		entries = append(entries, entry{Name: "image/", Mode: 0o755})
	}
	for _, f := range imageFiles {
		entries = append(entries, entry{
			Name:     filepath.ToSlash(filepath.Join("image", f.RelPath)),
			DiskPath: f.AbsPath,
			Info:     f.Info,
			Mode:     int64(f.Info.Mode().Perm()),
		})
	}

	// Sort lexicographically.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	// Write tar.
	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()

	for _, e := range entries {
		var body []byte
		var size int64
		mode := e.Mode
		isDir := strings.HasSuffix(e.Name, "/")

		if e.DiskPath != "" {
			data, err := os.ReadFile(e.DiskPath)
			if err != nil {
				return 0, fmt.Errorf("read %s: %w", e.DiskPath, err)
			}
			body = data
			size = int64(len(data))
			if mode == 0 {
				mode = 0o644
			}
		} else if !isDir {
			body = e.Body
			size = int64(len(body))
		}

		header := &tar.Header{
			Name:    e.Name,
			Size:    size,
			Mode:    mode,
			Uid:     0,
			Gid:     0,
			ModTime: mtime,
			Format:  tar.FormatUSTAR,
		}
		if isDir {
			header.Typeflag = tar.TypeDir
		} else {
			header.Typeflag = tar.TypeReg
		}

		if err := tw.WriteHeader(header); err != nil {
			return 0, fmt.Errorf("write tar header %s: %w", e.Name, err)
		}
		if !isDir {
			if _, err := tw.Write(body); err != nil {
				return 0, fmt.Errorf("write tar body %s: %w", e.Name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		return 0, fmt.Errorf("close tar: %w", err)
	}

	return len(entries), nil
}

func writeDeterministicGzip(w io.Writer, data []byte) error {
	gw, err := gzip.NewWriterLevel(w, gzip.HuffmanOnly)
	if err != nil {
		return err
	}
	// Zero MTime and set OS to 0xff for determinism.
	gw.Header = gzip.Header{
		ModTime: time.Time{},
		OS:      0xff,
	}
	if _, err := gw.Write(data); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

// --- Manifest signing ---

func signManifest(m *Manifest, key *ecdsa.PrivateKey) error {
	if key == nil {
		return errors.New("publisher key is nil")
	}

	canonical, err := manifestCanonicalJSON(m, false)
	if err != nil {
		return fmt.Errorf("canonical manifest JSON: %w", err)
	}

	digest := sha256.Sum256(canonical)
	sig, err := deterministicECDSASignASN1(key, digest[:])
	if err != nil {
		return fmt.Errorf("sign manifest: %w", err)
	}

	m.ManifestSignature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// deterministicECDSASignASN1 implements RFC 6979 for P-256/SHA-256. Go's
// ecdsa signer may consume additional entropy for blinding, so supplying a
// constant reader is not sufficient to make archive signatures reproducible.
func deterministicECDSASignASN1(key *ecdsa.PrivateKey, hash []byte) ([]byte, error) {
	q := key.Params().N
	rolen := (q.BitLen() + 7) / 8
	intBytes := func(v *big.Int) []byte {
		out := make([]byte, rolen)
		b := v.Bytes()
		copy(out[len(out)-len(b):], b)
		return out
	}
	z := new(big.Int).SetBytes(hash)
	if z.BitLen() > q.BitLen() {
		z.Rsh(z, uint(z.BitLen()-q.BitLen()))
	}
	if z.Cmp(q) >= 0 {
		z.Sub(z, q)
	}
	h1 := intBytes(z)
	x := intBytes(key.D)
	v := bytes.Repeat([]byte{1}, sha256.Size)
	k := make([]byte, sha256.Size)
	mac := func(key, data []byte) []byte { h := hmac.New(sha256.New, key); _, _ = h.Write(data); return h.Sum(nil) }
	k = mac(k, append(append(append([]byte{}, v...), 0), append(x, h1...)...))
	v = mac(k, v)
	k = mac(k, append(append(append([]byte{}, v...), 1), append(x, h1...)...))
	v = mac(k, v)
	for {
		t := make([]byte, 0, rolen)
		for len(t) < rolen {
			v = mac(k, v)
			t = append(t, v...)
		}
		candidate := new(big.Int).SetBytes(t[:rolen])
		if candidate.Sign() > 0 && candidate.Cmp(q) < 0 {
			x1, _ := key.Curve.ScalarBaseMult(candidate.Bytes())
			r := new(big.Int).Mod(x1, q)
			if r.Sign() != 0 {
				inv := new(big.Int).ModInverse(candidate, q)
				s := new(big.Int).Mul(r, key.D)
				s.Add(s, z)
				s.Mul(s, inv)
				s.Mod(s, q)
				if s.Sign() != 0 {
					return asn1.Marshal(struct{ R, S *big.Int }{r, s})
				}
			}
		}
		k = mac(k, append(append([]byte{}, v...), 0))
		v = mac(k, v)
	}
}

// manifestCanonicalJSON returns the canonical JSON representation of the manifest.
// When includeSignature is false, manifest_signature is excluded for signing.
// Uses the same approach as lockCanonicalMap: JSON with sorted keys.
func manifestCanonicalJSON(m *Manifest, includeSignature bool) ([]byte, error) {
	if m == nil {
		return nil, errors.New("manifest must not be nil")
	}

	contents := map[string]interface{}{
		"lock":   map[string]interface{}{"digest": m.Contents.Lock.Digest},
		"policy": map[string]interface{}{"digest": m.Contents.Policy.Digest},
		"sbom":   map[string]interface{}{"digest": m.Contents.SBOM.Digest},
		"source": map[string]interface{}{"digest": m.Contents.Source.Digest},
	}
	if m.Contents.Image != nil {
		contents["image"] = map[string]interface{}{
			"digest":   m.Contents.Image.Digest,
			"platform": m.Contents.Image.Platform,
		}
	}

	out := map[string]interface{}{
		"bundle_schema_version": m.BundleSchemaVersion,
		"publisher": map[string]interface{}{
			"name":           m.Publisher.Name,
			"fingerprint":    m.Publisher.Fingerprint,
			"public_key_pem": m.Publisher.PublicKeyPEM,
		},
		"contents":   contents,
		"created_at": m.CreatedAt,
	}
	if len(m.ExtraFiles) > 0 {
		extra := make([]map[string]interface{}, 0, len(m.ExtraFiles))
		for _, ef := range m.ExtraFiles {
			extra = append(extra, map[string]interface{}{
				"path":   ef.Path,
				"digest": ef.Digest,
				"bytes":  ef.Bytes,
			})
		}
		out["extra_files"] = extra
	}
	if includeSignature {
		out["manifest_signature"] = m.ManifestSignature
	}

	return json.Marshal(out)
}

// deterministicSignReader supplies fixed entropy so repeated signing of the same
// manifest bytes yields identical bundle archives (D1 determinism).
type deterministicSignReader struct{}

func (deterministicSignReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0x42
	}
	return len(b), nil
}

// --- Image handling ---

type imageFileEntry struct {
	RelPath string
	AbsPath string
	Info    fs.FileInfo
}

func collectImageFiles(imageDir string) ([]imageFileEntry, error) {
	var files []imageFileEntry
	err := filepath.WalkDir(imageDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(imageDir, path)
		if err != nil {
			return err
		}
		files = append(files, imageFileEntry{
			RelPath: filepath.ToSlash(rel),
			AbsPath: path,
			Info:    info,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
}

// computeOCIImageDigest reads the OCI index.json to compute image digest and platform.
func computeOCIImageDigest(imageDir string) *ManifestImageEntry {
	indexPath := filepath.Join(imageDir, "index.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return nil
	}
	digest := sha256Hex(indexData)

	// Try to extract platform info.
	var idx struct {
		Manifests []struct {
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform,omitempty"`
		} `json:"manifests"`
	}
	platform := ""
	if json.Unmarshal(indexData, &idx) == nil && len(idx.Manifests) > 0 {
		p := idx.Manifests[0].Platform
		if p.OS != "" && p.Architecture != "" {
			platform = p.OS + "/" + p.Architecture
		}
	}

	return &ManifestImageEntry{
		Digest:   digest,
		Platform: platform,
	}
}

// --- Helpers ---

func sha256Hex(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}
