package audit

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// BundleManifest is the signed metadata for an audit export bundle. It
// captures the audit chain head, checkpoint state, and public key identity
// at the time of export, all tied together by a signature from the daemon's
// audit signing key.
//
// Offline verification of a bundle proves:
//  1. The bundled audit JSONL and checkpoints match the manifest metadata.
//  2. The audit hash chain is internally consistent (no gaps, no tampered
//     records in the chain).
//  3. The checkpoint chain is internally consistent (no missing checkpoints,
//     no broken checkpoint-to-checkpoint links).
//  4. The checkpoint signatures are valid against the bundled public key.
//  5. The manifest is signed by the expected daemon audit key.
//
// It does NOT prove:
//   - That the bundle was ever published to a global transparency log.
//   - Global ordering of events across daemon instances.
//   - That no records were pruned from the audit chain before the oldest
//     checkpoint in the bundle.
type BundleManifest struct {
	BundleVersion     int    `json:"bundle_version"`
	ExportTimestamp   string `json:"export_timestamp"`
	AuditRecordCount  int64  `json:"audit_record_count"`
	AuditHeadSeq      int64  `json:"audit_head_seq"`
	AuditHeadHash     string `json:"audit_head_hash"`
	CheckpointCount   int    `json:"checkpoint_count"`
	LatestCpSeq       int64  `json:"latest_cp_seq"`
	LatestCpHash      string `json:"latest_cp_hash"`
	PubKeyFingerprint string `json:"pub_key_fingerprint"`
	ManifestHash      string `json:"manifest_hash"`
	Signature         []byte `json:"signature"`
}

// canonicalManifest is the deterministic subset of BundleManifest used for
// hash computation. It omits ManifestHash and Signature.
type canonicalManifest struct {
	BundleVersion     int    `json:"bundle_version"`
	ExportTimestamp   string `json:"export_timestamp"`
	AuditRecordCount  int64  `json:"audit_record_count"`
	AuditHeadSeq      int64  `json:"audit_head_seq"`
	AuditHeadHash     string `json:"audit_head_hash"`
	CheckpointCount   int    `json:"checkpoint_count"`
	LatestCpSeq       int64  `json:"latest_cp_seq"`
	LatestCpHash      string `json:"latest_cp_hash"`
	PubKeyFingerprint string `json:"pub_key_fingerprint"`
}

