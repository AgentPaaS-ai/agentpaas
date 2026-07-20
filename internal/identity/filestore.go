package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// File KV store format
// ---------------------------------------------------------------------------
//
// The keystore is a single JSON file at <dir>/keystore.json. The file is
// encrypted with AES-256-GCM using a key derived from the passphrase via
// PBKDF2-HMAC-SHA256 (100_000 iterations, 32-byte salt). The on-disk format
// is:
//
//	salt (32 bytes) || nonce (12 bytes) || ciphertext (variable)
//
// The ciphertext is the JSON-serialised plaintext encrypted with AES-256-GCM.
// Plaintext schema:
//
//	{
//	  "version": 1,
//	  "keys": {
//	    "<keyID>": {
//	      "type": "ca",
//	      "bytes_b64": "<base64 PEM material>",
//	      "created_at": "2025-01-01T00:00:00Z"
//	    }
//	  }
//	}
//
// Permission rule: the store file MUST have permissions 0600 or 0400
// (owner-only read/write or read-only). Any world-accessible or group-
// accessible permission (0644, 0755, etc.) causes the store to refuse
// loading with an actionable error. This prevents accidental credential
// exposure via misconfigured file modes.

const (
	// fileStoreVersion is the schema version for the encrypted store.
	fileStoreVersion = 1

	// pbkdf2Iterations is the iteration count for PBKDF2 key derivation.
	pbkdf2Iterations = 100_000

	// saltLen is the length of the random salt in bytes.
	saltLen = 32

	// nonceLen is the length of the AES-GCM nonce in bytes.
	nonceLen = 12

	// keyLen is the AES-256 key length in bytes.
	keyLen = 32

	// storeFileName is the name of the encrypted keystore file.
	storeFileName = "keystore.json"
)

// fileStoreEntry is a single key entry in the store.
type fileStoreEntry struct {
	Type      string    `json:"type"`
	BytesB64  string    `json:"bytes_b64"`
	CreatedAt time.Time `json:"created_at"`
}

// fileStorePlaintext is the plaintext structure that gets encrypted.
type fileStorePlaintext struct {
	Version int                       `json:"version"`
	Keys    map[string]fileStoreEntry `json:"keys"`
}

// FileKeyStore is an encrypted, passphrase-protected KeyStore implementation
// that stores keys in a single JSON file on disk. The file is encrypted with
// AES-256-GCM and the encryption key is derived from a passphrase via
// PBKDF2-HMAC-SHA256.
//
// Permission rule: the store file must be 0600 or 0400. Weaker permissions
// cause the store to refuse loading with ErrWeakPermissions.
//
// WARNING: FileKeyStore is a P1-approved fallback for environments where the
// macOS Keychain is unavailable. It is NOT a plaintext fallback — all data
// is encrypted at rest.
type FileKeyStore struct {
	mu         sync.RWMutex
	dir        string
	passphrase string
	keys       map[string]fileStoreEntry
	filePath   string
}

// ErrWeakPermissions is returned when the keystore file has permissions
// weaker than 0600 (e.g., world-readable 0644 or 0755).
var ErrWeakPermissions = errors.New("keystore file has weak permissions; expected 0600 or 0400")

// fileLocks provides per-file-path locking to prevent concurrent access
// to the same keystore file across multiple FileKeyStore instances within
// a single process (P1 daemon model). Cross-process locking is deferred to P2.
var fileLocks sync.Map // map[string]*sync.Mutex

// getFileLock returns or creates a mutex for the given file path. The mutex
// is keyed by the absolute path so that two stores pointing at the same file
// (via different relative paths) share the same lock.
func getFileLock(path string) *sync.Mutex {
	abs, _ := filepath.Abs(path)                      // best-effort abs; fallback to path
	v, _ := fileLocks.LoadOrStore(abs, &sync.Mutex{}) // sync.Map always ok
	return v.(*sync.Mutex)
}

// ErrWrongPassphrase is returned when the passphrase does not decrypt the
// keystore file correctly (authentication tag mismatch).
var ErrWrongPassphrase = errors.New("wrong passphrase or corrupted keystore file")

