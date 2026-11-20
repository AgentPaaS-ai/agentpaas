package port

import (
	"context"
	"io"
)

// ArtifactStore manages tenant-scoped durable artifacts.
type ArtifactStore interface {
	Commit(context.Context, CommitArtifactRequest) (ArtifactID, string, error)
	Authorize(context.Context, ArtifactID, string) error
	Stream(context.Context, ArtifactID) (io.ReadCloser, error)
	RangeRead(context.Context, ArtifactID, int64, int64) ([]byte, error)
	Verify(context.Context, ArtifactID, string) error
	Retain(context.Context, ArtifactID, RetentionPolicy) error
	Delete(context.Context, ArtifactID) error
}

// ArtifactID identifies an artifact.
type ArtifactID string

// CommitArtifactRequest describes an artifact to commit.
type CommitArtifactRequest struct {
	TenantID  string
	RunID     string
	AttemptID string
	RelPath   string
	MediaType string
	MaxBytes  int64
}

// RetentionPolicy controls artifact retention.
type RetentionPolicy struct {
	MinDays             int
	MaxBytes            int64
	DeleteAfterTerminal bool
}
