package mcpmanager

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

func TestGenerateStatusReportManagerOnly(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{
			Name:         "stdio-main",
			Transport:    "stdio",
			AllowedTools: []string{"search", "read"},
		},
	}, "agent-1", "run-1")

	report, err := GenerateStatusReport(context.Background(), manager, nil, nil)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	if len(report.Resources) != 1 {
		t.Fatalf("resources length = %d, want 1", len(report.Resources))
	}
	if len(report.Sidecars) != 0 {
		t.Fatalf("sidecars length = %d, want 0", len(report.Sidecars))
	}
	resource := report.Resources[0]
	if resource.ResourceType != "mcp_server" || resource.ServerID != "stdio-main" {
		t.Fatalf("resource identity = (%q, %q), want (mcp_server, stdio-main)", resource.ResourceType, resource.ServerID)
	}
	if resource.AgentID != "agent-1" || resource.RunID != "run-1" {
		t.Fatalf("resource owner = (%q, %q), want (agent-1, run-1)", resource.AgentID, resource.RunID)
	}
	if resource.Readiness != ReadinessStopped || resource.Health != HealthUnknown {
		t.Fatalf("resource state = (%q, %q), want (%q, %q)", resource.Readiness, resource.Health, ReadinessStopped, HealthUnknown)
	}
}

func TestGenerateStatusReportNilDriverOmitsSidecars(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{
			Name:         "http-main",
			Transport:    "http",
			URL:          "http://example.invalid/mcp",
			AllowedTools: []string{"list"},
		},
	}, "agent-1", "run-1")

	report, err := GenerateStatusReport(context.Background(), manager, nil, nil)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	if report.Sidecars != nil {
		t.Fatalf("sidecars = %#v, want nil", report.Sidecars)
	}
}

func TestGenerateStatusReportIncludesResourceFieldsAndLifecycleError(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{
			Name:         "stdio-crashed",
			Transport:    "stdio",
			AllowedTools: []string{"tool-a"},
		},
	}, "agent-9", "run-9")
	lifecycle := NewLifecycle(manager, nil, "")
	crashedAt := time.Now().UTC()
	lifecycle.procState["stdio-crashed"] = &stdioState{
		done:     closedDone(),
		exitCode: 2,
		exitTime: crashedAt,
		err:      io.ErrUnexpectedEOF,
	}

	report, err := GenerateStatusReport(context.Background(), manager, lifecycle, nil)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	resource := report.Resources[0]
	if resource.ResourceType != "mcp_server" {
		t.Fatalf("resource_type = %q, want mcp_server", resource.ResourceType)
	}
	if resource.ServerID != "stdio-crashed" {
		t.Fatalf("server_id = %q, want stdio-crashed", resource.ServerID)
	}
	if resource.PolicyDigest == "" {
		t.Fatal("policy_digest is empty")
	}
	if got := strings.Join(resource.AllowedTools, ","); got != "tool-a" {
		t.Fatalf("allowed_tools = %q, want tool-a", got)
	}
	if resource.Readiness != ReadinessUnhealthy || resource.Health != HealthFailed {
		t.Fatalf("state = (%q, %q), want (%q, %q)", resource.Readiness, resource.Health, ReadinessUnhealthy, HealthFailed)
	}
	if !strings.Contains(resource.LastError, io.ErrUnexpectedEOF.Error()) {
		t.Fatalf("last_error = %q, want unexpected EOF", resource.LastError)
	}
}

func TestGenerateStatusReportRepresentsLifecycleStates(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{Name: "stopped", Transport: "stdio"},
		{Name: "starting", Transport: "stdio"},
		{Name: "ready", Transport: "stdio"},
		{Name: "unhealthy", Transport: "stdio"},
	}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	manager.setReadiness("starting", ReadinessStarting)
	manager.setReadiness("ready", ReadinessReady)
	lifecycle.procState["ready"] = &stdioState{done: make(chan struct{})}
	lifecycle.procState["unhealthy"] = &stdioState{
		done:     closedDone(),
		exitCode: 1,
		exitTime: time.Now().UTC(),
		err:      io.ErrClosedPipe,
	}

	report, err := GenerateStatusReport(context.Background(), manager, lifecycle, nil)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	states := map[string]Resource{}
	for _, resource := range report.Resources {
		states[resource.ServerID] = resource
	}
	if states["stopped"].Readiness != ReadinessStopped {
		t.Fatalf("stopped readiness = %q, want %q", states["stopped"].Readiness, ReadinessStopped)
	}
	if states["starting"].Readiness != ReadinessStarting {
		t.Fatalf("starting readiness = %q, want %q", states["starting"].Readiness, ReadinessStarting)
	}
	if states["ready"].Readiness != ReadinessReady || states["ready"].Health != HealthHealthy {
		t.Fatalf("ready state = (%q, %q), want (%q, %q)", states["ready"].Readiness, states["ready"].Health, ReadinessReady, HealthHealthy)
	}
	if states["unhealthy"].Readiness != ReadinessUnhealthy || states["unhealthy"].Health != HealthFailed {
		t.Fatalf("unhealthy state = (%q, %q), want (%q, %q)", states["unhealthy"].Readiness, states["unhealthy"].Health, ReadinessUnhealthy, HealthFailed)
	}
}

