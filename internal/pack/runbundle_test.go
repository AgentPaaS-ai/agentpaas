package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunBundleTwoPacksSameTarSHA256(t *testing.T) {
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	}, nil)

	sum1 := writeRunBundleSHA(t, project, lockPath, image)
	if err := os.Remove(filepath.Join(filepath.Dir(lockPath), RunBundleName)); err != nil {
		t.Fatalf("remove first bundle: %v", err)
	}
	sum2 := writeRunBundleSHA(t, project, lockPath, image)
	if sum1 != sum2 {
		t.Fatalf("two packs of one source differ: %s != %s", sum1, sum2)
	}
}

func TestRunBundleEntriesMatchImageBytes(t *testing.T) {
	files := map[string][]byte{
		"main.py":                          []byte("print('from-image')\n"),
		"pkg/util.py":                      []byte("VALUE = 1\n"),
		"python/agentpaas_sdk/__init__.py": []byte("# sdk-from-image\n"),
		"deps/idna/__init__.py":            []byte("# dep-from-image\n"),
	}
	// Host project bytes differ from the image. The bundle must follow the image.
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py":     []byte("print('from-host')\n"),
		"pkg/util.py": []byte("VALUE = 0\n"),
	}, files)

	writeRunBundleSHA(t, project, lockPath, image)
	entries := readTarMap(t, mustRead(t, filepath.Join(filepath.Dir(lockPath), RunBundleName)))
	want := map[string][]byte{
		"app/main.py":                          files["main.py"],
		"app/pkg/util.py":                      files["pkg/util.py"],
		"app/python/agentpaas_sdk/__init__.py": files["python/agentpaas_sdk/__init__.py"],
		"app/deps/idna/__init__.py":            files["deps/idna/__init__.py"],
		"agentpaas/requirements.lock":          []byte("idna==3.7\n"),
	}
	for name, body := range want {
		got, ok := entries[name]
		if !ok {
			t.Errorf("tar missing %s", name)
			continue
		}
		if !bytes.Equal(got, body) {
			t.Errorf("%s bytes = %q, want image bytes %q", name, got, body)
		}
	}
	for name, hdr := range readTarHeaders(t, mustRead(t, filepath.Join(filepath.Dir(lockPath), RunBundleName))) {
		if hdr.Uid != 0 || hdr.Gid != 0 {
			t.Errorf("%s uid/gid = %d/%d, want 0/0", name, hdr.Uid, hdr.Gid)
		}
		if !hdr.ModTime.Equal(time.Unix(0, 0).UTC()) {
			t.Errorf("%s mtime = %s, want SOURCE_DATE_EPOCH", name, hdr.ModTime)
		}
	}
}

func TestRunBundleNoEntryOutsideThreeRoots(t *testing.T) {
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	}, map[string][]byte{
		"main.py":                          []byte("print('hello')\n"),
		"evil.txt":                         []byte("not-a-root\n"),
		"python/agentpaas_sdk/__init__.py": []byte("# sdk\n"),
		"deps/idna/__init__.py":            []byte("# dep\n"),
	})
	// Outside the three roots: harness, /etc, and an extra /app file.
	image = appendImageFiles(t, image, []imageFile{
		{name: "agentpaas/harness", body: []byte("harness-binary")},
		{name: "etc/passwd", body: []byte("root:x:0:0:root\n")},
		{name: "usr/bin/python", body: []byte("#!/usr/bin/python\n")},
	})

	writeRunBundleSHA(t, project, lockPath, image)
	for name := range readTarMap(t, mustRead(t, filepath.Join(filepath.Dir(lockPath), RunBundleName))) {
		if runBundleEntryOutsideRoots(name) {
			t.Errorf("tar entry %s is outside the three roots", name)
		}
	}
}

func TestRunBundleProjectSymlinkOutsideRefused(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("symlink semantics differ on this OS")
	}
	project := t.TempDir()
	writeBuildTestFile(t, project, "main.py", []byte("print('hello')\n"))
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("SENTINEL_EXPORT_SECRET\n"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "leak.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	lockDir := t.TempDir()
	lockPath := filepath.Join(lockDir, "agent.lock")
	err := WriteRunBundle(context.Background(), RunBundleConfig{
		ProjectDir:      project,
		Platform:        linuxAMD64Target,
		LockPath:        lockPath,
		ImageRef:        "agentpaas/test:bundle",
		SourceDateEpoch: time.Unix(0, 0).UTC(),
		imageTar:        bytes.NewReader(minimalImageTar(t)),
	})
	if err == nil {
		t.Fatal("project symlink pointing outside was accepted")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("error = %v, want outside-symlink refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(lockDir, RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle written after refusal, stat err = %v", statErr)
	}
}

