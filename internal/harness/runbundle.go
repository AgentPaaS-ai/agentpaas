package harness

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvBundleURL, EnvBundleSHA256 and EnvBundleSize are the platform-set
	// bundle values. All three must be set together. None set keeps the
	// 0.5.1 startup path. They are read once, before any component code runs,
	// and are never taken from the archive.
	EnvBundleURL    = "AGENTPAAS_BUNDLE_URL"
	EnvBundleSHA256 = "AGENTPAAS_BUNDLE_SHA256"
	EnvBundleSize   = "AGENTPAAS_BUNDLE_SIZE"
	// EnvBundleRehash selects a full re-hash of every file in a restored
	// tree. Unset uses FullRehashDefault. 0, false, no, or off skips it.
	EnvBundleRehash = "AGENTPAAS_BUNDLE_REHASH"

	// BundleManifestRel is the per-file hash manifest written after a
	// successful unpack, relative to the unpack root. It is not a bundle entry.
	BundleManifestRel = "app/.agentpaas-bundle-manifest.json"
	// BundleTarRel is the verified bundle kept beside the manifest so a
	// restored tree can be checked against the expected digest. The digest
	// is the one the platform took from the signed lock. File hashes in the
	// manifest are not that digest. This path is not a bundle entry.
	BundleTarRel = "app/.agentpaas-bundle.tar"

	ReasonBundleValuesIncomplete    = "bundle_values_incomplete"
	ReasonBundleValuesInvalid       = "bundle_values_invalid"
	ReasonBundleRedirectRefused     = "bundle_redirect_refused"
	ReasonBundleFetchFailed         = "bundle_fetch_failed"
	ReasonBundleSizeMismatch        = "bundle_size_mismatch"
	ReasonBundleDigestMismatch      = "bundle_digest_mismatch"
	ReasonBundlePathRejected        = "bundle_path_rejected"
	ReasonBundleLinkRejected        = "bundle_link_rejected"
	ReasonBundleCapExceeded         = "bundle_cap_exceeded"
	ReasonBundleUnpackFailed        = "bundle_unpack_failed"
	ReasonBundleTreeDigestMismatch  = "bundle_tree_digest_mismatch"
	ReasonBundleTreeManifestInvalid = "bundle_tree_manifest_invalid"
	ReasonBundleTreeRehashMismatch  = "bundle_tree_rehash_mismatch"

	defaultMaxRunBundleFile  int64 = 256 * 1024 * 1024
	defaultMaxRunBundleTotal int64 = 2 * 1024 * 1024 * 1024
	maxRunBundleEntries            = 500_000

	runBundleLockRel = "agentpaas/requirements.lock"
)

// RunBundleConfig is the platform's bundle fetch contract. Zero values mean
// the harness does not fetch a bundle.
type RunBundleConfig struct {
	URL    string
	SHA256 string
	Size   int64
	// Root is the filesystem root the archive is unpacked into. app/agent.py
	// lands at Root/app/agent.py. Production uses "/".
	Root string
	// MaxUnpackedBytes caps the sum of regular-file sizes. Zero uses the
	// default 2 GiB cap.
	MaxUnpackedBytes int64
	// MaxFileBytes caps one regular file. Zero uses the default 256 MiB cap.
	MaxFileBytes int64
	// HTTPClient is optional. Redirects are never followed, even when set.
	HTTPClient *http.Client
	// FullRehash selects a hash of every file when /app already holds an
	// unpacked bundle. Nil uses FullRehashDefault unless AGENTPAAS_BUNDLE_REHASH
	// is set. A restored tree is never downloaded again.
	FullRehash *bool
}

// BundleManifest is the per-file hash record written at unpack time.
type BundleManifest struct {
	BundleDigest string           `json:"bundle_digest"`
	BundleSize   int64            `json:"bundle_size"`
	Files        []BundleFileHash `json:"files"`
}

// BundleFileHash is one regular file in the unpacked bundle.
type BundleFileHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type bundleRefuseError struct {
	reason string
	detail string
}

func (e *bundleRefuseError) Error() string {
	if e == nil {
		return ""
	}
	if e.detail == "" {
		return e.reason
	}
	return e.reason + ": " + e.detail
}

func refuseBundle(reason, detail string) error {
	return &bundleRefuseError{reason: reason, detail: detail}
}

var errBundleRedirect = errors.New("redirect refused")

// Enabled reports whether any bundle value is set.
func (c RunBundleConfig) Enabled() bool {
	return c.URL != "" || c.SHA256 != "" || c.Size != 0
}

