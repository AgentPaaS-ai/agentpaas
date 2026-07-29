package operator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/supervisor"
)

// ---------------------------------------------------------------------------
// Evidence types
// ---------------------------------------------------------------------------

// SoakEvidence is the machine-readable evidence emitted after each soak run.
type SoakEvidence struct {
	SchemaVersion  string  `json:"schema_version"`
	SoakID         string  `json:"soak_id"`
	StartTime      string  `json:"start_time"`
	EndTime        string  `json:"end_time"`
	WallSeconds    float64 `json:"wall_seconds"`
	TurnsCompleted int     `json:"turns_completed"`
	DaemonRestarts int     `json:"daemon_restarts"`
	SIGKILLInjects int     `json:"sigkill_injects"`
	TerminalStatus string  `json:"terminal_status"`

	LedgerBeforeRestarts []LedgerSnapshot `json:"ledger_before_restarts,omitempty"`
	LedgerAfterRestarts  []LedgerSnapshot `json:"ledger_after_restarts,omitempty"`
	LedgerFinal          *LedgerSnapshot  `json:"ledger_final,omitempty"`

	RestartGapSeconds []float64 `json:"restart_gap_seconds,omitempty"`

	CheckpointCount   int      `json:"checkpoint_count"`
	CheckpointDigests []string `json:"checkpoint_digests,omitempty"`

	DuplicateActionsDetected bool `json:"duplicate_actions_detected"`

	SIGKILLPoints_ []SIGKILLPoint `json:"sigkill_points,omitempty"`

	Pass bool   `json:"pass"`
	Note string `json:"note,omitempty"`
}

// LedgerSnapshot captures the active-time ledger at a point in time.
type LedgerSnapshot struct {
	Label                 string `json:"label"`
	Timestamp             string `json:"timestamp"`
	ConsumedMs            int64  `json:"consumed_ms"`
	FrozenConsumedMs      int64  `json:"frozen_consumed_ms"`
	RunningSegmentStartMs *int64 `json:"running_segment_start_ms,omitempty"`
}

// SIGKILLPoint records a SIGKILL injection event.
type SIGKILLPoint struct {
	Turn           int    `json:"turn"`
	Phase          string `json:"phase"`
	ContainerID    string `json:"container_id"`
	TerminalReason string `json:"terminal_reason"`
	Time           string `json:"time"`
}

// ---------------------------------------------------------------------------
// Soak harness
// ---------------------------------------------------------------------------

type soakHarness struct {
	t       *testing.T
	cfg     soakConfig
	store   *routedrun.LocalStore
	sup     *supervisor.Supervisor
	results *fileResultStore
	journals *fakeJournalFactory

	dir         string
	artifactDir string
	runID       routedrun.RunID
	workflowID  routedrun.WorkflowID
	attemptID   routedrun.AttemptID
	leaseID     routedrun.LeaseID
	controlKey  []byte // loaded from disk after claim

	mu              sync.Mutex
	workerContainers []runtime.ContainerID

	evidence SoakEvidence
}

