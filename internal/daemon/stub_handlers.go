package daemon

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"sync"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// controlServer implements the ControlServiceServer interface by embedding
// UnimplementedControlServiceServer and overriding only the Doctor method with
// a stub response. All other RPCs return Unimplemented via the embedded default
// implementations.
//
// This lets the daemon start, accept connections, and respond to the Doctor
// diagnostic RPC while the remaining methods await real implementations.
type trackedRun struct {
	Container     runtime.ContainerID
	Network       string // internal network ID
	EgressNetwork string // egress network ID
	Gateway       runtime.ContainerID // gateway container ID (empty if no gateway)
	AuditDir          string // host path to harness-audit directory for post-run ingestion
	GatewayConfigDir  string // per-run gateway config dir (compiled from agent policy or default-deny)
	AgentName     string
	StartedAt     time.Time
	Status        string              // "running" | "succeeded" | "failed" | "cancelled"
	FailReason    string              // reason for failure (empty if not failed)
	CancelInvoke  context.CancelFunc
	InvokeDone    chan struct{} // closed when invoke goroutine exits
	InvokeErr     error         // written before close(InvokeDone); safe to read after channel receive
	InvokeResponse string       // raw stdout from the invoke command (agent's response payload)
	Tailer        *auditTailer    // real-time audit tailer (nil if not running)
	ProgressTailer *routedrun.ProgressTailer // B27: progress journal tailer
	JournalKeyPath string                    // host path to journal key file for cleanup
	ArtifactDir    string                    // host path to artifact workspace dir
	JournalHostPath string                   // host path to journal file for tailer

	// TimeEnvelope is the authoritative active-time envelope (B30-T03 Part B,
	// ceiling 1). When present (set by the durable admission path after
	// InvokeDeployment), the daemon's invoke-context timeout is derived from
	// env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs). When nil
	// (legacy v0.2.3 trigger path), the legacy 2-minute fallback applies.
	TimeEnvelope *routedrun.TimeEnvelope

	finalizeOnce  sync.Once       // ensures finalizeRun runs exactly once per run
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

	auditCheckpointPubKey *ecdsa.PublicKey
	auditCheckpointsPath  string

	runMu        sync.Mutex
	runs         map[string]*trackedRun
	secretMu     sync.Mutex
	secretGrants map[string]map[string]struct{}

	// secretStoreForTest is a SecretStore override for unit tests. When non-nil,
	// buildInvokePayload uses this instead of creating a real KeychainStore.
	// This field is NOT accessed outside tests and is NEVER set in production.
	secretStoreForTest secrets.SecretStore

	// cronScheduler manages cron-triggered agent invocations.
	cronScheduler *trigger.CronScheduler

	runtimeOnce sync.Once
	runtimeErr  error
	dockerRT    *runtime.DockerRuntime

	// B26 routed-run stores (state foundation). Initialized in Start via
	// initRoutedStores. Deployment/alias CRUD is enabled; invocation/control
	// fail closed until B28/B35.
	localStore       *routedrun.LocalStore
	deploymentStore  routedrun.DeploymentStore
	runStore         routedrun.RunStore
	workflowStore    routedrun.WorkflowStore

	// disableContainerLaunch prevents startDurableRun from launching
	// Docker containers. Set by unit tests that don't have a Docker
	// runtime available. Production daemons always leave this false.
	disableContainerLaunch bool

	// testRuntime is a RuntimeDriver injected by unit tests. When set
	// (non-nil), getOrCreateRuntime returns a DockerRuntime that delegates
	// all operations to testRuntime instead of connecting to a live Docker
	// daemon. Production daemons always leave this nil.
	testRuntime runtime.RuntimeDriver

	// mcpRegistry provides the MCP service registry for managed service
	// routing. When nil, MCP service routing fails closed with B33.
	mcpRegistry *mcpmanager.ServiceRegistry

	// pipelineEnabled controls the B34 pipeline runtime. When true or when
	// AGENTPAAS_PIPELINE_ENABLED=1, pipeline deployments are admitted.
	pipelineEnabled bool
}

// SetMCPServiceRegistry sets the MCP service registry hook on the daemon.
// When set (non-nil), managed MCP service requests are allowed through the
// fail-closed gate. Thread-safe; matches existing server patterns.
func (s *controlServer) SetMCPServiceRegistry(reg *mcpmanager.ServiceRegistry) {
	s.mcpRegistry = reg
}

// pipelineStore returns the underlying routed-run LocalStore suitable for
// use as a pipeline.PipelineStore. Returns nil when stores are not
// initialized (e.g. before initRoutedStores).
func (s *controlServer) pipelineStore() *routedrun.LocalStore {
	if s == nil {
		return nil
	}
	return s.localStore
}

// EnsureWorkflowMCPServices declares and starts every service binding for a
// workflow. Requires s.mcpRegistry != nil. Idempotent Start on already-READY
// services. On first failure, best-effort Stop/cleanup already-started
// services in this call and return error.
func (s *controlServer) EnsureWorkflowMCPServices(ctx context.Context, workflowID string, services []pack.ServiceBinding) error {
	if s.mcpRegistry == nil {
		return fmt.Errorf("agentpaas mcp service not enabled")
	}
	return s.mcpRegistry.EnsureServices(ctx, workflowID, services)
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
