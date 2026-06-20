package runtime

import (
	"context"
	"errors"
	"io"
	"time"
)

// errDockerNotImplemented is returned by DockerRuntime stub methods when
// the Docker driver has not yet been fully implemented. These stubs exist
// so that consumers can depend on the interface before B5-T02+ fills in
// the real Docker API calls.
var errDockerNotImplemented = errors.New("DockerRuntime: not yet implemented")

// DockerRuntime is a shell implementation of RuntimeDriver that delegates
// method calls to the Docker Engine API. For B5-T01, all methods return
// errDockerNotImplemented stubs; the real implementations ship in B5-T02+.
type DockerRuntime struct {
	// Docker client will be added in B5-T02.
}

// NewDockerRuntime creates a new DockerRuntime. It validates that the
// Docker daemon is reachable. If the daemon cannot be contacted, it
// returns an error only when the AGENTPAAS_DOCKER_TESTS environment
// variable is set; otherwise it returns a disabled driver.
//
// For B5-T01 this is a stub that always succeeds.
func NewDockerRuntime() (*DockerRuntime, error) {
	return &DockerRuntime{}, nil
}

// Create provisions a new Docker container. Not yet implemented (B5-T02).
func (d *DockerRuntime) Create(_ context.Context, _ ContainerSpec) (ContainerID, error) {
	return "", errDockerNotImplemented
}

// Start begins execution of a Docker container. Not yet implemented (B5-T02).
func (d *DockerRuntime) Start(_ context.Context, _ ContainerID) error {
	return errDockerNotImplemented
}

// Stop halts a Docker container. Not yet implemented (B5-T02).
func (d *DockerRuntime) Stop(_ context.Context, _ ContainerID, _ *time.Duration) error {
	return errDockerNotImplemented
}

// Remove deletes a Docker container. Not yet implemented (B5-T02).
func (d *DockerRuntime) Remove(_ context.Context, _ ContainerID, _ bool) error {
	return errDockerNotImplemented
}

// Status reports the current Docker container status. Not yet implemented
// (B5-T02).
func (d *DockerRuntime) Status(_ context.Context, _ ContainerID) (ContainerStatus, error) {
	return ContainerStatusUnknown, errDockerNotImplemented
}

// Stats returns resource usage for a Docker container. Not yet implemented
// (B5-T02).
func (d *DockerRuntime) Stats(_ context.Context, _ ContainerID) (ContainerStats, error) {
	return ContainerStats{}, errDockerNotImplemented
}

// Logs returns a reader for a Docker container's output. Not yet implemented
// (B5-T02).
func (d *DockerRuntime) Logs(_ context.Context, _ ContainerID, _ LogOptions) (io.ReadCloser, error) {
	return nil, errDockerNotImplemented
}