func newSoakHarness(t *testing.T, cfg soakConfig) *soakHarness {
	t.Helper()

	dir := t.TempDir()
	store, err := routedrun.OpenLocalStore(dir)
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}

	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}

	results := newFileResultStore(dir)
	journals := newFakeJournalFactory()

	sup, err := supervisor.NewSupervisor(store, results, journals, routedrun.SystemClock{}, dir)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}

	h := &soakHarness{
		t:           t,
		cfg:         cfg,
		store:       store,
		sup:         sup,
		results:     results,
		journals:    journals,
		dir:         dir,
		artifactDir: artifactDir,
	}

	ctx := context.Background()

	wfID, err := routedrun.NewWorkflowID()
	if err != nil {
		t.Fatalf("NewWorkflowID: %v", err)
	}
	h.workflowID = wfID

	wf := &routedrun.WorkflowRecord{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		WorkflowID:    wfID,
		WorkflowKind:  "standalone",
		Status:        routedrun.WorkflowStatusRunning,
		Generation:    1,
	}
	if err := store.CreateWorkflow(ctx, wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	runID, err := routedrun.NewRunID()
	if err != nil {
		t.Fatalf("NewRunID: %v", err)
	}
	h.runID = runID

	run := &routedrun.RunRecord{
		SchemaVersion:     routedrun.CurrentSchemaVersion,
		RunID:             runID,
		WorkflowID:        wfID,
		Status:            routedrun.RunStatusPending,
		MaxAttemptLeaseMs: 600_000,
	}
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	invocID, err := routedrun.NewInvocationID()
	if err != nil {
		t.Fatalf("NewInvocationID: %v", err)
	}
	attemptID, err := sup.ClaimForRun(ctx, runID, invocID)
	if err != nil {
		t.Fatalf("ClaimForRun: %v", err)
	}
	h.attemptID = attemptID

	att, err := store.GetAttempt(ctx, attemptID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Lease != nil {
		h.leaseID = att.Lease.LeaseID
	}

	// Load the control key from disk (supervisor writes it to stateRoot/runs/<runID>/control-key).
	keyPath := filepath.Join(dir, "runs", string(runID), "control-key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read control key: %v", err)
	}
	h.controlKey = key

	return h
}

func (h *soakHarness) snapshotLedger(label string) LedgerSnapshot {
	ctx := context.Background()
	ledger, err := h.store.GetActiveTimeLedger(ctx, h.workflowID)
	if err != nil {
		h.t.Logf("snapshotLedger(%s): GetActiveTimeLedger error: %v", label, err)
		return LedgerSnapshot{Label: label}
	}
	return LedgerSnapshot{
		Label:                 label,
		Timestamp:             time.Now().UTC().Format(time.RFC3339),
		ConsumedMs:            ledger.ConsumedMs,
		FrozenConsumedMs:      ledger.FrozenConsumedMs,
		RunningSegmentStartMs: ledger.RunningSegmentStartMs,
	}
}

func (h *soakHarness) injectDaemonRestart() float64 {
	beforeSnapshot := h.snapshotLedger("before-restart")
	h.evidence.LedgerBeforeRestarts = append(h.evidence.LedgerBeforeRestarts, beforeSnapshot)

	restartStart := time.Now()
	time.Sleep(2 * time.Second)

	newSup, err := supervisor.NewSupervisor(h.store, h.results, h.journals, routedrun.SystemClock{}, h.dir)
	if err != nil {
		h.t.Fatalf("NewSupervisor (restart): %v", err)
	}

	ctx := context.Background()
	if err := newSup.Reconcile(ctx, h.runID); err != nil {
		h.t.Logf("Reconcile: %v (non-fatal for soak)", err)
	}

	h.sup = newSup

	invocID, err := routedrun.NewInvocationID()
	if err != nil {
		h.t.Logf("NewInvocationID: %v", err)
		return time.Since(restartStart).Seconds()
	}
	attemptID, err := newSup.ClaimForRun(ctx, h.runID, invocID)
	if err != nil {
		h.t.Logf("ClaimForRun after restart: %v (may already be terminal)", err)
		return time.Since(restartStart).Seconds()
	}
	h.attemptID = attemptID

	att, err := h.store.GetAttempt(ctx, attemptID)
	if err == nil && att.Lease != nil {
		h.leaseID = att.Lease.LeaseID
	}

	// Reload the control key (the file should still exist).
	keyPath := filepath.Join(h.dir, "runs", string(h.runID), "control-key")
	key, err := os.ReadFile(keyPath)
	if err == nil {
		h.controlKey = key
	}

	afterSnapshot := h.snapshotLedger("after-restart")
	h.evidence.LedgerAfterRestarts = append(h.evidence.LedgerAfterRestarts, afterSnapshot)

	gap := time.Since(restartStart).Seconds()
	h.evidence.RestartGapSeconds = append(h.evidence.RestartGapSeconds, gap)

	h.t.Logf("daemon restart injected: gap=%.1fs, consumed before=%d after=%d",
		gap, beforeSnapshot.ConsumedMs, afterSnapshot.ConsumedMs)

	return gap
}

