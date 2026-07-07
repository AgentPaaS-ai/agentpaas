package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

type provenanceTestImageBuilder struct{}

func (provenanceTestImageBuilder) Build(ctx context.Context, sourceDir, agentName string) (string, error) {
	return "sha256:deadbeef0000000000000000000000000000000000000000000000000000000001", nil
}

type provenanceMaterialized struct {
	homeDir string
	ref     string
	pubName string
	pubFP   string
}

func materializeWeatherAgentForProvenance(t *testing.T) provenanceMaterialized {
	t.Helper()
	homeDir := t.TempDir()
	stateRoot := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", []byte("name: weather-agent\nversion: 1.0.0\nruntime: python\nentry: main.py\n"))
	writeFile(t, dir, "main.py", []byte("print('ok')\n"))
	policy := []byte(`version: "1.0"
agent:
  name: weather-agent
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "key"
    type: header
    header: "X-API-Key"
    value: "x"
`)
	sbom := []byte(`{"spdxVersion":"SPDX-2.3","name":"weather","packages":[{"name":"idna","SPDXID":"SPDXRef-Package-idna"}]}`)

	aidKey := detKey(t, "weather-aid")
	pubKey := detKey(t, "weather-pub")
	buildDigest, err := pack.ComputeBuildInputDigest(dir, nil)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	polDigest, err := pack.ComputePolicyDigest(policy)
	if err != nil {
		t.Fatalf("policy digest: %v", err)
	}
	aidPEM, _ := pubPEMBytes(&aidKey.PublicKey)
	now := time.Unix(0, 0).UTC()
	lock := &pack.AgentLock{
		SchemaVersion:        pack.LockSchemaVersion,
		AgentName:            "weather-agent",
		AgentVersion:         "1.0.0",
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:0000000000000000000000000000000000000000000000000000000000000001",
		HarnessVersion:       "test",
		BuildInputDigest:     buildDigest,
		ImageDigest:          sha256HexBytes([]byte("img")),
		SBOMDigest:           sha256HexBytes(sbom),
		PolicyDigest:         polDigest,
		PackageAID:           string(aidPEM),
		PublicKeyFingerprint: pack.PublicKeyFingerprint(&aidKey.PublicKey),
		Reproducibility: pack.ReproducibilityMeta{
			SourceDateEpoch: now,
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: now,
	}
	pubPEM, _ := pubPEMBytes(&pubKey.PublicKey)
	fp := identity.PublisherFingerprint(&pubKey.PublicKey)
	lock.Publisher = &pack.PublisherInfo{
		Name: "weather-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM), SignedAt: now,
	}
	entry := pack.ProvenanceEntry{
		Action: "created", PublisherFingerprint: fp, PublisherName: "weather-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: lock.AgentVersion,
		Timestamp: now,
	}
	if err := pack.SignProvenanceEntryWithKey(&entry, pubKey); err != nil {
		t.Fatalf("sign entry: %v", err)
	}
	lock.Provenance = []pack.ProvenanceEntry{entry}
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("sign lock: %v", err)
	}
	if err := pack.SignPublisherWithKey(lock, pubKey); err != nil {
		t.Fatalf("sign publisher: %v", err)
	}
	manifest := &bundle.Manifest{
		BundleSchemaVersion: bundle.BundleSchemaVersion,
		Publisher: bundle.ManifestPublisherInfo{
			Name: "weather-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM),
		},
		CreatedAt: now,
	}
	bundlePath := filepath.Join(t.TempDir(), "weather.agentpaas")
	if _, err := bundle.WriteToFile(bundle.BundleConfig{
		ProjectDir: dir, Manifest: manifest, Lock: lock, PolicyYAML: policy, SBOM: sbom,
		PublisherKey: pubKey, SourceDateEpoch: now,
	}, bundlePath); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	b, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	res, err := install.MaterializeInstall(context.Background(), install.MaterializeOpts{
		StateRoot:         stateRoot,
		Bundle:            b,
		Manifest: install.InstallManifest{
			PublisherFingerprint: fp,
			PublisherName:        "weather-publisher",
			AgentName:            "weather-agent",
			AgentVersion:         "1.0.0",
			AcceptedPolicyDigest: polDigest,
		},
		AllowUnlockedDeps: true,
		Builder:           provenanceTestImageBuilder{},
	})
	if err != nil {
		t.Fatalf("MaterializeInstall: %v", err)
	}
	return provenanceMaterialized{homeDir: homeDir, ref: res.AgentRef, pubName: "weather-publisher", pubFP: fp}
}

