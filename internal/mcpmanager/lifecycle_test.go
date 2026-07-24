package mcpmanager

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

func TestLifecycleStartStdioMCPServer(t *testing.T) {
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "stdio",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 60"},
	}}, nil)
	defer func() { _ = lc.StopAll(context.Background()) }()

	if err := lc.Start(context.Background(), "stdio", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !lc.IsRunning("stdio") {
		t.Fatal("IsRunning() = false, want true")
	}
}

func TestLifecycleStartHTTPMCPServerCreatesSidecar(t *testing.T) {
	driver := newFakeRuntimeDriver()
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "http",
		Transport: "http",
		URL:       "busybox:latest",
		Env:       map[string]string{"MCP_MODE": "test"},
	}}, driver)

	if err := lc.Start(context.Background(), "http", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	spec := driver.createdSpec("cid-1")
	if got := spec.Labels[runtime.LabelManagedBy]; got != runtime.ManagedByValue {
		t.Fatalf("managed-by label = %q, want %q", got, runtime.ManagedByValue)
	}
	if got := spec.Labels[runtime.LabelResourceType]; got != runtime.ResourceTypeMCP {
		t.Fatalf("resource-type label = %q, want %q", got, runtime.ResourceTypeMCP)
	}
	if got := spec.Labels[runtime.LabelMCPServerID]; got != "http" {
		t.Fatalf("mcp server label = %q, want http", got)
	}
	if len(spec.NetworkIDs) != 1 || spec.NetworkIDs[0] != "net-1" {
		t.Fatalf("NetworkIDs = %#v, want [net-1]", spec.NetworkIDs)
	}
	for _, networkID := range spec.NetworkIDs {
		if networkID == "host" {
			t.Fatal("MCP sidecar used host networking")
		}
	}
}

func TestLifecycleCheckReadinessStdioAlive(t *testing.T) {
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "stdio",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 60"},
	}}, nil)
	defer func() { _ = lc.StopAll(context.Background()) }()

	if err := lc.Start(context.Background(), "stdio", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := lc.CheckReadiness(context.Background(), "stdio", time.Second); err != nil {
		t.Fatalf("CheckReadiness() error = %v", err)
	}
}

func TestLifecycleStopStdio(t *testing.T) {
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "stdio",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 60"},
	}}, nil)

	if err := lc.Start(context.Background(), "stdio", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := lc.Stop(context.Background(), "stdio"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if lc.IsRunning("stdio") {
		t.Fatal("IsRunning() = true, want false")
	}
}

func TestLifecycleStopHTTP(t *testing.T) {
	driver := newFakeRuntimeDriver()
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "http",
		Transport: "http",
		URL:       "busybox:latest",
	}}, driver)

	if err := lc.Start(context.Background(), "http", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := lc.Stop(context.Background(), "http"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !driver.removed("cid-1") {
		t.Fatal("HTTP sidecar was not removed")
	}
	if lc.IsRunning("http") {
		t.Fatal("IsRunning() = true, want false")
	}
}

func TestLifecycleStopAll(t *testing.T) {
	driver := newFakeRuntimeDriver()
	lc := newTestLifecycle([]policy.MCPServer{
		{
			Name:      "stdio",
			Transport: "stdio",
			Command:   "/bin/sh",
			Args:      []string{"-c", "sleep 60"},
		},
		{
			Name:      "http",
			Transport: "http",
			URL:       "busybox:latest",
		},
	}, driver)

	if err := lc.Start(context.Background(), "stdio", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start(stdio) error = %v", err)
	}
	if err := lc.Start(context.Background(), "http", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start(http) error = %v", err)
	}

	if err := lc.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}
	if lc.IsRunning("stdio") || lc.IsRunning("http") {
		t.Fatal("servers still running after StopAll")
	}
}

func TestLifecycleCrashContextStdio(t *testing.T) {
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "crash",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", "exit 7"},
	}}, nil)

	if err := lc.Start(context.Background(), "crash", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForCrashContext(t, lc, "crash")

	crash := lc.CrashContext("crash")
	if crash == nil {
		t.Fatal("CrashContext() = nil, want context")
	}
	if crash.ServerID != "crash" || crash.Transport != "stdio" || crash.ExitCode != 7 || crash.Recoverable {
		t.Fatalf("CrashContext() = %#v", crash)
	}
}

func TestLifecycleCrashContextHTTP(t *testing.T) {
	driver := newFakeRuntimeDriver()
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "http",
		Transport: "http",
		URL:       "busybox:latest",
	}}, driver)

	if err := lc.Start(context.Background(), "http", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	driver.setStatus("cid-1", runtime.ContainerStatusStopped)

	crash := lc.CrashContext("http")
	if crash == nil {
		t.Fatal("CrashContext() = nil, want context")
	}
	if crash.ServerID != "http" || crash.Transport != "http" || crash.ExitCode != -1 || !crash.Recoverable {
		t.Fatalf("CrashContext() = %#v", crash)
	}
}