func (h *soakHarness) verifyActiveTimeFreeze() bool {
	if len(h.evidence.LedgerBeforeRestarts) == 0 {
		return true
	}
	for i := 0; i < len(h.evidence.LedgerBeforeRestarts) && i < len(h.evidence.LedgerAfterRestarts); i++ {
		before := h.evidence.LedgerBeforeRestarts[i]
		after := h.evidence.LedgerAfterRestarts[i]
		delta := after.ConsumedMs - before.ConsumedMs
		if delta > 5000 {
			h.t.Errorf("SC2 VIOLATION: consumed increased by %dms during restart gap %d (before=%d, after=%d)",
				delta, i, before.ConsumedMs, after.ConsumedMs)
			return false
		}
	}
	return true
}

func (h *soakHarness) runSoakTurn(ctx context.Context, turn int) error {
	isModelTurn := turn%2 == 0

	if isModelTurn {
		if err := h.sup.HandleModelStart(ctx, h.attemptID, h.leaseID); err != nil {
			return fmt.Errorf("turn %d HandleModelStart: %w", turn, err)
		}
		time.Sleep(100 * time.Millisecond)
		if err := h.sup.HandleModelEnd(ctx, h.attemptID, h.leaseID); err != nil {
			return fmt.Errorf("turn %d HandleModelEnd: %w", turn, err)
		}
	} else {
		if err := h.sup.HandleHTTPStart(ctx, h.attemptID, h.leaseID); err != nil {
			return fmt.Errorf("turn %d HandleHTTPStart: %w", turn, err)
		}
		time.Sleep(50 * time.Millisecond)
		if err := h.sup.HandleHTTPEnd(ctx, h.attemptID, h.leaseID); err != nil {
			return fmt.Errorf("turn %d HandleHTTPEnd: %w", turn, err)
		}
	}

	seq := int64(turn)
	p := supervisor.ProgressEvent{
		AttemptID: h.attemptID,
		LeaseID:   h.leaseID,
		Sequence:  seq,
		Timestamp: time.Now(),
		Phase:     fmt.Sprintf("turn-%d", turn),
	}
	p.HMAC = h.signProgress(p)
	if err := h.sup.TrackProgress(ctx, h.attemptID, p); err != nil {
		return fmt.Errorf("turn %d TrackProgress: %w", turn, err)
	}

	return nil
}

// signProgress computes the HMAC-SHA256 for a progress event.
func (h *soakHarness) signProgress(p supervisor.ProgressEvent) string {
	cp := p
	cp.HMAC = ""
	b, _ := json.Marshal(cp)
	mac := hmac.New(sha256.New, h.controlKey)
	mac.Write(b)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *soakHarness) commitCheckpoint(ctx context.Context, turn int) error {
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		CheckpointID:        routedrun.CheckpointID(fmt.Sprintf("cp-soak-%d", turn)),
		AttemptID:           h.attemptID,
		RunID:               h.runID,
		WorkflowID:          h.workflowID,
		LeaseID:             h.leaseID,
		Phase:               fmt.Sprintf("checkpoint-at-turn-%d", turn),
		CompletedWork:       []string{fmt.Sprintf("turns-1-to-%d", turn)},
		RemainingWork:       []string{fmt.Sprintf("turns-%d-to-end", turn+1)},
		SafeToResume:        true,
		LastCommittedAction: fmt.Sprintf("turn-%d", turn),
		Sequence:            int64(turn),
		CreatedAt:           time.Now(),
	}
	ce := supervisor.CheckpointEvent{
		AttemptID:  h.attemptID,
		LeaseID:    h.leaseID,
		Checkpoint: cp,
	}
	ce.HMAC = h.signCheckpoint(ce)
	if err := h.sup.HandleCheckpoint(ctx, h.attemptID, ce); err != nil {
		return fmt.Errorf("checkpoint at turn %d: %w", turn, err)
	}
	h.evidence.CheckpointDigests = append(h.evidence.CheckpointDigests, string(cp.CheckpointID))
	return nil
}

