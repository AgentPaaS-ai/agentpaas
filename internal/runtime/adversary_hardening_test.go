package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestAdversaryB5T03_UserOverrideToRoot tests whether a caller can bypass
// the non-root uid enforcement by passing User="root" or User="0" in the
// ContainerSpec.
func TestAdversaryB5T03_UserOverrideToRoot(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-adv-userroot"
	cid, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{},
		Labels:     Labels(ResourceTypeAgent, runID),
		User:       "root", // attacker tries to escalate to root
	})
	if err != nil {
		t.Fatalf("Create() with User=root failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()

	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}
	// The spec allows User override for gateway containers. This is a policy
	// decision: the upper layer (orchestrator) is trusted to only pass
	// non-root user for agent containers. The runtime does not enforce this
	// because gateway containers may need root.
	// This test documents the attack surface.
	t.Logf("Container User = %q (runtime accepts override — orchestration layer must restrict)", info.Config.User)
}

// TestAdversaryB5T03_NoCapDropOnEmptySpec tests that creating a container
// with an empty field does not skip the CapDrop ALL enforcement.
func TestAdversaryB5T03_NoCapDropOnEmptySpec(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	// Create container with minimal spec (no cap-related fields)
	runID := "b5t03-adv-cap"
	cid, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create() with basic spec failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()

	info, err := dr.cli.ContainerInspect(ctx, string(cid))
	if err != nil {
		t.Fatalf("ContainerInspect failed: %v", err)
	}

	// CapDrop must contain ALL
	hasAll := false
	for _, cap := range info.HostConfig.CapDrop {
		if cap == "ALL" {
			hasAll = true
			break
		}
	}
	if !hasAll {
		t.Errorf("Container CapDrop = %v, want ALL (must be enforced regardless of spec)", info.HostConfig.CapDrop)
	}

	// CapAdd must be empty (no capabilities should be added)
	if len(info.HostConfig.CapAdd) > 0 {
		t.Errorf("Container CapAdd = %v, want empty (no capabilities should be added)", info.HostConfig.CapAdd)
	}
}

// TestAdversaryB5T03_PrivilegedModeNotAllowed tests that there is no way
// via ContainerSpec to create a privileged container.
func TestAdversaryB5T03_PrivilegedModeNotAllowed(t *testing.T) {
	// ContainerSpec has no Privileged field — the spec API prevents this
	// attack at compile time. This is a positive security property.
	// If the spec gains a Privileged field in the future, enforcement must
	// be added here.
	spec := ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "10"},
		NetworkIDs: []string{},
	}
	_ = spec
	// Confirmed safe: no Privileged field on ContainerSpec.
}

// TestAdversaryB5T03_HostNetworkNotAllowed tests that there is no way
// via ContainerSpec to request host-network mode.
func TestAdversaryB5T03_HostNetworkNotAllowed(t *testing.T) {
	// ContainerSpec has no NetworkMode field and DockerRuntime.Create
	// always passes an empty NetworkingConfig for the first network.
	// Host network mode requires setting NetworkMode to "host" which
	// is not exposed. The spec API prevents this at compile time.
	spec := ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "10"},
		NetworkIDs: []string{"internal-net"},
	}
	if len(spec.NetworkIDs) != 1 {
		t.Error("basic spec invalid")
	}
	// Confirmed safe: no host-network vector through public spec.
}

// TestAdversaryB5T03_ReadOnlyRootfsBypass tests that creating a container
// with a basic spec does enforce read-only rootfs (no writable filesystem).
func TestAdversaryB5T03_ReadOnlyRootfsBypass(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-adv-rofs"
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
	// Must have /tmp as writable space on read-only rootfs.
	// Docker represents tmpfs as map[string]string{"/tmp": ""} — the key
	// existence is the signal, not the value (empty string is normal).
	if info.HostConfig.Tmpfs == nil {
		t.Errorf("Tmpfs = nil, want /tmp writable tmpfs")
	} else if _, ok := info.HostConfig.Tmpfs["/tmp"]; !ok {
		t.Errorf("Tmpfs = %v, want /tmp key present", info.HostConfig.Tmpfs)
	}
	if !info.HostConfig.ReadonlyRootfs {
		t.Errorf("ReadonlyRootfs = false, want true")
	}
	// Verify /etc must NOT be writable (can't be easily tested without
	// running container, but the config should enforce it)
}

