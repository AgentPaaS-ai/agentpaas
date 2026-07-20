package bundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// Verify performs offline verification of an opened bundle. All nine checks
// from the Block 22 T01 spec are evaluated; Verified is true only when every
// check passes.
func Verify(b *Bundle) (*VerifyReport, error) {
	if b == nil {
		return nil, errors.New("bundle must not be nil")
	}

	report := &VerifyReport{Verified: true}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, VerifyCheck{Name: name, Passed: passed, Detail: detail})
		if !passed {
			report.Verified = false
		}
	}

	// 1. manifest_parse
	if b.Manifest == nil {
		add(CheckManifestParse, false, "manifest is nil")
	} else if b.Manifest.BundleSchemaVersion != BundleSchemaVersion {
		add(CheckManifestParse, false, fmt.Sprintf("unsupported bundle_schema_version %d", b.Manifest.BundleSchemaVersion))
	} else if strings.TrimSpace(b.Manifest.Publisher.PublicKeyPEM) == "" {
		add(CheckManifestParse, false, "manifest publisher public_key_pem is empty")
	} else if strings.TrimSpace(b.Manifest.Publisher.Fingerprint) == "" {
		add(CheckManifestParse, false, "manifest publisher fingerprint is empty")
	} else {
		add(CheckManifestParse, true, "manifest parsed")
	}

	// 2. manifest_signature
	if b.Manifest != nil {
		if err := verifyManifestSignature(b.Manifest); err != nil {
			add(CheckManifestSignature, false, err.Error())
		} else {
			add(CheckManifestSignature, true, "manifest signature valid")
		}
	} else {
		add(CheckManifestSignature, false, "manifest missing")
	}

	// 3. publisher_match (manifest publisher vs lock publisher when lock has publisher block)
	if b.Manifest == nil || b.Lock == nil {
		add(CheckPublisherMatch, false, "manifest or lock missing")
	} else if b.Lock.Publisher == nil {
		add(CheckPublisherMatch, true, "lock has no publisher block")
	} else {
		mp := b.Manifest.Publisher
		lp := b.Lock.Publisher
		match := mp.Name == lp.Name &&
			mp.Fingerprint == lp.Fingerprint &&
			mp.PublicKeyPEM == lp.PublicKeyPEM
		if !match {
			add(CheckPublisherMatch, false, "manifest publisher does not match lock publisher")
		} else {
			add(CheckPublisherMatch, true, "manifest and lock publisher match")
		}
	}

	// 4. lock_provenance
	if b.Lock == nil {
		add(CheckLockProvenance, false, "lock missing")
	} else {
		var provDetail strings.Builder
		ok := true
		if err := pack.VerifyLockfileSignature(b.Lock); err != nil {
			ok = false
			provDetail.WriteString("lockfile signature: " + err.Error())
		}
		if b.Lock.SchemaVersion >= 2 && b.Lock.Publisher != nil {
			if err := pack.VerifyPublisherSignature(b.Lock); err != nil {
				ok = false
				if provDetail.Len() > 0 {
					provDetail.WriteString("; ")
				}
				provDetail.WriteString("publisher signature: " + err.Error())
			}
			if err := pack.VerifyProvenanceSignatures(b.Lock); err != nil {
				ok = false
				if provDetail.Len() > 0 {
					provDetail.WriteString("; ")
				}
				provDetail.WriteString("provenance signatures: " + err.Error())
			}
		}
		provReport, err := pack.VerifyProvenance(b.Lock)
		if err != nil {
			ok = false
			if provDetail.Len() > 0 {
				provDetail.WriteString("; ")
			}
			provDetail.WriteString("provenance: " + err.Error())
		} else if provReport != nil && !provReport.Verified {
			ok = false
			if provDetail.Len() > 0 {
				provDetail.WriteString("; ")
			}
			provDetail.WriteString("provenance chain invalid")
		}
		if ok {
			add(CheckLockProvenance, true, "lock and provenance verified")
		} else {
			add(CheckLockProvenance, false, provDetail.String())
		}
	}

	// 5. content_sha256 (manifest content digests vs raw bundle bytes)
	if b.Manifest == nil {
		add(CheckContentSHA256, false, "manifest missing")
	} else {
		contentOK := true
		var detail strings.Builder
		checkEntry := func(label string, want string, got string) {
			if want != got {
				contentOK = false
				if detail.Len() > 0 {
					detail.WriteString("; ")
				}
				detail.WriteString(label + " digest mismatch")
			}
		}
		if len(b.LockJSON) == 0 {
			contentOK = false
			detail.WriteString("lock bytes missing")
		} else {
			checkEntry("lock", b.Manifest.Contents.Lock.Digest, sha256Hex(b.LockJSON))
		}
		checkEntry("policy", b.Manifest.Contents.Policy.Digest, sha256Hex(b.PolicyYAML))
		checkEntry("sbom", b.Manifest.Contents.SBOM.Digest, sha256Hex(b.SBOM))
		if contentOK {
			add(CheckContentSHA256, true, "lock, policy, and sbom digests match manifest")
		} else {
			add(CheckContentSHA256, false, detail.String())
		}
	}

	// 6. policy_digest (lock policy_digest vs canonical policy digest of sidecar bytes)
	if b.Lock == nil {
		add(CheckPolicyDigest, false, "lock missing")
	} else {
		computed, err := pack.ComputePolicyDigest(b.PolicyYAML)
		if err != nil {
			add(CheckPolicyDigest, false, err.Error())
		} else if computed != b.Lock.PolicyDigest {
			add(CheckPolicyDigest, false, fmt.Sprintf("lock policy_digest %q != computed %q", b.Lock.PolicyDigest, computed))
		} else {
			add(CheckPolicyDigest, true, "policy digest matches lock")
		}
	}

	// 7. sbom_digest (lock sbom_digest vs SHA-256 of sbom bytes)
	if b.Lock == nil {
		add(CheckSBOMDigest, false, "lock missing")
	} else {
		got := sha256Hex(b.SBOM)
		if b.Lock.SBOMDigest != got {
			add(CheckSBOMDigest, false, fmt.Sprintf("lock sbom_digest %q != content %q", b.Lock.SBOMDigest, got))
		} else {
			add(CheckSBOMDigest, true, "sbom digest matches lock")
		}
	}

	// 8. source_digest (manifest source digest vs build-context digest from source/ tree)
	if b.Manifest == nil {
		add(CheckSourceDigest, false, "manifest missing")
	} else {
		computed, err := computeSourceDigestFromBundle(b)
		if err != nil {
			add(CheckSourceDigest, false, err.Error())
		} else if computed != b.Manifest.Contents.Source.Digest {
			add(CheckSourceDigest, false, fmt.Sprintf("manifest source digest %q != computed %q", b.Manifest.Contents.Source.Digest, computed))
		} else if b.Lock != nil && b.Lock.BuildInputDigest != computed {
			add(CheckSourceDigest, false, fmt.Sprintf("lock build_input_digest %q != computed %q", b.Lock.BuildInputDigest, computed))
		} else {
			add(CheckSourceDigest, true, "source digest matches manifest and lock")
		}
	}

	// 9. image_digest (optional OCI image/index.json)
	if b.Manifest == nil {
		add(CheckImageDigest, false, "manifest missing")
	} else if b.Manifest.Contents.Image == nil {
		add(CheckImageDigest, true, "no image in bundle")
	} else {
		indexData, err := readBundleTarFile(b, filepath.ToSlash(filepath.Join(ImagePrefix, "index.json")))
		if err != nil {
			add(CheckImageDigest, false, err.Error())
		} else if sha256Hex(indexData) != b.Manifest.Contents.Image.Digest {
			add(CheckImageDigest, false, "image index digest mismatch")
		} else {
			add(CheckImageDigest, true, "image digest matches manifest")
		}
	}

	return report, nil
}

