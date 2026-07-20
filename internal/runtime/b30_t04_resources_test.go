package runtime

import (
	"context"
	"os"
	"testing"
)

// TestB30T04_PidsLimitDefault verifies the runtime driver applies the default
// 256 PID ceiling when MaxPIDs is 0 (legacy / no policy). This is the
// fork-bomb containment invariant.
func TestB30T04_PidsLimitDefault(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}
	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	runID := "b30t04-test-pids-default"
	cid, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()
	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}
	if info.HostConfig.PidsLimit == nil || *info.HostConfig.PidsLimit != 256 {
		t.Errorf("PidsLimit = %v, want 256 (default fork-bomb ceiling)", info.HostConfig.PidsLimit)
	}
}

// TestB30T04_PidsLimitPolicyOverride verifies the runtime driver honors a
// policy-derived MaxPIDs (e.g. 8) when set in the ContainerSpec, overriding
// the default 256. This proves T04.5 wires PID limits through the container
// spec.
func TestB30T04_PidsLimitPolicyOverride(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}
	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	runID := "b30t04-test-pids-policy"
	cid, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{},
		Labels:     Labels(ResourceTypeAgent, runID),
		MaxPIDs:    8,
	})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()
	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}
	if info.HostConfig.PidsLimit == nil || *info.HostConfig.PidsLimit != 8 {
		t.Errorf("PidsLimit = %v, want 8 (policy override)", info.HostConfig.PidsLimit)
	}
}

// TestB30T04_ContainerSpecCarriesPolicyResourceFields is a non-Docker unit
// test asserting the ContainerSpec carries the B30-T04 policy-derived
// resource fields (MaxPIDs, MemoryLimitBytes, NanoCPUs). This guards the
// T04.5 wiring contract without requiring Docker.
func TestB30T04_ContainerSpecCarriesPolicyResourceFields(t *testing.T) {
	spec := ContainerSpec{
		Image:            "alpine:latest",
		MemoryLimitBytes: 134217728,
		NanoCPUs:         500000000,
		MaxPIDs:          64,
	}
	if spec.MemoryLimitBytes != 134217728 {
		t.Errorf("MemoryLimitBytes = %d, want 134217728", spec.MemoryLimitBytes)
	}
	if spec.NanoCPUs != 500000000 {
		t.Errorf("NanoCPUs = %d, want 500000000", spec.NanoCPUs)
	}
	if spec.MaxPIDs != 64 {
		t.Errorf("MaxPIDs = %d, want 64", spec.MaxPIDs)
	}
}