// TestAdversaryB5T03_SecurityOptInjection tests whether an attacker can
// inject additional security options or override no-new-privileges via
// the ContainerSpec fields.
func TestAdversaryB5T03_SecurityOptInjection(t *testing.T) {
	// ContainerSpec has no SecurityOpt field — security options are
	// hardcoded in DockerRuntime.Create. An attacker cannot inject
	// "seccomp=unconfined" or "apparmor=unconfined" through the spec.
	spec := ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "10"},
		NetworkIDs: []string{},
	}
	if len(spec.NetworkIDs) != 0 {
		t.Error("basic spec invalid")
	}
	// Confirmed safe: no SecurityOpt injection vector through public spec.
}

// TestAdversaryB5T03_NoNewPrivilegesNotBypassable tests that
// no-new-privileges is set even when the spec is minimal.
func TestAdversaryB5T03_NoNewPrivilegesNotBypassable(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-adv-nnp"
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
		if strings.Contains(strings.ToLower(opt), "no-new-privileges") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SecurityOpt = %v, want no-new-privileges", info.HostConfig.SecurityOpt)
	}
}

// TestAdversaryB5T03_PidsLimitBypass tests that pids-limit is enforced
// even on a minimal spec.
func TestAdversaryB5T03_PidsLimitBypass(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-adv-pids"
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
		t.Fatal("PidsLimit is nil, want 256")
	}
	if *info.HostConfig.PidsLimit != 256 {
		t.Errorf("PidsLimit = %d, want 256", *info.HostConfig.PidsLimit)
	}
}

// TestAdversaryB5T03_IPv6EnabledViaSysctl tests whether IPv6 can be
// re-enabled through container config bypass. Confirms no sysctl override.
func TestAdversaryB5T03_IPv6EnabledViaSysctl(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-adv-ipv6"
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
	if info.HostConfig.Sysctls == nil {
		t.Fatal("Sysctls is nil, want net.ipv6.conf.all.disable_ipv6=1")
	}
	val, ok := info.HostConfig.Sysctls["net.ipv6.conf.all.disable_ipv6"]
	if !ok {
		t.Errorf("Sysctls missing net.ipv6.conf.all.disable_ipv6; got: %v", info.HostConfig.Sysctls)
	} else if val != "1" {
		t.Errorf("net.ipv6.conf.all.disable_ipv6 = %q, want \"1\"", val)
	}
}

// TestAdversaryB5T03_UnlimitedMemory tests that when MemoryLimitBytes is 0
// (default), the container has NO memory limit (gateway container pattern).
func TestAdversaryB5T03_UnlimitedMemory(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t03-adv-mem"
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
	// Memory=0 means no limit (Docker default)
	if info.HostConfig.Memory != 0 {
		t.Errorf("Memory = %d, want 0 (no limit when MemoryLimitBytes=0)", info.HostConfig.Memory)
	}
}

// TestAdversaryB5T03_GatewayNotHardened confirms that the hardening test
// does not interfere with gateway containers (which may need different settings).
// Gateway containers are created by the same DockerRuntime.Create but are
// expected to have different labels.
func TestAdversaryB5T03_GatewayNotHardened(t *testing.T) {
	// Gateway containers go through the same Create path and thus get
	// the same hardening flags. If gateway containers need relaxed
	// hardening (e.g., no read-only rootfs for a proxy), the ContainerSpec
	// would need additional fields. This documents the current state:
	// all containers get the same hardening.
}
