package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
)

func TestMaterializeRefusesMissingUVLockNonTTY(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest: InstallManifest{
			PublisherFingerprint: fix.PublisherFP,
			PublisherName:        fix.PublisherName,
			AgentName:            fix.AgentName,
			AgentVersion:         fix.AgentVersion,
			AcceptedPolicyDigest: fix.PolicyDigest,
		},
		Builder: &fakeImageBuilder{},
	})
	if !errors.Is(err, ErrDepsUnlockedRefused) {
		t.Fatalf("err = %v", err)
	}
}

func TestMaterializeRebuildPath(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = b.Close() }()

	stateRoot := filepath.Join(t.TempDir(), "state")
	localDigest := "sha256:local1111111111111111111111111111111111111111111111111111111111"
	builder := &fakeImageBuilder{digest: localDigest}

	res, err := MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot:    stateRoot,
		Bundle:       b,
		BundlePath:   fix.Path,
		BundleDigest: "bundle-digest-hex",
		Manifest: InstallManifest{
			PublisherFingerprint: fix.PublisherFP,
			PublisherName:        fix.PublisherName,
			AgentName:            fix.AgentName,
			AgentVersion:         fix.AgentVersion,
			AcceptedPolicyDigest: fix.PolicyDigest,
		},
		AllowUnlockedDeps: true,
		Builder:           builder,
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if res.Manifest.InstallMode != "local-rebuild" {
		t.Fatalf("mode = %q", res.Manifest.InstallMode)
	}
	if res.Manifest.LocalImageDigest != localDigest {
		t.Fatalf("local digest = %q", res.Manifest.LocalImageDigest)
	}
	if !res.Manifest.DepsUnlockedRebuild {
		t.Fatal("expected deps_unlocked_rebuild true without uv.lock in fixture")
	}
	assertInstalledArtifacts(t, res.InstalledPath)
	if _, err := os.Stat(res.InstalledPath); err != nil {
		t.Fatalf("installed path: %v", err)
	}
	if err := VerifyInstalledAgent(stateRoot, res.AgentRef, nil); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if b.Lock.ImageDigest == res.Manifest.LocalImageDigest {
		t.Fatal("pitfall setup: local digest should differ from signed lock image digest for this test")
	}
}

func TestMaterializePrebuiltImage(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = b.Close() }()
	want := b.Lock.ImageDigest
	b.Manifest.Contents.Image = &bundle.ManifestImageEntry{
		Digest:   want,
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	loader := &fakeImageLoader{}

	stateRoot := filepath.Join(t.TempDir(), "state")
	res, err := MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot:  stateRoot,
		Bundle:     b,
		BundlePath: fix.Path,
		Manifest: InstallManifest{
			PublisherFingerprint: fix.PublisherFP,
			PublisherName:        fix.PublisherName,
			AgentName:            fix.AgentName,
			AgentVersion:         fix.AgentVersion,
			AcceptedPolicyDigest: fix.PolicyDigest,
		},
		PreferImage: true,
		Loader:      loader,
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if res.Manifest.InstallMode != "prebuilt-image" {
		t.Fatalf("mode = %q", res.Manifest.InstallMode)
	}
}

func TestMaterializePrebuiltDigestMismatch(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	b.Manifest.Contents.Image = &bundle.ManifestImageEntry{Digest: b.Lock.ImageDigest, Platform: runtime.GOOS + "/" + runtime.GOARCH}
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest: InstallManifest{
			PublisherFingerprint: fix.PublisherFP,
			PublisherName:        fix.PublisherName,
			AgentName:            fix.AgentName,
			AgentVersion:         fix.AgentVersion,
			AcceptedPolicyDigest: fix.PolicyDigest,
		},
		PreferImage: true,
		Loader:      &fakeImageLoader{digest: "sha256:bad000000000000000000000000000000000000000000000000000000000000"},
	})
	if !errors.Is(err, ErrImageDigestMismatch) {
		t.Fatalf("err = %v", err)
	}
	refDir, _ := InstalledAgentRefDirName(fix.AgentName, fix.PublisherFP)
	final := filepath.Join(stateRoot, installedAgentsDirName, refDir)
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("want no final state dir, stat err=%v", err)
	}
}

func TestMaterializeMissingUVLockWarn(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	var warns []string
	stateRoot := filepath.Join(t.TempDir(), "state")
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
		PrintWarn:         func(msg string) { warns = append(warns, msg) },
		Builder:           &fakeImageBuilder{},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if !res.Manifest.DepsUnlockedRebuild {
		t.Fatal("want deps_unlocked_rebuild true")
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "uv.lock") {
		t.Fatalf("warns = %v", warns)
	}
}

