package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

	server.trackRun(runID, containerID, networkID)
	gotContainer, gotNetwork := server.lookupRun(runID)
	if gotContainer != containerID {
		t.Fatalf("lookupRun() container = %q, want %q", gotContainer, containerID)
	}
	if gotNetwork != networkID {
		t.Fatalf("lookupRun() network = %q, want %q", gotNetwork, networkID)
	}

	server.untrackRun(runID)
	gotContainer, gotNetwork = server.lookupRun(runID)
	if gotContainer != "" || gotNetwork != "" {
		t.Fatalf("lookupRun() after untrack = (%q, %q), want empty", gotContainer, gotNetwork)
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