func (c RunBundleConfig) maxFile() int64 {
	if c.MaxFileBytes > 0 {
		return c.MaxFileBytes
	}
	return defaultMaxRunBundleFile
}

func (c RunBundleConfig) maxTotal() int64 {
	if c.MaxUnpackedBytes > 0 {
		return c.MaxUnpackedBytes
	}
	return defaultMaxRunBundleTotal
}

// RunBundleConfigFromEnv reads the platform bundle values once. An empty
// result means the 0.5.1 path. A partial or invalid set is a refusal.
func RunBundleConfigFromEnv() (RunBundleConfig, error) {
	rawURL := strings.TrimSpace(os.Getenv(EnvBundleURL))
	sha := strings.TrimSpace(os.Getenv(EnvBundleSHA256))
	sizeRaw := strings.TrimSpace(os.Getenv(EnvBundleSize))
	if rawURL == "" && sha == "" && sizeRaw == "" {
		return RunBundleConfig{}, nil
	}
	if rawURL == "" || sha == "" || sizeRaw == "" {
		return RunBundleConfig{}, refuseBundle(ReasonBundleValuesIncomplete, "fetch URL, sha256, and size must be set together")
	}
	size, err := strconv.ParseInt(sizeRaw, 10, 64)
	if err != nil || size <= 0 {
		return RunBundleConfig{}, refuseBundle(ReasonBundleValuesInvalid, "bundle size must be a positive integer")
	}
	return RunBundleConfig{
		URL:    rawURL,
		SHA256: sha,
		Size:   size,
		Root:   "/",
	}, nil
}

// LoadRunBundleFromEnv loads a bundle when the platform set the three values.
// Unset values are a no-op. A refusal writes one structured reason line to w.
func LoadRunBundleFromEnv(ctx context.Context, w io.Writer) error {
	cfg, err := RunBundleConfigFromEnv()
	if err != nil {
		writeBundleRefusal(w, err)
		return err
	}
	return LoadRunBundle(ctx, cfg, w)
}

// LoadRunBundle downloads, hashes while reading, and unpacks cfg into Root.
// A refusal writes one JSON reason line and leaves /app empty. The verified
// bytes are unpacked; the fetch is not repeated.
func LoadRunBundle(ctx context.Context, cfg RunBundleConfig, w io.Writer) error {
	if !cfg.Enabled() {
		return nil
	}
	if err := loadRunBundle(ctx, cfg); err != nil {
		writeBundleRefusal(w, err)
		return err
	}
	return nil
}

// StartAfterBundle loads a bundle when values are set, then calls start.
// A refusal does not call start.
func StartAfterBundle(ctx context.Context, cfg RunBundleConfig, w io.Writer, start func() error) error {
	if err := LoadRunBundle(ctx, cfg, w); err != nil {
		return err
	}
	if start == nil {
		return nil
	}
	return start()
}

func loadRunBundle(ctx context.Context, cfg RunBundleConfig) error {
	if err := validateBundleValues(cfg); err != nil {
		return err
	}
	restored, err := lookRestoredTree(cfg.Root)
	if err != nil {
		return err
	}
	if restored {
		return verifyRestoredTree(cfg)
	}
	// No manifest. Unpack writes only the files the bundle names, so only an
	// absent or empty /app is a fresh tree. A tree that lost its manifest is
	// refused without a download.
	if err := requireFreshTree(cfg.Root); err != nil {
		return err
	}
	body, err := downloadRunBundle(ctx, cfg)
	if err != nil {
		return err
	}
	files, err := unpackRunBundle(body, cfg.Root, cfg.maxFile(), cfg.maxTotal())
	if err != nil {
		return err
	}
	if err := requireOnlyBundleFiles(cfg.Root, bundlePaths(files)); err != nil {
		_ = removeUnpacked(cfg.Root, files) // best-effort cleanup
		return err
	}
	if err := writeVerifiedTar(cfg.Root, body); err != nil {
		_ = removeUnpacked(cfg.Root, files) // best-effort cleanup
		return refuseBundle(ReasonBundleUnpackFailed, "verified bundle write failed")
	}
	if err := writeBundleManifest(cfg.Root, cfg.SHA256, cfg.Size, files); err != nil {
		_ = removeUnpacked(cfg.Root, files) // best-effort cleanup
		return refuseBundle(ReasonBundleUnpackFailed, "manifest write failed")
	}
	log.Printf("harness: run bundle verified bytes=%d", cfg.Size)
	return nil
}

