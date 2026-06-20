package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestHardening_NonRootUser verifies containers run as uid 64000 (non-root).
func TestHardening_NonRootUser(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-user"
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

	// Inspect and verify User field
	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}
	if info.Config.User != "64000" {
		t.Errorf("Container User = %q, want %q", info.Config.User, "64000")
	}
}

// TestHardening_ReadOnlyRootfs verifies the container has a read-only rootfs.
func TestHardening_ReadOnlyRootfs(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-ro"
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
	if !info.HostConfig.ReadonlyRootfs {
		t.Error("Container ReadonlyRootfs = false, want true")
	}
}

// TestHardening_TmpfsOnTmp verifies /tmp is mounted as tmpfs.
func TestHardening_TmpfsOnTmp(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-tmpfs"
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
	if info.HostConfig.Tmpfs == nil {
		t.Fatal("Container Tmpfs is nil, want map containing /tmp")
	}
	_, ok := info.HostConfig.Tmpfs["/tmp"]
	if !ok {
		t.Errorf("Container Tmpfs missing /tmp mount; got: %v", info.HostConfig.Tmpfs)
	}
}

// TestHardening_CapDropAll verifies ALL capabilities are dropped.
func TestHardening_CapDropAll(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-cap"
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
	hasAll := false
	for _, cap := range info.HostConfig.CapDrop {
		if cap == "ALL" {
			hasAll = true
			break
		}
	}
	if !hasAll {
		t.Errorf("Container CapDrop = %v, want to contain \"ALL\"", info.HostConfig.CapDrop)
	}
}

// TestHardening_NoNewPrivileges verifies no-new-privileges security opt.
func TestHardening_NoNewPrivileges(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-nnp"
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
	found := false
	for _, opt := range info.HostConfig.SecurityOpt {
		if strings.EqualFold(opt, "no-new-privileges:true") || opt == "no-new-privileges" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Container SecurityOpt = %v, want to contain no-new-privileges", info.HostConfig.SecurityOpt)
	}
}

// TestHardening_DefaultSeccomp verifies the default Docker seccomp profile
// is applied (no custom seccomp profile override).
func TestHardening_DefaultSeccomp(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-seccomp"
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
	// Docker applies the default seccomp profile by default when
	// SecurityOpt does NOT contain "seccomp=unconfined" or "seccomp=/path/to/custom".
	// We want to verify we did NOT set a custom seccomp override.
	for _, opt := range info.HostConfig.SecurityOpt {
		if strings.HasPrefix(opt, "seccomp=") {
			t.Errorf("Custom seccomp profile set: %q (should use default)", opt)
		}
	}
	// Also verify no-new-privileges is present (already tested separately)
}

// TestHardening_PidsLimit verifies the container has pids-limit=256.
func TestHardening_PidsLimit(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-pids"
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
	if info.HostConfig.PidsLimit == nil {
		t.Fatal("Container PidsLimit is nil, want 256")
	}
	if *info.HostConfig.PidsLimit != 256 {
		t.Errorf("Container PidsLimit = %d, want 256", *info.HostConfig.PidsLimit)
	}
}

// TestHardening_IPv6Disabled verifies that IPv6 is disabled in the container
// via sysctl net.ipv6.conf.all.disable_ipv6=1.
func TestHardening_IPv6Disabled(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-ipv6"
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

	if err := dr.Start(ctx, cid); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() { _ = dr.Stop(ctx, cid, nil) }()

	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}
	if info.HostConfig.Sysctls == nil {
		t.Fatal("Container Sysctls is nil, want net.ipv6.conf.all.disable_ipv6=1")
	}
	val, ok := info.HostConfig.Sysctls["net.ipv6.conf.all.disable_ipv6"]
	if !ok {
		t.Errorf("Sysctls missing net.ipv6.conf.all.disable_ipv6; got: %v", info.HostConfig.Sysctls)
	} else if val != "1" {
		t.Errorf("net.ipv6.conf.all.disable_ipv6 = %q, want \"1\"", val)
	}
}

// TestHardening_MemoryAndCPULimits verifies that memory and CPU limits are
// applied when specified in the ContainerSpec.
func TestHardening_MemoryAndCPULimits(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-limits"
	cid, err := dr.Create(ctx, ContainerSpec{
		Image:            "alpine:latest",
		Command:          []string{"sleep", "3600"},
		NetworkIDs:       []string{},
		Labels:           Labels(ResourceTypeAgent, runID),
		MemoryLimitBytes: 134217728, // 128 MB
		NanoCPUs:         500000000,  // 0.5 CPU
	})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()

	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}
	if info.HostConfig.Memory != 134217728 {
		t.Errorf("Container Memory = %d, want 134217728 (128MB)", info.HostConfig.Memory)
	}
	if info.HostConfig.NanoCPUs != 500000000 {
		t.Errorf("Container NanoCPUs = %d, want 500000000 (0.5 CPU)", info.HostConfig.NanoCPUs)
	}
}

// TestHardening_AllFlagsApplied verifies all hardening flags are applied
// together in a single container (comprehensive test).
func TestHardening_AllFlagsApplied(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-test-all"
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

	// Verify User
	if info.Config.User != "64000" {
		t.Errorf("User = %q, want %q", info.Config.User, "64000")
	}

	// Verify ReadonlyRootfs
	if !info.HostConfig.ReadonlyRootfs {
		t.Error("ReadonlyRootfs = false, want true")
	}

	// Verify tmpfs /tmp.
	// Docker represents tmpfs as map[string]string{"/tmp": ""} — the key
	// existence is the signal, not the value (empty string is normal).
	if info.HostConfig.Tmpfs == nil {
		t.Error("Container Tmpfs is nil, want map containing /tmp")
	} else if _, ok := info.HostConfig.Tmpfs["/tmp"]; !ok {
		t.Errorf("Container Tmpfs missing /tmp mount; got: %v", info.HostConfig.Tmpfs)
	}

	// Verify CapDrop ALL
	hasAll := false
	for _, cap := range info.HostConfig.CapDrop {
		if cap == "ALL" {
			hasAll = true
			break
		}
	}
	if !hasAll {
		t.Errorf("CapDrop = %v, want ALL", info.HostConfig.CapDrop)
	}

	// Verify no-new-privileges
	hasNNP := false
	for _, opt := range info.HostConfig.SecurityOpt {
		if strings.HasPrefix(strings.ToLower(opt), "no-new-privileges") {
			hasNNP = true
			break
		}
	}
	if !hasNNP {
		t.Errorf("SecurityOpt = %v, want no-new-privileges", info.HostConfig.SecurityOpt)
	}

	// Verify PidsLimit
	if info.HostConfig.PidsLimit == nil || *info.HostConfig.PidsLimit != 256 {
		t.Errorf("PidsLimit = %v, want 256", info.HostConfig.PidsLimit)
	}

	// Verify IPv6 disabled via sysctl
	if info.HostConfig.Sysctls == nil || info.HostConfig.Sysctls["net.ipv6.conf.all.disable_ipv6"] != "1" {
		t.Errorf("Sysctls = %v, want net.ipv6.conf.all.disable_ipv6=1", info.HostConfig.Sysctls)
	}
	// Verify no custom seccomp override
	for _, opt := range info.HostConfig.SecurityOpt {
		if strings.HasPrefix(opt, "seccomp=") {
			t.Errorf("Custom seccomp profile set: %q (should use default)", opt)
		}
	}
}
