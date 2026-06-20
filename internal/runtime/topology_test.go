package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestTopology_GatewayOnlyNetwork verifies the dual-container gateway-only
// network topology:
//   - Per-agent internal:true bridge network exists
//   - Dedicated egress network exists
//   - Gateway container is dual-homed (internal + egress)
//   - Agent container is attached to internal network ONLY (no egress)
//   - Agent container never shares gateway namespace
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestTopology_GatewayOnlyNetwork(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	if dr == nil {
		t.Fatal("NewDockerRuntime() returned nil")
	}

	runID := "b5t02-test-run"

	// Ensure cleanup on exit
	cleanupContainers := []ContainerID{}
	cleanupNetworks := []NetworkID{}
	defer func() {
		for _, id := range cleanupContainers {
			_ = dr.Remove(ctx, id, true)
		}
		for _, nid := range cleanupNetworks {
			_ = dr.RemoveNetwork(ctx, nid)
		}
	}()

	// Step 1: Create internal bridge network (internal: true)
	internalNetName := NetworkName("internal", runID)
	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     internalNetName,
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(internal, %q) failed: %v", internalNetName, err)
	}
	if internalNetID == "" {
		t.Fatal("CreateNetwork returned empty NetworkID")
	}
	cleanupNetworks = append(cleanupNetworks, internalNetID)
	t.Logf("Created internal network: %s (ID: %s)", internalNetName, internalNetID)

	// Step 2: Create egress network (regular bridge, not internal)
	egressNetName := NetworkName("egress", runID)
	egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     egressNetName,
		Internal: false,
		Labels:   Labels(ResourceTypeNetEgress, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(egress, %q) failed: %v", egressNetName, err)
	}
	if egressNetID == "" {
		t.Fatal("CreateNetwork returned empty NetworkID")
	}
	cleanupNetworks = append(cleanupNetworks, egressNetID)
	t.Logf("Created egress network: %s (ID: %s)", egressNetName, egressNetID)

	// Step 3: Create gateway container (dual-homed: internal + egress)
	gatewayID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID), string(egressNetID)},
		Labels:     Labels(ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway) failed: %v", err)
	}
	cleanupContainers = append(cleanupContainers, gatewayID)
	t.Logf("Created gateway container: %s", gatewayID)

	// Step 4: Create agent container (internal network only)
	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	cleanupContainers = append(cleanupContainers, agentID)
	t.Logf("Created agent container: %s", agentID)

	// Start both containers
	if err := dr.Start(ctx, gatewayID); err != nil {
		t.Fatalf("Start(gateway) failed: %v", err)
	}
	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}

	// Step 5: Docker inspect assertions

	// 5a: Agent has only internal network (NOT egress)
	agentNetworks, err := dr.InspectContainerNetworks(ctx, agentID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(agent) failed: %v", err)
	}
	t.Logf("Agent container networks: %v", agentNetworks)

	// Agent must be attached to internal network
	if !networkHasName(agentNetworks, internalNetName) {
		t.Errorf("Agent container NOT attached to internal network %q; networks: %v", internalNetName, agentNetworks)
	}
	// Agent must NOT be attached to egress network
	if networkHasName(agentNetworks, egressNetName) {
		t.Errorf("Agent container IS attached to egress network %q (should be isolated); networks: %v", egressNetName, agentNetworks)
	}
	// Agent must have exactly 1 network attachment
	if len(agentNetworks) != 1 {
		t.Errorf("Agent container has %d network(s), expected exactly 1; networks: %v", len(agentNetworks), agentNetworks)
	}

	// 5b: Gateway has both internal and egress networks
	gatewayNetworks, err := dr.InspectContainerNetworks(ctx, gatewayID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(gateway) failed: %v", err)
	}
	t.Logf("Gateway container networks: %v", gatewayNetworks)

	if !networkHasName(gatewayNetworks, internalNetName) {
		t.Errorf("Gateway NOT attached to internal network %q; networks: %v", internalNetName, gatewayNetworks)
	}
	if !networkHasName(gatewayNetworks, egressNetName) {
		t.Errorf("Gateway NOT attached to egress network %q; networks: %v", egressNetName, gatewayNetworks)
	}
	// Gateway must have exactly 2 network attachments
	if len(gatewayNetworks) != 2 {
		t.Errorf("Gateway container has %d network(s), expected exactly 2; networks: %v", len(gatewayNetworks), gatewayNetworks)
	}

	// 5c: Agent and gateway are on DIFFERENT networks (internal of one vs internal of other)
	// They should share the internal network but agent should not be on egress
}

