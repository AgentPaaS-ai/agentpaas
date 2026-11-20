package bundle

import (
	"crypto/ecdsa"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const inspectSBOMJSON = `{
  "spdxVersion": "SPDX-2.3",
  "name": "bundle-inspect-test",
  "packages": [
    {"name": "idna", "SPDXID": "SPDXRef-Package-idna"},
    {"name": "requests", "SPDXID": "SPDXRef-Package-requests"}
  ],
  "relationships": [
    {"spdxElementId": "SPDXRef-Package-root", "relatedSpdxElement": "SPDXRef-Package-idna", "relationshipType": "DEPENDS_ON"},
    {"spdxElementId": "SPDXRef-Package-root", "relatedSpdxElement": "SPDXRef-Package-requests", "relationshipType": "DEPENDS_ON"}
  ]
}`

const lintBaitPolicyYAML = `version: "1.0"
agent:
  name: lint-bait
  description: "lint bait"
egress:
  - domain: "*.evil.com"
    ports: [443]
    methods: [GET]
    credential: wild-cred
    allow_wildcard: true
  - domain: "192.168.0.1"
    ports: [80]
    methods: [GET]
  - domain: "a1.example.com"
    ports: [443]
  - domain: "a2.example.com"
    ports: [443]
  - domain: "a3.example.com"
    ports: [443]
  - domain: "a4.example.com"
    ports: [443]
  - domain: "a5.example.com"
    ports: [443]
  - domain: "a6.example.com"
    ports: [443]
  - domain: "a7.example.com"
    ports: [443]
  - domain: "a8.example.com"
    ports: [443]
  - domain: "a9.example.com"
    ports: [443]
credentials:
  - id: "wild-cred"
    type: header
    header: "Authorization"
    value: "x"
`

func attachPublisherProvenance(t *testing.T, lock *pack.AgentLock, publisherKey *ecdsa.PrivateKey, entries int) {
	t.Helper()
	pubPEM, err := publicKeyPEM(&publisherKey.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	fp := identity.PublisherFingerprint(&publisherKey.PublicKey)
	now := testSourceDateEpoch()
	lock.SchemaVersion = pack.LockSchemaVersion
	lock.Publisher = &pack.PublisherInfo{
		Name:         "test-publisher",
		Fingerprint:  fp,
		PublicKeyPEM: string(pubPEM),
		SignedAt:     now,
	}
	lock.Provenance = nil
	for i := 0; i < entries; i++ {
		action := "created"
		parentLock := ""
		parentBundle := ""
		parentPolicy := ""
		var delta *pack.PolicyDelta
		if i > 0 {
			action = "forked"
			parentLock = "sha256:parentlock"
			parentBundle = "sha256:parentbundle"
			parentPolicy = "sha256:parentpolicy"
			delta = &pack.PolicyDelta{EgressAdded: []string{"api.slack.com:443"}}
		}
		e := pack.ProvenanceEntry{
			Action:                action,
			PublisherFingerprint:  fp,
			PublisherName:         "test-publisher",
			PublisherPublicKeyPEM: string(pubPEM),
			AgentName:             lock.AgentName,
			AgentVersion:          lock.AgentVersion,
			ParentLockDigest:      parentLock,
			ParentBundleDigest:    parentBundle,
			ParentPolicyDigest:    parentPolicy,
			PolicyDelta:           delta,
			Timestamp:             now.Add(time.Duration(i) * time.Hour),
		}
		if err := pack.SignProvenanceEntryWithKey(&e, publisherKey); err != nil {
			t.Fatalf("SignProvenanceEntryWithKey: %v", err)
		}
		lock.Provenance = append(lock.Provenance, e)
	}
}

func writeInspectFixtureBundle(t *testing.T, provenanceEntries int, policyYAML []byte, sbom []byte) string {
	t.Helper()
	projectDir := writeTestProject(t)
	aidKey, publisherKey := newTestKeys(t)
	if sbom == nil {
		sbom = []byte(inspectSBOMJSON)
	}
	if policyYAML == nil {
		policyYAML = []byte(testPolicyYAML)
	}
	lock := buildTestLock(t, projectDir, policyYAML, sbom, aidKey)
	attachPublisherProvenance(t, lock, publisherKey, provenanceEntries)
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("SignLockfileWithKey: %v", err)
	}
	if err := pack.SignPublisherWithKey(lock, publisherKey); err != nil {
		t.Fatalf("SignPublisherWithKey: %v", err)
	}
	manifest := buildTestManifest(t, publisherKey)
	cfg := BundleConfig{
		ProjectDir:      projectDir,
		Manifest:        manifest,
		Lock:            lock,
		PolicyYAML:      policyYAML,
		SBOM:            sbom,
		PublisherKey:    publisherKey,
		SourceDateEpoch: testSourceDateEpoch(),
	}
	outPath := filepath.Join(t.TempDir(), "inspect.agentpaas")
	if _, err := WriteToFile(cfg, outPath); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	return outPath
}

func openInspectReport(t *testing.T, path string) *InspectReport {
	t.Helper()
	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()
	vr, err := Verify(b)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	r, err := Inspect(path, b, vr)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	return r
}

func TestInspect_GoldenText_OneProvenance(t *testing.T) {
	path := writeInspectFixtureBundle(t, 1, nil, nil)
	report := openInspectReport(t, path)
	text := FormatInspectText(report)
	if !strings.Contains(text, "BUNDLE INSPECT") {
		t.Fatal("missing header")
	}
	if !strings.Contains(text, "It does not mean the agent is safe") {
		t.Fatal("missing D3 disclaimer")
	}
	if !strings.Contains(text, "created") {
		t.Fatal("missing provenance created line")
	}
	if !strings.Contains(text, "api.example.com:443") {
		t.Fatal("missing egress in policy summary")
	}
	if strings.Contains(text, "Policy lints (warnings)") {
		t.Fatal("clean policy should have no lints section")
	}
	golden := filepath.Join("testdata", "inspect_1prov.golden.txt")
	norm := NormalizeInspectText(text)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(golden, []byte(norm), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (set UPDATE_GOLDEN=1 to create): %v", err)
	}
	if !goldenTextEqual(string(want), text) {
		t.Fatalf("golden text mismatch\n--- want ---\n%s\n--- got ---\n%s", want, norm)
	}
}

func TestInspect_GoldenJSON_ThreeProvenance(t *testing.T) {
	path := writeInspectFixtureBundle(t, 3, nil, nil)
	report := openInspectReport(t, path)
	if report.Provenance == nil || len(report.Provenance.Entries) != 3 {
		t.Fatalf("want 3 provenance entries, got %+v", report.Provenance)
	}
	data := MustMarshalInspectGoldenJSON(report)
	golden := filepath.Join("testdata", "inspect_3prov.golden.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (set UPDATE_GOLDEN=1 to create): %v", err)
	}
	if !goldenJSONEqual(want, MustMarshalInspectGoldenJSON(report)) {
		t.Fatalf("golden JSON mismatch\n--- want ---\n%s\n--- got ---\n%s", want, data)
	}
}

