// Package identity — macOS Keychain–backed KeyStore.
//
// KeychainKeyStore stores key material in the macOS (i.e. Darwin) system
// keychain via the security(1) CLI.  On non-Darwin platforms the constructor
// returns an error at runtime; the type and package always compile so that
// consumers do not need build-tag gymnastics.

package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrKeychainLocked is returned when a security(1) command fails because the
// keychain is locked or the user is not authenticated.  The caller should
// prompt the user to unlock the keychain and retry.  There is NO silent
// plaintext fallback.
var ErrKeychainLocked = errors.New("macOS keychain is locked or unavailable")

// ---------------------------------------------------------------------------
// Internal on-disk entry format
// ---------------------------------------------------------------------------

// keychainEntry is the JSON payload stored as the generic-password value for
// each key.  It mirrors fileStoreEntry but lives inside the system keychain
// instead of an encrypted file.
type keychainEntry struct {
	Type      string    `json:"type"`
	BytesB64  string    `json:"bytes_b64"`
	CreatedAt time.Time `json:"created_at"`
}

// manifestKey is the account name used for the index-of-key-IDs entry.
// It must pass ValidateKeyID — it does (alphanumeric + underscore).
const manifestKey = "_index"

// ---------------------------------------------------------------------------
// KeychainKeyStore
// ---------------------------------------------------------------------------

// KeychainKeyStore implements the KeyStore interface backed by the macOS
// system keychain.  Key material is stored as generic-password items in the
// user's default keychain (usually login.keychain-db).
//
// A special manifest entry (account "_index") keeps a JSON array of all key
// IDs so that List() can be implemented efficiently without parsing the
// text output of security(1).
type KeychainKeyStore struct {
	service string
}

// NewKeychainKeyStore creates a new KeychainKeyStore for the given service
// name.  On non-Darwin platforms it returns an error; on Darwin it returns
// a ready-to-use store.
func NewKeychainKeyStore(service string) (*KeychainKeyStore, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("keychain not available on this OS")
	}
	if service == "" {
		return nil, errors.New("keychain service name must not be empty")
	}
	return &KeychainKeyStore{service: service}, nil
}

// ---------------------------------------------------------------------------
// security(1) helpers
// ---------------------------------------------------------------------------

