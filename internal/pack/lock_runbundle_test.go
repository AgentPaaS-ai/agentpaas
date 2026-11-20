package pack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
)

// The signed lock names the run bundle. Verification succeeds, and flipping
// one byte of the bundle digest fails verification because the digest is
// inside the signature.
func TestSignedLockNamesRunBundle(t *testing.T) {
	lock, key := signedTestLock(t)
	payload := []byte("run-bundle-bytes")
	sum := sha256.Sum256(payload)
	lock.Platform = linuxAMD64Target
	lock.BundleDigest = hex.EncodeToString(sum[:])
	lock.BundleSize = int64(len(payload))
	lock.RuntimeID = RuntimeIDPy311R1
	if err := SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("sign: %v", err)
	}

	if err := VerifyAgentLock(lock, ""); err != nil {
		t.Fatalf("lock verifies: %v", err)
	}
	if err := VerifyRunBundle(lock, payload); err != nil {
		t.Fatalf("bundle matches lock: %v", err)
	}

	// Stay inside hex so a format check cannot substitute for signature coverage.
	flipped := []byte(lock.BundleDigest)
	if flipped[0] == 'a' {
		flipped[0] = 'b'
	} else {
		flipped[0] = 'a'
	}
	lock.BundleDigest = string(flipped)
	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("flipping one byte of the bundle digest still verified")
	}
	if err := VerifyAgentLock(lock, ""); err == nil {
		t.Fatal("VerifyAgentLock accepted a flipped bundle digest")
	}
}

// Host-architecture locks keep the previous key set and signature input.
func TestHostArchitectureLockOmitsRunBundleKeys(t *testing.T) {
	lock, _ := signedTestLock(t)
	if lock.Platform == linuxAMD64Target {
		t.Fatal("signedTestLock is not a host-architecture lock")
	}
	raw, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	for _, key := range []string{"bundle_digest", "bundle_size", "runtime_id"} {
		if bytes.Contains(raw, []byte(`"`+key+`"`)) {
			t.Fatalf("host-architecture lock contains %s: %s", key, raw)
		}
	}
	prev := -1
	for _, key := range hostLockCanonicalKeys {
		i := bytes.Index(raw, []byte(`"`+key+`"`))
		if i < 0 || i <= prev {
			t.Fatalf("existing key %s out of canonical order in %s", key, raw)
		}
		prev = i
	}
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("host-architecture lock verifies: %v", err)
	}
}

// A lock whose bundle digest names a different file does not verify.
func TestRunBundleDigestNamesDifferentFile(t *testing.T) {
	lock, key := signedTestLock(t)
	payload := []byte("the-real-bundle")
	sum := sha256.Sum256(payload)
	lock.BundleDigest = hex.EncodeToString(sum[:])
	lock.BundleSize = int64(len(payload))
	lock.RuntimeID = RuntimeIDPy311R1
	if err := SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	other := []byte("a-different-file")
	if err := VerifyRunBundle(lock, other); err == nil {
		t.Fatal("lock verified a bundle digest that names a different file")
	}
	if err := VerifyRunBundle(lock, payload); err != nil {
		t.Fatalf("matching file rejected: %v", err)
	}
}

// Deleting the new keys is a downgrade: the signature no longer matches.
func TestDowngradeByDeletingRunBundleKeys(t *testing.T) {
	lock, key := signedTestLock(t)
	payload := []byte("named-bundle")
	sum := sha256.Sum256(payload)
	lock.BundleDigest = hex.EncodeToString(sum[:])
	lock.BundleSize = int64(len(payload))
	lock.RuntimeID = RuntimeIDPy311R1
	if err := SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	lock.BundleDigest = ""
	lock.BundleSize = 0
	lock.RuntimeID = ""
	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("deleting the run-bundle keys still verified")
	}
}

