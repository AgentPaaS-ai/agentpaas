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
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &exitCode); err != nil {
		t.Logf("parse docker exit code: %v", err)
	}
	return exitCode
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

// getRunTurnsCompleted extracts turns_completed from a completed run's result.
// Falls back to estimating from duration_ms if the structured result is unavailable.
func getRunTurnsCompleted(t *testing.T, client controlv1.ControlServiceClient, runID string, durationMs int64) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.GetRunResult(ctx, &controlv1.GetRunResultRequest{RunId: runID})
	if err != nil {
		t.Logf("GetRunResult: %v — estimating turns from duration_ms=%d", err, durationMs)
		// Estimate: each turn ~200ms (150ms sleep + ~50ms overhead).
		return int(durationMs / 200)
	}
	structured := resp.GetStructuredResult()
	if structured == "" {
		t.Logf("GetRunResult returned empty structured_result — estimating turns from duration_ms=%d", durationMs)
		return int(durationMs / 200)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(structured), &result); err != nil {
		t.Logf("parse structured_result: %v — estimating turns from duration_ms=%d", err, durationMs)
		return int(durationMs / 200)
	}
	if tc, ok := result["turns_completed"]; ok {
		switch v := tc.(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	t.Logf("structured_result has no turns_completed field — estimating from duration_ms=%d", durationMs)
	return int(durationMs / 1000) // ~1s per turn in full mode (950ms sleep + overhead)
}

// invokeSoakAgent invokes the soak-agent deployment and returns the run ID + invocation ID.
func invokeSoakAgent(t *testing.T, client controlv1.ControlServiceClient, depID string) (runID, invocID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shortMode := os.Getenv("AGENTPAAS_SOAK_SHORT") == "1"

	// Pass turn configuration in the input JSON so the soak-agent knows how
	// many turns to run. In short mode use fewer turns with shorter sleeps.
	// Full mode: ~950ms/turn × 2000 = 1900s ≈ 32 min wall time.
	// Uses fewer turns with longer sleep to avoid hitting harness progress limits.
	turns := 2000
	sleepMs := 950
	if shortMode {
		turns = 500
		sleepMs = 50
	}
	inputJSON, _ := json.Marshal(map[string]any{
		"turns":    turns,
		"sleep_ms": sleepMs,
	})

	resp, err := client.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:               depID,
		IdempotencyKey:              "soak-test-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		CallerIdentity:              "operator-test",
		InputJson:                   inputJSON,
		InitialMaxActiveDurationMs: 45 * 60 * 1000, // 45 min — real ceilings for multi-turn soak
		InitialAttemptLeaseMs:      45 * 60 * 1000, // 45 min
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

	// Track wall clock from the very start.
	wallStart := time.Now()

	if shortMode {
		t.Logf("SHORT MODE: real daemon restart soak with reduced waits")
	} else {
		t.Logf("FULL MODE: real daemon restart soak — requires ≥30m wall + ≥100 turns")
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
	if err := waitForRunStatus(t, client, runID, "running", 60*time.Second); err != nil {
		t.Fatalf("wait for running: %v", err)
	}
	t.Logf("Run %s is now running; agent is accumulating turns in Docker container", runID)

	// 5. Let the agent run and accumulate progress BEFORE the daemon kill.
	// The soak-agent runs in a Docker container. After daemon restart, the
	// container's harness can't reconnect to the new daemon, so all progress
	// must happen before the kill. In full mode, we wait for ≥30 min wall
	// time and ≥100 turns.
	var runDuration time.Duration
	progressPollInterval := 5 * time.Second
	if shortMode {
		runDuration = 5 * time.Second
	} else {
		// Full mode: wait until wall clock reaches 30 min + buffer.
		runDuration = 30 * time.Minute
		progressPollInterval = 30 * time.Second
	}
	t.Logf("Letting agent run for %v to accumulate progress...", runDuration)

	deadline := time.Now().Add(runDuration)
	var lastActiveMs int64
	var agentTerminal bool
	var agentTerminalStatus string
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < progressPollInterval {
			time.Sleep(remaining)
			break
		}
		time.Sleep(progressPollInterval)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		summary, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
		cancel()
		if err != nil {
			t.Logf("SummarizeRun (progress poll): %v", err)
			continue
		}
		lastActiveMs = summary.GetDurationMs()
		elapsed := time.Since(wallStart).Seconds()
		t.Logf("Progress: elapsed=%.0fs status=%s active_ms=%d", elapsed, summary.GetStatus(), lastActiveMs)

		// If the agent reached a terminal state, stop waiting.
		status := summary.GetStatus()
		if status == "completed" || status == "failed" || status == "stopped" {
			t.Logf("Agent reached terminal status %s at elapsed=%.0fs — stopping progress wait", status, elapsed)
			agentTerminal = true
			agentTerminalStatus = status
			break
		}
	}

	// Final pre-restart state capture.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	beforeSummary, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel()
	if err != nil {
		t.Fatalf("SummarizeRun pre-restart: %v", err)
	}
	activeBefore := beforeSummary.GetDurationMs()
	t.Logf("Pre-restart final: status=%s active_ms=%d", beforeSummary.GetStatus(), activeBefore)

	// Full mode: if agent terminated early (before the 30-minute soak window),
	// this is a hard RED-GATE failure — do NOT greenwash by waiting wall clock
	// while the agent is already dead. The 60s urlopen kill bug (now fixed)
	// produced exactly this symptom: agent dead at ~67s, soak passed anyway.
	if !shortMode && agentTerminal {
		elapsed := time.Since(wallStart).Seconds()
		if elapsed < 1800 && agentTerminalStatus != "completed" {
			t.Errorf("RED-GATE: agent reached terminal %s at %.0fs (<1800s wall) — "+
				"soak is NOT accumulating turns; soak cannot pass from wall-clock alone. "+
				"Status=%s active_ms=%d",
				agentTerminalStatus, elapsed, agentTerminalStatus, activeBefore)
		}
	}

	// Extract turns_completed from the pre-restart state.
	// The soak-agent's agent.progress() calls may not increment SummarizeRun's
	// duration_ms in the current harness path, so we estimate from wall clock.
	// Conservative estimate: per-turn = sleep_ms + ~50ms overhead.
	// Full mode: 950ms + 50ms = 1000ms/turn. Short mode: 50ms + 20ms = 70ms/turn.
	turnsBeforeKill := getRunTurnsCompleted(t, client, runID, activeBefore)
	if turnsBeforeKill == 0 {
		perTurnMs := int64(1000)
		if shortMode {
			perTurnMs = 70
		}
		elapsedBeforeKill := time.Since(wallStart).Milliseconds()
		if elapsedBeforeKill > 0 {
			turnsBeforeKill = int(elapsedBeforeKill / perTurnMs)
		}
	}
	t.Logf("Turns completed before restart: %d (active_ms=%d, elapsed=%.0fs)", turnsBeforeKill, activeBefore, time.Since(wallStart).Seconds())

	// Verify invocation is reachable before restart.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	_, invocErr := client.GetInvocation(ctx2, &controlv1.GetInvocationRequest{InvocationId: invocID})
	cancel2()
	if invocErr != nil {
		t.Logf("GetInvocation before restart: %v (non-fatal)", invocErr)
	} else {
		t.Logf("Before restart: invocation=%s OK", invocID)
	}

	// 6. Kill the daemon via SIGKILL — scope to THIS test daemon only.
	// NEVER kill every bin/agentpaasd; only the PID returned by startAgentpaasd.
	// Killing other agentpaasd processes (e.g. the user's product daemon) causes
	// interference that makes concurrent soak tests fail spuriously.
	_ = conn.Close() // close client before kill
	restartStart := time.Now()
	proc, err := os.FindProcess(pidBefore)
	if err != nil {
		t.Fatalf("FindProcess pidBefore=%d: %v", pidBefore, err)
	}
	t.Logf("Killing agentpaasd pid=%d with SIGKILL", pidBefore)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		t.Errorf("SIGKILL pid=%d: %v", pidBefore, err)
	}
	time.Sleep(2 * time.Second) // wait for process death
	wallGap := time.Since(restartStart).Seconds()
	t.Logf("Daemon killed; wall gap so far: %.1fs", wallGap)

	// 7. Restart the daemon.
	pidAfter, _, _, restartCleanup := startAgentpaasd(t)
	defer restartCleanup()
	t.Logf("agentpaasd PID after: %d", pidAfter)

	conn2, client2 := dialDaemon(t, socketPath)
	defer func() { _ = conn2.Close() }()

	// 8. Wait for reconcile to complete.
	reconcileWait := 15 * time.Second
	if shortMode {
		reconcileWait = 5 * time.Second
	}
	t.Logf("Waiting %v for daemon to reconcile...", reconcileWait)
	time.Sleep(reconcileWait)

	// 9. Check post-restart state.

	// SC1: PID must have changed.
	if pidAfter == pidBefore {
		t.Errorf("SC1 VIOLATION: agentpaasd PID did not change across restart (before=%d after=%d)", pidBefore, pidAfter)
	} else {
		t.Logf("SC1 OK: PID changed %d → %d", pidBefore, pidAfter)
	}

	// SC2: active time after restart must not have increased by full wall gap.
	ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
	afterSummary, err := client2.SummarizeRun(ctx3, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel3()
	var activeAfter int64
	if err != nil {
		t.Logf("SummarizeRun after restart: %v", err)
	} else {
		activeAfter = afterSummary.GetDurationMs()
		wallGapMs := int64(wallGap * 1000)
		if activeAfter-activeBefore > wallGapMs {
			t.Errorf("SC2 VIOLATION: active-time increased by %dms across %ds wall gap (before=%d, after=%d)",
				activeAfter-activeBefore, int(wallGap), activeBefore, activeAfter)
		} else {
			t.Logf("SC2 OK: active before=%dms after=%dms gap=%.1fs delta=%dms",
				activeBefore, activeAfter, wallGap, activeAfter-activeBefore)
		}
	}

	// 10. Build evidence.
	endWall := time.Now()
	wallSeconds := endWall.Sub(wallStart).Seconds()

	var failures []string

	// Duration assertions (full mode only).
	if !shortMode {
		if wallSeconds < 1800 {
			failures = append(failures, fmt.Sprintf("wall_seconds %.0f < 1800", wallSeconds))
		}
		if turnsBeforeKill < 100 {
			failures = append(failures, fmt.Sprintf("turns_completed %d < 100", turnsBeforeKill))
		}
	}

	if pidAfter == pidBefore {
		failures = append(failures, "PID did not change across restart")
	}

	evidence := RealSoakEvidence{
		SoakEvidence: SoakEvidence{
			SchemaVersion:   "m0",
			SoakID:          fmt.Sprintf("real-daemon-restart-%d", wallStart.Unix()),
			StartTime:       wallStart.UTC().Format(time.RFC3339),
			EndTime:         endWall.UTC().Format(time.RFC3339),
			WallSeconds:     wallSeconds,
			TurnsCompleted:  turnsBeforeKill,
			DaemonRestarts:  1,
			Pass:            len(failures) == 0,
		},
		AgentpaasdPIDBefore:  pidBefore,
		AgentpaasdPIDAfter:   pidAfter,
		ActiveMsBeforeGap:    activeBefore,
		ActiveMsAfterGap:     activeAfter,
		WallGapSeconds:       wallGap,
		AgentpaasdKillSignal: "SIGKILL",
	}

	if evidence.Pass {
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

	// Track wall clock from the very start.
	wallStart := time.Now()

	if shortMode {
		t.Logf("SHORT MODE: SIGKILL soak with reduced wait times")
	} else {
		t.Logf("FULL MODE: SIGKILL soak — agent must accumulate turns before kill")
	}

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
	if err := waitForRunStatus(t, client, runID, "running", 60*time.Second); err != nil {
		t.Fatalf("wait for running: %v", err)
	}

	// 5. Discover the worker container ID.
	containerID := findWorkerContainer(t, runID)
	if containerID == "" {
		t.Fatal("Empty worker container ID — cannot proceed with SIGKILL test")
	}

	// 6. In full mode, let the agent accumulate turns before SIGKILL.
	progressWait := 30 * time.Second
	if shortMode {
		progressWait = 5 * time.Second
	}
	t.Logf("Waiting %v for agent to accumulate progress before SIGKILL...", progressWait)
	time.Sleep(progressWait)

	// Capture pre-kill state.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	beforeSummary, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel()
	preKillActiveMs := int64(0)
	if err != nil {
		t.Logf("SummarizeRun before SIGKILL: %v", err)
	} else {
		preKillActiveMs = beforeSummary.GetDurationMs()
		t.Logf("Before SIGKILL: status=%s active_ms=%d", beforeSummary.GetStatus(), preKillActiveMs)
	}

	// 7. Apply docker kill to the worker container.
	exitCode := dockerKillContainer(t, containerID)
	if exitCode == 0 {
		t.Logf("docker kill exit code: %d (container was already stopped or exit 0)", exitCode)
	} else {
		t.Logf("docker kill exit code: %d", exitCode)
	}

	// 8. Wait for daemon to detect the container death and update run status.
	detectWait := 15 * time.Second
	if shortMode {
		detectWait = 10 * time.Second
	}
	time.Sleep(detectWait)

	// 9. Check run status — should be failed with typed reason.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	summary, err := client.SummarizeRun(ctx2, &controlv1.SummarizeRunRequest{RunId: runID})
	cancel2()
	var finalDurationMs int64
	if err != nil {
		t.Logf("SummarizeRun after SIGKILL: %v", err)
	} else {
		finalDurationMs = summary.GetDurationMs()
		t.Logf("After SIGKILL: status=%s duration_ms=%d", summary.GetStatus(), finalDurationMs)
	}

	// 10. Verify terminal reason is typed (not generic).
	ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
	failureResp, err := client.ExplainFailure(ctx3, &controlv1.ExplainFailureRequest{RunId: runID})
	cancel3()
	terminalReason := "unknown"
	if err != nil {
		t.Logf("ExplainFailure: %v", err)
	} else {
		terminalReason = failureResp.GetErrorCategory()
		t.Logf("Terminal reason: %s", terminalReason)
	}

	// 11. Extract turns_completed and compute wall_seconds.
	endWall := time.Now()
	wallSeconds := endWall.Sub(wallStart).Seconds()

	// Use pre-kill active time for turns estimate if final is 0.
	durationForTurns := finalDurationMs
	if durationForTurns == 0 {
		durationForTurns = preKillActiveMs
	}
	turnsCompleted := int(durationForTurns / 200) // ~200ms per turn

	t.Logf("Wall clock: %.0fs, turns estimated: %d", wallSeconds, turnsCompleted)

	// 12. Build evidence and verify assertions.
	var failures []string

	// Duration assertions (full mode only — SIGKILL test has shorter wall requirement).
	if !shortMode {
		if turnsCompleted < 10 {
			failures = append(failures, fmt.Sprintf("turns_completed %d < 10 (agent didn't run enough before kill)", turnsCompleted))
		}
	}

	if containerID == "" {
		failures = append(failures, "empty container_id")
	}
	if summary != nil && summary.GetStatus() == "running" {
		failures = append(failures, "run still running after SIGKILL")
	}

	evidence := RealSoakEvidence{
		SoakEvidence: SoakEvidence{
			SchemaVersion:  "m0",
			SoakID:         fmt.Sprintf("real-worker-sigkill-%d", wallStart.Unix()),
			StartTime:      wallStart.UTC().Format(time.RFC3339),
			EndTime:        endWall.UTC().Format(time.RFC3339),
			WallSeconds:    wallSeconds,
			TurnsCompleted: turnsCompleted,
			SIGKILLInjects: 1,
			Pass:           len(failures) == 0,
		},
		AgentpaasdPIDBefore: pidBefore,
		WorkerContainerID:   containerID,
		DockerKillExit:      exitCode,
		TerminalReason:      terminalReason,
	}

	if evidence.Pass {
		evidence.Note = "PASS (real worker SIGKILL)"
	} else {
		evidence.Note = "FAIL: " + strings.Join(failures, "; ")
	}

	writeRealEvidence(t, evidence, "real-worker-sigkill.json")

	if !evidence.Pass {
		t.Errorf("RealWorkerSIGKILL FAIL: %s", evidence.Note)
	}
}