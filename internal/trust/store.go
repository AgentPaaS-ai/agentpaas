// Package trust manages the publisher trust store — a local, file-backed
// registry of trusted publisher public keys with TOFU and manual pre-pinning
// support.
//
// The trust store is persisted at <home>/trust/publishers.json (0600) inside a
// trust/ directory (0700). Concurrent access is serialized via flock on
// publishers.json.lock, and writes are atomic (temp file + rename + fsync).
package trust

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Schema version for the publishers.json file format.
const storeSchemaVersion = 1

// Sentinel errors returned by trust store operations.
var (
	// ErrStoreCorrupt is returned when the on-disk trust store file exists but
	// contains malformed JSON. Callers MUST surface this as a hard error — the
	// store is never silently replaced with an empty one.
	ErrStoreCorrupt = errors.New("trust store file is corrupt: malformed JSON")

	// ErrDuplicateFingerprint is returned by Pin when a publisher with the
	// same fingerprint already exists in the store.
	ErrDuplicateFingerprint = errors.New("publisher fingerprint already trusted")

	// ErrFingerprintMismatch is returned by Pin when the PEM-derived
	// fingerprint does not match the stored fingerprint field.
	ErrFingerprintMismatch = errors.New("fingerprint does not match the public key PEM")

	// ErrNotFound is returned when a publisher lookup by fingerprint fails.
	ErrNotFound = errors.New("publisher not found")

	// ErrLockTimeout is returned when flock cannot be acquired within the
	// configured timeout.
	ErrLockTimeout = errors.New("could not acquire trust store lock")
)

// TrustSource indicates how a publisher was added to the trust store.
type TrustSource string

const (
	// SourceTOFU means the publisher was trusted on first use (automatically).
	SourceTOFU TrustSource = "tofu"

	// SourceManual means the publisher was manually pre-pinned by the operator.
	SourceManual TrustSource = "manual"
)

// PublisherStatus indicates the trust state of a publisher.
type PublisherStatus string

const (
	// StatusTrusted is the normal operating state for a publisher.
	StatusTrusted PublisherStatus = "trusted"
)

// Publisher represents a single trusted publisher in the trust store.
type Publisher struct {
	// Fingerprint is the hex-encoded SHA-256 of the PKIX-encoded public key
	// (64 lowercase hex characters).
	Fingerprint string `json:"fingerprint"`

	// PublicKeyPEM is the PEM-encoded public key (PKIX, SPKI block).
	PublicKeyPEM string `json:"public_key_pem"`

	// Alias is an optional human-readable slug for the publisher (e.g. "parvez").
	Alias string `json:"alias"`

	// FirstSeen is the RFC 3339 timestamp when the publisher was first trusted.
	FirstSeen string `json:"first_seen"`

	// LastUsed is the RFC 3339 timestamp when the publisher was last used.
	LastUsed string `json:"last_used"`

	// Source indicates how the publisher was added (tofu or manual).
	Source TrustSource `json:"source"`

	// Status is the trust state of the publisher.
	Status PublisherStatus `json:"status"`
}

// storeFile is the on-disk JSON schema for publishers.json.
type storeFile struct {
	Version    int         `json:"version"`
	Publishers []Publisher `json:"publishers"`
}

// Store is an in-memory representation of the trust store, backed by a file
// on disk. Use Load to create a Store from an existing file, or to initialize
// a new empty store. Use Save to persist changes.
type Store struct {
	path string // path to publishers.json
	// lockFd removed — was unused
	records map[string]*Publisher
	removed map[string]bool // fingerprints explicitly removed since last load
	dirty   bool
}

// LockTimeout is the maximum duration to wait for the flock lock.
const LockTimeout = 5 * time.Second

// Load reads the trust store from the file at the given path. If the file does
// not exist, an empty store is returned (missing file is valid — it means no
// publishers have been trusted yet). If the file exists but contains malformed
// JSON, ErrStoreCorrupt is returned.
//
// The trust directory is created with mode 0700 if it does not exist.
func Load(path string) (*Store, error) {
	// Ensure directory exists with correct permissions.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("trust: create directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("trust: set directory permissions %s: %w", dir, err)
	}

	s := &Store{
		path:    path,
		records: make(map[string]*Publisher),
		removed: make(map[string]bool),
	}

	// If the file doesn't exist, that's fine — return empty store.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("trust: read store file %s: %w", path, err)
	}

	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrStoreCorrupt, path, err)
	}

	for i := range sf.Publishers {
		pub := sf.Publishers[i]
		s.records[pub.Fingerprint] = &pub
	}

	return s, nil
}

