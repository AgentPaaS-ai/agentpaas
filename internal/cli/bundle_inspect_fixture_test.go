package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func writeCLITestBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", []byte("name: cli-bundle-test\nversion: 0.1.0\nruntime: python\nentry: main.py\n"))
	writeFile(t, dir, "main.py", []byte("print('ok')\n"))
	policy := []byte(`version: "1.0"
agent:
  name: cli-bundle-test
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "key"
    type: header
    header: "X-API-Key"
    value: "x"
`)
	sbom := []byte(`{"spdxVersion":"SPDX-2.3","name":"cli-test","packages":[{"name":"idna","SPDXID":"SPDXRef-Package-idna"}]}`)

	aidKey := detKey(t, "aid")
	pubKey := detKey(t, "pub")
	buildDigest, err := pack.ComputeBuildInputDigest(dir, nil)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	polDigest, err := pack.ComputePolicyDigest(policy)
	if err != nil {
		t.Fatalf("policy digest: %v", err)
	}
	aidPEM, _ := pubPEMBytes(&aidKey.PublicKey)
	lock := &pack.AgentLock{
		SchemaVersion:        pack.LockSchemaVersion,
		AgentName:            "cli-bundle-test",
		AgentVersion:         "0.1.0",
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
			SourceDateEpoch: time.Unix(0, 0).UTC(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: time.Unix(0, 0).UTC(),
	}
	pubPEM, _ := pubPEMBytes(&pubKey.PublicKey)
	fp := identity.PublisherFingerprint(&pubKey.PublicKey)
	now := time.Unix(0, 0).UTC()
	lock.Publisher = &pack.PublisherInfo{
		Name: "cli-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM), SignedAt: now,
	}
	entry := pack.ProvenanceEntry{
		Action: "created", PublisherFingerprint: fp, PublisherName: "cli-publisher",
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
			Name: "cli-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM),
		},
		CreatedAt: now,
	}
	out := filepath.Join(t.TempDir(), "cli.agentpaas")
	if _, err := bundle.WriteToFile(bundle.BundleConfig{
		ProjectDir: dir, Manifest: manifest, Lock: lock, PolicyYAML: policy, SBOM: sbom,
		PublisherKey: pubKey, SourceDateEpoch: now,
	}, out); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	return out
}

func writeFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

type seeded struct {
	seed []byte
	i    int
}

func (r *seeded) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.seed[r.i%len(r.seed)]
		r.i++
	}
	return len(p), nil
}

func detKey(t *testing.T, label string) *ecdsa.PrivateKey {
	t.Helper()
	h := sha256.Sum256([]byte("cli-bundle-fixture:" + label))
	key, err := ecdsa.GenerateKey(elliptic.P256(), &seeded{seed: h[:]})
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return key
}

func pubPEMBytes(pub *ecdsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}