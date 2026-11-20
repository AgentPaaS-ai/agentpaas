package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBake_ReadyWithoutPython(t *testing.T) {
	python, marker := pythonMarker(t)
	agent := writeAgent(t, "def invoke(payload):\n    return {\"ok\": True}\n")
	srv := NewServer(Config{
		Bake:      true,
		AgentPath: agent,
		Python:    python,
	})
	t.Cleanup(func() { _ = srv.Close() })

	if srv.worker != nil {
		t.Fatal("bake mode started the worker")
	}
	for _, path := range []string{"/healthz", "/health", "/readyz"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body %s", path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s body: %v", path, err)
		}
		if body["status"] != "OK" {
			t.Fatalf("%s status field = %v", path, body["status"])
		}
		if path != "/readyz" && body["ready"] != true {
			t.Fatalf("%s ready = %v, want true", path, body["ready"])
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"ping":1}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("bake invoke status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bake invoke body: %v", err)
	}
	if got.Reason != ReasonBakeMode {
		t.Fatalf("bake invoke reason = %s, want %s", got.Reason, ReasonBakeMode)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("python ran during bake: %v", err)
	}
}

func TestRequestCannotEnterBake(t *testing.T) {
	srv := newReadyServer(t, "def invoke(payload):\n    return {\"ok\": True, \"source\": \"live\"}\n")
	t.Cleanup(func() { _ = srv.Close() })
	t.Setenv("AGENTPAAS_BAKE", "1")

	for _, path := range []string{"/bake", "/healthz/bake"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"bake":true}`))
		req.Header.Set("X-Agentpaas-Bake", "1")
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s entered bake: %s", path, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"ping":1}`))
	req.Header.Set("X-Agentpaas-Bake", "1")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "live") {
		t.Fatalf("invoke after bake request = %d %s", rec.Code, rec.Body.String())
	}
	if srv.cfg.Bake {
		t.Fatal("a request flipped the server into bake mode")
	}
}

