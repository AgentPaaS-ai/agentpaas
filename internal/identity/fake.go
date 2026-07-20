package identity

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors returned by KeyStore implementations.
var (
	ErrKeyNotFound      = errors.New("key not found")
	ErrKeyAlreadyExists = errors.New("key already exists")
	ErrWrongKeyType     = errors.New("operation not supported for this key type")
)

// storedKey holds the in-memory representation of a key.
type storedKey struct {
	material  KeyMaterial
	createdAt time.Time
}

// FakeKeyStore is an in-memory implementation of KeyStore for testing. It
// stores key material in a map protected by a read-write mutex.
//
// Sign and Verify use the Go standard library's crypto/ecdsa with P-256.
// Workload keys are rejected for Sign operations with ErrWrongKeyType.
type FakeKeyStore struct {
	mu   sync.RWMutex
	keys map[KeyID]storedKey
}

// NewFakeKeyStore returns a ready-to-use FakeKeyStore.
func NewFakeKeyStore() *FakeKeyStore {
	return &FakeKeyStore{
		keys: make(map[KeyID]storedKey),
	}
}

// Create stores the key material under the given ID and type. It returns
// ErrKeyAlreadyExists if a key with that ID already exists, or
// ErrInvalidKeyID if the ID fails validation.
func (f *FakeKeyStore) Create(id KeyID, kt KeyType, material KeyMaterial) error {
	if err := ValidateKeyID(id); err != nil {
		return fmt.Errorf("fake key store create: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.keys[id]; exists {
		return ErrKeyAlreadyExists
	}
	f.keys[id] = storedKey{
		material:  KeyMaterial{Type: kt, Bytes: material.Bytes},
		createdAt: time.Now(),
	}
	return nil
}

// Load retrieves the key material for the given ID. It returns
// ErrKeyNotFound if the key does not exist, or ErrInvalidKeyID if the ID
// fails validation.
func (f *FakeKeyStore) Load(id KeyID) (KeyMaterial, error) {
	if err := ValidateKeyID(id); err != nil {
		return KeyMaterial{}, fmt.Errorf("fake key store load: %w", err)
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	sk, ok := f.keys[id]
	if !ok {
		return KeyMaterial{}, ErrKeyNotFound
	}
	return sk.material, nil
}

// Sign computes an ECDSA signature over the given digest using the key
// identified by id. Only signing key types (CA, AuditSigning,
// PackageIdentity) are supported; Workload keys return ErrWrongKeyType.
// Returns ErrKeyNotFound if the key does not exist, or ErrInvalidKeyID if
// the ID fails validation.
func (f *FakeKeyStore) Sign(id KeyID, digest []byte) ([]byte, error) {
	if err := ValidateKeyID(id); err != nil {
		return nil, fmt.Errorf("fake key store sign: %w", err)
	}
	f.mu.RLock()
	sk, ok := f.keys[id]
	f.mu.RUnlock()
	if !ok {
		return nil, ErrKeyNotFound
	}
	if !signingKeyTypes(sk.material.Type) {
		return nil, ErrWrongKeyType
	}
	key, err := parseECDSAPrivateKey(sk.material.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fake key store sign: %w", err)
	}
	// Deterministic RFC 6979 ECDSA signing via crypto/ecdsa.
	return ecdsa.SignASN1(testRandReader{}, key, digest)
}

// Verify checks an ECDSA signature over the given digest against the key
// identified by id. It returns false if the key is not found, is a
// non-signing key type, or if the ID fails validation.
func (f *FakeKeyStore) Verify(id KeyID, digest []byte, signature []byte) bool {
	if err := ValidateKeyID(id); err != nil {
		return false
	}
	f.mu.RLock()
	sk, ok := f.keys[id]
	f.mu.RUnlock()
	if !ok {
		return false
	}
	if !signingKeyTypes(sk.material.Type) {
		return false
	}
	key, err := parseECDSAPublicKey(sk.material.Bytes)
	if err != nil {
		return false
	}
	return ecdsa.VerifyASN1(key, digest, signature)
}

// Delete removes the key with the given ID. It returns ErrKeyNotFound if
// the key does not exist, or ErrInvalidKeyID if the ID fails validation.
func (f *FakeKeyStore) Delete(id KeyID) error {
	if err := ValidateKeyID(id); err != nil {
		return fmt.Errorf("fake key store delete: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.keys[id]; !exists {
		return ErrKeyNotFound
	}
	delete(f.keys, id)
	return nil
}

// List returns metadata for all stored keys. The returned KeyMetadata
// entries never contain raw key material (RawBytes is always nil).
func (f *FakeKeyStore) List() ([]KeyMetadata, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]KeyMetadata, 0, len(f.keys))
	for id, sk := range f.keys {
		result = append(result, KeyMetadata{
			ID:        id,
			Type:      sk.material.Type,
			CreatedAt: sk.createdAt,
		})
	}
	return result, nil
}

// testRandReader is a zero-reader used for deterministic signing in tests.
// In production, crypto/rand.Reader should be used.
type testRandReader struct{}

// testRandReader.Read fills b with a fixed byte pattern for deterministic tests.
func (testRandReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0x42
	}
	return len(b), nil
}

// parseECDSAPrivateKey decodes a PEM-encoded ECDSA private key.
func parseECDSAPrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes) // optional value; zero on miss
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ecdsaprivate key: %w", err)
	}
	return key, nil
}

// parseECDSAPublicKey decodes a PEM-encoded ECDSA private key and extracts
// the corresponding public key.
func parseECDSAPublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	priv, err := parseECDSAPrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse ecdsapublic key: %w", err)
	}
	return &priv.PublicKey, nil
}

// Compile-time check that FakeKeyStore satisfies the KeyStore interface.
var _ KeyStore = (*FakeKeyStore)(nil)
