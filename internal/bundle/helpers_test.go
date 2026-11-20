package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

const testPolicyYAML = `version: "1.0"
agent:
  name: bundle-test-agent
  description: "Bundle test"
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "${env:KEY}"
`

const testSBOMJSON = `{"spdxVersion":"SPDX-2.3","name":"bundle-test"}`

// goldenBundleDigest is intentionally empty: the first manifest/lock ECDSA signatures
// are non-deterministic across runs. Idempotent Write with a fixed cfg is asserted in
// TestWriteDeterministicGolden.
const goldenBundleDigest = ""

type seededReader struct {
	seed []byte
	i    int
}

func (r *seededReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.seed[r.i%len(r.seed)]
		r.i++
	}
	return len(p), nil
}

func deterministicPrivateKey(t *testing.T, label string) *ecdsa.PrivateKey {
	t.Helper()
	h := sha256.Sum256([]byte("b22-t01-bundle-fixture:" + label))
	key, err := ecdsa.GenerateKey(elliptic.P256(), &seededReader{seed: h[:]})
	if err != nil {
		t.Fatalf("deterministic key %q: %v", label, err)
	}
	return key
}

func testSourceDateEpoch() time.Time {
	return time.Unix(0, 0).UTC()
}

func writeProjectFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func publicKeyPEM(pub *ecdsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func newTestKeys(t *testing.T) (aidKey, publisherKey *ecdsa.PrivateKey) {
	t.Helper()
	return deterministicPrivateKey(t, "aid"), deterministicPrivateKey(t, "publisher")
}

func buildTestLock(t *testing.T, projectDir string, policyYAML, sbom []byte, aidKey *ecdsa.PrivateKey) *pack.AgentLock {
	t.Helper()
	buildDigest, err := pack.ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest: %v", err)
	}
	policyDigest, err := pack.ComputePolicyDigest(policyYAML)
	if err != nil {
		t.Fatalf("ComputePolicyDigest: %v", err)
	}
	pubPEM, err := publicKeyPEM(&aidKey.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	lock := &pack.AgentLock{
		SchemaVersion:        pack.LockSchemaVersion,
		AgentName:            "bundle-test-agent",
		AgentVersion:         "0.1.0",
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:0000000000000000000000000000000000000000000000000000000000000001",
		HarnessVersion:       "test",
		BuildInputDigest:     buildDigest,
		ImageDigest:          sha256Hex([]byte("image-seed")),
		SBOMDigest:           sha256Hex(sbom),
		PolicyDigest:         policyDigest,
		PackageAID:           string(pubPEM),
		PublicKeyFingerprint: pack.PublicKeyFingerprint(&aidKey.PublicKey),
		Reproducibility: pack.ReproducibilityMeta{
			SourceDateEpoch: testSourceDateEpoch(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: testSourceDateEpoch(),
	}
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("SignLockfileWithKey: %v", err)
	}
	return lock
}

func buildTestManifest(t *testing.T, publisherKey *ecdsa.PrivateKey) *Manifest {
	t.Helper()
	pubPEM, err := publicKeyPEM(&publisherKey.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	return &Manifest{
		BundleSchemaVersion: BundleSchemaVersion,
		Publisher: ManifestPublisherInfo{
			Name:         "test-publisher",
			Fingerprint:  identity.PublisherFingerprint(&publisherKey.PublicKey),
			PublicKeyPEM: string(pubPEM),
		},
		CreatedAt: testSourceDateEpoch(),
	}
}

func writeTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeProjectFile(t, dir, "agent.yaml", []byte("name: bundle-test-agent\nversion: 0.1.0\nruntime: python\nentry: main.py\n"))
	writeProjectFile(t, dir, "main.py", []byte("print('bundle-test')\n"))
	writeProjectFile(t, dir, "requirements.txt", []byte("idna==3.7\n"))
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

type testBundleFixture struct {
	Path         string
	ProjectDir   string
	PublisherKey *ecdsa.PrivateKey
	Config       BundleConfig
}

func writeTestBundle(t *testing.T, withImage bool) testBundleFixture {
	t.Helper()
	projectDir := writeTestProject(t)
	aidKey, publisherKey := newTestKeys(t)
	policyYAML := []byte(testPolicyYAML)
	sbom := []byte(testSBOMJSON)
	lock := buildTestLock(t, projectDir, policyYAML, sbom, aidKey)
	cfg := BundleConfig{
		ProjectDir:      projectDir,
		Manifest:        buildTestManifest(t, publisherKey),
		Lock:            lock,
		PolicyYAML:      policyYAML,
		SBOM:            sbom,
		PublisherKey:    publisherKey,
		SourceDateEpoch: testSourceDateEpoch(),
	}
	if withImage {
		imageDir := filepath.Join(t.TempDir(), "oci")
		writeProjectFile(t, imageDir, "index.json", []byte(`{"schemaVersion":2,"manifests":[{"platform":{"os":"linux","architecture":"arm64"}}]}`))
		cfg.ImageDir = imageDir
	}
	outPath := filepath.Join(t.TempDir(), "test.agentpaas")
	result, err := WriteToFile(cfg, outPath)
	if err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	if result.BundleDigest == "" {
		t.Fatal("empty bundle digest")
	}
	return testBundleFixture{Path: outPath, ProjectDir: projectDir, PublisherKey: publisherKey, Config: cfg}
}

func checkFailed(t *testing.T, report *VerifyReport, name string) {
	t.Helper()
	for _, c := range report.Checks {
		if c.Name == name {
			if c.Passed {
				t.Fatalf("check %q passed, want failure", name)
			}
			return
		}
	}
	t.Fatalf("check %q not found in report", name)
}

func readBundleTar(t *testing.T, path string) []tarEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var entries []tarEntry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		entries = append(entries, tarEntry{name: hdr.Name, hdr: hdr, body: body})
	}
	return entries
}

func writeRawTarFile(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		hdr := *e.hdr
		hdr.Name = e.name
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("header: %v", err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("body: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := os.WriteFile(path, tarBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tar: %v", err)
	}
}

func writeBundleTarFile(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		hdr := *e.hdr
		hdr.Name = e.name
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("header: %v", err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("body: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	var gzBuf bytes.Buffer
	if err := writeDeterministicGzip(&gzBuf, tarBuf.Bytes()); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := os.WriteFile(path, gzBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func mutateEntryBody(t *testing.T, srcPath, dstPath, entryName string, mutator func([]byte) []byte) {
	t.Helper()
	entries := readBundleTar(t, srcPath)
	found := false
	for i := range entries {
		if entries[i].name == entryName {
			entries[i].body = mutator(entries[i].body)
			entries[i].hdr.Size = int64(len(entries[i].body))
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("entry %q not found", entryName)
	}
	writeBundleTarFile(t, dstPath, entries)
}

func cloneBundleWithEntries(t *testing.T, srcPath, dstPath string, extra []tarEntry, remove map[string]bool) {
	t.Helper()
	entries := readBundleTar(t, srcPath)
	var out []tarEntry
	for _, e := range entries {
		if remove[e.name] {
			continue
		}
		out = append(out, e)
	}
	out = append(out, extra...)
	writeBundleTarFile(t, dstPath, out)
}

func manifestDigestFieldPatch(t *testing.T, manifestJSON []byte, field string, badDigest string) []byte {
	t.Helper()
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	switch field {
	case "policy":
		m.Contents.Policy.Digest = badDigest
	case "lock":
		m.Contents.Lock.Digest = badDigest
	default:
		t.Fatalf("unknown field %q", field)
	}
	out, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return out
}