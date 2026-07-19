package k8s

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// K8sPackageStore resolves signed packages from the local K8s image store.
// In the K8s adapter, the K8s daemon's image store IS the package store.
type K8sPackageStore struct {
	mu       sync.Mutex
	packages map[string]*port.PackageResolution // digest -> resolution
	metadata []port.PackageMetadata
}

var _ port.PackageStore = (*K8sPackageStore)(nil)

func (p *K8sPackageStore) Resolve(_ context.Context, tenantID, ref string) (*port.PackageResolution, error) {
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

func (p *K8sPackageStore) Verify(_ context.Context, digest string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.packages != nil {
		if _, ok := p.packages[digest]; ok {
			return nil
		}
	}
	return port.ErrNotFound
}

func (p *K8sPackageStore) List(_ context.Context, tenantID string) ([]port.PackageMetadata, error) {
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
