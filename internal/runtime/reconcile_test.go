package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestReconcileAfterCrash_NilDriver(t *testing.T) {
	_, err := ReconcileAfterCrash(context.Background(), nil)
	if err == nil {
		t.Error("ReconcileAfterCrash with nil driver should return error")
	}
}

func TestReconcileAfterCrash_NoOwnedContainers(t *testing.T) {
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{}, nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Expected 0 removals, got %d", len(removed))
	}
}

func TestReconcileAfterCrash_ListError(t *testing.T) {
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return nil, errors.New("docker error")
		},
	}

	_, err := ReconcileAfterCrash(context.Background(), mock)
	if err == nil {
		t.Error("ReconcileAfterCrash should propagate list error")
	}
}

func TestReconcileAfterCrash_GatewayRunning_AgentKept(t *testing.T) {
	removeCalled := false
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{
				{
					ID:           "gw-abc",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeGateway,
					Labels:       Labels(ResourceTypeGateway, "run-1"),
				},
				{
					ID:           "agent-def",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
			}, nil
		},
		removeFunc: func(_ context.Context, _ ContainerID, _ bool) error {
			removeCalled = true
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Expected 0 removals (gateway is running), got %d", len(removed))
	}
	if removeCalled {
		t.Error("Remove should not be called when gateway is running")
	}
}

func TestReconcileAfterCrash_GatewayAbsent_AgentRemoved(t *testing.T) {
	capturedCID := ContainerID("")
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{
				{
					ID:           "agent-abc",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
			}, nil
		},
		removeFunc: func(_ context.Context, id ContainerID, force bool) error {
			capturedCID = id
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("Expected 1 removal, got %d", len(removed))
	}
	if string(capturedCID) != "agent-abc" {
		t.Errorf("Expected remove to be called with 'agent-abc', got %q", capturedCID)
	}
}

func TestReconcileAfterCrash_MultipleRuns_SelectiveRemoval(t *testing.T) {
	var removedIDs []ContainerID
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{
				// Run 1: gateway running + agent — should be KEPT
				{
					ID:           "gw-run1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeGateway,
					Labels:       Labels(ResourceTypeGateway, "run-1"),
				},
				{
					ID:           "agent-run1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
				// Run 2: agent only, no gateway — should be REMOVED
				{
					ID:           "agent-run2",
					Status:       ContainerStatusRunning,
					RunID:        "run-2",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-2"),
				},
				// Run 3: gateway stopped + agent running — should be REMOVED
				{
					ID:           "gw-run3",
					Status:       ContainerStatusStopped,
					RunID:        "run-3",
					ResourceType: ResourceTypeGateway,
					Labels:       Labels(ResourceTypeGateway, "run-3"),
				},
				{
					ID:           "agent-run3",
					Status:       ContainerStatusRunning,
					RunID:        "run-3",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-3"),
				},
			}, nil
		},
		removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
			removedIDs = append(removedIDs, id)
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 2 {
		t.Errorf("Expected 2 removals (agent-run2, agent-run3), got %d: %v", len(removed), removed)
	}
}

func TestReconcileAfterCrash_UnrelatedContainersUntouched(t *testing.T) {
	var removedIDs []ContainerID
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, labelFilters ...string) ([]ContainerInfo, error) {
			// Only return AgentPaaS-owned containers (matching the label filter).
			// The reconciler only sees owned containers via the filter.
			return []ContainerInfo{
				// Run 1: gateway absent, agent running — should be REMOVED
				{
					ID:           "agent-only",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
			}, nil
		},
		removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
			removedIDs = append(removedIDs, id)
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("Expected 1 removal (only AgentPaaS-owned), got %d", len(removed))
	}
}

func TestReconcileAfterCrash_RemoveError_ReturnsPartial(t *testing.T) {
	removeCount := 0
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{
				{
					ID:           "agent-1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
				{
					ID:           "agent-2",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
			}, nil
		},
		removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
			removeCount++
			if removeCount == 1 {
				return errors.New("remove failed")
			}
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err == nil {
		t.Error("ReconcileAfterCrash should propagate remove error")
	}
	if len(removed) != 0 {
		t.Errorf("Expected 0 removals before first failure, got %d: %v", len(removed), removed)
	}
}

func TestReconcileAfterCrash_ContainerWithoutRunID_Skipped(t *testing.T) {
	removeCalled := false
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{
				{
					ID:           "no-run-id",
					Status:       ContainerStatusRunning,
					RunID:        "",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, ""),
				},
			}, nil
		},
		removeFunc: func(_ context.Context, _ ContainerID, _ bool) error {
			removeCalled = true
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Expected 0 removals (container has no run ID), got %d", len(removed))
	}
	if removeCalled {
		t.Error("Remove should not be called for containers without run ID")
	}
}

