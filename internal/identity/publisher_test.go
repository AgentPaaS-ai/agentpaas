package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidatePublisherName_Valid(t *testing.T) {
	valid := []string{
		"a",
		"ab",
		"my-publisher",
		"publisher-1",
		"a0b",
		"test-publisher-2024",
		"x" + strings.Repeat("y", 38), // 39 chars
	}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePublisherName(name); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidatePublisherName_Invalid(t *testing.T) {
	invalid := []string{
		"",           // empty
		"-leading",   // starts with hyphen
		"trailing-",  // ends with hyphen
		"UPPERCASE",  // has uppercase
		"has space",  // has space
		"has_underscore", // has underscore
		"under_score",    // has underscore
		"emoji😀",     // has emoji
		"special!",   // has special char
		strings.Repeat("x", 40), // 40 chars (over limit)
		"-",          // single hyphen
	}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePublisherName(name); err == nil {
				t.Errorf("expected error for %q, got nil", name)
			}
		})
	}
}

func TestPublisherFingerprint_MatchesPackDefinition(t *testing.T) {
	// Generate a key and verify PublisherFingerprint computes
	// hex(sha256(DER-encoded SPKI)) — same as pack.PublicKeyFingerprint.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	fp := PublisherFingerprint(&key.PublicKey)

	// Verify it's 64 lowercase hex
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(fp))
	}
	for _, c := range fp {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("fingerprint contains non-lowercase-hex character: %c", c)
		}
	}

	// Verify it matches the canonical SPKI-DER sha256 computation
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	expected := hex.EncodeToString(sum[:])
	if fp != expected {
		t.Errorf("PublisherFingerprint = %s, want %s", fp, expected)
	}
}

func TestPublisherFingerprint_NilKey(t *testing.T) {
	if got := PublisherFingerprint(nil); got != "" {
		t.Errorf("PublisherFingerprint(nil) = %q, want empty", got)
	}
}

func TestFormatFingerprintDisplay(t *testing.T) {
	fp := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6f7e8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	expected := "a1b2 c3d4 e5f6 a7b8 c9d0 e1f2 a3b4 c5d6 f7e8 a9b0 c1d2 e3f4 a5b6 c7d8 e9f0 a1b2"
	if got := FormatFingerprintDisplay(fp); got != expected {
		t.Errorf("FormatFingerprintDisplay:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestFormatFingerprintDisplay_ShortFingerprint(t *testing.T) {
	// Should still work for shorter fingerprints
	if got := FormatFingerprintDisplay("abcd"); got != "abcd" {
		t.Errorf("FormatFingerprintDisplay(abcd) = %q, want %q", got, "abcd")
	}
}

func TestParseFingerprintDisplay(t *testing.T) {
	input := "a1b2 c3d4 e5f6 a7b8 c9d0 e1f2 a3b4 c5d6 f7e8 a9b0 c1d2 e3f4 a5b6 c7d8 e9f0 a1b2"
	expected := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6f7e8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	if got := ParseFingerprintDisplay(input); got != expected {
		t.Errorf("ParseFingerprintDisplay:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestParseFingerprintDisplay_AlreadyBare(t *testing.T) {
	input := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6f7e8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	if got := ParseFingerprintDisplay(input); got != input {
		t.Errorf("ParseFingerprintDisplay on bare hex = %q, want unchanged", got)
	}
}

func TestCreatePublisherIdentity_Success(t *testing.T) {
	ks := NewFakeKeyStore()
	name := "test-publisher"

	ident, err := CreatePublisherIdentity(ks, name)
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}

	// Check returned struct
	if ident.Name != name {
		t.Errorf("Name = %q, want %q", ident.Name, name)
	}
	if ident.Fingerprint == "" {
		t.Error("Fingerprint is empty")
	}
	if len(ident.Fingerprint) != 64 {
		t.Errorf("Fingerprint length = %d, want 64", len(ident.Fingerprint))
	}
	if ident.PublicKeyPEM == "" {
		t.Error("PublicKeyPEM is empty")
	}
	if ident.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	// Verify no private key material in the struct
	verifyNoPrivateMaterial(t, ident, "PublisherIdentity from CreatePublisherIdentity")

	// Verify fingerprint matches PublicKeyPEM
	pub := parsePublicKeyFromPEM(t, ident.PublicKeyPEM)
	recomputedFP := PublisherFingerprint(pub)
	if recomputedFP != ident.Fingerprint {
		t.Errorf("fingerprint mismatch: stored=%s, recomputed=%s", ident.Fingerprint, recomputedFP)
	}
}

func TestCreatePublisherIdentity_AlreadyExists(t *testing.T) {
	ks := NewFakeKeyStore()

	_, err := CreatePublisherIdentity(ks, "first-publisher")
	if err != nil {
		t.Fatalf("first CreatePublisherIdentity: %v", err)
	}

	_, err = CreatePublisherIdentity(ks, "second-publisher")
	if !errors.Is(err, ErrPublisherIdentityExists) {
		t.Errorf("expected ErrPublisherIdentityExists, got: %v", err)
	}
}

func TestLoadPublisherIdentity_Success(t *testing.T) {
	ks := NewFakeKeyStore()

	created, err := CreatePublisherIdentity(ks, "my-publisher")
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}

	loaded, err := LoadPublisherIdentity(ks)
	if err != nil {
		t.Fatalf("LoadPublisherIdentity: %v", err)
	}

	if loaded.Name != created.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, created.Name)
	}
	if loaded.Fingerprint != created.Fingerprint {
		t.Errorf("Fingerprint: got %q, want %q", loaded.Fingerprint, created.Fingerprint)
	}
	if loaded.PublicKeyPEM != created.PublicKeyPEM {
		t.Errorf("PublicKeyPEM mismatch")
	}

	// Verify no private key material
	verifyNoPrivateMaterial(t, loaded, "PublisherIdentity from LoadPublisherIdentity")
}