func TestGenerateStatusReportSidecarInfo(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{
			Name:         "http-main",
			Transport:    "http",
			URL:          "http://example.invalid/mcp",
			AllowedTools: []string{"list"},
		},
	}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["container-1"] = runtime.ContainerInfo{
		ID:           "container-1",
		Status:       runtime.ContainerStatusRunning,
		ResourceType: runtime.ResourceTypeMCP,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelRunID:        "run-1",
			runtime.LabelMCPServerID:  "http-main",
			"credential_ref":          "secret://should-not-leak",
		},
	}
	driver.stats["container-1"] = runtime.ContainerStats{CPUPercent: 12.5, MemoryMB: 64}
	driver.networks["container-1"] = []runtime.ContainerNetworkInfo{{ID: "net-1", Name: "agentpaas-net-internal-run-1"}}
	driver.artifacts["container-1"] = containerArtifactMetadata{ImageDigest: "sha256:abc123", RestartCount: 3}

	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	if len(report.Sidecars) != 1 {
		t.Fatalf("sidecars length = %d, want 1", len(report.Sidecars))
	}
	sidecar := report.Sidecars[0]
	if sidecar.ServerID != "http-main" || sidecar.ContainerID != "container-1" {
		t.Fatalf("sidecar identity = (%q, %q), want (http-main, container-1)", sidecar.ServerID, sidecar.ContainerID)
	}
	if sidecar.ImageDigest != "sha256:abc123" || sidecar.RestartCount != 3 {
		t.Fatalf("artifact metadata = (%q, %d), want (sha256:abc123, 3)", sidecar.ImageDigest, sidecar.RestartCount)
	}
	if sidecar.MemoryBytes != 64*1024*1024 || sidecar.CPUPercent != 12.5 {
		t.Fatalf("stats = (%d, %f), want (%d, 12.5)", sidecar.MemoryBytes, sidecar.CPUPercent, int64(64*1024*1024))
	}
	if got := strings.Join(sidecar.Networks, ","); got != "agentpaas-net-internal-run-1" {
		t.Fatalf("networks = %q, want agentpaas-net-internal-run-1", got)
	}
	if sidecar.Health != HealthHealthy || sidecar.Readiness != ReadinessReady {
		t.Fatalf("state = (%q, %q), want (%q, %q)", sidecar.Health, sidecar.Readiness, HealthHealthy, ReadinessReady)
	}
	if _, ok := sidecar.Labels["credential_ref"]; ok {
		t.Fatal("sidecar labels leaked credential_ref")
	}
	if sidecar.Labels[runtime.LabelMCPServerID] != "http-main" {
		t.Fatalf("mcp server label = %q, want http-main", sidecar.Labels[runtime.LabelMCPServerID])
	}
}

func TestGenerateStatusReportDoesNotLeakSecrets(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{
			Name:      "http-secret",
			Transport: "http",
			URL:       "http://user:raw-password@example.invalid/mcp",
			Headers: map[string]string{
				"Authorization": "Bearer raw-token",
			},
			Env: map[string]string{
				"API_TOKEN": "raw-token",
			},
			AllowedTools: []string{"safe"},
		},
	}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["container-1"] = runtime.ContainerInfo{
		ID: "container-1",
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelMCPServerID:  "http-secret",
			runtime.LabelRunID:        "run-1",
			"API_TOKEN":               "raw-token",
		},
	}

	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, forbidden := range []string{"raw-password", "raw-token", "Authorization", "API_TOKEN"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("status report leaked %q in %s", forbidden, payload)
		}
	}
}

func TestGenerateStatusReportLifecycleReadiness(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{Name: "stdio-ready", Transport: "stdio"}}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	lifecycle.procState["stdio-ready"] = &stdioState{done: make(chan struct{})}

	report, err := GenerateStatusReport(context.Background(), manager, lifecycle, nil)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	if report.Resources[0].Readiness != ReadinessReady || report.Resources[0].Health != HealthHealthy {
		t.Fatalf("state = (%q, %q), want (%q, %q)", report.Resources[0].Readiness, report.Resources[0].Health, ReadinessReady, HealthHealthy)
	}
}