// ErrSymlinkDetected is returned when the keystore file is a symlink.
// Reading or writing through a symlink is refused to prevent TOCTOU attacks
// where an attacker replaces the keystore with a symlink to an arbitrary path.
var ErrSymlinkDetected = errors.New("keystore file is a symlink; refusing to read/write through symlink")

// NewFileKeyStore opens or creates an encrypted file keystore at the given
// directory. If the store file already exists, it is decrypted and loaded.
// If the directory does not exist, it is created. The passphrase is used to
// derive the encryption key.
//
// The store file is created with 0600 permissions. If an existing file has
// permissions weaker than 0600 (e.g., 0644 or 0755), ErrWeakPermissions is
// returned — no data is loaded.
//
// If the existing file cannot be decrypted (wrong passphrase or corruption),
// ErrWrongPassphrase is returned — no data is loaded.
func NewFileKeyStore(dir, passphrase string) (*FileKeyStore, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase must not be empty")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create keystore directory: %w", err)
	}

	fp := filepath.Join(dir, storeFileName)
	s := &FileKeyStore{
		dir:        dir,
		passphrase: passphrase,
		keys:       make(map[string]fileStoreEntry),
		filePath:   fp,
	}

	// Try to load existing store. Acquire file lock to avoid reading a
	// partially-written file from another instance.
	flock := getFileLock(fp)
	flock.Lock()
	defer flock.Unlock()

	if _, err := os.Stat(fp); err == nil {
		if err := s.loadFromDisk(); err != nil {
			return nil, fmt.Errorf("new file key store: %w", err)
		}
	}

	return s, nil
}

// Close synchronises the in-memory state to disk and releases resources.
// After Close, the store must not be used.
func (f *FileKeyStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveToDisk()
}

// loadFromDisk reads the encrypted store file, decrypts it, and populates
// the in-memory key map. The caller must hold f.mu.
func (f *FileKeyStore) loadFromDisk() error {
	// Check for symlink attack: use Lstat to detect symlinks.
	fi, err := os.Lstat(f.filePath)
	if err != nil {
		return fmt.Errorf("stat keystore file: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return ErrSymlinkDetected
	}

	// Check file permissions. Since Lstat confirmed this is not a symlink,
	// the Mode from Lstat reflects the actual file's permission bits.
	if err := checkPermissions(fi.Mode()); err != nil {
		return fmt.Errorf("file key store load from disk: %w", err)
	}

	data, err := os.ReadFile(f.filePath)
	if err != nil {
		return fmt.Errorf("read keystore file: %w", err)
	}

	if len(data) < saltLen+nonceLen+1 {
		return ErrWrongPassphrase
	}

	salt := data[:saltLen]
	nonce := data[saltLen : saltLen+nonceLen]
	ciphertext := data[saltLen+nonceLen:]

	key, err := deriveKey(f.passphrase, salt)
	if err != nil {
		return fmt.Errorf("derive key: %w", err)
	}
	plaintext, err := decryptAESGCM(key, nonce, ciphertext)
	if err != nil {
		return ErrWrongPassphrase
	}

	var pt fileStorePlaintext
	if err := json.Unmarshal(plaintext, &pt); err != nil {
		return fmt.Errorf("decrypt keystore: parse error: %w", err)
	}

	if pt.Version != fileStoreVersion {
		return fmt.Errorf("unsupported keystore version %d", pt.Version)
	}

	f.keys = pt.Keys
	return nil
}

// saveToDisk serialises the in-memory state and encrypts it to disk.
// The caller must hold f.mu.
func (f *FileKeyStore) saveToDisk() error {
	pt := fileStorePlaintext{
		Version: fileStoreVersion,
		Keys:    f.keys,
	}
	plaintext, err := json.Marshal(pt)
	if err != nil {
		return fmt.Errorf("marshal keystore: %w", err)
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	key, err := deriveKey(f.passphrase, salt)
	if err != nil {
		return fmt.Errorf("derive key: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext, err := encryptAESGCM(key, nonce, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt keystore: %w", err)
	}

	out := make([]byte, 0, saltLen+nonceLen+len(ciphertext))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)

	// Check for symlink attack before writing.
	fi, err := os.Lstat(f.filePath)
	if err == nil {
		// File exists — check if it's a symlink.
		if fi.Mode()&os.ModeSymlink != 0 {
			return ErrSymlinkDetected
		}
		// Existing regular file — verify permissions before overwriting.
		if err := checkPermissions(fi.Mode()); err != nil {
			return fmt.Errorf("file key store save to disk: %w", err)
		}
		outFile, err := os.OpenFile(f.filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("write keystore file: %w", err)
		}
		if _, err := outFile.Write(out); err != nil {
			_ = outFile.Close() // best-effort close
			return fmt.Errorf("write keystore file: %w", err)
		}
		return outFile.Close()
	} else if errors.Is(err, os.ErrNotExist) {
		// File doesn't exist — create with O_EXCL to prevent symlink race.
		outFile, err := os.OpenFile(f.filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("write keystore file: %w", err)
		}
		if _, err := outFile.Write(out); err != nil {
			_ = outFile.Close() // best-effort close
			return fmt.Errorf("write keystore file: %w", err)
		}
		return outFile.Close()
	} else {
		return fmt.Errorf("stat keystore file: %w", err)
	}
}

// deriveKey derives an AES-256 key from the passphrase and salt using
// PBKDF2-HMAC-SHA256.
func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, pbkdf2Iterations, keyLen)
}

