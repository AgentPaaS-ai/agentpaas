package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestExportImageTarDockerAgainstFakeDaemon drives the real Docker export path
// (create, export, remove) against a fake daemon. The Docker client refuses a
// create that names the image both in the options and in the config, before it
// sends anything, so an export built that way never reaches the daemon and a
// linux/amd64 pack writes no run bundle.
func TestExportImageTarDockerAgainstFakeDaemon(t *testing.T) {
	const imageRef = "agentpaas/weather-agent:0.1.0"
	containerID := strings.Repeat("ab", 32)
	payload := "image-export-bytes"

	var mu sync.Mutex
	var creates, removes int
	var createdImage, createdPlatform string
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/_ping"):
			w.Header().Set("Api-Version", "1.47")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "OK")
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/containers/create"):
			var body struct {
				Image string `json:"Image"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			creates++
			createdImage = body.Image
			createdPlatform = r.URL.Query().Get("platform")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": containerID, "Warnings": []string{}})
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/containers/"+containerID+"/export"):
			w.Header().Set("Content-Type", "application/x-tar")
			_, _ = io.WriteString(w, payload)
		case r.Method == http.MethodDelete && strings.HasSuffix(p, "/containers/"+containerID):
			mu.Lock()
			removes++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(daemon.URL, "http://"))
	t.Setenv("DOCKER_TLS_VERIFY", "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rc, err := exportImageTarDocker(ctx, imageRef)
	if err != nil {
		t.Fatalf("export through the fake daemon: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close export: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if creates != 1 {
		t.Fatalf("container creates = %d, want 1", creates)
	}
	if createdImage != imageRef {
		t.Fatalf("created image = %q, want %q", createdImage, imageRef)
	}
	if createdPlatform != "linux/amd64" {
		t.Fatalf("created platform = %q, want linux/amd64", createdPlatform)
	}
	if string(got) != payload {
		t.Fatalf("export bytes = %q, want %q", got, payload)
	}
	if removes != 1 {
		t.Fatalf("container removes after close = %d, want 1", removes)
	}
}

func dockerShapedTar(t *testing.T, entries []tar.Header, bodies map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i := range entries {
		h := entries[i]
		body := bodies[h.Name]
		h.Size = int64(len(body))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("write header %s: %v", h.Name, err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatalf("write body %s: %v", h.Name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return &buf
}

// TestReadImageExportAcceptsDockerDirectoryNames pins the shape of a real
// container export: Docker names every directory entry with a trailing slash.
func TestReadImageExportAcceptsDockerDirectoryNames(t *testing.T) {
	bodies := map[string]string{
		"agentpaas/requirements.lock": "\n",
		"app/main.py":                 "print('hi')\n",
	}
	export := dockerShapedTar(t, []tar.Header{
		{Name: ".dockerenv", Typeflag: tar.TypeReg, Mode: 0o755},
		{Name: "agentpaas/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "agentpaas/requirements.lock", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "app/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "app/python/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "app/main.py", Typeflag: tar.TypeReg, Mode: 0o600},
	}, bodies)
	entries, err := readImageExport(export)
	if err != nil {
		t.Fatalf("read a Docker-shaped export: %v", err)
	}
	for name, want := range bodies {
		got, ok := entries[name]
		if !ok {
			t.Fatalf("entry %s missing from the export read", name)
		}
		if string(got.body) != want {
			t.Fatalf("entry %s body = %q, want %q", name, got.body, want)
		}
	}
}

// TestReadImageExportTrimDoesNotOpenRefusals pins the names a one-slash
// directory trim must still refuse: a non-directory whose name ends in a
// slash, a directory with a ".." part, a directory whose name ends in two
// slashes, and a directory app/x/ beside the file app/x (a duplicate).
func TestReadImageExportTrimDoesNotOpenRefusals(t *testing.T) {
	tests := []struct {
		name    string
		headers []tar.Header
		bodies  map[string]string
		want    string
	}{
		{
			// A symlink may name a directory, so the writer allows a trailing
			// slash on it. The trim must not treat that as a directory entry.
			name: "non-directory trailing slash",
			headers: []tar.Header{
				{Name: "app/x/", Typeflag: tar.TypeSymlink, Linkname: "elsewhere", Mode: 0o777},
			},
			want: "path rejected",
		},
		{
			name: "directory dotdot",
			headers: []tar.Header{
				{Name: "app/../secret/", Typeflag: tar.TypeDir, Mode: 0o755},
			},
			want: "path must not contain '..'",
		},
		{
			name: "directory two trailing slashes",
			headers: []tar.Header{
				{Name: "app/x//", Typeflag: tar.TypeDir, Mode: 0o755},
			},
			want: "path rejected",
		},
		{
			name: "directory slash duplicates file",
			headers: []tar.Header{
				{Name: "app/x", Typeflag: tar.TypeReg, Mode: 0o644},
				{Name: "app/x/", Typeflag: tar.TypeDir, Mode: 0o755},
			},
			bodies: map[string]string{"app/x": "file\n"},
			want:   "duplicate image path app/x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readImageExport(dockerShapedTar(t, tt.headers, tt.bodies))
			if err == nil {
				t.Fatal("export was accepted")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
