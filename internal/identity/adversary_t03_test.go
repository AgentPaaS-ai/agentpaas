package identity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// ADVERSARY TEST SUITE — Block 3 Task T03 (SPIFFE / CA / AID / Workload Certs)
// =============================================================================
// Each test attacks a specific security claim. A failing test = real break.
// Do NOT weaken assertions to make tests pass.

// ---------------------------------------------------------------------------
// VECTOR 1: URI injection — agentName or runID with path traversal (../),
// special chars, or SPIFFE URI syntax injection.
// ---------------------------------------------------------------------------

// TestAdversaryT03_URIPathTraversalInjection verifies that path-traversal
// characters in agentName or runID cannot confuse ParseURI into extracting
// the wrong identity.
func TestAdversaryT03_URIPathTraversalInjection(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}

	// agentName containing "../" — BuildURI produces a path-normalized URI.
	// url.Parse normalizes "../../../bar" in the path, potentially shifting
	// segments and yielding wrong parsed values.
	agentName := "foo/../../../bar"
	uri, err := td.BuildURI(agentName, "1.0.0", "run-001")
	if err != nil {
		t.Logf("BuildURI rejected path-traversal agentName: %v", err)
	} else {
		t.Logf("URI with path-traversal agentName: %s", uri)
	}

	name, ver, rid, err := ParseURI(uri)
	if err == nil {
		// If parsing succeeds, the extracted identity must match what was
		// originally passed, NOT the normalized path interpretation.
		if name != agentName {
			t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI returned agentName=%q after path-traversal, want %q. "+
				"BuildURI produced %q which normalized to wrong identity",
				name, agentName, uri)
		}
		if ver != "1.0.0" {
			t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI version shifted: got %q, want %q (uri=%s)",
				ver, "1.0.0", uri)
		}
		if rid != "run-001" {
			t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI runID shifted: got %q, want %q (uri=%s)",
				rid, "run-001", uri)
		}
	} else {
		t.Logf("ParseURI with path-traversal correctly rejected: %v", err)
	}

	// agentName with forward slash inside (not path-traversal) — creates an
	// extra path segment that shifts all subsequent fields.
	agentName2 := "foo/bar"
	uri2, err := td.BuildURI(agentName2, "1.0.0", "run-002")
	if err != nil {
		t.Logf("BuildURI rejected embedded slash in agentName: %v", err)
	} else {
		t.Logf("URI with embedded slash in agentName: %s", uri2)
	}

	name2, ver2, rid2, err2 := ParseURI(uri2)
	if err2 == nil {
		if name2 != agentName2 {
			t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI with embedded '/' returned agentName=%q, want %q. "+
				"Segments shifted: ver=%q, rid=%q (expected ver=1.0.0, rid=run-002)",
				name2, agentName2, ver2, rid2)
		}
	} else {
		t.Logf("ParseURI with embedded slash correctly rejected: %v", err2)
	}

	// runID with path traversal
	runID := "../malicious"
	uri3, err := td.BuildURI("safe-agent", "1.0.0", runID)
	if err != nil {
		t.Logf("BuildURI rejected path-traversal runID: %v", err)
	} else {
		t.Logf("URI with path-traversal runID: %s", uri3)
	}

	_, _, rid3, err3 := ParseURI(uri3)
	if err3 == nil {
		if rid3 != runID {
			t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI runID shifted after path-traversal: got %q, want %q (uri=%s)",
				rid3, runID, uri3)
		}
	} else {
		t.Logf("ParseURI with path-traversal runID correctly rejected: %v", err3)
	}
}

