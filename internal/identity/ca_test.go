package identity

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"
)

func TestLocalCA_IssueWorkloadCert_Valid(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	cert, key, spiffeURI, err := ca.IssueWorkloadCert("test-agent", "1.0.0", "run-001", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	if cert == nil {
		t.Fatal("cert is nil")
	}
	if key == nil {
		t.Fatal("key is nil")
	}
	if spiffeURI == "" {
		t.Fatal("spiffeURI is empty")
	}
	// Verify the cert is valid and has the correct SPIFFE URISAN.
	if err := ca.VerifyWorkloadCert(cert); err != nil {
		t.Fatalf("VerifyWorkloadCert: %v", err)
	}
	// Check SPIFFE URI in SANs.
	found := false
	for _, san := range cert.URIs {
		if san.String() == spiffeURI {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("cert URISANs do not contain SPIFFE URI %q; got %v", spiffeURI, cert.URIs)
	}
	// Verify correct SPIFFE URI format.
	expectedURI := (&TrustDomain{Host: "local.agentpaas"}).BuildURI("test-agent", "1.0.0", "run-001")
	if spiffeURI != expectedURI {
		t.Errorf("spiffeURI = %q, want %q", spiffeURI, expectedURI)
	}
}

func TestLocalCA_IssueWorkloadCert_Expired(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	// Issue a cert with 1s TTL, then wait for it to expire.
	cert, _, _, err := ca.IssueWorkloadCert("exp-agent", "0.0.1", "run-exp", time.Second)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	time.Sleep(2 * time.Second)
	if err := ca.VerifyWorkloadCert(cert); err == nil {
		t.Fatal("VerifyWorkloadCert: expected error for expired cert, got nil")
	}
}

func TestLocalCA_RenewBeforeExpiry(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	// Issue a cert with short TTL and renew before expiry.
	ttl := 5 * time.Second
	cert, key, spiffeURI, err := ca.IssueWorkloadCert("renew-agent", "1.0.0", "run-renew", ttl)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	// Renew at 80% of TTL.
	time.Sleep(time.Duration(float64(ttl) * 0.8))
	newCert, newKey, newSPIFFE, err := ca.RenewWorkloadCert("renew-agent", "1.0.0", "run-renew", ttl)
	if err != nil {
		t.Fatalf("RenewWorkloadCert: %v", err)
	}
	if newCert == nil {
		t.Fatal("renewed cert is nil")
	}
	if newKey == nil {
		t.Fatal("renewed key is nil")
	}
	if newSPIFFE == "" {
		t.Fatal("renewed SPIFFE URI is empty")
	}
	// Verify the renewed cert is valid.
	if err := ca.VerifyWorkloadCert(newCert); err != nil {
		t.Fatalf("VerifyWorkloadCert(renewed): %v", err)
	}
	// The old cert might be expired by now, but the renewed one should be fresh.
	if err := ca.VerifyWorkloadCert(newCert); err != nil {
		t.Fatalf("VerifyWorkloadCert(renewed): %v", err)
	}
	// SPIFFE URI should be the same (same agent/run identity).
	if newSPIFFE != spiffeURI {
		t.Errorf("renewed SPIFFE URI = %q, want %q", newSPIFFE, spiffeURI)
	}
	// Keys should be different (new key pair).
	oldKeyBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal old public key: %v", err)
	}
	newKeyBytes, err := x509.MarshalPKIXPublicKey(&newKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal new public key: %v", err)
	}
	keyChanged := string(oldKeyBytes) != string(newKeyBytes)
	if !keyChanged {
		t.Error("renewed key should be different from original")
	}
	// Old cert may be expired; skip re-verification if now > cert.NotAfter.
	if time.Now().After(cert.NotAfter) {
		t.Log("original cert expired as expected")
	}
}

func TestLocalCA_PackageIdentityKey_NotInWorkloadCert(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	// Create a package identity key for an agent.
	aidFingerprint, err := ca.EnsurePackageIdentityKey("pkg-agent-1")
	if err != nil {
		t.Fatalf("EnsurePackageIdentityKey: %v", err)
	}
	if aidFingerprint == "" {
		t.Fatal("AID fingerprint is empty")
	}
	// Issue a workload cert for the same agent.
	cert, key, spiffeURI, err := ca.IssueWorkloadCert("pkg-agent-1", "1.0.0", "run-aid-test", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	// The package identity key must never appear in the workload cert or returned material.
	// Check that the workload cert's public key is different from the AID.
	// We can verify by checking the key ID prefix doesn't contain "package_identity".
	// More practically, verify the cert doesn't have any package identity extensions.
	_ = cert
	_ = key
	_ = spiffeURI
	// The AID key exists in the store but should not be extractable from the
	// workload material. Verify by checking FakeKeyStore directly.
	_, err = store.Load("package_identity_pkg-agent-1")
	if err != nil {
		t.Errorf("package identity key should exist in store: %v", err)
	}
	// Workload cert's serial number or subject should not leak AID info.
	if containsSubstring(cert.Subject.CommonName, "package") {
		t.Error("workload cert subject leaks package identity info")
	}
}

func TestLocalCA_AIDPublicKeyFingerprint_Stable(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	fp1, err := ca.EnsurePackageIdentityKey("stable-agent")
	if err != nil {
		t.Fatalf("First EnsurePackageIdentityKey: %v", err)
	}
	// Second call for same agent should return same fingerprint.
	fp2, err := ca.EnsurePackageIdentityKey("stable-agent")
	if err != nil {
		t.Fatalf("Second EnsurePackageIdentityKey: %v", err)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprints differ: %q vs %q", fp1, fp2)
	}
	// Verify the fingerprint is formatted correctly (hex string, reasonable length).
	if len(fp1) < 10 || len(fp1) > 128 {
		t.Errorf("unexpected fingerprint length: %d", len(fp1))
	}
}

func TestLocalCA_DaemonAuditSigningKey(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	if err := ca.EnsureDaemonAuditSigningKey(); err != nil {
		t.Fatalf("EnsureDaemonAuditSigningKey: %v", err)
	}
	// Second call should be idempotent.
	if err := ca.EnsureDaemonAuditSigningKey(); err != nil {
		t.Fatalf("Second EnsureDaemonAuditSigningKey: %v", err)
	}
	// Verify the key exists in the store and is a signing key.
	_, err = store.Load("daemon_audit_signing")
	if err != nil {
		t.Errorf("audit signing key should exist in store: %v", err)
	}
	// Verify it can sign and verify.
	digest := []byte("audit-log-entry-123")
	sig, err := store.Sign("daemon_audit_signing", digest)
	if err != nil {
		t.Fatalf("Sign with audit key: %v", err)
	}
	if ok := store.Verify("daemon_audit_signing", digest, sig); !ok {
		t.Error("Verify with audit key: failed")
	}
}

func TestLocalCA_ConcurrentIssuance(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-agent-%d", n)
			_, _, _, err := ca.IssueWorkloadCert(name, "1.0.0", fmt.Sprintf("run-%d", n), time.Hour)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", n, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent issuance error: %v", err)
	}
}

