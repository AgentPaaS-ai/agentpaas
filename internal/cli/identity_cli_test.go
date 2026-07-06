package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// phasedReader delivers data in separate "phases" (chunks), returning io.EOF
// at the end of each phase. This enables tests with io.ReadAll to consume
// each phase independently — critical for the export passphrase flow which
// calls readPassword twice (enter + confirm).
//
// Each phase byte slice typically ends with a newline so that readPassword's
// strings.TrimSpace produces a clean passphrase.
type phasedReader struct {
	phases   [][]byte
	phaseIdx int
	pos      int
}

func (r *phasedReader) Read(p []byte) (n int, err error) {
	if r.phaseIdx >= len(r.phases) {
		return 0, io.EOF
	}
	chunk := r.phases[r.phaseIdx]
	n = copy(p, chunk[r.pos:])
	r.pos += n
	if r.pos >= len(chunk) {
		r.pos = 0
		r.phaseIdx++
		return n, io.EOF
	}
	return n, nil
}

// executeIdentityCmdWithReader runs the `agent` root command with the given
// args, using the specified reader as stdin. The caller must have already
// wired identityStoreFactory (typically via setupIdentityTest).
// Captures both cobra-writer output and os.Stdout (for commands using
// fmt.Println).
func executeIdentityCmdWithReader(t *testing.T, r io.Reader, args ...string) (string, string, error) {
	t.Helper()
	resetAgentCmd()

	cmd := AgentCmd()
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	if r != nil {
		cmd.SetIn(r)
	}

	// Capture os.Stdout for commands using fmt.Println (JSON output, etc.).
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	stdoutDone := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rOut)
		stdoutDone <- buf.String()
	}()

	cmd.SetArgs(args)
	err := cmd.Execute()

	_ = wOut.Close()
	os.Stdout = oldStdout
	stdoutRaw := <-stdoutDone

	// Combine cobra-writer output with os.Stdout output.
	outResult := outBuf.String() + stdoutRaw
	errResult := errBuf.String()

	return outResult, errResult, err
}

// executeIdentityCmd runs the CLI with a string stdin input.
// If stdin is empty, sets cmd.In to an empty reader to force non-TTY mode.
func executeIdentityCmd(t *testing.T, stdin string, args ...string) (string, string, error) {
	var r io.Reader = &bytes.Buffer{} // empty reader = non-TTY default
	if stdin != "" {
		r = strings.NewReader(stdin)
	}
	return executeIdentityCmdWithReader(t, r, args...)
}

// setupIdentityTest creates a FakeKeyStore and wires identityStoreFactory
// to return it. Returns the fake keystore, home directory, and a cleanup
// function. Callers should `defer cleanup()`.
func setupIdentityTest(t *testing.T) (*identity.FakeKeyStore, string) {
	t.Helper()
	fakeKS := identity.NewFakeKeyStore()
	homeDir := t.TempDir()

	oldFactory := identityStoreFactory
	identityStoreFactory = func(cmd *cobra.Command) (identity.KeyStore, error) {
		return fakeKS, nil
	}
	t.Cleanup(func() {
		identityStoreFactory = oldFactory
		resetAgentCmd()
	})
	return fakeKS, homeDir
}

