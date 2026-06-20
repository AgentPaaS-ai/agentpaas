package runtime

import (
	"testing"
)

func TestContainerName_Agent(t *testing.T) {
	name := ContainerName("agent", "run-abc123")
	expected := "agentpaas-agent-run-abc123"
	if name != expected {
		t.Errorf("ContainerName(agent, run-abc123) = %q, want %q", name, expected)
	}
}

func TestContainerName_Gateway(t *testing.T) {
	name := ContainerName("gateway", "run-abc123")
	expected := "agentpaas-gateway-run-abc123"
	if name != expected {
		t.Errorf("ContainerName(gateway, run-abc123) = %q, want %q", name, expected)
	}
}

func TestContainerName_EmptyID(t *testing.T) {
	name := ContainerName("agent", "")
	expected := "agentpaas-agent-"
	if name != expected {
		t.Errorf("ContainerName(agent, empty) = %q, want %q", name, expected)
	}
}

func TestContainerName_DifferentIDs(t *testing.T) {
	n1 := ContainerName("agent", "id1")
	n2 := ContainerName("agent", "id2")
	if n1 == n2 {
		t.Error("ContainerName should produce different names for different IDs")
	}
}

func TestNetworkName_Internal(t *testing.T) {
	name := NetworkName("internal", "run-abc123")
	expected := "agentpaas-net-internal-run-abc123"
	if name != expected {
		t.Errorf("NetworkName(internal, run-abc123) = %q, want %q", name, expected)
	}
}

func TestNetworkName_Egress(t *testing.T) {
	name := NetworkName("egress", "run-abc123")
	expected := "agentpaas-net-egress-run-abc123"
	if name != expected {
		t.Errorf("NetworkName(egress, run-abc123) = %q, want %q", name, expected)
	}
}

func TestNetworkName_DifferentRunIDs(t *testing.T) {
	n1 := NetworkName("internal", "run-one")
	n2 := NetworkName("internal", "run-two")
	if n1 == n2 {
		t.Error("NetworkName should produce different names for different run IDs")
	}
}

func TestLabels_Agent(t *testing.T) {
	labels := Labels("agent", "run-abc123")
	if labels == nil {
		t.Fatal("Labels returned nil")
	}

	tests := []struct {
		key, expected string
	}{
		{"agentpaas.managed-by", "agentpaas"},
		{"agentpaas.resource-type", "agent"},
		{"agentpaas.run-id", "run-abc123"},
	}
	for _, tt := range tests {
		got, ok := labels[tt.key]
		if !ok {
			t.Errorf("Labels missing key %q", tt.key)
			continue
		}
		if got != tt.expected {
			t.Errorf("Labels[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestLabels_Gateway(t *testing.T) {
	labels := Labels("gateway", "run-xyz789")
	if labels == nil {
		t.Fatal("Labels returned nil")
	}

	tests := []struct {
		key, expected string
	}{
		{"agentpaas.managed-by", "agentpaas"},
		{"agentpaas.resource-type", "gateway"},
		{"agentpaas.run-id", "run-xyz789"},
	}
	for _, tt := range tests {
		got, ok := labels[tt.key]
		if !ok {
			t.Errorf("Labels missing key %q", tt.key)
			continue
		}
		if got != tt.expected {
			t.Errorf("Labels[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestLabels_NetworkInternal(t *testing.T) {
	labels := Labels("net-internal", "run-abc123")
	if labels["agentpaas.resource-type"] != "net-internal" {
		t.Errorf("resource-type = %q, want net-internal", labels["agentpaas.resource-type"])
	}
}

func TestLabels_NetworkEgress(t *testing.T) {
	labels := Labels("net-egress", "run-abc123")
	if labels["agentpaas.resource-type"] != "net-egress" {
		t.Errorf("resource-type = %q, want net-egress", labels["agentpaas.resource-type"])
	}
}

func TestIsOwned_WithAgentPaaSLabels(t *testing.T) {
	labels := map[string]string{
		"agentpaas.managed-by":    "agentpaas",
		"agentpaas.resource-type": "agent",
		"agentpaas.run-id":        "run-abc123",
	}
	if !IsOwned(labels) {
		t.Error("IsOwned should return true for labels with agentpaas.managed-by")
	}
}

func TestIsOwned_WithoutLabels(t *testing.T) {
	if IsOwned(nil) {
		t.Error("IsOwned should return false for nil labels")
	}
}

func TestIsOwned_EmptyLabels(t *testing.T) {
	if IsOwned(map[string]string{}) {
		t.Error("IsOwned should return false for empty labels")
	}
}

func TestIsOwned_NonAgentPaaS(t *testing.T) {
	labels := map[string]string{
		"some-other.managed-by": "other",
	}
	if IsOwned(labels) {
		t.Error("IsOwned should return false for labels without agentpaas.managed-by")
	}
}

func TestIsOwned_WrongValue(t *testing.T) {
	labels := map[string]string{
		"agentpaas.managed-by": "not-agentpaas",
	}
	if IsOwned(labels) {
		t.Error("IsOwned should return false when managed-by value is not agentpaas")
	}
}

func TestContainerName_Deterministic(t *testing.T) {
	// Same inputs must produce same outputs
	n1 := ContainerName("agent", "run-abc123")
	n2 := ContainerName("agent", "run-abc123")
	if n1 != n2 {
		t.Error("ContainerName must be deterministic (same inputs produce same output)")
	}
}

func TestNetworkName_Deterministic(t *testing.T) {
	n1 := NetworkName("internal", "run-abc123")
	n2 := NetworkName("internal", "run-abc123")
	if n1 != n2 {
		t.Error("NetworkName must be deterministic (same inputs produce same output)")
	}
}

func TestLabels_Deterministic(t *testing.T) {
	l1 := Labels("agent", "run-abc123")
	l2 := Labels("agent", "run-abc123")
	for k, v := range l1 {
		if l2[k] != v {
			t.Errorf("Labels not deterministic: %q changed from %q to %q", k, v, l2[k])
		}
	}
}

// Test that LabelAgentPaaS constants exist and have expected values
func TestLabelConstants(t *testing.T) {
	if LabelManagedBy != "agentpaas.managed-by" {
		t.Errorf("LabelManagedBy = %q, want %q", LabelManagedBy, "agentpaas.managed-by")
	}
	if LabelResourceType != "agentpaas.resource-type" {
		t.Errorf("LabelResourceType = %q, want %q", LabelResourceType, "agentpaas.resource-type")
	}
	if LabelRunID != "agentpaas.run-id" {
		t.Errorf("LabelRunID = %q, want %q", LabelRunID, "agentpaas.run-id")
	}
	if ManagedByValue != "agentpaas" {
		t.Errorf("ManagedByValue = %q, want %q", ManagedByValue, "agentpaas")
	}
}

func TestLabelResourceTypes(t *testing.T) {
	if ResourceTypeAgent != "agent" {
		t.Errorf("ResourceTypeAgent = %q, want %q", ResourceTypeAgent, "agent")
	}
	if ResourceTypeGateway != "gateway" {
		t.Errorf("ResourceTypeGateway = %q, want %q", ResourceTypeGateway, "gateway")
	}
	if ResourceTypeNetInternal != "net-internal" {
		t.Errorf("ResourceTypeNetInternal = %q, want %q", ResourceTypeNetInternal, "net-internal")
	}
	if ResourceTypeNetEgress != "net-egress" {
		t.Errorf("ResourceTypeNetEgress = %q, want %q", ResourceTypeNetEgress, "net-egress")
	}
}