package pack

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/AgentPaaS-ai/agentpaas/internal/dockerclient"
)

func TestCleanupLocalRegistry_RemovesContainer(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker registry cleanup test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if _, err := EnsureLocalRegistry(ctx); err != nil {
		if IsRegistryPortConflict(err) {
			t.Skipf("local registry port conflict: %v", err)
		}
		t.Fatalf("EnsureLocalRegistry: %v", err)
	}

	if err := CleanupLocalRegistry(ctx); err != nil {
		t.Fatalf("CleanupLocalRegistry: %v", err)
	}

	cli, err := dockerclient.New()
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", localRegistryName)),
	})
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	if len(containers) != 0 {
		t.Fatalf("expected no %q container after cleanup, found %d", localRegistryName, len(containers))
	}
}

func TestIsRegistryPortConflict(t *testing.T) {
	if !IsRegistryPortConflict(fmt.Errorf("registry port 5001 is already in use; set AGENTPAAS_TEST_REGISTRY_PORT")) {
		t.Fatal("expected port conflict")
	}
	if IsRegistryPortConflict(nil) {
		t.Fatal("nil should not be conflict")
	}
}