package pack

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/parvezsyed/agentpaas/internal/dockerclient"
)

const noTlogSigningConfigJSON = `{"mediaType":"application/vnd.dev.sigstore.signingconfig.v0.2+json","rekorTlogConfig":{},"tsaConfig":{}}`

// LockSchemaVersion is the current agent.lock schema version.
const LockSchemaVersion = 1

const externalSignatureTimeout = 30 * time.Second

// AgentLock is the canonical, signed manifest for a packed agent.
// This is the exact review unit consumed by `agent run` and promotion.
type AgentLock struct {
	// SchemaVersion is the agent.lock schema version (currently 1).
	SchemaVersion int `json:"schema_version"`
	// AgentName is the agent name from agent.yaml.
	AgentName string `json:"agent_name"`
	// AgentVersion is the agent version from agent.yaml.
	AgentVersion string `json:"agent_version"`
	// Runtime is the detected/explicit runtime type (python, langgraph, crewai).
	Runtime string `json:"runtime"`
	// Platform is the target platform (e.g. "linux/arm64").
	Platform string `json:"platform"`
	// BaseImageDigest is the digest-pinned distroless base image.
	BaseImageDigest string `json:"base_image_digest"`
	// HarnessVersion is the version of the harness binary embedded as PID 1.
	HarnessVersion string `json:"harness_version"`
	// BuildInputDigest is the SHA-256 over the canonical build context.
	BuildInputDigest string `json:"build_input_digest"`
	// ImageDigest is the SHA-256 digest of the built OCI image.
	ImageDigest string `json:"image_digest"`
	// SBOMDigest is the SHA-256 digest of the SBOM (SPDX-json).
	SBOMDigest string `json:"sbom_digest"`
	// PolicyDigest is the SHA-256 digest of the policy.yaml.
	// Placeholder: empty string for P1 (policy package integration later).
	PolicyDigest string `json:"policy_digest"`
	// PackageAID is the Agent Identity Document - the public key PEM.
	PackageAID string `json:"package_aid"`
	// PublicKeyFingerprint is the SHA-256 fingerprint of the public key.
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
	// SBOMReferrer is the OCI referrer path for the SBOM artifact.
	SBOMReferrer string `json:"sbom_referrer,omitempty"`
	// SignatureReferrer is the OCI referrer path for the cosign signature.
	SignatureReferrer string `json:"signature_referrer,omitempty"`
	// Reproducibility holds build reproducibility metadata.
	Reproducibility ReproducibilityMeta `json:"reproducibility"`
	// LockfileSignature is the ECDSA signature over the canonical JSON
	// of this struct (with LockfileSignature omitted). Base64-encoded.
	LockfileSignature string `json:"lockfile_signature"`
	// CreatedAt is the wall-clock time the lockfile was created.
	// For reproducibility, this is set to SOURCE_DATE_EPOCH, not time.Now().
	CreatedAt time.Time `json:"created_at"`
}

// ReproducibilityMeta holds metadata for verifying build reproducibility.
type ReproducibilityMeta struct {
	// SourceDateEpoch is the fixed timestamp used for the build.
	SourceDateEpoch time.Time `json:"source_date_epoch"`
	// BaseImagePinned is true if the base image is digest-pinned.
	BaseImagePinned bool `json:"base_image_pinned"`
	// DepsLocked is true if dependencies were locked via uv.
	DepsLocked bool `json:"deps_locked"`
	// TarOrder is "sorted" for deterministic tar order.
	TarOrder string `json:"tar_order"`
}

// LockConfig controls the agent.lock generation process.
type LockConfig struct {
	// BuildResult is the result from BuildImage (T02).
	BuildResult *BuildResult
	// ScanResult is the result from ScanSecrets (T03).
	ScanResult *ScanResult
	// AgentYAML is the parsed agent.yaml.
	AgentYAML *AgentYAML
	// Runtime is the detected runtime type.
	Runtime RuntimeType
	// BaseImageDigest is the digest-pinned base image.
	BaseImageDigest string
	// HarnessVersion is the harness binary version.
	HarnessVersion string
	// Platform is the target platform.
	Platform string
	// SourceDateEpoch is the fixed timestamp.
	SourceDateEpoch time.Time
	// KeyStore is the identity keystore for package identity key signing.
	KeyStore identityKeyStore
	// KeyID is the package identity key ID to use for signing.
	KeyID string
}