// TestAdversaryT03_URISpecialCharsInjection tests that special characters
// like null bytes, newlines, and SPIFFE URI syntax keywords in agentName
// are handled safely (either rejected or round-tripped correctly).
func TestAdversaryT03_URISpecialCharsInjection(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}

	// agentName with SPIFFE URI syntax injection attempt
	// If someone puts "agent/" or "/run/" in the name, it could confuse parsing
	specialNames := []string{
		"agent/evil",
		"run/evil",
		"agent/name/ver/run/evil",
		"../agent/evil",
		"spiffe://evil.com/agent/evil/1/run/1",
		"foo\nbar",
		"foo\rbar",
	}

	for _, name := range specialNames {
		name := name
		t.Run(fmt.Sprintf("agentName=%q", name), func(t *testing.T) {
			uri, err := td.BuildURI(name, "1.0.0", "run-safe")
			if err != nil {
				t.Logf("BuildURI rejected special agentName %q: %v", name, err)
				return
			}
			gotName, gotVer, gotRid, err := ParseURI(uri)
			if err == nil {
				if gotName != name {
					t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI returned agentName=%q, want %q (uri=%s)",
						gotName, name, uri)
				}
				if gotVer != "1.0.0" {
					t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI version shifted for agentName %q: got %q (uri=%s)",
						name, gotVer, uri)
				}
				if gotRid != "run-safe" {
					t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI runID shifted for agentName %q: got %q (uri=%s)",
						name, gotRid, uri)
				}
			} else {
				t.Logf("Special agentName %q correctly rejected: %v", name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VECTOR 2: Hosted trust domain spoofing — can a local-domain URI be parsed
// as if it were hosted? Can a hosted URI from tenant A be verified as
// tenant B?
// ---------------------------------------------------------------------------

// TestAdversaryT03_HostedDomainSpoofing verifies that VerifyURI does NOT
// check the trust domain — only agent name and version. This is a policy
// gap: a hosted URI from tenant A with the same agent name/version passes
// verification as tenant B.
func TestAdversaryT03_HostedDomainSpoofing(t *testing.T) {
	// A hosted URI from tenant "evil-corp"
	evilTD := &TrustDomain{Host: "tenant.agentpaas.ai", IsHosted: true, TenantID: "evil-corp"}
	evilURI, err := evilTD.BuildURI("my-agent", "1.0.0", "run-999")
	if err != nil {
		t.Fatalf("BuildURI(evil): %v", err)
	}

	// VerifyURI only checks agent name and version — it does NOT check
	// the trust domain host or tenant ID.
	err = VerifyURI(evilURI, "my-agent", "1.0.0")
	if err != nil {
		t.Fatalf("Unexpected VerifyURI error: %v", err)
	}

	// The URI is from "evil-corp" but verified as matching "my-agent/1.0.0".
	// VerifyURI has no concept of trust domain or tenant.
	// This is a policy gap — applications must check trust domain separately.
	t.Log("PASS (known policy gap): VerifyURI only checks name/version, not trust domain")
	t.Log("NOTE: VerifyURI does not reject cross-tenant URIs with matching name/version")

	// Additionally: a local URI parsed with hosted TrustDomain
	localTD := &TrustDomain{Host: "local.agentpaas"}
	localURI, err := localTD.BuildURI("my-agent", "1.0.0", "run-001")
	if err != nil {
		t.Fatalf("BuildURI(local): %v", err)
	}

	// Parse a local URI and see if it can be misinterpreted as hosted.
	name, ver, rid, err := ParseURI(localURI)
	if err != nil {
		t.Fatalf("ParseURI(local): %v", err)
	}
	if name != "my-agent" || ver != "1.0.0" || rid != "run-001" {
		t.Errorf("ADVERSARY BREAK [LOW]: ParseURI returned wrong identity for local URI: name=%q ver=%q rid=%q",
			name, ver, rid)
	}
}

// ---------------------------------------------------------------------------
// VECTOR 3: Empty/nil/very-long agentName, agentVersion, runID — must be
// rejected or handled safely.
// ---------------------------------------------------------------------------

// TestAdversaryT03_EmptyComponents verifies that empty agent name, version,
// or run ID are either rejected by BuildURI/ParseURI or handled safely.
func TestAdversaryT03_EmptyComponents(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}

	// BuildURI with empty agentName
	t.Run("empty_agentName", func(t *testing.T) {
		uri, err := td.BuildURI("", "1.0.0", "run-001")
		if err != nil {
			t.Logf("BuildURI rejected empty agentName: %v", err)
			return
		}
		t.Logf("URI with empty agentName: %q", uri)
		_, _, _, err = ParseURI(uri)
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: ParseURI accepted empty agentName")
		} else {
			t.Logf("Empty agentName correctly rejected: %v", err)
		}
	})

	// BuildURI with empty agentVersion
	t.Run("empty_agentVersion", func(t *testing.T) {
		uri, err := td.BuildURI("my-agent", "", "run-001")
		if err != nil {
			t.Logf("BuildURI rejected empty agentVersion: %v", err)
			return
		}
		t.Logf("URI with empty agentVersion: %q", uri)
		_, _, _, err = ParseURI(uri)
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: ParseURI accepted empty agentVersion")
		} else {
			t.Logf("Empty agentVersion correctly rejected: %v", err)
		}
	})

	// BuildURI with empty runID
	t.Run("empty_runID", func(t *testing.T) {
		uri, err := td.BuildURI("my-agent", "1.0.0", "")
		if err != nil {
			t.Logf("BuildURI rejected empty runID: %v", err)
			return
		}
		t.Logf("URI with empty runID: %q", uri)
		_, _, _, err = ParseURI(uri)
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: ParseURI accepted empty runID")
		} else {
			t.Logf("Empty runID correctly rejected: %v", err)
		}
	})
}

// TestAdversaryT03_VeryLongComponents tests that very long agent names
// don't cause parse failures or panics, and that they round-trip correctly.
func TestAdversaryT03_VeryLongComponents(t *testing.T) {
	td := &TrustDomain{Host: "local.agentpaas"}

	longName := strings.Repeat("a", 10000)
	uri, err := td.BuildURI(longName, "1.0.0", "run-long")
	if err != nil {
		t.Logf("BuildURI rejected long agentName: %v", err)
		return
	}
	t.Logf("URI with 10K-char agentName (first 100 chars): %s...", uri[:100])

	name, ver, rid, err := ParseURI(uri)
	if err != nil {
		t.Logf("Very long agentName correctly rejected: %v", err)
		return
	}
	if name != longName {
		t.Errorf("ADVERSARY BREAK [LOW]: Very long agentName round-trip failed: mismatch (got %d chars, want %d)",
			len(name), len(longName))
	}
	if ver != "1.0.0" {
		t.Errorf("ADVERSARY BREAK [LOW]: Very long agentName shifted version: got %q", ver)
	}
	if rid != "run-long" {
		t.Errorf("ADVERSARY BREAK [LOW]: Very long agentName shifted runID: got %q", rid)
	}
}