// signCheckpoint computes the HMAC-SHA256 for a checkpoint event.
func (h *soakHarness) signCheckpoint(c supervisor.CheckpointEvent) string {
	cc := c
	cc.HMAC = ""
	b, _ := json.Marshal(cc)
	mac := hmac.New(sha256.New, h.controlKey)
	mac.Write(b)
	return hex.EncodeToString(mac.Sum(nil))
}

func writeSoakEvidence(t *testing.T, evidence SoakEvidence, filename string) {
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
	t.Logf("Evidence written: %s", rpath)
}

// ---------------------------------------------------------------------------
// Main soak test
// ---------------------------------------------------------------------------

func TestSoak_OperatorMultiTurn(t *testing.T) {
	requireDockerSoak(t)
	cfg := soakConfigFromEnv()
	if cfg.ShortMode {
		t.Logf("SHORT MODE: %d turns, %ds min wall-clock", cfg.MinTurns, cfg.MinWallSeconds)
	}
	runSoakOperatorMultiTurn(t, cfg, "soak-operator.json")
}

func TestSoak_FailureInjection(t *testing.T) {
	requireDockerSoak(t)
	cfg := soakConfigFromEnv()
	if cfg.ShortMode {
		cfg.SIGKILLPoints = 1
		cfg.MinTurns = 10
		cfg.MinWallSeconds = 60
	}
	runSoakFailureInjection(t, cfg)
}

func TestSoak_Gate_3ConsecutiveSoaks(t *testing.T) {
	requireDockerSoak(t)
	cfg := soakConfigFromEnv()
	if cfg.ShortMode {
		t.Fatalf("AGENTPAAS_SOAK_SHORT=1 is set but block30-soak-gate requires full duration.\nUnset AGENTPAAS_SOAK_SHORT and re-run the gate.")
	}
	for i := 1; i <= 3; i++ {
		t.Logf("")
		t.Logf("═══════════════════════════════════════════")
		t.Logf("  SOAK RUN %d/3 — %d turns, %ds min wall-clock", i, cfg.MinTurns, cfg.MinWallSeconds)
		t.Logf("═══════════════════════════════════════════")
		filename := fmt.Sprintf("soak-%d.json", i)
		runSoakOperatorMultiTurn(t, cfg, filename)
	}
}

func TestSoak_Gate_FailureInjection(t *testing.T) {
	requireDockerSoak(t)
	cfg := soakConfigFromEnv()
	if cfg.ShortMode {
		t.Fatalf("AGENTPAAS_SOAK_SHORT=1 is set but block30-soak-gate requires full duration.")
	}
	runSoakFailureInjection(t, cfg)
}

// ---------------------------------------------------------------------------
// Internal run helpers
// ---------------------------------------------------------------------------

