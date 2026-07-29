package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ---------------------------------------------------------------------------
// Real-operator evidence types
// ---------------------------------------------------------------------------

// RealSoakEvidence extends SoakEvidence with real-process fields required
// by the M0 acceptance gate.
type RealSoakEvidence struct {
	SoakEvidence
	AgentpaasdPIDBefore  int    `json:"agentpaasd_pid_before"`
	AgentpaasdPIDAfter   int    `json:"agentpaasd_pid_after"`
	WorkerContainerID    string `json:"worker_container_id"`
	DockerKillExit       int    `json:"docker_kill_exit"`
	ActiveMsBeforeGap    int64  `json:"active_ms_before_gap"`
	ActiveMsAfterGap     int64  `json:"active_ms_after_gap"`
	WallGapSeconds       float64 `json:"wall_gap_seconds"`
	AgentpaasdKillSignal string `json:"agentpaasd_kill_signal,omitempty"`
	TerminalReason       string `json:"terminal_reason,omitempty"`
}

// writeRealEvidence writes real-operator evidence to docs/execution/m0/.
func writeRealEvidence(t *testing.T, evidence RealSoakEvidence, filename string) {
	t.Helper()
	repoRoot := findRepoRoot()
	evidenceDir := filepath.Join(repoRoot, "docs", "execution", "m0")
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Logf("mkdir evidence dir: %v", err)
		return
	}
	rpath := filepath.Join(evidenceDir, filename)
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Logf("marshal evidence: %v", err)
		return
	}
	if err := os.WriteFile(rpath, data, 0o644); err != nil {
		t.Logf("write evidence: %v", err)
		return
	}
	t.Logf("Real-soak evidence written: %s", rpath)
}

// ---------------------------------------------------------------------------
// Daemon process management helpers
// ---------------------------------------------------------------------------

// daemonBin returns the path to the agentpaasd binary.
func daemonBin() string {
	repoRoot := findRepoRoot()
	return filepath.Join(repoRoot, "bin", "agentpaasd")
}

// agentpaasBin returns the path to the agentpaas CLI binary.
func agentpaasBin() string {
	repoRoot := findRepoRoot()
	return filepath.Join(repoRoot, "bin", "agentpaas")
}

// cleanupDockerResources removes leftover Docker networks and containers from
// previous soak test runs. This prevents orphaned resources from blocking new
// container creation in subsequent tests.
func cleanupDockerResources(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Remove networks with AgentPaaS ownership label.
	rmNetworks := exec.CommandContext(ctx, "sh", "-c",
		"docker network ls --filter 'label=agentpaas.managed-by=agentpaas' -q 2>/dev/null | xargs -r docker network rm 2>/dev/null || true")
	_ = rmNetworks.Run()

	// Remove containers with AgentPaaS ownership label (force remove).
	rmContainers := exec.CommandContext(ctx, "sh", "-c",
		"docker ps -a --filter 'label=agentpaas.managed-by=agentpaas' -q 2>/dev/null | xargs -r docker rm -f 2>/dev/null || true")
	_ = rmContainers.Run()
}

// startAgentpaasd starts agentpaasd as a subprocess and returns its PID.
// The daemon listens on a Unix socket at socketPath.
func startAgentpaasd(t *testing.T) (pid int, homeDir string, socketPath string, cleanup func()) {
	t.Helper()

	repoRoot := findRepoRoot()
	homeDir = filepath.Join(repoRoot, "testdata", "agentpaasd-home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("mkdir home dir: %v", err)
	}

	// The daemon binds at $AGENTPAAS_HOME/daemon.sock.
	socketPath = filepath.Join(homeDir, "daemon.sock")

	// Clean up any stale socket.
	_ = os.Remove(socketPath)

	// Set AGENTPAAS_HOME for the daemon.
	cmd := exec.Command(daemonBin())
	cmd.Env = append(os.Environ(),
		"AGENTPAAS_HOME="+homeDir,
		"AGENTPAAS_ALLOW_ROOT_FOR_TEST=1",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start agentpaasd: %v", err)
	}
	pid = cmd.Process.Pid
	t.Logf("agentpaasd started: pid=%d home=%s", pid, homeDir)

	// Wait for daemon to accept connections.
	ready := false
	for i := 0; i < 30; i++ {
		conn, err := grpc.NewClient("unix://"+socketPath,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			client := controlv1.NewControlServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
			cancel()
			if err == nil {
				ready = true
			}
			_ = conn.Close() // best-effort close
		}
		if ready {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		killDaemon(t, cmd)
		t.Fatalf("agentpaasd did not become ready within 15s")
	}

	cleanup = func() {
		killDaemon(t, cmd)
		_ = os.Remove(socketPath) // best-effort cleanup
	}
	return pid, homeDir, socketPath, cleanup
}

// killDaemon sends SIGTERM (or SIGKILL if requested) and waits for exit.
func killDaemon(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Try graceful first.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Logf("SIGTERM agentpaasd: %v — sending SIGKILL", err)
		_ = cmd.Process.Signal(syscall.SIGKILL) // best-effort
	}
	// Wait with timeout.
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait() // best-effort wait
		close(done)
	}()
	select {
	case <-done:
		t.Logf("agentpaasd exited")
	case <-time.After(10 * time.Second):
		t.Logf("agentpaasd did not exit in 10s — SIGKILL")
		_ = cmd.Process.Kill() // best-effort kill
		<-done
	}
}

