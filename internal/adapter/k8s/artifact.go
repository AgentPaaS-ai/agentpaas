package k8s

import (
	"bytes"
	"context"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"io"
)

type K8sArtifactStore struct{ data map[port.ArtifactID][]byte }

var _ port.ArtifactStore = (*K8sArtifactStore)(nil)

func (a *K8sArtifactStore) Commit(context.Context, port.CommitArtifactRequest) (port.ArtifactID, string, error) {
	return "", "", port.ErrNotFound
}
func (a *K8sArtifactStore) Authorize(context.Context, port.ArtifactID, string) error { return nil }
func (a *K8sArtifactStore) Stream(context.Context, port.ArtifactID) (io.ReadCloser, error) {
	if a.data == nil {
		return nil, port.ErrNotFound
	}
	b, ok := a.data[port.ArtifactID("")]
	if !ok {
		return nil, port.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (a *K8sArtifactStore) RangeRead(context.Context, port.ArtifactID, int64, int64) ([]byte, error) {
	return nil, port.ErrNotFound
}
func (a *K8sArtifactStore) Verify(context.Context, port.ArtifactID, string) error { return nil }
func (a *K8sArtifactStore) Retain(context.Context, port.ArtifactID, port.RetentionPolicy) error {
	return nil
}
func (a *K8sArtifactStore) Delete(context.Context, port.ArtifactID) error { return nil }
