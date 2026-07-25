package pipeline

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ---------------------------------------------------------------------------
// Mock driver for pipeline isolation tests
// ---------------------------------------------------------------------------

// pipelineMockDriver implements runtime.RuntimeDriver with configurable
// behavior and call tracking for test assertions.
type pipelineMockDriver struct {
	mu sync.Mutex

	// Tracked calls.
	createCalls   []runtime.ContainerSpec
	startCalls    []runtime.ContainerID
	stopCalls     []runtime.ContainerID
	removeCalls   []runtime.ContainerID
	listCallCount int

	// Configurable behavior.
	statusFunc      func(id runtime.ContainerID) (runtime.ContainerStatus, error)
	listFunc        func(labelFilters ...string) ([]runtime.ContainerInfo, error)

	// Simulated container IDs (monotonic counter).
	nextCID int
}

func newPipelineMockDriver() *pipelineMockDriver {
	return &pipelineMockDriver{}
}

func (m *pipelineMockDriver) Create(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, spec)
	m.nextCID++
	cid := runtime.ContainerID("mock-cid-" + itoa(m.nextCID))
	return cid, nil
}

func (m *pipelineMockDriver) Start(_ context.Context, id runtime.ContainerID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls = append(m.startCalls, id)
	return nil
}

func (m *pipelineMockDriver) Stop(_ context.Context, id runtime.ContainerID, _ *time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls = append(m.stopCalls, id)
	return nil
}

func (m *pipelineMockDriver) Remove(_ context.Context, id runtime.ContainerID, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeCalls = append(m.removeCalls, id)
	return nil
}

func (m *pipelineMockDriver) Status(_ context.Context, id runtime.ContainerID) (runtime.ContainerStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusFunc != nil {
		return m.statusFunc(id)
	}
	return runtime.ContainerStatusRunning, nil
}

func (m *pipelineMockDriver) ListContainers(_ context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error) {
	m.mu.Lock()
	m.listCallCount++
	m.mu.Unlock()
	if m.listFunc != nil {
		return m.listFunc(labelFilters...)
	}
	return nil, nil
}

// Stub methods for the rest of the RuntimeDriver interface.

func (m *pipelineMockDriver) Stats(_ context.Context, _ runtime.ContainerID) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, nil
}
func (m *pipelineMockDriver) Logs(_ context.Context, _ runtime.ContainerID, _ runtime.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *pipelineMockDriver) Exec(_ context.Context, _ runtime.ContainerID, _ []string) (string, string, int, error) {
	return "", "", 0, nil
}
func (m *pipelineMockDriver) CreateNetwork(_ context.Context, _ runtime.NetworkSpec) (runtime.NetworkID, error) {
	return runtime.NetworkID("net-1"), nil
}
func (m *pipelineMockDriver) RemoveNetwork(_ context.Context, _ runtime.NetworkID) error { return nil }
func (m *pipelineMockDriver) InspectNetwork(_ context.Context, _ runtime.NetworkID) (runtime.NetworkInfo, error) {
	return runtime.NetworkInfo{}, nil
}
func (m *pipelineMockDriver) AttachNetwork(_ context.Context, _ runtime.ContainerID, _ runtime.NetworkID) error {
	return nil
}
func (m *pipelineMockDriver) AttachNetworkWithAliases(_ context.Context, _ runtime.ContainerID, _ runtime.NetworkID, _ []string) error {
	return nil
}
func (m *pipelineMockDriver) DetachNetwork(_ context.Context, _ runtime.ContainerID, _ runtime.NetworkID) error {
	return nil
}
func (m *pipelineMockDriver) InspectContainerNetworks(_ context.Context, _ runtime.ContainerID) ([]runtime.ContainerNetworkInfo, error) {
	return nil, nil
}
func (m *pipelineMockDriver) InspectContainerIP(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
	return "", nil
}
func (m *pipelineMockDriver) ListNetworks(_ context.Context, _ ...string) ([]runtime.NetworkInfo, error) {
	return nil, nil
}

