package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ProjectPlan is a RO bind list for the next stage (no host path leak into handoff).
type ProjectPlan struct {
	// Container binds host:container:ro — host is under a workflow-owned projection root.
	Binds []string
	// MountRoot is reserved SDK path e.g. /agentpaas/incoming-artifacts (container path).
	MountRoot string
}

// ArtifactProjection holds a materialized RO projection of artifacts on the host.
type ArtifactProjection struct {
	Plan ProjectPlan
	Dir  string
}

// Cleanup removes the projection directory.
func (p *ArtifactProjection) Cleanup() error {
	if p.Dir == "" {
		return nil
	}
	return os.RemoveAll(p.Dir)
}

// BuildROProjection creates a temp directory under baseDir, writes verified content
// as files named by safe basename of ImmutableRef, returns ProjectPlan with :ro binds.
// Rejects symlink, device, path escape. Caller must call Cleanup().
func BuildROProjection(
	ctx context.Context,
	store ArtifactStore,
	baseDir string,
	arts []HandoffArtifact,
) (*ArtifactProjection, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("%s: base directory is required", CodeArtifactPathRejected)
	}

	// Create a temp directory under baseDir.
	projDir, err := os.MkdirTemp(baseDir, "artifact-projection-")
	if err != nil {
		return nil, fmt.Errorf("mkdirtemp: %w", err)
	}

	binds := make([]string, 0, len(arts))
	for _, art := range arts {
		// Validate isSafeRef.
		if !isSafeRef(art.ImmutableRef) {
			_ = os.RemoveAll(projDir) // best-effort cleanup
			return nil, fmt.Errorf("%s: unsafe immutable_ref %q", CodeArtifactPathRejected, art.ImmutableRef)
		}

		// Get content from store.
		_, content, err := store.Get(ctx, art.ArtifactID)
		if err != nil {
			_ = os.RemoveAll(projDir)
			return nil, fmt.Errorf("%s: artifact %q not found: %w", CodeArtifactNotFound, art.ArtifactID, err)
		}

		// Verify digest.
		if err := store.VerifyDigest(ctx, art.ArtifactID); err != nil {
			_ = os.RemoveAll(projDir)
			return nil, fmt.Errorf("%s: artifact %q digest mismatch: %w", CodeArtifactDigestMismatch, art.ArtifactID, err)
		}

		// Write to projection dir using safe basename.
		writtenPath, err := artifactPutToFS(projDir, art, content)
		if err != nil {
			_ = os.RemoveAll(projDir)
			return nil, fmt.Errorf("write artifact to projection: %w", err)
		}

		// Build bind: host_path:container_path:ro
		safeBase := filepath.Base(writtenPath)
		containerPath := filepath.Join("/agentpaas/incoming-artifacts", safeBase)
		bind := fmt.Sprintf("%s:%s:ro", writtenPath, containerPath)
		binds = append(binds, bind)
	}

	return &ArtifactProjection{
		Plan: ProjectPlan{
			Binds:     binds,
			MountRoot: "/agentpaas/incoming-artifacts",
		},
		Dir: projDir,
	}, nil
}