func validateBundleValues(cfg RunBundleConfig) error {
	if cfg.URL == "" || cfg.SHA256 == "" || cfg.Size <= 0 {
		return refuseBundle(ReasonBundleValuesIncomplete, "fetch URL, sha256, and size must be set together")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return refuseBundle(ReasonBundleValuesInvalid, "fetch URL must be http or https")
	}
	if len(cfg.SHA256) != sha256.Size*2 || !isLowerHex(cfg.SHA256) {
		return refuseBundle(ReasonBundleValuesInvalid, "sha256 must be 64 lowercase hex characters")
	}
	if cfg.Size > cfg.maxTotal() {
		return refuseBundle(ReasonBundleCapExceeded, "expected size exceeds the unpacked cap")
	}
	if cfg.Root == "" || !filepath.IsAbs(cfg.Root) {
		return refuseBundle(ReasonBundleValuesInvalid, "unpack root must be an absolute path")
	}
	return nil
}

func downloadRunBundle(ctx context.Context, cfg RunBundleConfig) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, refuseBundle(ReasonBundleValuesInvalid, "fetch URL rejected")
	}
	resp, err := noRedirectClient(cfg.HTTPClient).Do(req)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }() // best-effort close
	}
	if err != nil {
		if errors.Is(err, errBundleRedirect) {
			return nil, refuseBundle(ReasonBundleRedirectRefused, "redirects are not followed")
		}
		return nil, refuseBundle(ReasonBundleFetchFailed, "download failed")
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, refuseBundle(ReasonBundleRedirectRefused, "redirects are not followed")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, refuseBundle(ReasonBundleFetchFailed, "download status "+strconv.Itoa(resp.StatusCode))
	}
	if resp.ContentLength >= 0 && resp.ContentLength != cfg.Size {
		return nil, refuseBundle(ReasonBundleSizeMismatch, fmt.Sprintf("content length %d, want %d", resp.ContentLength, cfg.Size))
	}
	hash := sha256.New()
	var buf bytes.Buffer
	n, err := io.Copy(io.MultiWriter(&buf, hash), io.LimitReader(resp.Body, cfg.Size+1))
	if n != cfg.Size {
		return nil, refuseBundle(ReasonBundleSizeMismatch, fmt.Sprintf("got %d bytes, want %d", n, cfg.Size))
	}
	if err != nil {
		return nil, refuseBundle(ReasonBundleFetchFailed, "download failed")
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != cfg.SHA256 {
		return nil, refuseBundle(ReasonBundleDigestMismatch, "sha256 mismatch")
	}
	return buf.Bytes(), nil
}

func noRedirectClient(base *http.Client) *http.Client {
	client := &http.Client{Timeout: 10 * time.Minute}
	if base != nil {
		copied := *base
		client = &copied
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errBundleRedirect
	}
	return client
}

type bundleFile struct {
	rel  string
	body []byte
}

func unpackRunBundle(tarBytes []byte, root string, maxFile, maxTotal int64) ([]BundleFileHash, error) {
	files, err := readRunBundle(bytes.NewReader(tarBytes), maxFile, maxTotal)
	if err != nil {
		return nil, err
	}
	hashes, err := materializeRunBundle(root, files)
	if err != nil {
		_ = removeUnpacked(root, hashes) // best-effort cleanup
		return nil, err
	}
	return hashes, nil
}

func readRunBundle(r io.Reader, maxFile, maxTotal int64) ([]bundleFile, error) {
	tr := tar.NewReader(r)
	seen := make(map[string]struct{})
	var (
		files []bundleFile
		total int64
		count int
	)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, refuseBundle(ReasonBundleUnpackFailed, "read tar entry")
		}
		count++
		if count > maxRunBundleEntries {
			return nil, refuseBundle(ReasonBundleCapExceeded, "too many tar entries")
		}
		name, err := cleanRunBundlePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return nil, refuseBundle(ReasonBundleLinkRejected, "links are not allowed")
		case tar.TypeDir:
			if err := allowRunBundlePath(name, true); err != nil {
				return nil, err
			}
			continue
		case tar.TypeReg:
		default:
			// A zero typeflag is a regular file in pre-ustar headers.
			if hdr.Typeflag != 0 {
				return nil, refuseBundle(ReasonBundlePathRejected, "entry type not allowed")
			}
		}
		if err := allowRunBundlePath(name, false); err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			return nil, refuseBundle(ReasonBundlePathRejected, "duplicate path")
		}
		seen[name] = struct{}{}
		if hdr.Size < 0 {
			return nil, refuseBundle(ReasonBundleCapExceeded, "negative entry size")
		}
		if hdr.Size > maxFile || total > maxTotal-hdr.Size {
			return nil, refuseBundle(ReasonBundleCapExceeded, "unpacked size exceeds the cap")
		}
		total += hdr.Size
		body := make([]byte, hdr.Size)
		if _, err := io.ReadFull(tr, body); err != nil {
			return nil, refuseBundle(ReasonBundleUnpackFailed, "truncated entry")
		}
		files = append(files, bundleFile{rel: name, body: body})
	}
	return files, nil
}

