// Package pipeline provides B34 pipeline and handoff conformance validation.
package pipeline

// Stable failure codes for pipeline and handoff validation.
// Tests pin these exact strings.
const (
	// Pipeline shape codes (owner: pipeline-validate).
	CodePipelineStageCount      = "PIPELINE_STAGE_COUNT"
	CodePipelineDuplicateNode   = "PIPELINE_DUPLICATE_NODE"
	CodePipelineUnsupportedShape = "PIPELINE_UNSUPPORTED_SHAPE"
	CodePipelineMutableRef      = "PIPELINE_MUTABLE_REF"
	CodePipelineSchemaMismatch  = "PIPELINE_SCHEMA_MISMATCH"
	CodePipelineUndeclaredMCP   = "PIPELINE_UNDECLARED_MCP"
	CodePipelineDeclassification = "PIPELINE_DECLASSIFICATION"

	// Handoff envelope codes (owner: handoff-validate).
	CodeHandoffSchemaVersion   = "HANDOFF_SCHEMA_VERSION"
	CodeHandoffContextOversize = "HANDOFF_CONTEXT_OVERSIZE"
	CodeHandoffArtifactMetaOversize = "HANDOFF_ARTIFACT_META_OVERSIZE"
	CodeHandoffReservedKey     = "HANDOFF_RESERVED_KEY"
	CodeHandoffDigestMismatch  = "HANDOFF_DIGEST_MISMATCH"
	CodeHandoffDeclassification = "HANDOFF_DECLASSIFICATION"
	CodeHandoffDuplicateID     = "HANDOFF_DUPLICATE_ID"
	CodeHandoffNonCanonical    = "HANDOFF_NON_CANONICAL"

	// Runtime code (owner: daemon).
	CodePipelineNotEnabled = "PIPELINE_NOT_ENABLED"
)
