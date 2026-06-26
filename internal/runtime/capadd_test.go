package runtime

import (
	"context"
	"os"
	"testing"
)

func TestContainerSpec_CapAdd_NET_ADMIN(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime: %v", err)
	}

	runID := "r17-egress-firewall"
	cid, err := dr.Create(ctx, ContainerSpec{
		Image:   "alpine:latest",
		Command: []string{"sleep", "3600"},
		Labels:  Labels(ResourceTypeAgent, runID),
		CapAdd:  []string{"NET_ADMIN"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()

	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect: %v", err)
	}
	hasNetAdmin := false
	for _, cap := range info.HostConfig.CapAdd {
		if cap == "NET_ADMIN" {
			hasNetAdmin = true
			break
		}
	}
	if !hasNetAdmin {
		t.Fatalf("CapAdd = %v, want NET_ADMIN", info.HostConfig.CapAdd)
	}
}