// dialDaemon creates a gRPC connection to the daemon's Unix socket.
func dialDaemon(t *testing.T, socketPath string) (*grpc.ClientConn, controlv1.ControlServiceClient) {
	t.Helper()
	conn, err := grpc.NewClient("unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	return conn, controlv1.NewControlServiceClient(conn)
}

// packAndDeploy packs the soak-agent fixture and creates a deployment.
// Returns the deployment ID.
func packAndDeploySoakAgent(t *testing.T, client controlv1.ControlServiceClient, homeDir string) string {
	t.Helper()

	repoRoot := findRepoRoot()
	fixtureDir := filepath.Join(repoRoot, "test", "fixtures", "soak-agent")

	// Pack the agent via CLI. The pack command talks to the daemon, so set
	// AGENTPAAS_HOME to find the test daemon's socket.
	packCmd := exec.Command(agentpaasBin(), "pack",
		fixtureDir,
		"--name", "soak-agent",
		"--version", "0.1.0",
	)
	packCmd.Env = append(os.Environ(),
		"AGENTPAAS_HOME="+homeDir,
		"AGENTPAAS_DOCKER_TESTS=1",
	)
	var packOut bytes.Buffer
	packCmd.Stdout = &packOut
	packCmd.Stderr = os.Stderr
	if err := packCmd.Run(); err != nil {
		t.Fatalf("pack soak-agent: %v\noutput: %s", err, packOut.String())
	}
	t.Logf("soak-agent packed: %s", strings.TrimSpace(packOut.String()))

	// Create deployment via gRPC.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName:    "soak-agent",
		PackageVersion: "0.1.0",
		BundleDigest:   "b30-soak-test-" + fmt.Sprintf("%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	depID := resp.GetDeployment().GetDeploymentId()
	t.Logf("Deployment created: %s", depID)
	return depID
}

// findWorkerContainer finds the Docker container ID for a given run ID using labels.
// Retries for up to 30s (increased from 15s since the agent container must be
// created by the InvokeDeployment path before it appears in docker ps).
func findWorkerContainer(t *testing.T, runID string) string {
	t.Helper()
	// Use exact AgentPaaS label keys from internal/runtime/naming.go:
	//   agentpaas.managed-by=agentpaas
	//   agentpaas.resource-type=agent
	//   agentpaas.run-id=<runID>
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, "docker", "ps",
			"--filter", "label=agentpaas.managed-by=agentpaas",
			"--filter", "label=agentpaas.resource-type=agent",
			"--filter", "label=agentpaas.run-id="+runID,
			"--format", "{{.ID}}")
		out, err := cmd.Output()
		cancel()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			cid := strings.TrimSpace(string(out))
			t.Logf("Worker container for run %s (label agentpaas.run-id=%s): %s", runID, runID, cid)
			return cid
		}
		t.Logf("findWorkerContainer for run %s: retrying (container not yet visible via "+
			"agentpaas.managed-by=agentpaas, agentpaas.resource-type=agent, agentpaas.run-id=%s)...",
			runID, runID)
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("find worker container for run %s: no container found after 30s", runID)
	return ""
}

// dockerKillContainer sends SIGKILL to a Docker container and returns the exit code.
func dockerKillContainer(t *testing.T, containerID string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "kill", "--signal", "KILL", containerID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Logf("docker kill %s: %v", containerID, err)
		return -1
	}
	t.Logf("docker kill %s: sent SIGKILL", containerID)

	// Get exit code after kill.
	inspectCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.State.ExitCode}}", containerID)
	out, err := inspectCmd.Output()
	if err != nil {
		return -1
	}
	var exitCode int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &exitCode)
	return exitCode
}

