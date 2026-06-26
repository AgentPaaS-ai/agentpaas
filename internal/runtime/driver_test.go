package runtime

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// compileTimeCheck ensures RuntimeDriver is a usable interface with the
// expected method signatures. The blank-identifier assignments verify that
// the interface compiles correctly.
func TestRuntimeDriver_InterfaceCompiles(t *testing.T) {
	// Verify the interface type exists by checking a nil-concrete assignment
	// compiles and runs without panic
	var _ RuntimeDriver = (*mockRuntimeDriver)(nil)
	t.Log("RuntimeDriver interface compiles with expected methods")
}

// mockRuntimeDriver is a minimal mock that implements RuntimeDriver for use
// in interface compilation checks and basic contract tests.
type mockRuntimeDriver struct {
	createFunc             func(ctx context.Context, spec ContainerSpec) (ContainerID, error)
	startFunc              func(ctx context.Context, id ContainerID) error
	stopFunc               func(ctx context.Context, id ContainerID, timeout *time.Duration) error
	removeFunc             func(ctx context.Context, id ContainerID, force bool) error
	statusFunc             func(ctx context.Context, id ContainerID) (ContainerStatus, error)
	statsFunc              func(ctx context.Context, id ContainerID) (ContainerStats, error)
	logsFunc               func(ctx context.Context, id ContainerID, opts LogOptions) (io.ReadCloser, error)
	execFunc               func(ctx context.Context, id ContainerID, cmd []string) (string, string, int, error)
	createNetworkFunc      func(ctx context.Context, spec NetworkSpec) (NetworkID, error)
	removeNetworkFunc      func(ctx context.Context, id NetworkID) error
	inspectNetworkFunc     func(ctx context.Context, id NetworkID) (NetworkInfo, error)
	inspectContainerNetFunc func(ctx context.Context, id ContainerID) ([]ContainerNetworkInfo, error)
	inspectContainerIPFunc func(ctx context.Context, id ContainerID, networkID string) (string, error)
	listContainersFunc      func(ctx context.Context, labelFilters ...string) ([]ContainerInfo, error)
	listNetworksFunc        func(ctx context.Context, labelFilters ...string) ([]NetworkInfo, error)
}

func (m *mockRuntimeDriver) Create(ctx context.Context, spec ContainerSpec) (ContainerID, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, spec)
	}
	return "", errors.New("not implemented")
}

func (m *mockRuntimeDriver) Start(ctx context.Context, id ContainerID) error {
	if m.startFunc != nil {
		return m.startFunc(ctx, id)
	}
	return errors.New("not implemented")
}

func (m *mockRuntimeDriver) Stop(ctx context.Context, id ContainerID, timeout *time.Duration) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, id, timeout)
	}
	return errors.New("not implemented")
}

func (m *mockRuntimeDriver) Remove(ctx context.Context, id ContainerID, force bool) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, id, force)
	}
	return errors.New("not implemented")
}

func (m *mockRuntimeDriver) Status(ctx context.Context, id ContainerID) (ContainerStatus, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx, id)
	}
	return ContainerStatusUnknown, errors.New("not implemented")
}

func (m *mockRuntimeDriver) Stats(ctx context.Context, id ContainerID) (ContainerStats, error) {
	if m.statsFunc != nil {
		return m.statsFunc(ctx, id)
	}
	return ContainerStats{}, errors.New("not implemented")
}

func (m *mockRuntimeDriver) Logs(ctx context.Context, id ContainerID, opts LogOptions) (io.ReadCloser, error) {
	if m.logsFunc != nil {
		return m.logsFunc(ctx, id, opts)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRuntimeDriver) Exec(ctx context.Context, id ContainerID, cmd []string) (string, string, int, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, id, cmd)
	}
	return "", "", -1, errors.New("not implemented")
}

func (m *mockRuntimeDriver) CreateNetwork(ctx context.Context, spec NetworkSpec) (NetworkID, error) {
	if m.createNetworkFunc != nil {
		return m.createNetworkFunc(ctx, spec)
	}
	return "", errors.New("not implemented")
}

func (m *mockRuntimeDriver) RemoveNetwork(ctx context.Context, id NetworkID) error {
	if m.removeNetworkFunc != nil {
		return m.removeNetworkFunc(ctx, id)
	}
	return errors.New("not implemented")
}

func (m *mockRuntimeDriver) InspectNetwork(ctx context.Context, id NetworkID) (NetworkInfo, error) {
	if m.inspectNetworkFunc != nil {
		return m.inspectNetworkFunc(ctx, id)
	}
	return NetworkInfo{}, errors.New("not implemented")
}

