package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// Cleanup removes ephemeral resources (containers, networks, temp creds,
// journal keys) for a run. It is idempotent: safe to call multiple times.
//
// T05 Part A scope: Cleanup does NOT remove Docker containers or networks
// (T07 owns that). It removes the per-attempt control-key file (the HMAC key
// is no longer needed once the run is terminal) and drops the in-memory
// trackers. It PRESERVES durable state: the run record, attempt records,
// checkpoints, artifacts, and the control journal (event history) are all
// kept for audit and B39 continuation.
//
// Cleanup is a no-op if the run is not terminal (it refuses to clean up a run
// that still has active work, to avoid losing liveness tracking).
func (s *Supervisor) Cleanup(ctx context.Context, runID routedrun.RunID) error {
	if runID == "" {
		return fmt.Errorf("%w: empty run id", ErrInvalidArgument)
	}
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("supervisor cleanup: %w", err)
	}
	// Do not clean up a non-terminal run (the work may still be active).
	if !run.Status.IsTerminal() {
		return fmt.Errorf("%w: cannot cleanup non-terminal run %s", ErrInvalidArgument, runID)
	}
	atts, err := s.store.ListAttempts(ctx, runID)
	if err != nil {
		return fmt.Errorf("supervisor cleanup: %w", err)
	}
	// Drop in-memory trackers for the run's attempts.
	s.mu.Lock()
	for _, att := range atts {
		delete(s.trackers, att.AttemptID)
	}
	s.mu.Unlock()
	// Remove the per-run control-key file (the HMAC key is no longer needed).
	// The control journals (event history) are PRESERVED for audit.
	if s.stateRoot != "" {
		keyPath := filepath.Join(s.stateRoot, "runs", string(runID), "control-key")
		if err := os.Remove(keyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			// Best-effort: do not fail cleanup on key removal failure.
			log.Printf("supervisor: remove control-key %s: %v", keyPath, err)
		}
	}
	if err := s.audit.Append(audit.AuditRecord{
		Timestamp:      s.nowWall().Format(time.RFC3339Nano),
		EventType:      "supervisor_cleanup",
		DeploymentMode: "local",
		Actor:          "supervisor",
		Payload: map[string]interface{}{
			"run_id": string(runID),
		},
	}); err != nil {
		log.Printf("supervisor: audit append (%s): %v", "supervisor_cleanup", err)
	}
	return nil
}