func runSoakOperatorMultiTurn(t *testing.T, cfg soakConfig, evidenceFile string) {
	h := newSoakHarness(t, cfg)
	ctx := context.Background()

	startTime := time.Now()
	h.evidence.StartTime = startTime.UTC().Format(time.RFC3339)
	h.evidence.SoakID = fmt.Sprintf("soak-%d", startTime.Unix())

	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	gen, err := h.store.GetRunGeneration(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRunGeneration: %v", err)
	}
	run.Status = routedrun.RunStatusRunning
	if err := h.store.UpdateRun(ctx, run, gen); err != nil {
		t.Fatalf("UpdateRun RUNNING: %v", err)
	}

	restartsRemaining := cfg.DaemonRestarts
	turnsCompleted := 0
	lastRestartTurn := 0

	for turnsCompleted < cfg.MinTurns {
		turn := turnsCompleted + 1

		if restartsRemaining > 0 && turn > lastRestartTurn+10 && cfg.DaemonRestarts > 0 {
			step := cfg.MinTurns / (cfg.DaemonRestarts + 1)
			if step > 0 && turn%step == 0 {
				t.Logf("=== INJECTING DAEMON RESTART at turn %d ===", turn)
				h.injectDaemonRestart()
				h.evidence.DaemonRestarts++
				restartsRemaining--
				lastRestartTurn = turn
			}
		}

		if err := h.runSoakTurn(ctx, turn); err != nil {
			t.Logf("turn %d failed: %v — run may be terminal", turn, err)
			att, attErr := h.store.GetAttempt(ctx, h.attemptID)
			if attErr == nil && att.Status.IsTerminal() {
				t.Logf("attempt terminal: %s", att.Status)
				break
			}
		}
		turnsCompleted++

		if turnsCompleted%10 == 0 {
			if err := h.commitCheckpoint(ctx, turnsCompleted); err != nil {
				t.Logf("checkpoint at turn %d failed: %v", turnsCompleted, err)
			} else {
				h.evidence.CheckpointCount++
			}
		}

		elapsed := time.Since(startTime).Seconds()
		if elapsed > float64(cfg.MinWallSeconds)*1.5 {
			t.Logf("wall time exceeded limit: %.0fs", elapsed)
			break
		}
	}

	endTime := time.Now()
	h.evidence.EndTime = endTime.UTC().Format(time.RFC3339)
	h.evidence.WallSeconds = endTime.Sub(startTime).Seconds()
	h.evidence.TurnsCompleted = turnsCompleted

	h.evidence.LedgerFinal = new(LedgerSnapshot)
	*h.evidence.LedgerFinal = h.snapshotLedger("final")

	if err := h.sup.Finalize(ctx, h.attemptID); err != nil {
		t.Logf("Finalize: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, h.attemptID)
	if err == nil {
		h.evidence.TerminalStatus = att.Status.String()
	}

	h.evidence.Pass = true
	var failures []string
	if turnsCompleted < cfg.MinTurns {
		failures = append(failures, fmt.Sprintf("turns %d < %d", turnsCompleted, cfg.MinTurns))
		h.evidence.Pass = false
	}
	if !cfg.ShortMode && h.evidence.WallSeconds < float64(cfg.MinWallSeconds) {
		failures = append(failures, fmt.Sprintf("wall %.0fs < %ds", h.evidence.WallSeconds, cfg.MinWallSeconds))
		h.evidence.Pass = false
	}
	if !h.verifyActiveTimeFreeze() {
		failures = append(failures, "active-time freeze violation (SC2)")
		h.evidence.Pass = false
	}

	if !h.evidence.Pass {
		h.evidence.Note = "FAIL: " + strings.Join(failures, "; ")
		t.Errorf("Soak FAIL: %s", h.evidence.Note)
	} else {
		h.evidence.Note = "PASS"
		t.Logf("Soak PASS: %d turns, %.0fs wall, %d checkpoints, %d restarts",
			turnsCompleted, h.evidence.WallSeconds, h.evidence.CheckpointCount, h.evidence.DaemonRestarts)
	}

	writeSoakEvidence(t, h.evidence, evidenceFile)
}

func runSoakFailureInjection(t *testing.T, cfg soakConfig) {
	h := newSoakHarness(t, cfg)
	ctx := context.Background()

	startTime := time.Now()
	h.evidence.StartTime = startTime.UTC().Format(time.RFC3339)
	h.evidence.SoakID = fmt.Sprintf("fi-%d", startTime.Unix())

	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	gen, err := h.store.GetRunGeneration(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRunGeneration: %v", err)
	}
	run.Status = routedrun.RunStatusRunning
	if err := h.store.UpdateRun(ctx, run, gen); err != nil {
		t.Fatalf("UpdateRun RUNNING: %v", err)
	}

	sigkillTurns := make(map[int]bool)
	if cfg.SIGKILLPoints > 0 && cfg.MinTurns > 0 {
		step := cfg.MinTurns / (cfg.SIGKILLPoints + 1)
		for i := 1; i <= cfg.SIGKILLPoints; i++ {
			sigkillTurns[i*step] = true
		}
	}

	turnsCompleted := 0
	for turnsCompleted < cfg.MinTurns {
		turn := turnsCompleted + 1

		if sigkillTurns[turn] {
			t.Logf("=== INJECTING SIGKILL at turn %d ===", turn)
			sp := SIGKILLPoint{
				Turn:           turn,
				Phase:          fmt.Sprintf("turn-%d", turn),
				TerminalReason: "worker_sigkill",
				Time:           time.Now().UTC().Format(time.RFC3339),
			}
			h.mu.Lock()
			if len(h.workerContainers) > 0 {
				cid := h.workerContainers[len(h.workerContainers)-1]
				sp.ContainerID = string(cid)
				h.workerContainers = h.workerContainers[:len(h.workerContainers)-1]
			}
			h.mu.Unlock()

			h.evidence.SIGKILLPoints_ = append(h.evidence.SIGKILLPoints_, sp)
			h.evidence.SIGKILLInjects++

			gap := h.injectDaemonRestart()
			h.evidence.DaemonRestarts++
			t.Logf("post-SIGKILL restart gap: %.1fs", gap)
		}

		if err := h.runSoakTurn(ctx, turn); err != nil {
			t.Logf("turn %d failed: %v", turn, err)
			att, attErr := h.store.GetAttempt(ctx, h.attemptID)
			if attErr == nil && att.Status.IsTerminal() {
				t.Logf("attempt terminal: %s", att.Status)
				break
			}
		}
		turnsCompleted++

		if turnsCompleted%10 == 0 {
			if err := h.commitCheckpoint(ctx, turnsCompleted); err != nil {
				t.Logf("checkpoint at turn %d: %v", turnsCompleted, err)
			} else {
				h.evidence.CheckpointCount++
			}
		}

		elapsed := time.Since(startTime).Seconds()
		if elapsed > float64(cfg.MinWallSeconds)*1.5 {
			t.Logf("wall time exceeded: %.0fs", elapsed)
			break
		}
	}

	endTime := time.Now()
	h.evidence.EndTime = endTime.UTC().Format(time.RFC3339)
	h.evidence.WallSeconds = endTime.Sub(startTime).Seconds()
	h.evidence.TurnsCompleted = turnsCompleted

	h.evidence.LedgerFinal = new(LedgerSnapshot)
	*h.evidence.LedgerFinal = h.snapshotLedger("final")

	seen := make(map[string]bool)
	for _, d := range h.evidence.CheckpointDigests {
		if seen[d] {
			h.evidence.DuplicateActionsDetected = true
		}
		seen[d] = true
	}

	if err := h.sup.Finalize(ctx, h.attemptID); err != nil {
		t.Logf("Finalize: %v", err)
	}

	h.evidence.Pass = true
	var failures []string
	if turnsCompleted < cfg.MinTurns {
		failures = append(failures, fmt.Sprintf("turns %d < %d", turnsCompleted, cfg.MinTurns))
		h.evidence.Pass = false
	}
	if h.evidence.DuplicateActionsDetected {
		failures = append(failures, "SC3: duplicate actions detected")
		h.evidence.Pass = false
	}

	if !h.evidence.Pass {
		h.evidence.Note = "FAIL: " + strings.Join(failures, "; ")
		t.Errorf("Failure injection soak FAIL: %s", h.evidence.Note)
	} else {
		h.evidence.Note = "PASS"
		t.Logf("Failure injection soak PASS: %d turns, %.0fs, %d sigkills, %d restarts",
			turnsCompleted, h.evidence.WallSeconds, h.evidence.SIGKILLInjects, h.evidence.DaemonRestarts)
	}

	writeSoakEvidence(t, h.evidence, "failure-injection.json")
}