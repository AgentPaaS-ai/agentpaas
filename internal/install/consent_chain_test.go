package install

import (
	"crypto/ecdsa"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func seedInstalledAgentLock(t *testing.T, stateRoot string, fix consentBundleFixture, lock *pack.AgentLock) {
	t.Helper()
	dir, err := InstalledAgentPath(stateRoot, fix.AgentName, fix.PublisherFP)
	if err != nil {
		t.Fatalf("installed path: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir installed: %v", err)
	}
	lockPath := filepath.Join(dir, installedLockName)
	if err := pack.WriteAgentLock(lock, lockPath); err != nil {
		t.Fatalf("write installed lock: %v", err)
	}
}

func writeForkedConsentBundle(t *testing.T, parentLockDigest, agentVersion string) consentBundleFixture {
	t.Helper()
	if agentVersion == "" {
		agentVersion = "0.2.0"
	}
	policyYAML := []byte(consentBasePolicyYAML)
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
	pubPEM, _ := consentPubPEM(&pubKey.PublicKey)
	fp := identity.PublisherFingerprint(&pubKey.PublicKey)
	now := time.Unix(0, 0).UTC()

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
			SourceDateEpoch: now,
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: now,
		Publisher: &pack.PublisherInfo{
			Name: "consent-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM), SignedAt: now,
		},
	}
	created := pack.ProvenanceEntry{
		Action: "created", PublisherFingerprint: fp, PublisherName: "consent-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: agentVersion,
		Timestamp: now,
	}
	if err := pack.SignProvenanceEntryWithKey(&created, pubKey); err != nil {
		t.Fatalf("sign created: %v", err)
	}
	forkEntry := pack.ProvenanceEntry{
		Action: "forked", PublisherFingerprint: fp, PublisherName: "consent-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: agentVersion,
		ParentLockDigest: parentLockDigest, ParentBundleDigest: "sha256:parentbundle",
		ParentPolicyDigest: polDigest,
		PolicyDelta:        &pack.PolicyDelta{EgressAdded: []string{"added.example.com:443"}},
		Timestamp:          now.Add(time.Hour),
	}
	if err := pack.SignProvenanceEntryWithKey(&forkEntry, pubKey); err != nil {
		t.Fatalf("sign fork: %v", err)
	}
	lock.Provenance = []pack.ProvenanceEntry{created, forkEntry}
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("sign lock: %v", err)
	}
	if err := pack.SignPublisherWithKey(lock, pubKey); err != nil {
		t.Fatalf("sign publisher: %v", err)
	}
	return writeConsentBundleFromLock(t, dir, lock, policyYAML, sbom, pubKey, fp, string(pubPEM), now, agentVersion)
}

func writeThreeHopConsentBundle(t *testing.T, parentD1, parentD2, agentVersion string) consentBundleFixture {
	t.Helper()
	policyYAML := []byte(consentBasePolicyYAML)
	dir := t.TempDir()
	writeConsentProjectFile(t, dir, "agent.yaml", []byte("name: consent-test-agent\nversion: "+agentVersion+"\nruntime: python\nentry: main.py\n"))
	writeConsentProjectFile(t, dir, "main.py", []byte("ok\n"))
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
	pubPEM, _ := consentPubPEM(&pubKey.PublicKey)
	fp := identity.PublisherFingerprint(&pubKey.PublicKey)
	now := time.Unix(0, 0).UTC()

	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion, AgentName: "consent-test-agent", AgentVersion: agentVersion,
		Runtime: "python", Platform: "linux/arm64",
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:0000000000000000000000000000000000000000000000000000000000000001",
		HarnessVersion: "test", BuildInputDigest: buildDigest, ImageDigest: consentSHA256([]byte("img")),
		SBOMDigest: consentSHA256(sbom), PolicyDigest: polDigest, PackageAID: string(aidPEM),
		PublicKeyFingerprint: pack.PublicKeyFingerprint(&aidKey.PublicKey),
		Reproducibility:      pack.ReproducibilityMeta{SourceDateEpoch: now, BaseImagePinned: true, DepsLocked: true, TarOrder: "sorted"},
		CreatedAt:            now,
		Publisher:            &pack.PublisherInfo{Name: "consent-publisher", Fingerprint: fp, PublicKeyPEM: string(pubPEM), SignedAt: now},
	}
	created := pack.ProvenanceEntry{
		Action: "created", PublisherFingerprint: fp, PublisherName: "consent-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: "0.1.0",
		Timestamp: now,
	}
	if err := pack.SignProvenanceEntryWithKey(&created, pubKey); err != nil {
		t.Fatalf("sign created: %v", err)
	}
	fork1 := pack.ProvenanceEntry{
		Action: "forked", PublisherFingerprint: fp, PublisherName: "consent-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: "0.2.0",
		ParentLockDigest: parentD1, ParentBundleDigest: "sha256:b1", ParentPolicyDigest: polDigest,
		PolicyDelta: &pack.PolicyDelta{EgressAdded: []string{"hop1.example.com:443"}}, Timestamp: now.Add(time.Hour),
	}
	if err := pack.SignProvenanceEntryWithKey(&fork1, pubKey); err != nil {
		t.Fatalf("sign fork1: %v", err)
	}
	fork2 := pack.ProvenanceEntry{
		Action: "forked", PublisherFingerprint: fp, PublisherName: "consent-publisher",
		PublisherPublicKeyPEM: string(pubPEM), AgentName: lock.AgentName, AgentVersion: agentVersion,
		ParentLockDigest: parentD2, ParentBundleDigest: "sha256:b2", ParentPolicyDigest: polDigest,
		PolicyDelta: &pack.PolicyDelta{EgressAdded: []string{"hop2.example.com:443"}}, Timestamp: now.Add(2 * time.Hour),
	}
	if err := pack.SignProvenanceEntryWithKey(&fork2, pubKey); err != nil {
		t.Fatalf("sign fork2: %v", err)
	}
	lock.Provenance = []pack.ProvenanceEntry{created, fork1, fork2}
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("sign lock: %v", err)
	}
	if err := pack.SignPublisherWithKey(lock, pubKey); err != nil {
		t.Fatalf("sign publisher: %v", err)
	}
	return writeConsentBundleFromLock(t, dir, lock, policyYAML, sbom, pubKey, fp, string(pubPEM), now, agentVersion)
}

