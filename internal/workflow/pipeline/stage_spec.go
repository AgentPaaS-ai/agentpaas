package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ---------------------------------------------------------------------------
// Stage launch request
// ---------------------------------------------------------------------------

// StageLaunchRequest carries all the metadata needed to build a container
// spec for a single pipeline stage with independent authority isolation.
type StageLaunchRequest struct {
	WorkflowID string
	NodeID     string
	RunID      string
	AttemptID  string

	StageOrder      int
	PackageDigest   string
	PolicyDigest    string
	Image           string
	LeaseGeneration int64

	// NetworkID is the stage-private internal network (required, non-empty).
	NetworkID string

	// ReadOnlyArtifactBinds are optional ":ro" bind mounts.
	ReadOnlyArtifactBinds []string

	// WritableWorkDirBind is an optional single RW bind unique per stage.
	// Must contain nodeID or runID in the path to avoid collisions.
	WritableWorkDirBind string

	// Env is the environment for the container. Must not include secret-looking
	// values (the builder strips secret-like env from labels, not from Env).
	Env []string

	CPUQuota        int64
	MaxPIDs         int64
	MemoryLimitBytes int64
}

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------

var (
	errEmptyImage     = errors.New("BuildStageContainerSpec: image is required")
	errEmptyNetworkID = errors.New("BuildStageContainerSpec: network ID is required")
	errEmptyRunID     = errors.New("BuildStageContainerSpec: run ID is required")
	errEmptyNodeID    = errors.New("BuildStageContainerSpec: node ID is required")
	errEmptyWorkflowID = errors.New("BuildStageContainerSpec: workflow ID is required")
)

// ---------------------------------------------------------------------------
// BuildStageContainerSpec
// ---------------------------------------------------------------------------

// BuildStageContainerSpec constructs a runtime.ContainerSpec from a
// StageLaunchRequest. The resulting spec includes:
//   - Full pipeline stage labels via PipelineStageLabels
//   - Exactly one NetworkID
//   - RO artifact binds (if any) + optional unique RW workdir
//   - No CapAdd (empty, no NET_ADMIN unless explicitly justified)
//
// Validation rules (stable errors):
//   - Empty Image, NetworkID, RunID, NodeID, WorkflowID
//   - Any RW bind that collides with a common pattern (must contain nodeID or runID)
//   - Label sanitization failure (newlines/NUL)
func BuildStageContainerSpec(req StageLaunchRequest) (runtime.ContainerSpec, error) {
	// Validate required fields.
	if req.Image == "" {
		return runtime.ContainerSpec{}, errEmptyImage
	}
	if req.NetworkID == "" {
		return runtime.ContainerSpec{}, errEmptyNetworkID
	}
	if req.RunID == "" {
		return runtime.ContainerSpec{}, errEmptyRunID
	}
	if req.NodeID == "" {
		return runtime.ContainerSpec{}, errEmptyNodeID
	}
	if req.WorkflowID == "" {
		return runtime.ContainerSpec{}, errEmptyWorkflowID
	}

	// Validate RW bind uniqueness: must contain nodeID or runID in path.
	if req.WritableWorkDirBind != "" {
		if !strings.Contains(req.WritableWorkDirBind, req.NodeID) &&
			!strings.Contains(req.WritableWorkDirBind, req.RunID) {
			return runtime.ContainerSpec{}, fmt.Errorf(
				"BuildStageContainerSpec: writable work dir bind must contain nodeID or runID for isolation, got %q",
				req.WritableWorkDirBind,
			)
		}
		// Must not be read-only.
		if strings.HasSuffix(req.WritableWorkDirBind, ":ro") {
			return runtime.ContainerSpec{}, fmt.Errorf(
				"BuildStageContainerSpec: writable work dir bind must not end with :ro, got %q",
				req.WritableWorkDirBind,
			)
		}
	}

	// Validate RO binds must end with ":ro".
	for i, bind := range req.ReadOnlyArtifactBinds {
		if !strings.HasSuffix(bind, ":ro") {
			return runtime.ContainerSpec{}, fmt.Errorf(
				"BuildStageContainerSpec: read-only artifact bind must end with :ro at index %d, got %q",
				i, bind,
			)
		}
	}

	// Build labels.
	labels, err := runtime.PipelineStageLabels(
		req.WorkflowID,
		req.NodeID,
		req.RunID,
		req.AttemptID,
		req.PackageDigest,
		req.PolicyDigest,
		req.LeaseGeneration,
		req.StageOrder,
	)
	if err != nil {
		return runtime.ContainerSpec{}, fmt.Errorf("BuildStageContainerSpec: %w", err)
	}

	// Build binds: RO artifacts first, then optional RW workdir.
	binds := make([]string, 0, len(req.ReadOnlyArtifactBinds)+1)
	binds = append(binds, req.ReadOnlyArtifactBinds...)
	if req.WritableWorkDirBind != "" {
		binds = append(binds, req.WritableWorkDirBind)
	}

	return runtime.ContainerSpec{
		Image:            req.Image,
		Env:              req.Env,
		Labels:           labels,
		NetworkIDs:       []string{req.NetworkID},
		Binds:            binds,
		MemoryLimitBytes: req.MemoryLimitBytes,
		NanoCPUs:         req.CPUQuota,
		MaxPIDs:          req.MaxPIDs,
		CapAdd:           nil, // empty: no NET_ADMIN unless explicitly justified
	}, nil
}
