package mcpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// MCPSidecarInfo contains Docker artifact metadata for an MCP sidecar.
type MCPSidecarInfo struct {
	ServerID     string            `json:"server_id"`
	ContainerID  string            `json:"container_id,omitempty"`
	ImageDigest  string            `json:"image_digest,omitempty"`
	Labels       map[string]string `json:"labels"`
	Networks     []string          `json:"networks"`
	Health       string            `json:"health"`
	Readiness    string            `json:"readiness"`
	RestartCount int               `json:"restart_count"`
	MemoryBytes  int64             `json:"memory_bytes,omitempty"`
	CPUPercent   float64           `json:"cpu_percent,omitempty"`
	LastError    string            `json:"last_error,omitempty"`
}

// StatusReport aggregates MCP resource status for operator visibility.
// This is the data model included in `agent status --json` and dashboard.
type StatusReport struct {
	Resources []Resource       `json:"resources"`
	Sidecars  []MCPSidecarInfo `json:"sidecars,omitempty"`
	Generated time.Time        `json:"generated_at"`
}

type containerArtifactMetadata struct {
	ImageDigest  string
	RestartCount int
}

type containerArtifactInspector interface {
	InspectContainerArtifact(context.Context, runtime.ContainerID) (containerArtifactMetadata, error)
}

// GenerateStatusReport produces a StatusReport from the Manager and
// optionally from Docker runtime data (if driver is provided).
// If driver is nil, sidecar info is omitted (stdio-only deployments).
func GenerateStatusReport(ctx context.Context, manager *Manager, lifecycle *Lifecycle, driver runtime.RuntimeDriver) (*StatusReport, error) {
	report := &StatusReport{Generated: time.Now().UTC()}
	if manager == nil {
		return report, nil
	}

	resources := manager.Status()
	for i := range resources {
		applyLifecycleState(&resources[i], lifecycle)
	}
	report.Resources = resources

	if driver == nil {
		return report, nil
	}

	sidecars, err := collectSidecars(ctx, resources, driver)
	if err != nil {
		return nil, err
	}
	if len(sidecars) > 0 {
		report.Sidecars = sidecars
	}
	return report, nil
}

// MCPStatusJSON returns an encoded MCP status payload suitable for CLI status output.
func MCPStatusJSON(ctx context.Context, manager *Manager, lifecycle *Lifecycle, driver runtime.RuntimeDriver) ([]byte, error) {
	report, err := GenerateStatusReport(ctx, manager, lifecycle, driver)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func applyLifecycleState(resource *Resource, lifecycle *Lifecycle) {
	if lifecycle == nil {
		return
	}
	if lifecycle.IsRunning(resource.ServerID) {
		resource.Readiness = ReadinessReady
		resource.Health = HealthHealthy
		resource.LastError = ""
		return
	}
	if crash := lifecycle.CrashContext(resource.ServerID); crash != nil {
		resource.Readiness = ReadinessUnhealthy
		resource.Health = HealthFailed
		resource.LastError = crash.Error
	}
}

func collectSidecars(ctx context.Context, resources []Resource, driver runtime.RuntimeDriver) ([]MCPSidecarInfo, error) {
	sidecars := make([]MCPSidecarInfo, 0)
	for _, resource := range resources {
		if resource.Transport != "http" {
			continue
		}
		containers, err := driver.ListContainers(ctx,
			runtime.LabelMCPServerID+"="+resource.ServerID,
			runtime.LabelManagedBy+"="+runtime.ManagedByValue,
			runtime.LabelResourceType+"="+runtime.ResourceTypeMCP,
		)
		if err != nil {
			return nil, fmt.Errorf("list mcp sidecars for %q: %w", resource.ServerID, err)
		}
		for _, container := range containers {
			sidecar := buildSidecarInfo(ctx, resource, container, driver)
			sidecars = append(sidecars, sidecar)
		}
	}
	sort.Slice(sidecars, func(i, j int) bool {
		if sidecars[i].ServerID == sidecars[j].ServerID {
			return sidecars[i].ContainerID < sidecars[j].ContainerID
		}
		return sidecars[i].ServerID < sidecars[j].ServerID
	})
	return sidecars, nil
}

func buildSidecarInfo(ctx context.Context, resource Resource, container runtime.ContainerInfo, driver runtime.RuntimeDriver) MCPSidecarInfo {
	id := runtime.ContainerID(container.ID)
	sidecar := MCPSidecarInfo{
		ServerID:    resource.ServerID,
		ContainerID: container.ID,
		Labels:      agentpaasLabels(container.Labels),
		Networks:    []string{},
		Health:      HealthUnknown,
		Readiness:   ReadinessStopped,
	}

	status, err := driver.Status(ctx, id)
	if err != nil {
		sidecar.Health = HealthFailed
		sidecar.Readiness = ReadinessUnhealthy
		sidecar.LastError = err.Error()
	} else {
		sidecar.Readiness, sidecar.Health = stateFromContainerStatus(status)
	}

	if stats, err := driver.Stats(ctx, id); err == nil {
		sidecar.MemoryBytes = int64(stats.MemoryMB * 1024 * 1024)
		sidecar.CPUPercent = stats.CPUPercent
	} else if sidecar.LastError == "" {
		sidecar.LastError = err.Error()
	}

	if networks, err := driver.InspectContainerNetworks(ctx, id); err == nil {
		sidecar.Networks = networkNames(networks)
	} else if sidecar.LastError == "" {
		sidecar.LastError = err.Error()
	}

	if inspector, ok := driver.(containerArtifactInspector); ok {
		if metadata, err := inspector.InspectContainerArtifact(ctx, id); err == nil {
			sidecar.ImageDigest = metadata.ImageDigest
			sidecar.RestartCount = metadata.RestartCount
		} else if sidecar.LastError == "" {
			sidecar.LastError = err.Error()
		}
	}

	return sidecar
}

func stateFromContainerStatus(status runtime.ContainerStatus) (string, string) {
	switch status {
	case runtime.ContainerStatusRunning:
		return ReadinessReady, HealthHealthy
	case runtime.ContainerStatusPaused:
		return ReadinessStarting, HealthUnknown
	case runtime.ContainerStatusStopped, runtime.ContainerStatusRemoved:
		return ReadinessStopped, HealthFailed
	default:
		return ReadinessUnhealthy, HealthUnknown
	}
}

func agentpaasLabels(labels map[string]string) map[string]string {
	allowed := map[string]struct{}{
		runtime.LabelManagedBy:    {},
		runtime.LabelResourceType: {},
		runtime.LabelRunID:        {},
		runtime.LabelMCPServerID:  {},
	}
	result := make(map[string]string, len(allowed))
	for key, value := range labels {
		if _, ok := allowed[key]; ok {
			result[key] = sanitizeLabelValue(value)
		}
	}
	return result
}

func sanitizeLabelValue(v string) string {
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, v)

	const maxLabelValueLen = 128
	if len(v) > maxLabelValueLen {
		v = v[:maxLabelValueLen] + "..."
	}
	return v
}

func networkNames(networks []runtime.ContainerNetworkInfo) []string {
	names := make([]string, 0, len(networks))
	for _, network := range networks {
		if network.Name != "" {
			names = append(names, network.Name)
			continue
		}
		names = append(names, network.ID)
	}
	sort.Strings(names)
	return names
}