func writeConsentBundleFromLock(t *testing.T, dir string, lock *pack.AgentLock, policyYAML, sbom []byte, pubKey *ecdsa.PrivateKey, fp, pubPEM string, now time.Time, agentVersion string) consentBundleFixture {
	t.Helper()
	manifest := &bundle.Manifest{
		BundleSchemaVersion: bundle.BundleSchemaVersion,
		Publisher: bundle.ManifestPublisherInfo{
			Name: "consent-publisher", Fingerprint: fp, PublicKeyPEM: pubPEM,
		},
		CreatedAt: now,
	}
	out := filepath.Join(t.TempDir(), "bundle.agentpaas")
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
		t.Fatalf("verify bundle: %v", err)
	}
	if !vr.Verified {
		for _, c := range vr.Checks {
			if !c.Passed {
				t.Logf("verify check %s: %s", c.Name, c.Detail)
			}
		}
		t.Fatalf("bundle not verified")
	}
	report, err := bundle.Inspect(out, b, vr)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	return consentBundleFixture{
		Path: out, PolicyDigest: lock.PolicyDigest, PolicyYAML: policyYAML, PublisherFP: fp,
		PublisherName: "consent-publisher", AgentName: lock.AgentName, AgentVersion: agentVersion,
		InspectReport: report,
	}
}

func TestResolvePolicyConsent_LocallyVerifiedHop(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open parent: %v", err)
	}
	parentDigest := pack.LockDigest(pb.Lock)
	seedInstalledAgentLock(t, root, parent, pb.Lock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close parent bundle: %v", err)
	}

	child := writeForkedConsentBundle(t, parentDigest, "0.2.0")
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: child.InspectReport, PolicyDigest: child.PolicyDigest, PolicyYAML: child.PolicyYAML,
		PublisherFingerprint: child.PublisherFP, PublisherName: child.PublisherName,
		AgentName: child.AgentName, AgentVersion: child.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: child.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("consent: %v", err)
	}
	if !strings.Contains(res.CardText, "(locally verified)") {
		t.Fatalf("expected locally verified hop:\n%s", res.CardText)
	}
}

func TestResolvePolicyConsent_NoParentInstall_SignerClaimed(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	parentDigest := pack.LockDigest(pb.Lock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	state, _ := newConsentState(t)
	child := writeForkedConsentBundle(t, parentDigest, "0.2.0")
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: child.InspectReport, PolicyDigest: child.PolicyDigest, PolicyYAML: child.PolicyYAML,
		PublisherFingerprint: child.PublisherFP, PublisherName: child.PublisherName,
		AgentName: child.AgentName, AgentVersion: child.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: child.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("consent: %v", err)
	}
	if strings.Contains(res.CardText, "(locally verified)") {
		t.Fatalf("must not show locally verified without parent install:\n%s", res.CardText)
	}
	if !strings.Contains(res.CardText, "(signer-claimed)") {
		t.Fatalf("expected signer-claimed:\n%s", res.CardText)
	}
}

func TestResolvePolicyConsent_MixedLocallyVerified(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open parent: %v", err)
	}
	parentLock := pb.Lock
	parentDigest := pack.LockDigest(parentLock)
	state, root := newConsentState(t)
	seedInstalledAgentLock(t, root, parent, parentLock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	middle := writeForkedConsentBundle(t, parentDigest, "0.2.0")
	mb, err := bundle.Open(middle.Path)
	if err != nil {
		t.Fatalf("open middle: %v", err)
	}
	middleDigest := pack.LockDigest(mb.Lock)
	if err := mb.Close(); err != nil {
		t.Fatalf("close middle: %v", err)
	}

	child := writeThreeHopConsentBundle(t, parentDigest, middleDigest, "0.3.0")

	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: child.InspectReport, PolicyDigest: child.PolicyDigest, PolicyYAML: child.PolicyYAML,
		PublisherFingerprint: child.PublisherFP, PublisherName: child.PublisherName,
		AgentName: child.AgentName, AgentVersion: child.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: child.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("consent: %v", err)
	}
	if !strings.Contains(res.CardText, "hop 1:") || !strings.Contains(res.CardText, "hop 2:") {
		t.Fatalf("expected two fork hops:\n%s", res.CardText)
	}
	if !strings.Contains(res.CardText, "hop 1: forked by consent-publisher — +egress hop1.example.com:443 (locally verified)") {
		t.Fatalf("expected hop 1 locally verified in chain section:\n%s", res.CardText)
	}
	if !strings.Contains(res.CardText, "hop 2: forked by consent-publisher — +egress hop2.example.com:443 (signer-claimed)") {
		t.Fatalf("expected hop 2 signer-claimed in chain section:\n%s", res.CardText)
	}
}