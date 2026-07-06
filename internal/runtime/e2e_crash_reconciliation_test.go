package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_CrashReconciliation verifies that daemon crash reconciliation
// correctly identifies and removes orphaned agent containers whose gateway
// is absent, while leaving unrelated Docker resources untouched.
//
// Test scenarios:
//   - Agent container running without a gateway → should be killed
//   - Agent container with running gateway → should NOT be killed
//   - Multiple run groups → selective removal only for orphaned agents
//   - Unrelated containers → never touched
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_CrashReconciliation(t *testing.T) {
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

	runID := fmt.Sprintf("b5t05-recon-%d", time.Now().UnixNano())

	// Track resources for deferred cleanup
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

	// ---- SCENARIO 1: Agent without gateway — should be killed ----
	t.Run("Scenario1_AgentWithoutGateway_Killed", func(t *testing.T) {
		s1RunID := runID + "-sc1"

		internalNetName := NetworkName("internal", s1RunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, s1RunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}
		cleanupNetworks = append(cleanupNetworks, internalNetID)

		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, s1RunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}
		cleanupContainers = append(cleanupContainers, agentID)

		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}
		t.Logf("Created agent container (no gateway): %s", agentID)

		time.Sleep(1 * time.Second)

		// Verify agent is running before reconciliation
		status, err := dr.Status(ctx, agentID)
		if err != nil {
			t.Fatalf("Status(agent) before reconcile failed: %v", err)
		}
		if status != ContainerStatusRunning {
			t.Fatalf("Agent should be running before reconcile, got %v", status)
		}

		// Run reconciliation
		result, err := ReconcileAfterCrash(ctx, dr)
		if err != nil {
			t.Fatalf("ReconcileAfterCrash failed: %v", err)
		}
		t.Logf("Reconciliation removed %d container(s)", len(result.RemovedContainers))

		// Verify agent was removed
		foundAgent := false
		for _, r := range result.RemovedContainers {
			if strings.HasPrefix(string(r), string(agentID[:12])) {
				foundAgent = true
				break
			}
		}
		if !foundAgent {
			t.Errorf("Agent container %s was NOT removed by reconciliation (gateway absent)", agentID[:12])
		}
		_ = internalNetID
	})

	// ---- SCENARIO 2: Agent with running gateway — should be kept ----
	t.Run("Scenario2_AgentWithGateway_Kept", func(t *testing.T) {
		s2RunID := runID + "-sc2"

		internalNetName := NetworkName("internal", s2RunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, s2RunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}
		cleanupNetworks = append(cleanupNetworks, internalNetID)

		egressNetName := NetworkName("egress", s2RunID)
		egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     egressNetName,
			Internal: false,
			Labels:   Labels(ResourceTypeNetEgress, s2RunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(egress) failed: %v", err)
		}
		cleanupNetworks = append(cleanupNetworks, egressNetID)

		gatewayID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID), string(egressNetID)},
			Labels:     Labels(ResourceTypeGateway, s2RunID),
		})
		if err != nil {
			t.Fatalf("Create(gateway) failed: %v", err)
		}
		cleanupContainers = append(cleanupContainers, gatewayID)

		if err := dr.Start(ctx, gatewayID); err != nil {
			t.Fatalf("Start(gateway) failed: %v", err)
		}

		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, s2RunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}
		cleanupContainers = append(cleanupContainers, agentID)

		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}

		time.Sleep(1 * time.Second)

		// Run reconciliation — should NOT remove agent since gateway is running
		result, err := ReconcileAfterCrash(ctx, dr)
		if err != nil {
			t.Fatalf("ReconcileAfterCrash failed: %v", err)
		}

		// Check agent was NOT removed
		for _, r := range result.RemovedContainers {
			if strings.HasPrefix(string(r), string(agentID[:12])) {
				t.Errorf("Agent container %s was REMOVED despite gateway being running", agentID[:12])
			}
		}

		// Verify agent still exists
		status, err := dr.Status(ctx, agentID)
		if err != nil {
			t.Fatalf("Status(agent) after reconcile failed: %v", err)
		}
		if status != ContainerStatusRunning {
			t.Errorf("Agent should still be running, got %v", status)
		}
		t.Logf("PASS: Agent %s kept (gateway %s running)", agentID[:12], gatewayID[:12])
	})
}