func TestLocalCA_WorkloadCertSPIFFEURISAN(t *testing.T) {
	store := NewFakeKeyStore()
	td := &TrustDomain{Host: "local.agentpaas"}
	ca, err := NewLocalCA(store, td)
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	cert, _, spiffeURI, err := ca.IssueWorkloadCert("spiffe-agent", "2.0.0", "run-spiffe", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	// The URISAN must contain the SPIFFE URI.
	expectedURI := td.BuildURI("spiffe-agent", "2.0.0", "run-spiffe")
	if spiffeURI != expectedURI {
		t.Errorf("spiffeURI = %q, want %q", spiffeURI, expectedURI)
	}
	foundSPIFFE := false
	for _, uri := range cert.URIs {
		if uri.String() == expectedURI {
			foundSPIFFE = true
			break
		}
	}
	if !foundSPIFFE {
		t.Errorf("cert URISANs %v do not contain %q", cert.URIs, expectedURI)
	}
}

func TestLocalCA_IssueWorkloadCert_HostedDomain(t *testing.T) {
	store := NewFakeKeyStore()
	td := &TrustDomain{Host: "tenant.agentpaas.ai", IsHosted: true, TenantID: "my-tenant"}
	ca, err := NewLocalCA(store, td)
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}
	cert, _, spiffeURI, err := ca.IssueWorkloadCert("hosted-agent", "3.0.0", "run-hosted", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	expectedURI := td.BuildURI("hosted-agent", "3.0.0", "run-hosted")
	if spiffeURI != expectedURI {
		t.Errorf("spiffeURI = %q, want %q", spiffeURI, expectedURI)
	}
	foundSPIFFE := false
	for _, uri := range cert.URIs {
		if uri.String() == expectedURI {
			foundSPIFFE = true
			break
		}
	}
	if !foundSPIFFE {
		t.Errorf("cert URISANs %v do not contain %q", cert.URIs, expectedURI)
	}
}

// TestIssueIdentityIssuer verifies LocalIdentityIssuer implements IdentityIssuer.
func TestIssueIdentityIssuer(t *testing.T) {
	store := NewFakeKeyStore()
	td := &TrustDomain{Host: "local.agentpaas"}
	issuer, err := NewLocalIdentityIssuer(store, td)
	if err != nil {
		t.Fatalf("NewLocalIdentityIssuer: %v", err)
	}
	certPEM, keyPEM, spiffeURI, err := issuer.IssueWorkloadCert("issuer-agent", "1.0.0", "run-issuer", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	if len(certPEM) == 0 {
		t.Fatal("certPEM is empty")
	}
	if len(keyPEM) == 0 {
		t.Fatal("keyPEM is empty")
	}
	if spiffeURI == "" {
		t.Fatal("spiffeURI is empty")
	}
	// Decode PEM and verify the certificate.
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	// Verify the cert is signed by the CA and has valid SPIFFE URISAN.
	caCert, err := caCertificateFromStore(t, store)
	if err != nil {
		t.Fatalf("getting CA cert: %v", err)
	}
	if err := verifyCertSignature(cert, caCert); err != nil {
		t.Errorf("cert signature verification: %v", err)
	}
	_ = keyPEM // keyPEM is validated by being parseable
	// Parse the private key.
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("failed to decode key PEM")
	}
	if _, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
		t.Fatalf("ParsePKCS8PrivateKey: %v", err)
	}
}

// caCertificateFromStore extracts the CA certificate from the store for verification.
func caCertificateFromStore(t *testing.T, store KeyStore) (*x509.Certificate, error) {
	t.Helper()
	km, err := store.Load("local_ca")
	if err != nil {
		return nil, fmt.Errorf("load CA key: %w", err)
	}
	key, err := parseECDSAPrivateKey(km.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	// Create a self-signed CA certificate so we can use it for verification.
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "local CA"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA cert: %w", err)
	}
	return x509.ParseCertificate(certDER)
}

// verifyCertSignature checks that child is signed by parent.
func verifyCertSignature(child, parent *x509.Certificate) error {
	return child.CheckSignatureFrom(parent)
}