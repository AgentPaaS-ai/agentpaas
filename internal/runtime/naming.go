// Package runtime provides the RuntimeDriver interface and implementations
// for managing containerized workloads on behalf of AgentPaaS.
package runtime

import (
	"fmt"
	"strings"
)

// Label keys for AgentPaaS-owned Docker resources. These labels enable
// reconciliation to discover only AgentPaaS-owned containers and networks.
const (
	// LabelManagedBy identifies resources managed by AgentPaaS.
	LabelManagedBy = "agentpaas.managed-by"

	// LabelResourceType identifies the type of AgentPaaS resource
	// (agent, gateway, net-internal, net-egress).
	LabelResourceType = "agentpaas.resource-type"

	// LabelRunID identifies the agent run that owns this resource.
	LabelRunID = "agentpaas.run-id"

	// LabelMCPServerID identifies which MCP server a container represents.
	LabelMCPServerID = "agentpaas.mcp-server-id"
)

// ManagedByValue is the value of LabelManagedBy for all AgentPaaS-managed
// resources.
const ManagedByValue = "agentpaas"

// Resource type constants for LabelResourceType.
const (
	ResourceTypeAgent       = "agent"
	ResourceTypeGateway     = "gateway"
	ResourceTypeMCP         = "mcp"
	ResourceTypeNetInternal = "net-internal"
	ResourceTypeNetEgress   = "net-egress"
)

// ContainerPrefixes map role types to their container name prefixes.
var ContainerPrefixes = map[string]string{
	"agent":   "agentpaas-agent-",
	"gateway": "agentpaas-gateway-",
	"mcp":     "agentpaas-mcp-",
}

// NetworkPrefixes map network role types to their network name prefixes.
var NetworkPrefixes = map[string]string{
	"internal": "agentpaas-net-internal-",
	"egress":   "agentpaas-net-egress-",
}

// ContainerName returns a deterministic container name for the given role
// and run ID. For known roles (agent, gateway) the format is
// "agentpaas-<role>-<id>", e.g. "agentpaas-agent-run-abc123".
// For unknown roles (which may contain hyphens), underscore separates
// role from ID to prevent ambiguity: "agentpaas-<role>_<id>".
func ContainerName(role, id string) string {
	if prefix, ok := ContainerPrefixes[role]; ok {
		return prefix + id
	}
	// Use underscore delimiter to avoid ambiguity when role contains hyphens:
	// ContainerName("agent", "foo-bar") == "agentpaas-agent-foo-bar"
	// ContainerName("agent-foo", "bar") == "agentpaas-agent-foo_bar"  (not a collision)
	return fmt.Sprintf("agentpaas-%s_%s", role, id)
}

// NetworkName returns a deterministic Docker network name for the given role
// and run ID. For known roles (internal, egress) the format is
// "agentpaas-net-<role>-<id>", e.g. "agentpaas-net-internal-run-abc123".
// For unknown roles, underscore separates role from ID:
// "agentpaas-net-<role>_<id>".
func NetworkName(role, id string) string {
	if prefix, ok := NetworkPrefixes[role]; ok {
		return prefix + id
	}
	return fmt.Sprintf("agentpaas-net-%s_%s", role, id)
}

// Labels returns a deterministic set of AgentPaaS ownership labels for a
// resource of the given type and run ID. The returned map includes:
//   - agentpaas.managed-by → "agentpaas"
//   - agentpaas.resource-type → <resourceType>
//   - agentpaas.run-id → <runID>
func Labels(resourceType, runID string) map[string]string {
	return map[string]string{
		LabelManagedBy:    ManagedByValue,
		LabelResourceType: resourceType,
		LabelRunID:        runID,
	}
}

// IsOwned returns true if the given Docker labels indicate the resource is
// owned by AgentPaaS. A resource is considered owned if it has a label
// "agentpaas.managed-by" with value "agentpaas".
func IsOwned(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	return strings.EqualFold(labels[LabelManagedBy], ManagedByValue)
}

// RunIDFromLabels extracts the run ID from AgentPaaS resource labels.
// Returns empty string if the label is not present.
func RunIDFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return labels[LabelRunID]
}

// ResourceTypeFromLabels extracts the resource type from AgentPaaS labels.
// Returns empty string if the label is not present.
func ResourceTypeFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return labels[LabelResourceType]
}
