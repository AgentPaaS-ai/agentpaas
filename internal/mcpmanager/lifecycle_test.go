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

	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/runtime"
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
}

func newFakeRuntimeDriver() *fakeRuntimeDriver {
	return &fakeRuntimeDriver{
		specs:      make(map[runtime.ContainerID]runtime.ContainerSpec),
		statuses:   make(map[runtime.ContainerID]runtime.ContainerStatus),
		removedIDs: make(map[runtime.ContainerID]bool),
	}
}

func (d *fakeRuntimeDriver) Create(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
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

func (d *fakeRuntimeDriver) CreateNetwork(context.Context, runtime.NetworkSpec) (runtime.NetworkID, error) {
	return "", errors.New("not implemented")
}

func (d *fakeRuntimeDriver) RemoveNetwork(context.Context, runtime.NetworkID) error {
	return errors.New("not implemented")
}

func (d *fakeRuntimeDriver) InspectNetwork(context.Context, runtime.NetworkID) (runtime.NetworkInfo, error) {
	return runtime.NetworkInfo{}, errors.New("not implemented")
}

func (d *fakeRuntimeDriver) InspectContainerNetworks(context.Context, runtime.ContainerID) ([]runtime.ContainerNetworkInfo, error) {
	return nil, errors.New("not implemented")
}

func (d *fakeRuntimeDriver) InspectContainerIP(context.Context, runtime.ContainerID, string) (string, error) {
	return "", errors.New("not implemented")
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

func (d *fakeRuntimeDriver) ListNetworks(context.Context, ...string) ([]runtime.NetworkInfo, error) {
	return nil, errors.New("not implemented")
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
