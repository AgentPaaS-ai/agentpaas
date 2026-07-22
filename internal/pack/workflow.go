package pack

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
	"github.com/AgentPaaS-ai/agentpaas/internal/money"
)

// WorkflowKind constants for workflow.yaml.
const (
	WorkflowKindStandalone  = "standalone"
	WorkflowKindPipeline    = "pipeline"
	WorkflowKindParentChild = "parent_child"
)

// Handoff classifications in least-to-most-restrictive order.
const (
	HandoffPublic        = "public"
	HandoffInternal      = "internal"
	HandoffConfidential  = "confidential"
	HandoffRestricted    = "restricted"
)

// validHandoffClassifications is the ordered set of allowed handoff classifications.
var validHandoffClassifications = []string{HandoffPublic, HandoffInternal, HandoffConfidential, HandoffRestricted}

// handoffClassificationRank returns the rank of a handoff classification string.
// Lower rank = less restrictive.
func handoffClassificationRank(c string) int {
	for i, v := range validHandoffClassifications {
		if v == c {
			return i
		}
	}
	return -1
}

// WorkflowYAML is the optional workflow.yaml envelope (v0.3).
type WorkflowYAML struct {
	Kind                  string             `yaml:"kind"` // standalone|pipeline|parent_child
	Pipeline              *PipelineConfig    `yaml:"pipeline,omitempty"`
	ParentChild           *ParentChildConfig `yaml:"parent_child,omitempty"`
	Services              []ServiceBinding   `yaml:"services,omitempty"`
	Delegations           []Delegation       `yaml:"delegations,omitempty"`
	MaxActiveDuration     string             `yaml:"max_active_duration,omitempty"`
	HandoffByteLimit      int                `yaml:"handoff_byte_limit,omitempty"`
	ArtifactLimit         int                `yaml:"artifact_limit,omitempty"`
	ActiveContainerLimit  int                `yaml:"active_container_limit,omitempty"`
	AggregateMaxTokens    int                `yaml:"aggregate_max_tokens,omitempty"`
	AggregateMaxLLMSpend  string             `yaml:"aggregate_max_llm_spend,omitempty"` // money.Decimal string
}

// PipelineConfig defines an ordered sequence of pipeline stages.
type PipelineConfig struct {
	Stages []PipelineStage `yaml:"stages"`
}

// PipelineStage defines a single stage in a pipeline workflow.
type PipelineStage struct {
	Name            string `yaml:"name"`
	PackageName     string `yaml:"package_name"`
	PackageVersion  string `yaml:"package_version"`
	BundleDigest    string `yaml:"bundle_digest"`
	Handoff         string `yaml:"handoff"` // public|internal|confidential|restricted
}

// ParentChildConfig defines a parent-child workflow topology.
type ParentChildConfig struct {
	ParentIdentity   ChildIdentity `yaml:"parent_identity"`
	ChildAllowlist   []ChildIdentity `yaml:"child_allowlist"`
	MaxFanout        int           `yaml:"max_fanout"`
	MaxConcurrency   int           `yaml:"max_concurrency"`
	LeafOnlyDepth    bool          `yaml:"leaf_only_depth"`
}

// ChildIdentity identifies an exact child agent by package name, version, and digest.
type ChildIdentity struct {
	PackageName    string `yaml:"package_name"`
	PackageVersion string `yaml:"package_version"`
	BundleDigest   string `yaml:"bundle_digest"`
}

// ServiceBinding binds a logical service ID to a service package.
type ServiceBinding struct {
	ServiceID       string   `yaml:"service_id"`
	PackageName     string   `yaml:"package_name"`
	PackageVersion  string   `yaml:"package_version"`
	BundleDigest    string   `yaml:"bundle_digest"`
	AllowedTools    []string `yaml:"allowed_tools,omitempty"`
}

