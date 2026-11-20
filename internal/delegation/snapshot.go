package delegation

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// WorkflowDelegationBinding
// ---------------------------------------------------------------------------

// WorkflowDelegationBinding represents a single delegation binding within a
// signed workflow snapshot. It pins the logical capability name, the exact
// callee package digest, optional operation, data classification ceiling,
// artifact audience, deadline, and budget.
type WorkflowDelegationBinding struct {
	BindingID            string   `json:"binding_id" yaml:"binding_id"`
	Operation            string   `json:"operation,omitempty" yaml:"operation,omitempty"`
	CalleePackageName    string   `json:"callee_package_name" yaml:"package_name"`
	CalleePackageVersion string   `json:"callee_package_version" yaml:"package_version"`
	CalleeBundleDigest   string   `json:"callee_bundle_digest" yaml:"bundle_digest"`
	CallerPackageName    string   `json:"caller_package_name,omitempty" yaml:"caller_package_name,omitempty"`
	MaxDataClass         string   `json:"max_data_class" yaml:"max_data_class"`
	ArtifactAudience     []string `json:"artifact_audience,omitempty" yaml:"artifact_audience,omitempty"`
	DeadlineMs           int64    `json:"deadline_ms,omitempty" yaml:"deadline_ms,omitempty"`
	MaxCostUSDDecimal    string   `json:"max_cost_usd_decimal,omitempty" yaml:"max_cost_usd_decimal,omitempty"`
}

// ---------------------------------------------------------------------------
// CommunicationSnapshot
// ---------------------------------------------------------------------------

// CommunicationSnapshot is the immutable, signed pin used at task admission.
// It binds a caller identity to a set of delegation bindings. The
// SnapshotDigest covers the caller identity, workflow, tenant, generation,
// and all bindings via canonical JSON.
type CommunicationSnapshot struct {
	SchemaVersion      string                       `json:"schema_version"`
	SnapshotGeneration int64                        `json:"snapshot_generation"`
	WorkflowID         string                       `json:"workflow_id"`
	TenantID           string                       `json:"tenant_id"`
	CallerDeploymentID string                       `json:"caller_deployment_id"`
	CallerPackageName  string                       `json:"caller_package_name"`
	CallerPackageDigest string                      `json:"caller_package_digest"`
	Bindings           []WorkflowDelegationBinding  `json:"bindings"`
	SnapshotDigest     string                       `json:"snapshot_digest,omitempty"`
}

// findBinding returns (binding, index, found).
func (s *CommunicationSnapshot) findBinding(bindingID string) (*WorkflowDelegationBinding, int, bool) {
	for i := range s.Bindings {
		if s.Bindings[i].BindingID == bindingID {
			return &s.Bindings[i], i, true
		}
	}
	return nil, -1, false
}

// ---------------------------------------------------------------------------
// Snapshot digest
// ---------------------------------------------------------------------------

// snapshotForDigest is the canonical form used for computing the snapshot digest.
// It excludes SnapshotDigest itself to avoid circularity.
type snapshotForDigest struct {
	SchemaVersion      string                       `json:"schema_version"`
	SnapshotGeneration int64                        `json:"snapshot_generation"`
	WorkflowID         string                       `json:"workflow_id"`
	TenantID           string                       `json:"tenant_id"`
	CallerDeploymentID string                       `json:"caller_deployment_id"`
	CallerPackageName  string                       `json:"caller_package_name"`
	CallerPackageDigest string                      `json:"caller_package_digest"`
	Bindings           []WorkflowDelegationBinding  `json:"bindings"`
}

// ComputeSnapshotDigest produces a deterministic SHA-256 hex digest over the
// canonical JSON of the snapshot (excluding the SnapshotDigest field itself).
// Bindings are sorted by BindingID for deterministic output.
func ComputeSnapshotDigest(s *CommunicationSnapshot) (string, error) {
	// Sort bindings by BindingID for determinism.
	bindings := make([]WorkflowDelegationBinding, len(s.Bindings))
	copy(bindings, s.Bindings)
	sort.SliceStable(bindings, func(i, j int) bool {
		return bindings[i].BindingID < bindings[j].BindingID
	})

	sfd := snapshotForDigest{
		SchemaVersion:       s.SchemaVersion,
		SnapshotGeneration:  s.SnapshotGeneration,
		WorkflowID:          s.WorkflowID,
		TenantID:            s.TenantID,
		CallerDeploymentID:  s.CallerDeploymentID,
		CallerPackageName:   s.CallerPackageName,
		CallerPackageDigest: s.CallerPackageDigest,
		Bindings:            bindings,
	}

	b, err := CanonicalJSON(sfd)
	if err != nil {
		return "", fmt.Errorf("snapshot digest: marshal: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", h), nil
}