func materializeRunBundle(root string, files []bundleFile) ([]BundleFileHash, error) {
	hashes := make([]BundleFileHash, 0, len(files))
	for _, file := range files {
		dest, err := destWithinRoot(root, file.rel)
		if err != nil {
			return hashes, err
		}
		if err := rejectDestSymlink(root, dest); err != nil {
			return hashes, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return hashes, refuseBundle(ReasonBundleUnpackFailed, "create directory")
		}
		if err := os.WriteFile(dest, file.body, 0o644); err != nil {
			return hashes, refuseBundle(ReasonBundleUnpackFailed, "write file")
		}
		sum := sha256.Sum256(file.body)
		hashes = append(hashes, BundleFileHash{
			Path:   file.rel,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(file.body)),
		})
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i].Path < hashes[j].Path })
	return hashes, nil
}

func writeBundleManifest(root, digest string, size int64, files []BundleFileHash) error {
	raw, err := manifestBytes(digest, size, files)
	if err != nil {
		return err
	}
	dest, err := destWithinRoot(root, BundleManifestRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, raw, 0o644)
}

func manifestBytes(digest string, size int64, files []BundleFileHash) ([]byte, error) {
	if files == nil {
		files = []BundleFileHash{}
	}
	raw, err := json.MarshalIndent(BundleManifest{
		BundleDigest: digest,
		BundleSize:   size,
		Files:        files,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func writeVerifiedTar(root string, body []byte) error {
	dest, err := destWithinRoot(root, BundleTarRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, body, 0o644)
}

func removeUnpacked(root string, files []BundleFileHash) error {
	var first error
	for _, file := range files {
		dest, err := destWithinRoot(root, file.Path)
		if err != nil {
			continue
		}
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	manifest, err := destWithinRoot(root, BundleManifestRel)
	if err == nil {
		_ = os.Remove(manifest) // best-effort remove
	}
	verified, err := destWithinRoot(root, BundleTarRel)
	if err == nil {
		_ = os.Remove(verified) // best-effort remove
	}
	return first
}

func cleanRunBundlePath(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", refuseBundle(ReasonBundlePathRejected, "path rejected")
	}
	dir := strings.HasSuffix(name, "/")
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || trimmed == "." {
		return "", refuseBundle(ReasonBundlePathRejected, "path rejected")
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return "", refuseBundle(ReasonBundlePathRejected, "path rejected")
		}
	}
	if dir {
		return trimmed + "/", nil
	}
	return trimmed, nil
}

func allowRunBundlePath(name string, dir bool) error {
	rel := strings.TrimSuffix(name, "/")
	if rel == BundleManifestRel || rel == BundleTarRel {
		return refuseBundle(ReasonBundlePathRejected, "manifest path is reserved")
	}
	switch {
	case rel == "app" || strings.HasPrefix(rel, "app/"):
		return nil
	case dir && rel == "agentpaas":
		return nil
	case !dir && rel == runBundleLockRel:
		return nil
	default:
		return refuseBundle(ReasonBundlePathRejected, "path is outside the run bundle roots")
	}
}

func destWithinRoot(root, rel string) (string, error) {
	rel = strings.TrimSuffix(rel, "/")
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if !pathWithin(root, dest) {
		return "", refuseBundle(ReasonBundlePathRejected, "path escapes unpack root")
	}
	return dest, nil
}

func pathWithin(root, dest string) bool {
	root = filepath.Clean(root)
	dest = filepath.Clean(dest)
	rel, err := filepath.Rel(root, dest)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func rejectDestSymlink(root, dest string) error {
	rel, err := filepath.Rel(root, dest)
	if err != nil {
		return refuseBundle(ReasonBundlePathRejected, "path escapes unpack root")
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return refuseBundle(ReasonBundleUnpackFailed, "inspect destination")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return refuseBundle(ReasonBundleLinkRejected, "destination is a symlink")
		}
	}
	return nil
}

func writeBundleRefusal(w io.Writer, err error) {
	if w == nil {
		w = io.Discard
	}
	reason, detail := ReasonBundleUnpackFailed, "bundle refused"
	var ref *bundleRefuseError
	if errors.As(err, &ref) && ref != nil {
		reason = ref.reason
		detail = ref.detail
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(ErrorResponse{Status: "FAILED", Reason: reason, Detail: detail}) // best-effort line
}

func isLowerHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// FullRehashDefault reports whether a restored tree re-hashes every file when
// AGENTPAAS_BUNDLE_REHASH is unset. A changed file under /app/deps must be
// refused with no extra switch, so the unset default is on.
func FullRehashDefault() bool { return true }

func (c RunBundleConfig) fullRehash() bool {
	if c.FullRehash != nil {
		return *c.FullRehash
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBundleRehash))) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return FullRehashDefault()
	}
}