// computeManifestHash computes the SHA-256 hex digest of the canonical
// manifest JSON (without ManifestHash and Signature).
func (m *BundleManifest) computeManifestHash() (string, error) {
	cm := &canonicalManifest{
		BundleVersion:     m.BundleVersion,
		ExportTimestamp:   m.ExportTimestamp,
		AuditRecordCount:  m.AuditRecordCount,
		AuditHeadSeq:      m.AuditHeadSeq,
		AuditHeadHash:     m.AuditHeadHash,
		CheckpointCount:   m.CheckpointCount,
		LatestCpSeq:       m.LatestCpSeq,
		LatestCpHash:      m.LatestCpHash,
		PubKeyFingerprint: m.PubKeyFingerprint,
	}
	data, err := json.Marshal(cm)
	if err != nil {
		return "", fmt.Errorf("marshal canonical manifest: %w", err)
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}

// VerifyManifestSignature checks that the manifest's signature is valid for
// its ManifestHash using the given ECDSA P-256 public key.
func (m *BundleManifest) VerifyManifestSignature(pub *ecdsa.PublicKey) bool {
	if m.ManifestHash == "" || len(m.Signature) == 0 || pub == nil {
		return false
	}
	hashBytes := sha256.Sum256([]byte(m.ManifestHash))
	return ecdsa.VerifyASN1(pub, hashBytes[:], m.Signature)
}

// BundlePaths holds the standard file paths within an audit bundle directory.
type BundlePaths struct {
	Dir             string
	ManifestPath    string
	AuditPath       string
	CheckpointsPath string
	PubKeyPath      string
}

// ExportBundleOptions configures the audit bundle export.
type ExportBundleOptions struct {
	// AuditPath is the path to the audit JSONL file.
	AuditPath string
	// CheckpointPath is the path to the checkpoint JSONL file.
	CheckpointPath string
	// SigningKey is the ECDSA P-256 private key used to sign the manifest.
	SigningKey *ecdsa.PrivateKey
	// PubKeyDER is the PKIX/SPKI DER-encoded public key corresponding to the
	// signing key. This is the key whose fingerprint is recorded in the
	// manifest and against which offline verifiers check signatures.
	PubKeyDER []byte
}

// ExportAuditBundle creates a portable audit bundle directory containing a
// copy of the audit JSONL, checkpoints JSONL, the daemon public key (PEM),
// and a signed manifest that ties them together.
//
// The bundle proves bundle-integrity and audit-chain integrity at export time.
// It does NOT constitute a global transparency-log anchoring claim. See
// BundleManifest's doc comment for details on what this verification proves
// and what it does not.
//
// The audit chain and checkpoint chain are verified before export; if either
// has integrity issues the export is aborted with an error.
func ExportAuditBundle(bundleDir string, opt *ExportBundleOptions) (*BundleManifest, error) {
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}

	bp := &BundlePaths{
		Dir:             bundleDir,
		ManifestPath:    filepath.Join(bundleDir, "manifest.json"),
		AuditPath:       filepath.Join(bundleDir, "audit.jsonl"),
		CheckpointsPath: filepath.Join(bundleDir, "audit.checkpoints"),
		PubKeyPath:      filepath.Join(bundleDir, "daemon.pub"),
	}

	// Verify audit chain integrity before export
	result, err := VerifyAuditChain(opt.AuditPath, opt.CheckpointPath, nil)
	if err != nil {
		return nil, fmt.Errorf("verify audit chain before export: %w", err)
	}
	if len(result.Issues) > 0 {
		return nil, fmt.Errorf("audit chain has issues: %v", result.Issues)
	}

	// Copy audit JSONL
	if err := copyFile(opt.AuditPath, bp.AuditPath); err != nil {
		return nil, fmt.Errorf("copy audit file: %w", err)
	}

	// Copy checkpoints
	if err := copyFile(opt.CheckpointPath, bp.CheckpointsPath); err != nil {
		return nil, fmt.Errorf("copy checkpoints file: %w", err)
	}

	// Compute public key fingerprint (SHA-256 of DER-encoded PKIX public key)
	pubKeyFingerprint := computePubKeyFingerprint(opt.PubKeyDER)

	// Write public key PEM
	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: opt.PubKeyDER,
	}
	if err := os.WriteFile(bp.PubKeyPath, pem.EncodeToMemory(pubBlock), 0644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	// Build manifest
	manifest := &BundleManifest{
		BundleVersion:     1,
		ExportTimestamp:   time.Now().UTC().Format(time.RFC3339),
		AuditRecordCount:  result.AuditRecordCount,
		AuditHeadSeq:      result.AuditHeadSeq,
		AuditHeadHash:     result.AuditHeadHash,
		CheckpointCount:   result.CheckpointCount,
		LatestCpSeq:       result.LatestAnchorSeq,
		LatestCpHash:      result.LatestAnchorHash,
		PubKeyFingerprint: pubKeyFingerprint,
	}

	// Compute manifest hash
	hash, err := manifest.computeManifestHash()
	if err != nil {
		return nil, fmt.Errorf("compute manifest hash: %w", err)
	}
	manifest.ManifestHash = hash

	// Sign the manifest
	if opt.SigningKey != nil {
		hashBytes := sha256.Sum256([]byte(hash))
		sig, err := ecdsa.SignASN1(rand.Reader, opt.SigningKey, hashBytes[:])
		if err != nil {
			return nil, fmt.Errorf("sign manifest: %w", err)
		}
		manifest.Signature = sig
	}

	// Write manifest
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(bp.ManifestPath, manifestData, 0644); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	return manifest, nil
}

// VerifyAuditBundleResult holds the outcome of verifying a bundle.
type VerifyAuditBundleResult struct {
	Manifest         *BundleManifest `json:"manifest,omitempty"`
	ManifestValid    bool            `json:"manifest_valid"`
	FingerprintMatch bool            `json:"fingerprint_match"`
	ChainResult      *VerificationResult
}