// identityKeyStore is a minimal interface for signing (subset of identity.KeyStore).
// This avoids importing internal/identity directly (avoids circular deps).
type identityKeyStore interface {
	Sign(id interface{}, digest []byte) ([]byte, error)
	Load(id interface{}) (interface{}, error)
}

// GenerateSBOM runs syft to produce an SPDX-json SBOM for the built image.
// Returns the SBOM content and its SHA-256 digest.
func GenerateSBOM(ctx context.Context, imageRef string) (sbom []byte, digest string, err error) {
	if strings.TrimSpace(imageRef) == "" {
		return nil, "", errors.New("image ref must not be empty")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, externalSignatureTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "syft", "scan", "--scope", "all-layers", "--output", "spdx-json", imageRef)
	cmd.Env = append(os.Environ(), dockerHostEnv()...)
	output, err := cmd.Output()
	if cmdCtx.Err() != nil {
		return nil, "", fmt.Errorf("generate sbom: %w", cmdCtx.Err())
	}
	if err != nil {
		return nil, "", fmt.Errorf("generate sbom: %w", err)
	}

	sum := sha256.Sum256(output)
	return output, hex.EncodeToString(sum[:]), nil
}

// SignImage signs the built image with cosign using the package identity key.
// Returns the signature referrer path.
func SignImage(ctx context.Context, imageRef string, keyPath string) (referrer string, err error) {
	if strings.TrimSpace(imageRef) == "" {
		return "", errors.New("image ref must not be empty")
	}
	if strings.TrimSpace(keyPath) == "" {
		return "", errors.New("key path must not be empty")
	}
	if err := validateSecurePath(keyPath, true); err != nil {
		return "", err
	}

	cmdCtx, cancel := context.WithTimeout(ctx, externalSignatureTimeout)
	defer cancel()

	signingConfigPath, cleanupConfig, err := ensureNoTlogSigningConfig()
	if err != nil {
		return "", err
	}
	defer cleanupConfig()

	cmd := exec.CommandContext(cmdCtx, "cosign", "sign",
		"--key", keyPath,
		"--signing-config", signingConfigPath,
		"--allow-insecure-registry",
		"--yes", imageRef)
	cmd.Env = append(os.Environ(), dockerHostEnv()...)
	cmd.Env = append(cmd.Env, "COSIGN_PASSWORD=")
	output, err := cmd.CombinedOutput()
	if cmdCtx.Err() != nil {
		return "", fmt.Errorf("sign image: %w", cmdCtx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("sign image: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return "cosign://" + imageRef, nil
}

// CreateAgentLock creates the canonical, signed agent.lock manifest.
func CreateAgentLock(ctx context.Context, cfg LockConfig) (*AgentLock, error) {
	if err := validateLockConfig(cfg); err != nil {
		return nil, err
	}

	sbom, sbomDigest, err := GenerateSBOM(ctx, cfg.BuildResult.ImageRef)
	if err != nil {
		return nil, err
	}
	_ = sbom

	keyMaterial, err := cfg.KeyStore.Load(cfg.KeyID)
	if err != nil {
		return nil, fmt.Errorf("load package identity key: %w", err)
	}
	privateKey, privateKeyPEM, err := privateKeyFromMaterial(keyMaterial)
	if err != nil {
		return nil, err
	}

	keyFile, cleanup, err := writeCosignSigningKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	signatureReferrer, err := SignImage(ctx, cfg.BuildResult.ImageRef, keyFile)
	if err != nil {
		return nil, err
	}

	publicKeyPEM, err := publicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}

	lock := &AgentLock{
		SchemaVersion:        LockSchemaVersion,
		AgentName:            agentYAMLString(cfg.AgentYAML, "Name", "AgentName"),
		AgentVersion:         agentYAMLString(cfg.AgentYAML, "Version", "AgentVersion"),
		Runtime:              string(cfg.Runtime),
		Platform:             cfg.Platform,
		BaseImageDigest:      cfg.BaseImageDigest,
		HarnessVersion:       cfg.HarnessVersion,
		BuildInputDigest:     cfg.BuildResult.BuildInputDigest,
		ImageDigest:          cfg.BuildResult.ImageDigest,
		SBOMDigest:           sbomDigest,
		PolicyDigest:         "",
		PackageAID:           string(publicKeyPEM),
		PublicKeyFingerprint: PublicKeyFingerprint(&privateKey.PublicKey),
		SBOMReferrer:         "oci://" + cfg.BuildResult.ImageRef + "#sbom@sha256:" + sbomDigest,
		SignatureReferrer:    signatureReferrer,
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: cfg.SourceDateEpoch.UTC(),
			BaseImagePinned: strings.Contains(cfg.BaseImageDigest, "@sha256:") || strings.HasPrefix(cfg.BaseImageDigest, "sha256:"),
			DepsLocked:      len(cfg.BuildResult.DepsLocked) > 0,
			TarOrder:        "sorted",
		},
		CreatedAt: cfg.SourceDateEpoch.UTC(),
	}

	canonical, err := canonicalJSON(lock)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	signature, err := cfg.KeyStore.Sign(cfg.KeyID, digest[:])
	if err != nil {
		return nil, fmt.Errorf("sign agent.lock: %w", err)
	}
	lock.LockfileSignature = base64.StdEncoding.EncodeToString(signature)

	return lock, nil
}

// canonicalJSON returns the canonical JSON encoding of the lockfile
// (sorted keys, no LockfileSignature field, no whitespace).
func canonicalJSON(lock *AgentLock) ([]byte, error) {
	if lock == nil {
		return nil, errors.New("lock must not be nil")
	}
	return json.Marshal(lockCanonicalMap(lock, false))
}

// VerifyAgentLock verifies an agent.lock manifest.
func VerifyAgentLock(lock *AgentLock, imageRef string) error {
	if lock == nil {
		return errors.New("lock must not be nil")
	}
	if lock.SchemaVersion != LockSchemaVersion {
		return fmt.Errorf("unsupported lock schema version %d", lock.SchemaVersion)
	}
	if err := VerifyLockfileSignature(lock); err != nil {
		return err
	}
	if err := verifyRequiredDigest("sbom_digest", lock.SBOMDigest); err != nil {
		return err
	}
	if err := verifyRequiredDigest("build_input_digest", lock.BuildInputDigest); err != nil {
		return err
	}
	if err := verifySBOMReferrer(lock); err != nil {
		return err
	}
	if strings.TrimSpace(imageRef) != "" {
		if err := verifyImageSignature(lock.PackageAID, imageRef); err != nil {
			return err
		}
	}
	return nil
}

// VerifyLockfileSignature verifies the lockfile's ECDSA signature
// against the AID public key embedded in the lockfile.
func VerifyLockfileSignature(lock *AgentLock) error {
	if lock == nil {
		return errors.New("lock must not be nil")
	}
	pub, err := PublicKeyFromPEM([]byte(lock.PackageAID))
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(lock.LockfileSignature)
	if err != nil {
		return fmt.Errorf("decode lockfile signature: %w", err)
	}
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return errors.New("lockfile signature verification failed")
	}
	return nil
}

