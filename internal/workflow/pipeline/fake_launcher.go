package pipeline

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------------
// Stage launcher interface
// ---------------------------------------------------------------------------

// StageLauncher abstracts the container/process launch for a stage.
type StageLauncher interface {
	// EnsureLaunch is idempotent for job.Key.
	// Returns nil on success.
	EnsureLaunch(ctx context.Context, job *StageLaunchJob) error
}

// ---------------------------------------------------------------------------
// Fake launcher (no Docker, no moby)
// ---------------------------------------------------------------------------

// FakeLauncher is a noop launcher that marks jobs STARTED immediately.
// It does not import Docker, moby, or any runtime driver.
type FakeLauncher struct{}

// EnsureLaunch marks the job as STARTED (idempotent: no-op if already
// non-PENDING).
func (f FakeLauncher) EnsureLaunch(_ context.Context, job *StageLaunchJob) error {
	if job.Status == LaunchStatusStarted {
		return nil
	}
	job.Status = LaunchStatusStarted
	job.UpdatedAt = time.Now().UTC()
	return nil
}
