package mcpmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

// ADVERSARY BREAK: ListContainers filter uses only LabelMCPServerID=serverID (Docker label filter);
// any container setting that label (even without managed-by=agentpaas or resource-type=mcp) is included.
// Spoofed containers can inject sidecar info into status report.
func TestAdversary_B7M_T05_SpoofedContainerWithoutOwnershipLabels(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "http-spoof",
		Transport: "http",
		URL:       "http://example.invalid/mcp",
	}}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["spoof-1"] = runtime.ContainerInfo{
		ID:     "spoof-1",
		Status: runtime.ContainerStatusRunning,
		Labels: map[string]string{
			runtime.LabelMCPServerID: "http-spoof",
			// deliberately missing: LabelManagedBy, LabelResourceType, LabelRunID
			"malicious": "injected",
		},
	}
	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("GenerateStatusReport error = %v", err)
	}
	if len(report.Sidecars) != 1 {
		t.Fatalf("sidecars = %d, want 1 (spoof accepted)", len(report.Sidecars))
	}
}

// ADVERSARY BREAK: agentpaasLabels whitelists keys but copies values verbatim.
// A spoofed container can set e.g. agentpaas.run-id containing secret value
// and the value reaches the JSON status report unredacted (potential secret leak).
func TestAdversary_B7M_T05_AllowedLabelValuesNotRedacted(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "http-labelval",
		Transport: "http",
		URL:       "http://example.invalid/mcp",
	}}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["labelval-1"] = runtime.ContainerInfo{
		ID:     "labelval-1",
		Status: runtime.ContainerStatusRunning,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelRunID:        "sk-liv...3456", // spoofed value under allowed key
			runtime.LabelMCPServerID:  "http-labelval",
		},
	}
	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	payload := string(mustMarshal(report))
	if !strings.Contains(payload, "sk-liv...3456") {
		t.Fatal("expected spoofed secret value under allowed label key to appear in report")
	}
}

// ADVERSARY BREAK: stateFromContainerStatus maps only Running/Stopped/Removed explicitly;
// Paused, Unknown, and other Docker states fall to default Unhealthy/Failed.
// Incomplete mapping for all runtime.ContainerStatus values.
func TestAdversary_B7M_T05_IncompleteStateMapping(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "http-paused",
		Transport: "http",
		URL:       "http://example.invalid/mcp",
	}}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["paused-1"] = runtime.ContainerInfo{
		ID:     "paused-1",
		Status: runtime.ContainerStatusPaused,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelRunID:        "run-1",
			runtime.LabelMCPServerID:  "http-paused",
		},
	}
	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(report.Sidecars) != 1 {
		t.Fatal("no sidecar")
	}
	s := report.Sidecars[0]
	if s.Readiness != ReadinessUnhealthy || s.Health != HealthFailed {
		t.Fatalf("paused mapped to (%s,%s), want unhealthy/failed per default", s.Readiness, s.Health)
	}
}

// ADVERSARY BREAK: collectSidecars + ListContainers uses single-label filter; malicious container
// can set LabelMCPServerID to a valid serverID even if it does not belong to that server (no additional
// ownership or signature check).
func TestAdversary_B7M_T05_ContainerIDMismatchViaLabelSpoof(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "http-real",
		Transport: "http",
		URL:       "http://example.invalid/mcp",
	}}, "agent-1", "run-1")
	driver := newStatusFakeRuntimeDriver()
	driver.containers["wrong-1"] = runtime.ContainerInfo{
		ID:     "wrong-1",
		Status: runtime.ContainerStatusRunning,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelMCPServerID:  "http-real", // label matches but container is malicious/wrong
		},
	}
	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(report.Sidecars) != 1 || report.Sidecars[0].ContainerID != "wrong-1" {
		t.Fatal("spoofed container ID accepted without further validation")
	}
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}