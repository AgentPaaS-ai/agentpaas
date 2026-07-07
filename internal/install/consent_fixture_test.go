package install

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

const consentBasePolicyYAML = `version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "x"
`

const consentSBOM = `{"spdxVersion":"SPDX-2.3","name":"consent-test","packages":[{"name":"idna","SPDXID":"SPDXRef-Package-idna"}]}`

type consentBundleFixture struct {
	Path           string
	PolicyDigest   string
	PolicyYAML     []byte
	PublisherFP    string
	PublisherName  string
	AgentName      string
	AgentVersion   string
	InspectReport  *bundle.InspectReport
}

func writeConsentFixtureBundle(t *testing.T, policyYAML []byte, agentVersion string) consentBundleFixture {
	t.Helper()
	if policyYAML == nil {
		policyYAML = []byte(consentBasePolicyYAML)
	}
	if agentVersion == "" {
		agentVersion = "0.1.0"
	}
	dir := t.TempDir()
	writeConsentProjectFile(t, dir, "agent.yaml", []byte("name: consent-test-agent\nversion: "+agentVersion+"\nruntime: python\nentry: main.py\n"))
	writeConsentProjectFile(t, dir, "main.py", []byte("print('ok')\n"))
	sbom := []byte(consentSBOM)
	aidKey := consentDetKey(t, "aid")
	pubKey := consentDetKey(t, "pub")
	buildDigest, err := pack.ComputeBuildInputDigest(dir, nil)
	if err != nil {
		t.Fatalf("build digest: %v", err)
	}
	polDigest, err := pack.ComputePolicyDigest(policyYAML)
	if err != nil {
		t.Fatalf("policy digest: %v", err)
	}
	aidPEM, _ := consentPubPEM(&aidKey.PublicKey)
	lock := &pack.AgentLock{
		SchemaVersion:        pack.LockSchemaVersion,
		AgentName:            "consent-test-agent",
		AgentVersion:         agentVersion,
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:0000000000000000000000000000000000000000000000000000000000000001",
		HarnessVersion:       "test",
		BuildInputDigest:     buildDigest,
		ImageDigest:          consentSHA256([]byte("img")),
		SBOMDigest:           consentSHA256(sbom),
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
	pubPEM, _ := consentPubPEM(&pubKey.PublicKey)
	fp := identity.PublisherFingerprint(&pubKey.PublicKey)
	now := time.Unix(0, 0).UTC()
	lock.Publisher = &pack.PublisherInfo{
		Name: "consent-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM), SignedAt: now,
	}
	entry := pack.ProvenanceEntry{
		Action: "created", PublisherFingerprint: fp, PublisherName: "consent-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: lock.AgentVersion,
		Timestamp: now,
	}
	if err := pack.SignProvenanceEntryWithKey(&entry, pubKey); err != nil {
		t.Fatalf("sign provenance: %v", err)
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
			Name: "consent-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM),
		},
		CreatedAt: now,
	}
	out := filepath.Join(t.TempDir(), "consent.agentpaas")
	if _, err := bundle.WriteToFile(bundle.BundleConfig{
		ProjectDir: dir, Manifest: manifest, Lock: lock, PolicyYAML: policyYAML, SBOM: sbom,
		PublisherKey: pubKey, SourceDateEpoch: now,
	}, out); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	b, err := bundle.Open(out)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()
	vr, err := bundle.Verify(b)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !vr.Verified {
		t.Fatal("bundle not verified")
	}
	report, err := bundle.Inspect(out, b, vr)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	return consentBundleFixture{
		Path: out, PolicyDigest: polDigest, PolicyYAML: policyYAML, PublisherFP: fp,
		PublisherName: "consent-publisher", AgentName: "consent-test-agent", AgentVersion: agentVersion,
		InspectReport: report,
	}
}

func writeConsentProjectFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

type consentSeeded struct {
	seed []byte
	i    int
}

func (r *consentSeeded) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.seed[r.i%len(r.seed)]
		r.i++
	}
	return len(p), nil
}

func consentDetKey(t *testing.T, label string) *ecdsa.PrivateKey {
	t.Helper()
	consentKeyMu.Lock()
	defer consentKeyMu.Unlock()
	if consentKeyCache == nil {
		consentKeyCache = make(map[string]*ecdsa.PrivateKey)
	}
	if k, ok := consentKeyCache[label]; ok {
		return k
	}
	h := sha256.Sum256([]byte("consent-fixture:" + label))
	key, err := ecdsa.GenerateKey(elliptic.P256(), &consentSeeded{seed: h[:]})
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	consentKeyCache[label] = key
	return key
}

var (
	consentKeyMu    sync.Mutex
	consentKeyCache map[string]*ecdsa.PrivateKey
)

func consentPubPEM(pub *ecdsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func consentSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return fmtHex(h[:])
}

func fmtHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func newConsentState(t *testing.T) (*FileInstallState, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	return &FileInstallState{StateRoot: root}, root
}

func assertNoStateGrowth(t *testing.T, root string, before int) {
	t.Helper()
	after, err := countFilesUnder(root)
	if err != nil {
		t.Fatalf("count files: %v", err)
	}
	if after != before {
		t.Fatalf("state mutation on decline: before=%d after=%d", before, after)
	}
}

func stateFileCount(t *testing.T, root string) int {
	t.Helper()
	n, err := countFilesUnder(root)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}