// createCount returns the number of Create calls.
func (m *pipelineMockDriver) createCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.createCalls)
}

// stopCount returns the number of Stop calls.
func (m *pipelineMockDriver) stopCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stopCalls)
}

// removeCount returns the number of Remove calls.
func (m *pipelineMockDriver) removeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.removeCalls)
}

// createdSpecs returns a copy of all created specs.
func (m *pipelineMockDriver) createdSpecs() []runtime.ContainerSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]runtime.ContainerSpec, len(m.createCalls))
	copy(cp, m.createCalls)
	return cp
}

// compile-time check: pipelineMockDriver implements RuntimeDriver.
var _ runtime.RuntimeDriver = (*pipelineMockDriver)(nil)

// ---------------------------------------------------------------------------
// Test 1: BuildStageContainerSpec labels complete
// ---------------------------------------------------------------------------

func TestBuildStageContainerSpec_LabelsComplete(t *testing.T) {
	req := StageLaunchRequest{
		WorkflowID:            "wf-test-1",
		NodeID:                "node-test-1",
		RunID:                 "run-test-1",
		AttemptID:             "at-test-1",
		StageOrder:            0,
		PackageDigest:         "sha256:abc123",
		PolicyDigest:          "sha256:def456",
		Image:                 "agentpaas/agent:latest",
		LeaseGeneration:       1,
		NetworkID:             "net-stage-0",
		ReadOnlyArtifactBinds: []string{"/host/artifacts:/container/artifacts:ro"},
		WritableWorkDirBind:   "/host/work/node-test-1:/work",
	}

	spec, err := BuildStageContainerSpec(req)
	if err != nil {
		t.Fatalf("BuildStageContainerSpec: %v", err)
	}

	// Verify all required labels present.
	checkLabel := func(key, expected string) {
		t.Helper()
		got, ok := spec.Labels[key]
		if !ok {
			t.Errorf("label %q missing from spec.Labels", key)
			return
		}
		if got != expected {
			t.Errorf("label %q = %q, want %q", key, got, expected)
		}
	}

	checkLabel(runtime.LabelManagedBy, "agentpaas")
	checkLabel(runtime.LabelResourceType, "agent")
	checkLabel(runtime.LabelRunID, "run-test-1")
	checkLabel(runtime.LabelWorkflowID, "wf-test-1")
	checkLabel(runtime.LabelNodeID, "node-test-1")
	checkLabel(runtime.LabelAttemptID, "at-test-1")
	checkLabel(runtime.LabelPackageDigest, "sha256:abc123")
	checkLabel(runtime.LabelPolicyDigest, "sha256:def456")
	checkLabel(runtime.LabelLeaseGeneration, "1")
	checkLabel(runtime.LabelPipelineStage, "true")
	checkLabel(runtime.LabelStageOrder, "0")

	// Verify no secret-like keys in labels.
	for k, v := range spec.Labels {
		if len(v) >= 3 && v[:3] == "sk-" {
			t.Errorf("label %q contains secret-like value: %q", k, v)
		}
		if len(v) >= 7 && v[:7] == "api_key" {
			t.Errorf("label %q contains secret-like value: %q", k, v)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: BuildStageContainerSpec single network
// ---------------------------------------------------------------------------

func TestBuildStageContainerSpec_SingleNetwork(t *testing.T) {
	req := StageLaunchRequest{
		WorkflowID: "wf-test-2",
		NodeID:     "node-test-2",
		RunID:      "run-test-2",
		Image:      "agentpaas/agent:latest",
		NetworkID:  "net-stage-solo",
	}

	spec, err := BuildStageContainerSpec(req)
	if err != nil {
		t.Fatalf("BuildStageContainerSpec: %v", err)
	}

	if len(spec.NetworkIDs) != 1 {
		t.Fatalf("expected exactly 1 NetworkID, got %d: %v", len(spec.NetworkIDs), spec.NetworkIDs)
	}
	if spec.NetworkIDs[0] != "net-stage-solo" {
		t.Errorf("NetworkIDs[0] = %q, want %q", spec.NetworkIDs[0], "net-stage-solo")
	}
}

// ---------------------------------------------------------------------------
// Test 3: BuildStageContainerSpec rejects shared writable bind
// ---------------------------------------------------------------------------

func TestBuildStageContainerSpec_RejectsSharedWritableBind(t *testing.T) {
	// Stage 1 uses a writable bind that lacks nodeID/runID.
	req := StageLaunchRequest{
		WorkflowID:          "wf-test-3",
		NodeID:              "node-test-3",
		RunID:               "run-test-3",
		Image:               "agentpaas/agent:latest",
		NetworkID:           "net-1",
		WritableWorkDirBind: "/shared/work:/work", // no nodeID or runID in path
	}

	_, err := BuildStageContainerSpec(req)
	if err == nil {
		t.Fatal("expected error for shared writable bind without nodeID/runID, got nil")
	}
}

// ---------------------------------------------------------------------------
// Test 4: IntersectStageAuthority drops extra hosts
// ---------------------------------------------------------------------------

func TestIntersectStageAuthority_DropsExtraHosts(t *testing.T) {
	workflow := StageAuthority{
		AllowHosts:   []string{"api.example.com", "cdn.example.com"},
		AllowMCP:     []string{"feedback", "search"},
		MaxActiveMs:  60000,
		MaxLLMSpend:  "100.00",
		NetworkEgress: true,
	}

	stage := StageAuthority{
		AllowHosts:   []string{"api.example.com", "secret.internal"},
		AllowMCP:     []string{"feedback"},
		MaxActiveMs:  30000,
		MaxLLMSpend:  "",
		NetworkEgress: true,
	}

	result := IntersectStageAuthority(workflow, stage)

	// Hosts: intersection = only api.example.com.
	if len(result.AllowHosts) != 1 || result.AllowHosts[0] != "api.example.com" {
		t.Errorf("AllowHosts = %v, want [api.example.com]", result.AllowHosts)
	}

	// MCP: intersection = only feedback.
	if len(result.AllowMCP) != 1 || result.AllowMCP[0] != "feedback" {
		t.Errorf("AllowMCP = %v, want [feedback]", result.AllowMCP)
	}

	// MaxActiveMs: min(60000, 30000) = 30000.
	if result.MaxActiveMs != 30000 {
		t.Errorf("MaxActiveMs = %d, want 30000", result.MaxActiveMs)
	}

	// MaxLLMSpend: workflow wins if set.
	if result.MaxLLMSpend != "100.00" {
		t.Errorf("MaxLLMSpend = %q, want %q", result.MaxLLMSpend, "100.00")
	}

	// NetworkEgress: true AND true = true.
	if !result.NetworkEgress {
		t.Error("NetworkEgress = false, want true")
	}
}

// ---------------------------------------------------------------------------
// Test 5: RuntimeStageLauncher EnsureLaunch idempotent
// ---------------------------------------------------------------------------

func TestRuntimeStageLauncher_EnsureLaunchIdempotent(t *testing.T) {
	ctx := context.Background()
	driver := newPipelineMockDriver()
	launcher := NewRuntimeStageLauncher(driver)

	job := &StageLaunchJob{
		Key:           "wf-5|node-5|1",
		WorkflowID:    "wf-5",
		NodeID:        "node-5",
		RunID:         "run-5",
		AttemptID:     "at-5",
		Generation:    1,
		Image:         "agentpaas/agent:latest",
		NetworkID:     "net-stage-5",
		StageOrder:    0,
		Status:        LaunchStatusPending,
	}

	// First call should create + start.
	if err := launcher.EnsureLaunch(ctx, job); err != nil {
		t.Fatalf("first EnsureLaunch: %v", err)
	}
	if driver.createCount() != 1 {
		t.Errorf("expected 1 Create call, got %d", driver.createCount())
	}
	if len(driver.startCalls) != 1 {
		t.Errorf("expected 1 Start call, got %d", len(driver.startCalls))
	}

	// Second call should be idempotent (no new Create).
	if err := launcher.EnsureLaunch(ctx, job); err != nil {
		t.Fatalf("second EnsureLaunch: %v", err)
	}
	if driver.createCount() != 1 {
		t.Errorf("expected still 1 Create call after second EnsureLaunch, got %d", driver.createCount())
	}
}

// ---------------------------------------------------------------------------
// Test 6: FenceStage stops prior before next
// ---------------------------------------------------------------------------

func TestFenceStage_StopsPriorBeforeNext(t *testing.T) {
	ctx := context.Background()
	driver := newPipelineMockDriver()
	launcher := NewRuntimeStageLauncher(driver)

	// First, launch a stage to get a container ID recorded.
	job0 := &StageLaunchJob{
		Key:           "wf-6|node-6|1",
		WorkflowID:    "wf-6",
		NodeID:        "node-6",
		RunID:         "run-6-stage0",
		AttemptID:     "at-6-stage0",
		Generation:    1,
		Image:         "agentpaas/agent:latest",
		NetworkID:     "net-stage-0",
		StageOrder:    0,
		Status:        LaunchStatusPending,
	}
	if err := launcher.EnsureLaunch(ctx, job0); err != nil {
		t.Fatalf("launch stage 0: %v", err)
	}
	cid0 := job0.ContainerID
	if cid0 == "" {
		t.Fatal("expected ContainerID to be set after launch")
	}

	// Set up ListContainers to return the first container with matching labels.
	driver.listFunc = func(labelFilters ...string) ([]runtime.ContainerInfo, error) {
		return []runtime.ContainerInfo{
			{
				ID:     cid0,
				Status: runtime.ContainerStatusRunning,
				Labels: map[string]string{
					runtime.LabelWorkflowID: "wf-6",
					runtime.LabelNodeID:     "node-6",
				},
			},
		}, nil
	}

	// Fence should stop and remove cid0.
	if err := launcher.FenceStage(ctx, "wf-6", "node-6"); err != nil {
		t.Fatalf("FenceStage: %v", err)
	}

	if driver.stopCount() < 1 {
		t.Error("expected at least 1 Stop call after fence")
	}
	if driver.removeCount() < 1 {
		t.Error("expected at least 1 Remove call after fence")
	}

	// Now launch the next stage with a different NetworkID.
	job1 := &StageLaunchJob{
		Key:           "wf-6|node-6|2",
		WorkflowID:    "wf-6",
		NodeID:        "node-6",
		RunID:         "run-6-stage1",
		AttemptID:     "at-6-stage1",
		Generation:    2,
		Image:         "agentpaas/agent:latest",
		NetworkID:     "net-stage-1",
		StageOrder:    1,
		Status:        LaunchStatusPending,
	}
	driver.listFunc = func(labelFilters ...string) ([]runtime.ContainerInfo, error) {
		return nil, nil // no prior containers
	}
	if err := launcher.EnsureLaunch(ctx, job1); err != nil {
		t.Fatalf("launch stage 1: %v", err)
	}

	// The new stage should have a different container ID.
	if job1.ContainerID == cid0 {
		t.Error("stage 1 should have a different ContainerID than stage 0")
	}

	// Verify stage0 spec used net-stage-0, stage1 spec used net-stage-1.
	specs := driver.createdSpecs()
	if len(specs) < 2 {
		t.Fatalf("expected at least 2 created specs, got %d", len(specs))
	}
	if specs[len(specs)-1].NetworkIDs[0] != "net-stage-1" {
		t.Errorf("stage1 NetworkID = %q, want net-stage-1", specs[len(specs)-1].NetworkIDs[0])
	}
}

// ---------------------------------------------------------------------------
// Test 7: Two stage specs have no shared network or RW volume
// ---------------------------------------------------------------------------

func TestTwoStageSpecs_NoSharedNetworkOrRWVolume(t *testing.T) {
	req0 := StageLaunchRequest{
		WorkflowID:          "wf-7",
		NodeID:              "node-7",
		RunID:               "run-7-stage0",
		Image:               "agentpaas/agent:latest",
		NetworkID:           "net-stage-0",
		WritableWorkDirBind: "/host/work/run-7-stage0:/work",
		StageOrder:          0,
	}

	req1 := StageLaunchRequest{
		WorkflowID:          "wf-7",
		NodeID:              "node-7",
		RunID:               "run-7-stage1",
		Image:               "agentpaas/agent:v2",
		NetworkID:           "net-stage-1",
		WritableWorkDirBind: "/host/work/run-7-stage1:/work",
		StageOrder:          1,
	}

	spec0, err := BuildStageContainerSpec(req0)
	if err != nil {
		t.Fatalf("stage0 BuildStageContainerSpec: %v", err)
	}
	spec1, err := BuildStageContainerSpec(req1)
	if err != nil {
		t.Fatalf("stage1 BuildStageContainerSpec: %v", err)
	}

	// Different NetworkIDs.
	if spec0.NetworkIDs[0] == spec1.NetworkIDs[0] {
		t.Errorf("stage0 and stage1 should have different NetworkIDs, both are %q", spec0.NetworkIDs[0])
	}

	// Different RW binds.
	for _, b0 := range spec0.Binds {
		for _, b1 := range spec1.Binds {
			// If bind doesn't end with :ro, it's RW — must differ.
			if !isReadOnly(b0) && !isReadOnly(b1) && b0 == b1 {
				t.Errorf("stage0 and stage1 share same RW bind: %q", b0)
			}
		}
	}

	// Distinct labels for stage order.
	if spec0.Labels[runtime.LabelStageOrder] == spec1.Labels[runtime.LabelStageOrder] {
		t.Error("stage0 and stage1 should have different stage order labels")
	}
}

func isReadOnly(bind string) bool {
	return len(bind) > 3 && bind[len(bind)-3:] == ":ro"
}

// ---------------------------------------------------------------------------
// Test 8: Labels reject secrets
// ---------------------------------------------------------------------------

func TestLabelsRejectSecrets(t *testing.T) {
	// Attempt to put a secret-like value via package digest.
	// The PipelineStageLabels function passes values through as-is
	// but BuildStageContainerSpec calls it. The builder itself does not
	// filter env — it puts Env on the spec directly. The spec says:
	// "Builder strips secret-like env from being copied to labels".
	// Since labels are constructed only from PipelineStageLabels which
	// takes explicit structured fields (not env), secret env values are
	// never copied to labels by design.

	req := StageLaunchRequest{
		WorkflowID: "wf-8",
		NodeID:     "node-8",
		RunID:      "run-8",
		Image:      "agentpaas/agent:latest",
		NetworkID:  "net-8",
		Env:        []string{"OPENAI_API_KEY=sk-secret123", "NORMAL_VAR=hello"},
	}

	spec, err := BuildStageContainerSpec(req)
	if err != nil {
		t.Fatalf("BuildStageContainerSpec: %v", err)
	}

	// Env should be passed through as-is (not secret-stripped from Env).
	foundAPIKey := false
	for _, e := range spec.Env {
		if e == "OPENAI_API_KEY=sk-secret123" {
			foundAPIKey = true
		}
	}
	if !foundAPIKey {
		t.Error("Env should contain OPENAI_API_KEY (stripping secrets is for labels, not Env)")
	}

	// Labels should NOT contain the secret values.
	for k, v := range spec.Labels {
		if len(v) >= 3 && v[:3] == "sk-" {
			t.Errorf("label %q contains secret-like value: %q", k, v)
		}
		if len(v) >= 7 && v[:7] == "api_key" {
			t.Errorf("label %q contains secret-like value: %q", k, v)
		}
	}
}