// lookRestoredTree reports whether root already holds an unpacked bundle.
// A present manifest that is not a regular file is a refusal, not a miss,
// so a swapped manifest cannot fall through to a fresh download.
func lookRestoredTree(root string) (bool, error) {
	path, err := destWithinRoot(root, BundleManifestRel)
	if err != nil {
		return false, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest unreadable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest must be a regular file")
	}
	return true, nil
}

func verifyRestoredTree(cfg RunBundleConfig) error {
	manifest, err := readBundleManifest(cfg.Root)
	if err != nil {
		return err
	}
	if manifest.BundleDigest != cfg.SHA256 || manifest.BundleSize != cfg.Size {
		return refuseBundle(ReasonBundleTreeDigestMismatch, "manifest does not match the expected digest")
	}
	if !cfg.fullRehash() {
		log.Printf("harness: restored tree digest matched; file rehash skipped")
		return nil
	}
	// File hashes in the restored manifest are writable by the same uid that
	// owns the tree. Expected hashes come from the verified bundle bytes,
	// which must still hash to the digest the platform took from the signed lock.
	expected, err := expectedFilesFromVerifiedTar(cfg)
	if err != nil {
		return err
	}
	if err := rehashRestoredTree(cfg.Root, BundleManifest{Files: expected}); err != nil {
		return err
	}
	if err := requireProducedManifest(cfg.Root, cfg.SHA256, cfg.Size, expected); err != nil {
		return err
	}
	log.Printf("harness: restored tree verified bytes=%d", cfg.Size)
	return nil
}

func expectedFilesFromVerifiedTar(cfg RunBundleConfig) ([]BundleFileHash, error) {
	raw, err := readVerifiedTar(cfg.Root, cfg.Size)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != cfg.SHA256 {
		return nil, refuseBundle(ReasonBundleTreeRehashMismatch, "tree does not match the expected digest")
	}
	files, err := readRunBundle(bytes.NewReader(raw), cfg.maxFile(), cfg.maxTotal())
	if err != nil {
		return nil, refuseBundle(ReasonBundleTreeRehashMismatch, "verified bundle unreadable")
	}
	return hashesFromBundleFiles(files), nil
}

func readVerifiedTar(root string, wantSize int64) ([]byte, error) {
	path, err := destWithinRoot(root, BundleTarRel)
	if err != nil {
		return nil, refuseBundle(ReasonBundleTreeRehashMismatch, "verified bundle path rejected")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, refuseBundle(ReasonBundleTreeRehashMismatch, "verified bundle missing")
	}
	if info.Size() != wantSize {
		return nil, refuseBundle(ReasonBundleTreeRehashMismatch, "tree does not match the expected digest")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, refuseBundle(ReasonBundleTreeRehashMismatch, "verified bundle unreadable")
	}
	return raw, nil
}

func hashesFromBundleFiles(files []bundleFile) []BundleFileHash {
	hashes := make([]BundleFileHash, 0, len(files))
	for _, file := range files {
		sum := sha256.Sum256(file.body)
		hashes = append(hashes, BundleFileHash{
			Path:   file.rel,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(file.body)),
		})
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i].Path < hashes[j].Path })
	return hashes
}

func requireProducedManifest(root, digest string, size int64, files []BundleFileHash) error {
	want, err := manifestBytes(digest, size, files)
	if err != nil {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "manifest unreadable")
	}
	path, err := destWithinRoot(root, BundleManifestRel)
	if err != nil {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "manifest path rejected")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "manifest unreadable")
	}
	if !bytes.Equal(got, want) {
		return refuseBundle(ReasonBundleTreeRehashMismatch, "manifest is not the re-hash of the bundle")
	}
	return nil
}

