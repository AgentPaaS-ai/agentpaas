package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/parvezsyed/agentpaas/internal/dockerclient"
)

// defaultImage is the default container image used for agent and gateway
// containers when no image is specified in the spec.
const defaultImage = "alpine:latest"

// errDockerNotImplemented is returned by DockerRuntime methods that are
// not yet implemented (Stats, Logs).
var errDockerNotImplemented = errors.New("DockerRuntime: not yet implemented")

// DockerRuntime is a Docker Engine implementation of RuntimeDriver that
// delegates method calls to the Docker Engine API.
type DockerRuntime struct {
	cli *client.Client
}

// NewDockerRuntime creates a new DockerRuntime backed by the Docker Engine
// API. It discovers the Docker daemon through environment variables
// (DOCKER_HOST, DOCKER_API_VERSION) or defaults to the local socket.
func NewDockerRuntime() (*DockerRuntime, error) {
	cli, err := dockerclient.New()
	if err != nil {
		return nil, fmt.Errorf("DockerRuntime: %w", err)
	}

	// Verify the daemon is reachable by pinging it. We return the runtime
	// regardless so the caller can decide how to handle an unreachable daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = cli.Ping(ctx)

	return &DockerRuntime{cli: cli}, nil
}

// ensureImage ensures the required image is pulled locally. For P1 we default
// to alpine:latest which is small and commonly cached.
func (d *DockerRuntime) ensureImage(ctx context.Context, imageRef string) error {
	summary, err := d.cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", imageRef)),
	})
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}
	if len(summary) > 0 {
		return nil // already present
	}

	// Pull the image
	reader, err := d.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", imageRef, err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("drain pull output: %w", err)
	}
	return nil
}

// Create provisions a new Docker container from the given spec and returns
// its ContainerID. The container is created but not started; call Start
// to begin execution.
func (d *DockerRuntime) Create(ctx context.Context, spec ContainerSpec) (ContainerID, error) {
	if d.cli == nil {
		return "", errors.New("DockerRuntime: not initialized (no Docker client)")
	}

	if spec.Image == "" {
		spec.Image = defaultImage
	}

	if err := d.ensureImage(ctx, spec.Image); err != nil {
		return "", fmt.Errorf("ensure image %q: %w", spec.Image, err)
	}

	env := spec.Env
	if env == nil {
		env = []string{}
	}

	config := &container.Config{
		Image:  spec.Image,
		Cmd:    spec.Command,
		Env:    env,
		Labels: spec.Labels,
	}

	// Apply P1 container hardening flags. These are security policy values,
	// not caller-configurable. The User field on ContainerSpec allows the
	// upper layer to override for gateway containers that may need a
	// different user.
	user := spec.User
	if user == "" {
		user = "64000" // non-root default for agent containers
	}
	config.User = user

	hostConfig := &container.HostConfig{
		// Read-only rootfs prevents container processes from modifying
		// the filesystem.
		ReadonlyRootfs: true,

		// tmpfs on /tmp gives a writable scratch space without needing
		// a writable rootfs.
		Tmpfs: map[string]string{
			"/tmp": "",
		},

		// Drop all Linux capabilities — the agent has no need for any
		// privileged operations.
		CapDrop: []string{"ALL"},

		// Prevent privilege escalation via setuid binaries or similar.
		SecurityOpt: []string{"no-new-privileges:true"},

		// Disable IPv6 inside the container. IPv6 is not used in the
		// current network topology; disabling it reduces attack surface.
		Sysctls: map[string]string{
			"net.ipv6.conf.all.disable_ipv6": "1",
		},

		// Binds mounts host directories into the container. Used for
		// audit volumes — the harness writes egress audit events to a
		// mounted volume that the daemon reads after the run.
		Binds: spec.Binds,
	}

	// Resource limits (embedded Resources struct — set separately for clarity)
	hostConfig.PidsLimit = int64Ptr(256) // Limit processes to prevent fork bombs
	hostConfig.Memory = spec.MemoryLimitBytes
	hostConfig.NanoCPUs = spec.NanoCPUs

	// For multi-network containers (gateway dual-homing), we create the
	// container with the FIRST network and then connect additional networks
	// via NetworkConnect.
	var firstNetID string
	var additionalNetIDs []string
	if len(spec.NetworkIDs) > 0 {
		firstNetID = spec.NetworkIDs[0]
		if len(spec.NetworkIDs) > 1 {
			additionalNetIDs = spec.NetworkIDs[1:]
		}
	}

	networkingConfig := &network.NetworkingConfig{}
	if firstNetID != "" {
		networkingConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			firstNetID: {},
		}
	}

	resp, err := d.cli.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, "")
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	// Connect additional networks for dual-homing
	for _, netID := range additionalNetIDs {
		if err := d.cli.NetworkConnect(ctx, netID, resp.ID, nil); err != nil {
			_ = d.Remove(ctx, ContainerID(resp.ID), true)
			return "", fmt.Errorf("connect network %q: %w", netID, err)
		}
	}

	return ContainerID(resp.ID), nil
}