// PublicKeyFromPEM parses a PEM-encoded ECDSA P-256 public key.
func PublicKeyFromPEM(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("decode public key PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is %T, not ECDSA", parsed)
	}
	if pub.Curve != elliptic.P256() {
		return nil, errors.New("public key is not ECDSA P-256")
	}
	return pub, nil
}

// PublicKeyFingerprint computes the SHA-256 fingerprint of a public key.
func PublicKeyFingerprint(pub *ecdsa.PublicKey) string {
	if pub == nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// WriteAgentLock writes the agent.lock manifest as canonical JSON to a file.
func WriteAgentLock(lock *AgentLock, path string) error {
	if err := validateSecurePath(path, false); err != nil {
		return err
	}
	content, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		return fmt.Errorf("marshal agent.lock: %w", err)
	}
	return os.WriteFile(path, content, 0o600)
}

// ReadAgentLock reads and parses an agent.lock file.
func ReadAgentLock(path string) (*AgentLock, error) {
	if err := validateSecurePath(path, true); err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent.lock: %w", err)
	}
	var lock AgentLock
	if err := json.Unmarshal(content, &lock); err != nil {
		return nil, fmt.Errorf("parse agent.lock: %w", err)
	}
	return &lock, nil
}

func validateLockConfig(cfg LockConfig) error {
	if cfg.BuildResult == nil {
		return errors.New("build result must not be nil")
	}
	if strings.TrimSpace(cfg.BuildResult.ImageRef) == "" {
		return errors.New("build image ref must not be empty")
	}
	if strings.TrimSpace(cfg.BuildResult.ImageDigest) == "" {
		return errors.New("build image digest must not be empty")
	}
	if strings.TrimSpace(cfg.BuildResult.BuildInputDigest) == "" {
		return errors.New("build input digest must not be empty")
	}
	if cfg.KeyStore == nil {
		return errors.New("keystore must not be nil")
	}
	if strings.TrimSpace(cfg.KeyID) == "" {
		return errors.New("key ID must not be empty")
	}
	return nil
}

