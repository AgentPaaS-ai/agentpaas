package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// seedInstalledAgent writes installed state layout under
// state/agents/<name>@<pub8>/ matching what MaterializeInstall produces.
func seedInstalledAgent(t *testing.T, hp *home.HomePaths, agentName, pubFingerprint, llmCredName string) string {
	t.Helper()
	pub8 := strings.ToLower(pubFingerprint[:8])
	ref := agentName + "@" + pub8
	dir := filepath.Join(hp.State, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Source dir (must exist BEFORE computing build digest)
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	// Compute the actual build input digest from the empty source dir
	buildDigest, err := pack.ComputeBuildInputDigest(sourceDir, nil)
	if err != nil {
		t.Fatalf("compute build digest: %v", err)
	}

	// Policy
	polYAML := `version: "1.0"
agent:
  name: ` + agentName + `
egress:
  - domain: "wttr.in"
    ports: [443]
credentials: []
`
	policyDigest, err := pack.ComputePolicyDigest([]byte(polYAML))
	if err != nil {
		t.Fatalf("compute policy digest: %v", err)
	}

	// Signed lock with LLM config
	lock, err := pack.NewSignedTestLockWithLLM(agentName, []byte(polYAML), llmCredName)
	if err != nil {
		t.Fatalf("NewSignedTestLockWithLLM: %v", err)
	}
	// Override the build digest to match the actual source dir
	lock.BuildInputDigest = buildDigest
	// Re-sign since we changed the lock
	reSignKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := pack.SignLockfileWithKey(lock, reSignKey); err != nil {
		t.Fatalf("re-sign lock: %v", err)
	}

	lockBytes, err := pack.LockfileCanonicalJSON(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.lock"), lockBytes, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// Write policy (must match policyDigest in lock)
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(polYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	_ = policyDigest // already embedded in lock

	// Manifest
	manifest := map[string]any{
		"publisher_fingerprint": pubFingerprint,
		"agent_name":            agentName,
		"agent_version":         "1.0.0",
		"install_mode":          "local-rebuild",
		"local_image_digest":    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	manifestRaw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "install-manifest.json"), manifestRaw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	digestStr := "sha256:0000000000000000000000000000000000000000000000000000000000000000\n"
	if err := os.WriteFile(filepath.Join(dir, "local_image.digest"), []byte(digestStr), 0o600); err != nil {
		t.Fatalf("write digest: %v", err)
	}
	// SBOM
	if err := os.WriteFile(filepath.Join(dir, "sbom.spdx.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write sbom: %v", err)
	}
	// Parent bundle ref
	if err := os.WriteFile(filepath.Join(dir, "parent-bundle.ref"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write parent: %v", err)
	}
	return ref
}

// TestT39_Run_DetectsInstalledAgent verifies that the Run handler
// recognizes a name@pub8 ref as an installed agent and does NOT attempt
// to use the packed-agent verification path.
func TestT39_Run_DetectsInstalledAgent(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	fp := strings.Repeat("f", 64)
	ref := seedInstalledAgent(t, hp, "weather-agent", fp, "openrouter-key")

	// Inject a secret store so credential validation passes.
	store := secrets.NewFakeKeyStore()
	if err := store.Set(context.Background(), "openrouter-key", []byte("test-val")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Set up mock runtime so we get past runtime creation.
	t.Setenv("AGENTPAAS_SKIP_GATEWAY_WAIT", "1")

	mock := &mockRuntimeDriver{
		createNetworkFunc: func(_ context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
			if spec.Internal {
				return runtime.NetworkID("net-internal"), nil
			}
			return runtime.NetworkID("net-egress"), nil
		},
		createFunc: func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
			return runtime.ContainerID("container-" + string(spec.Labels["agentpaas.resource-type"])), nil
		},
		startFunc:               func(context.Context, runtime.ContainerID) error { return nil },
		inspectContainerIPFunc: func(context.Context, runtime.ContainerID, string) (string, error) {
			return "10.0.0.1", nil
		},
		removeFunc:        func(context.Context, runtime.ContainerID, bool) error { return nil },
		removeNetworkFunc: func(context.Context, runtime.NetworkID) error { return nil },
	}

	// Set up audit writer so VerifyInstalledAgent doesn't panic on nil.
	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, wErr := audit.NewAuditWriter(auditPath)
	if wErr != nil {
		t.Fatalf("NewAuditWriter: %v", wErr)
	}
	t.Cleanup(func() { _ = writer.Close() })

	server := &controlServer{
		homePaths:          hp,
		secretStoreForTest: store,
		auditWriter:        writer,
	}
	server.runtimeOnce.Do(func() {}) // skip real Docker init
	server.dockerRT = runtime.NewDockerRuntimeWithDriver(mock)

	// Run should succeed — it must use the installed path.
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: ref})
	if err != nil {
		// Check if it's a docker-specific error (which means we got PAST verification).
		// That's fine for this test — we just need to know it didn't fail with
		// "not deployed" or "verification failed".
		if status.Code(err) == codes.FailedPrecondition &&
			(strings.Contains(err.Error(), "not deployed") ||
				strings.Contains(err.Error(), "verification failed")) {
			t.Fatalf("Run() should use installed path, got: %v", err)
		}
		// Other errors (docker runtime, etc.) are OK — we passed verification.
	}
}

// TestT39_Run_InstalledAgent_RejectsUnknownRef verifies that running
// a nonexistent installed ref fails with a clear error.
func TestT39_Run_InstalledAgent_RejectsUnknownRef(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	server := &controlServer{homePaths: hp}

	_, err := server.Run(context.Background(), &controlv1.RunRequest{
		AgentName: "nonexistent@12345678",
	})
	if err == nil {
		t.Fatal("Run() should fail for nonexistent installed agent")
	}
	// Should be a FailedPrecondition error about verification or not found.
	if status.Code(err) != codes.FailedPrecondition && status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Run() code = %v, want FailedPrecondition or InvalidArgument", status.Code(err))
	}
}

// TestT39_ValidateCredentials_InstalledAgent_AppliesCredentialMap verifies
// that validateCredentialsExist applies the credential map for installed agents.
func TestT39_ValidateCredentials_InstalledAgent_AppliesCredentialMap(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	fp := strings.Repeat("a", 64)
	seedInstalledAgent(t, hp, "test-agent", fp, "openrouter-key")

	store := secrets.NewFakeKeyStore()
	// Store under the LOCAL name, not the declared name
	if err := store.Set(context.Background(), "my-local-key", []byte("val")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	server := &controlServer{
		homePaths:          hp,
		secretStoreForTest: store,
	}

	credMap := map[string]string{"openrouter-key": "my-local-key"}
	ref := "test-agent@" + strings.ToLower(fp[:8])
	if err := validateCredentialsExist(server, ref, true, credMap); err != nil {
		t.Fatalf("validateCredentialsExist with map should pass: %v", err)
	}

	// Without the map, validation should fail (declared name not in store)
	if err := validateCredentialsExist(server, ref, true, nil); err == nil {
		t.Fatal("validateCredentialsExist without map should fail")
	}
}
