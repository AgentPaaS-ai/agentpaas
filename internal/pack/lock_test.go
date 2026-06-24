package pack

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type testKeyStore struct {
	keyID string
	key   *ecdsa.PrivateKey
	pem   []byte
}

func (s *testKeyStore) Sign(id interface{}, digest []byte) ([]byte, error) {
	if id != s.keyID {
		return nil, os.ErrNotExist
	}
	return ecdsa.SignASN1(rand.Reader, s.key, digest)
}

func (s *testKeyStore) Load(id interface{}) (interface{}, error) {
	if id != s.keyID {
		return nil, os.ErrNotExist
	}
	return s.pem, nil
}

func TestAgentLockCanonicalJSON(t *testing.T) {
	lockA, _ := signedTestLock(t)
	lockB := *lockA
	lockB.LockfileSignature = "different"

	jsonA, err := canonicalJSON(lockA)
	if err != nil {
		t.Fatalf("canonicalJSON(lockA): %v", err)
	}
	jsonB, err := canonicalJSON(&lockB)
	if err != nil {
		t.Fatalf("canonicalJSON(lockB): %v", err)
	}
	if !bytes.Equal(jsonA, jsonB) {
		t.Fatalf("canonical JSON differs:\n%s\n%s", jsonA, jsonB)
	}
}

func TestAgentLockSignatureRoundtrip(t *testing.T) {
	lock, _ := signedTestLock(t)

	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("VerifyLockfileSignature: %v", err)
	}
}

func TestAgentLockTamperedSignature(t *testing.T) {
	lock, _ := signedTestLock(t)
	lock.AgentName = "tampered"

	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("VerifyLockfileSignature succeeded for tampered lock")
	}
}

func TestAgentLockTamperedLockfileSig(t *testing.T) {
	lock, _ := signedTestLock(t)
	lock.LockfileSignature = base64.StdEncoding.EncodeToString([]byte("tampered"))

	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("VerifyLockfileSignature succeeded for tampered signature")
	}
}

func TestPublicKeyFromPEM(t *testing.T) {
	_, pubPEM := testKeyPair(t)

	pub, err := PublicKeyFromPEM(pubPEM)
	if err != nil {
		t.Fatalf("PublicKeyFromPEM: %v", err)
	}
	if pub.Curve != elliptic.P256() {
		t.Fatalf("curve = %v, want P-256", pub.Curve)
	}
}

func TestPublicKeyFromPEMInvalid(t *testing.T) {
	if _, err := PublicKeyFromPEM([]byte("not pem")); err == nil {
		t.Fatal("PublicKeyFromPEM succeeded for invalid PEM")
	}
}

func TestPublicKeyFingerprint(t *testing.T) {
	key, _ := testKeyPair(t)

	first := PublicKeyFingerprint(&key.PublicKey)
	second := PublicKeyFingerprint(&key.PublicKey)
	if first == "" {
		t.Fatal("fingerprint is empty")
	}
	if first != second {
		t.Fatalf("fingerprint not stable: %s != %s", first, second)
	}
}

func TestCanonicalJSONNoLockfileSig(t *testing.T) {
	lock, _ := signedTestLock(t)

	got, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	if bytes.Contains(got, []byte("lockfile_signature")) {
		t.Fatalf("canonical JSON contains lockfile_signature: %s", got)
	}
}

func TestCanonicalJSONSortedKeys(t *testing.T) {
	lock, _ := signedTestLock(t)

	got, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	if !bytes.HasPrefix(got, []byte(`{"agent_name":`)) {
		t.Fatalf("top-level keys are not sorted: %s", got)
	}
	wantNested := `"reproducibility":{"base_image_pinned":true,"deps_locked":true,"source_date_epoch":`
	if !strings.Contains(string(got), wantNested) {
		t.Fatalf("nested reproducibility keys are not sorted: %s", got)
	}
}

func TestWriteReadAgentLockRoundtrip(t *testing.T) {
	lock, _ := signedTestLock(t)
	path := filepath.Join(testSecureTempDir(t), "agent.lock")

	if err := WriteAgentLock(lock, path); err != nil {
		t.Fatalf("WriteAgentLock: %v", err)
	}
	got, err := ReadAgentLock(path)
	if err != nil {
		t.Fatalf("ReadAgentLock: %v", err)
	}
	if !reflect.DeepEqual(got, lock) {
		before, _ := json.Marshal(lock)
		after, _ := json.Marshal(got)
		t.Fatalf("roundtrip mismatch:\n%s\n%s", before, after)
	}
}

func TestGenerateSBOM(t *testing.T) {
	if _, err := exec.LookPath("syft"); err != nil {
		t.Skip("syft not available")
	}

	sbom, digest, err := GenerateSBOM(context.Background(), "dir:.")
	if err != nil {
		t.Fatalf("GenerateSBOM: %v", err)
	}
	if len(sbom) == 0 {
		t.Fatal("SBOM is empty")
	}
	if digest != sha256Hex(sbom) {
		t.Fatalf("digest = %s, want %s", digest, sha256Hex(sbom))
	}
}

func TestSignImage(t *testing.T) {
	if _, err := exec.LookPath("cosign"); err != nil {
		t.Skip("cosign not available")
	}
	if os.Getenv("AGENTPAAS_PACK_REAL_TOOLS") == "" {
		t.Skip("set AGENTPAAS_PACK_REAL_TOOLS=1 to run real cosign image signing test")
	}

	key, _ := testKeyPair(t)
	keyPEM := privateKeyPEMForTest(t, key)
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	if _, err := SignImage(context.Background(), "agentpaas-test:latest", keyPath); err != nil {
		t.Fatalf("SignImage: %v", err)
	}
}

