package pack

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/parvezsyed/agentpaas/internal/dockerclient"
)

const (
	// localRegistryPort avoids 5000, which macOS reserves for AirPlay Receiver.
	localRegistryPort  = 5001
	localRegistryName  = "agentpaas-registry"
	localRegistryImage = "registry:2"
	localRegistryHost  = "localhost"
)

func localRegistryURL() string {
	return fmt.Sprintf("%s:%d", localRegistryHost, localRegistryPort)
}

// LocalImageRef returns a digest-pinned image ref for the local registry.
func LocalImageRef(agentName, imageDigest string) string {
	return fmt.Sprintf("%s/agentpaas/%s@%s", localRegistryURL(), agentName, normalizeDigest(imageDigest))
}

func normalizeDigest(d string) string {
	d = strings.TrimSpace(d)
	if !strings.HasPrefix(d, "sha256:") {
		d = "sha256:" + d
	}
	return d
}

// EnsureLocalRegistry ensures a local OCI registry is running and returns its URL.
func EnsureLocalRegistry(ctx context.Context) (string, error) {
	cli, err := dockerclient.New()
	if err != nil {
		return "", fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = cli.Close() }()

	if err := ensureRegistryContainer(ctx, cli); err != nil {
		return "", err
	}
	return localRegistryURL(), nil
}

// PushImageToLocalRegistry tags and pushes a locally built image to the local
// registry, returning a digest-pinned image ref suitable for cosign signing.
func PushImageToLocalRegistry(ctx context.Context, sourceTag, agentName, agentVersion string) (string, error) {
	registryURL, err := EnsureLocalRegistry(ctx)
	if err != nil {
		return "", err
	}

	targetTag := fmt.Sprintf("%s/agentpaas/%s:%s", registryURL, agentName, agentVersion)

	cli, err := dockerclient.New()
	if err != nil {
		return "", fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = cli.Close() }()

	if err := cli.ImageTag(ctx, sourceTag, targetTag); err != nil {
		return "", fmt.Errorf("tag image %q as %q: %w", sourceTag, targetTag, err)
	}

	pushReader, err := cli.ImagePush(ctx, targetTag, image.PushOptions{})
	if err != nil {
		return "", fmt.Errorf("push image %q: %w", targetTag, err)
	}
	defer func() { _ = pushReader.Close() }()
	if _, err := io.Copy(io.Discard, pushReader); err != nil {
		return "", fmt.Errorf("drain push output: %w", err)
	}

	inspect, err := cli.ImageInspect(ctx, targetTag)
	if err != nil {
		return "", fmt.Errorf("inspect pushed image %q: %w", targetTag, err)
	}

	digest := imageDigest(inspect.ID, inspect.RepoDigests)
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	return fmt.Sprintf("%s/agentpaas/%s@%s", registryURL, agentName, digest), nil
}

func ensureRegistryContainer(ctx context.Context, cli *client.Client) error {
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", localRegistryName)),
	})
	if err != nil {
		return fmt.Errorf("list registry container: %w", err)
	}

	if len(containers) > 0 {
		if containers[0].State == "running" {
			return nil
		}
		if err := cli.ContainerStart(ctx, containers[0].ID, container.StartOptions{}); err != nil {
			return fmt.Errorf("start registry container: %w", err)
		}
		return nil
	}

	return createRegistryContainer(ctx, cli)
}

func createRegistryContainer(ctx context.Context, cli *client.Client) error {
	reader, err := cli.ImagePull(ctx, localRegistryImage, image.PullOptions{})
	if err == nil {
		defer func() { _ = reader.Close() }()
		if _, drainErr := io.Copy(io.Discard, reader); drainErr != nil {
			return fmt.Errorf("drain registry image pull: %w", drainErr)
		}
	}

	port := nat.Port("5000/tcp")
	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: localRegistryImage,
			ExposedPorts: nat.PortSet{
				port: struct{}{},
			},
		},
		&container.HostConfig{
			PortBindings: nat.PortMap{
				port: {{HostIP: "127.0.0.1", HostPort: strconv.Itoa(localRegistryPort)}},
			},
		},
		&network.NetworkingConfig{},
		nil,
		localRegistryName,
	)
	if err != nil {
		return fmt.Errorf("create registry container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start registry container: %w", err)
	}
	return nil
}