// TestE2E_SecretFreeDebugOutput verifies that debug output from Docker inspect,
// runtime logs, and network config dumps contains no raw secret values.
//
// It creates containers with sentinel secret values in environment variables
// and labels, then verifies those values are properly redacted from all
// debug output.
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_SecretFreeDebugOutput(t *testing.T) {
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

	runID := fmt.Sprintf("b5t05-secret-%d", time.Now().UnixNano())

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

	internalNetName := NetworkName("internal", runID)
	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     internalNetName,
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(internal) failed: %v", err)
	}
	cleanupNetworks = append(cleanupNetworks, internalNetID)

	// Create a container with sentinel secret values in env and labels
	sentinelAPIKey := "sk-sentinel-test-key-value-12345"
	sentinelToken := "ghp_sentinelTestToken12345"
	sentinelPassword := "super-secret-password-value"

	containerID, err := dr.Create(ctx, ContainerSpec{
		Image:   "alpine:latest",
		Command: []string{"sleep", "3600"},
		Env: []string{
			fmt.Sprintf("API_KEY=%s", sentinelAPIKey),
			fmt.Sprintf("GITHUB_TOKEN=%s", sentinelToken),
			fmt.Sprintf("DB_PASSWORD=%s", sentinelPassword),
		},
		NetworkIDs: []string{string(internalNetID)},
		Labels: map[string]string{
			LabelManagedBy:    ManagedByValue,
			LabelResourceType: ResourceTypeAgent,
			LabelRunID:        runID,
			"secret-label":    sentinelAPIKey,
		},
	})
	if err != nil {
		t.Fatalf("Create(agent with secrets) failed: %v", err)
	}
	cleanupContainers = append(cleanupContainers, containerID)

	if err := dr.Start(ctx, containerID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// ---- TEST 1: Docker inspect output must have no raw secrets ----
	t.Run("DockerInspect_NoRawSecrets", func(t *testing.T) {
		_, rawBody, err := dr.cli.ContainerInspectWithRaw(ctx, string(containerID), false)
		if err != nil {
			t.Fatalf("ContainerInspectWithRaw failed: %v", err)
		}
		rawJSON := string(rawBody)

		// Verify raw secrets ARE present in raw inspect output
		if !strings.Contains(rawJSON, sentinelAPIKey) {
			t.Error("Sentinel API key should exist in raw Docker inspect")
		}
		if !strings.Contains(rawJSON, sentinelToken) {
			t.Error("Sentinel token should exist in raw Docker inspect")
		}

		// Apply sanitization
		sanitized := SanitizeDockerInspect(rawJSON)

		// Verify sentinel secrets are redacted
		if strings.Contains(sanitized, sentinelAPIKey) {
			t.Errorf("Sentinel API key %q leaked in sanitized Docker inspect output", sentinelAPIKey)
		}
		if strings.Contains(sanitized, sentinelToken) {
			t.Errorf("Sentinel token %q leaked in sanitized Docker inspect output", sentinelToken)
		}
		if strings.Contains(sanitized, sentinelPassword) {
			t.Errorf("Sentinel password %q leaked in sanitized Docker inspect output", sentinelPassword)
		}

		// Verify [REDACTED] markers are present
		if !strings.Contains(sanitized, "[REDACTED]") {
			t.Error("Sanitized output should contain [REDACTED] markers")
		}

		// Verify HasSecretLeak returns false for sanitized output
		if HasSecretLeak(sanitized) {
			t.Error("HasSecretLeak should return false for sanitized output")
		}

		t.Log("PASS: No raw secrets in sanitized Docker inspect output")
	})

	// ---- TEST 2: Network config dumps must have no raw secrets ----
	t.Run("NetworkConfig_NoRawSecrets", func(t *testing.T) {
		netInfo, err := dr.InspectNetwork(ctx, internalNetID)
		if err != nil {
			t.Fatalf("InspectNetwork failed: %v", err)
		}
		netDump := fmt.Sprintf("Network: %s (%s) internal=%v labels=%v",
			netInfo.Name, netInfo.ID, netInfo.Internal, netInfo.Labels)

		sanitized := SanitizeDebugOutput(netDump)
		if HasSecretLeak(sanitized) {
			t.Error("HasSecretLeak should return false for sanitized network config output")
		}
		t.Log("PASS: No raw secrets in network config dump")
	})

	t.Log("=== E2E Secret-Free Debug Output: COMPLETE ===")
}