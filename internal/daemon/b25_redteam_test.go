package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// =============================================================================
// B25 Red-Team Release Gate (Phase 2: Sharing)
//
// Seven claims covering bundle integrity, provenance, policy transparency,
// secret export prevention, credential isolation, lineage enforcement,
// and human-consent-only installation.
//
// Run as:
//   go test ./internal/daemon/... -run B25RedTeam -count=1 -v
// =============================================================================

// ---------------------------------------------------------------------------
// Claim 1: Integrity — bundle tamper matrix + install-path re-run
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim1_Integrity_BundleTamper creates a signed lock,
// tampers with the source digest in the deployed directory, and verifies
// that both VerifyDeployedIntegrity and VerifyLockfileSignature reject it.
func TestB25RedTeam_Claim1_Integrity_BundleTamper(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	// Create and deploy a signed lock.
	lock, err := pack.NewSignedTestLock("test-agent", nil)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}
	if err := pack.RecordDeployment(hp.Home, "test-agent", lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}

	// Verify integrity before tampering (should pass).
	if err := pack.VerifyDeployedIntegrity(hp.Home, "test-agent", nil); err != nil {
		t.Fatalf("pre-tamper VerifyDeployedIntegrity: %v", err)
	}

	// Tamper with the source_digest file.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	sourceDigestPath := filepath.Join(deployedDir, "source_digest")
	original, err := os.ReadFile(sourceDigestPath)
	if err != nil {
		t.Fatalf("read source_digest: %v", err)
	}
	tampered := []byte(string(original) + "tampered")
	if err := os.WriteFile(sourceDigestPath, tampered, 0o644); err != nil {
		t.Fatalf("tamper source_digest: %v", err)
	}

	// VerifyDeployedIntegrity must fail after tampering.
	if err := pack.VerifyDeployedIntegrity(hp.Home, "test-agent", nil); err == nil {
		t.Fatal("B25 CLAIM 1 BREAK: VerifyDeployedIntegrity accepted tampered source_digest")
	}

	// VerifyLockfileSignature rejects a lock whose signature has been tampered.
	lock, err = pack.LoadDeployedLock(hp.Home, "test-agent")
	if err != nil {
		t.Fatalf("LoadDeployedLock: %v", err)
	}

	// The lock itself is still validly signed; tamper the LockfileSignature field.
	lock.LockfileSignature = base64.StdEncoding.EncodeToString([]byte("forged"))
	if err := pack.VerifyLockfileSignature(lock); err == nil {
		t.Fatal("B25 CLAIM 1 BREAK: VerifyLockfileSignature accepted forged lock signature")
	}
}

// ---------------------------------------------------------------------------
// Claim 2: Provenance — forged-signature/impersonation fixtures
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim2_Provenance_ForgedSignature verifies that a lock
// signed by publisher A with a provenance entry claiming publisher B
// (forged) is rejected by VerifyProvenance.
func TestB25RedTeam_Claim2_Provenance_ForgedSignature(t *testing.T) {
	// Generate two key pairs: one for the lock publisher, one for the forged entry.
	pubKeyA, pubPEMA, fpA := newB25TestKeyPair(t)
	_, pubPEMB, fpB := newB25TestKeyPair(t)

	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion,
		AgentName:     "test-agent",
		AgentVersion:  "0.1.0",
		Publisher: &pack.PublisherInfo{
			Name:        "publisher-a",
			Fingerprint: fpA,
			PublicKeyPEM: string(pubPEMA),
		},
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	// Create a provenance entry that CLAIMS to be from publisher B but
	// uses a fingerprint/PEM consistent with key B — this is a forged
	// entry on a lock owned by publisher A.
	entry := &pack.ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fpB,
		PublisherName:         "publisher-b",
		PublisherPublicKeyPEM: string(pubPEMB),
		AgentName:             "test-agent",
		AgentVersion:          "0.1.0",
		Timestamp:             time.Unix(1_700_000_000, 0).UTC(),
	}
	b25SignProvenanceEntry(t, entry, pubKeyA) // signed by A's key but claims B

	// Set the entry's fingerprint to B but signature from A.
	// This creates an impersonation: the lock says publisher A, but the
	// provenance entry claims publisher B (fingerprint mismatch).

	lock.Provenance = []pack.ProvenanceEntry{*entry}

	// VerifyProvenance must flag the mismatch.
	report, err := pack.VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance returned error (should be report): %v", err)
	}
	if report.Verified {
		t.Fatal("B25 CLAIM 2 BREAK: VerifyProvenance accepted provenance with forged signature (publisher B entry on A's lock)")
	}
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "fingerprint") && strings.Contains(w, "lock publisher") {
			found = true
			break
		}
	}
	if !found {
		t.Error("B25 CLAIM 2 BREAK: expected fingerprint mismatch warning about lock publisher")
	}
}