// Start begins execution of a previously created container.
func (d *DockerRuntime) Start(ctx context.Context, id ContainerID) error {
	if string(id) == "" {
		return ErrContainerNotFound
	}
	if d.cli == nil {
		return errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	if err := d.cli.ContainerStart(ctx, string(id), container.StartOptions{}); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("%w: %s", ErrContainerNotFound, string(id))
		}
		return fmt.Errorf("start container %q: %w", string(id), err)
	}
	return nil
}

// Stop halts a running container. If timeout is non-nil, the runtime waits
// at most that long for graceful shutdown before force-killing.
func (d *DockerRuntime) Stop(ctx context.Context, id ContainerID, timeout *time.Duration) error {
	if string(id) == "" {
		return ErrContainerNotFound
	}
	if d.cli == nil {
		return errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	stopOpts := container.StopOptions{}
	if timeout != nil {
		secs := int((*timeout).Seconds())
		stopOpts.Timeout = &secs
	}
	if err := d.cli.ContainerStop(ctx, string(id), stopOpts); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("%w: %s", ErrContainerNotFound, string(id))
		}
		return fmt.Errorf("stop container %q: %w", string(id), err)
	}
	return nil
}

// Remove deletes a container. If force is true, the container is killed
// first if running.
func (d *DockerRuntime) Remove(ctx context.Context, id ContainerID, force bool) error {
	if string(id) == "" {
		return ErrContainerNotFound
	}
	if d.cli == nil {
		return errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	removeOpts := container.RemoveOptions{
		Force: force,
	}
	if err := d.cli.ContainerRemove(ctx, string(id), removeOpts); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("%w: %s", ErrContainerNotFound, string(id))
		}
		return fmt.Errorf("remove container %q: %w", string(id), err)
	}
	return nil
}

// Status reports the current Docker container lifecycle status.
func (d *DockerRuntime) Status(ctx context.Context, id ContainerID) (ContainerStatus, error) {
	if string(id) == "" {
		return ContainerStatusUnknown, ErrContainerNotFound
	}
	if d.cli == nil {
		return ContainerStatusUnknown, errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	json, err := d.cli.ContainerInspect(ctx, string(id))
	if err != nil {
		if errdefs.IsNotFound(err) {
			return ContainerStatusRemoved, fmt.Errorf("%w: %s", ErrContainerNotFound, string(id))
		}
		return ContainerStatusUnknown, fmt.Errorf("inspect container %q: %w", string(id), err)
	}
	switch json.State.Status {
	case "running":
		return ContainerStatusRunning, nil
	case "paused":
		return ContainerStatusPaused, nil
	case "exited", "dead":
		return ContainerStatusStopped, nil
	case "removing":
		return ContainerStatusRemoved, nil
	default:
		return ContainerStatusUnknown, nil
	}
}

// Stats returns resource usage for a Docker container. Not yet implemented.
func (d *DockerRuntime) Stats(_ context.Context, _ ContainerID) (ContainerStats, error) {
	return ContainerStats{}, errDockerNotImplemented
}

// Logs returns a reader for a Docker container's stdout/stderr output.
func (d *DockerRuntime) Logs(ctx context.Context, id ContainerID, opts LogOptions) (io.ReadCloser, error) {
	if string(id) == "" {
		return nil, fmt.Errorf("%w: empty container ID", ErrContainerNotFound)
	}
	if d.cli == nil {
		return nil, errors.New("DockerRuntime: not initialized (no Docker client)")
	}

	tail := strconv.Itoa(opts.Tail)
	if opts.Tail <= 0 {
		tail = "all"
	}
	logOpts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Tail:       tail,
	}
	if opts.Since != nil {
		logOpts.Since = opts.Since.Format(time.RFC3339Nano)
	}

	reader, err := d.cli.ContainerLogs(ctx, string(id), logOpts)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrContainerNotFound, string(id))
		}
		return nil, fmt.Errorf("container logs: %w", err)
	}
	return reader, nil
}

// CreateNetwork provisions a new Docker network from the given spec and
// returns its NetworkID.
func (d *DockerRuntime) CreateNetwork(ctx context.Context, spec NetworkSpec) (NetworkID, error) {
	if spec.Name == "" {
		return "", fmt.Errorf("%w: network name is required", ErrInvalidSpec)
	}
	if d.cli == nil {
		return "", errors.New("DockerRuntime: not initialized (no Docker client)")
	}

	netCreate := network.CreateOptions{
		Driver:   "bridge",
		Internal: spec.Internal,
		Labels:   spec.Labels,
	}

	resp, err := d.cli.NetworkCreate(ctx, spec.Name, netCreate)
	if err != nil {
		return "", fmt.Errorf("create network %q: %w", spec.Name, err)
	}

	return NetworkID(resp.ID), nil
}

