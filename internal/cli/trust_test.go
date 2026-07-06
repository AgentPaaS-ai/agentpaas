package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

// generateTestKeyPEM returns a PEM-encoded P-256 public key and its hex fingerprint.
func generateTestKeyPEM(t *testing.T) (string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	sum := sha256.Sum256(der)
	fp := fmt.Sprintf("%x", sum)
	return string(pemBytes), fp
}

// prepareTrustHome creates a temp directory with the trust/ subdirectory set up
// and returns the home directory path and trust store path.
func prepareTrustHome(t *testing.T) (string, string) {
	t.Helper()
	homeDir := t.TempDir()
	storePath := trust.DefaultStorePath(homeDir)
	return homeDir, storePath
}

func TestTrustAdd(t *testing.T) {
	homeDir, storePath := prepareTrustHome(t)
	pemData, fp := generateTestKeyPEM(t)

	// Write PEM to temp file.
	keyFile := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyFile, []byte(pemData), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	resetAgentCmd()
	cmd := AgentCmd()
	outBuf := new(strings.Builder)
	errBuf := new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"trust", "add", fp, "--key", keyFile, "--alias", "test-pub", "--home", homeDir})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("trust add: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "Trusted publisher") {
		t.Errorf("unexpected output: %s", out)
	}

	// Verify store file was created.
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("store file not created: %v", err)
	}
}

func TestTrustAddFingerprintWithSpaces(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	pemData, fp := generateTestKeyPEM(t)

	// Use display form fingerprint.
	displayFP := trust.DisplayFingerprint(fp)

	keyFile := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyFile, []byte(pemData), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	resetAgentCmd()
	cmd := AgentCmd()
	outBuf := new(strings.Builder)
	errBuf := new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"trust", "add", displayFP, "--key", keyFile, "--home", homeDir})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("trust add with display fp: %v\nstderr: %s", err, errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "Trusted publisher") {
		t.Errorf("unexpected output: %s", outBuf.String())
	}
}

func TestTrustAddMissingKeyFlag(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	_, fp := generateTestKeyPEM(t)

	resetAgentCmd()
	cmd := AgentCmd()
	errBuf := new(strings.Builder)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"trust", "add", fp, "--home", homeDir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --key flag")
	}
}

func TestTrustList(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	pemData1, fp1 := generateTestKeyPEM(t)
	pemData2, fp2 := generateTestKeyPEM(t)

	// Add two publishers.
	keyFile1 := filepath.Join(t.TempDir(), "key1.pem")
	keyFile2 := filepath.Join(t.TempDir(), "key2.pem")
	_ = os.WriteFile(keyFile1, []byte(pemData1), 0o600)
	_ = os.WriteFile(keyFile2, []byte(pemData2), 0o600)

	for i, kf := range []string{keyFile1, keyFile2} {
		fp := []string{fp1, fp2}[i]
		alias := []string{"pub-alpha", "pub-beta"}[i]
		resetAgentCmd()
		cmd := AgentCmd()
		cmd.SetArgs([]string{"trust", "add", fp, "--key", kf, "--alias", alias, "--home", homeDir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("trust add %d: %v", i, err)
		}
	}

	// List.
	resetAgentCmd()
	cmd := AgentCmd()
	outBuf := new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "list", "--home", homeDir})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("trust list: %v", err)
	}
	out := outBuf.String()
	if !strings.Contains(out, trust.DisplayFingerprint(fp1)) {
		t.Errorf("list missing fp1: %s", out)
	}
	if !strings.Contains(out, trust.DisplayFingerprint(fp2)) {
		t.Errorf("list missing fp2: %s", out)
	}

	// JSON list.
	resetAgentCmd()
	cmd = AgentCmd()
	outBuf = new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "list", "--json", "--home", homeDir})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("trust list --json: %v", err)
	}
	var entries []trustListEntry
	if err := json.Unmarshal([]byte(outBuf.String()), &entries); err != nil {
		t.Fatalf("unmarshal list JSON: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestTrustShow(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	pemData, fp := generateTestKeyPEM(t)

	keyFile := filepath.Join(t.TempDir(), "key.pem")
	_ = os.WriteFile(keyFile, []byte(pemData), 0o600)

	// Add publisher.
	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"trust", "add", fp, "--key", keyFile, "--alias", "show-pub", "--home", homeDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trust add: %v", err)
	}

	// Show by fingerprint.
	resetAgentCmd()
	cmd = AgentCmd()
	outBuf := new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "show", fp, "--home", homeDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trust show: %v", err)
	}
	out := outBuf.String()
	if !strings.Contains(out, "show-pub") {
		t.Errorf("show missing alias: %s", out)
	}
	if !strings.Contains(out, "PUBLIC KEY") {
		t.Errorf("show missing key PEM: %s", out)
	}

	// Show by alias.
	resetAgentCmd()
	cmd = AgentCmd()
	outBuf = new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "show", "show-pub", "--home", homeDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trust show by alias: %v", err)
	}
	out = outBuf.String()
	if !strings.Contains(out, trust.DisplayFingerprint(fp)) {
		t.Errorf("show by alias missing fingerprint: %s", out)
	}

	// Show JSON.
	resetAgentCmd()
	cmd = AgentCmd()
	outBuf = new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "show", fp, "--json", "--home", homeDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trust show --json: %v", err)
	}
	var sho trustShowOutput
	if err := json.Unmarshal([]byte(outBuf.String()), &sho); err != nil {
		t.Fatalf("unmarshal show JSON: %v", err)
	}
	if sho.Alias != "show-pub" {
		t.Errorf("alias = %q, want show-pub", sho.Alias)
	}
}