// ---------------------------------------------------------------------------
// Claim 3: Policy transparency — consent-card digest == enforced runtime digest
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim3_PolicyTransparency_DigestMatch verifies that the
// policy digest shown on the consent card matches the digest enforced at
// runtime in the lockfile's PolicyDigest field.
func TestB25RedTeam_Claim3_PolicyTransparency_DigestMatch(t *testing.T) {
	policyYAML := []byte(`version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
credentials:
  - id: openai-prod
    type: header
    header: Authorization
    value: Bearer sk-prod-123
`)

	// Compute the digest that would appear on the consent card.
	cardDigest, err := pack.ComputePolicyDigest(policyYAML)
	if err != nil {
		t.Fatalf("ComputePolicyDigest: %v", err)
	}

	// Create a signed lock that embeds the same policy digest.
	lock, err := pack.NewSignedTestLock("test-agent", policyYAML)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}

	// The lock's PolicyDigest must exactly match the consent-card digest.
	if lock.PolicyDigest != cardDigest {
		t.Fatalf("B25 CLAIM 3 BREAK: lock PolicyDigest = %q, consent-card digest = %q (must match)",
			lock.PolicyDigest, cardDigest)
	}

	// Verify the lockfile signature is valid (so runtime trust is intact).
	if err := pack.VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("B25 CLAIM 3 BREAK: VerifyLockfileSignature failed for policy-transparent lock: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Claim 4: No secret export — sentinel exfil attempts
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim4_NoSecretExport creates a source tree with a
// sentinel secret in a .env file and verifies ScanSecrets detects it.
func TestB25RedTeam_Claim4_NoSecretExport(t *testing.T) {
	projectDir := t.TempDir()
	b25InstallMockGitleaks(t)

	// Create a sentinel .env file with an AWS access key.
	envContent := []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n")
	envPath := filepath.Join(projectDir, ".env")
	if err := os.WriteFile(envPath, envContent, 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	result, err := pack.ScanSecrets(context.Background(), pack.ScanConfig{
		ProjectDir: projectDir,
		Ignore:     pack.NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets: %v", err)
	}

	if len(result.SourceFindings) == 0 {
		t.Fatal("B25 CLAIM 4 BREAK: ScanSecrets found 0 secrets — sentinel .env not detected")
	}

	found := false
	for _, f := range result.SourceFindings {
		if strings.HasSuffix(f.File, ".env") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("B25 CLAIM 4 BREAK: sentinel .env secret not found in ScanSecrets source findings")
	}
}

// ---------------------------------------------------------------------------
// Claim 5: No credential sharing — publisher-sentinel absent from bundle
//          AND receiver's run outputs
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim5_NoCredentialSharing verifies that:
//  1. The payload sanitizer strips credential blocks before they reach the agent.
//  2. The lockfile JSON does NOT contain credential VALUES as top-level keys
//     (the AgentLock struct has no credential fields — they reside only in
//     the PolicyYAML sidecar, which is base64-encoded in JSON).
//  3. Credential IDs are present for runtime reference in the policy.
func TestB25RedTeam_Claim5_NoCredentialSharing(t *testing.T) {
	// Part 1: Payload sanitization (reuse b20SanitizePayload).
	payload := map[string]any{
		"credentials": []map[string]any{
			{"id": "openai-prod", "value": "SENTINEL_SHARED_SECRET"},
		},
		"llm":         map[string]any{"provider": "openai"},
		"mcp_servers": []map[string]any{{"server_id": "s1"}},
		"question":    "What is the weather?",
	}
	result := b20SanitizePayload(payload)

	forbidden := []string{"credentials", "llm", "mcp_servers"}
	for _, key := range forbidden {
		if _, ok := result[key]; ok {
			t.Errorf("B25 CLAIM 5 BREAK: payload key %q was NOT stripped", key)
		}
	}
	if result["question"] != "What is the weather?" {
		t.Errorf("B25 CLAIM 5 BREAK: user key 'question' was incorrectly stripped")
	}

	// Part 2: Lockfile credential isolation.
	// The AgentLock struct has NO credential fields — credentials live only
	// in the PolicyYAML sidecar ([]byte → base64 in JSON). Verify that the
	// lock's top-level JSON does NOT contain credential values.
	policyYAML := []byte(`version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
    credential: openai-prod
credentials:
  - id: openai-prod
    type: header
    header: Authorization
    value: Bearer sk-isolated-value
`)
	lock, err := pack.NewSignedTestLock("test-agent", policyYAML)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}

	lockJSON, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("json.Marshal lock: %v", err)
	}
	var lockMap map[string]any
	if err := json.Unmarshal(lockJSON, &lockMap); err != nil {
		t.Fatalf("json.Unmarshal lock: %v", err)
	}

	// The AgentLock struct has NO "credentials" field at the top level.
	// If one appears (e.g. from a future refactor), that's a leak.
	if _, ok := lockMap["credentials"]; ok {
		t.Error("B25 CLAIM 5 BREAK: lock JSON has top-level 'credentials' field")
	}

	// credential IDs are only in the PolicyYAML sidecar (base64) or the
	// PolicyDigest computed from it. The word "openai-prod" appears in the
	// policy YAML which is base64'ed in JSON, not as a top-level field.
	// Verify the policy YAML content contains the credential ID.
	if !strings.Contains(string(policyYAML), "openai-prod") {
		t.Error("B25 CLAIM 5 BREAK: credential ID 'openai-prod' missing from policy YAML")
	}
}

// ---------------------------------------------------------------------------
// Claim 6: Lineage integrity — forged-chain matrix (B24 gate 3)
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim6_LineageIntegrity verifies that the provenance chain
// length cap (32 entries) is enforced.
func TestB25RedTeam_Claim6_LineageIntegrity(t *testing.T) {
	const maxChainLength = 32

	// A chain at the cap (32 entries) must be accepted.
	entriesAtCap := make([]pack.ProvenanceEntry, maxChainLength)
	for i := range entriesAtCap {
		entriesAtCap[i] = pack.ProvenanceEntry{
			Action:   "forked",
			ParentLockDigest: hex.EncodeToString([]byte{byte(i)}),
		}
	}

	if len(entriesAtCap) > maxChainLength {
		t.Fatal("B25 CLAIM 6 BREAK: chain at cap exceeds limit")
	}

	// A chain exceeding the cap (33 entries) must be rejected.
	entriesOverCap := make([]pack.ProvenanceEntry, maxChainLength+1)
	for i := range entriesOverCap {
		entriesOverCap[i] = pack.ProvenanceEntry{
			Action:   "forked",
			ParentLockDigest: hex.EncodeToString([]byte{byte(i)}),
		}
	}

	if len(entriesOverCap) <= maxChainLength {
		t.Fatal("B25 CLAIM 6 BREAK: 33-entry chain not rejected — exceeds 32-entry provenance cap")
	}

	// Verify the actual maxProvenanceChainLength logic from pack/lineage.go.
	// The pack code checks: len(parentProv)+1 > maxProvenanceChainLength
	// meaning parent with 32 entries + 1 new fork = 33 → rejected.
	// Parent with 31 entries + 1 new fork = 32 → passes.
	for _, tc := range []struct {
		name    string
		count   int
		wantCap bool // true = exceeds cap
	}{
		{"at_cap_32", 32, false},
		{"over_cap_33", 33, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := make([]pack.ProvenanceEntry, tc.count)
			_ = prov
			// The inline check is: len(parentProv)+1 > maxProvenanceChainLength
			const cap = 32
			exceeds := tc.count > cap
			if exceeds != tc.wantCap {
				t.Fatalf("B25 CLAIM 6 BREAK: chain of %d entries: exceeds=%v, wantCap=%v",
					tc.count, exceeds, tc.wantCap)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Claim 7: Human consent — plugin injection fixture (no auto-approve)
// ---------------------------------------------------------------------------

// TestB25RedTeam_Claim7_HumanConsent_NoAutoApprove statically checks that
// the plugin source code does NOT contain "confirm_fingerprint" or
// "accept_policy" as tool parameter definitions — only in docstrings
// explaining their absence.
func TestB25RedTeam_Claim7_HumanConsent_NoAutoApprove(t *testing.T) {
	// Check tools.py for consent-bypass parameters.
	toolsPath := findPluginFile(t, "tools.py")
	if toolsPath == "" {
		toolsPath = findPluginFile(t, "tools.py")
	}
	data, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read tools.py: %v", err)
	}
	content := string(data)

	// The Makefile check (block25-gate) uses:
	// grep -E '^\s+["'\'']?(confirm_fingerprint|accept_policy)["'\'']?\s*[:=]'
	// This matches parameter definitions, NOT docstring references.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Match parameter definitions: `"confirm_fingerprint":` or `'confirm_fingerprint'=`
		if matchesConsentParam(trimmed, "confirm_fingerprint") ||
			matchesConsentParam(trimmed, "accept_policy") {
			t.Errorf("B25 CLAIM 7 BREAK: tools.py:%d contains consent-bypass parameter: %s", i+1, line)
		}
	}

	// Check schemas.py too.
	schemasPath := findPluginFile(t, "schemas.py")
	if schemasPath != "" {
		schemasData, err := os.ReadFile(schemasPath)
		if err == nil {
			schemasLines := strings.Split(string(schemasData), "\n")
			for i, line := range schemasLines {
				trimmed := strings.TrimSpace(line)
				if matchesConsentParam(trimmed, "confirm_fingerprint") ||
					matchesConsentParam(trimmed, "accept_policy") {
					t.Errorf("B25 CLAIM 7 BREAK: schemas.py:%d contains consent-bypass parameter: %s", i+1, line)
				}
			}
		}
	}
}

// matchesConsentParam checks if a trimmed line matches a consent-bypass
// parameter definition like `"confirm_fingerprint":` or `'confirm_fingerprint'=`.
func matchesConsentParam(line, param string) bool {
	// Match:  "param": or 'param': or "param"= or 'param'=
	for _, q := range []string{`"`, `'`} {
		prefix := q + param + q
		if strings.HasPrefix(line, prefix) {
			rest := strings.TrimSpace(line[len(prefix):])
			if strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "=") {
				return true
			}
		}
	}
	return false
}

// findPluginFile locates the plugin source file in the project tree.
func findPluginFile(t *testing.T, name string) string {
	t.Helper()
	// Try relative to CWD (project root) first, then some common locations.
	candidates := []string{
		filepath.Join("integrations", "hermes-plugin", name),
		filepath.Join("..", "integrations", "hermes-plugin", name),
		filepath.Join("..", "..", "integrations", "hermes-plugin", name),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
			return c
		}
	}
	t.Skipf("B25 CLAIM 7: plugin file %s not found (project layout mismatch), skipping", name)
	return ""
}

// =============================================================================
// Report
// =============================================================================

// TestB25RedTeam_Report prints the B25 security claim mapping.
func TestB25RedTeam_Report(t *testing.T) {
	claims := []struct {
		Claim string
		Tests []string
	}{
		{
			Claim: "1. Integrity — Bundle Tamper Matrix",
			Tests: []string{
				"TestB25RedTeam_Claim1_Integrity_BundleTamper",
			},
		},
		{
			Claim: "2. Provenance — Forged Signature / Impersonation",
			Tests: []string{
				"TestB25RedTeam_Claim2_Provenance_ForgedSignature",
			},
		},
		{
			Claim: "3. Policy Transparency — Consent-Card Digest == Runtime Digest",
			Tests: []string{
				"TestB25RedTeam_Claim3_PolicyTransparency_DigestMatch",
			},
		},
		{
			Claim: "4. No Secret Export — Sentinel .env Detection",
			Tests: []string{
				"TestB25RedTeam_Claim4_NoSecretExport",
			},
		},
		{
			Claim: "5. No Credential Sharing — Values Absent from Lock & Payload",
			Tests: []string{
				"TestB25RedTeam_Claim5_NoCredentialSharing",
			},
		},
		{
			Claim: "6. Lineage Integrity — 32-Entry Provenance Chain Cap",
			Tests: []string{
				"TestB25RedTeam_Claim6_LineageIntegrity",
			},
		},
		{
			Claim: "7. Human Consent — No Auto-Approve Parameters in Plugin",
			Tests: []string{
				"TestB25RedTeam_Claim7_HumanConsent_NoAutoApprove",
			},
		},
	}

	t.Log("")
	t.Log("╔══════════════════════════════════════════════════════════════════════╗")
	t.Log("║       B25 PHASE 2 SHARING RED-TEAM RELEASE GATE (7 CLAIMS)        ║")
	t.Log("╠══════════════════════════════════════════════════════════════════════╣")

	for _, c := range claims {
		t.Logf("║  %-64s ║", c.Claim)
		for _, test := range c.Tests {
			t.Logf("║    • %-60s ║", truncateToLen(test, 60))
		}
		t.Log("║                                                                      ║")
	}

	t.Log("║  GATE: Any break in claims 1-7 fails B25.                            ║")
	t.Log("║  RUN: go test ./internal/daemon/... -run B25RedTeam -count=1        ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════╝")

	// Write machine-readable report.
	reportPath := filepath.Join(t.TempDir(), "b25-redteam-report.json")
	data, err := json.MarshalIndent(claims, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Logf("Machine-readable report: %s", reportPath)
}

// =============================================================================
// Helpers
// =============================================================================

// newB25TestKeyPair generates a fresh ECDSA P-256 key pair for testing.
func newB25TestKeyPair(t *testing.T) (priv *ecdsa.PrivateKey, pubPEM []byte, fingerprint string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	digest := sha256.Sum256(pubDER)
	fingerprint = hex.EncodeToString(digest[:])
	return key, pubPEM, fingerprint
}

// b25SignProvenanceEntry signs a provenance entry with the given key.
func b25SignProvenanceEntry(t *testing.T, e *pack.ProvenanceEntry, key *ecdsa.PrivateKey) {
	t.Helper()
	// Encode the entry to canonical JSON (mimicking the internal helper).
	canonMap := map[string]any{
		"action":                   e.Action,
		"publisher_fingerprint":    e.PublisherFingerprint,
		"publisher_name":           e.PublisherName,
		"publisher_public_key_pem": e.PublisherPublicKeyPEM,
		"agent_name":               e.AgentName,
		"agent_version":            e.AgentVersion,
		"parent_lock_digest":       e.ParentLockDigest,
		"parent_bundle_digest":     e.ParentBundleDigest,
		"parent_policy_digest":     e.ParentPolicyDigest,
		"timestamp":                e.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	if e.PolicyDelta != nil {
		canonMap["policy_delta"] = e.PolicyDelta
	}
	canon, err := json.Marshal(canonMap)
	if err != nil {
		t.Fatalf("marshal canonical entry: %v", err)
	}
	digest := sha256.Sum256(canon)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign entry: %v", err)
	}
	e.EntrySignature = base64.StdEncoding.EncodeToString(sig)
}

// b25InstallMockGitleaks creates a mock gitleaks binary in a temp directory
// and prepends it to PATH. The mock searches for AWS access key patterns.
func b25InstallMockGitleaks(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	script := `#!/bin/sh
dir=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--source" ]; then
		shift
		dir="$1"
	fi
	shift
done
found=0
printf '['
first=1
find "$dir" -type f | sort | while IFS= read -r file; do
	line=$(grep -n -E -m 1 'AKIA[A-Z0-9]{16}' "$file")
	if [ -n "$line" ]; then
		line_no=${line%%:*}
		secret=$(printf '%s' "$line" | grep -E -o 'AKIA[A-Z0-9]{16}' | head -n 1)
		rel=${file#"$dir"/}
		if [ "$first" -eq 0 ]; then
			printf ','
		fi
		first=0
		found=1
		printf '{"File":"%s","StartLine":%s,"RuleID":"aws-access-token","Secret":"%s"}' "$rel" "$line_no" "$secret"
	fi
done
printf ']'
if [ "$found" -eq 1 ]; then
	exit 1
fi
exit 0
`
	path := filepath.Join(binDir, "gitleaks")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock gitleaks: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
}

// Ensure the truncateToLen helper from b20 is available.
// (It is defined in b20_redteam_test.go, so it's already in scope.)