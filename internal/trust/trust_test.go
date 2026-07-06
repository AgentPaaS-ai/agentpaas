package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// generateTestKey creates a P-256 ECDSA key pair and returns:
//   - the public key
//   - the PEM-encoded public key
//   - the hex fingerprint (sha256 of PKIX DER)
func generateTestKey(t *testing.T) (*ecdsa.PublicKey, string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	})

	sum := sha256.Sum256(der)
	fp := ""
	for _, b := range sum {
		fp += string("0123456789abcdef"[b>>4])
		fp += string("0123456789abcdef"[b&0xf])
	}

	return &priv.PublicKey, string(pemBytes), fp
}

func TestFingerprintFromPEM(t *testing.T) {
	_, pemData, expectedFP := generateTestKey(t)

	fp, err := FingerprintFromPEM(pemData)
	if err != nil {
		t.Fatalf("FingerprintFromPEM: %v", err)
	}
	if fp != expectedFP {
		t.Fatalf("fingerprint mismatch: got %s, want %s", fp, expectedFP)
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
		{"A1B2 C3D4 E5F6 A1B2 C3D4 E5F6 A1B2 C3D4 E5F6 A1B2 C3D4 E5F6 A1B2 C3D4 E5F6 A1B2", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
		{"a1:b2:c3:d4", "a1b2c3d4"},
		{"a1 b2-c3 d4", "a1b2c3d4"},
		{"\ta1b2\nc3d4\r", "a1b2c3d4"},
	}

	for _, tt := range tests {
		got := NormalizeFingerprint(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeFingerprint(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDisplayFingerprint(t *testing.T) {
	fp64 := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	expected := "a1b2 c3d4 e5f6 a1b2 c3d4 e5f6 a1b2 c3d4 e5f6 a1b2 c3d4 e5f6 a1b2 c3d4 e5f6 a1b2"
	got := DisplayFingerprint(fp64)
	if got != expected {
		t.Errorf("DisplayFingerprint = %q, want %q", got, expected)
	}

	// Short fingerprint should pass through.
	got = DisplayFingerprint("abc")
	if got != "abc" {
		t.Errorf("DisplayFingerprint(short) = %q, want %q", got, "abc")
	}
}

func TestIsValidAlias(t *testing.T) {
	tests := []struct {
		alias string
		valid bool
	}{
		{"parvez", true},
		{"my-publisher", true},
		{"publisher-123", true},
		{"a", true},
		{"", false},
		{"Invalid", false},
		{"has space", false},
		{"under_score", false},
		{string(make([]byte, 65)), false},
	}

	for _, tt := range tests {
		got := IsValidAlias(tt.alias)
		if got != tt.valid {
			t.Errorf("IsValidAlias(%q) = %v, want %v", tt.alias, got, tt.valid)
		}
	}
}

func TestPinGetRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, pemData, fp := generateTestKey(t)

	pub := Publisher{
		Fingerprint:  fp,
		PublicKeyPEM: pemData,
		Alias:        "test-publisher",
	}

	// Pin.
	if err := store.Pin(pub, SourceManual); err != nil {
		t.Fatalf("Pin: %v", err)
	}

	// Verify in-memory.
	got, ok := store.Get(fp)
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.Alias != "test-publisher" {
		t.Errorf("alias = %q, want %q", got.Alias, "test-publisher")
	}
	if got.Source != SourceManual {
		t.Errorf("source = %q, want %q", got.Source, SourceManual)
	}
	if got.FirstSeen == "" {
		t.Error("FirstSeen is empty")
	}
	if got.LastUsed == "" {
		t.Error("LastUsed is empty")
	}

	// Save.
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file permissions.
	fi, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("store file permissions = %#o, want 0600", fi.Mode().Perm())
	}

	// Verify dir permissions.
	di, err := os.Stat(filepath.Dir(storePath))
	if err != nil {
		t.Fatalf("stat store dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("store dir permissions = %#o, want 0700", di.Mode().Perm())
	}

	// Reload and verify.
	store2, err := Load(storePath)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	got2, ok := store2.Get(fp)
	if !ok {
		t.Fatal("Get after reload returned false")
	}
	if got2.Alias != "test-publisher" {
		t.Errorf("alias after reload = %q", got2.Alias)
	}

	// Remove.
	if err := store2.Remove(fp); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := store2.Save(); err != nil {
		t.Fatalf("Save after remove: %v", err)
	}

	// Reload again — should be empty.
	store3, err := Load(storePath)
	if err != nil {
		t.Fatalf("re-re-Load: %v", err)
	}
	if store3.Len() != 0 {
		t.Errorf("store has %d publishers after remove, want 0", store3.Len())
	}
}

func TestDuplicatePinFails(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, pemData, fp := generateTestKey(t)

	pub := Publisher{Fingerprint: fp, PublicKeyPEM: pemData, Alias: "p1"}
	if err := store.Pin(pub, SourceManual); err != nil {
		t.Fatalf("first Pin: %v", err)
	}

	// Second pin with same fingerprint should fail.
	pub2 := Publisher{Fingerprint: fp, PublicKeyPEM: pemData, Alias: "p2"}
	err = store.Pin(pub2, SourceManual)
	if err == nil {
		t.Fatal("expected duplicate pin to fail")
	}
	if !errors.Is(err, ErrDuplicateFingerprint) {
		t.Errorf("error = %v, want ErrDuplicateFingerprint", err)
	}
}

func TestMismatchedPEMFingerprint(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, pemData, _ := generateTestKey(t)
	_, _, wrongFP := generateTestKey(t) // different key → different fingerprint

	pub := Publisher{Fingerprint: wrongFP, PublicKeyPEM: pemData}
	err = store.Pin(pub, SourceManual)
	if err == nil {
		t.Fatal("expected fingerprint mismatch error")
	}
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("error = %v, want ErrFingerprintMismatch", err)
	}
}

func TestMalformedJSONStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	// Write corrupt JSON.
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(storePath, []byte("this is not json {{{"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := Load(storePath)
	if err == nil {
		t.Fatal("expected error for corrupt store file")
	}
	if !errors.Is(err, ErrStoreCorrupt) {
		t.Errorf("error = %v, want ErrStoreCorrupt", err)
	}
}

func TestMissingStoreFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if store.Len() != 0 {
		t.Fatalf("expected empty store, got %d publishers", store.Len())
	}
}

func TestCheckKeyConflict(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, pem1, fp1 := generateTestKey(t)
	_, _, fp2 := generateTestKey(t)

	// Pin two different keys, both with alias "parvez".
	// Only the first should succeed (second is duplicate alias? Actually,
	// aliases are not unique — CheckKeyConflict just warns about it).
	pub1 := Publisher{Fingerprint: fp1, PublicKeyPEM: pem1, Alias: "parvez"}
	if err := store.Pin(pub1, SourceManual); err != nil {
		t.Fatalf("Pin pub1: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload fresh.
	store, err = Load(storePath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	conflict := store.CheckKeyConflict("parvez", fp2)
	if conflict == nil {
		t.Fatal("expected conflict for alias 'parvez' with different fingerprint")
	}
	if conflict.Fingerprint != fp1 {
		t.Errorf("conflict fingerprint = %s, want %s", conflict.Fingerprint, fp1)
	}

	// CheckKeyConflict with same fp and alias should return nil.
	conflict = store.CheckKeyConflict("parvez", fp1)
	if conflict != nil {
		t.Errorf("expected no conflict for same alias+fp, got %+v", conflict)
	}

	// CheckKeyConflict with empty alias should return nil.
	conflict = store.CheckKeyConflict("", fp2)
	if conflict != nil {
		t.Errorf("expected no conflict for empty alias, got %+v", conflict)
	}
}

func TestAtomicWriteSurvivesCrash(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, pemData, fp := generateTestKey(t)
	pub := Publisher{Fingerprint: fp, PublicKeyPEM: pemData, Alias: "atomic-test"}
	if err := store.Pin(pub, SourceManual); err != nil {
		t.Fatalf("Pin: %v", err)
	}

	// Save normally first.
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Now "crash" by creating a temp file that looks like an in-flight write.
	// After Load, the original data should still be intact.
	tmpPath := storePath + ".tmp.crash"
	if err := os.WriteFile(tmpPath, []byte("partial write"), 0o600); err != nil {
		t.Fatalf("write crash temp: %v", err)
	}

	// Load should still see the original data (temp file is ignored).
	store2, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load after crash: %v", err)
	}
	if _, ok := store2.Get(fp); !ok {
		t.Fatal("publisher lost after simulated crash")
	}

	// Clean up the temp file.
	_ = os.Remove(tmpPath)
}

func TestConcurrency(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	// Pin 10 publishers concurrently from different goroutines.
	// Each goroutine loads, pins, and saves independently.
	// After all finish, the store should contain all 10.

	const n = 10
	type keyInfo struct {
		fp      string
		pemData string
	}

	keys := make([]keyInfo, n)
	for i := 0; i < n; i++ {
		_, pemData, fp := generateTestKey(t)
		keys[i] = keyInfo{fp: fp, pemData: pemData}
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			store, err := Load(storePath)
			if err != nil {
				errs <- err
				return
			}
			pub := Publisher{
				Fingerprint:  keys[idx].fp,
				PublicKeyPEM: keys[idx].pemData,
				Alias:        "",
			}
			if err := store.Pin(pub, SourceManual); err != nil {
				errs <- err
				return
			}
			if err := store.Save(); err != nil {
				errs <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent pin error: %v", err)
	}

	// Final load — should have all n publishers.
	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("final Load: %v", err)
	}
	if store.Len() != n {
		t.Errorf("store has %d publishers after concurrent pins, want %d", store.Len(), n)
	}

	// Verify all are present.
	for _, k := range keys {
		if _, ok := store.Get(k.fp); !ok {
			t.Errorf("missing publisher %s", k.fp)
		}
	}
}

func TestRemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, _, fp := generateTestKey(t)

	err = store.Remove(fp)
	if err == nil {
		t.Fatal("expected error removing non-existent publisher")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, _, fp := generateTestKey(t)

	_, ok := store.Get(fp)
	if ok {
		t.Fatal("expected Get to return false for non-existent publisher")
	}
}

func TestDefaultStorePath(t *testing.T) {
	got := DefaultStorePath("/home/user/.agentpaas")
	expected := "/home/user/.agentpaas/trust/publishers.json"
	if got != expected {
		t.Errorf("DefaultStorePath = %q, want %q", got, expected)
	}
}

func TestStoreSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, pemData, fp := generateTestKey(t)
	pub := Publisher{Fingerprint: fp, PublicKeyPEM: pemData, Alias: "vtest"}
	if err := store.Pin(pub, SourceManual); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read the file and verify schema version.
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sf.Version != storeSchemaVersion {
		t.Errorf("schema version = %d, want %d", sf.Version, storeSchemaVersion)
	}
}

func TestParsePublicKeyPEM_EC(t *testing.T) {
	// Generate key and marshal directly as EC PUBLIC KEY.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	ecDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ecPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PUBLIC KEY",
		Bytes: ecDER,
	})

	// Should parse correctly even with EC PUBLIC KEY block type.
	fp, err := FingerprintFromPEM(string(ecPEM))
	if err != nil {
		t.Fatalf("FingerprintFromPEM (EC PUBLIC KEY): %v", err)
	}
	if fp == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestSaveNoopIfClean(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "publishers.json")

	store, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Save on clean store should not create the file.
	if err := store.Save(); err != nil {
		t.Fatalf("Save (clean): %v", err)
	}

	_, err = os.Stat(storePath)
	if !os.IsNotExist(err) {
		t.Error("Save on clean store should not create file")
	}
}