// ---------------------------------------------------------------------------
// TestIdentityInitAndShow
// Tests: init (non-TTY, --name) then show; fingerprints match; JSON output
// parses.
// ---------------------------------------------------------------------------
func TestIdentityInitAndShow(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// 1. Init.
	_, stderr, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "test-pub", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity init: %v\nstderr: %s", err, stderr)
	}

	// 2. Show (text).
	stdout, stderr, err := executeIdentityCmd(t, "",
		"identity", "show", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity show: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Name:") || !strings.Contains(stdout, "Fingerprint:") {
		t.Errorf("expected name and fingerprint in output:\n%s", stdout)
	}

	// 3. Show (JSON).
	stdout, _, err = executeIdentityCmd(t, "",
		"identity", "show", "--json", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity show --json: %v", err)
	}
	var result identityShowResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("unmarshal show JSON: %v\noutput: %s", err, stdout)
	}
	if result.Name != "test-pub" {
		t.Errorf("name = %q, want test-pub", result.Name)
	}
	if result.Fingerprint == "" {
		t.Error("fingerprint must not be empty")
	}
	if result.FingerprintDisplay == "" {
		t.Error("fingerprint_display must not be empty")
	}
	if result.PublicKeyPEM == "" {
		t.Error("public_key_pem must not be empty")
	}
	if result.CreatedAt == "" {
		t.Error("created_at must not be empty")
	}

	// Fingerprint output is 64 hex chars, display is groups of 4.
	if len(result.Fingerprint) != 64 {
		t.Errorf("fingerprint length = %d, want 64: %s", len(result.Fingerprint), result.Fingerprint)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityExportImportRoundtrip
// Tests: export + import round-trip; fingerprint identical after import.
// ---------------------------------------------------------------------------
func TestIdentityExportImportRoundtrip(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// Init an identity.
	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "export-test", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity init: %v", err)
	}

	// Get the fingerprint.
	stdout, _, err := executeIdentityCmd(t, "",
		"identity", "show", "--json", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity show: %v", err)
	}
	var show identityShowResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &show); err != nil {
		t.Fatalf("unmarshal show: %v", err)
	}
	origFP := show.Fingerprint

	// Export with a phased reader (two phase reads for enter + confirm).
	passphrase := "test-passphrase-12345"
	exportFile := filepath.Join(t.TempDir(), "export.enc")
	phases := [][]byte{
		[]byte(passphrase + "\n"),
		[]byte(passphrase + "\n"),
	}
	pr := &phasedReader{phases: phases}

	stdout, stderr, err := executeIdentityCmdWithReader(t, pr,
		"identity", "export", "--out", exportFile, "--home", homeDir)
	if err != nil {
		t.Fatalf("identity export: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Identity exported to") {
		t.Errorf("expected export success message, got: %s", stdout)
	}

	// Verify export file exists.
	if _, err := os.Stat(exportFile); err != nil {
		t.Fatalf("export file not created: %v", err)
	}

	// Now import into a fresh keystore (different home).
	_, homeDir2 := setupIdentityTest(t)

	// Import passphrase (single read).
	stdout, _, err = executeIdentityCmd(t, passphrase+"\n",
		"identity", "import", exportFile, "--home", homeDir2)
	if err != nil {
		t.Fatalf("identity import: %v\nstdout: %s", err, stdout)
	}

	// Show imported identity.
	stdout, _, err = executeIdentityCmd(t, "",
		"identity", "show", "--json", "--home", homeDir2)
	if err != nil {
		t.Fatalf("identity show after import: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &show); err != nil {
		t.Fatalf("unmarshal show: %v", err)
	}
	if show.Fingerprint != origFP {
		t.Errorf("imported fingerprint %q != original %q", show.Fingerprint, origFP)
	}
	if show.Name != "export-test" {
		t.Errorf("imported name = %q, want export-test", show.Name)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityImportWrongPassphrase
// Tests: wrong passphrase on import fails with typed error.
// ---------------------------------------------------------------------------
func TestIdentityImportWrongPassphrase(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// Init + export.
	passphrase := "correct-passphrase-123"
	exportFile := filepath.Join(t.TempDir(), "export.enc")

	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "wrong-pass-test", "--home", homeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	phases := [][]byte{
		[]byte(passphrase + "\n"),
		[]byte(passphrase + "\n"),
	}
	_, _, err = executeIdentityCmdWithReader(t, &phasedReader{phases: phases},
		"identity", "export", "--out", exportFile, "--home", homeDir)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import with wrong passphrase into fresh keystore.
	_, homeDir2 := setupIdentityTest(t)

	_, _, err = executeIdentityCmd(t, "wrong-passphrase!!\n",
		"identity", "import", exportFile, "--home", homeDir2)
	if err == nil {
		t.Fatal("expected error for wrong import passphrase, got nil")
	}
	if !strings.Contains(err.Error(), "decrypt identity") && !strings.Contains(err.Error(), "wrong passphrase") {
		t.Errorf("error should mention decrypt/passphrase failure: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityImportOverDifferentIdentity
// Tests: importing over a different existing identity fails.
// ---------------------------------------------------------------------------
func TestIdentityImportOverDifferentIdentity(t *testing.T) {
	fakeKS, homeDir := setupIdentityTest(t)

	// Init first identity.
	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "first-identity", "--home", homeDir)
	if err != nil {
		t.Fatalf("init first identity: %v", err)
	}

	// Create a second identity directly via the identity package and export it
	// through the CLI using a temporary factory override.
	secondKS := identity.NewFakeKeyStore()
	_, err = identity.CreatePublisherIdentity(secondKS, "second-identity")
	if err != nil {
		t.Fatalf("create second identity: %v", err)
	}

	// Temporarily swap factory to use secondKS for the export.
	oldFactory := identityStoreFactory
	identityStoreFactory = func(cmd *cobra.Command) (identity.KeyStore, error) {
		return secondKS, nil
	}
	exportFile := filepath.Join(t.TempDir(), "export2.enc")
	passphrase := "export-passphrase-456"
	phases := [][]byte{
		[]byte(passphrase + "\n"),
		[]byte(passphrase + "\n"),
	}
	_, _, err = executeIdentityCmdWithReader(t, &phasedReader{phases: phases},
		"identity", "export", "--out", exportFile, "--home", homeDir)
	if err != nil {
		t.Fatalf("export second identity: %v", err)
	}
	// Restore factory to the first keystore.
	identityStoreFactory = oldFactory

	// Try to import the second identity into homeDir which already has the first.
	_, _, err = executeIdentityCmd(t, passphrase+"\n",
		"identity", "import", exportFile, "--home", homeDir)
	if err == nil {
		t.Fatal("expected error importing over different identity, got nil")
	}
	if !strings.Contains(err.Error(), "different publisher identity already exists") {
		t.Errorf("error should mention different publisher identity: %v", err)
	}

	// Verify the first identity is still intact.
	_, _, err = executeIdentityCmd(t, "",
		"identity", "show", "--home", homeDir)
	if err != nil {
		t.Errorf("first identity should still be present: %v", err)
	}
	_ = fakeKS
}

// ---------------------------------------------------------------------------
// TestIdentityImportSameIdentityIdempotent
// Tests: importing the same identity succeeds idempotently.
// ---------------------------------------------------------------------------
func TestIdentityImportSameIdentityIdempotent(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// Init + export.
	passphrase := "idempotent-passphrase-789"
	exportFile := filepath.Join(t.TempDir(), "export.enc")

	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "idempotent-test", "--home", homeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	// Export.
	phases := [][]byte{
		[]byte(passphrase + "\n"),
		[]byte(passphrase + "\n"),
	}
	_, _, err = executeIdentityCmdWithReader(t, &phasedReader{phases: phases},
		"identity", "export", "--out", exportFile, "--home", homeDir)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import the same identity back into the SAME keystore.
	stdout, _, err := executeIdentityCmd(t, passphrase+"\n",
		"identity", "import", exportFile, "--home", homeDir)
	if err != nil {
		t.Fatalf("identity import (same identity): %v\nstdout: %s", err, stdout)
	}
	if !strings.Contains(stdout, "already present") {
		t.Errorf("expected 'already present' message, got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityInitWithoutNameNonTTY
// Tests: init without --name in non-TTY mode errors.
// ---------------------------------------------------------------------------
func TestIdentityInitWithoutNameNonTTY(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--home", homeDir)
	if err == nil {
		t.Fatal("expected error for init without --name in non-TTY mode, got nil")
	}
	if !strings.Contains(err.Error(), "--name flag is required") {
		t.Errorf("error should mention --name: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityInitInvalidName
// Tests: init with invalid name errors.
// ---------------------------------------------------------------------------
func TestIdentityInitInvalidName(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// Name with uppercase and spaces is invalid.
	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "Invalid Name!", "--home", homeDir)
	if err == nil {
		t.Fatal("expected error for invalid publisher name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid publisher name") {
		t.Errorf("error should mention invalid publisher name: %v", err)
	}

	// Name that's too long.
	longName := strings.Repeat("a", 40)
	_, _, err = executeIdentityCmd(t, "",
		"identity", "init", "--name", longName, "--home", homeDir)
	if err == nil {
		t.Fatal("expected error for too-long publisher name, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestIdentityShowWhenNoIdentity
// Tests: show when no identity exits with code 1.
// ---------------------------------------------------------------------------
func TestIdentityShowWhenNoIdentity(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	_, _, err := executeIdentityCmd(t, "",
		"identity", "show", "--home", homeDir)
	if err == nil {
		t.Fatal("expected error for show with no identity, got nil")
	}
	if !strings.Contains(err.Error(), "no publisher identity") {
		t.Errorf("error should mention no publisher identity: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityForceRotate
// Tests: --force-rotate creates new identity with different fingerprint.
// ---------------------------------------------------------------------------
func TestIdentityForceRotate(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// Init first identity.
	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "rotate-test", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity init: %v", err)
	}

	// Get first fingerprint.
	stdout, _, err := executeIdentityCmd(t, "",
		"identity", "show", "--json", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity show: %v", err)
	}
	var show identityShowResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &show); err != nil {
		t.Fatalf("unmarshal show: %v", err)
	}
	firstFP := show.Fingerprint

	// Force-rotate.
	_, stderr, err := executeIdentityCmd(t, "",
		"identity", "init", "--force-rotate", "--name", "rotate-test-v2", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity force-rotate: %v\nstderr: %s", err, stderr)
	}

	// Get second fingerprint.
	stdout, _, err = executeIdentityCmd(t, "",
		"identity", "show", "--json", "--home", homeDir)
	if err != nil {
		t.Fatalf("identity show after rotate: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &show); err != nil {
		t.Fatalf("unmarshal show: %v", err)
	}
	secondFP := show.Fingerprint

	if firstFP == secondFP {
		t.Errorf("force-rotate should produce different fingerprint; both are %q", firstFP)
	}
}

// ---------------------------------------------------------------------------
// TestIdentityInitAlreadyExists
// Tests: init when identity exists returns error suggesting --force-rotate.
// ---------------------------------------------------------------------------
func TestIdentityInitAlreadyExists(t *testing.T) {
	_, homeDir := setupIdentityTest(t)

	// Init first.
	_, _, err := executeIdentityCmd(t, "",
		"identity", "init", "--name", "exists-test", "--home", homeDir)
	if err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Init again.
	_, _, err = executeIdentityCmd(t, "",
		"identity", "init", "--name", "exists-test-2", "--home", homeDir)
	if err == nil {
		t.Fatal("expected error for duplicate init, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention already exists: %v", err)
	}
}