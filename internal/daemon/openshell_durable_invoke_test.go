package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/openshellrt"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

const o02BrokerCred = "o02-broker"

// recordingOpenShell is the OpenShell client a deployment invoke must drive
// when OpenShell is configured. It does not store the broker secret.
type recordingOpenShell struct {
	mu          sync.Mutex
	brokerCalls int
	created     bool
	image       string
	name        string
	env         map[string]string
	provider    string
}

func (f *recordingOpenShell) EnsureBroker(_ context.Context, credID, secret string) (string, error) {
	if strings.TrimSpace(credID) == "" || strings.TrimSpace(secret) == "" {
		return "", errors.New("broker credential missing")
	}
	f.mu.Lock()
	f.brokerCalls++
	f.mu.Unlock()
	return "provider-" + credID, nil
}

func (f *recordingOpenShell) CreateAgentSandbox(_ context.Context, req openshellrt.SandboxRequest) (*openshellrt.SandboxHandle, error) {
	f.mu.Lock()
	f.created = true
	f.image = req.Image
	f.name = req.Name
	f.provider = req.Provider
	f.env = make(map[string]string, len(req.Env))
	for k, v := range req.Env {
		f.env[k] = v
	}
	f.mu.Unlock()
	return &openshellrt.SandboxHandle{
		Name:       req.Name,
		ServiceURL: "http://127.0.0.1:1",
	}, nil
}

func (f *recordingOpenShell) DeleteSandbox(context.Context, string) error { return nil }

func (f *recordingOpenShell) snapshot() (created bool, image, name, provider string, env map[string]string, brokerCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make(map[string]string, len(f.env))
	for k, v := range f.env {
		cp[k] = v
	}
	return f.created, f.image, f.name, f.provider, cp, f.brokerCalls
}

// TestInvokeDeployment_OpenShellSkipsDockerStack fails if a non-pipeline
// deployment invoke still takes the Docker path when OpenShell is set.
// A skipped launch is also a fail: the agent must be started in a sandbox.
func TestInvokeDeployment_OpenShellSkipsDockerStack(t *testing.T) {
	t.Setenv("AGENTPAAS_ALLOW_LEGACY_LOCK", "1")

	const agentName = "o02-openshell-agent"
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	lock, err := pack.NewSignedTestLockWithLLM(agentName, nil, o02BrokerCred)
	if err != nil {
		t.Fatalf("NewSignedTestLockWithLLM: %v", err)
	}
	if err := pack.RecordDeployment(hp.Home, agentName, lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}
	store := secrets.NewFakeKeyStore()
	if err := store.Set(context.Background(), o02BrokerCred, []byte("broker-secret-for-test")); err != nil {
		t.Fatalf("secret set: %v", err)
	}
	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	fake := &recordingOpenShell{}
	s := &controlServer{
		homePaths:          hp,
		auditWriter:        writer,
		version:            VersionInfo{DaemonVersion: "test"},
		secretStoreForTest: store,
		openshell:          fake,
		// disableContainerLaunch stays false. Skipping the launch is a fail.
		// testRuntime stays nil so useOpenShell is true, matching production.
	}
	if err := s.initRoutedStores(routedStoreRoot(hp)); err != nil {
		t.Fatalf("initRoutedStores: %v", err)
	}
	// Poison the Docker runtime seam so a mistaken Docker path cannot dial a
	// live engine. getOrCreateRuntime returns this error instead.
	const dockerEntered = "docker path entered while openshell is configured"
	s.runtimeOnce.Do(func() {
		s.runtimeErr = errors.New(dockerEntered)
	})
	if !s.useOpenShell() {
		t.Fatal("fixture must configure OpenShell without a Docker test runtime")
	}
	if s.disableContainerLaunch {
		t.Fatal("disableContainerLaunch skips the launch; that is not a pass")
	}
	if s.pipelineRuntime != nil {
		t.Fatal("pipeline reconcile is not the open break")
	}

	ctx := context.Background()
	dep, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName:    agentName,
		PackageVersion: "0.1.0",
		BundleDigest:   "sha256:o02bundle",
		ActorIdentity:  "tester",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	depID := dep.GetDeployment().GetDeploymentId()

	var runID string
	t.Cleanup(func() {
		if runID == "" {
			return
		}
		s.runMu.Lock()
		tr := s.runs[runID]
		s.runMu.Unlock()
		if tr == nil {
			return
		}
		if tr.CancelInvoke != nil {
			tr.CancelInvoke()
		}
		if tr.InvokeDone != nil {
			select {
			case <-tr.InvokeDone:
			case <-time.After(2 * time.Second):
			}
		}
	})

	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  depID,
		InputJson:      []byte(`{"x":1}`),
		IdempotencyKey: "o02-openshell-invoke",
		CallerIdentity: "tester",
	})
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("outcome=%v want ACCEPTED", resp.GetOutcome())
	}
	runID = resp.GetRunId()
	if runID == "" {
		t.Fatal("run_id empty")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		created, _, _, _, _, _ := fake.snapshot()
		if created {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// startDurableRun continues after CreateAgentSandbox in the same
	// goroutine. Wait so a Docker fallback cannot hide behind the flag.
	time.Sleep(150 * time.Millisecond)

	created, image, name, provider, env, brokerCalls := fake.snapshot()
	auditText := readAuditForTest(t, auditPath)
	if !created {
		t.Fatalf("OpenShell sandbox was not created; deployment invoke did not launch (skipped or Docker). audit=%s", auditText)
	}
	if brokerCalls == 0 {
		t.Fatal("EnsureBroker was not called; sandbox launch was not the Run path")
	}
	wantImage := pack.LocalImageRef(agentName, lock.ImageDigest)
	if image != wantImage {
		t.Fatalf("sandbox image=%q want %q", image, wantImage)
	}
	if name != openShellSandboxName(runID) {
		t.Fatalf("sandbox name=%q want %q", name, openShellSandboxName(runID))
	}
	if provider != "provider-"+o02BrokerCred {
		t.Fatalf("sandbox provider=%q", provider)
	}
	if env["AGENTPAAS_RUNTIME"] != "openshell" || env["AGENTPAAS_OPENSHELL"] != "1" {
		t.Fatalf("sandbox env is not the OpenShell run env: %#v", env)
	}

	s.runMu.Lock()
	tr := s.runs[runID]
	s.runMu.Unlock()
	if tr == nil || tr.OpenShellSandbox == "" {
		t.Fatal("tracked run is not an OpenShell sandbox")
	}
	if tr.Container != "" || tr.Network != "" || tr.EgressNetwork != "" || tr.Gateway != "" {
		t.Fatalf("docker resources allocated: container=%q network=%q egress=%q gateway=%q", tr.Container, tr.Network, tr.EgressNetwork, tr.Gateway)
	}

	for _, marker := range []string{
		dockerEntered,
		"docker_runtime_unavailable",
		"vulnerable_docker_engine",
		"create_network",
		"create_egress_network",
		"create_container",
		"start_container",
	} {
		if strings.Contains(auditText, marker) {
			t.Fatalf("deployment invoke took the Docker path (%s). audit=%s", marker, auditText)
		}
	}
}

func readAuditForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read audit: %v", err)
	}
	return string(b)
}
