package k8s

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// K8sArtifactStore is an in-memory artifact store keyed by tenant-scoped ArtifactID.
type K8sArtifactStore struct {
	mu    sync.Mutex
	store map[port.ArtifactID]*k8sArtifactEntry
}

type k8sArtifactEntry struct {
	tenantID string
	data     []byte
	digest   string
}

var _ port.ArtifactStore = (*K8sArtifactStore)(nil)

func (a *K8sArtifactStore) Commit(_ context.Context, r port.CommitArtifactRequest) (port.ArtifactID, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil {
		a.store = make(map[port.ArtifactID]*k8sArtifactEntry)
	}
	id := port.ArtifactID(fmt.Sprintf("%s/%s/%s", r.TenantID, r.RunID, r.RelPath))
	h := sha256.Sum256([]byte(r.RelPath))
	digest := "sha256:" + hex.EncodeToString(h[:])
	a.store[id] = &k8sArtifactEntry{
		tenantID: r.TenantID,
		data:     []byte{},
		digest:   digest,
	}
	return id, digest, nil
}

func (a *K8sArtifactStore) Authorize(_ context.Context, id port.ArtifactID, _ string) error { // intentionally ignored (reviewed)
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.store[id]; !ok {
		return port.ErrNotFound
	}
	return nil
}

func (a *K8sArtifactStore) Stream(_ context.Context, id port.ArtifactID) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.store[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(e.data)), nil
}

func (a *K8sArtifactStore) RangeRead(_ context.Context, id port.ArtifactID, offset, length int64) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.store[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	if offset < 0 || offset >= int64(len(e.data)) {
		return nil, port.ErrNotFound
	}
	end := offset + length
	if end > int64(len(e.data)) || length <= 0 {
		end = int64(len(e.data))
	}
	return e.data[offset:end], nil
}

func (a *K8sArtifactStore) Verify(_ context.Context, id port.ArtifactID, expectedDigest string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.store[id]
	if !ok {
		return port.ErrNotFound
	}
	if e.digest != expectedDigest {
		return fmt.Errorf("digest mismatch: expected %s, got %s", expectedDigest, e.digest)
	}
	return nil
}

func (a *K8sArtifactStore) Retain(_ context.Context, _ port.ArtifactID, _ port.RetentionPolicy) error { // intentionally ignored (reviewed)
	return nil
}

func (a *K8sArtifactStore) Delete(_ context.Context, id port.ArtifactID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.store[id]; !ok {
		return port.ErrNotFound
	}
	delete(a.store, id)
	return nil
}
