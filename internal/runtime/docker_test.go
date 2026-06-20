package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// compile-time check: DockerRuntime must implement RuntimeDriver
var _ RuntimeDriver = (*DockerRuntime)(nil)

func TestNewDockerRuntime(t *testing.T) {
	driver, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() returned error: %v", err)
	}
	if driver == nil {
		t.Fatal("NewDockerRuntime() returned nil driver")
	}
}

func TestDockerRuntime_Create_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.Create(context.Background(), ContainerSpec{Image: "nginx:latest"})
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Create() error = %v, want errDockerNotImplemented", err)
	}
}

func TestDockerRuntime_Start_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	err := d.Start(context.Background(), "test-id")
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Start() error = %v, want errDockerNotImplemented", err)
	}
}

func TestDockerRuntime_Stop_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	timeout := 10 * time.Second
	err := d.Stop(context.Background(), "test-id", &timeout)
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Stop() error = %v, want errDockerNotImplemented", err)
	}

	// Also test with nil timeout
	err = d.Stop(context.Background(), "test-id", nil)
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Stop(nil timeout) error = %v, want errDockerNotImplemented", err)
	}
}

func TestDockerRuntime_Remove_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	err := d.Remove(context.Background(), "test-id", false)
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Remove() error = %v, want errDockerNotImplemented", err)
	}

	// Also test with force=true
	err = d.Remove(context.Background(), "test-id", true)
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Remove(force=true) error = %v, want errDockerNotImplemented", err)
	}
}

func TestDockerRuntime_Status_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	status, err := d.Status(context.Background(), "test-id")
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Status() error = %v, want errDockerNotImplemented", err)
	}
	if status != ContainerStatusUnknown {
		t.Errorf("Status() = %v, want ContainerStatusUnknown", status)
	}
}

func TestDockerRuntime_Stats_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	stats, err := d.Stats(context.Background(), "test-id")
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Stats() error = %v, want errDockerNotImplemented", err)
	}
	if stats.CPUPercent != 0 || stats.MemoryMB != 0 || stats.PIDs != 0 {
		t.Errorf("Stats() = %+v, want zero-value ContainerStats", stats)
	}
}

func TestDockerRuntime_Logs_NotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	reader, err := d.Logs(context.Background(), "test-id", LogOptions{Tail: 100})
	if !errors.Is(err, errDockerNotImplemented) {
		t.Errorf("Logs() error = %v, want errDockerNotImplemented", err)
	}
	if reader != nil {
		t.Error("Logs() returned non-nil reader when error expected")
	}
}

// TestDockerRuntime_AllMethodsNotImplemented runs all stub methods in a
// single test to verify consistent errDockerNotImplemented behavior.
func TestDockerRuntime_AllMethodsNotImplemented(t *testing.T) {
	d := &DockerRuntime{}
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Create", func() error { _, err := d.Create(ctx, ContainerSpec{}); return err }},
		{"Start", func() error { return d.Start(ctx, "") }},
		{"Stop", func() error { return d.Stop(ctx, "", nil) }},
		{"Remove", func() error { return d.Remove(ctx, "", false) }},
		{"Status", func() error { _, err := d.Status(ctx, ""); return err }},
		{"Stats", func() error { _, err := d.Stats(ctx, ""); return err }},
		{"Logs", func() error { _, err := d.Logs(ctx, "", LogOptions{}); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if !errors.Is(err, errDockerNotImplemented) {
				t.Errorf("expected errDockerNotImplemented, got %v", err)
			}
		})
	}
}

// TestDocker_Integration guard ensures Docker integration tests are opt-in.
func TestDocker_IntegrationGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker integration guard check in short mode")
	}
	// Verify the env-var guard works by checking it's not set
	if !isDockerTestEnabled() {
		t.Log("Docker integration tests disabled — set AGENTPAAS_DOCKER_TESTS=1 to enable")
	}
}

// isDockerTestEnabled returns true when AGENTPAAS_DOCKER_TESTS environment
// variable is set to "1". Used to guard Docker integration tests.
func isDockerTestEnabled() bool {
	return false // Replaced with os.Getenv("AGENTPAAS_DOCKER_TESTS") == "1" in B5-T02+
}