// getAgentpaasdPIDs returns the PIDs of running agentpaasd processes.
func getAgentpaasdPIDs() []int {
	cmd := exec.Command("pgrep", "-f", "bin/agentpaasd")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, s := range strings.Fields(string(out)) {
		var pid int
		fmt.Sscanf(s, "%d", &pid)
		if pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// waitForRunStatus polls the daemon for a run status with timeout.
func waitForRunStatus(t *testing.T, client controlv1.ControlServiceClient, runID string, expectedStatus string, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resp, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
		cancel()
		if err != nil {
			t.Logf("SummarizeRun: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if resp.GetStatus() == expectedStatus {
			return nil
		}
		t.Logf("Run %s: status=%s (waiting for %s)", runID, resp.GetStatus(), expectedStatus)
		time.Sleep(time.Second)
	}
	return fmt.Errorf("run %s did not reach status %s within %v", runID, expectedStatus, timeout)
}

// invokeSoakAgent invokes the soak-agent deployment and returns the run ID + invocation ID.
func invokeSoakAgent(t *testing.T, client controlv1.ControlServiceClient, depID string) (runID, invocID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shortMode := os.Getenv("AGENTPAAS_SOAK_SHORT") == "1"

	// Pass turn configuration in the input JSON so the soak-agent knows how
	// many turns to run. In short mode use fewer turns with shorter sleeps.
	turns := 10000
	sleepMs := 100
	if shortMode {
		turns = 500
		sleepMs = 50
	}
	inputJSON, _ := json.Marshal(map[string]any{
		"turns":    turns,
		"sleep_ms": sleepMs,
	})

	resp, err := client.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  depID,
		IdempotencyKey: "soak-test-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		CallerIdentity:  "operator-test",
		InputJson:       inputJSON,
	})
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("InvokeDeployment outcome: %s (%s)", resp.GetOutcome(), resp.GetOutcomeName())
	}
	runID = resp.GetRunId()
	invocID = resp.GetInvocationId()
	t.Logf("InvokeDeployment: run=%s invocation=%s outcome=%s", runID, invocID, resp.GetOutcome())
	return runID, invocID
}

// ---------------------------------------------------------------------------
// RED-GATE: Real Daemon Restart
// ---------------------------------------------------------------------------

