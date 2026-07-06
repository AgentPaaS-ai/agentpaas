package export

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestExport_DirtySourceBlocked(t *testing.T) {
	home, project, ks := setupExportFixture(t)
	if err := os.WriteFile(filepath.Join(project, "agent.py"), []byte("print('dirty')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Config{
		Home: home, ProjectDir: project, OutputPath: filepath.Join(t.TempDir(), "out.agentpaas"),
		SkipConfirm: true, PublisherStore: ks,
	})
	if err == nil || !strings.Contains(err.Error(), "source changed") {
		t.Fatalf("want dirty error, got %v", err)
	}
}

func TestExport_DenylistBlocksEnv(t *testing.T) {
	home, project, ks := setupExportFixture(t)
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("KEY=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	relockProject(t, home, project, ks)
	_, err := Run(context.Background(), Config{
		Home: home, ProjectDir: project, OutputPath: filepath.Join(t.TempDir(), "out.agentpaas"),
		SkipConfirm: true, PublisherStore: ks,
	})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("want denylist error, got %v", err)
	}
}

func TestExport_SentinelBlocked(t *testing.T) {
	home, project, ks := setupExportFixture(t)
	if err := os.WriteFile(filepath.Join(project, "notes.txt"), []byte("SENTINEL_EXPORT_SECRET=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	relockProject(t, home, project, ks)
	_, err := Run(context.Background(), Config{
		Home: home, ProjectDir: project, OutputPath: filepath.Join(t.TempDir(), "out.agentpaas"),
		SkipConfirm: true, PublisherStore: ks,
	})
	if err == nil || !strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("want sentinel error, got %v", err)
	}
}

func TestExport_HappyPathVerifyBundle(t *testing.T) {
	home, project, ks := setupExportFixture(t)
	installExportGitleaks(t)
	pub, err := identity.LoadPublisherIdentity(ks)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "demo.agentpaas")
	result, err := Run(context.Background(), Config{
		Home: home, ProjectDir: project, OutputPath: out,
		SkipConfirm: true, PublisherStore: ks,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if result.PublisherFingerprint != pub.Fingerprint {
		t.Fatalf("fingerprint mismatch")
	}
	b, err := bundle.Open(out)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	vr, err := bundle.Verify(b)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.Verified {
		t.Fatalf("verify failed: %+v", vr.Checks)
	}
}

func setupExportFixture(t *testing.T) (home, project string, pubKS identity.KeyStore) {
	t.Helper()
	home = t.TempDir()
	project = t.TempDir()
	writeMinimalAgentProject(t, project)
	installExportPackTools(t)
	pubKS, _ = publisherKS(t)
	pkgKS, keyID := packageKS(t)
	buildAndDeployLock(t, home, project, pkgKS, keyID, pubKS)
	return home, project, pubKS
}

func publisherKS(t *testing.T) (identity.KeyStore, *identity.PublisherIdentity) {
	t.Helper()
	ks := identity.NewFakeKeyStore()
	pub, err := identity.CreatePublisherIdentity(ks, "export-test-publisher")
	if err != nil {
		t.Fatal(err)
	}
	return ks, pub
}

func packageKS(t *testing.T) (*packBridgeKS, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	ks := identity.NewFakeKeyStore()
	id := identity.KeyID("package_identity_demo")
	if err := ks.Create(id, identity.KeyTypePackageIdentity, identity.KeyMaterial{
		Type: identity.KeyTypePackageIdentity, Bytes: pemBytes,
	}); err != nil {
		t.Fatal(err)
	}
	return &packBridgeKS{KeyStore: ks, keyID: id}, string(id)
}

type packBridgeKS struct {
	identity.KeyStore
	keyID identity.KeyID
}

func (p *packBridgeKS) Sign(id interface{}, digest []byte) ([]byte, error) {
	return p.KeyStore.Sign(p.keyID, digest)
}

func (p *packBridgeKS) Load(id interface{}) (interface{}, error) {
	return p.KeyStore.Load(p.keyID)
}

func writeMinimalAgentProject(t *testing.T, dir string) {
	t.Helper()
	for name, body := range map[string]string{
		"agent.yaml":       "name: demo\nversion: 1.0.0\n",
		"agent.py":         "print('hello')\n",
		"policy.yaml":      "egress:\n  - domain: example.com\n    ports: [443]\n",
		"requirements.txt": "",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func buildAndDeployLock(t *testing.T, home, project string, pkgKS *packBridgeKS, pkgKeyID string, pubKS identity.KeyStore) {
	t.Helper()
	agentYAML, err := pack.LoadAgentYAML(project)
	if err != nil {
		t.Fatal(err)
	}
	ignore, err := ExportIgnoreMatcher(project)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := pack.ComputeBuildInputDigest(project, ignore)
	if err != nil {
		t.Fatal(err)
	}
	policyYAML, err := os.ReadFile(filepath.Join(project, "policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	imgDigest := testDigest("image")
	lock, err := pack.CreateAgentLock(context.Background(), pack.LockConfig{
		BuildResult: &pack.BuildResult{
			ImageDigest:      imgDigest,
			ImageRef:         pack.LocalImageRef(agentYAML.Name, imgDigest),
			BuildInputDigest: digest,
			DepsLocked:       []string{},
		},
		AgentYAML:         agentYAML,
		Runtime:           pack.RuntimePython,
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + testDigest("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   time.Unix(1700000000, 0).UTC(),
		KeyStore:          pkgKS,
		KeyID:             pkgKeyID,
		PublisherKeyStore: pubKS,
		PolicyYAML:        policyYAML,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pack.RecordDeployment(home, agentYAML.Name, lock); err != nil {
		t.Fatal(err)
	}
	sbom, _, err := pack.GenerateSBOM(context.Background(), pack.LocalImageRef(agentYAML.Name, lock.ImageDigest))
	if err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(home, "state", "agents", agentYAML.Name)
	if err := os.WriteFile(filepath.Join(agentDir, "sbom.spdx.json"), sbom, 0o644); err != nil {
		t.Fatal(err)
	}
}

func relockProject(t *testing.T, home, project string, pubKS identity.KeyStore) {
	t.Helper()
	pkgKS, keyID := packageKS(t)
	buildAndDeployLock(t, home, project, pkgKS, keyID, pubKS)
}

func testDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func installExportPackTools(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, script := range map[string]string{
		"syft":   "#!/bin/sh\nprintf '{\"spdxVersion\":\"SPDX-2.3\",\"name\":\"demo\"}'\n",
		"cosign": "#!/bin/sh\ncase \"$1\" in\n  sign-blob) echo \"c2lnbmF0dXJl\" ;;\n  import-key-pair)\n    # cosign import-key-pair --key <src> --output-key-prefix <prefix> --yes\n    prefix=\"\"\n    for i in \"$@\"; do\n      case \"$prev\" in\n        --output-key-prefix) prefix=\"$i\" ;;\n      esac\n      prev=\"$i\"\n    done\n    [ -n \"$prefix\" ] && touch \"$prefix.key\" \"$prefix.pub\"\n    ;;\nesac\nexit 0\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func installExportGitleaks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "gitleaks"), []byte("#!/bin/sh\necho '[]'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}