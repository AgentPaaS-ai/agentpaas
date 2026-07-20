package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestTrustDomainLocal_BuildAndParseRoundTrip(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}
	uri, err := td.BuildURI("my-agent", "1.0.0", "run-abc123")
	if err != nil {
		t.Fatalf("BuildURI: %v", err)
	}
	name, ver, rid, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if name != "my-agent" {
		t.Errorf("agentName = %q, want %q", name, "my-agent")
	}
	if ver != "1.0.0" {
		t.Errorf("agentVersion = %q, want %q", ver, "1.0.0")
	}
	if rid != "run-abc123" {
		t.Errorf("runID = %q, want %q", rid, "run-abc123")
	}
}

func TestTrustDomainHosted_BuildAndParseRoundTrip(t *testing.T) {
	td := &TrustDomain{Host: "tenant.agentpaas.ai", IsHosted: true, TenantID: "acme-corp"}
	uri, err := td.BuildURI("assistant", "2.1.0", "run-def456")
	if err != nil {
		t.Fatalf("BuildURI: %v", err)
	}
	name, ver, rid, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if name != "assistant" {
		t.Errorf("agentName = %q, want %q", name, "assistant")
	}
	if ver != "2.1.0" {
		t.Errorf("agentVersion = %q, want %q", ver, "2.1.0")
	}
	if rid != "run-def456" {
		t.Errorf("runID = %q, want %q", rid, "run-def456")
	}
	// Verify the hosted URI contains the tenant segment
	if !containsHostedTenant(uri, "acme-corp") {
		t.Errorf("hosted URI missing tenant segment: %s", uri)
	}
}

func TestVerifyURI_RejectsWrongName(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}
	uri, err := td.BuildURI("correct-name", "1.0.0", "run-111")
	if err != nil {
		t.Fatalf("BuildURI: %v", err)
	}
	err = VerifyURI(uri, "wrong-name", "1.0.0")
	if err == nil {
		t.Fatal("VerifyURI: expected error for wrong agent name, got nil")
	}
}

func TestVerifyURI_RejectsWrongVersion(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}
	uri, err := td.BuildURI("agent", "2.0.0", "run-222")
	if err != nil {
		t.Fatalf("BuildURI: %v", err)
	}
	err = VerifyURI(uri, "agent", "9.9.9")
	if err == nil {
		t.Fatal("VerifyURI: expected error for wrong version, got nil")
	}
}

func TestVerifyURI_PassesCorrectIdentity(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}
	uri, err := td.BuildURI("ok-agent", "3.0.0", "run-333")
	if err != nil {
		t.Fatalf("BuildURI: %v", err)
	}
	err = VerifyURI(uri, "ok-agent", "3.0.0")
	if err != nil {
		t.Fatalf("VerifyURI: unexpected error: %v", err)
	}
}

func TestAlternateHostedDomain_PassesWithoutSchemaChange(t *testing.T) {
	// Both local and hosted use the same spiffe://host/agent/... schema
	// Only the trust domain hostname changes.
	localTD := &TrustDomain{Host: "local.agentpaas"}
	hostedTD := &TrustDomain{Host: "tenant.agentpaas.ai", IsHosted: true, TenantID: "other-tenant"}

	localURI, err := localTD.BuildURI("a", "1", "r1")
	if err != nil {
		t.Fatalf("BuildURI(local): %v", err)
	}
	hostedURI, err := hostedTD.BuildURI("b", "2", "r2")
	if err != nil {
		t.Fatalf("BuildURI(hosted): %v", err)
	}

	// Both should parse identically via the same ParseURI function
	_, _, _, err = ParseURI(localURI)
	if err != nil {
		t.Fatalf("ParseURI(local): %v", err)
	}
	_, _, _, err = ParseURI(hostedURI)
	if err != nil {
		t.Fatalf("ParseURI(hosted): %v", err)
	}

	// Verify the URI schemas are the same — only host prefix differs
	if !containsPrefix(localURI, "spiffe://local.agentpaas/agent/") {
		t.Errorf("local URI unexpected format: %s", localURI)
	}
	if !containsPrefix(hostedURI, "spiffe://tenant.agentpaas.ai/") {
		t.Errorf("hosted URI unexpected format: %s", hostedURI)
	}
	if !containsSubstring(hostedURI, "/agent/") {
		t.Errorf("hosted URI missing /agent/ segment: %s", hostedURI)
	}
}

func TestParseURI_RejectsMalformed(t *testing.T) {
	invalid := []string{
		"",
		"not-a-uri",
		"spiffe://",
		"spiffe://local.agentpaas",
		"spiffe://local.agentpaas/agent",
		"spiffe://local.agentpaas/agent/name",
		"spiffe://local.agentpaas/agent/name/ver",
		"spiffe://local.agentpaas/agent/name/ver/run",
		"spiffe://local.agentpaas/agent/name/ver/run/",
		"spiffe:///agent/name/ver/run/rid",
		"https://local.agentpaas/agent/name/ver/run/rid",
	}
	for _, u := range invalid {
		t.Run(u, func(t *testing.T) {
			_, _, _, err := ParseURI(u)
			if err == nil {
				t.Errorf("ParseURI(%q): expected error, got nil", u)
			}
		})
	}
}

func TestExpiredCert_Rejected(t *testing.T) {
	// Create an expired certificate and verify VerifyWorkloadCert rejects it.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	caCert, err := createSelfSignedCert(key, time.Now().Add(-2*time.Hour), time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("createSelfSignedCert: %v", err)
	}
	if err := VerifyWorkloadCert(caCert); err == nil {
		t.Fatal("VerifyWorkloadCert: expected error for expired cert, got nil")
	}
}

func TestValidCert_Accepted(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cert, err := createSelfSignedCert(key, time.Now().Add(-1*time.Hour), time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("createSelfSignedCert: %v", err)
	}
	if err := VerifyWorkloadCert(cert); err != nil {
		t.Fatalf("VerifyWorkloadCert: unexpected error: %v", err)
	}
}

// containsHostedTenant checks if a hosted SPIFFE URI contains the tenant
// segment: spiffe://host/<tenant>/agent/...
func containsHostedTenant(uri, tenant string) bool {
	// Expected format: spiffe://tenant.agentpaas.ai/<tenant>/agent/...
	// So after the host, there's a single path segment for the tenant.
	return len(uri) > len("spiffe://x/") && containsSubstring(uri, "/"+tenant+"/agent/")
}

// containsPrefix checks if s starts with prefix.
func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

// searchString finds substr within s.
func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// createSelfSignedCert creates a self-signed x509 certificate for testing.
func createSelfSignedCert(key *ecdsa.PrivateKey, notBefore, notAfter time.Time) (*x509.Certificate, error) {
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(certDER)
}

func TestValidateURIComponent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		component string
		wantErr   bool
	}{
		{"ok", "agent-1", false},
		{"ok_version", "1.2.3", false},
		{"dotdot", "..", true},
		{"embedded_dotdot", "a..b", true}, // contains ".."
		{"slash", "a/b", true},
		{"leading_slash", "/a", true},
		{"empty_ok", "", false}, // empty is not traversal
		{"unicode_ok", "エージェント", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateURIComponent(tc.component, "field")
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateURIComponent(%q) expected error", tc.component)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateURIComponent(%q) unexpected: %v", tc.component, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidComponent) {
				t.Fatalf("want ErrInvalidComponent, got %v", err)
			}
		})
	}
}