func (m *mockRuntimeDriver) InspectContainerNetworks(ctx context.Context, id ContainerID) ([]ContainerNetworkInfo, error) {
	if m.inspectContainerNetFunc != nil {
		return m.inspectContainerNetFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRuntimeDriver) InspectContainerIP(ctx context.Context, id ContainerID, networkID string) (string, error) {
	if m.inspectContainerIPFunc != nil {
		return m.inspectContainerIPFunc(ctx, id, networkID)
	}
	if m.inspectContainerNetFunc == nil {
		return "", errors.New("not implemented")
	}
	if string(id) == "" {
		return "", ErrContainerNotFound
	}
	networks, err := m.inspectContainerNetFunc(ctx, id)
	if err != nil {
		return "", err
	}
	for _, n := range networks {
		if n.ID == networkID || n.Name == networkID {
			return n.IPAddress, nil
		}
	}
	return "", nil
}

func (m *mockRuntimeDriver) ListContainers(ctx context.Context, labelFilters ...string) ([]ContainerInfo, error) {
	if m.listContainersFunc != nil {
		return m.listContainersFunc(ctx, labelFilters...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockRuntimeDriver) ListNetworks(ctx context.Context, labelFilters ...string) ([]NetworkInfo, error) {
	if m.listNetworksFunc != nil {
		return m.listNetworksFunc(ctx, labelFilters...)
	}
	return nil, errors.New("not implemented")
}

func TestContainerID_Type(t *testing.T) {
	id := ContainerID("test-id")
	if string(id) != "test-id" {
		t.Errorf("ContainerID string conversion failed: got %q", string(id))
	}
}

func TestContainerStatus_Values(t *testing.T) {
	tests := []struct {
		status ContainerStatus
		want   string
	}{
		{ContainerStatusUnknown, "unknown"},
		{ContainerStatusRunning, "running"},
		{ContainerStatusStopped, "stopped"},
		{ContainerStatusPaused, "paused"},
		{ContainerStatusRemoved, "removed"},
	}

	for _, tt := range tests {
		got := tt.status.String()
		if got != tt.want {
			t.Errorf("ContainerStatus(%d).String() = %q, want %q", int(tt.status), got, tt.want)
		}
	}
}

func TestContainerStatus_UnknownIsZero(t *testing.T) {
	// Verify that the zero value of ContainerStatus is ContainerStatusUnknown
	var s ContainerStatus
	if s != ContainerStatusUnknown {
		t.Errorf("zero value of ContainerStatus should be ContainerStatusUnknown, got %d", int(s))
	}
}

func TestLogOptions_Defaults(t *testing.T) {
	opts := LogOptions{}
	if opts.Tail != 0 {
		t.Errorf("default Tail should be 0, got %d", opts.Tail)
	}
	if opts.Follow {
		t.Error("default Follow should be false")
	}
	if opts.Since != nil {
		t.Error("default Since should be nil")
	}
}

func TestContainerSpec_Defaults(t *testing.T) {
	spec := ContainerSpec{
		Image:   "nginx:latest",
		Command: []string{},
	}
	if spec.Image != "nginx:latest" {
		t.Errorf("spec.Image = %q, want nginx:latest", spec.Image)
	}
}

func TestRuntimeDriver_Create(t *testing.T) {
	mock := &mockRuntimeDriver{
		createFunc: func(_ context.Context, spec ContainerSpec) (ContainerID, error) {
			if spec.Image == "" {
				return "", ErrInvalidSpec
			}
			return ContainerID("created-container"), nil
		},
	}

	// Valid creation
	id, err := mock.Create(context.Background(), ContainerSpec{Image: "nginx:latest"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if id != "created-container" {
		t.Errorf("Create returned wrong ID: %q", id)
	}

	// Invalid spec
	_, err = mock.Create(context.Background(), ContainerSpec{Image: ""})
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("Create with empty image should return ErrInvalidSpec, got %v", err)
	}
}

func TestRuntimeDriver_Start(t *testing.T) {
	called := false
	mock := &mockRuntimeDriver{
		startFunc: func(_ context.Context, id ContainerID) error {
			called = true
			if id == "" {
				return ErrContainerNotFound
			}
			return nil
		},
	}

	err := mock.Start(context.Background(), "running-container")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !called {
		t.Error("startFunc was not called")
	}

	// Unknown container
	err = mock.Start(context.Background(), "")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Start with empty ID should return ErrContainerNotFound, got %v", err)
	}
}

func TestRuntimeDriver_Stop(t *testing.T) {
	timeout := 10 * time.Second
	mock := &mockRuntimeDriver{
		stopFunc: func(_ context.Context, id ContainerID, to *time.Duration) error {
			if id == "" {
				return ErrContainerNotFound
			}
			if to != nil && *to != timeout {
				return errors.New("unexpected timeout")
			}
			return nil
		},
	}

	err := mock.Stop(context.Background(), "running-container", &timeout)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// With nil timeout (default)
	err = mock.Stop(context.Background(), "running-container", nil)
	if err != nil {
		t.Fatalf("Stop with nil timeout failed: %v", err)
	}
}

func TestRuntimeDriver_Remove(t *testing.T) {
	mock := &mockRuntimeDriver{
		removeFunc: func(_ context.Context, id ContainerID, force bool) error {
			if id == "" {
				return ErrContainerNotFound
			}
			return nil
		},
	}

	err := mock.Remove(context.Background(), "running-container", false)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Force remove
	err = mock.Remove(context.Background(), "running-container", true)
	if err != nil {
		t.Fatalf("Remove with force failed: %v", err)
	}
}

func TestRuntimeDriver_Status(t *testing.T) {
	mock := &mockRuntimeDriver{
		statusFunc: func(_ context.Context, id ContainerID) (ContainerStatus, error) {
			if id == "running" {
				return ContainerStatusRunning, nil
			}
			if id == "stopped" {
				return ContainerStatusStopped, nil
			}
			return ContainerStatusUnknown, ErrContainerNotFound
		},
	}

	status, err := mock.Status(context.Background(), "running")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status != ContainerStatusRunning {
		t.Errorf("Status = %v, want running", status)
	}

	status, err = mock.Status(context.Background(), "stopped")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status != ContainerStatusStopped {
		t.Errorf("Status = %v, want stopped", status)
	}

	_, err = mock.Status(context.Background(), "nonexistent")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Status for nonexistent container should return ErrContainerNotFound, got %v", err)
	}
}

func TestRuntimeDriver_Stats(t *testing.T) {
	mock := &mockRuntimeDriver{
		statsFunc: func(_ context.Context, id ContainerID) (ContainerStats, error) {
			if id == "" {
				return ContainerStats{}, ErrContainerNotFound
			}
			return ContainerStats{
				CPUPercent: 12.5,
				MemoryMB:   64.0,
				PIDs:       3,
			}, nil
		},
	}

	stats, err := mock.Stats(context.Background(), "running-container")
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.CPUPercent != 12.5 {
		t.Errorf("Stats CPUPercent = %f, want 12.5", stats.CPUPercent)
	}
	if stats.MemoryMB != 64.0 {
		t.Errorf("Stats MemoryMB = %f, want 64.0", stats.MemoryMB)
	}
	if stats.PIDs != 3 {
		t.Errorf("Stats PIDs = %d, want 3", stats.PIDs)
	}
}

func TestRuntimeDriver_Logs(t *testing.T) {
	expectedLog := "hello from agent"
	mock := &mockRuntimeDriver{
		logsFunc: func(_ context.Context, id ContainerID, opts LogOptions) (io.ReadCloser, error) {
			if id == "" {
				return nil, ErrContainerNotFound
			}
			return io.NopCloser(stringsNewReader(expectedLog)), nil
		},
	}

	reader, err := mock.Logs(context.Background(), "running-container", LogOptions{Tail: 100})
	if err != nil {
		t.Fatalf("Logs failed: %v", err)
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading logs failed: %v", err)
	}
	if string(data) != expectedLog {
		t.Errorf("Logs content = %q, want %q", string(data), expectedLog)
	}
}

// stringsNewReader is a convenience wrapper for tests.
func stringsNewReader(s string) *stringsReader {
	return &stringsReader{data: s}
}

// stringsReader is a minimal io.Reader implementation for test use.
type stringsReader struct {
	data string
	pos  int
}

func (r *stringsReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *stringsReader) Close() error {
	return nil
}

func TestContainerStats_ZeroValue(t *testing.T) {
	var s ContainerStats
	if s.CPUPercent != 0 {
		t.Errorf("zero CPUPercent = %f", s.CPUPercent)
	}
	if s.MemoryMB != 0 {
		t.Errorf("zero MemoryMB = %f", s.MemoryMB)
	}
	if s.PIDs != 0 {
		t.Errorf("zero PIDs = %d", s.PIDs)
	}
}

func TestErrSentinelValues(t *testing.T) {
	if ErrInvalidSpec == nil {
		t.Error("ErrInvalidSpec must not be nil")
	}
	if ErrContainerNotFound == nil {
		t.Error("ErrContainerNotFound must not be nil")
	}
	if !errors.Is(ErrInvalidSpec, ErrInvalidSpec) {
		t.Error("ErrInvalidSpec must be self-equal via errors.Is")
	}
	if !errors.Is(ErrContainerNotFound, ErrContainerNotFound) {
		t.Error("ErrContainerNotFound must be self-equal via errors.Is")
	}
}

func TestLogOptions_String(t *testing.T) {
	opts := LogOptions{
		Tail:   50,
		Follow: true,
	}
	if opts.Tail != 50 {
		t.Errorf("LogOptions.Tail = %d, want 50", opts.Tail)
	}
	if !opts.Follow {
		t.Error("LogOptions.Follow should be true")
	}
}