func executeProvenanceShow(t *testing.T, homeDir string, args ...string) (string, string, error) {
	t.Helper()
	full := append([]string{"provenance", "show"}, args...)
	full = append(full, "--home", homeDir)
	return executeCmd(full...)
}

func TestProvenanceShow_InstalledGoldenOneEntry(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	out, _, err := executeProvenanceShow(t, fix.homeDir, fix.ref)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "Provenance:") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "created") || !strings.Contains(out, "weather-agent") || !strings.Contains(out, "1.0.0") {
		t.Fatalf("missing chain fields: %q", out)
	}
	if !strings.Contains(out, fix.pubName) {
		t.Fatalf("missing publisher: %q", out)
	}
	pub8 := strings.ToLower(fix.pubFP[:8])
	if !strings.Contains(out, pub8) {
		t.Fatalf("missing fingerprint prefix %q in %q", pub8, out)
	}
}

func TestProvenanceShow_InstalledJSON(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	out, _, err := executeProvenanceShow(t, fix.homeDir, fix.ref, "--json")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var report pack.ProvenanceReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if !report.Verified || len(report.Entries) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Entries[0].Action != "created" || report.Entries[0].AgentName != "weather-agent" {
		t.Fatalf("entry = %+v", report.Entries[0])
	}
	if report.ChainSemantics == "" {
		t.Fatal("missing chain_semantics")
	}
	// Stable re-marshal
	again, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var report2 pack.ProvenanceReport
	if err := json.Unmarshal(again, &report2); err != nil {
		t.Fatal(err)
	}
}

func TestProvenanceShow_BundlePath(t *testing.T) {
	bundlePath := writeCLITestBundle(t)
	out, _, err := executeProvenanceShow(t, t.TempDir(), bundlePath)
	if err != nil {
		t.Fatalf("show bundle: %v", err)
	}
	if !strings.Contains(out, "Provenance:") || !strings.Contains(out, "created") {
		t.Fatalf("out = %q", out)
	}
	b, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	vr, err := bundle.Verify(b)
	if err != nil {
		t.Fatal(err)
	}
	ir, err := bundle.Inspect(bundlePath, b, vr)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(ir.ProvenanceText) {
		t.Fatalf("bundle provenance mismatch:\nwant %q\ngot %q", ir.ProvenanceText, out)
	}
}

func TestProvenanceShow_NonexistentRef(t *testing.T) {
	homeDir := t.TempDir()
	_ = home.NewHomePaths(homeDir)
	stateDir := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	out, _, err := executeProvenanceShow(t, homeDir, "missing-agent@deadbeef")
	if err == nil {
		t.Fatalf("want error, out=%q", out)
	}
	if strings.Contains(out, "Provenance:") {
		t.Fatalf("must not render on error: %q", out)
	}
}

func TestProvenanceShow_Phase1BareName(t *testing.T) {
	homeDir := t.TempDir()
	stateRoot := filepath.Join(homeDir, "state")
	agentDir := filepath.Join(stateRoot, "agents", "legacy-local")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := detKey(t, "phase1")
	pubPEM, _ := pubPEMBytes(&key.PublicKey)
	now := time.Unix(0, 0).UTC()
	lock := &pack.AgentLock{
		SchemaVersion:        1,
		AgentName:            "legacy-local",
		AgentVersion:         "0.0.1",
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:0000000000000000000000000000000000000000000000000000000000000001",
		HarnessVersion:       "test",
		BuildInputDigest:     sha256HexBytes([]byte("src")),
		ImageDigest:          sha256HexBytes([]byte("img")),
		SBOMDigest:           sha256HexBytes([]byte("sbom")),
		PolicyDigest:         sha256HexBytes([]byte("pol")),
		PackageAID:           string(pubPEM),
		PublicKeyFingerprint: pack.PublicKeyFingerprint(&key.PublicKey),
		CreatedAt:            now,
	}
	if err := pack.SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	lockPath := filepath.Join(agentDir, "agent.lock")
	if err := pack.WriteAgentLock(lock, lockPath); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	out, _, err := executeProvenanceShow(t, homeDir, "legacy-local")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "Provenance: (none)") {
		t.Fatalf("want local-only provenance, got %q", out)
	}
}