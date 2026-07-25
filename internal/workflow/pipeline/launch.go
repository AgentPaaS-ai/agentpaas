package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// Launch idempotency key
// ---------------------------------------------------------------------------

// LaunchIdempotencyKey constructs a stable idempotency key for a stage launch.
// Format: workflowID|nodeID|generation.
func LaunchIdempotencyKey(workflowID routedrun.WorkflowID, nodeID routedrun.NodeID, generation int64) string {
	return fmt.Sprintf("%s|%s|%d", workflowID, nodeID, generation)
}

// ---------------------------------------------------------------------------
// Launch status
// ---------------------------------------------------------------------------

// LaunchStatus represents the status of a stage launch job.
type LaunchStatus string

const (
	LaunchStatusPending   LaunchStatus = "PENDING"
	LaunchStatusStarted   LaunchStatus = "STARTED"
	LaunchStatusCompleted LaunchStatus = "COMPLETED"
	LaunchStatusFailed    LaunchStatus = "FAILED"
)

// ---------------------------------------------------------------------------
// Stage launch job
// ---------------------------------------------------------------------------

// StageLaunchJob records a stage launch attempt with idempotency.
type StageLaunchJob struct {
	Key        string               // LaunchIdempotencyKey
	WorkflowID routedrun.WorkflowID
	NodeID     routedrun.NodeID
	RunID      routedrun.RunID
	AttemptID  routedrun.AttemptID
	Generation int64
	Status     LaunchStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// Optional container-spec fields (zero-value safe for old tests).
	// The FakeLauncher ignores these.

	// Image is the container image reference for this stage.
	Image string
	// NetworkID is the Docker network ID for stage-private isolation.
	NetworkID string
	// PackageDigest is the content digest of the stage package.
	PackageDigest string
	// PolicyDigest is the content digest of the stage policy.
	PolicyDigest string
	// StageOrder is the 0-based ordinal of this stage in the pipeline.
	StageOrder int
	// ContainerID is the runtime container ID set after a successful launch.
	ContainerID string
}

// ---------------------------------------------------------------------------
// Launch store interface
// ---------------------------------------------------------------------------

// LaunchStore is the store interface for launch job records.
type LaunchStore interface {
	// PutIfAbsent stores the job only if the key is not already present.
	// Returns existing job and created=false if already present.
	PutIfAbsent(ctx context.Context, job *StageLaunchJob) (existing *StageLaunchJob, created bool, err error)

	// Get retrieves a launch job by key.
	Get(ctx context.Context, key string) (*StageLaunchJob, error)

	// Update updates an existing launch job.
	Update(ctx context.Context, job *StageLaunchJob) error

	// ListByWorkflow returns all launch jobs for a workflow.
	ListByWorkflow(ctx context.Context, workflowID routedrun.WorkflowID) ([]*StageLaunchJob, error)
}

// ---------------------------------------------------------------------------
// Memory launch store
// ---------------------------------------------------------------------------

// MemoryLaunchStore is an in-memory implementation of LaunchStore.
type MemoryLaunchStore struct {
	mu   sync.RWMutex
	jobs map[string]*StageLaunchJob
}

// NewMemoryLaunchStore creates an empty MemoryLaunchStore.
func NewMemoryLaunchStore() *MemoryLaunchStore {
	return &MemoryLaunchStore{
		jobs: make(map[string]*StageLaunchJob),
	}
}

// PutIfAbsent stores the job only if the key is not already present.
func (s *MemoryLaunchStore) PutIfAbsent(_ context.Context, job *StageLaunchJob) (*StageLaunchJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.jobs[job.Key]; ok {
		cp := *existing
		return &cp, false, nil
	}

	cp := *job
	s.jobs[job.Key] = &cp
	return nil, true, nil
}

// Get retrieves a launch job by key.
func (s *MemoryLaunchStore) Get(_ context.Context, key string) (*StageLaunchJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[key]
	if !ok {
		return nil, fmt.Errorf("launch job not found: %s", key)
	}
	cp := *job
	return &cp, nil
}

// Update updates an existing launch job.
func (s *MemoryLaunchStore) Update(_ context.Context, job *StageLaunchJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.Key]; !ok {
		return fmt.Errorf("launch job not found for update: %s", job.Key)
	}
	cp := *job
	s.jobs[job.Key] = &cp
	return nil
}

// ListByWorkflow returns all launch jobs for a workflow.
func (s *MemoryLaunchStore) ListByWorkflow(_ context.Context, workflowID routedrun.WorkflowID) ([]*StageLaunchJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*StageLaunchJob
	for _, job := range s.jobs {
		if job.WorkflowID == workflowID {
			cp := *job
			out = append(out, &cp)
		}
	}
	return out, nil
}
