package install

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
)

func TestAdversaryTamperMaterializedLockFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	lock := filepath.Join(dir, installedLockName)
	if err := os.WriteFile(lock, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after lock tamper")
	}
}

func TestAdversaryTamperSourceFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	src := filepath.Join(dir, installedSourceDir, "main.py")
	if err := os.WriteFile(src, []byte("print('evil')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after source tamper")
	}
}

func TestAdversaryTamperMaterializedPolicyFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	pol := filepath.Join(dir, installedPolicyName)
	if err := os.WriteFile(pol, []byte("egress: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after policy tamper")
	}
}

func TestAdversaryTamperManifestImageDigestFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	mp := filepath.Join(dir, installedManifestName)
	raw, _ := os.ReadFile(mp)
	var m InstallManifest
	_ = json.Unmarshal(raw, &m)
	m.LocalImageDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(mp, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after manifest digest tamper")
	}
}

func TestAdversaryRemoveRetainsTrustPin(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := RemoveInstalledAgent(context.Background(), stateRoot, ref, &FakeContainerStopper{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("state should be removed")
	}
}

func TestAdversarySentinelNotInManifest(t *testing.T) {
	_, dir, _ := materializeInstalledFixture(t)
	raw, err := os.ReadFile(filepath.Join(dir, installedManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SENTINEL-SECRET-VALUE") {
		t.Fatal("manifest leaked sentinel")
	}
}

func materializeInstalledFixture(t *testing.T) (stateRoot, installedDir, ref string) {
	t.Helper()
	stateRoot = filepath.Join(t.TempDir(), "state")
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	res, err := MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest: InstallManifest{
			PublisherFingerprint: fix.PublisherFP,
			PublisherName:        fix.PublisherName,
			AgentName:            fix.AgentName,
			AgentVersion:         fix.AgentVersion,
			AcceptedPolicyDigest: fix.PolicyDigest,
		},
		AllowUnlockedDeps: true,
		Builder:           &fakeImageBuilder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return stateRoot, res.InstalledPath, res.AgentRef
}