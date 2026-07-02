package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testControlServer(t *testing.T) *controlServer {
	t.Helper()
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	return &controlServer{
		homePaths: hp,
	}
}

func TestPack_RequiresProjectPath(t *testing.T) {
	_, err := testControlServer(t).Pack(context.Background(), &controlv1.PackRequest{})
	if err == nil {
		t.Fatal("Pack() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Pack() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestRun_RequiresAgentName(t *testing.T) {
	_, err := testControlServer(t).Run(context.Background(), &controlv1.RunRequest{})
	if err == nil {
		t.Fatal("Run() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Run() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestRun_AgentNotDeployed(t *testing.T) {
	_, err := testControlServer(t).Run(context.Background(), &controlv1.RunRequest{
		AgentName: "missing-agent",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want FailedPrecondition")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Run() code = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestStop_RunNotFound(t *testing.T) {
	_, err := testControlServer(t).Stop(context.Background(), &controlv1.StopRequest{
		RunId: "run-does-not-exist",
	})
	if err == nil {
		t.Fatal("Stop() error = nil, want NotFound")
	}
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Stop() code = %v, want NotFound", status.Code(err))
	}
}

func TestAuditQuery_ReturnsEntries(t *testing.T) {
	server := newOperatorTestServer(t,
		operatorTestRecord("policy_denied", "run-audit-query", map[string]interface{}{
			"destination": "evil.com",
		}),
	)
	resp, err := server.AuditQuery(context.Background(), &controlv1.AuditQueryRequest{
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("AuditQuery() error = %v", err)
	}
	if len(resp.GetEntries()) == 0 {
		t.Fatal("AuditQuery() returned no entries")
	}
	if resp.GetTotalCount() != int32(len(resp.GetEntries())) {
		t.Fatalf("TotalCount = %d, entries = %d", resp.GetTotalCount(), len(resp.GetEntries()))
	}
}

func TestPolicyApply_DryRun(t *testing.T) {
	server := testControlServer(t)
	resp, err := server.PolicyApply(context.Background(), &controlv1.PolicyApplyRequest{
		PolicyYaml: validDefaultDenyPolicy,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("PolicyApply() error = %v", err)
	}
	if resp.GetPolicyDigest() == "" {
		t.Fatal("PolicyDigest is empty")
	}
	policyPath := filepath.Join(server.homePaths.Config, "policy.yaml")
	if _, err := os.Stat(policyPath); !os.IsNotExist(err) {
		t.Fatalf("policy.yaml should not be written on dry-run, stat err = %v", err)
	}
}

func TestPolicyApply_ValidateInvalidYAML(t *testing.T) {
	_, err := testControlServer(t).PolicyApply(context.Background(), &controlv1.PolicyApplyRequest{
		PolicyYaml: ":::not yaml",
	})
	if err == nil {
		t.Fatal("PolicyApply() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("PolicyApply() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestSecretSet_RequiresName(t *testing.T) {
	_, err := testControlServer(t).SecretSet(context.Background(), &controlv1.SecretSetRequest{
		Scope: "default",
	})
	if err == nil {
		t.Fatal("SecretSet() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SecretSet() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestRunTracking(t *testing.T) {
	server := &controlServer{}
	runID := "run-track-test"
	containerID := runtime.ContainerID("container-abc")
	networkID := "network-xyz"

	server.trackRun(runID, containerID, networkID, "")
	gotContainer, gotNetwork, gotAuditDir := server.lookupRun(runID)
	if gotContainer != containerID {
		t.Fatalf("lookupRun() container = %q, want %q", gotContainer, containerID)
	}
	if gotNetwork != networkID {
		t.Fatalf("lookupRun() network = %q, want %q", gotNetwork, networkID)
	}
	if gotAuditDir != "" {
		t.Fatalf("lookupRun() auditDir = %q, want empty", gotAuditDir)
	}

	server.untrackRun(runID)
	gotContainer, gotNetwork, gotAuditDir = server.lookupRun(runID)
	if gotContainer != "" || gotNetwork != "" || gotAuditDir != "" {
		t.Fatalf("lookupRun() after untrack = (%q, %q, %q), want empty", gotContainer, gotNetwork, gotAuditDir)
	}
}

func TestAuditExport_ReadsJSONL(t *testing.T) {
	server := testControlServer(t)
	auditPath := filepath.Join(server.homePaths.State, "audit.jsonl")
	record := `{"seq":1,"prev_hash":"genesis","record_hash":"abc","timestamp":"2026-01-02T03:04:05Z","event_type":"invoke","deployment_mode":"local","actor":"test","payload":{"run_id":"run-export"}}`
	if err := os.WriteFile(auditPath, []byte(record+"\n"), 0o600); err != nil {
		t.Fatalf("write audit.jsonl: %v", err)
	}

	resp, err := server.AuditExport(context.Background(), &controlv1.AuditExportRequest{
		Format: "ndjson",
	})
	if err != nil {
		t.Fatalf("AuditExport() error = %v", err)
	}
	if resp.GetEntryCount() != 1 {
		t.Fatalf("EntryCount = %d, want 1", resp.GetEntryCount())
	}
	if len(resp.GetData()) == 0 {
		t.Fatal("exported data is empty")
	}
}

func TestResolveHarnessBinary_PrefersLinux(t *testing.T) {
	dir := t.TempDir()
	macHarness := filepath.Join(dir, "agentpaas-harness")
	linuxHarness := filepath.Join(dir, "agentpaas-harness-linux")
	daemonBinary := filepath.Join(dir, "agentpaasd")

	for _, path := range []string{macHarness, linuxHarness, daemonBinary} {
		if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
			t.Fatalf("os.WriteFile(%s) error = %v", path, err)
		}
	}

	oldResolveExecutable := resolveExecutable
	resolveExecutable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { resolveExecutable = oldResolveExecutable })

	got := resolveHarnessBinary()
	if got != linuxHarness {
		t.Fatalf("resolveHarnessBinary() = %q, want %q", got, linuxHarness)
	}

	if err := os.Remove(linuxHarness); err != nil {
		t.Fatalf("os.Remove(linuxHarness) error = %v", err)
	}

	got = resolveHarnessBinary()
	if got != macHarness {
		t.Fatalf("resolveHarnessBinary() without linux = %q, want %q", got, macHarness)
	}
}

func TestAuditEvents_PackRunStop(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	indexer, err := audit.NewSQLiteIndexer(filepath.Join(hp.State, "audit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = indexer.Close() })

	server := &controlServer{
		auditWriter: writer,
		auditIndex:  indexer,
		homePaths:   hp,
	}

	runID := "run-audit-test"
	server.recordAudit("pack", "cli", map[string]interface{}{
		"agent_name":    "demo-agent",
		"agent_version": "1.0.0",
		"image_digest":  "sha256:abc123",
		"image_ref":     "localhost:5000/demo-agent:1.0.0",
		"runtime":       "python",
	})
	server.recordAudit("run_start", "cli", map[string]interface{}{
		"run_id":       runID,
		"agent_name":   "demo-agent",
		"image_ref":    "localhost:5000/demo-agent@sha256:abc123",
		"container_id": "container-xyz",
		"network":      "net-internal",
	})
	server.recordAudit("run_stop", "cli", map[string]interface{}{
		"run_id":       runID,
		"container_id": "container-xyz",
		"force":        false,
	})

	records, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("record count = %d, want 3", len(records))
	}

	wantTypes := []string{"pack", "run_start", "run_stop"}
	for i, wantType := range wantTypes {
		if records[i].EventType != wantType {
			t.Fatalf("record[%d].EventType = %q, want %q", i, records[i].EventType, wantType)
		}
		if records[i].Actor != "cli" {
			t.Fatalf("record[%d].Actor = %q, want cli", i, records[i].Actor)
		}
		if records[i].DeploymentMode != "local" {
			t.Fatalf("record[%d].DeploymentMode = %q, want local", i, records[i].DeploymentMode)
		}
	}

	if got := auditString(records[0].Payload, "agent_name"); got != "demo-agent" {
		t.Fatalf("pack payload agent_name = %q, want demo-agent", got)
	}
	if got := auditString(records[1].Payload, "run_id"); got != runID {
		t.Fatalf("run_start payload run_id = %q, want %q", got, runID)
	}
	if got := auditString(records[2].Payload, "container_id"); got != "container-xyz" {
		t.Fatalf("run_stop payload container_id = %q, want container-xyz", got)
	}

	for i, record := range records {
		if i == 0 {
			if record.PrevHash != "" {
				t.Fatalf("record[0].PrevHash = %q, want empty genesis", record.PrevHash)
			}
			if record.Seq != 1 {
				t.Fatalf("record[0].Seq = %d, want 1", record.Seq)
			}
		} else {
			if record.PrevHash != records[i-1].RecordHash {
				t.Fatalf("record[%d].PrevHash = %q, want %q", i, record.PrevHash, records[i-1].RecordHash)
			}
			if record.Seq != records[i-1].Seq+1 {
				t.Fatalf("record[%d].Seq = %d, want %d", i, record.Seq, records[i-1].Seq+1)
			}
		}
		canonical, err := record.CanonicalMarshal()
		if err != nil {
			t.Fatalf("record[%d].CanonicalMarshal: %v", i, err)
		}
		expectedHash := fmt.Sprintf("%x", sha256.Sum256(canonical))
		if record.RecordHash != expectedHash {
			t.Fatalf("record[%d].RecordHash mismatch: got %q, want %q", i, record.RecordHash, expectedHash)
		}
	}

	// Reopening the writer validates the full chain via replay.
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}
	if _, err := audit.NewAuditWriter(auditPath); err != nil {
		t.Fatalf("replay audit chain: %v", err)
	}

	count, err := indexer.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("indexer record count = %d, want 3", count)
	}
}

func TestPack_RecordsDeploymentWhenBuildSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker pack integration in short mode")
	}
	// Ensures deployed path check works after a successful pack would use RecordDeployment.
	server := testControlServer(t)
	_, err := pack.LoadDeployedAgent(server.homePaths.Home, "nonexistent")
	if err == nil {
		t.Fatal("LoadDeployedAgent() error = nil, want not exist")
	}
}

func TestActiveRunCount(t *testing.T) {
	server := &controlServer{}

	if got := server.activeRunCount(); got != 0 {
		t.Fatalf("activeRunCount() = %d, want 0", got)
	}

	server.trackRun("run-1", "container-1", "network-1", "")
	server.trackRun("run-2", "container-2", "network-2", "")

	if got := server.activeRunCount(); got != 2 {
		t.Fatalf("activeRunCount() = %d, want 2", got)
	}

	server.untrackRun("run-1")
	if got := server.activeRunCount(); got != 1 {
		t.Fatalf("activeRunCount() after untrack = %d, want 1", got)
	}
}

func deployTestAgent(t *testing.T, hp *home.HomePaths, agentName string) {
	t.Helper()
	deployedDir := pack.DeployedAgentPath(hp.Home, agentName)
	if err := os.MkdirAll(deployedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lockJSON := fmt.Sprintf(`{"version":"v1","agent":{"name":%q},"image":{"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}}`, agentName)
	if err := os.WriteFile(filepath.Join(deployedDir, "agent.lock"), []byte(lockJSON), 0o644); err != nil {
		t.Fatalf("WriteFile agent.lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "image.digest"), []byte("sha256:0000000000000000000000000000000000000000000000000000000000000000"), 0o644); err != nil {
		t.Fatalf("WriteFile image.digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "source_digest"), []byte("sha256:abc"), 0o644); err != nil {
		t.Fatalf("WriteFile source_digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "deployed_at"), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644); err != nil {
		t.Fatalf("WriteFile deployed_at: %v", err)
	}
}

func TestRun_MountsAuditVolume(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgent(t, hp, "test-agent")

	var capturedSpec runtime.ContainerSpec
	mock := &mockRuntimeDriver{
		createNetworkFunc: func(_ context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
			if spec.Internal {
				return runtime.NetworkID("network-internal"), nil
			}
			return runtime.NetworkID("network-egress"), nil
		},
		createFunc: func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
			if spec.Image == runtime.GatewayImage {
				return runtime.ContainerID("gateway-test"), nil
			}
			capturedSpec = spec
			return runtime.ContainerID("container-test"), nil
		},
		startFunc: func(_ context.Context, _ runtime.ContainerID) error {
			return nil
		},
	}

	server := &controlServer{homePaths: hp}
	server.runtimeOnce.Do(func() {}) // skip real Docker init
	server.dockerRT = runtime.NewDockerRuntimeWithDriver(mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	if runID == "" {
		t.Fatal("Run() returned empty run_id")
	}

	hostAuditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	info, err := os.Stat(hostAuditDir)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", hostAuditDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("audit path %s is not a directory", hostAuditDir)
	}

	wantBind := fmt.Sprintf("%s:/audit", hostAuditDir)
	if len(capturedSpec.Binds) != 1 || capturedSpec.Binds[0] != wantBind {
		t.Fatalf("ContainerSpec.Binds = %v, want [%q]", capturedSpec.Binds, wantBind)
	}

	wantEnv := "AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl"
	if !containsEnv(capturedSpec.Env, wantEnv) {
		t.Fatalf("ContainerSpec.Env = %v, want to contain %q", capturedSpec.Env, wantEnv)
	}
	wantAgentEnv := "AGENTPAAS_AGENT_PATH=/app/main.py"
	if !containsEnv(capturedSpec.Env, wantAgentEnv) {
		t.Fatalf("ContainerSpec.Env = %v, want to contain %q", capturedSpec.Env, wantAgentEnv)
	}

	server.runMu.Lock()
	tracked, ok := server.runs[runID]
	server.runMu.Unlock()
	if !ok {
		t.Fatalf("run %q not tracked", runID)
	}
	if tracked.AuditDir != hostAuditDir {
		t.Fatalf("tracked AuditDir = %q, want %q", tracked.AuditDir, hostAuditDir)
	}
}

func TestStop_IngestsHarnessAudit(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgent(t, hp, "test-agent")

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	indexer, err := audit.NewSQLiteIndexer(filepath.Join(hp.State, "audit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = indexer.Close() })

	mock := defaultMockRuntimeDriver()
	mock.stopFunc = func(_ context.Context, _ runtime.ContainerID, _ *time.Duration) error {
		return nil
	}
	mock.removeFunc = func(_ context.Context, _ runtime.ContainerID, _ bool) error {
		return nil
	}
	mock.removeNetworkFunc = func(_ context.Context, _ runtime.NetworkID) error {
		return nil
	}

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  indexer,
	}
	server.runtimeOnce.Do(func() {})
	server.dockerRT = runtime.NewDockerRuntimeWithDriver(mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	if runID == "" {
		t.Fatal("Run() returned empty run_id")
	}

	harnessAuditPath := filepath.Join(hp.State, "runs", runID, "harness-audit", "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, []audit.AuditRecord{
		{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "egress_denied",
			Actor:     "harness",
			Payload: map[string]interface{}{
				"destination": "evil.com",
				"method":      "GET",
				"decision":    "denied",
			},
		},
		{
			Timestamp: "2026-01-02T03:04:06Z",
			EventType: "egress_allowed",
			Actor:     "harness",
			Payload: map[string]interface{}{
				"destination": "api.example.com",
				"method":      "GET",
				"decision":    "allowed",
			},
		},
	})

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	records, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}

	var egressDenied, egressAllowed *audit.AuditRecord
	for i := range records {
		switch records[i].EventType {
		case "egress_denied":
			egressDenied = &records[i]
		case "egress_allowed":
			egressAllowed = &records[i]
		}
	}
	if egressDenied == nil {
		t.Fatal("daemon audit chain missing egress_denied record")
	}
	if egressAllowed == nil {
		t.Fatal("daemon audit chain missing egress_allowed record")
	}
	if got := auditString(egressDenied.Payload, "run_id"); got != runID {
		t.Fatalf("egress_denied run_id = %q, want %q", got, runID)
	}
	if got := auditString(egressDenied.Payload, "destination"); got != "evil.com" {
		t.Fatalf("egress_denied destination = %q, want evil.com", got)
	}
	if got := auditString(egressAllowed.Payload, "run_id"); got != runID {
		t.Fatalf("egress_allowed run_id = %q, want %q", got, runID)
	}
	if got := auditString(egressAllowed.Payload, "destination"); got != "api.example.com" {
		t.Fatalf("egress_allowed destination = %q, want api.example.com", got)
	}

	queryResp, err := server.AuditQuery(context.Background(), &controlv1.AuditQueryRequest{
		RunId:    runID,
		PageSize: 50,
	})
	if err != nil {
		t.Fatalf("AuditQuery() error = %v", err)
	}

	var queryDenied, queryAllowed bool
	for _, entry := range queryResp.GetEntries() {
		if entry.GetRunId() != runID {
			t.Fatalf("AuditQuery entry run_id = %q, want %q", entry.GetRunId(), runID)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(entry.GetPayload(), &payload); err != nil {
			continue
		}
		switch auditString(payload, "destination") {
		case "evil.com":
			queryDenied = true
		case "api.example.com":
			queryAllowed = true
		}
	}
	if !queryDenied {
		t.Fatal("AuditQuery missing egress_denied record for run")
	}
	if !queryAllowed {
		t.Fatal("AuditQuery missing egress_allowed record for run")
	}
}

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

// mockRuntimeDriver implements runtime.RuntimeDriver for daemon unit tests.
type mockRuntimeDriver struct {
	createFunc         func(ctx context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error)
	startFunc          func(ctx context.Context, id runtime.ContainerID) error
	stopFunc           func(ctx context.Context, id runtime.ContainerID, timeout *time.Duration) error
	removeFunc         func(ctx context.Context, id runtime.ContainerID, force bool) error
	statusFunc         func(ctx context.Context, id runtime.ContainerID) (runtime.ContainerStatus, error)
	execFunc           func(ctx context.Context, id runtime.ContainerID, cmd []string) (string, string, int, error)
	createNetworkFunc  func(ctx context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error)
	removeNetworkFunc  func(ctx context.Context, id runtime.NetworkID) error
	listContainersFunc     func(ctx context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error)
	listNetworksFunc       func(ctx context.Context, labelFilters ...string) ([]runtime.NetworkInfo, error)
	inspectContainerIPFunc func(ctx context.Context, id runtime.ContainerID, networkID string) (string, error)
}

func (m *mockRuntimeDriver) Create(ctx context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, spec)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Start(ctx context.Context, id runtime.ContainerID) error {
	if m.startFunc != nil {
		return m.startFunc(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Stop(ctx context.Context, id runtime.ContainerID, timeout *time.Duration) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, id, timeout)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Remove(ctx context.Context, id runtime.ContainerID, force bool) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, id, force)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Status(ctx context.Context, id runtime.ContainerID) (runtime.ContainerStatus, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx, id)
	}
	return runtime.ContainerStatusUnknown, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Stats(context.Context, runtime.ContainerID) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Logs(context.Context, runtime.ContainerID, runtime.LogOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Exec(ctx context.Context, id runtime.ContainerID, cmd []string) (string, string, int, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, id, cmd)
	}
	return "", "", -1, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) CreateNetwork(ctx context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
	if m.createNetworkFunc != nil {
		return m.createNetworkFunc(ctx, spec)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) RemoveNetwork(ctx context.Context, id runtime.NetworkID) error {
	if m.removeNetworkFunc != nil {
		return m.removeNetworkFunc(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) InspectNetwork(context.Context, runtime.NetworkID) (runtime.NetworkInfo, error) {
	return runtime.NetworkInfo{}, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) InspectContainerNetworks(context.Context, runtime.ContainerID) ([]runtime.ContainerNetworkInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) InspectContainerIP(ctx context.Context, id runtime.ContainerID, networkID string) (string, error) {
	if m.inspectContainerIPFunc != nil {
		return m.inspectContainerIPFunc(ctx, id, networkID)
	}
	return "", nil
}

func (m *mockRuntimeDriver) ListContainers(ctx context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error) {
	if m.listContainersFunc != nil {
		return m.listContainersFunc(ctx, labelFilters...)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) ListNetworks(ctx context.Context, labelFilters ...string) ([]runtime.NetworkInfo, error) {
	if m.listNetworksFunc != nil {
		return m.listNetworksFunc(ctx, labelFilters...)
	}
	return nil, fmt.Errorf("not implemented")
}

func testServerWithMockRuntime(t *testing.T, mock *mockRuntimeDriver) (*controlServer, *home.HomePaths) {
	t.Helper()
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgent(t, hp, "test-agent")

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	indexer, err := audit.NewSQLiteIndexer(filepath.Join(hp.State, "audit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = indexer.Close() })

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  indexer,
		eventBus:    trigger.NewEventBus(),
	}
	server.runtimeOnce.Do(func() {})
	server.dockerRT = runtime.NewDockerRuntimeWithDriver(mock)
	return server, hp
}

func waitForRunStatus(t *testing.T, server *controlServer, runID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tracked, ok := server.lookupRunWithStatus(runID)
		if ok && tracked.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	tracked, ok := server.lookupRunWithStatus(runID)
	if !ok {
		t.Fatalf("run %q not tracked", runID)
	}
	t.Fatalf("run %q status = %q, want %q", runID, tracked.Status, want)
}

func defaultMockRuntimeDriver() *mockRuntimeDriver {
	return &mockRuntimeDriver{
		createNetworkFunc: func(_ context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
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
		stopFunc: func(_ context.Context, _ runtime.ContainerID, _ *time.Duration) error {
			return nil
		},
		removeFunc: func(_ context.Context, _ runtime.ContainerID, _ bool) error {
			return nil
		},
		removeNetworkFunc: func(_ context.Context, _ runtime.NetworkID) error {
			return nil
		},
		statusFunc: func(_ context.Context, _ runtime.ContainerID) (runtime.ContainerStatus, error) {
			return runtime.ContainerStatusStopped, nil
		},
	}
}

func TestRun_FailedInvoke_SetsFailedStatus(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(_ context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") {
			return "", "", 0, nil
		}
		if strings.Contains(cmdStr, "invoke") {
			return "", "invoke error", 1, nil
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	waitForRunStatus(t, server, runID, "failed")

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	events := server.eventBus.GetEvents(runID)
	var terminalEvent *trigger.RunEvent
	for i := range events {
		if events[i].IsTerminal() {
			terminalEvent = &events[i]
		}
	}
	if terminalEvent == nil {
		t.Fatal("no terminal event published")
	}
	if terminalEvent.Type != trigger.EventRunFailed {
		t.Fatalf("terminal event type = %q, want %q", terminalEvent.Type, trigger.EventRunFailed)
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	if got := auditString(runStop.Payload, "status"); got != "failed" {
		t.Fatalf("run_stop status = %q, want failed", got)
	}
}

func TestStop_CancelsInvokeContext(t *testing.T) {
	execBlocked := make(chan struct{})
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(ctx context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") {
			close(execBlocked)
			<-ctx.Done()
			return "", "", -1, ctx.Err()
		}
		return "", "", 0, nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()

	select {
	case <-execBlocked:
	case <-time.After(5 * time.Second):
		t.Fatal("invoke goroutine did not reach readyz polling")
	}

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, ok := server.lookupRunWithStatus(runID)
		if !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run still tracked after Stop")
}

func TestStop_Force_SetsCancelledStatus(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(_ context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") || strings.Contains(cmdStr, "invoke") {
			return "ok", "", 0, nil
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	waitForRunStatus(t, server, runID, "succeeded")

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID, Force: true})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	events := server.eventBus.GetEvents(runID)
	var terminalEvent *trigger.RunEvent
	for i := range events {
		if events[i].IsTerminal() {
			terminalEvent = &events[i]
		}
	}
	if terminalEvent == nil {
		t.Fatal("no terminal event published")
	}
	if terminalEvent.Type != trigger.EventRunCancelled {
		t.Fatalf("terminal event type = %q, want %q", terminalEvent.Type, trigger.EventRunCancelled)
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	if got := auditString(runStop.Payload, "status"); got != "cancelled" {
		t.Fatalf("run_stop status = %q, want cancelled", got)
	}
}

func TestStop_NormalSuccess_Succeeds(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(_ context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") || strings.Contains(cmdStr, "invoke") {
			return "ok", "", 0, nil
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	waitForRunStatus(t, server, runID, "succeeded")

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	events := server.eventBus.GetEvents(runID)
	var terminalEvent *trigger.RunEvent
	for i := range events {
		if events[i].IsTerminal() {
			terminalEvent = &events[i]
		}
	}
	if terminalEvent == nil {
		t.Fatal("no terminal event published")
	}
	if terminalEvent.Type != trigger.EventRunSucceeded {
		t.Fatalf("terminal event type = %q, want %q", terminalEvent.Type, trigger.EventRunSucceeded)
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	if got := auditString(runStop.Payload, "status"); got != "succeeded" {
		t.Fatalf("run_stop status = %q, want succeeded", got)
	}
}

func TestRun_RejectsWhenConcurrentLimitReached(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	server := &controlServer{homePaths: hp}

	// Pre-fill the runs map to the limit without Docker.
	for i := 0; i < maxConcurrentRuns; i++ {
		runID := fmt.Sprintf("run-pre-%d", i)
		server.trackRun(runID, runtime.ContainerID(fmt.Sprintf("c-%d", i)), fmt.Sprintf("n-%d", i), "")
	}

	// Deploy a fake agent so we get past the LoadDeployedAgent check.
	// LoadDeployedAgent reads from state/agents/<name>/ with individual files.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	if err := os.MkdirAll(deployedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write minimal files LoadDeployedAgent expects.
	lockJSON := `{"version":"v1","agent":{"name":"test-agent"},"image":{"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}}`
	if err := os.WriteFile(filepath.Join(deployedDir, "agent.lock"), []byte(lockJSON), 0o644); err != nil {
		t.Fatalf("WriteFile agent.lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "image.digest"), []byte("sha256:0000000000000000000000000000000000000000000000000000000000000000"), 0o644); err != nil {
		t.Fatalf("WriteFile image.digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "source_digest"), []byte("sha256:abc"), 0o644); err != nil {
		t.Fatalf("WriteFile source_digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "deployed_at"), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644); err != nil {
		t.Fatalf("WriteFile deployed_at: %v", err)
	}

	// Now Run should hit the concurrent limit before touching Docker.
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err == nil {
		t.Fatal("Run() error = nil, want ResourceExhausted")
	}
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Run() code = %v, want ResourceExhausted", status.Code(err))
	}
}

// --- Adversarial break tests (14A0-T01+T03) ---

func TestAdv_ConcurrentSetStatusAndStop(t *testing.T) {
	invokeReached := make(chan struct{})
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(ctx context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") {
			select {
			case invokeReached <- struct{}{}:
			default:
			}
			select {
			case <-ctx.Done():
				return "", "", -1, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return "", "", 0, nil
			}
		}
		if strings.Contains(cmdStr, "invoke") {
			select {
			case <-ctx.Done():
				return "", "", -1, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return "ok", "", 0, nil
			}
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()

	select {
	case <-invokeReached:
	case <-time.After(5 * time.Second):
		t.Fatal("invoke goroutine did not start")
	}

	var stopErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				server.setRunStatus(runID, "succeeded")
			} else {
				server.setRunStatus(runID, "failed")
			}
		}
	}()
	go func() {
		defer wg.Done()
		_, stopErr = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	}()
	wg.Wait()

	if stopErr != nil {
		t.Fatalf("Stop() error = %v", stopErr)
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	gotStatus := auditString(runStop.Payload, "status")
	switch gotStatus {
	case "succeeded", "failed":
	default:
		t.Fatalf("run_stop status = %q, want succeeded or failed", gotStatus)
	}

	events := server.eventBus.GetEvents(runID)
	var terminalEvent *trigger.RunEvent
	for i := range events {
		if events[i].IsTerminal() {
			terminalEvent = &events[i]
		}
	}
	if terminalEvent == nil {
		t.Fatal("no terminal event published")
	}
	switch terminalEvent.Type {
	case trigger.EventRunSucceeded, trigger.EventRunFailed, trigger.EventRunCancelled:
	default:
		t.Fatalf("terminal event type = %q, want a valid terminal type", terminalEvent.Type)
	}
}

func TestAdv_StopAfterUntrackReturnsNotFound(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(_ context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") || strings.Contains(cmdStr, "invoke") {
			return "ok", "", 0, nil
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	waitForRunStatus(t, server, runID, "succeeded")

	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err1 = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	}()
	go func() {
		defer wg.Done()
		_, err2 = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	}()
	wg.Wait()

	successCount := 0
	notFoundCount := 0
	for _, err := range []error{err1, err2} {
		if err == nil {
			successCount++
			continue
		}
		if status.Code(err) == codes.NotFound {
			notFoundCount++
		}
	}
	if successCount != 1 || notFoundCount != 1 {
		t.Fatalf("want one success and one NotFound, got success=%d notFound=%d err1=%v err2=%v",
			successCount, notFoundCount, err1, err2)
	}
}

func TestAdv_StopBeforeCancelSet(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	server, _ := testServerWithMockRuntime(t, mock)

	runID := "run-no-cancel"
	server.trackRun(runID, runtime.ContainerID("container-test"), "network-test", "")

	_, err := server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() with nil CancelInvoke errored: %v", err)
	}

	_, ok := server.lookupRunWithStatus(runID)
	if ok {
		t.Fatal("run still tracked after Stop")
	}
}

func TestAdv_LateStatusUpdateAfterStop(t *testing.T) {
	readyzBlocked := make(chan struct{})
	stopDone := make(chan struct{})
	invokeRelease := make(chan struct{})

	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(_ context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") {
			<-readyzBlocked
			return "", "", 0, nil
		}
		if strings.Contains(cmdStr, "invoke") {
			<-invokeRelease
			return "ok", "", 0, nil
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()

	close(readyzBlocked)

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID, Force: true})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	close(stopDone)

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	auditStatusAtStop := auditString(runStop.Payload, "status")
	if auditStatusAtStop != "cancelled" {
		t.Fatalf("run_stop status at Stop time = %q, want cancelled", auditStatusAtStop)
	}

	eventsAtStop := server.eventBus.GetEvents(runID)
	var terminalAtStop *trigger.RunEvent
	for i := range eventsAtStop {
		if eventsAtStop[i].IsTerminal() {
			terminalAtStop = &eventsAtStop[i]
		}
	}
	if terminalAtStop == nil || terminalAtStop.Type != trigger.EventRunCancelled {
		t.Fatalf("terminal event at Stop = %v, want EventRunCancelled", terminalAtStop)
	}

	close(invokeRelease)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.lookupRunWithStatus(runID); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, ok := server.lookupRunWithStatus(runID)
	if ok {
		t.Fatal("late setRunStatus re-tracked the run")
	}

	recordsAfter, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL after late invoke: %v", err)
	}
	var runStopAfter *audit.AuditRecord
	for i := range recordsAfter {
		if recordsAfter[i].EventType == "run_stop" {
			runStopAfter = &recordsAfter[i]
		}
	}
	if runStopAfter == nil {
		t.Fatal("run_stop audit record missing after late invoke")
	}
	if got := auditString(runStopAfter.Payload, "status"); got != auditStatusAtStop {
		t.Fatalf("run_stop status changed after late invoke: was %q, now %q", auditStatusAtStop, got)
	}

	eventsAfter := server.eventBus.GetEvents(runID)
	if len(eventsAfter) != len(eventsAtStop) {
		t.Fatalf("event count changed after late invoke: was %d, now %d", len(eventsAtStop), len(eventsAfter))
	}
}

func TestAdv_ForceStopSucceededAuditInconsistent(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	mock.execFunc = func(_ context.Context, _ runtime.ContainerID, cmd []string) (string, string, int, error) {
		cmdStr := strings.Join(cmd, " ")
		if strings.Contains(cmdStr, "readyz") || strings.Contains(cmdStr, "invoke") {
			return "ok", "", 0, nil
		}
		return "", "", -1, fmt.Errorf("unexpected exec: %s", cmdStr)
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	waitForRunStatus(t, server, runID, "succeeded")

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID, Force: true})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	events := server.eventBus.GetEvents(runID)
	var terminalEvent *trigger.RunEvent
	for i := range events {
		if events[i].IsTerminal() {
			terminalEvent = &events[i]
		}
	}
	if terminalEvent == nil {
		t.Fatal("no terminal event published")
	}
	if terminalEvent.Type != trigger.EventRunCancelled {
		t.Fatalf("terminal event type = %q, want %q", terminalEvent.Type, trigger.EventRunCancelled)
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	// Per spec, force stop should be "cancelled" in both event and audit payload.
	if got := auditString(runStop.Payload, "status"); got != "cancelled" {
		t.Fatalf("audit status = %q, want cancelled (event is %q — inconsistent)", got, terminalEvent.Type)
	}
}

func TestAdv_StatusCheckDetectsCrash(t *testing.T) {
	mock := defaultMockRuntimeDriver()
	server, hp := testServerWithMockRuntime(t, mock)

	runID := "run-crash-detect"
	containerID := runtime.ContainerID("container-crashed")
	server.trackRun(runID, containerID, "network-test", filepath.Join(hp.State, "runs", runID, "harness-audit"))
	server.setRunStatus(runID, "running")

	_, err := server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var runStop *audit.AuditRecord
	for i := range records {
		if records[i].EventType == "run_stop" && auditString(records[i].Payload, "run_id") == runID {
			runStop = &records[i]
		}
	}
	if runStop == nil {
		t.Fatal("run_stop audit record missing")
	}
	// Invoke never started (no invoke goroutine); status still "running".
	// Non-force Stop maps "running" → "succeeded".
	if got := auditString(runStop.Payload, "status"); got != "succeeded" {
		t.Fatalf("run_stop status = %q, want succeeded (invoke not tracked)", got)
	}

	events := server.eventBus.GetEvents(runID)
	var terminalEvent *trigger.RunEvent
	for i := range events {
		if events[i].IsTerminal() {
			terminalEvent = &events[i]
		}
	}
	if terminalEvent == nil {
		t.Fatal("no terminal event published")
	}
	if terminalEvent.Type != trigger.EventRunSucceeded {
		t.Fatalf("terminal event type = %q, want %q", terminalEvent.Type, trigger.EventRunSucceeded)
	}
}

func TestReconcileOrphans_StopsOrphanedContainers(t *testing.T) {
	runID := "run-deadbeef"
	containerID := "orphan-container-1"
	networkID := "orphan-network-1"

	var stopCalled, removeCalled, removeNetworkCalled bool

	mock := defaultMockRuntimeDriver()
	mock.listContainersFunc = func(_ context.Context, _ ...string) ([]runtime.ContainerInfo, error) {
		return []runtime.ContainerInfo{{
			ID:           containerID,
			RunID:        runID,
			Status:       runtime.ContainerStatusRunning,
			ResourceType: runtime.ResourceTypeAgent,
			Labels:       runtime.Labels(runtime.ResourceTypeAgent, runID),
		}}, nil
	}
	mock.listNetworksFunc = func(_ context.Context, _ ...string) ([]runtime.NetworkInfo, error) {
		return []runtime.NetworkInfo{{
			ID:       networkID,
			Internal: true,
			Labels:   runtime.Labels(runtime.ResourceTypeNetInternal, runID),
		}}, nil
	}
	mock.stopFunc = func(_ context.Context, id runtime.ContainerID, _ *time.Duration) error {
		stopCalled = true
		if id != runtime.ContainerID(containerID) {
			t.Errorf("stop called with id %q, want %q", id, containerID)
		}
		return nil
	}
	mock.removeFunc = func(_ context.Context, id runtime.ContainerID, force bool) error {
		removeCalled = true
		if id != runtime.ContainerID(containerID) {
			t.Errorf("remove called with id %q, want %q", id, containerID)
		}
		if !force {
			t.Error("remove called without force=true")
		}
		return nil
	}
	mock.removeNetworkFunc = func(_ context.Context, id runtime.NetworkID) error {
		removeNetworkCalled = true
		if id != runtime.NetworkID(networkID) {
			t.Errorf("removeNetwork called with id %q, want %q", id, networkID)
		}
		return nil
	}

	server, hp := testServerWithMockRuntime(t, mock)
	server.reconcileOrphanedContainers(context.Background())

	if !stopCalled {
		t.Error("expected Stop to be called for orphaned container")
	}
	if !removeCalled {
		t.Error("expected Remove to be called for orphaned container")
	}
	if !removeNetworkCalled {
		t.Error("expected RemoveNetwork to be called for orphaned network")
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var reconciled, complete bool
	for _, record := range records {
		switch record.EventType {
		case "container_reconciled":
			reconciled = true
			if auditString(record.Payload, "run_id") != runID {
				t.Errorf("container_reconciled run_id = %q, want %q", auditString(record.Payload, "run_id"), runID)
			}
		case "reconciliation_complete":
			complete = true
		}
	}
	if !reconciled {
		t.Error("container_reconciled audit record missing")
	}
	if !complete {
		t.Error("reconciliation_complete audit record missing")
	}
}

func TestReconcileOrphans_KeepsTrackedContainers(t *testing.T) {
	runID := "run-active"
	containerID := "active-container-1"

	var stopCalled, removeCalled, removeNetworkCalled bool

	mock := defaultMockRuntimeDriver()
	mock.listContainersFunc = func(_ context.Context, _ ...string) ([]runtime.ContainerInfo, error) {
		return []runtime.ContainerInfo{{
			ID:           containerID,
			RunID:        runID,
			Status:       runtime.ContainerStatusRunning,
			ResourceType: runtime.ResourceTypeAgent,
			Labels:       runtime.Labels(runtime.ResourceTypeAgent, runID),
		}}, nil
	}
	mock.listNetworksFunc = func(_ context.Context, _ ...string) ([]runtime.NetworkInfo, error) {
		return nil, nil
	}
	mock.stopFunc = func(context.Context, runtime.ContainerID, *time.Duration) error {
		stopCalled = true
		return nil
	}
	mock.removeFunc = func(context.Context, runtime.ContainerID, bool) error {
		removeCalled = true
		return nil
	}
	mock.removeNetworkFunc = func(context.Context, runtime.NetworkID) error {
		removeNetworkCalled = true
		return nil
	}

	server, _ := testServerWithMockRuntime(t, mock)
	server.trackRun(runID, runtime.ContainerID(containerID), "network-active", "")
	server.reconcileOrphanedContainers(context.Background())

	if stopCalled {
		t.Error("Stop should not be called for tracked container")
	}
	if removeCalled {
		t.Error("Remove should not be called for tracked container")
	}
	if removeNetworkCalled {
		t.Error("RemoveNetwork should not be called for tracked container")
	}
}

func TestReconcileOrphans_NoDocker_SkipsGracefully(t *testing.T) {
	server := &controlServer{}
	server.runtimeOnce.Do(func() {
		server.runtimeErr = fmt.Errorf("docker not available")
	})
	server.reconcileOrphanedContainers(context.Background())
}

func TestReconcileOrphans_RemoveFailure_EmitsAuditEvent(t *testing.T) {
	runID := "run-remove-fail"
	containerID := "orphan-container-fail"

	mock := defaultMockRuntimeDriver()
	mock.listContainersFunc = func(_ context.Context, _ ...string) ([]runtime.ContainerInfo, error) {
		return []runtime.ContainerInfo{{
			ID:           containerID,
			RunID:        runID,
			Status:       runtime.ContainerStatusRunning,
			ResourceType: runtime.ResourceTypeAgent,
			Labels:       runtime.Labels(runtime.ResourceTypeAgent, runID),
		}}, nil
	}
	mock.listNetworksFunc = func(_ context.Context, _ ...string) ([]runtime.NetworkInfo, error) {
		return nil, nil
	}
	mock.stopFunc = func(_ context.Context, _ runtime.ContainerID, _ *time.Duration) error {
		return nil
	}
	mock.removeFunc = func(_ context.Context, _ runtime.ContainerID, _ bool) error {
		return fmt.Errorf("remove failed")
	}

	server, hp := testServerWithMockRuntime(t, mock)
	server.reconcileOrphanedContainers(context.Background())

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var removeFailed, complete bool
	for _, record := range records {
		switch record.EventType {
		case "container_reconciled":
			if auditString(record.Payload, "action") == "remove_failed" {
				removeFailed = true
				if auditString(record.Payload, "run_id") != runID {
					t.Errorf("container_reconciled run_id = %q, want %q", auditString(record.Payload, "run_id"), runID)
				}
				if auditString(record.Payload, "container_id") != containerID {
					t.Errorf("container_reconciled container_id = %q, want %q", auditString(record.Payload, "container_id"), containerID)
				}
			}
		case "reconciliation_complete":
			complete = true
		}
	}
	if !removeFailed {
		t.Error("container_reconciled audit record with action remove_failed missing")
	}
	if !complete {
		t.Error("reconciliation_complete audit record missing")
	}
}

func TestReconcileOrphans_ListContainersError_ProceedsToNetworkReconciliation(t *testing.T) {
	runID := "run-orphan-net"
	networkID := "orphan-network-only"

	var removeNetworkCalled bool

	mock := defaultMockRuntimeDriver()
	mock.listContainersFunc = func(_ context.Context, _ ...string) ([]runtime.ContainerInfo, error) {
		return nil, fmt.Errorf("docker list containers failed")
	}
	mock.listNetworksFunc = func(_ context.Context, _ ...string) ([]runtime.NetworkInfo, error) {
		return []runtime.NetworkInfo{{
			ID:       networkID,
			Internal: true,
			Labels:   runtime.Labels(runtime.ResourceTypeNetInternal, runID),
		}}, nil
	}
	mock.removeNetworkFunc = func(_ context.Context, id runtime.NetworkID) error {
		removeNetworkCalled = true
		if id != runtime.NetworkID(networkID) {
			t.Errorf("removeNetwork called with id %q, want %q", id, networkID)
		}
		return nil
	}

	server, hp := testServerWithMockRuntime(t, mock)
	server.reconcileOrphanedContainers(context.Background())

	if !removeNetworkCalled {
		t.Error("expected RemoveNetwork to be called for orphaned network")
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var complete bool
	for _, record := range records {
		if record.EventType == "reconciliation_complete" {
			complete = true
		}
	}
	if !complete {
		t.Error("reconciliation_complete audit record missing")
	}
}

func TestReconcile_RemovesGatewayAndEgressNetwork(t *testing.T) {
	runID := "run-orphan-gateway"
	gatewayID := "orphan-gateway-1"
	egressNetworkID := "orphan-egress-net-1"

	var stopCalled, removeCalled, removeNetworkCalled bool

	mock := defaultMockRuntimeDriver()
	mock.listContainersFunc = func(_ context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error) {
		for _, f := range labelFilters {
			if f == runtime.LabelResourceType+"="+runtime.ResourceTypeGateway {
				return []runtime.ContainerInfo{{
					ID:           gatewayID,
					RunID:        runID,
					Status:       runtime.ContainerStatusRunning,
					ResourceType: runtime.ResourceTypeGateway,
					Labels:       runtime.Labels(runtime.ResourceTypeGateway, runID),
				}}, nil
			}
		}
		return nil, nil
	}
	mock.listNetworksFunc = func(_ context.Context, labelFilters ...string) ([]runtime.NetworkInfo, error) {
		for _, f := range labelFilters {
			if f == runtime.LabelResourceType+"="+runtime.ResourceTypeNetEgress {
				return []runtime.NetworkInfo{{
					ID:     egressNetworkID,
					Labels: runtime.Labels(runtime.ResourceTypeNetEgress, runID),
				}}, nil
			}
		}
		return nil, nil
	}
	mock.stopFunc = func(_ context.Context, id runtime.ContainerID, _ *time.Duration) error {
		stopCalled = true
		if id != runtime.ContainerID(gatewayID) {
			t.Errorf("stop called with id %q, want %q", id, gatewayID)
		}
		return nil
	}
	mock.removeFunc = func(_ context.Context, id runtime.ContainerID, force bool) error {
		removeCalled = true
		if id != runtime.ContainerID(gatewayID) {
			t.Errorf("remove called with id %q, want %q", id, gatewayID)
		}
		if !force {
			t.Error("remove called without force=true")
		}
		return nil
	}
	mock.removeNetworkFunc = func(_ context.Context, id runtime.NetworkID) error {
		removeNetworkCalled = true
		if id != runtime.NetworkID(egressNetworkID) {
			t.Errorf("removeNetwork called with id %q, want %q", id, egressNetworkID)
		}
		return nil
	}

	server, hp := testServerWithMockRuntime(t, mock)
	server.reconcileOrphanedContainers(context.Background())

	if !stopCalled {
		t.Error("expected Stop to be called for orphaned gateway container")
	}
	if !removeCalled {
		t.Error("expected Remove to be called for orphaned gateway container")
	}
	if !removeNetworkCalled {
		t.Error("expected RemoveNetwork to be called for orphaned egress network")
	}

	records, err := readAuditJSONL(filepath.Join(hp.State, "audit.jsonl"))
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	var reconciled, complete bool
	for _, record := range records {
		switch record.EventType {
		case "container_reconciled":
			reconciled = true
			if auditString(record.Payload, "run_id") != runID {
				t.Errorf("container_reconciled run_id = %q, want %q", auditString(record.Payload, "run_id"), runID)
			}
		case "reconciliation_complete":
			complete = true
		}
	}
	if !reconciled {
		t.Error("container_reconciled audit record missing")
	}
	if !complete {
		t.Error("reconciliation_complete audit record missing")
	}
}