func TestCreateAgentLock(t *testing.T) {
	installFakeTool(t, "syft", "#!/bin/sh\nprintf '%s' '{\"spdxVersion\":\"SPDX-2.3\",\"name\":\"agentpaas-test\"}'\n")
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)

	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:       &AgentYAML{},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
		KeyStore:        store,
		KeyID:           store.keyID,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if lock.SchemaVersion != LockSchemaVersion {
		t.Fatalf("schema version = %d", lock.SchemaVersion)
	}
	if lock.PackageAID == "" {
		t.Fatal("PackageAID is empty")
	}
	if strings.Contains(lock.PackageAID, "PRIVATE KEY") {
		t.Fatal("PackageAID contains private key material")
	}
	if lock.LockfileSignature == "" {
		t.Fatal("LockfileSignature is empty")
	}
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("VerifyLockfileSignature: %v", err)
	}
}

func TestVerifyAgentLock(t *testing.T) {
	installFakeTool(t, "cosign", fakeCosignScript())
	lock, _ := signedTestLock(t)
	sbomPath := filepath.Join(testSecureTempDir(t), "sbom.spdx.json")
	sbom := []byte(`{"spdxVersion":"SPDX-2.3"}`)
	if err := os.WriteFile(sbomPath, sbom, 0o600); err != nil {
		t.Fatalf("write sbom: %v", err)
	}
	lock.SBOMReferrer = sbomPath
	lock.SBOMDigest = sha256Hex(sbom)
	signLockForTest(t, lock)

	if err := VerifyAgentLock(lock, "agentpaas-test@sha256:"+digestString("image")); err != nil {
		t.Fatalf("VerifyAgentLock: %v", err)
	}
}

func signedTestLock(t *testing.T) (*AgentLock, *ecdsa.PrivateKey) {
	t.Helper()

	key, pubPEM := testKeyPair(t)
	lock := &AgentLock{
		SchemaVersion:        LockSchemaVersion,
		AgentName:            "agent",
		AgentVersion:         "0.1.0",
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:       "test",
		BuildInputDigest:     digestString("input"),
		ImageDigest:          digestString("image"),
		SBOMDigest:           digestString("sbom"),
		PolicyDigest:         "",
		PackageAID:           string(pubPEM),
		PublicKeyFingerprint: PublicKeyFingerprint(&key.PublicKey),
		SBOMReferrer:         "oci://agentpaas-test:latest#sbom",
		SignatureReferrer:    "cosign://agentpaas-test:latest",
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: testTime(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: testTime(),
	}
	signLockWithKeyForTest(t, lock, key)
	return lock, key
}

func signLockForTest(t *testing.T, lock *AgentLock) {
	t.Helper()

	key, err := privateKeyForLock(lock)
	if err != nil {
		t.Fatalf("privateKeyForLock: %v", err)
	}
	signLockWithKeyForTest(t, lock, key)
}

func signLockWithKeyForTest(t *testing.T, lock *AgentLock, key *ecdsa.PrivateKey) {
	t.Helper()

	lock.LockfileSignature = ""
	canonical, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	digest := sha256.Sum256(canonical)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}
	lock.LockfileSignature = base64.StdEncoding.EncodeToString(signature)
	testLockKeys.Store(lock.PackageAID, key)
}

var testLockKeys syncMap

type syncMap struct {
	keys map[string]*ecdsa.PrivateKey
}

func (m *syncMap) Store(aid string, key *ecdsa.PrivateKey) {
	if m.keys == nil {
		m.keys = make(map[string]*ecdsa.PrivateKey)
	}
	m.keys[aid] = key
}

func (m *syncMap) Load(aid string) (*ecdsa.PrivateKey, bool) {
	key, ok := m.keys[aid]
	return key, ok
}

func privateKeyForLock(lock *AgentLock) (*ecdsa.PrivateKey, error) {
	if key, ok := testLockKeys.Load(lock.PackageAID); ok {
		return key, nil
	}
	return nil, os.ErrNotExist
}

func testKeyPair(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return key, pubPEM
}

func testStoreForKey(t *testing.T, key *ecdsa.PrivateKey) *testKeyStore {
	t.Helper()
	return &testKeyStore{
		keyID: "package-test",
		key:   key,
		pem:   privateKeyPEMForTest(t, key),
	}
}

func privateKeyPEMForTest(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func fakeCosignScript() string {
	return `#!/bin/sh
if [ "$1" = "import-key-pair" ]; then
  prefix=""
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
      -o|--output-key-prefix) prefix="$2"; shift 2 ;;
      -k|--key) shift 2 ;;
      -y|--yes) shift ;;
      *) shift ;;
    esac
  done
  if [ -z "$prefix" ]; then prefix="import-cosign"; fi
  printf '%s\n' '-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----' 'fake' '-----END ENCRYPTED SIGSTORE PRIVATE KEY-----' > "${prefix}.key"
  printf '%s\n' '-----BEGIN PUBLIC KEY-----' 'fake' '-----END PUBLIC KEY-----' > "${prefix}.pub"
  exit 0
fi
exit 0
`
}

func installFakeTool(t *testing.T, name string, body string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func testSecureTempDir(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	realWD, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	dir, err := os.MkdirTemp(realWD, "lock-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func testTime() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

func digestString(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
