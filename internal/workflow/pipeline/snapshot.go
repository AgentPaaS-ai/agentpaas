package pipeline

// SchemaVersionSnapshotV1 is the canonical schema version for compiled pipeline snapshots.
const SchemaVersionSnapshotV1 = "agentpaas.workflow.pipeline_snapshot/v1"

// CompiledPipelineSnapshot is the immutable execution plan for a pipeline workflow.
// It contains no generated IDs, timestamps, or runtime identifiers — it is a pure
// function of its inputs: the workflow YAML, policy, and resolved packages.
type CompiledPipelineSnapshot struct {
	SchemaVersion string `json:"schema_version"` // agentpaas.workflow.pipeline_snapshot/v1
	Kind          string `json:"kind"`           // pipeline

	Stages   []CompiledStage           `json:"stages"`
	Services []CompiledServiceBinding  `json:"services,omitempty"`
	Limits   CompiledLimits            `json:"limits"`

	// Digests of inputs that bound the snapshot.
	WorkflowYAMLDigest string `json:"workflow_yaml_digest"` // sha256 of raw workflow bytes
	PolicyDigest       string `json:"policy_digest,omitempty"`

	// NestedPackageDigests maps logical "name@version" → installed bundle digest.
	NestedPackageDigests map[string]string `json:"nested_package_digests"`

	// SnapshotDigest is sha256 over canonical JSON of this struct with SnapshotDigest empty.
	SnapshotDigest string `json:"snapshot_digest"`
}

// CompiledStage is a single pipeline stage in the compiled snapshot.
type CompiledStage struct {
	Order           int      `json:"order"`
	Name            string   `json:"name"`
	PackageName     string   `json:"package_name"`
	PackageVersion  string   `json:"package_version"`
	BundleDigest    string   `json:"bundle_digest"` // exact resolved
	HandoffClass    string   `json:"handoff"`
	OutputSchema    string   `json:"output_schema,omitempty"`
	AcceptedSchemas []string `json:"accepted_schemas,omitempty"`
	MCPServiceIDs   []string `json:"mcp_service_ids,omitempty"` // resolved allowlist refs
	PolicyDigest    string   `json:"policy_digest,omitempty"`   // per-stage if available
}

// CompiledServiceBinding binds a service ID to a resolved package.
type CompiledServiceBinding struct {
	ServiceID      string   `json:"service_id"`
	PackageName    string   `json:"package_name"`
	PackageVersion string   `json:"package_version"`
	BundleDigest   string   `json:"bundle_digest"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
}

// CompiledLimits captures pipeline-level resource/classification bounds.
type CompiledLimits struct {
	MaxActiveDuration    string `json:"max_active_duration,omitempty"`
	HandoffByteLimit     int    `json:"handoff_byte_limit,omitempty"`
	ArtifactLimit        int    `json:"artifact_limit,omitempty"`
	ActiveContainerLimit int    `json:"active_container_limit,omitempty"`
	AggregateMaxTokens   int    `json:"aggregate_max_tokens,omitempty"`
	AggregateMaxLLMSpend string `json:"aggregate_max_llm_spend,omitempty"`
}

// PackageResolver resolves a logical package name+version+declared digest to an
// installed signed bundle digest. Injectable for testing.
type PackageResolver interface {
	// Resolve returns the installed signed bundle digest for name+version.
	// Must fail if not installed, digest mismatch vs declared, or mutable tag.
	Resolve(packageName, packageVersion, declaredDigest string) (resolvedDigest string, err error)
}