// Delegation defines a delegation binding in a workflow.yaml (v0.3).
// Maps to delegation.WorkflowDelegationBinding at snapshot graph time.
type Delegation struct {
	BindingID           string   `yaml:"binding_id"`
	Operation           string   `yaml:"operation,omitempty"`
	PackageName         string   `yaml:"package_name"`
	PackageVersion      string   `yaml:"package_version"`
	BundleDigest        string   `yaml:"bundle_digest"`
	CallerPackageName   string   `yaml:"caller_package_name,omitempty"`
	MaxDataClass        string   `yaml:"max_data_class"`
	ArtifactAudience    []string `yaml:"artifact_audience,omitempty"`
	DeadlineMs          int64    `yaml:"deadline_ms,omitempty"`
	MaxCostUSDDecimal   string   `yaml:"max_cost_usd_decimal,omitempty"`
}

// ValidateWorkflowYAML validates a workflow.yaml configuration.
func ValidateWorkflowYAML(wf *WorkflowYAML) []string {
	var errs []string

	if wf == nil {
		return nil
	}

	// kind is required and must be one of the valid values.
	switch wf.Kind {
	case WorkflowKindStandalone, WorkflowKindPipeline, WorkflowKindParentChild:
		// valid
	case "":
		errs = append(errs, "kind is required")
	default:
		errs = append(errs, fmt.Sprintf("unknown kind %q; must be %q, %q, or %q",
			wf.Kind, WorkflowKindStandalone, WorkflowKindPipeline, WorkflowKindParentChild))
	}

	// Pipeline and parent_child are mutually exclusive.
	if wf.Pipeline != nil && wf.ParentChild != nil {
		errs = append(errs, "pipeline and parent_child are mutually exclusive")
	}

	// Standalone means no pipeline/parent_child sections (only services allowed).
	if wf.Kind == WorkflowKindStandalone {
		if wf.Pipeline != nil {
			errs = append(errs, "standalone workflow must not have pipeline section")
		}
		if wf.ParentChild != nil {
			errs = append(errs, "standalone workflow must not have parent_child section")
		}
	}

	// kind: pipeline with parent_child section
	if wf.Kind == WorkflowKindPipeline && wf.ParentChild != nil {
		errs = append(errs, "pipeline workflow must not have parent_child section")
	}

	// kind: parent_child with pipeline section
	if wf.Kind == WorkflowKindParentChild && wf.Pipeline != nil {
		errs = append(errs, "parent_child workflow must not have pipeline section")
	}

	// Pipeline validation
	if wf.Pipeline != nil {
		errs = append(errs, validatePipelineStages(wf.Pipeline)...)
	}

	// ParentChild validation
	if wf.ParentChild != nil {
		errs = append(errs, validateParentChild(wf.ParentChild)...)
	}

	// Service validation
	errs = append(errs, validateServices(wf.Services)...)

	// Delegation validation
	errs = append(errs, validateDelegations(wf.Delegations)...)

	// MCP service kind check
	// For mcp_service kind: require a service declaration.
	// This is checked as part of LoadAgentYAML — here we validate
	// that service bindings reference valid package info.

	// Validate aggregate_max_llm_spend
	if wf.AggregateMaxLLMSpend != "" {
		if _, err := money.Parse(wf.AggregateMaxLLMSpend); err != nil {
			errs = append(errs, fmt.Sprintf("aggregate_max_llm_spend: %v", err))
		}
	}

	return errs
}

