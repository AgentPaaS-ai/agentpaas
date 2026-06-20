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

// NetworkID is a unique identifier for a Docker network managed by the runtime
// driver. Implementations use Docker network IDs (hex strings or short IDs).
type NetworkID string

// ContainerNetworkInfo describes a single network attachment on a container.
type ContainerNetworkInfo struct {
	// ID is the Docker network ID.
	ID string
	// Name is the Docker network name.
	Name string
	// Aliases are network-scoped DNS aliases for the container.
	Aliases []string
}

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

// ContainerSpec defines the parameters for creating a container. Hardening
// flags (User, ReadonlyRootfs, tmpfs, CapDrop, no-new-privileges, PidsLimit,
// IPv6 disable) are applied automatically by DockerRuntime.Create based on
// P1 security policy — callers do not need to set them in the spec.
// MemoryLimitBytes and NanoCPUs are populated from agent.yaml configuration
// by the upper orchestrator layer.
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

	// NetworkIDs is a list of Docker network IDs to attach the container to.
	// Multiple network IDs enable dual-homing (e.g., gateway sidecar on
	// both internal and egress networks). For single-network containers
	// (e.g., agent), pass exactly one ID.
	NetworkIDs []string

	// User is the container-internal user to run as. When empty, the runtime
	// enforces a non-root default (uid 64000). Callers may override for
	// gateway containers that need privileged operations.
	User string

	// MemoryLimitBytes is the maximum memory the container may consume, in
	// bytes. 0 means no limit (gateway containers may need more). Populated
	// from agent.yaml by the upper orchestrator.
	MemoryLimitBytes int64

	// NanoCPUs is the CPU quota in nanoseconds of CPU time per period.
	// 0 means no limit. Populated from agent.yaml by the upper orchestrator.
	NanoCPUs int64
}

// NetworkSpec defines the parameters for creating a Docker network.
type NetworkSpec struct {
	// Name is the Docker network name.
	Name string

	// Internal, when true, creates an internal bridge network that has no
	// external access. The agent's internal bridge uses this to prevent
	// direct egress.
	Internal bool

	// Labels are Docker labels to apply to the network for ownership
	// tracking and reconciliation.
	Labels map[string]string
}

// NetworkInfo contains details about a Docker network, typically from an
// inspect operation.
type NetworkInfo struct {
	// ID is the Docker network ID.
	ID string
	// Name is the Docker network name.
	Name string
	// Internal indicates whether the network is an internal bridge (no
	// external access).
	Internal bool
	// Labels are the Docker labels attached to the network.
	Labels map[string]string
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

	// ErrNetworkNotFound is returned when the target network does not
	// exist or has been removed.
	ErrNetworkNotFound = errors.New("network not found")
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

	// CreateNetwork provisions a new Docker network from the given spec and
	// returns its NetworkID. An internal:true network restricts external
	// access (used for the per-agent internal bridge).
	CreateNetwork(ctx context.Context, spec NetworkSpec) (NetworkID, error)

	// RemoveNetwork deletes a Docker network. Returns ErrNetworkNotFound
	// if the network does not exist.
	RemoveNetwork(ctx context.Context, id NetworkID) error

	// InspectNetwork returns detailed information about a Docker network,
	// including its Internal flag and labels.
	InspectNetwork(ctx context.Context, id NetworkID) (NetworkInfo, error)

	// InspectContainerNetworks returns the list of networks a container is
	// attached to, with network names and IDs. Used for topology assertions.
	InspectContainerNetworks(ctx context.Context, id ContainerID) ([]ContainerNetworkInfo, error)
}
