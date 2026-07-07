package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
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

// --- B23-T04 adversary break tests (TestAdversary_B23T04_*) ---

func TestAdversary_B23T04_AtomicInstallNoFinalDirOnBuildFail(t *testing.T) {
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
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		Builder:   &fakeImageBuilder{err: errors.New("build failed")},
	})
	if err == nil {
		t.Fatal("want materialize error")
	}
	assertNoInstalledFinalDir(t, stateRoot, fix)
	assertNoStagingTmpDirs(t, stateRoot)
}

func TestAdversary_B23T04_AtomicInstallNoFinalDirOnVerifyFail(t *testing.T) {
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
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		Builder:   &fakeImageBuilder{},
		PostWriteHook: func(staging string) error {
			return os.WriteFile(filepath.Join(staging, installedPolicyName), []byte("tampered: true\n"), 0o600)
		},
	})
	if !errors.Is(err, ErrMaterializeFailed) {
		t.Fatalf("err = %v", err)
	}
	assertNoInstalledFinalDir(t, stateRoot, fix)
	assertNoStagingTmpDirs(t, stateRoot)
}

func TestAdversary_B23T04_RebuildVerifyPassesDespiteLockImageDigestMismatch(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	localDigest := "sha256:local1111111111111111111111111111111111111111111111111111111111"
	stateRoot := filepath.Join(t.TempDir(), "state")
	res, err := MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		Builder:   &fakeImageBuilder{digest: localDigest},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Lock.ImageDigest == res.Manifest.LocalImageDigest {
		t.Fatal("need lock image digest != local rebuild digest for pitfall test")
	}
	if err := VerifyInstalledAgent(stateRoot, res.AgentRef, nil); err != nil {
		t.Fatalf("verify should pass on manifest local digest only: %v", err)
	}
}

func TestAdversary_B23T04_TamperLocalDigestFileFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	digestPath := filepath.Join(dir, installedLocalImageDigestFile)
	if err := os.WriteFile(digestPath, []byte("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after local_image.digest tamper")
	}
}

func TestAdversary_B23T04_TamperLockFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := os.WriteFile(filepath.Join(dir, installedLockName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after agent.lock tamper")
	}
}

func TestAdversary_B23T04_TamperPolicyFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := os.WriteFile(filepath.Join(dir, installedPolicyName), []byte("egress: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after policy tamper")
	}
}

func TestAdversary_B23T04_TamperSourceFailsVerify(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := os.WriteFile(filepath.Join(dir, installedSourceDir, "main.py"), []byte("evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstalledAgent(stateRoot, ref, nil); err == nil {
		t.Fatal("want verify failure after source tamper")
	}
}

func TestAdversary_B23T04_PrebuiltDigestMismatchZeroState(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	b.Manifest.Contents.Image = &bundle.ManifestImageEntry{
		Digest:   b.Lock.ImageDigest,
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest:  materializeManifestFromFix(fix),
		PreferImage: true,
		Loader:    &fakeImageLoader{digest: "sha256:bad000000000000000000000000000000000000000000000000000000000000"},
	})
	if !errors.Is(err, ErrImageDigestMismatch) {
		t.Fatalf("err = %v", err)
	}
	assertNoInstalledFinalDir(t, stateRoot, fix)
}

func TestAdversary_B23T04_PrebuiltPlatformMismatchRefuse(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	wrong := "linux/amd64"
	if runtime.GOARCH == "amd64" {
		wrong = "linux/arm64"
	}
	b.Manifest.Contents.Image = &bundle.ManifestImageEntry{Digest: b.Lock.ImageDigest, Platform: wrong}
	stateRoot := filepath.Join(t.TempDir(), "state")
	_, err = MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest:  materializeManifestFromFix(fix),
		PreferImage: true,
		Loader:    &fakeImageLoader{},
	})
	if !errors.Is(err, ErrPrebuiltPlatformMismatch) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "reinstall without --prefer-image") {
		t.Fatalf("message = %q", err.Error())
	}
	assertNoInstalledFinalDir(t, stateRoot, fix)
}