func validatePipelineStages(p *PipelineConfig) []string {
	var errs []string

	if len(p.Stages) == 0 {
		errs = append(errs, "pipeline requires at least one stage")
		return errs
	}

	if len(p.Stages) > 16 {
		errs = append(errs, "pipeline has more than 16 stages (v0.3 bound)")
	}

	stageNames := make(map[string]bool)
	prevRank := -1

	for i, stage := range p.Stages {
		prefix := fmt.Sprintf("pipeline.stages[%d]", i)

		if stage.Name == "" {
			errs = append(errs, prefix+": stage name is required")
		} else {
			if stageNames[stage.Name] {
				errs = append(errs, fmt.Sprintf("%s: duplicate stage name %q", prefix, stage.Name))
			}
			stageNames[stage.Name] = true
		}

		if stage.PackageName == "" {
			errs = append(errs, fmt.Sprintf("%s: package_name is required", prefix))
		}
		if stage.PackageVersion == "" {
			errs = append(errs, fmt.Sprintf("%s: package_version is required", prefix))
		}
		if stage.BundleDigest == "" {
			errs = append(errs, fmt.Sprintf("%s: bundle_digest is required", prefix))
		}

		// Handoff classification validation
		rank := handoffClassificationRank(stage.Handoff)
		if rank == -1 {
			errs = append(errs, fmt.Sprintf("%s: invalid handoff %q; must be one of %v",
				prefix, stage.Handoff, validHandoffClassifications))
		} else {
			if prevRank != -1 && rank < prevRank {
				errs = append(errs, fmt.Sprintf("%s: handoff declassification from %q to %q not allowed",
					prefix, p.Stages[i-1].Handoff, stage.Handoff))
			}
			prevRank = rank
		}

		// Reject cycles by checking no stage references its own name
		// (simple cycle detection: stages are ordered and must be unique)
		if strings.TrimSpace(stage.Name) != "" && strings.Contains(stage.Name, "->") {
			errs = append(errs, fmt.Sprintf("%s: stage name contains cycle marker", prefix))
		}
	}

	return errs
}

func validateParentChild(pc *ParentChildConfig) []string {
	var errs []string

	// Parent identity fields
	if pc.ParentIdentity.PackageName == "" {
		errs = append(errs, "parent_child.parent_identity.package_name is required")
	}
	if pc.ParentIdentity.PackageVersion == "" {
		errs = append(errs, "parent_child.parent_identity.package_version is required")
	}
	if pc.ParentIdentity.BundleDigest == "" {
		errs = append(errs, "parent_child.parent_identity.bundle_digest is required")
	}

	// Child allowlist
	if len(pc.ChildAllowlist) == 0 {
		errs = append(errs, "parent_child.child_allowlist must have at least one entry")
	}

	childIDs := make(map[string]bool)
	for i, child := range pc.ChildAllowlist {
		prefix := fmt.Sprintf("parent_child.child_allowlist[%d]", i)
		if child.PackageName == "" {
			errs = append(errs, prefix+".package_name is required")
		}
		if child.PackageVersion == "" {
			errs = append(errs, prefix+".package_version is required")
		}
		if child.BundleDigest == "" {
			errs = append(errs, prefix+".bundle_digest is required")
		}
		// Check for duplicate identities
		id := child.PackageName + "@" + child.PackageVersion + ":" + child.BundleDigest
		if childIDs[id] {
			errs = append(errs, fmt.Sprintf("%s: duplicate child identity %q", prefix, id))
		}
		childIDs[id] = true
	}

	// MaxFanout validation (1-64)
	if pc.MaxFanout < 1 || pc.MaxFanout > 64 {
		errs = append(errs, "parent_child.max_fanout must be between 1 and 64")
	}

	// MaxConcurrency validation (1-64)
	if pc.MaxConcurrency < 1 || pc.MaxConcurrency > 64 {
		errs = append(errs, "parent_child.max_concurrency must be between 1 and 64")
	}

	// LeafOnlyDepth must be true (v0.3 children are leaf-only)
	if !pc.LeafOnlyDepth {
		errs = append(errs, "parent_child.leaf_only_depth must be true (v0.3 children are leaf-only)")
	}

	return errs
}

func validateServices(services []ServiceBinding) []string {
	var errs []string
	if len(services) == 0 {
		return nil
	}

	svcIDs := make(map[string]bool)
	for i, s := range services {
		prefix := fmt.Sprintf("services[%d]", i)
		if s.ServiceID == "" {
			errs = append(errs, prefix+".service_id is required")
		} else {
			if svcIDs[s.ServiceID] {
				errs = append(errs, fmt.Sprintf("%s: duplicate service_id %q", prefix, s.ServiceID))
			}
			svcIDs[s.ServiceID] = true
		}
		if s.PackageName == "" {
			errs = append(errs, prefix+".package_name is required")
		}
		if s.PackageVersion == "" {
			errs = append(errs, prefix+".package_version is required")
		}
		if s.BundleDigest == "" {
			errs = append(errs, prefix+".bundle_digest is required")
		}
	}

	// Sort services by service_id for deterministic output
	sort.Slice(services, func(i, j int) bool {
		return services[i].ServiceID < services[j].ServiceID
	})

	return errs
}