// encryptAESGCM encrypts plaintext with AES-256-GCM using the given key and
// nonce. The nonce must be unique for each encryption with this key.
func encryptAESGCM(key, nonce, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("encrypt aesgcm: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encrypt aesgcm: %w", err)
	}
	// Sealed output: nonce || ciphertext || tag (tag appended by GCM).
	return gcm.Seal(nil, nonce, plaintext, nil), nil
}

// decryptAESGCM decrypts ciphertext with AES-256-GCM using the given key and
// nonce. The ciphertext must include the authentication tag (as produced by
// encryptAESGCM).
func decryptAESGCM(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("decrypt aesgcm: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("decrypt aesgcm: %w", err)
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// checkPermissions validates that the file mode only allows owner access
// (0600 or 0400). Returns ErrWeakPermissions otherwise.
func checkPermissions(mode os.FileMode) error {
	// Only check the permission bits, not the file type.
	perm := mode.Perm()
	// Allowed: exactly 0600 (owner rw) or 0400 (owner r).
	if perm != 0600 && perm != 0400 {
		return fmt.Errorf("%w: got %#o, expected 0600 or 0400", ErrWeakPermissions, perm)
	}
	return nil
}

// ---------------------------------------------------------------------------
// KeyStore interface implementation
// ---------------------------------------------------------------------------

// Create stores key material under the given ID and type. It returns
// ErrKeyAlreadyExists if a key with that ID already exists, or
// ErrInvalidKeyID if the ID fails validation.
func (f *FileKeyStore) Create(id KeyID, kt KeyType, material KeyMaterial) error {
	if err := ValidateKeyID(id); err != nil {
		return fmt.Errorf("file key store create: %w", err)
	}

	// Encode key material as base64 for storage.
	b64 := b64Encode(material.Bytes)

	// Acquire file lock before f.mu to prevent cross-instance data loss.
	// The file lock wraps the entire read-modify-write cycle so that
	// concurrent stores pointing at the same file serialize correctly.
	flock := getFileLock(f.filePath)
	flock.Lock()
	defer flock.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	// Reload from disk to pick up keys added by other instances since
	// this store was opened. Without this, the in-memory map is stale
	// and saveToDisk would overwrite other instances' additions.
	if _, err := os.Stat(f.filePath); err == nil {
		if err := f.loadFromDisk(); err != nil {
			return fmt.Errorf("file key store create: %w", err)
		}
	}

	if _, exists := f.keys[string(id)]; exists {
		return ErrKeyAlreadyExists
	}

	f.keys[string(id)] = fileStoreEntry{
		Type:      string(kt),
		BytesB64:  b64,
		CreatedAt: time.Now(),
	}

	return f.saveToDisk()
}

// Load retrieves key material for the given ID. Returns ErrKeyNotFound if the
// key does not exist, or ErrInvalidKeyID if the ID fails validation.
func (f *FileKeyStore) Load(id KeyID) (KeyMaterial, error) {
	if err := ValidateKeyID(id); err != nil {
		return KeyMaterial{}, fmt.Errorf("file key store load: %w", err)
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	entry, ok := f.keys[string(id)]
	if !ok {
		return KeyMaterial{}, ErrKeyNotFound
	}

	bytes, err := b64Decode(entry.BytesB64)
	if err != nil {
		return KeyMaterial{}, fmt.Errorf("decode key material: %w", err)
	}

	return KeyMaterial{Type: KeyType(entry.Type), Bytes: bytes}, nil
}

// Sign computes an ECDSA signature over the given digest using the key
// identified by id. Only signing key types (CA, AuditSigning,
// PackageIdentity) are supported; Workload keys return ErrWrongKeyType.
func (f *FileKeyStore) Sign(id KeyID, digest []byte) ([]byte, error) {
	if err := ValidateKeyID(id); err != nil {
		return nil, fmt.Errorf("file key store sign: %w", err)
	}

	f.mu.RLock()
	entry, ok := f.keys[string(id)]
	f.mu.RUnlock()

	if !ok {
		return nil, ErrKeyNotFound
	}

	kt := KeyType(entry.Type)
	if !signingKeyTypes(kt) {
		return nil, ErrWrongKeyType
	}

	bytes, err := b64Decode(entry.BytesB64)
	if err != nil {
		return nil, fmt.Errorf("decode key material: %w", err)
	}

	key, err := parseECDSAPrivateKey(bytes)
	if err != nil {
		return nil, fmt.Errorf("file key store sign: %w", err)
	}

	return ecdsaSign(key, digest)
}

// Verify checks an ECDSA signature over the given digest against the key
// identified by id. Returns false if the key is not found, is a non-signing
// key type, or if the ID fails validation.
func (f *FileKeyStore) Verify(id KeyID, digest []byte, signature []byte) bool {
	if err := ValidateKeyID(id); err != nil {
		return false
	}

	f.mu.RLock()
	entry, ok := f.keys[string(id)]
	f.mu.RUnlock()

	if !ok {
		return false
	}

	kt := KeyType(entry.Type)
	if !signingKeyTypes(kt) {
		return false
	}

	bytes, err := b64Decode(entry.BytesB64)
	if err != nil {
		return false
	}

	key, err := parseECDSAPublicKey(bytes)
	if err != nil {
		return false
	}

	return ecdsaVerify(key, digest, signature)
}

// Delete removes the key with the given ID. Returns ErrKeyNotFound if the key
// does not exist, or ErrInvalidKeyID if the ID fails validation.
func (f *FileKeyStore) Delete(id KeyID) error {
	if err := ValidateKeyID(id); err != nil {
		return fmt.Errorf("file key store delete: %w", err)
	}

	// Acquire file lock before f.mu to prevent cross-instance data loss.
	flock := getFileLock(f.filePath)
	flock.Lock()
	defer flock.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	// Reload from disk to pick up changes from other instances.
	if _, err := os.Stat(f.filePath); err == nil {
		if err := f.loadFromDisk(); err != nil {
			return fmt.Errorf("file key store delete: %w", err)
		}
	}

	if _, exists := f.keys[string(id)]; !exists {
		return ErrKeyNotFound
	}

	delete(f.keys, string(id))
	return f.saveToDisk()
}

// List returns metadata for all stored keys. The returned KeyMetadata entries
// never contain raw key material (RawBytes is always nil).
func (f *FileKeyStore) List() ([]KeyMetadata, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]KeyMetadata, 0, len(f.keys))
	// Sort by key ID for deterministic output.
	ids := make([]string, 0, len(f.keys))
	for id := range f.keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entry := f.keys[id]
		result = append(result, KeyMetadata{
			ID:        KeyID(id),
			Type:      KeyType(entry.Type),
			CreatedAt: entry.CreatedAt,
		})
	}
	return result, nil
}

// Compile-time check that FileKeyStore satisfies the KeyStore interface.
var _ KeyStore = (*FileKeyStore)(nil)
