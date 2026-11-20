package docker

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// DockerPackageStore resolves signed packages from the local Docker image store.
// In the Docker adapter, the Docker daemon's image store IS the package store.
type DockerPackageStore struct {
	mu       sync.Mutex
	packages map[string]*port.PackageResolution // digest -> resolution
	metadata []port.PackageMetadata
}

var _ port.PackageStore = (*DockerPackageStore)(nil)

// DockerPackageStore.Resolve resolves docker package store.
//
// It returns an error if the operation fails or inputs are invalid.
func (p *DockerPackageStore) Resolve(_ context.Context, tenantID, ref string) (*port.PackageResolution, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.packages == nil {
		return nil, port.ErrNotFound
	}
	// Look up by ref (which may be a digest or name:version)
	for _, res := range p.packages {
		if res.ImageDigest == ref || res.PackageName+":"+res.PackageVersion == ref {
			return res, nil
		}
	}
	return nil, port.ErrNotFound
}

// DockerPackageStore.Verify verifies docker package store.
//
// It returns an error if the operation fails or inputs are invalid.
func (p *DockerPackageStore) Verify(_ context.Context, digest string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.packages != nil {
		if _, ok := p.packages[digest]; ok {
			return nil
		}
	}
	return port.ErrNotFound
}

// DockerPackageStore.List lists docker package store.
//
// It returns an error if the operation fails or inputs are invalid.
func (p *DockerPackageStore) List(_ context.Context, tenantID string) ([]port.PackageMetadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []port.PackageMetadata
	for _, m := range p.metadata {
		if m.TenantID == tenantID {
			out = append(out, m)
		}
	}
	return out, nil
}
