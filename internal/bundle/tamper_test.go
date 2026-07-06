package bundle

import (
	"archive/tar"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func openAndVerify(t *testing.T, path string) *VerifyReport {
	t.Helper()
	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()
	report, err := Verify(b)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return report
}

func TestTamperMatrix(t *testing.T) {
	base := writeTestBundle(t, true)
	tmp := filepath.Join(t.TempDir(), "tampered.agentpaas")

	cases := []struct {
		name      string
		mutate    func()
		wantCheck string
	}{
		{
			name: "manifest byte",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, ManifestPath, func(b []byte) []byte {
					var m Manifest
					if err := json.Unmarshal(b, &m); err != nil {
						t.Fatalf("unmarshal: %v", err)
					}
					m.CreatedAt = m.CreatedAt.Add(1)
					out, err := json.Marshal(&m)
					if err != nil {
						t.Fatalf("marshal: %v", err)
					}
					return out
				})
			},
			wantCheck: CheckManifestSignature,
		},
		{
			name: "manifest signature",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, ManifestPath, func(b []byte) []byte {
					var m Manifest
					if err := json.Unmarshal(b, &m); err != nil {
						t.Fatalf("unmarshal: %v", err)
					}
					if len(m.ManifestSignature) == 0 {
						t.Fatal("empty signature")
					}
					sig := []byte(m.ManifestSignature)
					sig[0] ^= 0x04
					m.ManifestSignature = string(sig)
					out, err := json.Marshal(&m)
					if err != nil {
						t.Fatalf("marshal: %v", err)
					}
					return out
				})
			},
			wantCheck: CheckManifestSignature,
		},
		{
			name: "lock byte",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, AgentLockPath, func(b []byte) []byte {
					var lock pack.AgentLock
					if err := json.Unmarshal(b, &lock); err != nil {
						t.Fatalf("unmarshal lock: %v", err)
					}
					lock.AgentVersion = lock.AgentVersion + "-tampered"
					out, err := json.Marshal(&lock)
					if err != nil {
						t.Fatalf("marshal lock: %v", err)
					}
					return out
				})
			},
			wantCheck: CheckLockProvenance,
		},
		{
			name: "policy byte",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, PolicyPath, func(b []byte) []byte {
					return append(b, '#')
				})
			},
			wantCheck: CheckContentSHA256,
		},
		{
			name: "manifest policy digest mismatch",
			mutate: func() {
				entries := readBundleTar(t, base.Path)
				for i := range entries {
					if entries[i].name != ManifestPath {
						continue
					}
					entries[i].body = manifestDigestFieldPatch(t, entries[i].body, "policy", "0000000000000000000000000000000000000000000000000000000000000000")
					entries[i].hdr.Size = int64(len(entries[i].body))
				}
				writeBundleTarFile(t, tmp, entries)
			},
			wantCheck: CheckContentSHA256,
		},
		{
			name: "manifest lock digest mismatch",
			mutate: func() {
				entries := readBundleTar(t, base.Path)
				for i := range entries {
					if entries[i].name != ManifestPath {
						continue
					}
					entries[i].body = manifestDigestFieldPatch(t, entries[i].body, "lock", "0000000000000000000000000000000000000000000000000000000000000000")
					entries[i].hdr.Size = int64(len(entries[i].body))
				}
				writeBundleTarFile(t, tmp, entries)
			},
			wantCheck: CheckContentSHA256,
		},
		{
			name: "sbom byte",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, SBOMPath, func(b []byte) []byte {
					return append(b, ' ')
				})
			},
			wantCheck: CheckSBOMDigest,
		},
		{
			name: "source file byte",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, "source/main.py", func(b []byte) []byte {
					return append(b, '\n')
				})
			},
			wantCheck: CheckSourceDigest,
		},
		{
			name: "added source file",
			mutate: func() {
				cloneBundleWithEntries(t, base.Path, tmp, []tarEntry{{
					name: "source/extra.txt",
					hdr: &tar.Header{
						Name:     "source/extra.txt",
						Mode:     0o644,
						Size:     5,
						Typeflag: tar.TypeReg,
						Format:   tar.FormatUSTAR,
					},
					body: []byte("extra"),
				}}, nil)
			},
			wantCheck: CheckSourceDigest,
		},
		{
			name: "removed source file",
			mutate: func() {
				cloneBundleWithEntries(t, base.Path, tmp, nil, map[string]bool{"source/main.py": true})
			},
			wantCheck: CheckSourceDigest,
		},
		{
			name: "image blob byte",
			mutate: func() {
				mutateEntryBody(t, base.Path, tmp, "image/index.json", func(b []byte) []byte {
					out := append([]byte(nil), b...)
					out[0] ^= 0x08
					return out
				})
			},
			wantCheck: CheckImageDigest,
		},
		{
			name: "publisher mismatch manifest vs lock",
			mutate: func() {
				lockWithPublisher := writeTestBundleWithPublisherLock(t)
				mutateEntryBody(t, lockWithPublisher.Path, tmp, ManifestPath, func(b []byte) []byte {
					var m Manifest
					if err := json.Unmarshal(b, &m); err != nil {
						t.Fatalf("unmarshal: %v", err)
					}
					m.Publisher.Fingerprint = "0000000000000000000000000000000000000000000000000000000000000000"
					out, err := json.Marshal(&m)
					if err != nil {
						t.Fatalf("marshal: %v", err)
					}
					return out
				})
			},
			wantCheck: CheckPublisherMatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.mutate()
			report := openAndVerify(t, tmp)
			if report.Verified {
				t.Fatal("expected verification to fail")
			}
			checkFailed(t, report, tc.wantCheck)
		})
	}
}

func writeTestBundleWithPublisherLock(t *testing.T) testBundleFixture {
	t.Helper()
	projectDir := writeTestProject(t)
	aidKey, publisherKey := newTestKeys(t)
	policyYAML := []byte(testPolicyYAML)
	sbom := []byte(testSBOMJSON)
	lock := buildTestLock(t, projectDir, policyYAML, sbom, aidKey)
	pubPEM, err := publicKeyPEM(&publisherKey.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	lock.Publisher = &pack.PublisherInfo{
		Name:         "lock-publisher",
		Fingerprint:  identity.PublisherFingerprint(&publisherKey.PublicKey),
		PublicKeyPEM: string(pubPEM),
		SignedAt:     testSourceDateEpoch(),
	}
	if err := pack.SignLockfileWithKey(lock, aidKey); err != nil {
		t.Fatalf("re-sign lock: %v", err)
	}
	cfg := BundleConfig{
		ProjectDir:      projectDir,
		Manifest:        buildTestManifest(t, publisherKey),
		Lock:            lock,
		PolicyYAML:      policyYAML,
		SBOM:            sbom,
		PublisherKey:    publisherKey,
		SourceDateEpoch: testSourceDateEpoch(),
	}
	outPath := filepath.Join(t.TempDir(), "publisher-mismatch.agentpaas")
	if _, err := WriteToFile(cfg, outPath); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	return testBundleFixture{Path: outPath, ProjectDir: projectDir, PublisherKey: publisherKey, Config: cfg}
}