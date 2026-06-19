package identity

import (
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

// TestFileKeyStore runs the full contract suite against FileKeyStore.
func TestFileKeyStore(t *testing.T) {
	t.Parallel()

	passphrase := "test-passphrase-1279"

	var idx atomic.Int32
	ContractTests(t, func() KeyStore {
		n := idx.Add(1)
		sub := filepath.Join(t.TempDir(), strconv.Itoa(int(n)))
		if err := os.MkdirAll(sub, 0700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		s, err := NewFileKeyStore(sub, passphrase)
		if err != nil {
			t.Fatalf("NewFileKeyStore: %v", err)
		}
		return s
	})
}

// TestFileKeyStore_WrongPassphrase verifies that loading with a wrong
// passphrase fails closed — no data returned, clear error.
func TestFileKeyStore_WrongPassphrase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create store with passphrase A.
	s1, err := NewFileKeyStore(dir, "correct-passphrase")
	if err != nil {
		t.Fatalf("NewFileKeyStore: %v", err)
	}
	material := testKeyMaterial(t, KeyTypeCA)
	if err := s1.Create("my-key", KeyTypeCA, material); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Close / release — s1 goes out of scope.

	// Open same dir with wrong passphrase — must fail.
	_, err = NewFileKeyStore(dir, "wrong-passphrase")
	if err == nil {
		t.Fatal("NewFileKeyStore with wrong passphrase: want error, got nil")
	}
	t.Logf("Wrong passphrase correctly rejected: %v", err)
}

// TestFileKeyStore_WeakPermissions verifies that stores with file permissions
// weaker than 0600 are refused (fail closed).
func TestFileKeyStore_WeakPermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a valid store first.
	s1, err := NewFileKeyStore(dir, "pass")
	if err != nil {
		t.Fatalf("NewFileKeyStore: %v", err)
	}
	material := testKeyMaterial(t, KeyTypeCA)
	if err := s1.Create("k1", KeyTypeCA, material); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Locate the store file and weaken its permissions.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no store files found in dir")
	}
	fp := filepath.Join(dir, entries[0].Name())

	// 0644 — world-readable, must be refused.
	if err := os.Chmod(fp, 0644); err != nil {
		t.Fatalf("Chmod 0644: %v", err)
	}
	if _, err := NewFileKeyStore(dir, "pass"); err == nil {
		t.Error("NewFileKeyStore with 0644 store file: want error, got nil")
	} else {
		t.Logf("0644 correctly rejected: %v", err)
	}

	// Reset to 0600 (needed for subsequent tests that might re-create the store in this dir).
	_ = os.Chmod(fp, 0600)

	// 0755 — executable by all, must be refused.
	testDir := t.TempDir()
	s2, err := NewFileKeyStore(testDir, "pass")
	if err != nil {
		t.Fatalf("NewFileKeyStore: %v", err)
	}
	if err := s2.Create("k2", KeyTypeCA, material); err != nil {
		t.Fatalf("Create: %v", err)
	}
	entries2, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries2) == 0 {
		t.Fatal("no store files in testDir")
	}
	fp2 := filepath.Join(testDir, entries2[0].Name())
	if err := os.Chmod(fp2, 0755); err != nil {
		t.Fatalf("Chmod 0755: %v", err)
	}
	if _, err := NewFileKeyStore(testDir, "pass"); err == nil {
		t.Error("NewFileKeyStore with 0755 store file: want error, got nil")
	} else {
		t.Logf("0755 correctly rejected: %v", err)
	}

	// 0600 — correct permissions, must be accepted.
	_ = os.Chmod(fp, 0600)
	s3, err := NewFileKeyStore(dir, "pass")
	if err != nil {
		t.Errorf("NewFileKeyStore with 0600 store file: want nil, got %v", err)
	} else {
		t.Log("0600 correctly accepted")
		_ = s3.Close()
	}
}

// TestFileKeyStore_RoundTrip verifies a full write/read cycle.
func TestFileKeyStore_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := NewFileKeyStore(dir, "roundtrip-pass")
	if err != nil {
		t.Fatalf("NewFileKeyStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	caMat := testKeyMaterial(t, KeyTypeCA)
	if err := s.Create("ca-key", KeyTypeCA, caMat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := s.Load("ca-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Type != KeyTypeCA {
		t.Errorf("loaded type = %q, want %q", loaded.Type, KeyTypeCA)
	}
	if len(loaded.Bytes) == 0 {
		t.Error("loaded bytes are empty")
	}
}

// TestFileKeyStore_NoPlaintextOnDisk verifies that raw key bytes are not
// present in the store file as plaintext.
func TestFileKeyStore_NoPlaintextOnDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := NewFileKeyStore(dir, "noplain")
	if err != nil {
		t.Fatalf("NewFileKeyStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	caMat := testKeyMaterial(t, KeyTypeCA)
	if err := s.Create("sensitive-key", KeyTypeCA, caMat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Force a sync/flush by closing and reopening.
	_ = s.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", e.Name(), err)
		}
		// Check for PEM-like markers that indicate plaintext key material.
		plaintextMarkers := []string{
			"EC PRIVATE KEY",
			"PRIVATE KEY",
			"BEGIN CERTIFICATE",
			"-----BEGIN",
		}
		for _, marker := range plaintextMarkers {
			if containsBytes(data, []byte(marker)) {
				t.Errorf("plaintext key marker %q found in store file %s", marker, e.Name())
			}
		}
	}
	t.Log("No plaintext key material found on disk")
}

// containsBytes reports whether b contains sub.
func containsBytes(b, sub []byte) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(b) {
		return false
	}
	for i := 0; i <= len(b)-len(sub); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}

// TestFileKeyStore_ListNoRawMaterial ensures List() returns only metadata.
func TestFileKeyStore_ListNoRawMaterial(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := NewFileKeyStore(dir, "list-test")
	if err != nil {
		t.Fatalf("NewFileKeyStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	for _, kt := range []KeyType{KeyTypeCA, KeyTypeAuditSigning, KeyTypePackageIdentity, KeyTypeWorkload} {
		if err := s.Create(KeyID(kt), kt, testKeyMaterial(t, kt)); err != nil {
			t.Fatalf("Create(%s): %v", kt, err)
		}
	}

	meta, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, m := range meta {
		if m.RawBytes != nil {
			t.Errorf("List entry %q leaks raw bytes", m.ID)
		}
	}
}

// TestFileKeyStore_ReopenNonExistentDir verifies that opening a non-existent
// directory creates it automatically.
func TestFileKeyStore_ReopenNonExistentDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "new-subdir")
	s, err := NewFileKeyStore(dir, "pass")
	if err != nil {
		t.Fatalf("NewFileKeyStore on non-existent dir: %v", err)
	}
	defer func() { _ = s.Close() }()

	material := testKeyMaterial(t, KeyTypeCA)
	if err := s.Create("created", KeyTypeCA, material); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// ensure FileKeyStore implements KeyStore at compile time.
var _ KeyStore = (*FileKeyStore)(nil)