func TestAdversary_B23T04_MissingUVLockRefusedNonTTY(t *testing.T) {
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
		Manifest:  materializeManifestFromFix(fix),
		Builder:   &fakeImageBuilder{},
	})
	if !errors.Is(err, ErrDepsUnlockedRefused) {
		t.Fatalf("err = %v", err)
	}
	assertNoInstalledFinalDir(t, stateRoot, fix)
}

func TestAdversary_B23T04_MissingUVLockSetsDepsUnlockedWhenAllowed(t *testing.T) {
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
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		PrintWarn: func(msg string) { warns = append(warns, msg) },
		Builder:   &fakeImageBuilder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Manifest.DepsUnlockedRebuild {
		t.Fatal("want deps_unlocked_rebuild true")
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "uv.lock") {
		t.Fatalf("warns = %v", warns)
	}
}

func TestAdversary_B23T04_VerifyFailRollbackRemovesFinalPath(t *testing.T) {
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
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		Builder:   &fakeImageBuilder{},
		PostWriteHook: func(staging string) error {
			return os.WriteFile(filepath.Join(staging, installedSourceDir, "main.py"), []byte("tamper\n"), 0o600)
		},
	})
	if !errors.Is(err, ErrMaterializeFailed) {
		t.Fatalf("err = %v", err)
	}
	assertNoInstalledFinalDir(t, stateRoot, fix)
}

func TestAdversary_B23T04_RemoveRetainsTrustPin(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := b.Manifest.Publisher.PublicKeyPEM
	_ = b.Close()

	store, storePath := newTestStore(t)
	pub := trust.Publisher{
		Fingerprint:  trust.NormalizeFingerprint(fix.PublisherFP),
		PublicKeyPEM: pubPEM,
		Alias:        fix.PublisherName,
	}
	if err := store.Pin(pub, trust.SourceTOFU); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	before := trustStoreSnapshot(t, storePath)

	stateRoot, _, ref := materializeInstalledFixture(t)
	if err := RemoveInstalledAgent(context.Background(), stateRoot, ref, &FakeContainerStopper{}, nil); err != nil {
		t.Fatal(err)
	}
	assertTrustStoreUnchanged(t, storePath, before)
}

func TestAdversary_B23T04_RemoveStopsContainersBeforeStateGone(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	stopper := &recordingStopper{fail: errors.New("stop blocked")}
	err := RemoveInstalledAgent(context.Background(), stateRoot, ref, stopper, nil)
	if err == nil {
		t.Fatal("want stop failure")
	}
	if !stopper.called {
		t.Fatal("stopper should have been invoked before remove")
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("state dir should remain when stop fails: %v", statErr)
	}
}

func TestAdversary_B23T04_SameNameDifferentPublishersCoexist(t *testing.T) {
	fixA := writeConsentFixtureBundle(t, nil, "0.1.0")
	fixB := writeConsentFixtureBundleAltPublisher(t, nil, "0.1.0")
	if fixA.PublisherFP == fixB.PublisherFP {
		t.Fatal("need distinct publisher fingerprints")
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	materializeFix(t, stateRoot, fixA)
	materializeFix(t, stateRoot, fixB)
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 installed agents, got %+v", list)
	}
	dirA, err := InstalledAgentPath(stateRoot, fixA.AgentName, fixA.PublisherFP)
	if err != nil {
		t.Fatal(err)
	}
	dirB, err := InstalledAgentPath(stateRoot, fixB.AgentName, fixB.PublisherFP)
	if err != nil {
		t.Fatal(err)
	}
	if dirA == dirB {
		t.Fatal("installed paths must differ by pub8")
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Fatal(err)
	}
}

func TestAdversary_B23T04_SentinelNotInManifestListOrAudit(t *testing.T) {
	secretPolicy := []byte(`version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "` + adversarySecretSentinel + `"
`)
	fix := writeConsentFixtureBundle(t, secretPolicy, "0.1.0")
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	var auditRecords []audit.AuditRecord
	appender := &sliceAuditAppender{records: &auditRecords}
	stateRoot := filepath.Join(t.TempDir(), "state")
	res, err := MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		Builder:   &fakeImageBuilder{},
		Audit:     appender,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, _ := os.ReadFile(filepath.Join(res.InstalledPath, installedManifestName))
	list, _ := ListInstalledAgents(stateRoot)
	listJSON, _ := json.Marshal(list)
	var auditBlob strings.Builder
	for _, r := range auditRecords {
		blob, _ := json.Marshal(r)
		auditBlob.Write(blob)
	}
	policyOnDisk, _ := os.ReadFile(filepath.Join(res.InstalledPath, installedPolicyName))
	if !strings.Contains(string(policyOnDisk), adversarySecretSentinel) {
		t.Fatal("policy.yaml on disk should retain secret for runtime")
	}
	assertSentinelAbsent(t, adversarySecretSentinel,
		string(manifestRaw), string(listJSON), auditBlob.String())
}