func TestInspect_LintBaitRenderedInReport(t *testing.T) {
	path := writeInspectFixtureBundle(t, 1, []byte(lintBaitPolicyYAML), nil)
	report := openInspectReport(t, path)
	if len(report.PolicyLints) < 5 {
		t.Fatalf("expected all lints, got %d: %v", len(report.PolicyLints), report.PolicyLints)
	}
	text := FormatInspectText(report)
	if !strings.Contains(text, "Policy lints (warnings)") {
		t.Fatal("missing lints section in text output")
	}
}

func TestInspect_ExtraFilesSection(t *testing.T) {
	projectDir := writeTestProject(t)
	aidKey, publisherKey := newTestKeys(t)
	lock := buildTestLock(t, projectDir, []byte(testPolicyYAML), []byte(inspectSBOMJSON), aidKey)
	attachPublisherProvenance(t, lock, publisherKey, 1)
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("sign lock: %v", err)
	}
	if err := pack.SignPublisherWithKey(lock, publisherKey); err != nil {
		t.Fatalf("sign publisher: %v", err)
	}
	manifest := buildTestManifest(t, publisherKey)
	manifest.ExtraFiles = []ManifestExtraFile{{Path: "notes.txt", Digest: "sha256:abc", Bytes: 3}}
	cfg := BundleConfig{
		ProjectDir: projectDir, Manifest: manifest, Lock: lock,
		PolicyYAML: []byte(testPolicyYAML), SBOM: []byte(inspectSBOMJSON),
		PublisherKey: publisherKey, SourceDateEpoch: testSourceDateEpoch(),
	}
	outPath := filepath.Join(t.TempDir(), "extra.agentpaas")
	if _, err := WriteToFile(cfg, outPath); err != nil {
		t.Fatalf("write: %v", err)
	}
	report := openInspectReport(t, outPath)
	text := FormatInspectText(report)
	if !strings.Contains(text, "extra files (not part of build)") {
		t.Fatalf("missing extra files heading: %s", text)
	}
	if !strings.Contains(text, "notes.txt") {
		t.Fatal("missing extra file path")
	}
}

func TestInspect_PolicyLints_AllFire(t *testing.T) {
	pol, err := policy.ParsePolicy(strings.NewReader(lintBaitPolicyYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	lints := ComputePolicyLints(pol)
	codes := map[string]bool{}
	for _, l := range lints {
		codes[l.Code] = true
	}
	for _, code := range []string{
		LintWildcardDomain,
		LintRawIPEgress,
		LintNonTLSPort,
		LintManyEgressDomains,
		LintCredWildcardDest,
	} {
		if !codes[code] {
			t.Fatalf("expected lint %q, got codes %v", code, codes)
		}
	}
}

func TestInspect_PolicyLints_CleanNone(t *testing.T) {
	pol, err := policy.ParsePolicy(strings.NewReader(testPolicyYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if l := ComputePolicyLints(pol); len(l) != 0 {
		t.Fatalf("expected no lints, got %v", l)
	}
}

func TestInspect_TamperedWithholdsAuthenticatedSections(t *testing.T) {
	base := writeInspectFixtureBundle(t, 1, nil, nil)
	tmp := filepath.Join(t.TempDir(), "tampered.agentpaas")
	mutateEntryBody(t, base, tmp, ManifestPath, func(b []byte) []byte {
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		m.CreatedAt = m.CreatedAt.Add(time.Second)
		out, err := json.Marshal(&m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return out
	})
	b, err := Open(tmp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()
	vr, err := Verify(b)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if vr.Verified {
		t.Fatal("expected verify fail")
	}
	report, err := Inspect(tmp, b, vr)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	text := FormatInspectText(report)
	if !strings.Contains(text, "FAIL") {
		t.Fatal("expected FAIL in integrity section")
	}
	if strings.Contains(text, "Policy summary") {
		t.Fatal("policy summary must be absent for tampered bundle")
	}
	if strings.Contains(text, "Provenance:") {
		t.Fatal("provenance must be absent for tampered bundle")
	}
	if report.Publisher != nil {
		t.Fatal("publisher section must be nil when unverified")
	}
}