func lockCanonicalMap(lock *AgentLock, includeSignature bool) map[string]interface{} {
	if lock == nil {
		return nil
	}

	m := map[string]interface{}{
		"agent_name":             lock.AgentName,
		"agent_version":          lock.AgentVersion,
		"base_image_digest":      lock.BaseImageDigest,
		"build_input_digest":     lock.BuildInputDigest,
		"created_at":             lock.CreatedAt,
		"harness_version":        lock.HarnessVersion,
		"image_digest":           lock.ImageDigest,
		"package_aid":            lock.PackageAID,
		"platform":               lock.Platform,
		"policy_digest":          lock.PolicyDigest,
		"public_key_fingerprint": lock.PublicKeyFingerprint,
		"reproducibility": map[string]interface{}{
			"base_image_pinned": lock.Reproducibility.BaseImagePinned,
			"deps_locked":       lock.Reproducibility.DepsLocked,
			"source_date_epoch": lock.Reproducibility.SourceDateEpoch,
			"tar_order":         lock.Reproducibility.TarOrder,
		},
		"runtime":        lock.Runtime,
		"sbom_digest":    lock.SBOMDigest,
		"schema_version": lock.SchemaVersion,
	}
	if lock.SBOMReferrer != "" {
		m["sbom_referrer"] = lock.SBOMReferrer
	}
	if lock.SignatureReferrer != "" {
		m["signature_referrer"] = lock.SignatureReferrer
	}
	if includeSignature {
		m["lockfile_signature"] = lock.LockfileSignature
	}
	return m
}

func privateKeyFromMaterial(material interface{}) (*ecdsa.PrivateKey, []byte, error) {
	switch v := material.(type) {
	case *ecdsa.PrivateKey:
		pemBytes, err := privateKeyPEM(v)
		return v, pemBytes, err
	case []byte:
		key, err := parsePrivateKeyPEM(v)
		if err != nil {
			return nil, nil, err
		}
		pkcs8PEM, err := privateKeyPEM(key)
		if err != nil {
			return nil, nil, err
		}
		return key, pkcs8PEM, nil
	default:
		bytes, ok := exportedBytesField(material)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported key material type %T", material)
		}
		key, err := parsePrivateKeyPEM(bytes)
		if err != nil {
			return nil, nil, err
		}
		pkcs8PEM, err := privateKeyPEM(key)
		if err != nil {
			return nil, nil, err
		}
		return key, pkcs8PEM, nil
	}
}

func exportedBytesField(v interface{}) ([]byte, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil, false
	}
	field := rv.FieldByName("Bytes")
	if !field.IsValid() || field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	bytes := make([]byte, field.Len())
	reflect.Copy(reflect.ValueOf(bytes), field)
	return bytes, true
}

func parsePrivateKeyPEM(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("decode private key PEM")
	}
	// Try PKCS8 first (what privateKeyPEM generates and what issuer.go uses).
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Fall back to SEC1 (ECPrivateKey). The identity keystore (ca.go)
		// stores keys with x509.MarshalECPrivateKey, which produces SEC1 DER.
		sec1Key, sec1Err := x509.ParseECPrivateKey(block.Bytes)
		if sec1Err != nil {
			return nil, fmt.Errorf("parse private key (tried PKCS8: %v; SEC1: %v): %w", err, sec1Err, err)
		}
		parsed = sec1Key
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is %T, not ECDSA", parsed)
	}
	if key.Curve != elliptic.P256() {
		return nil, errors.New("private key is not ECDSA P-256")
	}
	return key, nil
}

func privateKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func publicKeyPEM(pub *ecdsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func dockerHostEnv() []string {
	host, err := dockerclient.ResolvedDockerHost()
	if err != nil || host == "" {
		if env := os.Getenv("DOCKER_HOST"); env != "" {
			return []string{"DOCKER_HOST=" + env}
		}
		return nil
	}
	return []string{"DOCKER_HOST=" + host}
}

func ensureNoTlogSigningConfig() (string, func(), error) {
	f, err := os.CreateTemp("", "agentpaas-signing-config-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create signing config: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.WriteString(noTlogSigningConfigJSON); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write signing config: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close signing config: %w", err)
	}
	return path, cleanup, nil
}