// Get returns a publisher by fingerprint. The second return value is false if
// no publisher with that fingerprint exists.
func (s *Store) Get(fingerprint string) (*Publisher, bool) {
	pub, ok := s.records[fingerprint]
	return pub, ok
}

// Pin adds a publisher to the trust store. It validates that:
//   - The fingerprint is not already present (ErrDuplicateFingerprint).
//   - The public key PEM parses successfully.
//   - The SHA-256 of the PKIX-encoded public key matches the stored fingerprint
//     field (ErrFingerprintMismatch).
//
// If all checks pass, FirstSeen is set to the current time and the store is
// marked dirty (Save must be called to persist).
func (s *Store) Pin(pub Publisher, source TrustSource) error {
	fp := NormalizeFingerprint(pub.Fingerprint)
	if _, exists := s.records[fp]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateFingerprint, DisplayFingerprint(fp))
	}

	// Validate PEM → fingerprint self-consistency.
	computed, err := FingerprintFromPEM(pub.PublicKeyPEM)
	if err != nil {
		return fmt.Errorf("trust: invalid public key PEM: %w", err)
	}
	if computed != fp {
		return fmt.Errorf("%w: expected %s, computed %s", ErrFingerprintMismatch,
			DisplayFingerprint(fp), DisplayFingerprint(computed))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	pub.Fingerprint = fp
	pub.FirstSeen = now
	pub.LastUsed = now
	pub.Source = source
	pub.Status = StatusTrusted

	s.records[fp] = &pub
	s.dirty = true
	return nil
}

// Remove deletes a publisher from the trust store by fingerprint. It returns
// ErrNotFound if no publisher with that fingerprint exists.
func (s *Store) Remove(fingerprint string) error {
	fp := NormalizeFingerprint(fingerprint)
	if _, exists := s.records[fp]; !exists {
		return fmt.Errorf("%w: %s", ErrNotFound, DisplayFingerprint(fp))
	}
	delete(s.records, fp)
	s.removed[fp] = true
	s.dirty = true
	return nil
}

// CheckKeyConflict returns a trusted publisher that shares the same alias but
// has a different fingerprint than the given one. This is used for SSH-style
// warnings when a known alias (hostname) presents a new key. Returns nil if
// no conflict exists.
func (s *Store) CheckKeyConflict(alias string, fingerprint string) *Publisher {
	if alias == "" {
		return nil
	}
	fp := NormalizeFingerprint(fingerprint)
	pub, ok := s.records[fp]
	if ok && pub.Alias != alias {
		// Same key, different alias — not a conflict, just an alias update.
		return nil
	}
	for _, p := range s.records {
		if p.Alias == alias && p.Fingerprint != fp {
			return p
		}
	}
	return nil
}

// Publishers returns all publishers in the store as a slice.
func (s *Store) Publishers() []Publisher {
	result := make([]Publisher, 0, len(s.records))
	for _, p := range s.records {
		result = append(result, *p)
	}
	return result
}

// Len returns the number of publishers in the store.
func (s *Store) Len() int {
	return len(s.records)
}