func TestRestoredTree_SameDigestSkipsFetchAndInvokes(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	cfg := runBundleCfg(srv.URL, body, root)
	if err := LoadRunBundle(context.Background(), cfg, ioDiscard()); err != nil {
		t.Fatalf("first load: %v", err)
	}
	if fetches != 1 {
		t.Fatalf("fetches = %d, want 1", fetches)
	}

	var buf bytes.Buffer
	err := StartAfterBundle(context.Background(), cfg, &buf, func() error {
		agent := filepath.Join(root, "app", "agent.py")
		hs := NewServer(Config{AgentPath: agent})
		t.Cleanup(func() { _ = hs.Close() })
		waitHarnessReady(t, hs)
		rec := httptest.NewRecorder()
		hs.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"ping":"bundle"}`)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "bundle") {
			t.Fatalf("restored invoke = %d %s", rec.Code, rec.Body.String())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("restored start: %v %q", err, buf.String())
	}
	if fetches != 1 {
		t.Fatalf("restored start fetched again: %d", fetches)
	}
}

func TestRestoredTree_DifferentDigestRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.SHA256 = strings.Repeat("ab", 32)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	var started bool
	err := StartAfterBundle(context.Background(), cfg, &buf, func() error {
		started = true
		return nil
	})
	if err == nil || started {
		t.Fatalf("different digest started: err=%v", err)
	}
	assertOneReason(t, &buf, ReasonBundleTreeDigestMismatch)
}

func TestRestoredTree_ChangedDepFileRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	dep := filepath.Join(root, "app", "deps", "idna", "__init__.py")
	if err := os.WriteFile(dep, []byte("# tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	err := LoadRunBundle(context.Background(), cfg, &buf)
	if err == nil {
		t.Fatal("changed /app/deps file was accepted")
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
}

func TestRestoredTree_ManifestDigestAlteredRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	rewriteManifest(t, root, func(m *BundleManifest) {
		m.BundleDigest = strings.Repeat("cd", 32)
	})
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	err := LoadRunBundle(context.Background(), cfg, &buf)
	if err == nil {
		t.Fatal("altered manifest digest was accepted")
	}
	assertOneReason(t, &buf, ReasonBundleTreeDigestMismatch)
}

func TestRestoredTree_ManifestFileHashAlteredRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	rewriteManifest(t, root, func(m *BundleManifest) {
		if len(m.Files) == 0 {
			t.Fatal("manifest has no files")
		}
		m.Files[0].SHA256 = strings.Repeat("ef", 32)
	})
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	err := LoadRunBundle(context.Background(), cfg, &buf)
	if err == nil {
		t.Fatal("altered manifest file hash was accepted")
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
}

func TestRestoredTree_PairedFileAndManifestHashRewriteRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	const rewritten = "def invoke(payload):\n    return {\"ok\": True, \"source\": \"rewritten\"}\n"
	agent := filepath.Join(root, "app", "agent.py")
	if err := os.WriteFile(agent, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	rewriteManifest(t, root, func(m *BundleManifest) {
		for i := range m.Files {
			if m.Files[i].Path != "app/agent.py" {
				continue
			}
			m.Files[i].SHA256 = sha256Hex(rewritten)
			m.Files[i].Size = int64(len(rewritten))
		}
	})
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	var started bool
	err := StartAfterBundle(context.Background(), cfg, &buf, func() error {
		started = true
		return nil
	})
	if err == nil || started {
		t.Fatalf("paired rewrite of agent.py and its manifest hash started: err=%v", err)
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
	got, err := os.ReadFile(agent)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != rewritten {
		t.Fatalf("refusal rewrote the agent: %q", got)
	}
}

func TestRestoredTree_RewrittenVerifiedTarRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	const rewritten = "def invoke(payload):\n    return {\"ok\": True, \"source\": \"rewritten\"}\n"
	evil := goodRunBundleTar(t, rewritten)
	if len(evil) != len(body) {
		t.Fatalf("evil tar size = %d, want %d so the check is the digest, not the size", len(evil), len(body))
	}
	if err := os.WriteFile(filepath.Join(root, "app", "agent.py"), []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(BundleTarRel)), evil, 0o644); err != nil {
		t.Fatal(err)
	}
	rewriteManifest(t, root, func(m *BundleManifest) {
		for i := range m.Files {
			if m.Files[i].Path != "app/agent.py" {
				continue
			}
			m.Files[i].SHA256 = sha256Hex(rewritten)
			m.Files[i].Size = int64(len(rewritten))
		}
	})
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	var started bool
	err := StartAfterBundle(context.Background(), cfg, &buf, func() error {
		started = true
		return nil
	})
	if err == nil || started {
		t.Fatalf("rewritten verified tar started under the honest digest: err=%v", err)
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
}

// A restored tree whose manifest was removed is not a fresh tree. Unpack
// writes only the files the bundle names, so a download over it would keep
// planted startup code (sitecustomize on PYTHONPATH, a shadow agentpaas_sdk
// under /app/python) and start it under the honest digest.
func TestRestoredTree_DeletedManifestRefusedWithoutFetch(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	for _, tc := range []struct {
		name   string
		remove []string
	}{
		{name: "manifest removed", remove: []string{BundleManifestRel}},
		{name: "manifest kept tar and bundle files removed", remove: []string{
			BundleManifestRel, BundleTarRel, "app/agent.py", "app/deps/idna/__init__.py",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := unpackBundle(t, body)
			for _, rel := range tc.remove {
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Fatal(err)
				}
			}
			if err := plantStartupCode(root); err != nil {
				t.Fatal(err)
			}
			cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
			cfg.HTTPClient = failIfFetched(t)
			var buf bytes.Buffer
			var started bool
			err := StartAfterBundle(context.Background(), cfg, &buf, func() error {
				started = true
				return nil
			})
			if err == nil || started {
				t.Fatalf("tree without its manifest started: err=%v", err)
			}
			assertOneReason(t, &buf, ReasonBundleTreeManifestInvalid)
		})
	}
}

func TestRunBundle_EmptyAppDirDownloads(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := LoadRunBundle(context.Background(), runBundleCfg(serveRunBundle(t, body).URL, body, root), &buf); err != nil {
		t.Fatalf("empty /app (the base image) refused: %v %q", err, buf.String())
	}
}

func TestRunBundle_FileOutsideBundleAfterUnpackRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Lands after the fresh-tree check and before unpack.
		if err := plantStartupCode(root); err != nil {
			t.Errorf("plant: %v", err)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	var buf bytes.Buffer
	var started bool
	err := StartAfterBundle(context.Background(), runBundleCfg(srv.URL, body, root), &buf, func() error {
		started = true
		return nil
	})
	if err == nil || started {
		t.Fatalf("file the bundle does not name started: err=%v", err)
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(BundleManifestRel))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused tree got a manifest: %v", err)
	}
}

func plantStartupCode(root string) error {
	for rel, src := range map[string]string{
		"app/deps/sitecustomize.py":            "open('/tmp/planted', 'a').write('sitecustomize')\n",
		"app/python/agentpaas_sdk/__init__.py": "def run(*a, **k):\n    open('/tmp/planted', 'a').write('sdk-run')\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestRestoredTree_PythonUstarSameDigestSkipsFetch(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "bundle.tar")
	script := `
import io, sys, tarfile
agent = b'def invoke(payload):\n    return {"ok": True, "source": "bundle"}\n'
entries = [
    ("app/", None),
    ("app/agent.py", agent),
    ("app/deps/", None),
    ("app/deps/idna/", None),
    ("app/deps/idna/__init__.py", b"# dep\n"),
    ("agentpaas/", None),
    ("agentpaas/requirements.lock", b"idna==3.7\n"),
]
buf = io.BytesIO()
with tarfile.open(fileobj=buf, mode="w", format=tarfile.USTAR_FORMAT) as tw:
    for name, body in entries:
        info = tarfile.TarInfo(name)
        info.uid = 0
        info.gid = 0
        info.mtime = 0
        if name.endswith("/"):
            info.type = tarfile.DIRTYPE
            info.mode = 0o755
        else:
            info.type = tarfile.REGTYPE
            info.mode = 0o644
            info.size = len(body)
        tw.addfile(info, io.BytesIO(body) if body is not None else None)
open(sys.argv[1], "wb").write(buf.getvalue())
`
	cmd := exec.Command("python3", "-c", script, tarPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python tar: %v %s", err, out)
	}
	body, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	cfg := runBundleCfg(srv.URL, body, root)
	if err := LoadRunBundle(context.Background(), cfg, ioDiscard()); err != nil {
		t.Fatalf("python tar unpack: %v", err)
	}
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	err = StartAfterBundle(context.Background(), cfg, &buf, func() error { return nil })
	if err != nil {
		t.Fatalf("python tar restore: %v %q", err, buf.String())
	}
	if fetches != 1 {
		t.Fatalf("python tar restore fetched again: %d", fetches)
	}
}

func TestRestoredTree_RehashSwitchOffSkipsFileCheck(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	dep := filepath.Join(root, "app", "deps", "idna", "__init__.py")
	if err := os.WriteFile(dep, []byte("# tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	off := false
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.FullRehash = &off
	cfg.HTTPClient = failIfFetched(t)
	if err := LoadRunBundle(context.Background(), cfg, ioDiscard()); err != nil {
		t.Fatalf("rehash off refused a changed file: %v", err)
	}
}

func TestRestoredTree_ImageHarnessIsNotABundleFile(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	harness := filepath.Join(root, "agentpaas", "harness")
	if err := os.WriteFile(harness, []byte("not-a-bundle-file"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	if err := LoadRunBundle(context.Background(), cfg, ioDiscard()); err != nil {
		t.Fatalf("image harness binary refused a restored tree: %v", err)
	}
	extra := filepath.Join(root, "app", "deps", "extra.py")
	if err := os.WriteFile(extra, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := LoadRunBundle(context.Background(), cfg, &buf); err == nil {
		t.Fatal("extra file under /app was accepted")
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
}

func TestFullRehashDefaultOn(t *testing.T) {
	t.Setenv(EnvBundleRehash, "")
	if !FullRehashDefault() {
		t.Fatal("unset rehash switch must be on so a changed /app/deps file is refused")
	}
	body := goodRunBundleTar(t, bundleAgentSource)
	root := unpackBundle(t, body)
	if err := os.WriteFile(filepath.Join(root, "app", "deps", "idna", "__init__.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runBundleCfg("http://127.0.0.1:9/bundle", body, root)
	cfg.HTTPClient = failIfFetched(t)
	var buf bytes.Buffer
	if err := LoadRunBundle(context.Background(), cfg, &buf); err == nil {
		t.Fatal("default rehash accepted a changed file")
	}
	assertOneReason(t, &buf, ReasonBundleTreeRehashMismatch)
}

func TestLegacyStartWithoutBundleEnv(t *testing.T) {
	t.Setenv(EnvBundleURL, "")
	t.Setenv(EnvBundleSHA256, "")
	t.Setenv(EnvBundleSize, "")
	root := t.TempDir()
	agent := filepath.Join(root, "app", "main.py")
	if err := os.MkdirAll(filepath.Dir(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, []byte("def invoke(payload):\n    return {\"ok\": True, \"source\": \"legacy\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := LoadRunBundleFromEnv(context.Background(), &buf); err != nil {
		t.Fatalf("unset bundle env refused: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("legacy start wrote %q", buf.String())
	}
	srv := NewServer(Config{AgentPath: agent})
	t.Cleanup(func() { _ = srv.Close() })
	waitHarnessReady(t, srv)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "legacy") {
		t.Fatalf("legacy invoke = %d %s", rec.Code, rec.Body.String())
	}
}

func TestBaseImageContract(t *testing.T) {
	root := repoRoot(t)
	docker := string(mustRead(t, filepath.Join(root, "build", "base-image", "Dockerfile")))
	makeText := string(mustRead(t, filepath.Join(root, "Makefile")))
	for _, want := range []string{
		"gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		"USER 64000:64000",
		`ENTRYPOINT ["/agentpaas/harness"]`,
		"--chown=64000:64000",
		"/app",
	} {
		if !strings.Contains(docker, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
	if strings.Count(docker, "FROM ") < 2 {
		t.Fatal("Dockerfile must create empty /app in a stage and use pinned distroless as the runtime base")
	}
	final := docker[strings.LastIndex(docker, "FROM "):]
	if !strings.Contains(final, "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f") {
		t.Fatal("runtime base is not the pinned distroless image")
	}
	for _, want := range []string{"base-image", "py311-r1", "linux/amd64", "build-harness-linux-amd64", "agentpaas/py311-r1"} {
		if !strings.Contains(makeText, want) {
			t.Fatalf("Makefile missing %q", want)
		}
	}
}

func unpackBundle(t *testing.T, body []byte) string {
	t.Helper()
	root := t.TempDir()
	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	if err := LoadRunBundle(context.Background(), runBundleCfg(srv.URL, body, root), ioDiscard()); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if fetches != 1 {
		t.Fatalf("unpack fetches = %d", fetches)
	}
	return root
}

func rewriteManifest(t *testing.T, root string, edit func(*BundleManifest)) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(BundleManifestRel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	edit(&manifest)
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pythonMarker(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "python3")
	body := "#!/bin/sh\ntouch " + marker + "\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, marker
}

func failIfFetched(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("restored tree must not fetch")
		return nil, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func ioDiscard() *bytes.Buffer { return &bytes.Buffer{} }

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
