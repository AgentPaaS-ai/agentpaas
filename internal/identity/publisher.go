package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Sentinel errors returned by publisher identity operations.
var (
	// ErrNoPublisherIdentity is returned when no publisher identity exists
	// in the keystore.
	ErrNoPublisherIdentity = errors.New("no publisher identity configured")

	// ErrPublisherIdentityExists is returned when attempting to create a
	// publisher identity that already exists.
	ErrPublisherIdentityExists = errors.New("publisher identity already exists")

	// ErrInvalidPublisherName is returned when a publisher name fails
	// validation.
	ErrInvalidPublisherName = errors.New("invalid publisher name")
)

// publisherNamePattern is the regex for valid publisher names:
// 1-39 chars, lowercase alphanumeric and hyphens, must start and end with
// alphanumeric (GitHub-style slug).
var publisherNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// ValidatePublisherName checks that name satisfies the publisher naming
// rules: 1-39 characters, lowercase alphanumeric and hyphens, must start
// and end with alphanumeric (GitHub-style slug). The name is a display
// label, NOT an identity — the fingerprint is the identity.
func ValidatePublisherName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidPublisherName)
	}
	if len(name) > 39 {
		return fmt.Errorf("%w: name length %d exceeds maximum 39", ErrInvalidPublisherName, len(name))
	}
	if !publisherNamePattern.MatchString(name) {
		return fmt.Errorf("%w: name %q must be lowercase alphanumeric with optional hyphens, starting and ending with alphanumeric", ErrInvalidPublisherName, name)
	}
	return nil
}

// PublisherIdentity holds the public-facing information about a publisher
// identity. It never contains private key material.
type PublisherIdentity struct {
	// Name is the display label for this publisher identity (a GitHub-style
	// slug). It is a label, NOT the identity — the Fingerprint is the
	// canonical identity.
	Name string `json:"name"`

	// Fingerprint is the hex-encoded SHA-256 of the DER-encoded SPKI of the
	// ECDSA P-256 public key (64 lowercase hex characters).
	Fingerprint string `json:"fingerprint"`

	// PublicKeyPEM is the PEM-encoded SPKI public key.
	PublicKeyPEM string `json:"public_key_pem"`

	// CreatedAt is the time the publisher identity was created.
	CreatedAt time.Time `json:"created_at"`
}

// PublisherFingerprint computes the SHA-256 fingerprint of an ECDSA public
// key as hex(sha256(DER-encoded SPKI)). This is the SAME computation as
// pack.PublicKeyFingerprint — it is redefined here in the identity package
// so that publisher identity operations do not need to import pack.
// Both produce the same 64-char lowercase hex string for the same key.
func PublisherFingerprint(pub *ecdsa.PublicKey) string {
	if pub == nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// FormatFingerprintDisplay formats a bare 64-hex fingerprint into display
// form: groups of 4 characters separated by spaces (16 groups of 4).
func FormatFingerprintDisplay(fp string) string {
	if len(fp) == 0 {
		return ""
	}
	var out strings.Builder
	for i := 0; i < len(fp); i++ {
		if i > 0 && i%4 == 0 {
			out.WriteByte(' ')
		}
		out.WriteByte(fp[i])
	}
	return out.String()
}

// ParseFingerprintDisplay removes all whitespace from a fingerprint string,
// converting display form back to bare 64-hex storage form. It also accepts
// bare hex (no spaces), returning it unchanged.
func ParseFingerprintDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

// CreatePublisherIdentity generates a new ECDSA P-256 key pair, stores the
// private key in the keystore, and returns the public identity information.
// The private key is never returned to callers. The publisher name is stored
// alongside the key material for display purposes.
//
// If a publisher identity already exists in the keystore, it returns
// ErrPublisherIdentityExists.
func CreatePublisherIdentity(ks KeyStore, name string) (*PublisherIdentity, error) {
	// Validate name.
	if err := ValidatePublisherName(name); err != nil {
		return nil, fmt.Errorf("create publisher identity: %w", err)
	}

	// Check if identity already exists.
	_, err := ks.Load(publisherIdentityKeyID)
	if err == nil {
		return nil, ErrPublisherIdentityExists
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return nil, fmt.Errorf("check existing publisher identity: %w", err)
	}

	// Generate ECDSA P-256 key pair.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate publisher key: %w", err)
	}

	// Marshal private key as SEC1 DER → PEM (same format as ca.go).
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal publisher key: %w", err)
	}
	pemBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	keyMaterial := KeyMaterial{
		Type:  KeyTypePublisher,
		Bytes: pem.EncodeToMemory(pemBlock),
	}

	// Store private key in keystore.
	if err := ks.Create(publisherIdentityKeyID, KeyTypePublisher, keyMaterial); err != nil {
		return nil, fmt.Errorf("store publisher key: %w", err)
	}

	// Store name as a separate keystore entry.
	nameMaterial := KeyMaterial{
		Type:  KeyTypePublisher,
		Bytes: []byte(name),
	}
	if err := ks.Create(publisherIdentityNameKeyID, KeyTypePublisher, nameMaterial); err != nil {
		// Clean up the key entry if name storage fails.
		_ = ks.Delete(publisherIdentityKeyID) // best-effort key delete on replace
		return nil, fmt.Errorf("store publisher name: %w", err)
	}

	// Encode public key as PEM.
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	now := time.Now()
	return &PublisherIdentity{
		Name:         name,
		Fingerprint:  PublisherFingerprint(&key.PublicKey),
		PublicKeyPEM: string(pubPEM),
		CreatedAt:    now,
	}, nil
}

