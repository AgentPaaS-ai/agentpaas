package bundle

import (
	"io"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// BundleSchemaVersion is the current .agentpaas bundle schema version.
const BundleSchemaVersion = 1

// --- Cap constants for hardened reader ---

const (
	// MaxEntries is the maximum number of entries allowed in a bundle tar.
	MaxEntries = 10_000
	// MaxSingleFileSize is the maximum size of a single file entry (256 MB).
	MaxSingleFileSize = 256 * 1024 * 1024
	// MaxTotalUncompressed is the maximum total uncompressed size of all entries (2 GB).
	MaxTotalUncompressed = 2 * 1024 * 1024 * 1024
	// MaxMetadataFileSize is the maximum size for manifest/lock/policy/sbom (10 MB each).
	MaxMetadataFileSize = 10 * 1024 * 1024
)

// --- Path constants ---

const (
	ManifestPath  = "manifest.json"
	AgentLockPath = "agent.lock"
	PolicyPath    = "policy.yaml"
	SBOMPath      = "sbom.spdx.json"
	SourcePrefix  = "source/"
	ImagePrefix   = "image/"
	ExtraPrefix   = "extra/"
)

// --- Verification check IDs ---

const (
	CheckManifestParse     = "manifest_parse"
	CheckManifestSignature = "manifest_signature"
	CheckPublisherMatch    = "publisher_match"
	CheckLockProvenance    = "lock_provenance"
	CheckContentSHA256     = "content_sha256"
	CheckPolicyDigest      = "policy_digest"
	CheckSBOMDigest        = "sbom_digest"
	CheckSourceDigest      = "source_digest"
	CheckImageDigest       = "image_digest"
)

// --- Bundle manifest types ---

// Manifest describes the bundle contents and is signed by the publisher.
type Manifest struct {
	BundleSchemaVersion int                   `json:"bundle_schema_version"`
	Publisher           ManifestPublisherInfo `json:"publisher"`
	Contents            ManifestContents      `json:"contents"`
	CreatedAt           time.Time             `json:"created_at"`
	// ManifestSignature is the ECDSA signature over the canonical JSON,
	// excluding this field. Base64-encoded.
	ManifestSignature string `json:"manifest_signature,omitempty"`
	// ExtraFiles lists --include paths digest-pinned but excluded from source digest.
	ExtraFiles []ManifestExtraFile `json:"extra_files,omitempty"`
}

// ManifestExtraFile is an explicitly included file outside the locked source digest.
type ManifestExtraFile struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
}

// ManifestPublisherInfo identifies the publisher of the bundle.
type ManifestPublisherInfo struct {
	Name         string `json:"name"`
	Fingerprint  string `json:"fingerprint"`
	PublicKeyPEM string `json:"public_key_pem"`
}

// ManifestContents describes what's in the bundle, with per-file content digests.
type ManifestContents struct {
	Lock   ManifestDigestEntry  `json:"lock"`
	Policy ManifestDigestEntry  `json:"policy"`
	SBOM   ManifestDigestEntry  `json:"sbom"`
	Source ManifestDigestEntry  `json:"source"`
	Image  *ManifestImageEntry  `json:"image,omitempty"`
}

// ManifestDigestEntry is a single content entry with its SHA-256 digest.
type ManifestDigestEntry struct {
	Digest string `json:"digest"`
}

// ManifestImageEntry describes the OCI image included in the bundle.
type ManifestImageEntry struct {
	Digest   string `json:"digest"`
	Platform string `json:"platform"`
}

// --- Bundle configuration ---

// BundleConfig controls bundle creation.
type BundleConfig struct {
	// ProjectDir is the source directory to include as source/ entries.
	ProjectDir string
	// Manifest is the manifest to write (manifest_signature is added by Write).
	Manifest *Manifest
	// Lock is the signed agent lockfile.
	Lock *pack.AgentLock
	// PolicyYAML is the raw policy.yaml bytes (exact sidecar, never re-marshaled).
	PolicyYAML []byte
	// SBOM is the raw SPDX JSON bytes.
	SBOM []byte
	// ImageDir is an optional OCI image layout directory to include as image/.
	ImageDir string
	// Ignore filters source/ collection (export uses export-specific matcher).
	Ignore *pack.IgnoreMatcher
	// ExtraFiles are written under extra/ and listed in manifest.extra_files.
	ExtraFiles []pack.BuildFile
	// PublisherKey is the ECDSA private key used to sign the manifest.
	PublisherKey interface{} // *ecdsa.PrivateKey — use interface to avoid import
	// SourceDateEpoch is the fixed timestamp for deterministic tar entries.
	SourceDateEpoch time.Time
}

// --- Bundle result ---

// BundleResult is returned by Write after successful bundle creation.
type BundleResult struct {
	// BundleDigest is the SHA-256 of the final .agentpaas file bytes (hex).
	BundleDigest string
	// Path is the path to the written file (empty when writing to io.Writer).
	Path string
	// FileCount is the total number of entries in the bundle.
	FileCount int
	// TotalBytes is the final size in bytes.
	TotalBytes int64
}

// --- Opened bundle ---

// Bundle represents a fully opened and validated .agentpaas file.
type Bundle struct {
	Manifest   *Manifest
	Lock       *pack.AgentLock
	LockJSON   []byte
	PolicyYAML []byte
	SBOM       []byte

	// Internal state for on-demand extraction.
	raw        readSeekCloser
	meta       map[string]bundleMetaEntry
	sourceMeta []bundleMetaEntry
	imageMeta  []bundleMetaEntry
}

type readSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// bundleMetaEntry stores metadata about a tar entry for on-demand extraction.
type bundleMetaEntry struct {
	Name   string
	Mode   int64
	Offset int64 // offset in the underlying file for the entry content
	Size   int64
	IsDir  bool
}

// --- Verification ---

// VerifyCheck is a single verification check result.
type VerifyCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// VerifyReport is the result of Verify().
type VerifyReport struct {
	Verified bool          `json:"verified"`
	Checks   []VerifyCheck `json:"checks"`
}

// --- Error types ---

// ErrCapExceeded is returned when a bundle exceeds a size/entry cap.
type ErrCapExceeded struct {
	Cap string
	Got int64
	Max int64
}

func (e *ErrCapExceeded) Error() string {
	return "bundle cap exceeded: " + e.Cap
}

// ErrPathRejected is returned when a tar entry path fails validation.
type ErrPathRejected struct {
	Path   string
	Reason string
}

func (e *ErrPathRejected) Error() string {
	return "bundle path rejected: " + e.Path + " — " + e.Reason
}

// ErrUnsupported is returned when a bundle feature is unsupported.
type ErrUnsupported struct {
	What string
}

func (e *ErrUnsupported) Error() string {
	return "unsupported bundle feature: " + e.What
}