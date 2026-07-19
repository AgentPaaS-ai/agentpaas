package docker

import (
	"bytes"
	"context"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"io"
)

type DockerArtifactStore struct{ data map[port.ArtifactID][]byte }

var _ port.ArtifactStore = (*DockerArtifactStore)(nil)

func (a *DockerArtifactStore) Commit(context.Context, port.CommitArtifactRequest) (port.ArtifactID, string, error) {
	return "", "", port.ErrNotFound
}
func (a *DockerArtifactStore) Authorize(context.Context, port.ArtifactID, string) error { return nil }
func (a *DockerArtifactStore) Stream(context.Context, port.ArtifactID) (io.ReadCloser, error) {
	if a.data == nil {
		return nil, port.ErrNotFound
	}
	b, ok := a.data[port.ArtifactID("")]
	if !ok {
		return nil, port.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (a *DockerArtifactStore) RangeRead(context.Context, port.ArtifactID, int64, int64) ([]byte, error) {
	return nil, port.ErrNotFound
}
func (a *DockerArtifactStore) Verify(context.Context, port.ArtifactID, string) error { return nil }
func (a *DockerArtifactStore) Retain(context.Context, port.ArtifactID, port.RetentionPolicy) error {
	return nil
}
func (a *DockerArtifactStore) Delete(context.Context, port.ArtifactID) error { return nil }