func TestRunBundleHostArchitecturePackWritesNoBundle(t *testing.T) {
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	}, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	})
	err := WriteRunBundle(context.Background(), RunBundleConfig{
		ProjectDir:      project,
		Platform:        "", // daemon leaves --target unset for a host pack
		LockPath:        lockPath,
		ImageRef:        "agentpaas/test:host",
		SourceDateEpoch: time.Unix(0, 0).UTC(),
		imageTar:        bytes.NewReader(image),
	})
	if err != nil {
		t.Fatalf("host pack error = %v, want nil", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("host-architecture pack wrote a bundle, stat err = %v", statErr)
	}

	err = WriteRunBundle(context.Background(), RunBundleConfig{
		ProjectDir:      project,
		Platform:        "linux/arm64",
		LockPath:        lockPath,
		ImageRef:        "agentpaas/test:arm64",
		SourceDateEpoch: time.Unix(0, 0).UTC(),
		imageTar:        bytes.NewReader(image),
	})
	if err != nil {
		t.Fatalf("non-amd64 pack error = %v, want nil", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("non-amd64 pack wrote a bundle, stat err = %v", statErr)
	}
}

func TestRunBundleHardlinkOutsideRefused(t *testing.T) {
	for _, link := range []string{"etc/passwd", "/etc/passwd"} {
		t.Run(link, func(t *testing.T) {
			project, lockPath, image := runBundleFixture(t, map[string][]byte{
				"main.py": []byte("print('hello')\n"),
			}, map[string][]byte{
				"main.py": []byte("print('hello')\n"),
			})
			image = appendImageFiles(t, image, []imageFile{
				{name: "etc/passwd", body: []byte("root:x:0:0:root\nSENTINEL_EXPORT_SECRET\n")},
				{name: "app/python/passwd", link: link, hard: true},
			})
			err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
			if err == nil {
				t.Fatal("hardlink to an outside member was accepted")
			}
			if !strings.Contains(err.Error(), "hardlink") {
				t.Fatalf("error = %v, want hardlink refusal", err)
			}
			bundlePath := filepath.Join(filepath.Dir(lockPath), RunBundleName)
			if _, statErr := os.Stat(bundlePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("bundle written after hardlink refusal, stat err = %v", statErr)
			}
		})
	}
}

func TestRunBundleInTreeHardlinkRewritten(t *testing.T) {
	body := []byte("# sdk-from-image\n")
	project := t.TempDir()
	writeBuildTestFile(t, project, "main.py", []byte("print('hello')\n"))
	lockPath := filepath.Join(t.TempDir(), "agent.lock")
	// The hardlink precedes its target. Its linkname is the archive member.
	image := tarBytes(t, []imageFile{
		{name: "app/main.py", body: []byte("print('hello')\n")},
		{name: "app/python/alias.py", link: "app/python/real.py", hard: true},
		{name: "app/python/real.py", body: body},
		{name: "agentpaas/requirements.lock", body: []byte("idna==3.7\n")},
	})
	writeRunBundleSHA(t, project, lockPath, image)
	data := mustRead(t, filepath.Join(filepath.Dir(lockPath), RunBundleName))
	entries := readTarMap(t, data)
	if !bytes.Equal(entries["app/python/alias.py"], body) {
		t.Fatalf("alias bytes = %q, want image bytes %q", entries["app/python/alias.py"], body)
	}
	for name, hdr := range readTarHeaders(t, data) {
		if hdr.Typeflag == tar.TypeLink {
			t.Errorf("%s stored a hardlink linkname %q", name, hdr.Linkname)
		}
	}
}

func TestRunBundleHardlinkToUnselectedAppFileRefused(t *testing.T) {
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	}, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	})
	image = appendImageFiles(t, image, []imageFile{
		{name: "app/evil.txt", body: []byte("not-a-root\n")},
		{name: "app/python/leak", link: "app/evil.txt", hard: true},
	})
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	if err == nil {
		t.Fatal("hardlink to an unselected /app file was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle written after unselected hardlink, stat err = %v", statErr)
	}
}

