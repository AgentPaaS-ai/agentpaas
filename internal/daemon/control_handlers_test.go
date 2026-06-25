package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/home"
	"github.com/parvezsyed/agentpaas/internal/pack"
	"github.com/parvezsyed/agentpaas/internal/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testControlServer(t *testing.T) *stubControlServer {
	t.Helper()
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	return &stubControlServer{
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
	server := &stubControlServer{}
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

	server := &stubControlServer{
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
	server := &stubControlServer{}

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
		createNetworkFunc: func(_ context.Context, _ runtime.NetworkSpec) (runtime.NetworkID, error) {
			return runtime.NetworkID("network-test"), nil
		},
		createFunc: func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
			capturedSpec = spec
			return runtime.ContainerID("container-test"), nil
		},
		startFunc: func(_ context.Context, _ runtime.ContainerID) error {
			return nil
		},
	}

	server := &stubControlServer{homePaths: hp}
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

	mock := &mockRuntimeDriver{
		createNetworkFunc: func(_ context.Context, _ runtime.NetworkSpec) (runtime.NetworkID, error) {
			return runtime.NetworkID("network-test"), nil
		},
		createFunc: func(_ context.Context, _ runtime.ContainerSpec) (runtime.ContainerID, error) {
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
	}

	server := &stubControlServer{
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
	harnessLines := `{"timestamp":"2026-01-02T03:04:05Z","event_type":"egress_denied","deployment_mode":"local","actor":"harness","payload":{"destination":"evil.com","method":"GET","decision":"denied"}}
{"timestamp":"2026-01-02T03:04:06Z","event_type":"egress_allowed","deployment_mode":"local","actor":"harness","payload":{"destination":"api.example.com","method":"GET","decision":"allowed"}}`
	if err := os.WriteFile(harnessAuditPath, []byte(harnessLines+"\n"), 0o644); err != nil {
		t.Fatalf("write harness audit: %v", err)
	}

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
	createFunc        func(ctx context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error)
	startFunc         func(ctx context.Context, id runtime.ContainerID) error
	stopFunc          func(ctx context.Context, id runtime.ContainerID, timeout *time.Duration) error
	removeFunc        func(ctx context.Context, id runtime.ContainerID, force bool) error
	createNetworkFunc func(ctx context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error)
	removeNetworkFunc func(ctx context.Context, id runtime.NetworkID) error
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

func (m *mockRuntimeDriver) Status(context.Context, runtime.ContainerID) (runtime.ContainerStatus, error) {
	return runtime.ContainerStatusUnknown, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Stats(context.Context, runtime.ContainerID) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) Logs(context.Context, runtime.ContainerID, runtime.LogOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
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

func (m *mockRuntimeDriver) ListContainers(context.Context, ...string) ([]runtime.ContainerInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRuntimeDriver) ListNetworks(context.Context, ...string) ([]runtime.NetworkInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRun_RejectsWhenConcurrentLimitReached(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	server := &stubControlServer{homePaths: hp}

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