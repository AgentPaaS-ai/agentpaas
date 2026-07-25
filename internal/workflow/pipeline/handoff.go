package pipeline

import "encoding/json"

// SchemaVersionHandoffV1 is the B34 canonical handoff schema version.
const SchemaVersionHandoffV1 = "agentpaas.workflow.handoff/v1"

// HandoffContextMaxBytes is the maximum size for context.value canonical JSON.
const HandoffContextMaxBytes = 256 * 1024 // 256 KiB

// HandoffArtifactMetaMaxBytes is the maximum aggregate size for artifact reference metadata.
const HandoffArtifactMetaMaxBytes = 64 * 1024 // 64 KiB

// HandoffEnvelope is the B34 canonical handoff envelope between pipeline stages.
// This is a NEW type separate from routedrun.HandoffEnvelope (B26 skeleton).
type HandoffEnvelope struct {
	SchemaVersion string `json:"schema_version"`

	WorkflowID        string `json:"workflow_id"`
	HandoffID         string `json:"handoff_id"`
	FromNodeID        string `json:"from_node_id"`
	ToNodeID          string `json:"to_node_id"`
	ProducerRunID     string `json:"producer_run_id"`
	ProducerAttemptID string `json:"producer_attempt_id"`
	ProducerResultDigest string `json:"producer_result_digest"`

	Sequence  int    `json:"sequence"`
	CreatedAt string `json:"created_at"`

	Classification string          `json:"classification"`
	Context        HandoffContext  `json:"context"`
	Artifacts      []HandoffArtifact `json:"artifacts,omitempty"`
}

// HandoffContext wraps the structured context passed between stages.
type HandoffContext struct {
	Schema string          `json:"schema"`
	Value  json.RawMessage `json:"value"`
}

// HandoffArtifact is a reference to an artifact passed in a handoff.
type HandoffArtifact struct {
	ArtifactID     string `json:"artifact_id"`
	OwnerNodeID    string `json:"owner_node_id"`
	OwnerRunID     string `json:"owner_run_id"`
	ImmutableRef   string `json:"immutable_ref"`
	Digest         string `json:"digest"`
	MediaType      string `json:"media_type"`
	SizeBytes      int64  `json:"size_bytes"`
	Classification string `json:"classification"`
}

// validClassifications is the ordered set of allowed classifications (least to most restrictive).
var validClassifications = []string{"public", "internal", "confidential", "restricted"}

// classificationRank returns the rank (0=public, 1=internal, 2=confidential, 3=restricted).
// Returns -1 for invalid classifications.
func classificationRank(c string) int {
	for i, v := range validClassifications {
		if v == c {
			return i
		}
	}
	return -1
}

// reservedContextKeys is the set of keys forbidden in handoff context.value.
var reservedContextKeys = map[string]bool{
	"password":         true,
	"api_key":          true,
	"OPENAI_API_KEY":   true,
	"credential":       true,
	"capability_token": true,
	"docker.sock":      true,
	"token":            true,
	"secret":           true,
	"private_key":      true,
	"access_key":       true,
}

// forbiddenPathPrefixes are path prefixes forbidden in immutable_ref.
var forbiddenPathPrefixes = []string{
	"/var/",
	"/app/",
	"/etc/",
	"/root/",
	"/home/",
	"/tmp/",
	"/proc/",
	"/sys/",
	"/dev/",
}
