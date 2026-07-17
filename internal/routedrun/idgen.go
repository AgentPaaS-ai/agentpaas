package routedrun

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

// ID prefixes match the stable ID types and existing contract tests.
const (
	PrefixDeployment     = "dep-"
	PrefixInvocation     = "inv-"
	PrefixControl        = "ctrl-"
	PrefixAmendment      = "amend-"
	PrefixWorkflow       = "wf-"
	PrefixNode           = "node-"
	PrefixService        = "svc-"
	PrefixHandoff        = "ho-"
	PrefixChildBatch     = "cb-"
	PrefixChildResult    = "cr-"
	PrefixArtifact       = "art-"
	PrefixRun            = "run-"
	PrefixAttempt        = "at-"
	PrefixLease          = "ls-"
	PrefixCheckpoint     = "cp-"
	PrefixModelCall      = "mc-"
	PrefixIdempotency    = "idem-"
)

// idEntropyBytes is the number of random bytes used in each generated ID.
const idEntropyBytes = 16

// base32NoPad is a lowercase Crockford-friendly encoding without padding or
// hyphens in the random part (standard base32 with padding stripped, lowercased).
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateID returns prefix + base32(random bytes), lowercase, no hyphens in the
// random part. Uses crypto/rand only.
func generateID(prefix string) (string, error) {
	var b [idEntropyBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("routedrun: generate id: %w", err)
	}
	encoded := strings.ToLower(base32NoPad.EncodeToString(b[:]))
	// base32 alphabet has no hyphens; keep random part free of separators.
	return prefix + encoded, nil
}

// NewDeploymentID generates a cryptographically random deployment ID.
func NewDeploymentID() (DeploymentID, error) {
	s, err := generateID(PrefixDeployment)
	return DeploymentID(s), err
}

// NewInvocationID generates a cryptographically random invocation ID.
func NewInvocationID() (InvocationID, error) {
	s, err := generateID(PrefixInvocation)
	return InvocationID(s), err
}

// NewControlRequestID generates a cryptographically random control request ID.
func NewControlRequestID() (ControlRequestID, error) {
	s, err := generateID(PrefixControl)
	return ControlRequestID(s), err
}

// NewLimitAmendmentID generates a cryptographically random limit amendment ID.
func NewLimitAmendmentID() (LimitAmendmentID, error) {
	s, err := generateID(PrefixAmendment)
	return LimitAmendmentID(s), err
}

// NewWorkflowID generates a cryptographically random workflow ID.
func NewWorkflowID() (WorkflowID, error) {
	s, err := generateID(PrefixWorkflow)
	return WorkflowID(s), err
}

// NewNodeID generates a cryptographically random node ID.
func NewNodeID() (NodeID, error) {
	s, err := generateID(PrefixNode)
	return NodeID(s), err
}

// NewServiceID generates a cryptographically random service ID.
func NewServiceID() (ServiceID, error) {
	s, err := generateID(PrefixService)
	return ServiceID(s), err
}

// NewHandoffID generates a cryptographically random handoff ID.
func NewHandoffID() (HandoffID, error) {
	s, err := generateID(PrefixHandoff)
	return HandoffID(s), err
}

// NewChildBatchID generates a cryptographically random child batch ID.
func NewChildBatchID() (ChildBatchID, error) {
	s, err := generateID(PrefixChildBatch)
	return ChildBatchID(s), err
}

// NewChildResultID generates a cryptographically random child result ID.
func NewChildResultID() (ChildResultID, error) {
	s, err := generateID(PrefixChildResult)
	return ChildResultID(s), err
}

// NewArtifactID generates a cryptographically random artifact ID.
func NewArtifactID() (ArtifactID, error) {
	s, err := generateID(PrefixArtifact)
	return ArtifactID(s), err
}

// NewRunID generates a cryptographically random run ID.
func NewRunID() (RunID, error) {
	s, err := generateID(PrefixRun)
	return RunID(s), err
}

// NewAttemptID generates a cryptographically random attempt ID.
func NewAttemptID() (AttemptID, error) {
	s, err := generateID(PrefixAttempt)
	return AttemptID(s), err
}

// NewLeaseID generates a cryptographically random opaque fencing token.
// Callers must never supply their own lease IDs; stores overwrite any
// caller-selected lease identity with NewLeaseID.
func NewLeaseID() (LeaseID, error) {
	s, err := generateID(PrefixLease)
	return LeaseID(s), err
}

// NewCheckpointID generates a cryptographically random checkpoint ID.
func NewCheckpointID() (CheckpointID, error) {
	s, err := generateID(PrefixCheckpoint)
	return CheckpointID(s), err
}

// NewModelCallID generates a cryptographically random model call ID.
func NewModelCallID() (ModelCallID, error) {
	s, err := generateID(PrefixModelCall)
	return ModelCallID(s), err
}

// ValidateIDPrefix returns true if id is non-empty and starts with the given prefix.
func ValidateIDPrefix(id, prefix string) bool {
	return id != "" && strings.HasPrefix(id, prefix) && len(id) > len(prefix)
}