func TestRunBundleImageSymlinkOutsideRefused(t *testing.T) {
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	}, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	})
	image = appendImageFiles(t, image, []imageFile{
		{name: "app/python/leak", link: "/etc/passwd"},
	})
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	if err == nil {
		t.Fatal("image symlink pointing outside was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle written after image symlink refusal, stat err = %v", statErr)
	}
}

func TestRunBundleDotDotPathRefused(t *testing.T) {
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	}, map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	})
	image = appendImageFiles(t, image, []imageFile{
		{name: "app/python/../../etc/passwd", body: []byte("root:x:0:0:root\n")},
	})
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	if err == nil {
		t.Fatal(".. path in the image tar was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle written after .. refusal, stat err = %v", statErr)
	}
}

func TestRunBundleSecretScanCoversBundle(t *testing.T) {
	// Source is clean. The secret exists only in the image copy of /app.
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('clean')\n"),
	}, map[string][]byte{
		"main.py": []byte("token = \"SENTINEL_EXPORT_SECRET\"\n"),
	})
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	if err == nil {
		t.Fatal("secret in the image entered the run bundle")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v, want secret refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle written with a secret, stat err = %v", statErr)
	}
}

func TestRunBundleGitleaksUnavailableFailsClosed(t *testing.T) {
	// Source is clean. The image copy holds a key only gitleaks would catch.
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('clean')\n"),
	}, map[string][]byte{
		"main.py":         []byte("print('clean')\n"),
		"python/creds.py": []byte("key = \"" + testAWSKey + "\"\n"),
	})
	t.Setenv("PATH", t.TempDir()) // no gitleaks
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	if err == nil {
		t.Fatal("run bundle written without gitleaks (secret scan failed open)")
	}
	if !strings.Contains(err.Error(), "gitleaks") {
		t.Fatalf("error = %v, want gitleaks-unavailable refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(lockPath), RunBundleName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle written without a secret scan, stat err = %v", statErr)
	}
}

func TestRunBundleStoredBesideLock(t *testing.T) {
	home := t.TempDir()
	lock, key := signedTestLock(t)
	lock.Platform = linuxAMD64Target
	payload := []byte("deterministic-tar")
	sum := sha256.Sum256(payload)
	lock.BundleDigest = hex.EncodeToString(sum[:])
	lock.BundleSize = int64(len(payload))
	lock.RuntimeID = RuntimeIDPy311R1
	if err := SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	storeRunBundle(lock.ImageDigest, payload)
	if err := RecordDeployment(home, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}
	got := mustRead(t, filepath.Join(DeployedAgentPath(home, lock.AgentName), RunBundleName))
	if !bytes.Equal(got, payload) {
		t.Fatalf("bundle beside lock = %q, want stashed tar", got)
	}
	lockBytes := mustRead(t, filepath.Join(DeployedAgentPath(home, lock.AgentName), "agent.lock"))
	if bytes.Contains(lockBytes, payload) {
		t.Fatal("run bundle bytes were written into agent.lock")
	}
}

func TestScanSecretsCoversRunBundle(t *testing.T) {
	installMockGitleaks(t)
	project := t.TempDir()
	writeScanTestFile(t, project, "agent.py", "print('clean')\n")
	bundle := tarBytes(t, []imageFile{
		{name: "app/notes.txt", body: []byte("key = " + testAWSKey + "\n")},
	})
	if err := os.WriteFile(filepath.Join(project, RunBundleName), bundle, 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: project,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets: %v", err)
	}
	if !result.HasSecrets() {
		t.Fatal("ScanSecrets missed the secret inside the run bundle")
	}
	found := false
	for _, f := range result.BundleFindings {
		if strings.Contains(f.File, "notes.txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bundle findings = %#v, want notes.txt", result.BundleFindings)
	}
}

type imageFile struct {
	name string
	body []byte
	link string
	hard bool
}