func TestAdversary_B23T04_InstalledDirFilePerms(t *testing.T) {
	_, dir, _ := materializeInstalledFixture(t)
	walkStatePermissions(t, dir)
}

func TestAdversary_B23T04_Phase1DirIgnoredByListAndRemove(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	phase1 := filepath.Join(stateRoot, installedAgentsDirName, "legacy-phase1-agent")
	if err := os.MkdirAll(phase1, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(phase1, "marker"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("phase1 dir must not appear in list: %+v", list)
	}
	err = RemoveInstalledAgent(context.Background(), stateRoot, "legacy-phase1-agent", &FakeContainerStopper{}, nil)
	if err == nil {
		t.Fatal("want error removing non-installed phase1 name")
	}
	if _, err := os.Stat(filepath.Join(phase1, "marker")); err != nil {
		t.Fatalf("phase1 dir must remain untouched: %v", err)
	}
}

type sliceAuditAppender struct {
	records *[]audit.AuditRecord
}

func (s *sliceAuditAppender) Append(r audit.AuditRecord) error {
	*s.records = append(*s.records, r)
	return nil
}

type recordingStopper struct {
	called bool
	fail   error
}

func (r *recordingStopper) StopByAgentRef(ctx context.Context, agentRef string) error {
	r.called = true
	return r.fail
}

func materializeManifestFromFix(fix consentBundleFixture) InstallManifest {
	return InstallManifest{
		PublisherFingerprint: fix.PublisherFP,
		PublisherName:        fix.PublisherName,
		AgentName:            fix.AgentName,
		AgentVersion:         fix.AgentVersion,
		AcceptedPolicyDigest: fix.PolicyDigest,
	}
}

func materializeFix(t *testing.T, stateRoot string, fix consentBundleFixture) *MaterializeResult {
	t.Helper()
	b, err := bundle.Open(fix.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	res, err := MaterializeInstall(context.Background(), MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest:  materializeManifestFromFix(fix),
		AllowUnlockedDeps: true,
		Builder:   &fakeImageBuilder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertNoInstalledFinalDir(t *testing.T, stateRoot string, fix consentBundleFixture) {
	t.Helper()
	final, err := InstalledAgentPath(stateRoot, fix.AgentName, fix.PublisherFP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("want no final state at %s, stat err=%v", final, err)
	}
}

func assertNoStagingTmpDirs(t *testing.T, stateRoot string) {
	t.Helper()
	agentsDir := filepath.Join(stateRoot, installedAgentsDirName)
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".tmp-") {
			t.Fatalf("leftover staging dir %s", ent.Name())
		}
	}
}

// writeConsentFixtureBundleAltPublisher builds a verified bundle with a different publisher key (same agent name).
func writeConsentFixtureBundleAltPublisher(t *testing.T, policyYAML []byte, agentVersion string) consentBundleFixture {
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
	aidKey := consentDetKey(t, "aid-alt")
	pubKey := consentDetKey(t, "pub-alt")
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
		ImageDigest:          consentSHA256([]byte("img-alt")),
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
	publisherName := "consent-publisher-alt"
	lock.Publisher = &pack.PublisherInfo{
		Name: publisherName, Fingerprint: fp, PublicKeyPEM: string(pubPEM), SignedAt: now,
	}
	entry := pack.ProvenanceEntry{
		Action: "created", PublisherFingerprint: fp, PublisherName: publisherName,
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
			Name: publisherName, Fingerprint: fp, PublicKeyPEM: string(pubPEM),
		},
		CreatedAt: now,
	}
	out := filepath.Join(t.TempDir(), "consent-alt.agentpaas")
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
		PublisherName: publisherName, AgentName: "consent-test-agent", AgentVersion: agentVersion,
		InspectReport: report,
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