func TestLoadPublisherIdentity_NotFound(t *testing.T) {
	ks := NewFakeKeyStore()

	_, err := LoadPublisherIdentity(ks)
	if !errors.Is(err, ErrNoPublisherIdentity) {
		t.Errorf("expected ErrNoPublisherIdentity, got: %v", err)
	}
}

func TestSignAsPublisher_Success(t *testing.T) {
	ks := NewFakeKeyStore()

	ident, err := CreatePublisherIdentity(ks, "my-publisher")
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}

	digest := sha256.Sum256([]byte("test message"))
	sig, err := SignAsPublisher(ks, digest[:])
	if err != nil {
		t.Fatalf("SignAsPublisher: %v", err)
	}
	if len(sig) == 0 {
		t.Error("signature is empty")
	}

	// Verify signature against the public key
	pub := parsePublicKeyFromPEM(t, ident.PublicKeyPEM)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Error("signature verification failed")
	}
}

func TestSignAsPublisher_NoIdentity(t *testing.T) {
	ks := NewFakeKeyStore()

	digest := sha256.Sum256([]byte("test"))
	_, err := SignAsPublisher(ks, digest[:])
	if !errors.Is(err, ErrNoPublisherIdentity) {
		t.Errorf("expected ErrNoPublisherIdentity, got: %v", err)
	}
}

func TestPublisherIdentity_RoundTrip(t *testing.T) {
	ks := NewFakeKeyStore()

	// Create
	created, err := CreatePublisherIdentity(ks, "round-trip-pub")
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}

	// Load
	loaded, err := LoadPublisherIdentity(ks)
	if err != nil {
		t.Fatalf("LoadPublisherIdentity: %v", err)
	}

	// Sign a different message, verify
	msg := []byte("another message for signing")
	digest := sha256.Sum256(msg)
	sig, err := SignAsPublisher(ks, digest[:])
	if err != nil {
		t.Fatalf("SignAsPublisher: %v", err)
	}

	// Verify against both created and loaded public key
	pub1 := parsePublicKeyFromPEM(t, created.PublicKeyPEM)
	pub2 := parsePublicKeyFromPEM(t, loaded.PublicKeyPEM)

	if !ecdsa.VerifyASN1(pub1, digest[:], sig) {
		t.Error("created public key failed verification")
	}
	if !ecdsa.VerifyASN1(pub2, digest[:], sig) {
		t.Error("loaded public key failed verification")
	}

	// Loaded must match Created
	if loaded.Fingerprint != created.Fingerprint {
		t.Error("fingerprints differ between create and load")
	}
	if loaded.Name != created.Name {
		t.Error("names differ between create and load")
	}
	if loaded.PublicKeyPEM != created.PublicKeyPEM {
		t.Error("PublicKeyPEM differs between create and load")
	}

	// Verify no private material
	verifyNoPrivateMaterial(t, created, "created identity")
	verifyNoPrivateMaterial(t, loaded, "loaded identity")
}