func verifyManifestSignature(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if strings.TrimSpace(m.ManifestSignature) == "" {
		return errors.New("manifest_signature is empty")
	}
	pub, err := pack.PublicKeyFromPEM([]byte(m.Publisher.PublicKeyPEM))
	if err != nil {
		return fmt.Errorf("parse publisher public key: %w", err)
	}
	if identity.PublisherFingerprint(pub) != m.Publisher.Fingerprint {
		return errors.New("manifest publisher fingerprint does not match public key")
	}
	sig, err := base64.StdEncoding.DecodeString(m.ManifestSignature)
	if err != nil {
		return fmt.Errorf("decode manifest signature: %w", err)
	}
	canonical, err := manifestCanonicalJSON(m, false)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return errors.New("manifest signature verification failed")
	}
	return nil
}

func computeSourceDigestFromBundle(b *Bundle) (string, error) {
	if b == nil {
		return "", errors.New("bundle is nil")
	}
	if len(b.sourceMeta) == 0 {
		// Empty source tree: digest over zero files.
		return pack.ComputeBuildInputDigestFromFiles(nil)
	}
	tmpDir, err := os.MkdirTemp("", "agentpaas-bundle-verify-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort remove

	if err := b.ExtractSource(tmpDir); err != nil {
		return "", fmt.Errorf("extract source: %w", err)
	}
	// Use the .agentpaasignore from the extracted source if present,
	// matching what pack used. CollectBuildFiles with nil will call
	// LoadIgnore(tmpDir) which won't find the bundled .agentpaasignore
	// (it's at the root of the extracted files, not necessarily read
	// by LoadIgnore). Pass an explicit ignore matcher to ensure
	// consistency with pack's digest computation.
	ignore, err := pack.LoadIgnore(tmpDir)
	if err != nil {
		return "", fmt.Errorf("load ignore for verification: %w", err)
	}
	files, err := pack.CollectBuildFiles(tmpDir, ignore)
	if err != nil {
		return "", fmt.Errorf("collect source files: %w", err)
	}
	return pack.ComputeBuildInputDigestFromFiles(files)
}

func readBundleTarFile(b *Bundle, name string) ([]byte, error) {
	if b == nil || b.raw == nil {
		return nil, errors.New("bundle file handle is not available")
	}
	if _, err := b.raw.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek bundle: %w", err)
	}
	gzReader, err := gzip.NewReader(b.raw)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }() // best-effort close
	tr := tar.NewReader(gzReader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Name != name {
			if hdr.Size > 0 {
				if _, err := io.CopyN(io.Discard, tr, hdr.Size); err != nil {
					return nil, err
				}
			}
			continue
		}
		if hdr.Size < 0 {
			return nil, fmt.Errorf("invalid size for %s", name)
		}
		data := make([]byte, hdr.Size)
		if _, err := io.ReadFull(tr, data); err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("entry %q not found in bundle", name)
}