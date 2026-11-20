package dashboard

import "context"

// MockDockerArtifactProvider returns canned Docker metadata for log viewer tests.
type MockDockerArtifactProvider struct {
	Artifacts []DockerArtifact
	Err       error
}

func (m *MockDockerArtifactProvider) ListDockerArtifacts(_ context.Context, _ string) ([]DockerArtifact, error) {
	return m.Artifacts, m.Err
}