// RemoveNetwork deletes a Docker network.
func (d *DockerRuntime) RemoveNetwork(ctx context.Context, id NetworkID) error {
	if string(id) == "" {
		return ErrNetworkNotFound
	}
	if d.cli == nil {
		return errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	if err := d.cli.NetworkRemove(ctx, string(id)); err != nil {
		if errdefs.IsNotFound(err) || strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("%w: %s", ErrNetworkNotFound, string(id))
		}
		return fmt.Errorf("remove network %q: %w", string(id), err)
	}
	return nil
}

// InspectNetwork returns detailed information about a Docker network.
func (d *DockerRuntime) InspectNetwork(ctx context.Context, id NetworkID) (NetworkInfo, error) {
	if string(id) == "" {
		return NetworkInfo{}, ErrNetworkNotFound
	}
	if d.cli == nil {
		return NetworkInfo{}, errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	resource, err := d.cli.NetworkInspect(ctx, string(id), network.InspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return NetworkInfo{}, fmt.Errorf("%w: %s", ErrNetworkNotFound, string(id))
		}
		return NetworkInfo{}, fmt.Errorf("inspect network %q: %w", string(id), err)
	}
	return NetworkInfo{
		ID:       resource.ID,
		Name:     resource.Name,
		Internal: resource.Internal,
		Labels:   resource.Labels,
	}, nil
}

// InspectContainerNetworks returns the list of networks a container is
// attached to. Used for topology assertions.
func (d *DockerRuntime) InspectContainerNetworks(ctx context.Context, id ContainerID) ([]ContainerNetworkInfo, error) {
	if string(id) == "" {
		return nil, ErrContainerNotFound
	}
	if d.cli == nil {
		return nil, errors.New("DockerRuntime: not initialized (no Docker client)")
	}
	json, err := d.cli.ContainerInspect(ctx, string(id))
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrContainerNotFound, string(id))
		}
		return nil, fmt.Errorf("inspect container %q: %w", string(id), err)
	}

	var result []ContainerNetworkInfo
	for netName, netSettings := range json.NetworkSettings.Networks {
		info := ContainerNetworkInfo{
			ID:      netSettings.NetworkID,
			Name:    netName,
			Aliases: netSettings.Aliases,
		}
		result = append(result, info)
	}
	return result, nil
}

// int64Ptr returns a pointer to the given int64 value. Used for Docker API
// fields that expect *int64 (e.g., PidsLimit).
func int64Ptr(v int64) *int64 {
	return &v
}

// ListContainers returns all Docker containers matching the given label
// filters. Each filter should be in "key=value" format. The results include
// each container's ID, name, status, and Docker labels.
func (d *DockerRuntime) ListContainers(ctx context.Context, labelFilters ...string) ([]ContainerInfo, error) {
	if d.cli == nil {
		return nil, errors.New("DockerRuntime: not initialized (no Docker client)")
	}

	filterArgs := filters.NewArgs()
	for _, lf := range labelFilters {
		filterArgs.Add("label", lf)
	}

	containers, err := d.cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var result []ContainerInfo
	for _, c := range containers {
		status := ContainerStatusUnknown
		switch {
		case strings.Contains(c.Status, "Up"):
			status = ContainerStatusRunning
		case strings.Contains(c.Status, "Exited"), strings.Contains(c.Status, "Created"):
			status = ContainerStatusStopped
		case strings.Contains(c.Status, "Paused"):
			status = ContainerStatusPaused
		case strings.Contains(c.Status, "Removal In Progress"):
			status = ContainerStatusRemoved
		}

		labels := c.Labels
		if labels == nil {
			labels = map[string]string{}
		}

		// Truncate container name — Docker returns names with leading slash
		name := c.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}

		result = append(result, ContainerInfo{
			ID:           c.ID,
			Name:         name,
			Status:       status,
			Labels:       labels,
			RunID:        RunIDFromLabels(labels),
			ResourceType: ResourceTypeFromLabels(labels),
		})
	}

	return result, nil
}

// ListNetworks returns all Docker networks matching the given label filters.
// Each filter should be in "key=value" format.
func (d *DockerRuntime) ListNetworks(ctx context.Context, labelFilters ...string) ([]NetworkInfo, error) {
	if d.cli == nil {
		return nil, errors.New("DockerRuntime: not initialized (no Docker client)")
	}

	filterArgs := filters.NewArgs()
	for _, lf := range labelFilters {
		filterArgs.Add("label", lf)
	}

	resources, err := d.cli.NetworkList(ctx, network.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}

	var result []NetworkInfo
	for _, n := range resources {
		result = append(result, NetworkInfo{
			ID:       n.ID,
			Name:     n.Name,
			Internal: n.Internal,
			Labels:   n.Labels,
		})
	}

	return result, nil
}