// LoadPublisherIdentity loads the publisher identity from the keystore. It
// returns the public identity information only — the private key is never
// exposed. Returns ErrNoPublisherIdentity when no identity exists.
func LoadPublisherIdentity(ks KeyStore) (*PublisherIdentity, error) {
	// Load the private key material.
	km, err := ks.Load(publisherIdentityKeyID)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, ErrNoPublisherIdentity
		}
		return nil, fmt.Errorf("load publisher key: %w", err)
	}

	// Parse the private key.
	key, err := parseECDSAPrivateKey(km.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse publisher key: %w", err)
	}

	// Encode public key as PEM.
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	// Load name from the separate keystore entry (best-effort).
	name := ""
	nameMaterial, nameErr := ks.Load(publisherIdentityNameKeyID)
	if nameErr == nil {
		name = string(nameMaterial.Bytes)
	}

	// Get creation time from List (best-effort).
	var createdAt time.Time
	entries, listErr := ks.List()
	if listErr == nil {
		for _, e := range entries {
			if e.ID == publisherIdentityKeyID {
				createdAt = e.CreatedAt
				break
			}
		}
	}

	return &PublisherIdentity{
		Name:         name,
		Fingerprint:  PublisherFingerprint(&key.PublicKey),
		PublicKeyPEM: string(pubPEM),
		CreatedAt:    createdAt,
	}, nil
}

// SignAsPublisher signs the given digest using the publisher identity key
// stored in the keystore. The private key never leaves the keystore.
// Returns the ASN.1-encoded ECDSA signature.
// Returns ErrNoPublisherIdentity when no identity exists.
func SignAsPublisher(ks KeyStore, digest []byte) ([]byte, error) {
	sig, err := ks.Sign(publisherIdentityKeyID, digest)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, ErrNoPublisherIdentity
		}
		return nil, fmt.Errorf("sign as publisher: %w", err)
	}
	return sig, nil
}

// LoadPublisherSigningKey loads the publisher ECDSA private key for bundle manifest signing.
// Callers must not log or persist the returned key.
func LoadPublisherSigningKey(ks KeyStore) (*ecdsa.PrivateKey, error) {
	km, err := ks.Load(publisherIdentityKeyID)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, ErrNoPublisherIdentity
		}
		return nil, fmt.Errorf("load publisher key: %w", err)
	}
	key, err := parseECDSAPrivateKey(km.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse publisher key: %w", err)
	}
	return key, nil
}