// securityCall runs "security <args…>" and returns its combined output.
// On success the output is trimmed of trailing newlines and returned.
// On failure the error is classified:
//
//   - "item could not be found" / "no matching" → ErrKeyNotFound
//   - "locked" / "unlock" / "authenticated"   → ErrKeychainLocked
//   - anything else                            → generic wrapped error
func (k *KeychainKeyStore) securityCall(args ...string) (string, error) {
	cmd := exec.Command("security", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		low := strings.ToLower(msg)
		if strings.Contains(low, "item could not be found") || strings.Contains(low, "no matching") {
			return "", ErrKeyNotFound
		}
		if strings.Contains(low, "locked") || strings.Contains(low, "unlock") || strings.Contains(low, "authenticated") {
			return "", ErrKeychainLocked
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("keychain operation: %s", msg)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// ---------------------------------------------------------------------------
// Manifest helpers
// ---------------------------------------------------------------------------

// loadManifest reads the JSON list of key IDs stored under the manifest
// account.  If the manifest does not exist (first use) an empty list is
// returned — the error is swallowed.
func (k *KeychainKeyStore) loadManifest() []string {
	out, err := k.securityCall("find-generic-password", "-a", manifestKey, "-s", k.service, "-w")
	if err != nil {
		// Item-not-found on the manifest is expected on first use.
		return []string{}
	}
	var manifest []string
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		return []string{} // corrupted — start fresh
	}
	return manifest
}

// saveManifest persists the list of key IDs under the manifest account.
func (k *KeychainKeyStore) saveManifest(manifest []string) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	_, err = k.securityCall("add-generic-password", "-a", manifestKey, "-s", k.service, "-w", string(data), "-U")
	return err
}

// addToManifest appends keyID to the manifest and persists it.  No-op if
// already present.
func (k *KeychainKeyStore) addToManifest(keyID string) error {
	manifest := k.loadManifest()
	for _, id := range manifest {
		if id == keyID {
			return nil // already present
		}
	}
	manifest = append(manifest, keyID)
	return k.saveManifest(manifest)
}

// removeFromManifest deletes keyID from the manifest and persists it.
// No-op if not present.
func (k *KeychainKeyStore) removeFromManifest(keyID string) error {
	manifest := k.loadManifest()
	filtered := make([]string, 0, len(manifest))
	for _, id := range manifest {
		if id != keyID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == len(manifest) {
		return nil // not found — nothing to remove
	}
	if len(filtered) == 0 {
		// Remove the manifest entry entirely when the last key is gone.
		_, _ = k.securityCall("delete-generic-password", "-a", manifestKey, "-s", k.service)
		return nil
	}
	return k.saveManifest(filtered)
}

// ---------------------------------------------------------------------------
// KeyStore interface implementation
// ---------------------------------------------------------------------------

// Create stores key material under the given ID and type.  It returns
// ErrKeyAlreadyExists if a key with that ID already exists, or
// ErrInvalidKeyID if the ID fails validation.
func (k *KeychainKeyStore) Create(id KeyID, kt KeyType, material KeyMaterial) error {
	if err := ValidateKeyID(id); err != nil {
		return err
	}

	// Check manifest first to give a clean ErrKeyAlreadyExists.
	for _, existing := range k.loadManifest() {
		if existing == string(id) {
			return ErrKeyAlreadyExists
		}
	}

	entry := keychainEntry{
		Type:      string(kt),
		BytesB64:  b64Encode(material.Bytes),
		CreatedAt: time.Now(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal keychain entry: %w", err)
	}

	if _, err := k.securityCall("add-generic-password", "-a", string(id), "-s", k.service, "-w", string(data)); err != nil {
		return err
	}

	return k.addToManifest(string(id))
}

// Load retrieves key material for the given ID.  Returns ErrKeyNotFound if
// the key does not exist, or ErrInvalidKeyID if the ID fails validation.
func (k *KeychainKeyStore) Load(id KeyID) (KeyMaterial, error) {
	if err := ValidateKeyID(id); err != nil {
		return KeyMaterial{}, err
	}

	out, err := k.securityCall("find-generic-password", "-a", string(id), "-s", k.service, "-w")
	if err != nil {
		return KeyMaterial{}, err
	}

	var entry keychainEntry
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		return KeyMaterial{}, fmt.Errorf("parse keychain entry: %w", err)
	}

	bytes, err := b64Decode(entry.BytesB64)
	if err != nil {
		return KeyMaterial{}, fmt.Errorf("decode key material: %w", err)
	}

	return KeyMaterial{Type: KeyType(entry.Type), Bytes: bytes}, nil
}

// Sign computes an ECDSA signature over the given digest using the key
// identified by id.  Only signing key types (CA, AuditSigning,
// PackageIdentity) are supported; Workload keys return ErrWrongKeyType.
func (k *KeychainKeyStore) Sign(id KeyID, digest []byte) ([]byte, error) {
	mat, err := k.Load(id)
	if err != nil {
		return nil, err
	}
	if !signingKeyTypes(mat.Type) {
		return nil, ErrWrongKeyType
	}
	key, err := parseECDSAPrivateKey(mat.Bytes)
	if err != nil {
		return nil, err
	}
	return ecdsaSign(key, digest)
}

// Verify checks an ECDSA signature over the given digest against the key
// identified by id.  Returns false if the key is not found, is a non-signing
// key type, or if the ID fails validation.
func (k *KeychainKeyStore) Verify(id KeyID, digest []byte, signature []byte) bool {
	mat, err := k.Load(id)
	if err != nil {
		return false
	}
	if !signingKeyTypes(mat.Type) {
		return false
	}
	key, err := parseECDSAPublicKey(mat.Bytes)
	if err != nil {
		return false
	}
	return ecdsaVerify(key, digest, signature)
}

// Delete removes the key with the given ID.  Returns ErrKeyNotFound if the
// key does not exist, or ErrInvalidKeyID if the ID fails validation.
func (k *KeychainKeyStore) Delete(id KeyID) error {
	if err := ValidateKeyID(id); err != nil {
		return err
	}

	if _, err := k.securityCall("delete-generic-password", "-a", string(id), "-s", k.service); err != nil {
		return err
	}

	return k.removeFromManifest(string(id))
}

// List returns metadata for all stored keys.  The returned KeyMetadata
// entries never contain raw key material (RawBytes is always nil).
func (k *KeychainKeyStore) List() ([]KeyMetadata, error) {
	ids := k.loadManifest()
	result := make([]KeyMetadata, 0, len(ids))
	for _, id := range ids {
		out, err := k.securityCall("find-generic-password", "-a", id, "-s", k.service, "-w")
		if err != nil {
			// Entry disappeared between loading manifest and reading it —
			// skip silently.
			continue
		}
		var entry keychainEntry
		if err := json.Unmarshal([]byte(out), &entry); err != nil {
			continue
		}
		result = append(result, KeyMetadata{
			ID:        KeyID(id),
			Type:      KeyType(entry.Type),
			CreatedAt: entry.CreatedAt,
		})
	}
	return result, nil
}

// Compile-time check that KeychainKeyStore satisfies the KeyStore interface.
var _ KeyStore = (*KeychainKeyStore)(nil)