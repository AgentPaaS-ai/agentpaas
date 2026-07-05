package mcpmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

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
	if len(report.Sidecars) != 0 {
		t.Fatalf("sidecars = %d, want 0 (spoof rejected)", len(report.Sidecars))
	}
}

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
			runtime.LabelRunID:        "run-1\x00inject\x1b[31m" + strings.Repeat("x", 200),
			runtime.LabelMCPServerID:  "http-labelval",
		},
	}
	report, err := GenerateStatusReport(context.Background(), manager, nil, driver)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	payload := string(mustMarshal(report))
	if strings.Contains(payload, "\x00") {
		t.Fatal("expected control character to be escaped in label value")
	}
	if strings.Contains(payload, strings.Repeat("x", 200)) {
		t.Fatal("expected excessive label value to be truncated")
	}
	if !strings.Contains(payload, "run-1?inject?[31m") {
		t.Fatal("expected sanitized label value to appear in report")
	}
}

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
	if s.Readiness != ReadinessStarting || s.Health != HealthUnknown {
		t.Fatalf("paused mapped to (%s,%s), want (starting,unknown)", s.Readiness, s.Health)
	}
}

func TestAdversary_B7M_T05_OwnedContainerWithMatchingLabelsAccepted(t *testing.T) {
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
	// Known limitation: a container with valid AgentPaaS ownership labels and
	// a matching MCPServerID is accepted. In production, labels are daemon-set,
	// so spoofing requires daemon access.
	if len(report.Sidecars) != 1 || report.Sidecars[0].ContainerID != "wrong-1" {
		t.Fatal("owned container with matching labels was not accepted")
	}
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