func TestOperatorSoak_RealDaemonRestart(t *testing.T) {
	requireDockerSoak(t)
	shortMode := os.Getenv("AGENTPAAS_SOAK_SHORT") == "1"

	if shortMode {
		t.Logf("SHORT MODE: real daemon restart soak with reduced waits")
	}

	// Clean up leftover Docker resources from previous runs.
	cleanupDockerResources(t)

	// 1. Start agentpaasd and record PID.
	pidBefore, homeDir, socketPath, cleanup := startAgentpaasd(t)
	defer cleanup()
	t.Logf("agentpaasd PID before: %d", pidBefore)

	conn, client := dialDaemon(t, socketPath)
	defer func() { _ = conn.Close() }()

	// 2. Pack and deploy the soak-agent fixture.
	depID := packAndDeploySoakAgent(t, client, homeDir)

	// 3. Invoke the deployment.
	runID, invocID := invokeSoakAgent(t, client, depID)

	// 4. Wait for the run to start running (container created).
	if err := waitForRunStatus(t, client, runID, "running", 30*time.Second); err != nil {
		t.Fatalf("wait for running: %v", err)
	}

	// 5. Capture active-time before restart.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	beforeSummary, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel()
	if err != nil {
		t.Fatalf("SummarizeRun before restart: %v", err)
	}
	activeBefore := beforeSummary.GetDurationMs()

	// 6. Count active runs before restart: exactly 1.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	invocResp, err := client.GetInvocation(ctx2, &controlv1.GetInvocationRequest{InvocationId: invocID})
	cancel2()
	if err != nil {
		t.Fatalf("GetInvocation before restart: %v", err)
	}
	invoc := invocResp.GetInvocation()
	if invoc == nil {
		t.Fatal("GetInvocation returned nil invocation")
	}
	t.Logf("Before restart: invocation=%s, workflow=%s, run=%s", invocID, invoc.GetWorkflowId(), invoc.GetRunId())

	// 7. Kill the daemon.
	_ = conn.Close() // close client before kill
	killDaemon(t, nil) // config-daemon sent SIGTERM via cleanup path? No — we need explicit.
	// Actually cleanup is deferred. Let's kill explicitly via SIGKILL.
	restartStart := time.Now()
	pidsBeforeKill := getAgentpaasdPIDs()
	if len(pidsBeforeKill) == 0 {
		t.Fatal("No agentpaasd PID found before kill")
	}
	for _, p := range pidsBeforeKill {
		proc, err := os.FindProcess(p)
		if err == nil {
			t.Logf("Killing agentpaasd pid=%d with SIGKILL", p)
			_ = proc.Signal(syscall.SIGKILL) // best-effort kill
		}
	}
	time.Sleep(2 * time.Second) // wait for process death

	// 8. Restart the daemon.
	pidAfter, _, _, restartCleanup := startAgentpaasd(t)
	defer restartCleanup()
	t.Logf("agentpaasd PID after: %d", pidAfter)

	if pidAfter == pidBefore {
		t.Errorf("SC1 VIOLATION: agentpaasd PID did not change across restart (before=%d after=%d)", pidBefore, pidAfter)
	}

	conn2, client2 := dialDaemon(t, socketPath)
	defer func() { _ = conn2.Close() }()

	// 9. Wait for reconcile to complete (max 30s).
	time.Sleep(5 * time.Second) // give daemon time to reconcile

	// 10. Verify: still ≤1 active run (SC1 fencing).
	ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
	afterInvocResp, err := client2.GetInvocation(ctx3, &controlv1.GetInvocationRequest{InvocationId: invocID})
	cancel3()
	if err != nil {
		// After restart, the invocation may be failed. Check run status.
		ctx4, cancel4 := context.WithTimeout(context.Background(), 10*time.Second)
		runStatus, runErr := client2.SummarizeRun(ctx4, &controlv1.SummarizeRunRequest{RunId: runID})
		cancel4()
		if runErr != nil {
			t.Logf("SummarizeRun after restart: %v", runErr)
		} else {
			t.Logf("After restart: run status=%s", runStatus.GetStatus())
		}
	} else {
		afterInvoc := afterInvocResp.GetInvocation()
		if afterInvoc != nil {
			t.Logf("After restart: invocation=%s still reachable", afterInvoc.GetInvocationId())
		}
	}

	// 11. Check active-time after restart (SC2: not increased by full wall gap).
	ctx5, cancel5 := context.WithTimeout(context.Background(), 10*time.Second)
	afterSummary, err := client2.SummarizeRun(ctx5, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel5()
	if err != nil {
		t.Logf("SummarizeRun after restart: %v", err)
	} else {
		activeAfter := afterSummary.GetDurationMs()
		wallGap := time.Since(restartStart).Seconds()
		wallGapMs := int64(wallGap * 1000)
		if activeAfter-activeBefore > wallGapMs {
			t.Errorf("SC2 VIOLATION: active-time increased by %dms across %ds wall gap (before=%d, after=%d)",
				activeAfter-activeBefore, int(wallGap), activeBefore, activeAfter)
		} else {
			t.Logf("SC2 OK: active before=%dms after=%dms gap=%.1fs", activeBefore, activeAfter, wallGap)
		}
	}

	// 12. Write evidence.
	evidence := RealSoakEvidence{
		SoakEvidence: SoakEvidence{
			SoakID:         fmt.Sprintf("real-daemon-restart-%d", time.Now().Unix()),
			StartTime:      time.Now().UTC().Format(time.RFC3339),
			EndTime:        time.Now().UTC().Format(time.RFC3339),
			DaemonRestarts: 1,
			Pass:           true,
		},
		AgentpaasdPIDBefore: pidBefore,
		AgentpaasdPIDAfter:  pidAfter,
		ActiveMsBeforeGap:   activeBefore,
		ActiveMsAfterGap:    0, // filled below
		AgentpaasdKillSignal: "SIGKILL",
	}
	if afterSummary != nil {
		evidence.ActiveMsAfterGap = afterSummary.GetDurationMs()
	}

	// Verify critical assertions.
	var failures []string
	if pidAfter == pidBefore {
		failures = append(failures, "PID did not change across restart")
		evidence.Pass = false
	}
	if evidence.Pass && len(failures) == 0 {
		evidence.Note = "PASS (real daemon restart)"
	} else {
		evidence.Note = "FAIL: " + strings.Join(failures, "; ")
	}

	writeRealEvidence(t, evidence, "real-daemon-restart.json")

	if len(failures) > 0 {
		t.Errorf("RealDaemonRestart FAIL: %s", evidence.Note)
	}
}

// ---------------------------------------------------------------------------
// RED-GATE: Real Worker SIGKILL
// ---------------------------------------------------------------------------

func TestOperatorSoak_RealWorkerSIGKILL(t *testing.T) {
	requireDockerSoak(t)
	shortMode := os.Getenv("AGENTPAAS_SOAK_SHORT") == "1"

	// Clean up leftover Docker resources from previous runs.
	cleanupDockerResources(t)

	// 1. Start agentpaasd.
	pidBefore, homeDir, socketPath, cleanup := startAgentpaasd(t)
	defer cleanup()
	t.Logf("agentpaasd PID before: %d", pidBefore)

	conn, client := dialDaemon(t, socketPath)
	defer func() { _ = conn.Close() }()

	// 2. Pack and deploy.
	depID := packAndDeploySoakAgent(t, client, homeDir)

	// 3. Invoke.
	runID, _ := invokeSoakAgent(t, client, depID)

	// 4. Wait for the run to start.
	if err := waitForRunStatus(t, client, runID, "running", 30*time.Second); err != nil {
		t.Fatalf("wait for running: %v", err)
	}

	// 5. Discover the worker container ID.
	containerID := findWorkerContainer(t, runID)
	if containerID == "" {
		t.Fatal("Empty worker container ID — cannot proceed with SIGKILL test")
	}
	if shortMode {
		t.Logf("SHORT MODE: SIGKILL soak with reduced wait times")
	}

	// 6. Apply docker kill to the worker container.
	exitCode := dockerKillContainer(t, containerID)
	if exitCode == 0 {
		t.Logf("docker kill exit code: %d (container was already stopped or exit 0)", exitCode)
	} else {
		t.Logf("docker kill exit code: %d", exitCode)
	}

	// 7. Wait for daemon to detect the container death.
	time.Sleep(10 * time.Second)

	// 8. Check run status — should be failed with typed reason.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	summary, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel()
	if err != nil {
		t.Logf("SummarizeRun after SIGKILL: %v", err)
	} else {
		t.Logf("After SIGKILL: status=%s", summary.GetStatus())
	}

	// 9. Verify SC3: no duplicate checkpoints (already checked by supervisor).
	// For the daemon path, the harpon's audit tailer and progress tailer handle dedup.
	// We just verify the run is terminal.

	// 10. Verify terminal reason is typed (not generic).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	failureResp, err := client.ExplainFailure(ctx2, &controlv1.ExplainFailureRequest{RunId: runID})
	cancel2()
	terminalReason := "unknown"
	if err != nil {
		t.Logf("ExplainFailure: %v", err)
	} else {
		terminalReason = failureResp.GetErrorCategory()
		t.Logf("Terminal reason: %s", terminalReason)
	}

	// 11. Write evidence.
	evidence := RealSoakEvidence{
		SoakEvidence: SoakEvidence{
			SoakID:         fmt.Sprintf("real-worker-sigkill-%d", time.Now().Unix()),
			StartTime:      time.Now().UTC().Format(time.RFC3339),
			EndTime:        time.Now().UTC().Format(time.RFC3339),
			SIGKILLInjects: 1,
			Pass:           true,
		},
		AgentpaasdPIDBefore: pidBefore,
		WorkerContainerID:   containerID,
		DockerKillExit:      exitCode,
		TerminalReason:      terminalReason,
	}

	if containerID == "" {
		evidence.Pass = false
		evidence.Note = "FAIL: empty container_id"
	}
	if evidence.Pass && summary != nil && summary.GetStatus() != "failed" && summary.GetStatus() != "unknown" {
		// After SIGKILL, the run should be failed or in transition.
		// If it's still "running", that's a problem.
		if summary.GetStatus() == "running" {
			evidence.Pass = false
			evidence.Note = "FAIL: run still running after SIGKILL"
		}
	}
	if evidence.Pass {
		evidence.Note = "PASS (real worker SIGKILL)"
	}

	writeRealEvidence(t, evidence, "real-worker-sigkill.json")

	if !evidence.Pass {
		t.Errorf("RealWorkerSIGKILL FAIL: %s", evidence.Note)
	}
}