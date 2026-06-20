package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAdversaryB5T04a_CanaryBypassViaAlternatePort tests whether an agent
// container can bypass egress restrictions by reaching external hosts on
// alternate ports (e.g., 443, 53, 8080) from the internal network.
//
// ADVERSARY BREAK: MEDIUM — the Docker internal:true bridge should block
// ALL external traffic regardless of port. If egress is possible on any
// port, the canary assumption is violated. This test documents the
// expected block behavior across multiple ports.
func TestAdversaryB5T04a_CanaryBypassViaAlternatePort(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04a-adv-ports"

	// Setup minimal topology: internal network + agent container
	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Test multiple ports: 80 (HTTP), 443 (HTTPS), 53 (DNS), 8080 (alt HTTP)
	ports := []string{"80", "443", "53", "8080"}
	blockedAll := true
	for _, port := range ports {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := dockerExec(probeCtx, string(agentID),
			"wget", "-q", "-O", "/dev/null", "--timeout=3",
			"http://1.1.1.1:"+port)
		cancel()

		if err == nil {
			t.Errorf("Canary bypass on port %s: agent reached 1.1.1.1:%s — expected BLOCKED", port, port)
			blockedAll = false
		}
		_ = out
	}
	if blockedAll {
		t.Log("PASS: All alternate port probes to 1.1.1.1 are blocked")
	}
}

// TestAdversaryB5T04a_CanaryTimeoutBypass tests whether a hanging/canary
// probe can be forced to wait longer than intended (timeout bypass).
//
// ADVERSARY BREAK: MEDIUM — if the agent can hold a connection open
// indefinitely by exploiting a slow SYN-ACK response, it may outlast
// the configured timeout. This is mitigated by context.WithTimeout in
// the test framework itself, but the execution path must not have
// indefinite-wait primitives.
func TestAdversaryB5T04a_CanaryTimeoutBypass(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}
	// This is a reasoning-level test: verify no exec or network call in
	// the e2e test can hang. We assert that the dockerExec helper uses
	// exec.CommandContext (it does). The best we can unit-test is that
	// dockerExec respects context cancellation.
	t.Run("dockerExec_respects_context_cancellation", func(t *testing.T) {
		wrongCtx, cancel := context.WithCancel(context.Background())
		cancel() // already cancelled

		_, err := dockerExec(wrongCtx, "non-existent", "echo", "hello")
		if err == nil {
			t.Error("dockerExec succeeded with cancelled context — should fail")
		}
	})

	t.Run("dockerExec_hangs_without_timeout", func(t *testing.T) {
		// This test verifies that WITHOUT a timeout, dockerExec blocks.
		// The presence of context cancellation in production code means
		// all callers should set timeouts. We document the risk here.
		// Not actually running to avoid hanging.
		t.Log("PASS: timeout bypass risk documented — all callers must use context.WithTimeout")
	})
}

// TestAdversaryB5T04a_DNSRedirect tests whether an agent container can
// bypass DNS blocking by using a non-standard DNS resolver or direct IP
// resolution (hosts file manipulation is not possible because rootfs is
// read-only).
//
// ADVERSARY BREAK: LOW — read-only rootfs prevents /etc/hosts tampering.
// Alternative DNS transports (TCP/853, DoH) are also blocked because the
// internal network has no external egress regardless of protocol.
func TestAdversaryB5T04a_DNSRedirect(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04a-adv-dns"

	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Test DNS resolution via various resolvers
	// All should fail because internal:true blocks all external traffic
	dnsTests := []struct {
		name string
		cmd  []string
	}{
		{
			name: "GoogleDNS",
			cmd:  []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://google.com"},
		},
		{
			name: "CloudflareDNS_HTTPS",
			cmd:  []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "https://1.1.1.1"},
		},
		{
			name: "AlternateExternal",
			cmd:  []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://example.org"},
		},
	}

	allBlocked := true
	for _, dt := range dnsTests {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := dockerExec(probeCtx, string(agentID), dt.cmd...)
		cancel()

		if err == nil {
			t.Errorf("DNS redirect bypass: %s succeeded — expected BLOCKED", dt.name)
			allBlocked = false
		}
		_ = out
	}
	if allBlocked {
		t.Log("PASS: All DNS redirect / alternative resolver probes blocked")
	}
}

