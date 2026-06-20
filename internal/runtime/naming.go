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
)

// ManagedByValue is the value of LabelManagedBy for all AgentPaaS-managed
// resources.
const ManagedByValue = "agentpaas"

// Resource type constants for LabelResourceType.
const (
	ResourceTypeAgent       = "agent"
	ResourceTypeGateway     = "gateway"
	ResourceTypeNetInternal = "net-internal"
	ResourceTypeNetEgress   = "net-egress"
)

// ContainerPrefixes map role types to their container name prefixes.
var ContainerPrefixes = map[string]string{
	"agent":   "agentpaas-agent-",
	"gateway": "agentpaas-gateway-",
}

// NetworkPrefixes map network role types to their network name prefixes.
var NetworkPrefixes = map[string]string{
	"internal": "agentpaas-net-internal-",
	"egress":   "agentpaas-net-egress-",
}

// ContainerName returns a deterministic container name for the given role
// and run ID. The format is "agentpaas-<role>-<id>", e.g.
// "agentpaas-agent-run-abc123" or "agentpaas-gateway-run-abc123".
func ContainerName(role, id string) string {
	if prefix, ok := ContainerPrefixes[role]; ok {
		return prefix + id
	}
	return fmt.Sprintf("agentpaas-%s-%s", role, id)
}

// NetworkName returns a deterministic Docker network name for the given role
// and run ID. The format is "agentpaas-net-<role>-<id>", e.g.
// "agentpaas-net-internal-run-abc123" or "agentpaas-net-egress-run-abc123".
func NetworkName(role, id string) string {
	if prefix, ok := NetworkPrefixes[role]; ok {
		return prefix + id
	}
	return fmt.Sprintf("agentpaas-net-%s-%s", role, id)
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
