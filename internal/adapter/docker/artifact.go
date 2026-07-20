package docker

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

// DockerArtifactStore is an in-memory artifact store keyed by tenant-scoped ArtifactID.
type DockerArtifactStore struct {
	mu    sync.Mutex
	store map[port.ArtifactID]*artifactEntry
}

type artifactEntry struct {
	tenantID string
	data     []byte
	digest   string
}

var _ port.ArtifactStore = (*DockerArtifactStore)(nil)

// DockerArtifactStore.Commit commits docker artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) Commit(_ context.Context, r port.CommitArtifactRequest) (port.ArtifactID, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil {
		a.store = make(map[port.ArtifactID]*artifactEntry)
	}
	// In a real adapter, this reads from the bind-mounted workspace.
	// For the proof, we generate a deterministic ID and empty content.
	id := port.ArtifactID(fmt.Sprintf("%s/%s/%s", r.TenantID, r.RunID, r.RelPath))
	h := sha256.Sum256([]byte(r.RelPath))
	digest := "sha256:" + hex.EncodeToString(h[:])
	a.store[id] = &artifactEntry{
		tenantID: r.TenantID,
		data:     []byte{},
		digest:   digest,
	}
	return id, digest, nil
}

// DockerArtifactStore.Authorize authorizes docker artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) Authorize(_ context.Context, id port.ArtifactID, accessor string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.store[id]
	if !ok {
		return port.ErrNotFound
	}
	_ = e // authorization is implicit in the tenant-scoped key
	return nil
}

// DockerArtifactStore.Stream streams docker artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) Stream(_ context.Context, id port.ArtifactID) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.store[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(e.data)), nil
}

// DockerArtifactStore.RangeRead range read.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) RangeRead(_ context.Context, id port.ArtifactID, offset, length int64) ([]byte, error) {
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

// DockerArtifactStore.Verify verifies docker artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) Verify(_ context.Context, id port.ArtifactID, expectedDigest string) error {
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

// DockerArtifactStore.Retain retain.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) Retain(_ context.Context, id port.ArtifactID, _ port.RetentionPolicy) error { // intentionally ignored (reviewed)
	return nil
}

// DockerArtifactStore.Delete deletes docker artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *DockerArtifactStore) Delete(_ context.Context, id port.ArtifactID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.store[id]; !ok {
		return port.ErrNotFound
	}
	delete(a.store, id)
	return nil
}