func TestLifecycleStartUndeclaredServer(t *testing.T) {
	lc := newTestLifecycle(nil, nil)

	if err := lc.Start(context.Background(), "missing", "agent-1", "run-1"); err == nil {
		t.Fatal("Start() error = nil, want error")
	}
}

func TestLifecycleStartAlreadyRunningServer(t *testing.T) {
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "stdio",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 60"},
	}}, nil)
	defer func() { _ = lc.StopAll(context.Background()) }()

	if err := lc.Start(context.Background(), "stdio", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := lc.Start(context.Background(), "stdio", "agent-1", "run-1"); err == nil {
		t.Fatal("second Start() error = nil, want error")
	}
}

func TestLifecycleStartHTTPNilDriver(t *testing.T) {
	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "http",
		Transport: "http",
		URL:       "busybox:latest",
	}}, nil)

	err := lc.Start(context.Background(), "http", "agent-1", "run-1")
	if err == nil || !strings.Contains(err.Error(), "requires Docker runtime") {
		t.Fatalf("Start() error = %v, want Docker runtime error", err)
	}
}

func TestLifecycleStdioMinimalEnv(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "env-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	outPath := out.Name()
	defer func() { _ = out.Close() }()
	t.Setenv("AGENTPAAS_DAEMON_SECRET", "must-not-leak")

	lc := newTestLifecycle([]policy.MCPServer{{
		Name:      "env",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", "env > \"$OUT\"; sleep 60"},
		Env:       map[string]string{"OUT": outPath, "DECLARED_ENV": "present"},
	}}, nil)
	defer func() { _ = lc.StopAll(context.Background()) }()

	if err := lc.Start(context.Background(), "env", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForFileContent(t, outPath)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	env := string(data)
	if strings.Contains(env, "AGENTPAAS_DAEMON_SECRET=must-not-leak") {
		t.Fatalf("stdio env leaked daemon environment:\n%s", env)
	}
	if !strings.Contains(env, "PATH=/usr/local/bin:/usr/bin:/bin") {
		t.Fatalf("stdio env missing minimal PATH:\n%s", env)
	}
	if !strings.Contains(env, "DECLARED_ENV=present") {
		t.Fatalf("stdio env missing declared env:\n%s", env)
	}
}

func newTestLifecycle(servers []policy.MCPServer, driver runtime.RuntimeDriver) *Lifecycle {
	manager := NewManager()
	manager.Register(servers, "agent-1", "run-1")
	return NewLifecycle(manager, driver, "net-1")
}

func waitForCrashContext(t *testing.T, lc *Lifecycle, serverID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lc.CrashContext(serverID) != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for crash context")
}

func waitForFileContent(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for env output")
}

type fakeRuntimeDriver struct {
	mu         sync.Mutex
	nextID     int
	specs      map[runtime.ContainerID]runtime.ContainerSpec
	statuses   map[runtime.ContainerID]runtime.ContainerStatus
	removedIDs map[runtime.ContainerID]bool
	networks   map[runtime.NetworkID]runtime.NetworkSpec
	failCreate bool // when true, Create returns an error
}

func newFakeRuntimeDriver() *fakeRuntimeDriver {
	return &fakeRuntimeDriver{
		specs:      make(map[runtime.ContainerID]runtime.ContainerSpec),
		statuses:   make(map[runtime.ContainerID]runtime.ContainerStatus),
		removedIDs: make(map[runtime.ContainerID]bool),
		networks:   make(map[runtime.NetworkID]runtime.NetworkSpec),
	}
}

func (d *fakeRuntimeDriver) Create(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failCreate {
		return "", errors.New("fake create failure")
	}
	d.nextID++
	id := runtime.ContainerID("cid-" + string(rune('0'+d.nextID)))
	d.specs[id] = spec
	d.statuses[id] = runtime.ContainerStatusStopped
	return id, nil
}

func (d *fakeRuntimeDriver) Start(_ context.Context, id runtime.ContainerID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.statuses[id]; !ok {
		return runtime.ErrContainerNotFound
	}
	d.statuses[id] = runtime.ContainerStatusRunning
	return nil
}

func (d *fakeRuntimeDriver) Stop(_ context.Context, id runtime.ContainerID, _ *time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.statuses[id]; !ok {
		return runtime.ErrContainerNotFound
	}
	d.statuses[id] = runtime.ContainerStatusStopped
	return nil
}

func (d *fakeRuntimeDriver) Remove(_ context.Context, id runtime.ContainerID, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.statuses[id]; !ok {
		return runtime.ErrContainerNotFound
	}
	d.statuses[id] = runtime.ContainerStatusRemoved
	d.removedIDs[id] = true
	return nil
}

func (d *fakeRuntimeDriver) Status(_ context.Context, id runtime.ContainerID) (runtime.ContainerStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	status, ok := d.statuses[id]
	if !ok {
		return runtime.ContainerStatusUnknown, runtime.ErrContainerNotFound
	}
	return status, nil
}