// ValidateWorkflowServiceDeclaration checks that an mcp_service kind has a
// matching service declaration in the workflow.yaml.
func ValidateWorkflowServiceDeclaration(wf *WorkflowYAML, kind string) []string {
	var errs []string
	if kind == "mcp_service" && wf != nil {
		if len(wf.Services) == 0 {
			errs = append(errs, "mcp_service kind requires at least one service declaration in workflow.yaml")
		}
	}
	return errs
}

// validateDelegations validates the delegations section of a workflow.yaml.
func validateDelegations(delegations []Delegation) []string {
	var errs []string
	if len(delegations) == 0 {
		return nil
	}

	bindingIDs := make(map[string]bool)
	for i, d := range delegations {
		prefix := fmt.Sprintf("delegations[%d]", i)

		if d.BindingID == "" {
			errs = append(errs, prefix+".binding_id is required")
		} else {
			if bindingIDs[d.BindingID] {
				errs = append(errs, fmt.Sprintf("%s: duplicate binding_id %q", prefix, d.BindingID))
			}
			bindingIDs[d.BindingID] = true
		}

		if d.PackageName == "" {
			errs = append(errs, prefix+".package_name is required")
		}
		if d.PackageVersion == "" {
			errs = append(errs, prefix+".package_version is required")
		}
		if d.BundleDigest == "" {
			errs = append(errs, prefix+".bundle_digest is required")
		}

		// MaxDataClass must be a valid classification.
		if d.MaxDataClass == "" {
			errs = append(errs, prefix+".max_data_class is required")
		} else if handoffClassificationRank(d.MaxDataClass) == -1 {
			errs = append(errs, fmt.Sprintf("%s: invalid max_data_class %q; must be one of %v",
				prefix, d.MaxDataClass, validHandoffClassifications))
		}

		if d.MaxCostUSDDecimal != "" {
			if _, err := money.Parse(d.MaxCostUSDDecimal); err != nil {
				errs = append(errs, fmt.Sprintf("%s.max_cost_usd_decimal: %v", prefix, err))
			}
		}
	}

	return errs
}

// BuildCommunicationSnapshot constructs a delegation.CommunicationSnapshot
// from a WorkflowYAML + caller identity.
func BuildCommunicationSnapshot(
	wf *WorkflowYAML,
	workflowID string,
	tenantID string,
	callerDeploymentID string,
	callerPackageName string,
	callerPackageDigest string,
	snapshotGeneration int64,
) (*delegation.CommunicationSnapshot, error) {
	bindings := make([]delegation.WorkflowDelegationBinding, 0, len(wf.Delegations))
	for _, d := range wf.Delegations {
		bindings = append(bindings, delegation.WorkflowDelegationBinding{
			BindingID:            d.BindingID,
			Operation:            d.Operation,
			CalleePackageName:    d.PackageName,
			CalleePackageVersion: d.PackageVersion,
			CalleeBundleDigest:   d.BundleDigest,
			CallerPackageName:    d.CallerPackageName,
			MaxDataClass:         d.MaxDataClass,
			ArtifactAudience:     d.ArtifactAudience,
			DeadlineMs:           d.DeadlineMs,
			MaxCostUSDDecimal:    d.MaxCostUSDDecimal,
		})
	}

	snap := &delegation.CommunicationSnapshot{
		SchemaVersion:       delegation.CurrentSchemaVersion,
		SnapshotGeneration:  snapshotGeneration,
		WorkflowID:          workflowID,
		TenantID:            tenantID,
		CallerDeploymentID:  callerDeploymentID,
		CallerPackageName:   callerPackageName,
		CallerPackageDigest: callerPackageDigest,
		Bindings:            bindings,
	}

	dg, err := delegation.ComputeSnapshotDigest(snap)
	if err != nil {
		return nil, fmt.Errorf("build communication snapshot: %w", err)
	}
	snap.SnapshotDigest = dg

	return snap, nil
}