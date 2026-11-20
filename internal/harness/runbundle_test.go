package harness

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const bundleAgentSource = "def invoke(payload):\n    return {\"ok\": True, \"source\": \"bundle\"}\n"

func TestRunBundle_GoodBundleStartsWorker(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	var buf bytes.Buffer
	var started bool
	err := StartAfterBundle(context.Background(), runBundleCfg(srv.URL, body, root), &buf, func() error {
		started = true
		agentPath := filepath.Join(root, "app", "agent.py")
		srv := NewServer(Config{AgentPath: agentPath})
		t.Cleanup(func() { _ = srv.Close() })
		waitHarnessReady(t, srv)

		req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"ping":"bundle"}`))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("invoke status = %d, body %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "bundle") {
			t.Fatalf("invoke did not run unpacked agent: %s", rec.Body.String())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StartAfterBundle: %v; reason line %q", err, buf.String())
	}
	if !started {
		t.Fatal("worker was not started")
	}
	if buf.Len() != 0 {
		t.Fatalf("success wrote a refusal line: %q", buf.String())
	}
	if fetches != 1 {
		t.Fatalf("fetches = %d, want 1 (hash and unpack must use the same read)", fetches)
	}

	gotAgent, err := os.ReadFile(filepath.Join(root, "app", "agent.py"))
	if err != nil {
		t.Fatalf("read unpacked agent: %v", err)
	}
	if string(gotAgent) != bundleAgentSource {
		t.Fatalf("unpacked agent = %q", gotAgent)
	}
	lock, err := os.ReadFile(filepath.Join(root, "agentpaas", "requirements.lock"))
	if err != nil {
		t.Fatalf("read requirements lock: %v", err)
	}
	if string(lock) != "idna==3.7\n" {
		t.Fatalf("requirements lock = %q", lock)
	}

	manifestPath := filepath.Join(root, BundleManifestRel)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.BundleDigest != sha256Hex(string(body)) || manifest.BundleSize != int64(len(body)) {
		t.Fatalf("manifest identity = %+v, want digest %s size %d", manifest, sha256Hex(string(body)), len(body))
	}
	wantFiles := map[string]string{
		"app/agent.py":                bundleAgentSource,
		"app/deps/idna/__init__.py":   "# dep\n",
		"agentpaas/requirements.lock": "idna==3.7\n",
	}
	if len(manifest.Files) != len(wantFiles) {
		t.Fatalf("manifest files = %+v, want %d entries", manifest.Files, len(wantFiles))
	}
	for _, file := range manifest.Files {
		want, ok := wantFiles[file.Path]
		if !ok {
			t.Fatalf("unexpected manifest path %s", file.Path)
		}
		if file.SHA256 != sha256Hex(want) || file.Size != int64(len(want)) {
			t.Fatalf("manifest entry %+v, want sha %s size %d", file, sha256Hex(want), len(want))
		}
	}
}

func TestRunBundle_ChangedByteRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	tampered := bytes.Clone(body)
	tampered[len(tampered)/2] ^= 0xff
	if bytes.Equal(body, tampered) {
		t.Fatal("tamper did not change the bundle")
	}
	root := t.TempDir()
	assertBundleRefused(t, root, serveRunBundle(t, tampered), body, ReasonBundleDigestMismatch)
}

func TestRunBundle_DotDotRefused(t *testing.T) {
	escape := "agentpaas-bundle-escape-" + strings.ReplaceAll(t.Name(), "/", "_") + ".txt"
	body := mustTar(t, []tarEntry{
		{name: "app/ok.txt", body: []byte("ok")},
		{name: "../" + escape, body: []byte("pwned")},
	})
	root := t.TempDir()
	assertBundleRefused(t, root, serveRunBundle(t, body), body, ReasonBundlePathRejected)
	escaped := filepath.Join(root, "..", escape)
	if _, err := os.Stat(escaped); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dotdot entry wrote %s: %v", escaped, err)
	}
	t.Cleanup(func() { _ = os.Remove(escaped) })
}

func TestRunBundle_SymlinkRefused(t *testing.T) {
	body := mustTar(t, []tarEntry{
		{name: "app/ok.txt", body: []byte("ok")},
		{name: "app/link", symlink: "/etc/passwd"},
	})
	root := t.TempDir()
	assertBundleRefused(t, root, serveRunBundle(t, body), body, ReasonBundleLinkRejected)
	if _, err := os.Lstat(filepath.Join(root, "app", "link")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink was created: %v", err)
	}
}

func TestRunBundle_OversizedExpansionRefused(t *testing.T) {
	body := mustTar(t, []tarEntry{
		{name: "app/a.txt", body: bytes.Repeat([]byte("a"), 10)},
		{name: "app/b.txt", body: bytes.Repeat([]byte("b"), 10)},
	})
	root := t.TempDir()
	srv := serveRunBundle(t, body)
	var started bool
	var buf bytes.Buffer
	err := StartAfterBundle(context.Background(), RunBundleConfig{
		URL:              srv.URL,
		SHA256:           sha256Hex(string(body)),
		Size:             int64(len(body)),
		Root:             root,
		MaxUnpackedBytes: 16,
	}, &buf, func() error {
		started = true
		return nil
	})
	assertRefused(t, root, err, &buf, ReasonBundleCapExceeded, started)
}

func TestRunBundle_ShortBodyDespiteContentLengthRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		short := body[:len(body)-1]
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(body))
		_, _ = conn.Write(short)
	}()

	root := t.TempDir()
	var started bool
	var out bytes.Buffer
	err = StartAfterBundle(context.Background(), RunBundleConfig{
		URL:    "http://" + ln.Addr().String() + "/bundle",
		SHA256: sha256Hex(string(body)),
		Size:   int64(len(body)),
		Root:   root,
	}, &out, func() error {
		started = true
		return nil
	})
	assertRefused(t, root, err, &out, ReasonBundleSizeMismatch, started)
}

func TestRunBundle_ShortDownloadRefused(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	short := body[:len(body)-1]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(short)))
		_, _ = w.Write(short)
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	var started bool
	var buf bytes.Buffer
	err := StartAfterBundle(context.Background(), RunBundleConfig{
		URL:    srv.URL,
		SHA256: sha256Hex(string(body)),
		Size:   int64(len(body)),
		Root:   root,
	}, &buf, func() error {
		started = true
		return nil
	})
	assertRefused(t, root, err, &buf, ReasonBundleSizeMismatch, started)
}

func TestRunBundle_RedirectNotFollowed(t *testing.T) {
	body := goodRunBundleTar(t, bundleAgentSource)
	var targetHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/bundle", func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/bundle", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	root := t.TempDir()
	var started bool
	var buf bytes.Buffer
	err := StartAfterBundle(context.Background(), RunBundleConfig{
		URL:    srv.URL + "/start",
		SHA256: sha256Hex(string(body)),
		Size:   int64(len(body)),
		Root:   root,
	}, &buf, func() error {
		started = true
		return nil
	})
	assertRefused(t, root, err, &buf, ReasonBundleRedirectRefused, started)
	if targetHits != 0 {
		t.Fatalf("redirect target hits = %d, want 0", targetHits)
	}
}

func TestRunBundle_NoValuesPathUnchanged(t *testing.T) {
	t.Setenv(EnvBundleURL, "")
	t.Setenv(EnvBundleSHA256, "")
	t.Setenv(EnvBundleSize, "")

	root := t.TempDir()
	agentPath := filepath.Join(root, "app", "agent.py")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const source = "def invoke(payload):\n    return payload\n"
	if err := os.WriteFile(agentPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}

	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	if err := LoadRunBundleFromEnv(context.Background(), &buf); err != nil {
		t.Fatalf("unset bundle values: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("unset path wrote %q", buf.String())
	}

	var started bool
	err = StartAfterBundle(context.Background(), RunBundleConfig{}, &buf, func() error {
		started = true
		worker := NewServer(Config{AgentPath: agentPath})
		t.Cleanup(func() { _ = worker.Close() })
		waitHarnessReady(t, worker)
		req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"n":1}`))
		rec := httptest.NewRecorder()
		worker.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"n"`) {
			t.Fatalf("no-values invoke = %d %s", rec.Code, rec.Body.String())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StartAfterBundle with no values: %v", err)
	}
	if !started {
		t.Fatal("no-values path did not start the worker")
	}
	if fetches != 0 {
		t.Fatalf("no-values path fetched %d times", fetches)
	}
	after, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("agent bytes changed on the no-values path")
	}
	if _, err := os.Stat(filepath.Join(root, BundleManifestRel)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest written on the no-values path: %v", err)
	}
}

func TestRunBundle_DigestComesFromCallerNotBundle(t *testing.T) {
	const planted = "sha256=0000000000000000000000000000000000000000000000000000000000000000\n"
	body := mustTar(t, []tarEntry{
		{name: "app/agent.py", body: []byte(bundleAgentSource)},
		{name: "app/expected.sha256", body: []byte(planted)},
		{name: "agentpaas/requirements.lock", body: []byte(planted)},
	})
	root := t.TempDir()
	var buf bytes.Buffer
	err := LoadRunBundle(context.Background(), runBundleCfg(serveRunBundle(t, body).URL, body, root), &buf)
	if err != nil {
		t.Fatalf("caller digest should verify despite planted digest text: %v %q", err, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(root, "agentpaas", "requirements.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != planted {
		t.Fatalf("lock bytes = %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(root, BundleManifestRel))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(sha256Hex(string(body)))) {
		t.Fatalf("manifest did not record the caller digest: %s", raw)
	}
	if bytes.Contains(raw, []byte("0000000000000000000000000000000000000000000000000000000000000000")) {
		t.Fatalf("manifest took the digest from bundle bytes: %s", raw)
	}
}

func TestRunBundle_PartialValuesDoNotStartWorker(t *testing.T) {
	t.Setenv(EnvBundleURL, "http://127.0.0.1:1/bundle")
	t.Setenv(EnvBundleSHA256, "")
	t.Setenv(EnvBundleSize, "")
	var envBuf bytes.Buffer
	if err := LoadRunBundleFromEnv(context.Background(), &envBuf); err == nil {
		t.Fatal("expected refusal from the environment")
	}
	assertOneReason(t, &envBuf, ReasonBundleValuesIncomplete)

	var started bool
	var buf bytes.Buffer
	err := StartAfterBundle(context.Background(), RunBundleConfig{
		URL:  "http://127.0.0.1:1/bundle",
		Root: t.TempDir(),
	}, &buf, func() error {
		started = true
		return nil
	})
	if started {
		t.Fatal("partial bundle values started the worker")
	}
	assertOneReason(t, &buf, ReasonBundleValuesIncomplete)
	if err == nil {
		t.Fatal("expected refusal")
	}
}

func TestRunBundleCaps(t *testing.T) {
	if defaultMaxRunBundleFile != 256*1024*1024 {
		t.Fatalf("file cap = %d", defaultMaxRunBundleFile)
	}
	if defaultMaxRunBundleTotal != 2*1024*1024*1024 {
		t.Fatalf("total cap = %d", defaultMaxRunBundleTotal)
	}
}

type tarEntry struct {
	name    string
	body    []byte
	symlink string
}

func goodRunBundleTar(t *testing.T, agent string) []byte {
	t.Helper()
	return mustTar(t, []tarEntry{
		{name: "app/", body: nil},
		{name: "app/agent.py", body: []byte(agent)},
		{name: "app/deps/", body: nil},
		{name: "app/deps/idna/", body: nil},
		{name: "app/deps/idna/__init__.py", body: []byte("# dep\n")},
		{name: "agentpaas/", body: nil},
		{name: "agentpaas/requirements.lock", body: []byte("idna==3.7\n")},
	})
}

func mustTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, ent := range entries {
		hdr := &tar.Header{
			Name:    ent.name,
			Uid:     0,
			Gid:     0,
			ModTime: time.Unix(0, 0).UTC(),
			Format:  tar.FormatUSTAR,
		}
		switch {
		case strings.HasSuffix(ent.name, "/"):
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
		case ent.symlink != "":
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = ent.symlink
			hdr.Mode = 0o777
		default:
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = 0o644
			hdr.Size = int64(len(ent.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", ent.name, err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write(ent.body); err != nil {
				t.Fatalf("write body %s: %v", ent.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serveRunBundle(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runBundleCfg(url string, body []byte, root string) RunBundleConfig {
	return RunBundleConfig{
		URL:    url,
		SHA256: sha256Hex(string(body)),
		Size:   int64(len(body)),
		Root:   root,
	}
}

func assertBundleRefused(t *testing.T, root string, srv *httptest.Server, good []byte, reason string) {
	t.Helper()
	var started bool
	var buf bytes.Buffer
	err := StartAfterBundle(context.Background(), runBundleCfg(srv.URL, good, root), &buf, func() error {
		started = true
		return nil
	})
	assertRefused(t, root, err, &buf, reason, started)
}

func assertRefused(t *testing.T, root string, err error, buf *bytes.Buffer, reason string, started bool) {
	t.Helper()
	if err == nil {
		t.Fatal("expected refusal")
	}
	if started {
		t.Fatal("worker started after refusal")
	}
	assertNothingWritten(t, root)
	assertOneReason(t, buf, reason)
}

func assertNothingWritten(t *testing.T, root string) {
	t.Helper()
	app := filepath.Join(root, "app")
	entries, err := os.ReadDir(app)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read /app: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("/app not empty: %v", entries)
	}
	lock := filepath.Join(root, "agentpaas", "requirements.lock")
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock path written: %v", err)
	}
}

func assertOneReason(t *testing.T, buf *bytes.Buffer, reason string) {
	t.Helper()
	text := buf.String()
	if text == "" || !strings.HasSuffix(text, "\n") {
		t.Fatalf("want one reason line, got %q", text)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one reason line, got %q", text)
	}
	var got ErrorResponse
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("reason line is not JSON: %v %q", err, text)
	}
	if got.Status != "FAILED" || got.Reason != reason {
		t.Fatalf("reason line = %+v, want reason %s", got, reason)
	}
}

func waitHarnessReady(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}