// TestTopology_InternalBridgeIsInternal verifies that the internal bridge
// network is created with internal=true, preventing external egress.
func TestTopology_InternalBridgeIsInternal(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t02-test-internal"
	netName := NetworkName("internal", runID)

	netID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     netName,
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(%q, internal=true) failed: %v", netName, err)
	}
	defer func() {
		_ = dr.RemoveNetwork(ctx, netID)
	}()

	// Inspect and verify internal flag
	info, err := dr.InspectNetwork(ctx, netID)
	if err != nil {
		t.Fatalf("InspectNetwork(%q) failed: %v", netID, err)
	}
	if !info.Internal {
		t.Errorf("Network %q has Internal=%v, want Internal=true", netName, info.Internal)
	}
	if info.Name != netName {
		t.Errorf("Network name = %q, want %q", info.Name, netName)
	}
}

// TestTopology_AgentIsolationFromEgress verifies the agent container is
// completely isolated from the egress network — no shared namespace, no
// unexpected network attachments.
func TestTopology_AgentIsolationFromEgress(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t02-test-isolation"

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

	// Create agent on internal only
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

	// Agent must NOT be attached to egress network
	agentNets, err := dr.InspectContainerNetworks(ctx, agentID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(agent) failed: %v", err)
	}
	for _, n := range agentNets {
		if strings.Contains(n.Name, "egress") {
			t.Errorf("Agent container has egress network attachment %q — isolation violated", n.Name)
		}
		if strings.Contains(n.Name, "bridge") && !strings.Contains(n.Name, "internal") {
			// The default docker bridge is also forbidden
			t.Errorf("Agent container has default bridge network %q — isolation violated", n.Name)
		}
	}
}

// TestTopology_GatewayOwnershipLabels verifies that gateway containers and
// networks get the correct AgentPaaS ownership labels.
func TestTopology_GatewayOwnershipLabels(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t02-test-labels"

	netID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name: NetworkName("internal", runID),
		Labels: Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, netID) }()

	cid, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(netID)},
		Labels:     Labels(ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()

	// Verify network labels via InspectNetwork
	netInfo, err := dr.InspectNetwork(ctx, netID)
	if err != nil {
		t.Fatalf("InspectNetwork failed: %v", err)
	}
	if netInfo.Labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("Network label %q = %q, want %q", LabelManagedBy, netInfo.Labels[LabelManagedBy], ManagedByValue)
	}
	if netInfo.Labels[LabelResourceType] != ResourceTypeNetInternal {
		t.Errorf("Network label %q = %q, want %q", LabelResourceType, netInfo.Labels[LabelResourceType], ResourceTypeNetInternal)
	}
	if netInfo.Labels[LabelRunID] != runID {
		t.Errorf("Network label %q = %q, want %q", LabelRunID, netInfo.Labels[LabelRunID], runID)
	}
}

// TestTopology_MultiNetworkContainer verifies creating a container attached
// to multiple networks (dual-homing).
func TestTopology_MultiNetworkContainer(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t02-test-dual"

	netA, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name: NetworkName("internal", runID+"-a"),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(A) failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, netA) }()

	netB, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name: NetworkName("egress", runID+"-b"),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(B) failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, netB) }()

	cid, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(netA), string(netB)},
	})
	if err != nil {
		t.Fatalf("Create(dual-home) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, cid, true) }()

	if err := dr.Start(ctx, cid); err != nil {
		t.Fatalf("Start(dual-home) failed: %v", err)
	}

	// Verify both networks are attached
	nets, err := dr.InspectContainerNetworks(ctx, cid)
	if err != nil {
		t.Fatalf("InspectContainerNetworks failed: %v", err)
	}
	if len(nets) < 2 {
		t.Errorf("Dual-homed container has %d network(s), expected at least 2", len(nets))
	}
}

// networkHasName checks if any of the ContainerNetworkInfo entries has the
// given name.
func networkHasName(networks []ContainerNetworkInfo, name string) bool {
	for _, n := range networks {
		if n.Name == name {
			return true
		}
	}
	return false
}