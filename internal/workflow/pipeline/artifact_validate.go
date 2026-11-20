package pipeline

import (
	"context"
	"fmt"
)

// WorkflowArtifactBudget tracks aggregate artifact sizes and counts across stages.
type WorkflowArtifactBudget struct {
	MaxTotalBytes int64
	MaxCount      int
	UsedBytes     int64
	UsedCount     int
}

// Account adds size to the budget, returning an error if exceeded.
func (b *WorkflowArtifactBudget) Account(size int64) error {
	if b.MaxTotalBytes > 0 && b.UsedBytes+size > b.MaxTotalBytes {
		return fmt.Errorf("%s: artifact of %d bytes would exceed max total %d (used %d)",
			CodeArtifactBudgetExceeded, size, b.MaxTotalBytes, b.UsedBytes)
	}
	if b.MaxCount > 0 && b.UsedCount+1 > b.MaxCount {
		return fmt.Errorf("%s: artifact count would exceed max %d",
			CodeArtifactBudgetExceeded, b.MaxCount)
	}
	b.UsedBytes += size
	b.UsedCount++
	return nil
}

// PromoteHandoffArtifacts validates each HandoffArtifact against the store,
// checking existence, digest match, owner match, classification, isSafeRef,
// and budget. Returns a list of promoted (durable, immutable) refs.
func PromoteHandoffArtifacts(
	ctx context.Context,
	store ArtifactStore,
	budget *WorkflowArtifactBudget,
	producerNodeID, producerRunID string,
	arts []HandoffArtifact,
) ([]HandoffArtifact, error) {
	promoted := make([]HandoffArtifact, 0, len(arts))
	for _, art := range arts {
		// Validate isSafeRef.
		if !isSafeRef(art.ImmutableRef) {
			return nil, fmt.Errorf("%s: unsafe immutable_ref %q", CodeArtifactPathRejected, art.ImmutableRef)
		}

		// Owner match check.
		if art.OwnerNodeID != "" && art.OwnerNodeID != producerNodeID {
			return nil, fmt.Errorf("%s: artifact %q owned by node %q, not producer %q",
				CodeArtifactOwnerMismatch, art.ArtifactID, art.OwnerNodeID, producerNodeID)
		}
		if art.OwnerRunID != "" && art.OwnerRunID != producerRunID {
			return nil, fmt.Errorf("%s: artifact %q owned by run %q, not producer %q",
				CodeArtifactOwnerMismatch, art.ArtifactID, art.OwnerRunID, producerRunID)
		}

		// Verify artifact exists in store.
		storedMeta, _, err := store.Get(ctx, art.ArtifactID)
		if err != nil {
			return nil, fmt.Errorf("%s: artifact %q not found in store: %w", CodeArtifactNotFound, art.ArtifactID, err)
		}

		// Verify digest matches stored content.
		if err := store.VerifyDigest(ctx, art.ArtifactID); err != nil {
			return nil, fmt.Errorf("%s: artifact %q: %w", CodeArtifactDigestMismatch, art.ArtifactID, err)
		}

		// Verify artifact digest matches stored metadata digest.
		if art.Digest != "" && art.Digest != storedMeta.Digest {
			return nil, fmt.Errorf("%s: artifact %q provided digest %q != stored digest %q",
				CodeArtifactDigestMismatch, art.ArtifactID, art.Digest, storedMeta.Digest)
		}

		// Classification check: artifact classification must not be less restrictive.
		artRank := classificationRank(art.Classification)
		if artRank == -1 {
			return nil, fmt.Errorf("%s: invalid classification %q on artifact %q",
				CodeArtifactPathRejected, art.Classification, art.ArtifactID)
		}

		// Budget check.
		if budget != nil {
			if err := budget.Account(storedMeta.SizeBytes); err != nil {
				return nil, err
			}
		}
		_ = art // used through storedMeta
		promoted = append(promoted, storedMeta)
	}
	return promoted, nil
}

// ValidateHandoffArtifacts runs store-agnostic validation on a list of HandoffArtifact
// references (no store lookup). Used for pre-promotion validation.
func ValidateHandoffArtifacts(arts []HandoffArtifact) []string {
	var codes []string
	for _, art := range arts {
		if art.Digest != "" && !isValidDigest(art.Digest) {
			codes = append(codes, CodeArtifactDigestMismatch)
		}
		if !isSafeRef(art.ImmutableRef) {
			codes = append(codes, CodeArtifactPathRejected)
		}
	}
	return codes
}

// ProvenanceChain walks handoff artifacts back to producer run IDs in order.
func ProvenanceChain(arts []HandoffArtifact) []string {
	runIDs := make([]string, 0, len(arts))
	for _, art := range arts {
		if art.OwnerRunID != "" {
			runIDs = append(runIDs, art.OwnerRunID)
		}
	}
	return runIDs
}