func readBundleManifest(root string) (BundleManifest, error) {
	path, err := destWithinRoot(root, BundleManifestRel)
	if err != nil {
		return BundleManifest{}, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest path rejected")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return BundleManifest{}, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest must be a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return BundleManifest{}, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest unreadable")
	}
	var manifest BundleManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return BundleManifest{}, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest is not json")
	}
	if manifest.BundleDigest == "" || manifest.BundleSize <= 0 {
		return BundleManifest{}, refuseBundle(ReasonBundleTreeManifestInvalid, "manifest identity missing")
	}
	if manifest.Files == nil {
		manifest.Files = []BundleFileHash{}
	}
	return manifest, nil
}

func rehashRestoredTree(root string, manifest BundleManifest) error {
	want := make(map[string]BundleFileHash, len(manifest.Files))
	for _, file := range manifest.Files {
		if file.Path == BundleManifestRel {
			return refuseBundle(ReasonBundleTreeManifestInvalid, "manifest path is reserved")
		}
		if _, ok := want[file.Path]; ok {
			return refuseBundle(ReasonBundleTreeManifestInvalid, "duplicate manifest path")
		}
		if err := allowRunBundlePath(file.Path, false); err != nil {
			return refuseBundle(ReasonBundleTreeManifestInvalid, "manifest path rejected")
		}
		want[file.Path] = file
	}
	seen := make(map[string]struct{}, len(want))
	for path, wantFile := range want {
		abs, err := destWithinRoot(root, path)
		if err != nil {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "manifest path rejected")
		}
		sum, size, err := hashTreeFile(abs)
		if err != nil {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "manifest file missing")
		}
		if wantFile.SHA256 != sum || wantFile.Size != size {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "file does not match the manifest")
		}
		seen[path] = struct{}{}
	}
	if err := requireOnlyBundleFiles(root, seen); err != nil {
		return err
	}
	if len(seen) != len(want) {
		return refuseBundle(ReasonBundleTreeRehashMismatch, "manifest file missing")
	}
	return nil
}

// requireFreshTree allows a download only into an absent or empty /app. A
// missing manifest beside any other entry is a restored tree that lost its
// manifest, not a fresh one: downloading over it would keep every file the
// bundle does not name.
func requireFreshTree(root string) error {
	app := filepath.Join(root, "app")
	info, err := os.Lstat(app)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "inspect tree")
	}
	if !info.IsDir() {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "app must be a directory")
	}
	entries, err := os.ReadDir(app)
	if err != nil {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "inspect tree")
	}
	if len(entries) != 0 {
		return refuseBundle(ReasonBundleTreeManifestInvalid, "manifest missing from a tree that is not empty")
	}
	return nil
}

func bundlePaths(files []BundleFileHash) map[string]struct{} {
	named := make(map[string]struct{}, len(files))
	for _, file := range files {
		named[file.Path] = struct{}{}
	}
	return named
}

// requireOnlyBundleFiles walks /app and refuses any entry that is not a
// directory, a named bundle file, the manifest, or the kept tar. Both the
// download and the restored path run it before the worker starts.
func requireOnlyBundleFiles(root string, named map[string]struct{}) error {
	app := filepath.Join(root, "app")
	info, err := os.Lstat(app)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return refuseBundle(ReasonBundleTreeRehashMismatch, "inspect tree")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return refuseBundle(ReasonBundleTreeRehashMismatch, "symlink not allowed")
	}
	return filepath.WalkDir(app, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "walk tree")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "symlink not allowed")
		}
		if d.IsDir() || !d.Type().IsRegular() {
			if !d.IsDir() && d.Type()&os.ModeSymlink == 0 {
				return refuseBundle(ReasonBundleTreeRehashMismatch, "entry type not allowed")
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || !pathWithin(root, path) {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "path escapes unpack root")
		}
		rel = filepath.ToSlash(rel)
		if rel == BundleManifestRel || rel == BundleTarRel {
			return nil
		}
		if _, ok := named[rel]; !ok {
			return refuseBundle(ReasonBundleTreeRehashMismatch, "file is not in the bundle")
		}
		return nil
	})
}

func hashTreeFile(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }() // best-effort close
	hash := sha256.New()
	n, err := io.Copy(hash, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}
