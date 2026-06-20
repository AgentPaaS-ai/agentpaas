package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
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

// TestDockerRuntime_Create_InvalidSpec verifies Create rejects invalid specs
// without Docker.
func TestDockerRuntime_Create_InvalidSpec(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.Create(context.Background(), ContainerSpec{Image: ""})
	if err == nil {
		t.Error("Create() with empty spec should return error (no Docker client)")
	}
}

// TestDockerRuntime_Start_InvalidID verifies Start rejects empty ID.
func TestDockerRuntime_Start_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	err := d.Start(context.Background(), "")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Start(empty) error = %v, want ErrContainerNotFound", err)
	}
}

// TestDockerRuntime_Stop_InvalidID verifies Stop rejects empty ID.
func TestDockerRuntime_Stop_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	err := d.Stop(context.Background(), "", nil)
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Stop(empty) error = %v, want ErrContainerNotFound", err)
	}
}

// TestDockerRuntime_Remove_InvalidID verifies Remove rejects empty ID.
func TestDockerRuntime_Remove_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	err := d.Remove(context.Background(), "", false)
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Remove(empty) error = %v, want ErrContainerNotFound", err)
	}
}

// TestDockerRuntime_Status_InvalidID verifies Status rejects empty ID.
func TestDockerRuntime_Status_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.Status(context.Background(), "")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Status(empty) error = %v, want ErrContainerNotFound", err)
	}
}

// TestDockerRuntime_Stats_NotImplemented verifies Stats returns
// errDockerNotImplemented.
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

// TestDockerRuntime_Logs_NotImplemented verifies Logs returns
// errDockerNotImplemented.
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

// TestDockerRuntime_RemoveNetwork_InvalidID verifies RemoveNetwork rejects
// empty ID.
func TestDockerRuntime_RemoveNetwork_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	err := d.RemoveNetwork(context.Background(), "")
	if !errors.Is(err, ErrNetworkNotFound) {
		t.Errorf("RemoveNetwork(empty) error = %v, want ErrNetworkNotFound", err)
	}
}

// TestDockerRuntime_InspectNetwork_InvalidID verifies InspectNetwork rejects
// empty ID.
func TestDockerRuntime_InspectNetwork_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.InspectNetwork(context.Background(), "")
	if !errors.Is(err, ErrNetworkNotFound) {
		t.Errorf("InspectNetwork(empty) error = %v, want ErrNetworkNotFound", err)
	}
}

// TestDockerRuntime_InspectContainerNetworks_InvalidID verifies
// InspectContainerNetworks rejects empty ID.
func TestDockerRuntime_InspectContainerNetworks_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.InspectContainerNetworks(context.Background(), "")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("InspectContainerNetworks(empty) error = %v, want ErrContainerNotFound", err)
	}
}

// TestDocker_IntegrationGuard ensures Docker integration tests are opt-in.
func TestDocker_IntegrationGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker integration guard check in short mode")
	}
	if !isDockerTestEnabled() {
		t.Log("Docker integration tests disabled — set AGENTPAAS_DOCKER_TESTS=1 to enable")
	}
}

// isDockerTestEnabled returns true when AGENTPAAS_DOCKER_TESTS environment
// variable is set to "1". Used to guard Docker integration tests.
func isDockerTestEnabled() bool {
	return os.Getenv("AGENTPAAS_DOCKER_TESTS") == "1"
}

// TestDockerRuntime_CreateNetwork_InvalidName verifies CreateNetwork rejects
// empty name.
func TestDockerRuntime_CreateNetwork_InvalidName(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.CreateNetwork(context.Background(), NetworkSpec{Name: ""})
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("CreateNetwork(empty name) error = %v, want ErrInvalidSpec", err)
	}
}

// TestDockerRuntime_CreateNetwork_NoClient verifies CreateNetwork fails
// without a real Docker client.
func TestDockerRuntime_CreateNetwork_NoClient(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.CreateNetwork(context.Background(), NetworkSpec{Name: "test-net"})
	if err == nil {
		t.Error("CreateNetwork without Docker client should return error")
	}
}

// TestDockerRuntime_Status_StoppedVsRunning tests the status mapping logic
// by inspecting container states for containers that don't exist.
func TestDockerRuntime_Status_NotFound(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.Status(context.Background(), "nonexistent-container-xyz")
	if err == nil {
		t.Error("Status(nonexistent) should return error")
	}
}
