package runtime

import (
	"context"
	"errors"
	"io"
	"time"
)

// ContainerID is a unique identifier for a container managed by the runtime
// driver. Implementations use Docker container IDs, which are hex strings.
type ContainerID string

// ContainerStatus represents the current lifecycle state of a container.
type ContainerStatus int

const (
	// ContainerStatusUnknown indicates the container state could not be
	// determined or is not a known status value.
	ContainerStatusUnknown ContainerStatus = iota
	// ContainerStatusRunning indicates the container is actively running.
	ContainerStatusRunning
	// ContainerStatusStopped indicates the container has exited or was
	// stopped.
	ContainerStatusStopped
	// ContainerStatusPaused indicates the container process is suspended.
	ContainerStatusPaused
	// ContainerStatusRemoved indicates the container has been removed and
	// no longer exists.
	ContainerStatusRemoved
)

// String returns a human-readable representation of the container status.
func (s ContainerStatus) String() string {
	switch s {
	case ContainerStatusRunning:
		return "running"
	case ContainerStatusStopped:
		return "stopped"
	case ContainerStatusPaused:
		return "paused"
	case ContainerStatusRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// ContainerSpec defines the parameters for creating a container. Fields are
// intentionally minimal for P1; they will be extended in B5-T02+.
type ContainerSpec struct {
	// Image is the container image reference (e.g., "nginx:latest").
	Image string

	// Command is the entrypoint command override for the container.
	Command []string

	// Env is a list of environment variable assignments (KEY=VALUE format).
	Env []string

	// Labels are Docker labels to apply to the container for ownership
	// tracking and reconciliation.
	Labels map[string]string

	// NetworkID is the Docker network ID to attach the container to.
	NetworkID string
}

// ContainerStats represents a snapshot of container resource usage.
type ContainerStats struct {
	// CPUPercent is the CPU usage percentage (0.0-100.0).
	CPUPercent float64

	// MemoryMB is the current memory usage in megabytes.
	MemoryMB float64

	// PIDs is the number of processes running inside the container.
	PIDs int
}

// LogOptions controls how container logs are fetched.
type LogOptions struct {
	// Tail is the number of recent log lines to return. 0 means all.
	Tail int

	// Follow streams log output as the container produces it.
	Follow bool

	// Since returns logs only after this timestamp.
	Since *time.Time
}

// Sentinel errors returned by RuntimeDriver implementations. Drivers should
// wrap these with additional context using fmt.Errorf and %w.
var (
	// ErrInvalidSpec is returned when the ContainerSpec fails validation
	// (e.g., empty Image or invalid network ID).
	ErrInvalidSpec = errors.New("invalid container spec")

	// ErrContainerNotFound is returned when the target container does not
	// exist or has been removed.
	ErrContainerNotFound = errors.New("container not found")
)

// RuntimeDriver is the abstraction over container runtimes (Docker, etc.)
// that AgentPaaS uses to manage agent and gateway containers, Docker
// networks, and container lifecycle operations.
type RuntimeDriver interface {
	// Create provisions a new container from the given spec and returns
	// its ContainerID. The container is created but not started; call
	// Start to begin execution.
	Create(ctx context.Context, spec ContainerSpec) (ContainerID, error)

	// Start begins execution of a previously created container.
	Start(ctx context.Context, id ContainerID) error

	// Stop halts a running container. If timeout is non-nil, the runtime
	// waits at most that long for graceful shutdown before force-killing.
	Stop(ctx context.Context, id ContainerID, timeout *time.Duration) error

	// Remove deletes a container. If force is true, the container is
	// killed first if running.
	Remove(ctx context.Context, id ContainerID, force bool) error

	// Status returns the current lifecycle status of the container.
	Status(ctx context.Context, id ContainerID) (ContainerStatus, error)

	// Stats returns a snapshot of the container's resource usage.
	Stats(ctx context.Context, id ContainerID) (ContainerStats, error)

	// Logs returns a reader for the container's stdout/stderr output.
	// The caller MUST close the returned reader when done.
	Logs(ctx context.Context, id ContainerID, opts LogOptions) (io.ReadCloser, error)
}
