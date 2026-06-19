package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// ContractTests runs the KeyStore contract suite against a fresh store
// returned by newStore. Every implementation's _test.go can call this to
// guarantee conformance.
func ContractTests(t *testing.T, newStore func() KeyStore) {
	t.Helper()

	t.Run("CreateAndLoad", func(t *testing.T) {
		s := newStore()
		material := testKeyMaterial(t, KeyTypeCA)
		if err := s.Create("test-ca", KeyTypeCA, material); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := s.Load("test-ca")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Type != KeyTypeCA {
			t.Errorf("got type %q, want %q", got.Type, KeyTypeCA)
		}
		if len(got.Bytes) == 0 {
			t.Error("Load returned empty bytes")
		}
	})

	t.Run("CreateAndLoadAllKeyTypes", func(t *testing.T) {
		s := newStore()
		types := []KeyType{KeyTypeCA, KeyTypeAuditSigning, KeyTypePackageIdentity, KeyTypeWorkload}
		for _, kt := range types {
			m := testKeyMaterial(t, kt)
			if err := s.Create(KeyID(kt), kt, m); err != nil {
				t.Fatalf("Create(%s): %v", kt, err)
			}
		}
		for _, kt := range types {
			got, err := s.Load(KeyID(kt))
			if err != nil {
				t.Fatalf("Load(%s): %v", kt, err)
			}
			if got.Type != kt {
				t.Errorf("Load(%s) type = %q, want %q", kt, got.Type, kt)
			}
		}
	})

	t.Run("SignAndVerifyRoundtrip", func(t *testing.T) {
		s := newStore()
		material := testKeyMaterial(t, KeyTypeCA)
		if err := s.Create("sign-test", KeyTypeCA, material); err != nil {
			t.Fatalf("Create: %v", err)
		}
		digest := []byte("hello-world-digest")
		sig, err := s.Sign("sign-test", digest)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if len(sig) == 0 {
			t.Fatal("Sign returned empty signature")
		}
		if ok := s.Verify("sign-test", digest, sig); !ok {
			t.Error("Verify returned false for valid signature")
		}
	})

	t.Run("VerifyBadSignature", func(t *testing.T) {
		s := newStore()
		material := testKeyMaterial(t, KeyTypePackageIdentity)
		if err := s.Create("vfy-key", KeyTypePackageIdentity, material); err != nil {
			t.Fatalf("Create: %v", err)
		}
		digest := []byte("digest")
		sig, err := s.Sign("vfy-key", digest)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		// Tamper the signature.
		if len(sig) > 0 {
			sig[0] ^= 0xFF
		}
		if ok := s.Verify("vfy-key", digest, sig); ok {
			t.Error("Verify returned true for tampered signature")
		}
	})

	t.Run("VerifyWrongKey", func(t *testing.T) {
		s := newStore()
		m1 := testKeyMaterial(t, KeyTypeCA)
		m2 := testKeyMaterial(t, KeyTypeAuditSigning)
		if err := s.Create("key-a", KeyTypeCA, m1); err != nil {
			t.Fatalf("Create key-a: %v", err)
		}
		if err := s.Create("key-b", KeyTypeAuditSigning, m2); err != nil {
			t.Fatalf("Create key-b: %v", err)
		}
		digest := []byte("digest")
		sig, err := s.Sign("key-a", digest)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if ok := s.Verify("key-b", digest, sig); ok {
			t.Error("Verify returned true for signature from different key")
		}
	})

	t.Run("ListReturnsMetadata", func(t *testing.T) {
		s := newStore()
		if err := s.Create("lst-ca", KeyTypeCA, testKeyMaterial(t, KeyTypeCA)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := s.Create("lst-audit", KeyTypeAuditSigning, testKeyMaterial(t, KeyTypeAuditSigning)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		meta, err := s.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(meta) != 2 {
			t.Fatalf("List returned %d entries, want 2", len(meta))
		}
		// Verify each entry has expected metadata.
		found := map[KeyID]bool{"lst-ca": false, "lst-audit": false}
		for _, m := range meta {
			if m.ID == "lst-ca" || m.ID == "lst-audit" {
				found[m.ID] = true
			}
			if m.CreatedAt.IsZero() {
				t.Errorf("Key %q has zero CreatedAt", m.ID)
			}
		}
		for id, ok := range found {
			if !ok {
				t.Errorf("List missing key %q", id)
			}
		}
	})

	t.Run("ListContainsNoRawMaterial", func(t *testing.T) {
		s := newStore()
		if err := s.Create("no-raw", KeyTypeCA, testKeyMaterial(t, KeyTypeCA)); err != nil {
			t.Fatalf("Create: %v", err)
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
	})

	t.Run("DeleteRemovesKey", func(t *testing.T) {
		s := newStore()
		if err := s.Create("del-key", KeyTypeCA, testKeyMaterial(t, KeyTypeCA)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := s.Delete("del-key"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Load("del-key"); err == nil {
			t.Error("Load after Delete succeeded, expected error")
		}
	})

	t.Run("LoadNonexistent", func(t *testing.T) {
		s := newStore()
		if _, err := s.Load("nonexistent"); err == nil {
			t.Error("Load of nonexistent key: want error, got nil")
		}
	})

	t.Run("DeleteNonexistent", func(t *testing.T) {
		s := newStore()
		if err := s.Delete("nonexistent"); err == nil {
			t.Error("Delete of nonexistent key: want error, got nil")
		}
	})

	t.Run("SignWrongKeyType", func(t *testing.T) {
		s := newStore()
		// Workload keys are not signing keys; Sign should error.
		if err := s.Create("workload-key", KeyTypeWorkload, testKeyMaterial(t, KeyTypeWorkload)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := s.Sign("workload-key", []byte("digest")); err == nil {
			t.Error("Sign with workload key: want error for wrong key type, got nil")
		}
	})

	t.Run("ListEmpty", func(t *testing.T) {
		s := newStore()
		meta, err := s.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(meta) != 0 {
			t.Errorf("List on empty store returned %d entries, want 0", len(meta))
		}
	})
}

// testKeyMaterial generates realistic key material for the given key type.
// For signing key types (CA, AuditSigning, PackageIdentity) it generates a
// PEM-encoded ECDSA P-256 private key. For Workload it generates a fake
// PEM certificate + private key pair.
func testKeyMaterial(t *testing.T, kt KeyType) KeyMaterial {
	t.Helper()
	switch kt {
	case KeyTypeCA, KeyTypeAuditSigning, KeyTypePackageIdentity:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatalf("MarshalECPrivateKey: %v", err)
		}
		block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
		return KeyMaterial{Type: kt, Bytes: pem.EncodeToMemory(block)}
	case KeyTypeWorkload:
		// Generate a cert+key pair (simplified: self-signed cert + key).
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatalf("MarshalECPrivateKey: %v", err)
		}
		pemKey := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		// A real workload cert would have an x509 cert; for the contract test
		// we just need distinguishable bytes that include a cert-like block.
		certBlock := &pem.Block{Type: "CERTIFICATE", Bytes: []byte("fake-cert-der-bytes")}
		pemCert := pem.EncodeToMemory(certBlock)
		// Concatenate cert then key (common convention).
		return KeyMaterial{Type: kt, Bytes: append(pemCert, pemKey...)}
	default:
		t.Fatalf("unknown key type %q", kt)
		return KeyMaterial{}
	}
}