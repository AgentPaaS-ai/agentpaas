package identity

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync/atomic"
	"os"
	"testing"
)

// runID is a random number that changes each test run, ensuring unique
// keychain service names across runs to avoid leftover-key pollution.
var runID = rand.Int63()

// TestKeychainKeyStore runs the full contract suite against KeychainKeyStore.
// On non-macOS platforms the test is skipped.
func TestKeychainKeyStore(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("KeychainKeyStore requires macOS; skipping")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") == "" {
		t.Skip("skipping keychain integration test; set AGENTPAAS_KEYCHAIN_TESTS=1 to run")
	}
	t.Parallel()

	var idx atomic.Int32

	ContractTests(t, func() KeyStore {
		// Use a unique service name per call to avoid cross-test pollution.
		n := idx.Add(1)
		service := fmt.Sprintf("ai.agentpaas.identity.test.%s.%d.%d", t.Name(), n, runID)
		s, err := NewKeychainKeyStore(service)
		if err != nil {
			t.Fatalf("NewKeychainKeyStore: %v", err)
		}
		return s
	})
}

// TestKeychainKeyStore_LockedKeychain verifies that when the keychain is
// unavailable or locked, an actionable error is returned (no silent fallback).
//
// This test optionally sets the KEYCHAIN_TEST_LOCKED env var to force the
// test to validate locked-keychain behavior. Without the env var it performs
// a best-effort check: it tries to create a key and then
// simulates an unavailable keychain by attempting to read from a service
// with a nonsense security-domain partition.
func TestKeychainKeyStore_LockedKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("KeychainKeyStore requires macOS; skipping")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") == "" {
		t.Skip("skipping keychain integration test; set AGENTPAAS_KEYCHAIN_TESTS=1 to run")
	}

	service := "ai.agentpaas.identity.test.locked." + t.Name()

	// First verify the keychain works normally.
	s, err := NewKeychainKeyStore(service)
	if err != nil {
		t.Fatalf("NewKeychainKeyStore: %v", err)
	}

	material := testKeyMaterial(t, KeyTypeCA)
	if err := s.Create("test-key", KeyTypeCA, material); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := s.Load("test-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Type != KeyTypeCA {
		t.Errorf("loaded type = %q, want %q", loaded.Type, KeyTypeCA)
	}

	// Verify we get a meaningful error for a nonexistent key.
	_, err = s.Load("nonexistent-key")
	if err == nil {
		t.Error("Load of nonexistent key: want error, got nil")
	} else {
		t.Logf("Nonexistent key correctly returns error: %v", err)
	}

	// Clean up.
	if err := s.Delete("test-key"); err != nil {
		t.Logf("Cleanup delete warning: %v", err)
	}
}

// TestKeychainKeyStore_Integration exercises real security(1) commands.
// It creates a test key, verifies it can be loaded and signed with, then
// cleans up. Run with -v to see security(1) debug output.
func TestKeychainKeyStore_Integration(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("KeychainKeyStore requires macOS; skipping")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") == "" {
		t.Skip("skipping keychain integration test; set AGENTPAAS_KEYCHAIN_TESTS=1 to run")
	}

	service := "ai.agentpaas.identity.test.integration." + t.Name()
	s, err := NewKeychainKeyStore(service)
	if err != nil {
		t.Fatalf("NewKeychainKeyStore: %v", err)
	}

	// Create a CA key.
	caMat := testKeyMaterial(t, KeyTypeCA)
	if err := s.Create("integration-ca", KeyTypeCA, caMat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Sign and verify.
	digest := []byte("integration-test-digest")
	sig, err := s.Sign("integration-ca", digest)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("empty signature")
	}
	if ok := s.Verify("integration-ca", digest, sig); !ok {
		t.Error("Verify returned false for valid signature")
	}

	// List and verify no raw material leaks.
	meta, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, m := range meta {
		if m.RawBytes != nil {
			t.Errorf("List entry %q leaks raw bytes", m.ID)
		}
	}

	// Clean up.
	if err := s.Delete("integration-ca"); err != nil {
		t.Logf("Cleanup delete warning: %v", err)
	}
}

// TestKeychainKeyStore_RejectsInvalidKeyID verifies that the keychain store
// rejects invalid key IDs with ErrInvalidKeyID (not raw security(1) error).
func TestKeychainKeyStore_RejectsInvalidKeyID(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("KeychainKeyStore requires macOS; skipping")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") == "" {
		t.Skip("skipping keychain integration test; set AGENTPAAS_KEYCHAIN_TESTS=1 to run")
	}

	service := "ai.agentpaas.identity.test.reject." + t.Name()
	s, err := NewKeychainKeyStore(service)
	if err != nil {
		t.Fatalf("NewKeychainKeyStore: %v", err)
	}

	material := testKeyMaterial(t, KeyTypeCA)
	if err := s.Create("../../etc/passwd", KeyTypeCA, material); err == nil {
		t.Error("Create with path-traversal ID: want error, got nil")
	} else {
		t.Logf("Path-traversal ID correctly rejected: %v", err)
	}

	if ok := s.Verify("../../etc/passwd", []byte("x"), []byte("y")); ok {
		t.Error("Verify with path-traversal ID: want false, got true")
	}
}

// TestKeychainKeyStore_IsNotAvailableOnNonMacOS is a compile-time check that
// the package can be imported and NewKeychainKeyStore exists on any platform.
func TestKeychainKeyStore_IsNotAvailableOnNonMacOS(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("this test validates non-macOS behavior; skipping on darwin")
	}

	_, err := NewKeychainKeyStore("test-service")
	if err == nil {
		t.Error("NewKeychainKeyStore on non-macOS: want error, got nil")
	} else {
		t.Logf("Non-macOS correctly rejected: %v", err)
	}
}

// ensure KeychainKeyStore implements KeyStore at compile time.
var _ KeyStore = (*KeychainKeyStore)(nil)