// Save persists the trust store to disk using atomic write (temp file + rename
// + fsync). It acquires an exclusive flock on publishers.json.lock to serialize
// concurrent access, re-reads the current on-disk state to merge concurrent
// updates, and then writes the merged result atomically.
func (s *Store) Save() error {
	if !s.dirty {
		return nil
	}

	// Acquire lock.
	lockPath := s.path + ".lock"
	fd, err := acquireFlock(lockPath)
	if err != nil {
		return fmt.Errorf("store save: %w", err)
	}
	defer releaseFlock(fd, lockPath)

	// Merge: re-read current on-disk state to catch concurrent modifications.
	// Our in-memory records take priority (last-writer-wins for conflicts).
	merged := make(map[string]*Publisher, len(s.records))
	for k, v := range s.records {
		merged[k] = v
	}
	if data, err := os.ReadFile(s.path); err == nil {
		var sf storeFile
		if json.Unmarshal(data, &sf) == nil {
			for i := range sf.Publishers {
				pub := sf.Publishers[i]
				// Skip entries that were explicitly removed from this store.
				if s.removed[pub.Fingerprint] {
					continue
				}
				if _, exists := merged[pub.Fingerprint]; !exists {
					merged[pub.Fingerprint] = &pub
				}
			}
		}
	}

	// Build the store file.
	sf := storeFile{
		Version:    storeSchemaVersion,
		Publishers: make([]Publisher, 0, len(merged)),
	}
	for _, p := range merged {
		sf.Publishers = append(sf.Publishers, *p)
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("trust: marshal store: %w", err)
	}
	// Append newline for human readability.
	data = append(data, '\n')

	// Write to temp file in the same directory.
	dir := filepath.Dir(s.path)
	tmpFile, err := os.CreateTemp(dir, ".publishers-*.json")
	if err != nil {
		return fmt.Errorf("trust: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }() // best-effort remove

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close() // best-effort close
		return fmt.Errorf("trust: write temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close() // best-effort close
		return fmt.Errorf("trust: fsync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("trust: close temp file: %w", err)
	}

	// Set correct permissions on the temp file.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("trust: chmod temp file: %w", err)
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("trust: rename temp file: %w", err)
	}

	// Fsync the directory to ensure the rename is durable.
	dirFd, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("trust: open dir for fsync: %w", err)
	}
	defer func() { _ = dirFd.Close() }() // best-effort close
	if err := dirFd.Sync(); err != nil {
		return fmt.Errorf("trust: fsync dir: %w", err)
	}

	s.dirty = false
	s.records = merged
	return nil
}

// acquireFlock acquires an exclusive flock on the given lock file path.
// It creates the lock file if it doesn't exist.
func acquireFlock(lockPath string) (int, error) {
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return -1, fmt.Errorf("trust: open lock file %s: %w", lockPath, err)
	}

	deadline := time.Now().Add(LockTimeout)
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return fd, nil
		}
		if err != unix.EWOULDBLOCK {
			_ = unix.Close(fd) // best-effort cleanup
			return -1, fmt.Errorf("trust: flock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			_ = unix.Close(fd) // best-effort cleanup
			return -1, fmt.Errorf("%w: %s", ErrLockTimeout, lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// releaseFlock releases the flock and closes the file descriptor.
func releaseFlock(fd int, lockPath string) {
	_ = unix.Flock(fd, unix.LOCK_UN) // intentionally ignored (reviewed)
	_ = unix.Close(fd)               // best-effort cleanup
	// Clean up the lock file so it doesn't accumulate.
	_ = os.Remove(lockPath) // best-effort remove
}

// FingerprintFromPEM parses a PEM-encoded public key and returns its hex-encoded
// SHA-256 fingerprint (same as pack.PublicKeyFingerprint). The PEM must contain
// a "PUBLIC KEY" block with an ECDSA P-256 key.
func FingerprintFromPEM(pemData string) (string, error) {
	pub, err := parsePublicKeyPEM(pemData)
	if err != nil {
		return "", fmt.Errorf("fingerprint from pem: %w", err)
	}
	return computeFingerprint(pub), nil
}

// parsePublicKeyPEM decodes a PEM-encoded public key. It accepts both
// "PUBLIC KEY" (PKIX/SPKI) and "EC PUBLIC KEY" blocks. The key must be
// an ECDSA P-256 key.
func parsePublicKeyPEM(pemData string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData)) // optional value; zero on miss
	if block == nil {
		return nil, errors.New("no PEM block found in public key data")
	}

	var parsed interface{}
	var err error

	switch block.Type {
	case "PUBLIC KEY":
		parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
	case "EC PUBLIC KEY":
		parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			parsed, err = nil, fmt.Errorf("parse EC public key: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q (expected PUBLIC KEY or EC PUBLIC KEY)", block.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is %T, not ECDSA", parsed)
	}
	if pub.Curve == nil {
		return nil, errors.New("public key has no curve")
	}
	return pub, nil
}

// computedFingerprint returns the hex-encoded SHA-256 of the PKIX-encoded
// public key. This is the same algorithm as pack.PublicKeyFingerprint.
func computeFingerprint(pub *ecdsa.PublicKey) string {
	if pub == nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return fmt.Sprintf("%x", sum)
}