func TestCreateAgentLockStampsLinuxAMD64RunBundle(t *testing.T) {
	installFakeTool(t, "syft", "#!/bin/sh\nprintf '%s' '{\"spdxVersion\":\"SPDX-2.3\",\"name\":\"agentpaas-test\"}'\n")
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	payload := []byte("stamped-run-bundle")
	imageDigest := digestString("s08-bundle")
	storeRunBundle(imageDigest, payload)
	t.Cleanup(func() { runBundleStash.Delete(imageDigest) })

	lock, err := CreateAgentLock(context.Background(), lockConfigForRunBundle(store, pubKS, linuxAMD64Target, imageDigest))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])
	if lock.BundleDigest != want || lock.BundleSize != int64(len(payload)) || lock.RuntimeID != RuntimeIDPy311R1 {
		t.Fatalf("stamp = digest %q size %d runtime %q", lock.BundleDigest, lock.BundleSize, lock.RuntimeID)
	}
	if err := VerifyAgentLock(lock, ""); err != nil {
		t.Fatalf("stamped lock verifies: %v", err)
	}
	if err := VerifyRunBundle(lock, payload); err != nil {
		t.Fatalf("stamped lock does not name the packed bundle: %v", err)
	}
	raw, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"bundle_digest":"`+want+`"`)) {
		t.Fatal("bundle digest is outside the signature input")
	}
}

func TestCreateAgentLockHostArchitectureOmitsRunBundleKeys(t *testing.T) {
	installFakeTool(t, "syft", "#!/bin/sh\nprintf '%s' '{\"spdxVersion\":\"SPDX-2.3\",\"name\":\"agentpaas-test\"}'\n")
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	lock, err := CreateAgentLock(context.Background(), lockConfigForRunBundle(store, pubKS, "linux/arm64", digestString("s08-host")))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if lock.BundleDigest != "" || lock.BundleSize != 0 || lock.RuntimeID != "" {
		t.Fatalf("host-architecture lock gained keys: digest %q size %d runtime %q", lock.BundleDigest, lock.BundleSize, lock.RuntimeID)
	}
	raw, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	for _, key := range []string{"bundle_digest", "bundle_size", "runtime_id"} {
		if bytes.Contains(raw, []byte(`"`+key+`"`)) {
			t.Fatalf("host-architecture signature input contains %s", key)
		}
	}
}

func TestCreateAgentLockLinuxAMD64WithoutBundleFails(t *testing.T) {
	installFakeTool(t, "syft", "#!/bin/sh\nprintf '%s' '{\"spdxVersion\":\"SPDX-2.3\",\"name\":\"agentpaas-test\"}'\n")
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	_, err := CreateAgentLock(context.Background(), lockConfigForRunBundle(store, pubKS, linuxAMD64Target, digestString("s08-missing")))
	if err == nil {
		t.Fatal("linux/amd64 lock created with no run bundle")
	}
}

func TestDeployedRunBundleMismatchFailsIntegrity(t *testing.T) {
	home := symlinkSafeTempDir(t)
	lock, key := signedTestLock(t)
	lock.Platform = linuxAMD64Target
	payload := []byte("deployed-bundle")
	sum := sha256.Sum256(payload)
	lock.BundleDigest = hex.EncodeToString(sum[:])
	lock.BundleSize = int64(len(payload))
	lock.RuntimeID = RuntimeIDPy311R1
	if err := SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	storeRunBundle(lock.ImageDigest, payload)
	t.Cleanup(func() { runBundleStash.Delete(lock.ImageDigest) })
	if err := RecordDeployment(home, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}
	if err := VerifyDeployedIntegrity(home, lock.AgentName, nil); err != nil {
		t.Fatalf("integrity before replace: %v", err)
	}
	bundlePath := filepath.Join(DeployedAgentPath(home, lock.AgentName), RunBundleName)
	if err := os.WriteFile(bundlePath, []byte("replaced-file"), 0o600); err != nil {
		t.Fatalf("replace bundle: %v", err)
	}
	if err := VerifyDeployedIntegrity(home, lock.AgentName, nil); err == nil {
		t.Fatal("integrity passed for a bundle the lock digest does not name")
	}
}

func lockConfigForRunBundle(store *testKeyStore, pubKS identity.KeyStore, platform, imageDigest string) LockConfig {
	return LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      imageDigest,
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:         &AgentYAML{},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          platform,
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	}
}

// hostLockCanonicalKeys is the signature-input order of a lock that does not
// name a run bundle. New keys must not appear here and must not reorder these.
var hostLockCanonicalKeys = []string{
	"agent_name",
	"agent_version",
	"base_image_digest",
	"build_input_digest",
	"created_at",
	"harness_version",
	"image_digest",
	"package_aid",
	"platform",
	"policy_digest",
	"public_key_fingerprint",
	"reproducibility",
	"runtime",
	"sbom_digest",
	"sbom_referrer",
	"schema_version",
	"signature_referrer",
}

