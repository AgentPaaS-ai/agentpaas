package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// newEnsureImageRuntime creates a DockerRuntime backed by a test server.
func newEnsureImageRuntime(t *testing.T, handler http.HandlerFunc) *DockerRuntime {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost(srv.URL),
		client.WithHTTPClient(srv.Client()),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("client.NewClientWithOpts() error = %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return &DockerRuntime{cli: cli}
}

// TestEnsureImage_BareDigestFound verifies that a bare sha256: digest ref
// is resolved from the local Docker image store WITHOUT attempting a pull.
func TestEnsureImage_BareDigestFound(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	pullCalled := false
	rt := newEnsureImageRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/images/create") {
			pullCalled = true
			t.Errorf("ensureImage must NOT pull for bare digest refs")
		}
		if strings.Contains(r.URL.Path, "/images/json") {
			summaries := []image.Summary{
				{ID: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			}
			_ = json.NewEncoder(w).Encode(summaries)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	if err := rt.ensureImage(t.Context(), digest); err != nil {
		t.Fatalf("ensureImage failed: %v", err)
	}
	if pullCalled {
		t.Fatal("ImagePull was called for a bare digest ref — should never happen")
	}
}

// TestEnsureImage_BareDigestNotFound verifies that a missing bare digest
// returns a clear error without attempting a pull.
func TestEnsureImage_BareDigestNotFound(t *testing.T) {
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	pullCalled := false
	rt := newEnsureImageRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/images/create") {
			pullCalled = true
			t.Errorf("ensureImage must NOT pull for bare digest refs")
		}
		if strings.Contains(r.URL.Path, "/images/json") {
			// Return a different image, not the one we're looking for
			summaries := []image.Summary{
				{ID: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
			}
			_ = json.NewEncoder(w).Encode(summaries)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	err := rt.ensureImage(t.Context(), digest)
	if err == nil {
		t.Fatal("ensureImage should fail when image not found locally")
	}
	if pullCalled {
		t.Fatal("ImagePull was called for a bare digest ref — should never happen")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should mention 'not found', got: %v", err)
	}
}

// TestEnsureImage_BareDigestMatchWithoutPrefix verifies the ID match works when
// Docker returns IDs without the sha256: prefix.
func TestEnsureImage_BareDigestMatchWithoutPrefix(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	rt := newEnsureImageRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/images/json") {
			// Docker sometimes returns ID without sha256: prefix
			summaries := []image.Summary{
				{ID: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			}
			_ = json.NewEncoder(w).Encode(summaries)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	if err := rt.ensureImage(t.Context(), digest); err != nil {
		t.Fatalf("ensureImage failed: %v", err)
	}
}