func TestGenerateStatusReportTimestampIsRecent(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{Name: "stdio-main", Transport: "stdio"}}, "agent-1", "run-1")
	before := time.Now().UTC()
	report, err := GenerateStatusReport(context.Background(), manager, nil, nil)
	if err != nil {
		t.Fatalf("GenerateStatusReport() error = %v", err)
	}
	after := time.Now().UTC()
	if report.Generated.Before(before) || report.Generated.After(after.Add(time.Second)) {
		t.Fatalf("generated_at = %s, want between %s and %s", report.Generated, before, after)
	}
}

func TestMCPStatusJSON(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{Name: "stdio-main", Transport: "stdio"}}, "agent-1", "run-1")

	payload, err := MCPStatusJSON(context.Background(), manager, nil, nil)
	if err != nil {
		t.Fatalf("MCPStatusJSON() error = %v", err)
	}
	if !strings.Contains(string(payload), `"server_id":"stdio-main"`) {
		t.Fatalf("payload = %s, want server_id", payload)
	}
}

func TestGenerateStatusReportConcurrent(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{
		{Name: "http-main", Transport: "http"},
		{Name: "stdio-main", Transport: "stdio"},
	}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["container-1"] = runtime.ContainerInfo{
		ID:     "container-1",
		Status: runtime.ContainerStatusRunning,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelMCPServerID:  "http-main",
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := GenerateStatusReport(context.Background(), manager, nil, driver); err != nil {
				t.Errorf("GenerateStatusReport() error = %v", err)
			}
		}()
	}
	wg.Wait()
}

func closedDone() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

type statusFakeRuntimeDriver struct {
	mu         sync.RWMutex
	containers map[string]runtime.ContainerInfo
	stats      map[string]runtime.ContainerStats
	networks   map[string][]runtime.ContainerNetworkInfo
	artifacts  map[string]containerArtifactMetadata
}

func newStatusFakeRuntimeDriver() *statusFakeRuntimeDriver {
	return &statusFakeRuntimeDriver{
		containers: map[string]runtime.ContainerInfo{},
		stats:      map[string]runtime.ContainerStats{},
		networks:   map[string][]runtime.ContainerNetworkInfo{},
		artifacts:  map[string]containerArtifactMetadata{},
	}
}

func (d *statusFakeRuntimeDriver) Create(context.Context, runtime.ContainerSpec) (runtime.ContainerID, error) {
	return "", nil
}

func (d *statusFakeRuntimeDriver) Start(context.Context, runtime.ContainerID) error {
	return nil
}

func (d *statusFakeRuntimeDriver) Stop(context.Context, runtime.ContainerID, *time.Duration) error {
	return nil
}

func (d *statusFakeRuntimeDriver) Remove(context.Context, runtime.ContainerID, bool) error {
	return nil
}

func (d *statusFakeRuntimeDriver) Status(_ context.Context, id runtime.ContainerID) (runtime.ContainerStatus, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	info := d.containers[string(id)]
	return info.Status, nil
}

func (d *statusFakeRuntimeDriver) Stats(_ context.Context, id runtime.ContainerID) (runtime.ContainerStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stats[string(id)], nil
}

func (d *statusFakeRuntimeDriver) Logs(context.Context, runtime.ContainerID, runtime.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (d *statusFakeRuntimeDriver) Exec(context.Context, runtime.ContainerID, []string) (string, string, int, error) {
	return "", "", 0, nil
}

func (d *statusFakeRuntimeDriver) CreateNetwork(context.Context, runtime.NetworkSpec) (runtime.NetworkID, error) {
	return "", nil
}

func (d *statusFakeRuntimeDriver) RemoveNetwork(context.Context, runtime.NetworkID) error {
	return nil
}

func (d *statusFakeRuntimeDriver) InspectNetwork(context.Context, runtime.NetworkID) (runtime.NetworkInfo, error) {
	return runtime.NetworkInfo{}, nil
}

func (d *statusFakeRuntimeDriver) InspectContainerNetworks(_ context.Context, id runtime.ContainerID) ([]runtime.ContainerNetworkInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	networks := d.networks[string(id)]
	result := make([]runtime.ContainerNetworkInfo, len(networks))
	copy(result, networks)
	return result, nil
}

func (d *statusFakeRuntimeDriver) ListContainers(_ context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]runtime.ContainerInfo, 0, len(d.containers))
	for _, container := range d.containers {
		if labelsMatch(container.Labels, labelFilters) {
			result = append(result, container)
		}
	}
	return result, nil
}

func (d *statusFakeRuntimeDriver) ListNetworks(context.Context, ...string) ([]runtime.NetworkInfo, error) {
	return nil, nil
}

func (d *statusFakeRuntimeDriver) InspectContainerArtifact(_ context.Context, id runtime.ContainerID) (containerArtifactMetadata, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.artifacts[string(id)], nil
}

func labelsMatch(labels map[string]string, filters []string) bool {
	for _, filter := range filters {
		key, value, ok := strings.Cut(filter, "=")
		if !ok {
			return false
		}
		if labels[key] != value {
			return false
		}
	}
	return true
}