func writeCosignSigningKey(pkcs8PEM []byte) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "agentpaas-cosign-import-*")
	if err != nil {
		return "", nil, fmt.Errorf("create cosign import dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	srcPath := filepath.Join(tmpDir, "src.pem")
	if err := os.WriteFile(srcPath, pkcs8PEM, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write source key: %w", err)
	}

	outputPrefix := filepath.Join(tmpDir, "signing-key")
	cmd := exec.Command("cosign", "import-key-pair",
		"--key", srcPath,
		"--output-key-prefix", outputPrefix,
		"--yes")
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("import cosign key: %w: %s", err, strings.TrimSpace(string(output)))
	}

	keyPath := outputPrefix + ".key"
	if err := os.Chmod(keyPath, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("chmod cosign key: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(keyPath)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("resolve cosign key path: %w", err)
	}
	return realPath, cleanup, nil
}

func verifyRequiredDigest(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if strings.Contains(value, "\x00") || strings.Contains(value, "\n") {
		return fmt.Errorf("%s contains invalid characters", name)
	}
	return nil
}

func verifySBOMReferrer(lock *AgentLock) error {
	if lock.SBOMReferrer == "" || !filepath.IsAbs(lock.SBOMReferrer) {
		return nil
	}
	if err := validateSecurePath(lock.SBOMReferrer, true); err != nil {
		return err
	}
	content, err := os.ReadFile(lock.SBOMReferrer)
	if err != nil {
		return fmt.Errorf("read SBOM referrer: %w", err)
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != lock.SBOMDigest {
		return fmt.Errorf("SBOM digest mismatch: got %s want %s", got, lock.SBOMDigest)
	}
	return nil
}

func verifyImageSignature(packageAID string, imageRef string) error {
	pubFile, cleanup, err := writeTempPublicKey([]byte(packageAID))
	if err != nil {
		return err
	}
	defer cleanup()

	cmdCtx, cancel := context.WithTimeout(context.Background(), externalSignatureTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "cosign", "verify",
		"--insecure-ignore-tlog",
		"--allow-insecure-registry",
		"--key", pubFile, imageRef)
	output, err := cmd.CombinedOutput()
	if cmdCtx.Err() != nil {
		return fmt.Errorf("verify image signature: %w", cmdCtx.Err())
	}
	if err != nil {
		return fmt.Errorf("verify image signature: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeTempPublicKey(keyPEM []byte) (string, func(), error) {
	if _, err := PublicKeyFromPEM(keyPEM); err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", "agentpaas-package-pub-*.pem")
	if err != nil {
		return "", nil, fmt.Errorf("create temp public key: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.Write(keyPEM); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temp public key: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temp public key: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("resolve temp public key path: %w", err)
	}
	return realPath, cleanup, nil
}

func validateSecurePath(path string, mustExist bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	clean := filepath.Clean(path)
	if clean != path {
		return fmt.Errorf("path must be clean: %s", path)
	}
	for _, part := range strings.Split(clean, string(os.PathSeparator)) {
		if part == ".." {
			return fmt.Errorf("path must not contain '..': %s", path)
		}
	}
	resolved, err := resolvePathSymlinks(clean, mustExist)
	if err != nil {
		return err
	}
	resolved = filepath.Clean(resolved)
	for _, protected := range []string{"/etc", "/usr", "/bin", "/sbin"} {
		if resolved == protected || strings.HasPrefix(resolved, protected+string(os.PathSeparator)) {
			return fmt.Errorf("path is in protected system directory: %s", path)
		}
	}
	if err := rejectSymlinkComponents(resolved, mustExist); err != nil {
		return err
	}
	return nil
}

func resolvePathSymlinks(path string, mustExist bool) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve symlinks in %s: %w", path, err)
	}
	if mustExist {
		return "", fmt.Errorf("resolve symlinks in %s: %w", path, err)
	}
	parent := filepath.Dir(path)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", fmt.Errorf("resolve parent symlinks in %s: %w", parent, err)
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

func rejectSymlinkComponents(path string, mustExist bool) error {
	current := string(os.PathSeparator)
	parts := strings.Split(strings.TrimPrefix(path, string(os.PathSeparator)), string(os.PathSeparator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) && !mustExist && i == len(parts)-1 {
				return nil
			}
			return fmt.Errorf("lstat %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path must not contain symlink: %s", current)
		}
	}
	return nil
}

func agentYAMLString(agentYAML *AgentYAML, names ...string) string {
	if agentYAML == nil {
		return ""
	}
	rv := reflect.ValueOf(agentYAML)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return ""
	}
	for _, name := range names {
		field := rv.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
	}
	return ""
}
