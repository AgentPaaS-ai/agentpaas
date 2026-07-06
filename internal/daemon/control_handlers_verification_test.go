package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

const testPolicyYAML = `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.example.com
    ports: [443]
`

// deployTestAgentWithPolicy deploys an agent with a signed lock that includes
// a policy digest. The deployed policy.yaml is written as a sidecar.
func deployTestAgentWithPolicy(t *testing.T, hp *home.HomePaths, agentName string) {
	t.Helper()
	lock, err := pack.NewSignedTestLock(agentName, []byte(testPolicyYAML))
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}
	if err := pack.RecordDeployment(hp.Home, agentName, lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}
}

// newVerificationTestServer creates a controlServer with a mock runtime that
// tracks CreateNetwork calls. Returns the server and a pointer to the call count.
func newVerificationTestServer(t *testing.T, hp *home.HomePaths) (*controlServer, *int) {
	t.Helper()
	networkCalls := 0
	mock := &mockRuntimeDriver{
		createNetworkFunc: func(_ context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
			networkCalls++
			if spec.Internal {
				return runtime.NetworkID("network-internal"), nil
			}
			return runtime.NetworkID("network-egress"), nil
		},
		createFunc: func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
			if spec.Image == runtime.GatewayImage {
				return runtime.ContainerID("gateway-test"), nil
			}
			return runtime.ContainerID("container-test"), nil
		},
		startFunc: func(_ context.Context, _ runtime.ContainerID) error {
			return nil
		},
		inspectContainerIPFunc: func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
			return "10.0.0.2", nil
		},
	}

	// Set up audit writer so VerifyDeployedIntegrity doesn't panic on nil.
	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}
	server.runtimeOnce.Do(func() {})
	server.dockerRT = runtime.NewDockerRuntimeWithDriver(mock)
	return server, &networkCalls
}

func TestVerifyDeployedAgent_MutatedPolicy_FailsBeforeDocker(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	// Mutate deployed policy.yaml.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	mutatedPolicy := []byte("version: \"1.0\"\nagent:\n  name: test-agent\negress:\n  - domain: evil.com\n    ports: [443]\n")
	if err := os.WriteFile(filepath.Join(deployedDir, "policy.yaml"), mutatedPolicy, 0o644); err != nil {
		t.Fatalf("mutate policy.yaml: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err == nil {
		t.Fatal("Run() should fail when policy.yaml is mutated")
	}
	if *networkCalls != 0 {
		t.Fatalf("CreateNetwork called %d times, want 0 (must fail before Docker resources)", *networkCalls)
	}
}

func TestVerifyDeployedAgent_DeletedPolicy_FailsBeforeDocker(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	// Delete deployed policy.yaml.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	if err := os.Remove(filepath.Join(deployedDir, "policy.yaml")); err != nil {
		t.Fatalf("remove policy.yaml: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err == nil {
		t.Fatal("Run() should fail when policy.yaml is missing but lock has policy_digest")
	}
	if *networkCalls != 0 {
		t.Fatalf("CreateNetwork called %d times, want 0", *networkCalls)
	}
}

func TestVerifyDeployedAgent_MutatedLock_FailsBeforeDocker(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	// Mutate agent.lock (change a field without re-signing).
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	lockPath := filepath.Join(deployedDir, "agent.lock")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read agent.lock: %v", err)
	}
	// Corrupt by appending garbage.
	mutated := append(lockData, []byte("\n// tampered")...)
	if err := os.WriteFile(lockPath, mutated, 0o644); err != nil {
		t.Fatalf("mutate agent.lock: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err = server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err == nil {
		t.Fatal("Run() should fail when agent.lock is tampered")
	}
	if *networkCalls != 0 {
		t.Fatalf("CreateNetwork called %d times, want 0", *networkCalls)
	}
}

func TestVerifyDeployedAgent_MutatedImageDigest_FailsBeforeDocker(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	// Mutate image.digest.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	digestPath := filepath.Join(deployedDir, "image.digest")
	if err := os.WriteFile(digestPath, []byte("sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"), 0o644); err != nil {
		t.Fatalf("mutate image.digest: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err == nil {
		t.Fatal("Run() should fail when image.digest is tampered")
	}
	if *networkCalls != 0 {
		t.Fatalf("CreateNetwork called %d times, want 0", *networkCalls)
	}
}

func TestVerifyDeployedAgent_LegacyLockWithoutEnv_Fails(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	// Deploy with legacy lock (no policy YAML → no PolicyDigest).
	lock, err := pack.NewSignedTestLock("test-agent", nil)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}
	if err := pack.RecordDeployment(hp.Home, "test-agent", lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}

	// Ensure AGENTPAAS_ALLOW_LEGACY_LOCK is not set.
	os.Unsetenv("AGENTPAAS_ALLOW_LEGACY_LOCK")

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err = server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err == nil {
		t.Fatal("Run() should fail for legacy lock without AGENTPAAS_ALLOW_LEGACY_LOCK=1")
	}
	if *networkCalls != 0 {
		t.Fatalf("CreateNetwork called %d times, want 0", *networkCalls)
	}
}

func TestVerifyDeployedAgent_LegacyLockWithEnv_Passes(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	// Deploy with legacy lock (no policy YAML → no PolicyDigest).
	lock, err := pack.NewSignedTestLock("test-agent", nil)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}
	if err := pack.RecordDeployment(hp.Home, "test-agent", lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}

	t.Setenv("AGENTPAAS_ALLOW_LEGACY_LOCK", "1")

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err = server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() should pass for legacy lock with AGENTPAAS_ALLOW_LEGACY_LOCK=1: %v", err)
	}
	// Network calls should be > 0 because Run proceeds normally.
	if *networkCalls == 0 {
		t.Fatal("CreateNetwork should have been called for a valid legacy lock with env var set")
	}
}

func TestVerifyDeployedAgent_ValidPolicy_Passes(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() should pass with valid deployed agent: %v", err)
	}
	if *networkCalls == 0 {
		t.Fatal("CreateNetwork should have been called for a valid deployed agent")
	}
}