func TestReconcileAfterCrash_OrphanedMCPRemoved(t *testing.T) {
	var removedIDs []ContainerID
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
			return []ContainerInfo{
				{
					ID:           "gateway-run1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeGateway,
					Labels:       Labels(ResourceTypeGateway, "run-1"),
				},
				{
					ID:           "agent-run1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
				{
					ID:           "mcp-run1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeMCP,
					Labels: map[string]string{
						LabelManagedBy:    ManagedByValue,
						LabelResourceType: ResourceTypeMCP,
						LabelRunID:        "run-1",
						LabelMCPServerID:  "stdio",
					},
				},
				{
					ID:           "mcp-run2",
					Status:       ContainerStatusRunning,
					RunID:        "run-2",
					ResourceType: ResourceTypeMCP,
					Labels: map[string]string{
						LabelManagedBy:    ManagedByValue,
						LabelResourceType: ResourceTypeMCP,
						LabelRunID:        "run-2",
						LabelMCPServerID:  "http",
					},
				},
			}, nil
		},
		removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
			removedIDs = append(removedIDs, id)
			return nil
		},
	}

	removed, err := ReconcileAfterCrash(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileAfterCrash failed: %v", err)
	}
	if len(removed) != 1 || removed[0] != "mcp-run2" {
		t.Fatalf("removed = %v, want [mcp-run2]", removed)
	}
	if len(removedIDs) != 1 || removedIDs[0] != "mcp-run2" {
		t.Fatalf("Remove called with %v, want [mcp-run2]", removedIDs)
	}
}

func TestReconcileMCPServersDiscoversLabeledContainers(t *testing.T) {
	mock := &mockRuntimeDriver{
		listContainersFunc: func(_ context.Context, labelFilters ...string) ([]ContainerInfo, error) {
			wantFilters := map[string]bool{
				LabelManagedBy + "=" + ManagedByValue:     true,
				LabelResourceType + "=" + ResourceTypeMCP: true,
			}
			for _, filter := range labelFilters {
				delete(wantFilters, filter)
			}
			if len(wantFilters) != 0 {
				t.Fatalf("missing label filters: %v", wantFilters)
			}
			return []ContainerInfo{
				{
					ID:           "mcp-1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeMCP,
					Labels: map[string]string{
						LabelManagedBy:    ManagedByValue,
						LabelResourceType: ResourceTypeMCP,
						LabelRunID:        "run-1",
						LabelMCPServerID:  "server-1",
					},
				},
				{
					ID:           "agent-1",
					Status:       ContainerStatusRunning,
					RunID:        "run-1",
					ResourceType: ResourceTypeAgent,
					Labels:       Labels(ResourceTypeAgent, "run-1"),
				},
			}, nil
		},
	}

	infos, err := ReconcileMCPServers(context.Background(), mock)
	if err != nil {
		t.Fatalf("ReconcileMCPServers failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("len(infos) = %d, want 1", len(infos))
	}
	if infos[0] != (MCPContainerInfo{ContainerID: "mcp-1", ServerID: "server-1", RunID: "run-1"}) {
		t.Fatalf("infos[0] = %#v", infos[0])
	}
}

func TestIsUnrelatedContainer(t *testing.T) {
	tests := []struct {
		name      string
		info      ContainerInfo
		unrelated bool
	}{
		{
			name: "owned container",
			info: ContainerInfo{
				Labels: Labels(ResourceTypeAgent, "run-1"),
			},
			unrelated: false,
		},
		{
			name: "unrelated container without labels",
			info: ContainerInfo{
				Labels: map[string]string{},
			},
			unrelated: true,
		},
		{
			name: "unrelated container with non-agentpaas labels",
			info: ContainerInfo{
				Labels: map[string]string{"com.docker.compose.project": "myapp"},
			},
			unrelated: true,
		},
		{
			name: "nil labels",
			info: ContainerInfo{
				Labels: nil,
			},
			unrelated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnrelatedContainer(tt.info)
			if got != tt.unrelated {
				t.Errorf("IsUnrelatedContainer() = %v, want %v", got, tt.unrelated)
			}
		})
	}
}