func (d *fakeRuntimeDriver) Stats(context.Context, runtime.ContainerID) (runtime.ContainerStats, error) {
	return runtime.ContainerStats{}, errors.New("not implemented")
}

func (d *fakeRuntimeDriver) Logs(context.Context, runtime.ContainerID, runtime.LogOptions) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (d *fakeRuntimeDriver) Exec(context.Context, runtime.ContainerID, []string) (string, string, int, error) {
	return "", "", -1, errors.New("not implemented")
}

func (d *fakeRuntimeDriver) CreateNetwork(_ context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextID++
	id := runtime.NetworkID("net-" + string(rune('0'+d.nextID)))
	d.networks[id] = spec
	return id, nil
}

func (d *fakeRuntimeDriver) RemoveNetwork(_ context.Context, id runtime.NetworkID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.networks, id)
	return nil
}

func (d *fakeRuntimeDriver) InspectNetwork(_ context.Context, id runtime.NetworkID) (runtime.NetworkInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	spec, ok := d.networks[id]
	if !ok {
		return runtime.NetworkInfo{}, runtime.ErrNetworkNotFound
	}
	return runtime.NetworkInfo{
		ID:       string(id),
		Name:     spec.Name,
		Internal: spec.Internal,
		Labels:   spec.Labels,
	}, nil
}

func (d *fakeRuntimeDriver) AttachNetwork(_ context.Context, containerID runtime.ContainerID, networkID runtime.NetworkID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	spec, ok := d.specs[containerID]
	if !ok {
		return runtime.ErrContainerNotFound
	}
	// Track attachment by appending network ID to spec.NetworkIDs.
	netStr := string(networkID)
	for _, nid := range spec.NetworkIDs {
		if nid == netStr {
			return nil // already attached
		}
	}
	spec.NetworkIDs = append(spec.NetworkIDs, netStr)
	d.specs[containerID] = spec
	return nil
}

func (d *fakeRuntimeDriver) DetachNetwork(_ context.Context, containerID runtime.ContainerID, networkID runtime.NetworkID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	spec, ok := d.specs[containerID]
	if !ok {
		return runtime.ErrContainerNotFound
	}
	netStr := string(networkID)
	filtered := make([]string, 0, len(spec.NetworkIDs))
	for _, nid := range spec.NetworkIDs {
		if nid != netStr {
			filtered = append(filtered, nid)
		}
	}
	spec.NetworkIDs = filtered
	d.specs[containerID] = spec
	return nil
}

func (d *fakeRuntimeDriver) ListContainers(_ context.Context, _ ...string) ([]runtime.ContainerInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	containers := make([]runtime.ContainerInfo, 0, len(d.specs))
	for id, spec := range d.specs {
		containers = append(containers, runtime.ContainerInfo{
			ID:           string(id),
			Status:       d.statuses[id],
			Labels:       spec.Labels,
			RunID:        spec.Labels[runtime.LabelRunID],
			ResourceType: spec.Labels[runtime.LabelResourceType],
		})
	}
	return containers, nil
}

func (d *fakeRuntimeDriver) ListNetworks(_ context.Context, labelFilters ...string) ([]runtime.NetworkInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []runtime.NetworkInfo
	for id, spec := range d.networks {
		if !labelsMatchSpec(spec.Labels, labelFilters) {
			continue
		}
		result = append(result, runtime.NetworkInfo{
			ID:       string(id),
			Name:     spec.Name,
			Internal: spec.Internal,
			Labels:   spec.Labels,
		})
	}
	return result, nil
}

func (d *fakeRuntimeDriver) InspectContainerNetworks(_ context.Context, id runtime.ContainerID) ([]runtime.ContainerNetworkInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	spec, ok := d.specs[id]
	if !ok {
		return nil, runtime.ErrContainerNotFound
	}
	var result []runtime.ContainerNetworkInfo
	for _, netID := range spec.NetworkIDs {
		result = append(result, runtime.ContainerNetworkInfo{
			ID: netID,
		})
	}
	return result, nil
}

func (d *fakeRuntimeDriver) InspectContainerIP(context.Context, runtime.ContainerID, string) (string, error) {
	return "", errors.New("not implemented")
}

func (d *fakeRuntimeDriver) createdSpec(id runtime.ContainerID) runtime.ContainerSpec {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.specs[id]
}

func (d *fakeRuntimeDriver) removed(id runtime.ContainerID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.removedIDs[id]
}

func (d *fakeRuntimeDriver) setStatus(id runtime.ContainerID, status runtime.ContainerStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statuses[id] = status
}

// labelsMatchSpec checks whether a set of labels matches the given label
// filters. Each filter is a "key=value" pair.
func labelsMatchSpec(labels map[string]string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		key, value, ok := splitLabelFilter(filter)
		if !ok {
			return false
		}
		if labels[key] != value {
			return false
		}
	}
	return true
}

func splitLabelFilter(filter string) (key, value string, ok bool) {
	for i := 0; i < len(filter); i++ {
		if filter[i] == '=' {
			return filter[:i], filter[i+1:], true
		}
	}
	return "", "", false
}