func TestTrustRemoveWithYes(t *testing.T) {
	homeDir, storePath := prepareTrustHome(t)
	pemData, fp := generateTestKeyPEM(t)

	keyFile := filepath.Join(t.TempDir(), "key.pem")
	_ = os.WriteFile(keyFile, []byte(pemData), 0o600)

	// Add.
	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"trust", "add", fp, "--key", keyFile, "--alias", "remove-pub", "--home", homeDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trust add: %v", err)
	}

	// Remove with --yes (non-interactive).
	resetAgentCmd()
	cmd = AgentCmd()
	outBuf := new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "remove", fp, "--yes", "--home", homeDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trust remove: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Removed publisher") {
		t.Errorf("unexpected output: %s", outBuf.String())
	}

	// Verify store is now empty.
	store, err := trust.Load(storePath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if store.Len() != 0 {
		t.Errorf("store has %d publishers after remove, want 0", store.Len())
	}
}

func TestTrustRemoveNotFound(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	_, fp := generateTestKeyPEM(t)

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"trust", "remove", fp, "--yes", "--home", homeDir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error removing non-existent publisher")
	}
}

func TestTrustShowNotFound(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	_, fp := generateTestKeyPEM(t)

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"trust", "show", fp, "--home", homeDir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error showing non-existent publisher")
	}
}

func TestTrustMismatchedFingerprint(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	pemData, _ := generateTestKeyPEM(t)
	_, wrongFP := generateTestKeyPEM(t) // different key

	keyFile := filepath.Join(t.TempDir(), "key.pem")
	_ = os.WriteFile(keyFile, []byte(pemData), 0o600)

	resetAgentCmd()
	cmd := AgentCmd()
	errBuf := new(strings.Builder)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"trust", "add", wrongFP, "--key", keyFile, "--home", homeDir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for mismatched fingerprint")
	}
}

func TestTrustCorruptStore(t *testing.T) {
	homeDir, storePath := prepareTrustHome(t)

	// Create corrupt store file.
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(storePath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	resetAgentCmd()
	cmd := AgentCmd()
	errBuf := new(strings.Builder)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"trust", "list", "--home", homeDir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for corrupt store")
	}
	if !strings.Contains(err.Error(), "trust store") || !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error doesn't mention corrupt store: %v", err)
	}
}

func TestTrustEmptyList(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)

	resetAgentCmd()
	cmd := AgentCmd()
	outBuf := new(strings.Builder)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"trust", "list", "--home", homeDir})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("trust list empty: %v", err)
	}
	if !strings.Contains(outBuf.String(), "No trusted publishers") {
		t.Errorf("unexpected output: %s", outBuf.String())
	}
}

func TestTrustInvalidAlias(t *testing.T) {
	homeDir, _ := prepareTrustHome(t)
	pemData, fp := generateTestKeyPEM(t)

	keyFile := filepath.Join(t.TempDir(), "key.pem")
	_ = os.WriteFile(keyFile, []byte(pemData), 0o600)

	resetAgentCmd()
	cmd := AgentCmd()
	errBuf := new(strings.Builder)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"trust", "add", fp, "--key", keyFile, "--alias", "Invalid Alias!", "--home", homeDir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid alias")
	}
}