func runBundleFixture(t *testing.T, projectFiles, imageApp map[string][]byte) (project, lockPath string, image []byte) {
	t.Helper()
	project = t.TempDir()
	for rel, body := range projectFiles {
		writeBuildTestFile(t, project, rel, body)
	}
	var files []imageFile
	if imageApp == nil {
		imageApp = projectFiles
	}
	for rel, body := range imageApp {
		files = append(files, imageFile{name: "app/" + rel, body: body})
	}
	files = append(files, imageFile{name: "agentpaas/requirements.lock", body: []byte("idna==3.7\n")})
	lockPath = filepath.Join(t.TempDir(), "agent.lock")
	return project, lockPath, tarBytes(t, files)
}

func runBundleConfig(project, lockPath string, image []byte) RunBundleConfig {
	return RunBundleConfig{
		ProjectDir:      project,
		Platform:        linuxAMD64Target,
		LockPath:        lockPath,
		ImageRef:        "agentpaas/test:bundle",
		SourceDateEpoch: time.Unix(0, 0).UTC(),
		imageTar:        bytes.NewReader(image),
	}
}

func writeRunBundleSHA(t *testing.T, project, lockPath string, image []byte) string {
	t.Helper()
	// The writer fails closed without gitleaks. Use the hermetic scanner stub.
	installMockGitleaks(t)
	if err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image)); err != nil {
		t.Fatalf("WriteRunBundle: %v", err)
	}
	data := mustRead(t, filepath.Join(filepath.Dir(lockPath), RunBundleName))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func minimalImageTar(t *testing.T) []byte {
	t.Helper()
	return tarBytes(t, []imageFile{
		{name: "app/main.py", body: []byte("print('hello')\n")},
		{name: "agentpaas/requirements.lock", body: []byte("idna==3.7\n")},
	})
}

func imageHeader(f imageFile) *tar.Header {
	hdr := &tar.Header{Name: f.name, Mode: 0o644, ModTime: time.Unix(1, 0)}
	switch {
	case f.hard:
		hdr.Typeflag = tar.TypeLink
		hdr.Linkname = f.link
		hdr.Size = 0
	case f.link != "":
		hdr.Typeflag = tar.TypeSymlink
		hdr.Linkname = f.link
		hdr.Size = 0
	default:
		hdr.Typeflag = tar.TypeReg
		hdr.Size = int64(len(f.body))
	}
	return hdr
}

func tarBytes(t *testing.T, files []imageFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		hdr := imageHeader(f)
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", f.name, err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write(f.body); err != nil {
				t.Fatalf("write body %s: %v", f.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func appendImageFiles(t *testing.T, existing []byte, extra []imageFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	tr := tar.NewReader(bytes.NewReader(existing))
	out := tar.NewWriter(&buf)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read existing tar: %v", err)
		}
		if err := out.WriteHeader(hdr); err != nil {
			t.Fatalf("copy header: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == 0 {
			if _, err := io.Copy(out, tr); err != nil {
				t.Fatalf("copy body: %v", err)
			}
		}
	}
	for _, f := range extra {
		hdr := imageHeader(f)
		if err := out.WriteHeader(hdr); err != nil {
			t.Fatalf("append header: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := out.Write(f.body); err != nil {
				t.Fatalf("append body: %v", err)
			}
		}
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close appended tar: %v", err)
	}
	return buf.Bytes()
}

func readTarMap(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(data))
	var prev string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if prev != "" && hdr.Name < prev {
			t.Fatalf("tar entries not sorted: %s then %s", prev, hdr.Name)
		}
		prev = hdr.Name
		if hdr.Typeflag == tar.TypeDir {
			out[hdr.Name] = nil
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = body
	}
}

func readTarHeaders(t *testing.T, data []byte) map[string]*tar.Header {
	t.Helper()
	out := map[string]*tar.Header{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		out[hdr.Name] = hdr
		if _, err := io.Copy(io.Discard, tr); err != nil {
			t.Fatalf("discard %s: %v", hdr.Name, err)
		}
	}
}

func runBundleEntryOutsideRoots(name string) bool {
	name = strings.TrimSuffix(name, "/")
	switch {
	case name == "agentpaas" || name == "agentpaas/requirements.lock":
		return false
	case name == "app" || name == "app/python" || strings.HasPrefix(name, "app/python/"):
		return false
	case name == "app/deps" || strings.HasPrefix(name, "app/deps/"):
		return false
	case name == "app/main.py" || name == "app/pkg" || name == "app/pkg/util.py":
		return false
	default:
		return true
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
