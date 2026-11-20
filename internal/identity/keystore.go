package identity

import (
	"errors"
	"fmt"
	"time"
)

// KeyID is a unique identifier for a key stored in the KeyStore.
type KeyID string

// ErrInvalidKeyID is returned when a KeyID fails validation. Implementations
// must reject any KeyID that does not satisfy the constraints enforced by
// ValidateKeyID.
var ErrInvalidKeyID = errors.New("invalid key ID")

// ValidateKeyID validates a KeyID against the KeyStore contract to prevent
// path-traversal, injection, and resource-exhaustion attacks in file-backed
// and DB-backed keystores (T02+). The following constraints are enforced:
//   - Non-empty
//   - Max 128 characters
//   - Allowed charset: [a-zA-Z0-9._-] only (no path separators, no unicode,
//     no control chars, no spaces)
//   - Must not be "." or ".."
//
// Returns ErrInvalidKeyID with an actionable message naming the violated rule.
func ValidateKeyID(id KeyID) error {
	s := string(id)
	if s == "" {
		return fmt.Errorf("%w: key ID must not be empty", ErrInvalidKeyID)
	}
	if s == "." || s == ".." {
		return fmt.Errorf("%w: key ID must not be %q", ErrInvalidKeyID, s)
	}
	if len(s) > 128 {
		return fmt.Errorf("%w: key ID length %d exceeds maximum 128", ErrInvalidKeyID, len(s))
	}
	for i, r := range s {
		if r > 127 {
			return fmt.Errorf("%w: key ID contains non-ASCII character %q at position %d", ErrInvalidKeyID, r, i)
		}
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return fmt.Errorf("%w: key ID contains disallowed character %q at position %d", ErrInvalidKeyID, r, i)
		}
	}
	return nil
}

// KeyType represents the purpose/type of a cryptographic key.
type KeyType string

const (
	// KeyTypeCA is the local certificate authority signing key. It is an
	// ECDSA P-256 key used to sign workload certificates.
	KeyTypeCA KeyType = "ca"

	// KeyTypeAuditSigning is the daemon audit signing key. It is an ECDSA
	// P-256 key used to sign audit checkpoint records.
	KeyTypeAuditSigning KeyType = "audit_signing"

	// KeyTypePackageIdentity is a per-agent package identity key. It is an
	// ECDSA P-256 key used to sign package manifests.
	KeyTypePackageIdentity KeyType = "package_identity"

	// KeyTypeWorkload is a per-run workload key/certificate pair. It
	// contains an x509 certificate and associated private key, used for
	// mTLS between gateway/harness and the daemon.
	KeyTypeWorkload KeyType = "workload"

	// KeyTypePublisher is the publisher identity signing key. It is an
	// ECDSA P-256 key used to sign shared agent bundles for distribution.
	// Unlike package identity keys (per-agent AIDs), the publisher key is
	// a single long-lived identity that spans all agents published by the
	// same operator.
	KeyTypePublisher KeyType = "publisher"
)

// signingKeyTypes returns true if kt is a key type that supports Sign/Verify
// operations (all ECDSA signing key types).
func signingKeyTypes(kt KeyType) bool {
	switch kt {
	case KeyTypeCA, KeyTypeAuditSigning, KeyTypePackageIdentity, KeyTypePublisher:
		return true
	default:
		return false
	}
}

// KeyMaterial holds the raw cryptographic material for a stored key. For
// signing keys (CA, AuditSigning, PackageIdentity) Bytes contains PEM-encoded
// ECDSA P-256 private key bytes. For workload keys Bytes contains the
// concatenation of the PEM-encoded certificate and PEM-encoded private key.
type KeyMaterial struct {
	Type  KeyType
	Bytes []byte
}

// KeyMetadata holds non-sensitive information about a stored key returned by
// List. It never contains raw key material, prefixes, or hashes.
type KeyMetadata struct {
	ID        KeyID     `json:"id"`
	Type      KeyType   `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	// RawBytes is never populated by List; it exists only so contract tests
	// can verify no raw material leaks through the metadata API.
	RawBytes []byte `json:"-"`
}

// KeyStore is a narrow interface for storing, loading, and using
// cryptographic keys. Implementations must never expose raw key material
// through the List() method.
//
// Operations:
//   - Create stores key material under the given ID and type.
//   - Load retrieves key material by ID.
//   - Sign computes a signature over the given digest using the key's
//     private key (only valid for signing key types).
//   - Verify checks a signature against the given digest using the key's
//     public key (only valid for signing key types).
//   - Delete removes a key by ID.
//   - List returns metadata for all stored keys, never raw material.
type KeyStore interface {
	Create(id KeyID, kt KeyType, material KeyMaterial) error
	Load(id KeyID) (KeyMaterial, error)
	Sign(id KeyID, digest []byte) ([]byte, error)
	Verify(id KeyID, digest []byte, signature []byte) bool
	Delete(id KeyID) error
	List() ([]KeyMetadata, error)
}

// IdentityIssuer issues SPIFFE-style workload certificates. The full
// SPIFFE URI builder and verifier is implemented in B3-T03; this interface
// is defined now so that consumers can depend on it.
type IdentityIssuer interface {
	// IssueWorkloadCert generates a workload certificate and key for the
	// given agent run. It returns the PEM-encoded certificate, PEM-encoded
	// private key, and the SPIFFE URI for the identity.
	IssueWorkloadCert(agentName, agentVersion, runID string, ttl time.Duration) (certPEM, keyPEM []byte, spiffeURI string, err error)
}