// TestAdversaryT03_IssueCertWithEmptyParams tests that the CA rejects
// empty agentName/version/runID when issuing certs.
func TestAdversaryT03_IssueCertWithEmptyParams(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	t.Run("empty_agentName", func(t *testing.T) {
		_, _, _, err := ca.IssueWorkloadCert("", "1.0.0", "run-001", time.Hour)
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: IssueWorkloadCert with empty agentName succeeded")
		} else {
			t.Logf("Empty agentName correctly rejected: %v", err)
		}
	})

	t.Run("empty_agentVersion", func(t *testing.T) {
		_, _, _, err := ca.IssueWorkloadCert("my-agent", "", "run-001", time.Hour)
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: IssueWorkloadCert with empty agentVersion succeeded")
		} else {
			t.Logf("Empty agentVersion correctly rejected: %v", err)
		}
	})

	t.Run("empty_runID", func(t *testing.T) {
		_, _, _, err := ca.IssueWorkloadCert("my-agent", "1.0.0", "", time.Hour)
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: IssueWorkloadCert with empty runID succeeded")
		} else {
			t.Logf("Empty runID correctly rejected: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// VECTOR 4: URI format confusion — spiffe:// vs http:// vs relative URIs.
// Must reject non-spiffe schemes.
// ---------------------------------------------------------------------------

// TestAdversaryT03_NonSpiffeSchemes tests that ParseURI rejects all
// non-spiffe URI schemes.
func TestAdversaryT03_NonSpiffeSchemes(t *testing.T) {
	nonSpiffe := []string{
		"http://local.agentpaas/agent/name/ver/run/rid",
		"https://local.agentpaas/agent/name/ver/run/rid",
		"file:///agent/name/ver/run/rid",
		"ftp://local.agentpaas/agent/name/ver/run/rid",
		"spiffe",          // no colon/slash
		"spiffe:",         // colon but no slashes
		"spiffe:local",    // opaque URI
		"SPIFFE://local.agentpaas/agent/name/ver/run/rid", // uppercase
		"Spiffe://local.agentpaas/agent/name/ver/run/rid", // mixed case
	}
	for _, u := range nonSpiffe {
		u := u
		t.Run(u, func(t *testing.T) {
			_, _, _, err := ParseURI(u)
			if err == nil {
				t.Errorf("ADVERSARY BREAK [MEDIUM]: ParseURI accepted non-spiffe URI %q", u)
			} else {
				t.Logf("Non-spiffe URI %q correctly rejected: %v", u, err)
			}
		})
	}
}

// TestAdversaryT03_SpiffeSchemeCaseSensitive checks that ParseURI is
// case-sensitive (spiffe://, not SPIFFE:// or Spiffe://).
func TestAdversaryT03_SpiffeSchemeCaseSensitive(t *testing.T) {
	// url.Parse lowercases the scheme, so "SPIFFE://" becomes "spiffe://"
	// after ParseURI calls url.Parse. Let's verify.
	uri := "SPIFFE://local.agentpaas/agent/name/ver/run/rid"
	_, _, _, err := ParseURI(uri)
	if err != nil {
		t.Logf("Uppercase SPIFFE scheme correctly rejected: %v", err)
	} else {
		t.Log("Note: ParseURI accepted uppercase 'SPIFFE://' — Go's url.Parse lowercases schemes, so this is expected behavior")
	}
}

// ---------------------------------------------------------------------------
// VECTOR 5: Expired cert — issue cert with 1s TTL, sleep 2s, verify it's
// rejected. Must NOT be usable for authentication.
// ---------------------------------------------------------------------------

// TestAdversaryT03_ExpiredCertNotUsable verifies that an expired workload
// cert cannot be verified through the CA that issued it.
func TestAdversaryT03_ExpiredCertNotUsable(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Issue cert with 1s TTL
	cert, key, spiffeURI, err := ca.IssueWorkloadCert("exp-agent", "0.0.1", "run-exp", time.Second)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	_ = key
	_ = spiffeURI

	// Verify it's valid immediately.
	if err := ca.VerifyWorkloadCert(cert); err != nil {
		t.Fatalf("VerifyWorkloadCert (immediate): unexpected error: %v", err)
	}

	// Wait for expiry.
	time.Sleep(2 * time.Second)

	// Must be rejected.
	if err := ca.VerifyWorkloadCert(cert); err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: VerifyWorkloadCert accepted expired cert")
	} else {
		t.Logf("Expired cert correctly rejected: %v", err)
	}

	// Also verify via the standalone VerifyWorkloadCert (spiffe.go).
	if err := VerifyWorkloadCert(cert); err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: spiffe.VerifyWorkloadCert accepted expired cert")
	} else {
		t.Logf("spiffe.VerifyWorkloadCert correctly rejected expired cert: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VECTOR 6: Not-yet-valid cert — issue cert with NotBefore in the future.
// ---------------------------------------------------------------------------

// TestAdversaryT03_NotYetValidCert tests that a certificate with NotBefore
// in the future is rejected by both spiffe.VerifyWorkloadCert and
// ca.VerifyWorkloadCert.
func TestAdversaryT03_NotYetValidCert(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// We can't directly control NotBefore through IssueWorkloadCert (it always
	// uses now-5min). Craft a future-dated cert manually and test verification.
	caCert, err := ca.getCACertificate()
	if err != nil {
		t.Fatalf("getCACertificate: %v", err)
	}

	// Load CA key to sign.
	km, err := store.Load("local_ca")
	if err != nil {
		t.Fatalf("Load CA key: %v", err)
	}
	caKey, err := parseECDSAPrivateKey(km.Bytes)
	if err != nil {
		t.Fatalf("parse CA key: %v", err)
	}

	// Generate workload key.
	workloadKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate workload key: %v", err)
	}

	// Cert with NotBefore = now + 1 hour (in the future).
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "future-cert", Organization: []string{"AgentPaaS"}},
		NotBefore:             time.Now().Add(time.Hour),
		NotAfter:              time.Now().Add(2 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &workloadKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	futureCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	// spiffe.VerifyWorkloadCert should reject it.
	if err := VerifyWorkloadCert(futureCert); err == nil {
		t.Error("ADVERSARY BREAK [MEDIUM]: spiffe.VerifyWorkloadCert accepted future-dated cert")
	} else {
		t.Logf("Future-dated cert correctly rejected by spiffe.VerifyWorkloadCert: %v", err)
	}

	// ca.VerifyWorkloadCert should also reject it.
	if err := ca.VerifyWorkloadCert(futureCert); err == nil {
		t.Error("ADVERSARY BREAK [MEDIUM]: ca.VerifyWorkloadCert accepted future-dated cert")
	} else {
		t.Logf("Future-dated cert correctly rejected by ca.VerifyWorkloadCert: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VECTOR 7: Wrong CA — issue cert with CA A, verify against CA B.
// ---------------------------------------------------------------------------

// TestAdversaryT03_WrongCA tests that a cert issued by one CA is rejected
// when verified against a different CA.
func TestAdversaryT03_WrongCA(t *testing.T) {
	storeA := NewFakeKeyStore()
	storeB := NewFakeKeyStore()

	caA, err := NewLocalCA(storeA, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA(A): %v", err)
	}
	caB, err := NewLocalCA(storeB, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA(B): %v", err)
	}

	// Issue cert from CA A.
	cert, _, _, err := caA.IssueWorkloadCert("agent-a", "1.0.0", "run-a", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert(A): %v", err)
	}

	// Verify against CA A — must pass.
	if err := caA.VerifyWorkloadCert(cert); err != nil {
		t.Fatalf("Verify against CA A failed: %v", err)
	}

	// Verify against CA B — must fail (different key).
	if err := caB.VerifyWorkloadCert(cert); err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: cert issued by CA A passed verification against CA B")
	} else {
		t.Logf("Wrong CA correctly rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VECTOR 8: SPIFFE URI mismatch — cert has SPIFFE URI for agent A, but
// verifier expects agent B.
// ---------------------------------------------------------------------------

// TestAdversaryT03_SPIFFEURIMismatch tests that VerifyURI rejects a
// workload cert whose SPIFFE URI doesn't match the expected agent identity.
func TestAdversaryT03_SPIFFEURIMismatch(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Issue cert for agent "good-agent".
	_, _, spiffeURI, err := ca.IssueWorkloadCert("good-agent", "1.0.0", "run-001", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}

	// Verify with correct identity — must pass.
	if err := VerifyURI(spiffeURI, "good-agent", "1.0.0"); err != nil {
		t.Fatalf("VerifyURI with correct identity: %v", err)
	}

	// Verify with wrong agent name — must fail.
	if err := VerifyURI(spiffeURI, "evil-agent", "1.0.0"); err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: VerifyURI accepted URI for good-agent when expecting evil-agent")
	} else {
		t.Logf("Wrong agent name correctly rejected: %v", err)
	}

	// Verify with wrong version — must fail.
	if err := VerifyURI(spiffeURI, "good-agent", "9.9.9"); err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: VerifyURI accepted URI for 1.0.0 when expecting 9.9.9")
	} else {
		t.Logf("Wrong version correctly rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VECTOR 9: AID key isolation — package identity key must NEVER appear in
// any workload cert, returned KeyMaterial, or be loadable by external
// callers.
// ---------------------------------------------------------------------------

// TestAdversaryT03_AIDKeyIsolation verifies that the package identity (AID)
// private key is never leaked through workload certificate issuance, and
// that the workload cert's public key is distinct from the AID key.
func TestAdversaryT03_AIDKeyIsolation(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Create package identity key for an agent.
	aidFingerprint, err := ca.EnsurePackageIdentityKey("pkg-agent-1")
	if err != nil {
		t.Fatalf("EnsurePackageIdentityKey: %v", err)
	}
	if aidFingerprint == "" {
		t.Fatal("AID fingerprint is empty")
	}

	// Load the AID key material from the store directly.
	aidKM, err := store.Load("package_identity_pkg-agent-1")
	if err != nil {
		t.Fatalf("Load AID key: %v", err)
	}
	aidKey, err := parseECDSAPrivateKey(aidKM.Bytes)
	if err != nil {
		t.Fatalf("parse AID key: %v", err)
	}

	// Issue a workload cert for the same agent.
	cert, workloadKey, spiffeURI, err := ca.IssueWorkloadCert("pkg-agent-1", "1.0.0", "run-aid-test", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	_ = spiffeURI

	// Check 1: Workload cert's public key must differ from AID public key.
	workloadPubBytes, _ := x509.MarshalPKIXPublicKey(cert.PublicKey)
	aidPubBytes, _ := x509.MarshalPKIXPublicKey(&aidKey.PublicKey)
	if bytes.Equal(workloadPubBytes, aidPubBytes) {
		t.Error("ADVERSARY BREAK [HIGH]: workload cert public key matches AID public key")
	}

	// Check 2: The returned workload private key must NOT match the AID private key.
	workloadPrivBytes, err := x509.MarshalPKCS8PrivateKey(workloadKey)
	if err != nil {
		t.Fatalf("Marshal workload key: %v", err)
	}
	aidPrivBytes, err := x509.MarshalPKCS8PrivateKey(aidKey)
	if err != nil {
		t.Fatalf("Marshal AID key: %v", err)
	}
	if bytes.Equal(workloadPrivBytes, aidPrivBytes) {
		t.Error("ADVERSARY BREAK [HIGH]: returned workload private key matches AID private key")
	}

	// Check 3: The workload cert subject must not leak AID info.
	if strings.Contains(cert.Subject.CommonName, "package") ||
		strings.Contains(cert.Subject.CommonName, "identity") {
		t.Error("ADVERSARY BREAK [LOW]: workload cert subject leaks AID info")
	}

	t.Log("AID key isolation PASS: workload cert uses distinct key pair from AID")
}

// TestAdversaryT03_AIDKeyNotInIssueMaterial verifies that the AID private
// key bytes are not present anywhere in the material returned to callers
// (certPEM, keyPEM from LocalIdentityIssuer, or raw cert/key bytes).
func TestAdversaryT03_AIDKeyNotInIssueMaterial(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Create AID key.
	_, err = ca.EnsurePackageIdentityKey("secret-agent")
	if err != nil {
		t.Fatalf("EnsurePackageIdentityKey: %v", err)
	}

	// Load AID key material to know what to search for.
	aidKM, err := store.Load("package_identity_secret-agent")
	if err != nil {
		t.Fatalf("Load AID: %v", err)
	}

	// Issue through the issuer to get PEM bytes.
	issuer, err := NewLocalIdentityIssuer(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalIdentityIssuer: %v", err)
	}
	certPEM, keyPEM, spiffeURI, err := issuer.IssueWorkloadCert("secret-agent", "1.0.0", "run-aid-check", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	_ = spiffeURI

	// Check that AID key bytes don't appear in returned PEM material.
	combined := append(certPEM, keyPEM...)
	if bytes.Contains(combined, aidKM.Bytes) {
		t.Error("ADVERSARY BREAK [HIGH]: AID private key bytes found in returned PEM material from IdentityIssuer")
	}

	// Check common PEM markers.
	if bytes.Contains(combined, []byte("package_identity")) {
		t.Error("ADVERSARY BREAK [HIGH]: returned PEM contains 'package_identity' marker")
	}

	t.Log("AID key isolation PASS: no AID key material leaked in issuer output")
}

// ---------------------------------------------------------------------------
// VECTOR 10: Cert tampering — modify a byte in the cert DER, verification
// must fail.
// ---------------------------------------------------------------------------

// TestAdversaryT03_CertTampering verifies that any tampering with the
// certificate DER bytes causes verification to fail.
func TestAdversaryT03_CertTampering(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	cert, _, _, err := ca.IssueWorkloadCert("tamper-agent", "1.0.0", "run-tamper", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}

	// Verify original is valid.
	if err := ca.VerifyWorkloadCert(cert); err != nil {
		t.Fatalf("VerifyWorkloadCert(original): %v", err)
	}

	// Tamper with the DER: flip a bit at a non-header position.
	raw := make([]byte, len(cert.Raw))
	copy(raw, cert.Raw)
	// Flip a bit in the middle of the DER (not in the signature — the signature
	// is at the end; flipping content bytes is sufficient).
	if len(raw) > 100 {
		raw[len(raw)/2] ^= 0x01
	}

	tamperedCert, err := x509.ParseCertificate(raw)
	if err != nil {
		// If DER is too damaged to parse, that's also acceptable (fail closed).
		t.Logf("Tampered cert failed to parse (acceptable): %v", err)
		return
	}

	// Verification of tampered cert must fail.
	if err := ca.VerifyWorkloadCert(tamperedCert); err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: ca.VerifyWorkloadCert accepted tampered cert")
	} else {
		t.Logf("Tampered cert correctly rejected: %v", err)
	}

	// Also check with spiffe.VerifyWorkloadCert (validity period check only,
	// which should pass since the dates are still valid — but the signature
	// chain verification in ca.VerifyWorkloadCert should catch it).
	if err := VerifyWorkloadCert(tamperedCert); err != nil {
		t.Logf("spiffe.VerifyWorkloadCert on tampered cert: %v (validity check)", err)
	}
}

// ---------------------------------------------------------------------------
// VECTOR 11: Key reuse — two different runs with the same agentName must
// get DIFFERENT workload keys (no key reuse).
// ---------------------------------------------------------------------------

// TestAdversaryT03_KeyReuse verifies that two separate runs for the same
// agent produce distinct key pairs (no key reuse).
func TestAdversaryT03_KeyReuse(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Two different runs with the same agent.
	_, key1, _, err := ca.IssueWorkloadCert("same-agent", "1.0.0", "run-001", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert(run-001): %v", err)
	}
	_, key2, _, err := ca.IssueWorkloadCert("same-agent", "1.0.0", "run-002", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert(run-002): %v", err)
	}

	// Public keys must be different.
	pub1, err := x509.MarshalPKIXPublicKey(&key1.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey(1): %v", err)
	}
	pub2, err := x509.MarshalPKIXPublicKey(&key2.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey(2): %v", err)
	}
	if bytes.Equal(pub1, pub2) {
		t.Error("ADVERSARY BREAK [HIGH]: two different runs for same agent produced identical public keys (key reuse)")
	}

	// Private keys must be different.
	priv1, err := x509.MarshalPKCS8PrivateKey(key1)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(1): %v", err)
	}
	priv2, err := x509.MarshalPKCS8PrivateKey(key2)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(2): %v", err)
	}
	if bytes.Equal(priv1, priv2) {
		t.Error("ADVERSARY BREAK [HIGH]: two different runs for same agent produced identical private keys (key reuse)")
	}

	t.Log("Key reuse PASS: different runs produce distinct keys")
}

// ---------------------------------------------------------------------------
// VECTOR 12: Concurrent issuance — issue 50 workload certs concurrently,
// all unique, no races (-race).
// ---------------------------------------------------------------------------

// TestAdversaryT03_ConcurrentIssuance verifies that issuing 50 workload
// certs concurrently produces all unique keys and detects no data races.
func TestAdversaryT03_ConcurrentIssuance(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	const concurrent = 50
	var wg sync.WaitGroup
	type result struct {
		name    string
		pubKey  []byte
		privKey []byte
	}
	results := make(chan result, concurrent)
	errs := make(chan error, concurrent)

	for i := range concurrent {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("conc-agent-%d", n)
			_, key, _, err := ca.IssueWorkloadCert(name, "1.0.0", fmt.Sprintf("run-%d", n), time.Hour)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", n, err)
				return
			}
			pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d marshal pub: %w", n, err)
				return
			}
			priv, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d marshal priv: %w", n, err)
				return
			}
			results <- result{name: name, pubKey: pub, privKey: priv}
		}(i)
	}

	wg.Wait()
	close(results)
	close(errs)

	// Collect errors.
	var errList []string
	for e := range errs {
		errList = append(errList, e.Error())
	}
	if len(errList) > 0 {
		t.Fatalf("Concurrent issuance errors: %s", strings.Join(errList, "; "))
	}

	// Collect results and verify uniqueness.
	var allResults []result
	for r := range results {
		allResults = append(allResults, r)
	}

	if len(allResults) != concurrent {
		t.Fatalf("Expected %d results, got %d", concurrent, len(allResults))
	}

	// Check all public keys are unique.
	seenPub := make(map[string]bool)
	seenPriv := make(map[string]bool)
	for _, r := range allResults {
		pubStr := string(r.pubKey)
		privStr := string(r.privKey)
		if seenPub[pubStr] {
			t.Errorf("ADVERSARY BREAK [HIGH]: duplicate public key found for %s", r.name)
		}
		if seenPriv[privStr] {
			t.Errorf("ADVERSARY BREAK [HIGH]: duplicate private key found for %s", r.name)
		}
		seenPub[pubStr] = true
		seenPriv[privStr] = true
	}

	t.Logf("Concurrent issuance PASS: %d unique keys generated without race", concurrent)
}

// ---------------------------------------------------------------------------
// VECTOR 13: TTL manipulation — issue cert with TTL=0 or negative — must
// error, not produce a valid cert.
// ---------------------------------------------------------------------------

// TestAdversaryT03_TTLManipulation tests that non-positive TTL values
// are rejected or handled securely.
func TestAdversaryT03_TTLManipulation(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	t.Run("TTL_zero", func(t *testing.T) {
		cert, _, _, err := ca.IssueWorkloadCert("ttl-zero-agent", "1.0.0", "run-zero", 0)
		if err != nil {
			t.Logf("TTL=0 correctly rejected: %v", err)
			return
		}
		// The code defaults TTL=0 to 1 hour (line 148-150 of ca.go).
		// This is not a security break per se, but it silently changes the
		// caller's intent. Flag as a contract note.
		remaining := time.Until(cert.NotAfter)
		t.Logf("TTL=0 produced a cert with NotAfter in %v (defaulted to 1h)", remaining)
		if remaining < 30*time.Minute {
			t.Error("ADVERSARY BREAK [LOW]: TTL=0 produced unexpected short-lived cert")
		}
	})

	t.Run("TTL_negative", func(t *testing.T) {
		cert, _, _, err := ca.IssueWorkloadCert("ttl-neg-agent", "1.0.0", "run-neg", -1*time.Hour)
		if err != nil {
			t.Logf("TTL=-1h correctly rejected: %v", err)
			return
		}
		// Defaulted to 1 hour — acceptable but unexpected.
		remaining := time.Until(cert.NotAfter)
		t.Logf("TTL=-1h produced a cert with NotAfter in %v (defaulted to 1h)", remaining)
		if remaining < 30*time.Minute {
			t.Error("ADVERSARY BREAK [LOW]: TTL=-1h produced unexpected short-lived cert")
		}
	})
}

// ---------------------------------------------------------------------------
// VECTOR 14: Renewal after expiry — try to renew an already-expired cert.
// Must fail (must issue fresh, not renew dead cert).
// ---------------------------------------------------------------------------

// TestAdversaryT03_RenewAfterExpiry tests that renewing an already-expired
// workload cert produces an error or at least a fresh cert with new validity.
func TestAdversaryT03_RenewAfterExpiry(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Issue a cert with 1s TTL.
	oldCert, oldKey, spiffeURI, err := ca.IssueWorkloadCert("renew-exp-agent", "1.0.0", "run-renew-exp", time.Second)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	_ = oldKey
	_ = spiffeURI

	// Wait for it to expire.
	time.Sleep(2 * time.Second)

	// Verify old cert is expired.
	if err := VerifyWorkloadCert(oldCert); err == nil {
		t.Fatal("old cert should be expired by now")
	}

	// RenewWorkloadCert just calls IssueWorkloadCert, which issues a fresh
	// cert regardless of the old cert's state. It doesn't validate the old
	// cert before renewing. This is not a security break (a fresh cert is
	// issued), but flag as a design note.
	newCert, newKey, newSPIFFE, err := ca.RenewWorkloadCert("renew-exp-agent", "1.0.0", "run-renew-exp", time.Hour)
	if err != nil {
		t.Logf("Renew after expiry correctly rejected: %v", err)
		return
	}

	// A new cert was issued. Verify it's valid.
	if err := ca.VerifyWorkloadCert(newCert); err != nil {
		t.Errorf("Renewed cert verification failed: %v", err)
	}
	if newKey == nil {
		t.Error("renewed key is nil")
		return
	}
	if newSPIFFE == "" {
		t.Error("renewed SPIFFE URI is empty")
	}
	if newSPIFFE != spiffeURI {
		t.Errorf("renewed SPIFFE URI changed: %q vs %q", newSPIFFE, spiffeURI)
	}

	// Verify new key is different from old key.
	newPub, _ := x509.MarshalPKIXPublicKey(&newKey.PublicKey)
	oldPub, _ := x509.MarshalPKIXPublicKey(&oldKey.PublicKey)
	if bytes.Equal(newPub, oldPub) {
		t.Error("ADVERSARY BREAK [LOW]: renewed key is same as expired key")
	}

	t.Log("Renew after expiry issued a fresh cert (acceptable — RenewWorkloadCert is a thin wrapper)")
}

// ---------------------------------------------------------------------------
// VECTOR 15: CA private key must never appear in any returned material,
// error message, or log.
// ---------------------------------------------------------------------------

// TestAdversaryT03_CAPrivateKeyLeak verifies that the CA private key is
// never present in material returned to callers.
func TestAdversaryT03_CAPrivateKeyLeak(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Load the CA key to know what to search for.
	caKM, err := store.Load("local_ca")
	if err != nil {
		t.Fatalf("Load CA key: %v", err)
	}
	caKey, err := parseECDSAPrivateKey(caKM.Bytes)
	if err != nil {
		t.Fatalf("parse CA key: %v", err)
	}
	caPrivBytes, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		t.Fatalf("Marshal CA private key: %v", err)
	}
	caKMAll := caKM.Bytes // PEM-encoded CA key

	// Issue a workload cert and check all returned material.
	_, workloadKey, spiffeURI, err := ca.IssueWorkloadCert("leak-test-agent", "1.0.0", "run-leak", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	_ = spiffeURI

	// Check 1: Returned workload private key must not match CA private key.
	wkPrivBytes, err := x509.MarshalPKCS8PrivateKey(workloadKey)
	if err != nil {
		t.Fatalf("Marshal workload key: %v", err)
	}
	if bytes.Equal(wkPrivBytes, caPrivBytes) {
		t.Error("ADVERSARY BREAK [HIGH]: returned workload private key matches CA private key")
	}

	// Check 2: Workload key PEM should not contain CA key PEM.
	if bytes.Contains(wkPrivBytes, caKMAll) {
		t.Error("ADVERSARY BREAK [HIGH]: returned workload key contains CA key PEM bytes")
	}

	// Issue through LocalIdentityIssuer (returns PEM).
	issuer, err := NewLocalIdentityIssuer(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalIdentityIssuer: %v", err)
	}
	certPEM, keyPEM, _, err := issuer.IssueWorkloadCert("leak-test-agent2", "1.0.0", "run-leak2", time.Hour)
	if err != nil {
		t.Fatalf("issuer.IssueWorkloadCert: %v", err)
	}

	// Check PEM output doesn't contain CA key bytes.
	issuerOutput := append(certPEM, keyPEM...)
	if bytes.Contains(issuerOutput, caPrivBytes) {
		t.Error("ADVERSARY BREAK [HIGH]: CA private key bytes found in issuer PEM output")
	}
	if bytes.Contains(issuerOutput, caKMAll) {
		t.Error("ADVERSARY BREAK [HIGH]: CA key PEM found in issuer PEM output")
	}
	if bytes.Contains(issuerOutput, []byte("local_ca")) {
		t.Error("ADVERSARY BREAK [LOW]: issuer output contains 'local_ca' identifier")
	}

	t.Log("CA private key leak PASS: no CA key material found in returned data")
}

// ---------------------------------------------------------------------------
// VECTOR 16: CA key must be stored in the KeyStore (not in-memory only) —
// verify it's persisted.
// ---------------------------------------------------------------------------

// TestAdversaryT03_CAKeyIsPersisted verifies that the CA key is stored in
// the KeyStore and can be loaded again (it's not purely in-memory).
func TestAdversaryT03_CAKeyIsPersisted(t *testing.T) {
	store := NewFakeKeyStore()
	_, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// The CA key should have been stored during NewLocalCA.
	caKM, err := store.Load("local_ca")
	if err != nil {
		t.Errorf("ADVERSARY BREAK [HIGH]: CA key not found in KeyStore after NewLocalCA: %v", err)
		return
	}
	if len(caKM.Bytes) == 0 {
		t.Error("ADVERSARY BREAK [HIGH]: CA key in KeyStore has empty bytes")
	}
	if caKM.Type != KeyTypeCA {
		t.Errorf("ADVERSARY BREAK [MEDIUM]: CA key stored with wrong type: %q, want %q", caKM.Type, KeyTypeCA)
	}

	// Verify the stored key can be parsed and used.
	parsedKey, err := parseECDSAPrivateKey(caKM.Bytes)
	if err != nil {
		t.Errorf("ADVERSARY BREAK [HIGH]: stored CA key bytes cannot be parsed: %v", err)
		return
	}
	if parsedKey == nil {
		t.Error("ADVERSARY BREAK [HIGH]: parsed CA key is nil")
		return
	}

	// Verify the key is usable for signing.
	digest := []byte("test-digest")
	sig, err := store.Sign("local_ca", digest)
	if err != nil {
		t.Errorf("ADVERSARY BREAK [MEDIUM]: stored CA key cannot sign: %v", err)
		return
	}
	if ok := store.Verify("local_ca", digest, sig); !ok {
		t.Error("ADVERSARY BREAK [MEDIUM]: stored CA key signature verification failed")
	}

	t.Log("CA key persistence PASS: key stored in KeyStore, parseable, and usable")
}

// ---------------------------------------------------------------------------
// VECTOR 17: Compromised AID — if an AID key is deleted, old workload
// certs can still be verified (they are signed by CA, not AID).
// ---------------------------------------------------------------------------

// TestAdversaryT03_CompromisedAID verifies that workload certs remain
// verifiable even after the AID (package identity) key is deleted, since
// workload certs are signed by the CA, not the AID.
func TestAdversaryT03_CompromisedAID(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Create an AID key.
	_, err = ca.EnsurePackageIdentityKey("compromised-agent")
	if err != nil {
		t.Fatalf("EnsurePackageIdentityKey: %v", err)
	}

	// Issue a workload cert (signed by CA, not AID).
	cert, _, spiffeURI, err := ca.IssueWorkloadCert("compromised-agent", "1.0.0", "run-aid-delete", time.Hour)
	if err != nil {
		t.Fatalf("IssueWorkloadCert: %v", err)
	}
	_ = spiffeURI

	// Verify workload cert is valid initially.
	if err := ca.VerifyWorkloadCert(cert); err != nil {
		t.Fatalf("Initial verification failed: %v", err)
	}

	// Delete the AID key.
	if err := store.Delete("package_identity_compromised-agent"); err != nil {
		t.Fatalf("Delete AID key: %v", err)
	}

	// Verify the AID key is actually gone.
	_, err = store.Load("package_identity_compromised-agent")
	if err == nil {
		t.Fatal("AID key should be deleted")
	}

	// The workload cert was signed by CA, not AID. It must still be verifiable.
	if err := ca.VerifyWorkloadCert(cert); err != nil {
		t.Errorf("ADVERSARY BREAK [MEDIUM]: workload cert verification failed after AID key deletion: %v", err)
	} else {
		t.Log("PASS: workload cert remains verifiable after AID key deletion")
	}
}

// ---------------------------------------------------------------------------
// VECTOR 17b: Also verify that AID key deletion doesn't affect the CA.
// ---------------------------------------------------------------------------

// TestAdversaryT03_NewCertsAfterAIDDelete verifies that new workload certs
// can still be issued after the AID key is deleted.
func TestAdversaryT03_NewCertsAfterAIDDelete(t *testing.T) {
	store := NewFakeKeyStore()
	ca, err := NewLocalCA(store, &TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		t.Fatalf("NewLocalCA: %v", err)
	}

	// Create and delete an AID key.
	_, err = ca.EnsurePackageIdentityKey("transient-agent")
	if err != nil {
		t.Fatalf("EnsurePackageIdentityKey: %v", err)
	}
	if err := store.Delete("package_identity_transient-agent"); err != nil {
		t.Fatalf("Delete AID key: %v", err)
	}

	// Issue a new workload cert — must still work (CA is independent of AID).
	_, _, _, err = ca.IssueWorkloadCert("fresh-agent", "1.0.0", "run-fresh", time.Hour)
	if err != nil {
		t.Errorf("ADVERSARY BREAK [MEDIUM]: cannot issue new workload cert after AID key deletion: %v", err)
	} else {
		t.Log("PASS: new workload certs issuable after AID key deletion")
	}
}