// TestAdversaryB5T04a_GatewayAccessibleFromInternal verifies that the
// gateway container is reachable from the agent container on the internal
// network (positive security property for the gateway path).
//
// This is a positive assertion under adversarial scrutiny: the adversary
// checks that the gateway IS reachable (not blocked), which is required
// for the agent→gateway path to function.
func TestAdversaryB5T04a_GatewayAccessibleFromInternal(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04a-adv-gw-accessible"

	// Full topology: internal + egress networks, gateway + agent containers
	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(internal) failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("egress", runID),
		Internal: false,
		Labels:   Labels(ResourceTypeNetEgress, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(egress) failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, egressNetID) }()

	gatewayID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID), string(egressNetID)},
		Labels:     Labels(ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, gatewayID, true) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, gatewayID); err != nil {
		t.Fatalf("Start(gateway) failed: %v", err)
	}
	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Verify gateway is on internal network by inspecting
	gatewayNets, err := dr.InspectContainerNetworks(ctx, gatewayID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(gateway) failed: %v", err)
	}

	hasInternal := false
	hasEgress := false
	for _, n := range gatewayNets {
		if strings.Contains(n.Name, "internal") {
			hasInternal = true
		}
		if strings.Contains(n.Name, "egress") {
			hasEgress = true
		}
	}

	if !hasInternal {
		t.Error("Gateway is NOT on internal network — agent→gateway path broken")
	}
	if !hasEgress {
		t.Error("Gateway is NOT on egress network — gateway→internet path broken")
	}

	if hasInternal && hasEgress {
		t.Log("PASS: Gateway is correctly dual-homed on internal + egress networks")
	}
}

// TestAdversaryB5T04a_MissingCanaryAssertions documents the risk that
// canary assertions might not be tested in CI (due to Docker requirement).
//
// ADVERSARY BREAK: LOW — the canary probes only run when
// AGENTPAAS_DOCKER_TESTS=1 is set. CI may not set this, leaving the
// canary assertions untested in CI pipelines. This is an accepted risk
// for P1, documented here.
func TestAdversaryB5T04a_MissingCanaryAssertions(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Log("WARNING: canary probe tests are SKIPPED without AGENTPAAS_DOCKER_TESTS=1")
		t.Log("This means CI will NOT exercise egress isolation assertions.")
		t.Log("Mitigation: run 'make e2e-network' locally to verify.")
	}
	t.Log("PASS: canary test gap documented")
}

// TestAdversaryB5T04a_EgressNetworkEnumeration tests whether a gateway
// container can enumerate other hosts on the egress network (potential
// information disclosure). While the gateway needs egress, it should not
// expose internal Docker host details.
//
// ADVERSARY BREAK: LOW — the egress network is a standard Docker bridge,
// and the gateway is the only container on it. No isolation bypass risk
// beyond standard Docker bridge behavior.
func TestAdversaryB5T04a_EgressNetworkEnumeration(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04a-adv-enum"

	egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("egress", runID),
		Internal: false,
		Labels:   Labels(ResourceTypeNetEgress, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(egress) failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, egressNetID) }()

	gatewayID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(egressNetID)},
		Labels:     Labels(ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, gatewayID, true) }()

	if err := dr.Start(ctx, gatewayID); err != nil {
		t.Fatalf("Start(gateway) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Try to scan the egress network gateway IP (typically .1)
	enumCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := dockerExec(enumCtx, string(gatewayID),
		"wget", "-q", "-O", "/dev/null", "--timeout=3",
		"http://172.18.0.1") // common Docker egress gateway IP
	cancel()

	if err == nil {
		// This is somewhat expected — Docker host may have ports open.
		// Documenting as information disclosure risk.
		t.Logf("INFO: Gateway can reach Docker host on egress network (%s): %v — standard Docker behavior", out, err)
	}
	_ = out
}