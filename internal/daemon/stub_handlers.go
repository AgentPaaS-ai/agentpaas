package daemon

import (
	"context"
	"sync"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/home"
	"github.com/parvezsyed/agentpaas/internal/runtime"
	"github.com/parvezsyed/agentpaas/internal/trigger"
)

// controlServer implements the ControlServiceServer interface by embedding
// UnimplementedControlServiceServer and overriding only the Doctor method with
// a stub response. All other RPCs return Unimplemented via the embedded default
// implementations.
//
// This lets the daemon start, accept connections, and respond to the Doctor
// diagnostic RPC while the remaining methods await real implementations.
type trackedRun struct {
	Container    runtime.ContainerID
	Network      string
	AuditDir     string // host path to harness-audit directory for post-run ingestion
	Status       string // "running" | "succeeded" | "failed" | "cancelled"
	CancelInvoke context.CancelFunc
	InvokeDone   chan struct{} // closed when invoke goroutine exits
	InvokeErr    error         // written before close(InvokeDone); safe to read after channel receive
}

// maxConcurrentRuns is the hard limit on simultaneously active agent runs.
// Enforced in the Run handler before any Docker resources are created.
const maxConcurrentRuns = 3

type controlServer struct {
	controlv1.UnimplementedControlServiceServer

	version     VersionInfo
	auditIndex  *audit.SQLiteIndexer
	auditWriter *audit.AuditWriter
	homePaths   *home.HomePaths
	eventBus    *trigger.EventBus

	runMu        sync.Mutex
	runs         map[string]*trackedRun
	secretMu     sync.Mutex
	secretGrants map[string]map[string]struct{}

	runtimeOnce sync.Once
	runtimeErr  error
	dockerRT    *runtime.DockerRuntime
}

// compile-time interface check.
var _ controlv1.ControlServiceServer = (*controlServer)(nil)

// Doctor returns a stub diagnostic response with version info and a single
// "ok" check indicating the daemon skeleton is running.
func (s *controlServer) Doctor(ctx context.Context, req *controlv1.DoctorRequest) (*controlv1.DoctorResponse, error) {
	checks := []*controlv1.CheckResult{
		{
			Name:    "version",
			Status:  "ok",
			Message: s.version.String(),
		},
		{
			Name:    "daemon_skeleton",
			Status:  "ok",
			Message: "Daemon skeleton is running. Stub implementation — full methods pending.",
		},
	}

	return &controlv1.DoctorResponse{
		Checks:        checks,
		OverallStatus: "ok",
	}, nil
}