// VerifyAuditBundle verifies a previously exported audit bundle directory.
// It checks:
//  1. The manifest is parseable and self-consistent (ManifestHash matches).
//  2. The manifest signature is valid (if pubKey is non-nil).
//  3. The bundled public key fingerprint matches the manifest's fingerprint.
//  4. The bundled audit chain hash integrity via VerifyAuditChain.
//  5. Checkpoint signatures match the bundled public key.
//
// expectedFingerprint is the hex-encoded SHA-256 of the expected daemon
// audit public key's PKIX DER bytes. If empty, fingerprint checking is
// skipped (useful when the fingerprint is verified out-of-band).
//
// The result's ChainResult.Issues contains any audit chain or checkpoint
// chain integrity violations found.
func VerifyAuditBundle(bundleDir string, expectedFingerprint string, verifySignature bool) (*VerifyAuditBundleResult, error) {
	bp := &BundlePaths{
		Dir:             bundleDir,
		ManifestPath:    filepath.Join(bundleDir, "manifest.json"),
		AuditPath:       filepath.Join(bundleDir, "audit.jsonl"),
		CheckpointsPath: filepath.Join(bundleDir, "audit.checkpoints"),
		PubKeyPath:      filepath.Join(bundleDir, "daemon.pub"),
	}

	// Phase 1: Read and verify manifest
	manifestData, err := os.ReadFile(bp.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Verify manifest self-hash
	computedHash, err := manifest.computeManifestHash()
	if err != nil {
		return nil, fmt.Errorf("compute manifest hash: %w", err)
	}
	manifestValid := computedHash == manifest.ManifestHash

	// Phase 2: Read bundled public key
	pubKeyData, err := os.ReadFile(bp.PubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}

	pubBlock, _ := pem.Decode(pubKeyData)
	if pubBlock == nil {
		return nil, fmt.Errorf("decode public key PEM: no PEM block found")
	}

	rawPubKey, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pubKey, ok := rawPubKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ECDSA")
	}

	// Check fingerprint
	bundledFingerprint := computePubKeyFingerprint(pubBlock.Bytes)
	fingerprintMatch := bundledFingerprint == manifest.PubKeyFingerprint

	if expectedFingerprint != "" && bundledFingerprint != expectedFingerprint {
		// Fingerprint mismatch against expected — still continue checking
		// chain integrity but record the mismatch
		fingerprintMatch = false
	}

	// Phase 3: Verify manifest signature
	if verifySignature && manifestValid {
		if !manifest.VerifyManifestSignature(pubKey) {
			manifestValid = false
		}
	}

	// Phase 4: Verify audit chain and checkpoints against the bundled public key
	chainResult, err := VerifyAuditChain(bp.AuditPath, bp.CheckpointsPath, pubKey)
	if err != nil {
		return nil, fmt.Errorf("verify bundle chain: %w", err)
	}

	// Cross-check manifest metadata against chain result
	if manifestValid && chainResult != nil && len(chainResult.Issues) == 0 {
		// Check that manifest metadata matches the chain
		if manifest.AuditHeadSeq != chainResult.AuditHeadSeq {
			chainResult.Issues = append(chainResult.Issues, &CheckpointError{
				Type:    ErrTypeTamperMiddle,
				Message: fmt.Sprintf("manifest head seq %d does not match chain head seq %d", manifest.AuditHeadSeq, chainResult.AuditHeadSeq),
			})
		}
		if manifest.AuditHeadHash != chainResult.AuditHeadHash {
			chainResult.Issues = append(chainResult.Issues, &CheckpointError{
				Type:    ErrTypeTamperMiddle,
				Message: fmt.Sprintf("manifest head hash %q does not match chain head hash %q", manifest.AuditHeadHash, chainResult.AuditHeadHash),
			})
		}
	}

	return &VerifyAuditBundleResult{
		Manifest:         &manifest,
		ManifestValid:    manifestValid,
		FingerprintMatch: fingerprintMatch,
		ChainResult:      chainResult,
	}, nil
}

// PubKeyFingerprint computes the hex-encoded SHA-256 fingerprint of a PKIX
// DER-encoded public key. This is the value stored in a BundleManifest and
// used by offline verifiers to identify the expected daemon audit key.
func PubKeyFingerprint(pubKeyDER []byte) string {
	return computePubKeyFingerprint(pubKeyDER)
}

// computePubKeyFingerprint returns the hex-encoded SHA-256 digest of the
// given PKIX DER-encoded public key bytes.
func computePubKeyFingerprint(der []byte) string {
	hash := sha256.Sum256(der)
	return fmt.Sprintf("%x", hash)
}

// copyFile copies a file from src to dst. Both paths must exist and be
// regular files (symlinks are rejected for safety).
func copyFile(src, dst string) error {
	// Use Lstat to reject symlinks
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source is a symlink: %s", src)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", src)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	return dstFile.Sync()
}