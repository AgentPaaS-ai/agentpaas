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

	// LabelAgentRef identifies the installed agent ref on container labels.
	LabelAgentRef = "agentpaas.agent-ref"

	// LabelMCPServerID identifies which MCP server a container represents.
	LabelMCPServerID = "agentpaas.mcp-server-id"

	// LabelWorkflowID is the AgentPaaS workflow that owns this service instance.
	LabelWorkflowID = "agentpaas.workflow_id"

	// LabelServiceID is the logical service binding ID within the workflow.
	LabelServiceID = "agentpaas.service_id"

	// LabelServiceGeneration is the monotonic generation counter for CAS checks.
	LabelServiceGeneration = "agentpaas.service_generation"

	// LabelServiceRunID identifies the service run that owns this container.
	LabelServiceRunID = "agentpaas.service_run_id"

	// Pipeline-specific labels for multi-stage container execution with
	// independent authority per stage.

	// LabelNodeID identifies the pipeline node that owns this container.
	LabelNodeID = "agentpaas.node_id"

	// LabelAttemptID identifies the pipeline attempt that owns this container.
	LabelAttemptID = "agentpaas.attempt_id"

	// LabelPackageDigest is the content digest of the package running in
	// this container.
	LabelPackageDigest = "agentpaas.package_digest"

	// LabelPolicyDigest is the content digest of the policy applied to this
	// container.
	LabelPolicyDigest = "agentpaas.policy_digest"

	// LabelLeaseGeneration is the monotonic lease generation counter for
	// CAS fencing.
	LabelLeaseGeneration = "agentpaas.lease_generation"

	// LabelPipelineStage marks a container as a pipeline stage (value "true").
	LabelPipelineStage = "agentpaas.pipeline_stage"

	// LabelStageOrder is the 0-based ordinal position of this stage in the
	// pipeline DAG.
	LabelStageOrder = "agentpaas.stage_order"
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
	// ResourceTypeMCPServiceNet is the label value for workflow-scoped MCP
	// service networks that carry no external route and are only attached
	// to trusted gateway/service containers.
	ResourceTypeMCPServiceNet = "mcp-service-net"
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

// LabelsWithAgentRef returns ownership labels plus optional installed agent ref.
func LabelsWithAgentRef(resourceType, runID, agentRef string) map[string]string {
	labels := Labels(resourceType, runID)
	if agentRef != "" {
		labels[LabelAgentRef] = agentRef
	}
	return labels
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

// ---------------------------------------------------------------------------
// Pipeline stage labels
// ---------------------------------------------------------------------------

// sanitizeLabelValue returns an error if val contains a newline or NUL byte.
func sanitizeLabelValue(val string) error {
	for i := 0; i < len(val); i++ {
		if val[i] == 0 || val[i] == '\n' || val[i] == '\r' {
			return fmt.Errorf("label value contains forbidden character at offset %d", i)
		}
	}
	return nil
}

// PipelineStageLabels returns the full set of labels for a pipeline stage
// container. Includes all ownership labels (ManagedBy, ResourceType=agent,
// RunID) plus pipeline-specific fields.
//
// All values are sanitized; an error is returned if any value contains
// newline or NUL characters. This function never puts credential values,
// tokens, or secret env values into labels.
func PipelineStageLabels(workflowID, nodeID, runID, attemptID, packageDigest, policyDigest string, leaseGen int64, stageOrder int) (map[string]string, error) {
	fields := map[string]string{
		LabelWorkflowID:     workflowID,
		LabelNodeID:         nodeID,
		LabelRunID:          runID,
		LabelAttemptID:      attemptID,
		LabelPackageDigest:  packageDigest,
		LabelPolicyDigest:   policyDigest,
		LabelLeaseGeneration: fmt.Sprintf("%d", leaseGen),
		LabelPipelineStage:  "true",
		LabelStageOrder:     fmt.Sprintf("%d", stageOrder),
	}

	for _, v := range fields {
		if err := sanitizeLabelValue(v); err != nil {
			return nil, fmt.Errorf("PipelineStageLabels: %w", err)
		}
	}

	// Merge ownership labels (LabelManagedBy, LabelResourceType=agent, LabelRunID).
	labels := Labels(ResourceTypeAgent, runID)
	// Merge workflow_id (already set in Labels as run-id, but we need both).
	for k, v := range fields {
		labels[k] = v
	}

	return labels, nil
}
