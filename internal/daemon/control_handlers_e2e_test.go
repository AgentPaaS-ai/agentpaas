package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

const weatherPolicyYAML = `version: "1"
agent:
  name: governed-weather
  description: ""
egress:
  - domain: "api.weather.gov"
    ports: [443]
credentials: []
mcp_servers: []
hooks: []
ingress: []
`

func TestE2E_PolicyEnforcement_AllowedAndDenied(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	homeDir := filepath.Join(os.Getenv("HOME"), "agentpaas-e2e-policy-"+fmt.Sprint(time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })

	hp := home.NewHomePaths(homeDir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	rt, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime: %v", err)
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
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  indexer,
	}
	server.runtimeOnce.Do(func() {})
	server.dockerRT = rt

	repoRoot := e2eRepoRoot(t)
	ensureHarnessLinux(t, repoRoot)
	t.Setenv("PATH", filepath.Join(repoRoot, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

	projectDir := prepareGovernedWeatherProject(t, repoRoot, homeDir)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	_, err = server.PolicyApply(ctx, &controlv1.PolicyApplyRequest{
		PolicyYaml: weatherPolicyYAML,
	})
	if err != nil {
		t.Fatalf("PolicyApply() error = %v", err)
	}

	_, err = server.Pack(ctx, &controlv1.PackRequest{
		AgentProjectPath: projectDir,
	})
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}

	const agentName = "governed-weather"
	runResp, err := server.Run(ctx, &controlv1.RunRequest{AgentName: agentName})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := runResp.GetRunId()
	if runID == "" {
		t.Fatal("Run() returned empty run_id")
	}
	t.Cleanup(func() {
		_, _ = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	})

	waitForInvokeComplete(t, server, runID, 2*time.Minute)

	_, err = server.Stop(ctx, &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	records, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("audit.jsonl is empty after stop")
	}
	assertAuditHashChain(t, records)

	var hasEgressAllowed, hasEgressDenied bool
	var allowedWeather, deniedExfil bool
	for _, record := range records {
		if auditString(record.Payload, "run_id") != runID {
			continue
		}
		dest := auditString(record.Payload, "destination")
		switch record.EventType {
		case "egress_allowed":
			hasEgressAllowed = true
			if strings.Contains(dest, "api.weather.gov") {
				allowedWeather = true
			}
		case "egress_denied":
			hasEgressDenied = true
			if strings.Contains(dest, "evil-exfil.example.com") {
				deniedExfil = true
			}
		}
	}
	if !hasEgressAllowed {
		t.Error("expected egress_allowed audit event for policy-permitted call")
	}
	if !allowedWeather {
		t.Error("expected egress_allowed event with destination api.weather.gov")
	}
	if !hasEgressDenied {
		t.Error("expected egress_denied audit event for policy-denied call")
	}
	if !deniedExfil {
		t.Error("expected egress_denied event with destination evil-exfil.example.com")
	}
}

func TestE2E_PackRunInvokeStopAudit(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	homeDir := filepath.Join(os.Getenv("HOME"), "agentpaas-e2e-"+fmt.Sprint(time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })

	hp := home.NewHomePaths(homeDir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	rt, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime: %v", err)
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
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  indexer,
	}
	server.runtimeOnce.Do(func() {})
	server.dockerRT = rt

	repoRoot := e2eRepoRoot(t)
	ensureHarnessLinux(t, repoRoot)
	t.Setenv("PATH", filepath.Join(repoRoot, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

	projectDir := prepareGovernedWeatherProject(t, repoRoot, homeDir)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	_, err = server.Pack(ctx, &controlv1.PackRequest{
		AgentProjectPath: projectDir,
	})
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}

	const agentName = "governed-weather"
	runResp, err := server.Run(ctx, &controlv1.RunRequest{AgentName: agentName})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := runResp.GetRunId()
	if runID == "" {
		t.Fatal("Run() returned empty run_id")
	}
	t.Cleanup(func() {
		_, _ = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	})

	waitForInvokeComplete(t, server, runID, 2*time.Minute)

	_, err = server.Stop(ctx, &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	queryResp, err := server.AuditQuery(ctx, &controlv1.AuditQueryRequest{
		RunId:    runID,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("AuditQuery() error = %v", err)
	}

	records, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("audit.jsonl is empty after stop")
	}
	assertAuditHashChain(t, records)

	var hasEgressDenied bool
	var hasExpectedDestination bool
	for _, record := range records {
		if auditString(record.Payload, "run_id") != runID {
			continue
		}
		if record.EventType != "egress_denied" {
			continue
		}
		hasEgressDenied = true
		dest := auditString(record.Payload, "destination")
		if strings.Contains(dest, "api.weather.gov") || strings.Contains(dest, "evil-exfil.example.com") {
			hasExpectedDestination = true
		}
	}
	if !hasEgressDenied {
		t.Error("expected at least one egress_denied audit event for run")
	}
	if !hasExpectedDestination {
		t.Error("expected egress_denied event with destination api.weather.gov or evil-exfil.example.com")
	}

	var queryHasDestination bool
	for _, entry := range queryResp.GetEntries() {
		if entry.GetRunId() != runID {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(entry.GetPayload(), &payload); err != nil {
			continue
		}
		dest := auditString(payload, "destination")
		if strings.Contains(dest, "api.weather.gov") || strings.Contains(dest, "evil-exfil.example.com") {
			queryHasDestination = true
			break
		}
	}
	if !queryHasDestination {
		t.Error("AuditQuery missing egress_denied destination for run")
	}
}

func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

func ensureHarnessLinux(t *testing.T, repoRoot string) {
	t.Helper()
	harnessPath := filepath.Join(repoRoot, "bin", "agentpaas-harness-linux")
	if _, err := os.Stat(harnessPath); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(harnessPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", harnessPath, "./cmd/harness")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v\n%s", err, out)
	}
}

func prepareGovernedWeatherProject(t *testing.T, repoRoot, homeDir string) string {
	t.Helper()
	srcProject := filepath.Join(repoRoot, "demo", "governed-weather")
	projectDir := filepath.Join(homeDir, "governed-weather-project")
	if err := copyDirRecursive(srcProject, projectDir); err != nil {
		t.Fatalf("copy project: %v", err)
	}

	sdkSrc := filepath.Join(repoRoot, "python", "agentpaas_sdk")
	sdkDst := filepath.Join(projectDir, "python", "agentpaas_sdk")
	if err := os.MkdirAll(filepath.Dir(sdkDst), 0o755); err != nil {
		t.Fatalf("MkdirAll python: %v", err)
	}
	if err := copyDirRecursive(sdkSrc, sdkDst); err != nil {
		t.Fatalf("copy SDK: %v", err)
	}
	return projectDir
}

func copyDirRecursive(src, dst string) error {
	cmd := exec.Command("cp", "-R", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp -R %s %s: %w\n%s", src, dst, err, out)
	}
	return nil
}

func waitForInvokeComplete(t *testing.T, server *controlServer, runID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tracked, ok := server.lookupRunWithStatus(runID)
		if ok && tracked.Status != "running" {
			if tracked.Status == "failed" && tracked.InvokeErr != nil {
				t.Logf("invoke finished with status=%s err=%v", tracked.Status, tracked.InvokeErr)
			}
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("invoke did not complete within %s for run %q", timeout, runID)
}

func assertAuditHashChain(t *testing.T, records []audit.AuditRecord) {
	t.Helper()
	for i, record := range records {
		if i == 0 {
			if record.PrevHash != "" {
				t.Fatalf("record[0].PrevHash = %q, want empty genesis", record.PrevHash)
			}
			if record.Seq != 1 {
				t.Fatalf("record[0].Seq = %d, want 1", record.Seq)
			}
			continue
		}
		if record.PrevHash != records[i-1].RecordHash {
			t.Fatalf("record[%d].PrevHash = %q, want %q", i, record.PrevHash, records[i-1].RecordHash)
		}
		if record.Seq != records[i-1].Seq+1 {
			t.Fatalf("record[%d].Seq = %d, want %d", i, record.Seq, records[i-1].Seq+1)
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
}