func TestCreatePublisherIdentity_StoresName(t *testing.T) {
	ks := NewFakeKeyStore()
	name := "my-custom-pub"

	_, err := CreatePublisherIdentity(ks, name)
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}

	// Verify name is stored in a separate keystore entry
	nameMaterial, err := ks.Load(publisherIdentityNameKeyID)
	if err != nil {
		t.Fatalf("Load publisher_identity_name: %v", err)
	}
	if string(nameMaterial.Bytes) != name {
		t.Errorf("stored name = %q, want %q", string(nameMaterial.Bytes), name)
	}
	if nameMaterial.Type != KeyTypePublisher {
		t.Errorf("name material type = %q, want %q", nameMaterial.Type, KeyTypePublisher)
	}

	// Verify key is stored in publisher_identity entry
	keyMaterial, err := ks.Load(publisherIdentityKeyID)
	if err != nil {
		t.Fatalf("Load publisher_identity: %v", err)
	}
	if keyMaterial.Type != KeyTypePublisher {
		t.Errorf("key material type = %q, want %q", keyMaterial.Type, KeyTypePublisher)
	}
	// Should be a valid PEM-encoded ECDSA private key
	priv, err := parseECDSAPrivateKey(keyMaterial.Bytes)
	if err != nil {
		t.Fatalf("parseECDSAPrivateKey: %v", err)
	}
	if priv.Curve != elliptic.P256() {
		t.Errorf("curve = %v, want P-256", priv.Curve)
	}
}

func TestLoadPublisherIdentity_NameNotStored(t *testing.T) {
	// If the key exists but the name entry is missing, Load should still work
	// but return empty name
	ks := NewFakeKeyStore()

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalECPrivateKey(key)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	_ = ks.Create(publisherIdentityKeyID, KeyTypePublisher, KeyMaterial{Type: KeyTypePublisher, Bytes: pemBytes})

	ident, err := LoadPublisherIdentity(ks)
	if err != nil {
		t.Fatalf("LoadPublisherIdentity with missing name: %v", err)
	}
	if ident.Fingerprint == "" {
		t.Error("fingerprint is empty")
	}
	// Name might be empty if name entry is missing
}

// Darwin integration test, gated by AGENTPAAS_KEYCHAIN_TESTS=1
func TestPublisherIdentity_KeychainRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychain tests only run on macOS")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") != "1" {
		t.Skip("AGENTPAAS_KEYCHAIN_TESTS not set")
	}

	ks, err := NewKeychainKeyStore("agentpaas-test-publisher")
	if err != nil {
		t.Fatalf("NewKeychainKeyStore: %v", err)
	}
	// Clean up any pre-existing test entries
	_ = ks.Delete(publisherIdentityKeyID)
	_ = ks.Delete(publisherIdentityNameKeyID)

	name := "keychain-test-" + time.Now().Format("20060102-150405")
	t.Logf("creating publisher identity: %s", name)

	// Create
	created, err := CreatePublisherIdentity(ks, name)
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}
	t.Logf("created fingerprint: %s", created.Fingerprint)

	// Verify no private material
	verifyNoPrivateMaterial(t, created, "keychain-create")

	// Load
	loaded, err := LoadPublisherIdentity(ks)
	if err != nil {
		t.Fatalf("LoadPublisherIdentity: %v", err)
	}
	if loaded.Fingerprint != created.Fingerprint {
		t.Errorf("fingerprint mismatch: created=%s loaded=%s", created.Fingerprint, loaded.Fingerprint)
	}
	if loaded.Name != created.Name {
		t.Errorf("name mismatch: created=%q loaded=%q", created.Name, loaded.Name)
	}

	// Verify no private material
	verifyNoPrivateMaterial(t, loaded, "keychain-load")

	// Sign & verify round-trip
	msg := []byte("keychain signing test message")
	digest := sha256.Sum256(msg)
	sig, err := SignAsPublisher(ks, digest[:])
	if err != nil {
		t.Fatalf("SignAsPublisher: %v", err)
	}

	pub := parsePublicKeyFromPEM(t, loaded.PublicKeyPEM)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Error("keychain signature verification failed")
	}

	// Cleanup
	_ = ks.Delete(publisherIdentityKeyID)
	_ = ks.Delete(publisherIdentityNameKeyID)
}

// helpers

func parsePublicKeyFromPEM(t *testing.T, pemStr string) *ecdsa.PublicKey {
	t.Helper()
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("failed to decode public key PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKIXPublicKey: %v", err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("public key is %T, not *ecdsa.PublicKey", pub)
	}
	return ecdsaPub
}

func verifyNoPrivateMaterial(t *testing.T, v interface{}, label string) {
	t.Helper()
	// Marshal to JSON and check for PRIVATE keyword
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%s): %v", label, err)
	}
	jsonStr := string(data)
	if strings.Contains(jsonStr, "PRIVATE") {
		t.Errorf("%s: JSON output contains PRIVATE keyword (private key material leaked)", label)
	}
	if strings.Contains(jsonStr, "private") {
		t.Errorf("%s: JSON output contains 'private' keyword (private key material leaked)", label)
	}
	// Also check for PEM headers
	if strings.Contains(jsonStr, "EC PRIVATE KEY") {
		t.Errorf("%s: JSON output contains EC PRIVATE KEY", label)
	}
}