func TestMaterializeRequirementsTxtNoPrompt(t *testing.T) {
	// Bundle has requirements.txt but no uv.lock — should emit an
	// informational note and proceed without interactive prompt.
	fix := writeConsentFixtureBundleWithRequirementsTxt(t)
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	var warns []string
	stateRoot := filepath.Join(t.TempDir(), "state")
	// Note: no AllowUnlockedDeps set, and no PromptUnlocked — the
	// requirements.txt path should not require either.
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
		PrintWarn: func(msg string) { warns = append(warns, msg) },
		Builder:   &fakeImageBuilder{},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if !res.Manifest.DepsUnlockedRebuild {
		t.Fatal("want deps_unlocked_rebuild true")
	}
	if res.Manifest.InstallMode != "local-rebuild" {
		t.Fatalf("mode = %q", res.Manifest.InstallMode)
	}
	// Should have the info note, not the old uv.lock warning.
	found := false
	for _, w := range warns {
		if strings.Contains(w, "requirements.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected requirements.txt note in warns, got: %v", warns)
	}
	// Must NOT contain the old uv.lock warning.
	for _, w := range warns {
		if strings.Contains(w, "WARNING: uv.lock missing") {
			t.Fatalf("unexpected uv.lock warning when requirements.txt present: %v", warns)
		}
	}
	assertInstalledArtifacts(t, res.InstalledPath)
}

func TestMaterializePostWriteTamperRollback(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
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
		PostWriteHook: func(staging string) error {
			p := filepath.Join(staging, installedPolicyName)
			return os.WriteFile(p, []byte("tampered: true\n"), 0o600)
		},
	})
	if !errors.Is(err, ErrMaterializeFailed) {
		t.Fatalf("err = %v", err)
	}
	refDir, _ := InstalledAgentRefDirName(fix.AgentName, fix.PublisherFP)
	if _, err := os.Stat(filepath.Join(stateRoot, installedAgentsDirName, refDir)); !os.IsNotExist(err) {
		t.Fatal("want no installed state after verify rollback")
	}
}

func TestMaterializeBuildFailAtomicity(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
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
		Builder:           &fakeImageBuilder{err: errors.New("build failed")},
	})
	if err == nil {
		t.Fatal("want error")
	}
	refDir, _ := InstalledAgentRefDirName(fix.AgentName, fix.PublisherFP)
	if _, err := os.Stat(filepath.Join(stateRoot, installedAgentsDirName, refDir)); !os.IsNotExist(err) {
		t.Fatal("want no state dir after build failure")
	}
}

func TestListRemoveRoundTrip(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
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
	list, err := ListInstalledAgents(stateRoot)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	stopper := &FakeContainerStopper{}
	if err := RemoveInstalledAgent(context.Background(), stateRoot, list[0].Ref, stopper, nil); err != nil {
		t.Fatal(err)
	}
	if len(stopper.Stopped) != 1 {
		t.Fatalf("stopped = %v", stopper.Stopped)
	}
	list, _ = ListInstalledAgents(stateRoot)
	if len(list) != 0 {
		t.Fatalf("want empty list, got %+v", list)
	}
}

func TestPhase1AgentDirIgnoredByList(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	agents := filepath.Join(stateRoot, installedAgentsDirName)
	if err := os.MkdirAll(filepath.Join(agents, "phase1-agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("phase1 dir should be ignored, got %+v", list)
	}
}

func TestMaterializeInstall_AliasCollisionRejected(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	stateRoot := filepath.Join(t.TempDir(), "state")
	writeInstalledManifestFixture(t, stateRoot, "other", "bbbbbbbb", "maria")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest: InstallManifest{
			PublisherFingerprint: fix.PublisherFP,
			PublisherName:        fix.PublisherName,
			AgentName:            fix.AgentName,
			AgentVersion:         fix.AgentVersion,
			AcceptedPolicyDigest: fix.PolicyDigest,
			Alias:                "maria",
		},
		Builder: &fakeImageBuilder{digest: "sha256:abc"},
	})
	if err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("want alias collision, got %v", err)
	}
}

func assertInstalledArtifacts(t *testing.T, root string) {
	t.Helper()
	names := []string{installedLockName, installedPolicyName, installedSBOMName, installedManifestName, installedParentBundleRef, installedLocalImageDigestFile}
	for _, n := range names {
		info, err := os.Stat(filepath.Join(root, n))
		if err != nil {
			t.Fatalf("missing %s: %v", n, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s perm = %o want 0600", n, info.Mode().Perm())
		}
	}
	src := filepath.Join(root, installedSourceDir)
	si, err := os.Stat(src)
	if err != nil || si.Mode().Perm() != 0o700 {
		t.Fatalf("source dir: %v perm=%o", err, si.Mode().Perm())
	}
}