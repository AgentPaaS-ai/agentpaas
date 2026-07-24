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
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
	"github.com/AgentPaaS-ai/agentpaas/internal/dockerclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// ErrNilPrivateKey is a package-level sentinel for repeated validation failures.
var ErrNilPrivateKey = errors.New("private key must not be nil")

// ErrNilLock is a package-level sentinel for repeated validation failures.
var ErrNilLock = errors.New("lock must not be nil")

const noTlogSigningConfigJSON = `{"mediaType":"application/vnd.dev.sigstore.signingconfig.v0.2+json","rekorTlogConfig":{},"tsaConfig":{}}`

// LockSchemaVersion is the current agent.lock schema version.
const LockSchemaVersion = 2

const externalSignatureTimeout = 30 * time.Second

// AgentLock is the canonical, signed manifest for a packed agent.
// This is the exact review unit consumed by `agent run` and promotion.
type AgentLock struct {
	// SBOM is the generated SPDX document retained for bundle export. It is
	// deployed as a sidecar and intentionally excluded from signed JSON.
	SBOM []byte `json:"-"`
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
	// Computed at pack time from the project's policy.yaml.
	PolicyDigest string `json:"policy_digest"`
	// PolicyYAML is the raw policy.yaml content. Stored in the deployed directory
	// as a separate file (NOT in the signed lockfile JSON — it would change the
	// canonical signature map). Used at run time to compile the gateway config.
	// NOT included in lockCanonicalMap — it is deployed as a sidecar file.
	PolicyYAML []byte `json:"policy_yaml,omitempty"`
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
	// AgentYAML is the parsed agent.yaml (including LLM config). Stored as part
	// of the lockfile for runtime LLM credential resolution. nil when absent.
	AgentYAML *AgentYAML `json:"agent_yaml,omitempty"`
	// WorkflowYAML is the parsed workflow.yaml envelope (v0.3). nil when absent.
	WorkflowYAML *WorkflowYAML `json:"workflow_yaml,omitempty"`
	// Publisher holds the publisher identity block (schema v2+). nil when
	// the pack was performed without a publisher identity (local-only pack).
	Publisher *PublisherInfo `json:"publisher,omitempty"`
	// PublisherSignature is the ECDSA signature over the canonical JSON
	// of the lock (excluding both lockfile_signature and publisher_signature),
	// signed by the publisher identity key. Base64-encoded. Empty when no
	// publisher identity is configured.
	PublisherSignature string `json:"publisher_signature,omitempty"`
	// Provenance is the ordered list of provenance entries recording the
	// full lifecycle of this lockfile (created, updated, etc.). Empty for
	// local-only packs or pre-v2 schema. Each entry carries its own
	// signature from the publisher that created it.
	Provenance []ProvenanceEntry `json:"provenance,omitempty"`
	// Capabilities are declared capabilities from the agent.yaml manifest.
	// Stored verbatim in the lockfile; not schema-validated in v0.3.
	Capabilities []DeclaredCapability `json:"capabilities,omitempty"`
	// CommunicationSnapshot is the pre-built delegation communication snapshot
	// (BUG-040). Populated at pack time when workflow.yaml contains delegations.
	// It is signed into the lockfile and injected into the harness at run time.
	CommunicationSnapshot *delegation.CommunicationSnapshot `json:"communication_snapshot,omitempty"`
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

// PublisherInfo holds the public-facing identity information for the
// publisher who signed this lockfile. It is embedded in the lockfile's
// publisher block (schema v2+).
type PublisherInfo struct {
	// Name is the display label (GitHub-style slug).
	Name string `json:"name"`
	// Fingerprint is the hex-encoded SHA-256 of the DER-encoded SPKI of
	// the publisher's ECDSA P-256 public key.
	Fingerprint string `json:"fingerprint"`
	// PublicKeyPEM is the PEM-encoded SPKI public key.
	PublicKeyPEM string `json:"public_key_pem"`
	// SignedAt is the wall-clock time the publisher signature was created.
	SignedAt time.Time `json:"signed_at"`
}

// ProvenanceEntry records a single lifecycle event for an agent lockfile
// (created, updated, etc.). Each entry is independently signed by the
// publisher that performed the action.
type ProvenanceEntry struct {
	// Action is the lifecycle action (e.g. "created", "updated").
	Action string `json:"action"`
	// PublisherFingerprint is the fingerprint of the publisher that
	// performed the action.
	PublisherFingerprint string `json:"publisher_fingerprint"`
	// PublisherName is the display name of the publisher.
	PublisherName string `json:"publisher_name"`
	// PublisherPublicKeyPEM is the public key PEM of the publisher.
	PublisherPublicKeyPEM string `json:"publisher_public_key_pem"`
	// AgentName is the agent name at the time of the action.
	AgentName string `json:"agent_name"`
	// AgentVersion is the agent version at the time of the action.
	AgentVersion string `json:"agent_version"`
	// ParentLockDigest is the LockDigest of the parent lock this entry
	// was built on top of. Empty for the initial "created" action.
	ParentLockDigest string `json:"parent_lock_digest"`
	// ParentBundleDigest is the SHA-256 of the parent shared bundle.
	// Empty for the initial "created" action.
	ParentBundleDigest string `json:"parent_bundle_digest"`
	// ParentPolicyDigest is the policy digest of the parent lock.
	// Empty for the initial "created" action.
	ParentPolicyDigest string `json:"parent_policy_digest"`
	// PolicyDelta records changes to policy between parent and child.
	// nil when there is no change or for the initial "created" action.
	PolicyDelta *PolicyDelta `json:"policy_delta,omitempty"`
	// Timestamp is the wall-clock time the action was performed.
	Timestamp time.Time `json:"timestamp"`
	// EntrySignature is the ECDSA signature over the canonical
	// representation of this entry (excluding entry_signature), signed by
	// the publisher that performed the action. Base64-encoded.
	EntrySignature string `json:"entry_signature"`
}

// PolicyDelta records additions and removals to policy between a parent
// lock and its child (update).
type PolicyDelta struct {
	// EgressAdded is the list of egress destinations added.
	EgressAdded []string `json:"egress_added,omitempty"`
	// EgressRemoved is the list of egress destinations removed.
	EgressRemoved []string `json:"egress_removed,omitempty"`
	// CredentialsAdded is the list of credential names added.
	CredentialsAdded []string `json:"credentials_added,omitempty"`
	// CredentialsRemoved is the list of credential names removed.
	CredentialsRemoved []string `json:"credentials_removed,omitempty"`
	// MCPToolsAdded is the list of MCP tools added.
	MCPToolsAdded []string `json:"mcp_tools_added,omitempty"`
	// MCPToolsRemoved is the list of MCP tools removed.
	MCPToolsRemoved []string `json:"mcp_tools_removed,omitempty"`
	// ModelRoutesAdded is the list of model route keys added.
	ModelRoutesAdded []string `json:"model_routes_added,omitempty"`
	// ModelRoutesRemoved is the list of model route keys removed.
	ModelRoutesRemoved []string `json:"model_routes_removed,omitempty"`
	// RoutedRunChanged is true when the routed_run block was added or removed.
	RoutedRunChanged bool `json:"routed_run_changed,omitempty"`
}

// NewSignedTestLock generates an ECDSA P-256 key pair and creates a signed
// AgentLock with the given agent name and optional policy YAML bytes.
// If policyYAML is non-empty, it is parsed, validated, and its canonical
// digest is stored in lock.PolicyDigest. The PolicyYAML sidecar is NOT set
// on the lock — callers must write it separately via RecordDeployment.
//
// This is exported for use by external test packages (e.g., internal/daemon).
func NewSignedTestLock(agentName string, policyYAML []byte) (*AgentLock, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("encode public key: %w", err)
	}

	seedDigest := func(seed string) string {
		sum := sha256.Sum256([]byte(seed))
		return hex.EncodeToString(sum[:])
	}

	var policyDigest string
	if len(policyYAML) > 0 {
		policyDigest, err = ComputePolicyDigest(policyYAML)
		if err != nil {
			return nil, fmt.Errorf("compute policy digest: %w", err)
		}
	}

	lock := &AgentLock{
		SchemaVersion:        LockSchemaVersion,
		AgentName:            agentName,
		AgentVersion:         "0.1.0",
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:" + seedDigest("base"),
		HarnessVersion:       "test",
		BuildInputDigest:     seedDigest("input"),
		ImageDigest:          seedDigest("image"),
		SBOMDigest:           seedDigest("sbom"),
		PolicyDigest:         policyDigest,
		PolicyYAML:           policyYAML,
		PackageAID:           string(pubPEM),
		PublicKeyFingerprint: PublicKeyFingerprint(&key.PublicKey),
		SBOMReferrer:         "oci://agentpaas-test:latest#sbom",
		SignatureReferrer:    "cosign://agentpaas-test:latest",
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: time.Unix(1_700_000_000, 0).UTC(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	// Sign the lockfile.
	lock.LockfileSignature = ""
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	digest := sha256.Sum256(canonical)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("sign lock: %w", err)
	}
	lock.LockfileSignature = base64.StdEncoding.EncodeToString(sig)

	return lock, nil
}

// NewSignedTestLockWithLLM creates a signed test lock that includes an
// AgentYAML with an LLM credential. This is for testing installed-agent
// flows where the signed lock's LLM credential must be present.
func NewSignedTestLockWithLLM(agentName string, policyYAML []byte, llmCredentialName string) (*AgentLock, error) {
	lock, err := NewSignedTestLock(agentName, policyYAML)
	if err != nil {
		return nil, fmt.Errorf("new signed test lock with llm: %w", err)
	}
	lock.AgentYAML = &AgentYAML{
		Name:    agentName,
		Version: lock.AgentVersion,
		LLM: LLMConfig{
			Provider:   "openrouter",
			Model:      "anthropic/claude-sonnet-4",
			Credential: llmCredentialName,
		},
	}
	// Re-sign because AgentYAML changes the canonical JSON.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("encode public key: %w", err)
	}
	lock.PackageAID = string(pubPEM)
	lock.PublicKeyFingerprint = PublicKeyFingerprint(&key.PublicKey)
	lock.LockfileSignature = ""
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	digest := sha256.Sum256(canonical)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("sign lock: %w", err)
	}
	lock.LockfileSignature = base64.StdEncoding.EncodeToString(sig)
	return lock, nil
}

// SignLockfileWithKey sets lockfile_signature on lock using the package AID
// private key. The lock must have LockfileSignature cleared before calling.
// Intended for tests and bundle fixtures that mutate lock fields after creation.
func SignLockfileWithKey(lock *AgentLock, key *ecdsa.PrivateKey) error {
	if lock == nil {
		return ErrNilLock
	}
	if key == nil {
		return ErrNilPrivateKey
	}
	lock.LockfileSignature = ""
	lock.PackageAID = ""
	lock.PublicKeyFingerprint = ""
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		return fmt.Errorf("encode public key: %w", err)
	}
	lock.PackageAID = string(pubPEM)
	lock.PublicKeyFingerprint = PublicKeyFingerprint(&key.PublicKey)
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return fmt.Errorf("canonical JSON: %w", err)
	}
	digest := sha256.Sum256(canonical)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return fmt.Errorf("sign lock: %w", err)
	}
	lock.LockfileSignature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// SignProvenanceEntryWithKey sets entry_signature on e using the publisher
// private key. Intended for tests and bundle fixtures.
func SignProvenanceEntryWithKey(e *ProvenanceEntry, key *ecdsa.PrivateKey) error {
	if e == nil {
		return errors.New("provenance entry must not be nil")
	}
	if key == nil {
		return ErrNilPrivateKey
	}
	e.EntrySignature = ""
	canonical, err := provenanceEntryCanonical(e)
	if err != nil {
		return fmt.Errorf("sign provenance entry with key: %w", err)
	}
	digest := sha256.Sum256(canonical)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return fmt.Errorf("sign provenance entry: %w", err)
	}
	e.EntrySignature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// SignPublisherWithKey sets publisher_signature on lock using the publisher
// private key. The lock must have a publisher block. Intended for tests and
// bundle fixtures.
func SignPublisherWithKey(lock *AgentLock, key *ecdsa.PrivateKey) error {
	if lock == nil {
		return ErrNilLock
	}
	if key == nil {
		return ErrNilPrivateKey
	}
	if lock.Publisher == nil {
		return errors.New("lock has no publisher block")
	}
	lock.PublisherSignature = ""
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return fmt.Errorf("sign publisher with key: %w", err)
	}
	digest := sha256.Sum256(canonical)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return fmt.Errorf("sign publisher: %w", err)
	}
	lock.PublisherSignature = base64.StdEncoding.EncodeToString(sig)
	return nil
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
	// PolicyYAML is the raw policy.yaml file contents. If nil/empty (no policy.yaml
	// in the project), the lockfile's PolicyDigest is left empty for backward compat.
	PolicyYAML []byte
	// PublisherKeyStore is the identity keystore for publisher identity
	// operations (signing the publisher block and provenance entries). If nil,
	// the lock is produced without a publisher block (local-only pack).
	// This may be the same underlying store as KeyStore since the identity
	// keystore holds all key types.
	PublisherKeyStore identity.KeyStore
	// ProjectDir is the agent project directory; used to detect lineage.json
	// for fork-aware provenance chain append.
	ProjectDir string
	// WorkflowYAML is the parsed workflow.yaml (v0.3). nil when absent.
	// When set and delegations are non-empty, a CommunicationSnapshot is
	// built and stored in the lock (BUG-040).
	WorkflowYAML *WorkflowYAML
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

// signMaxRetries is the total number of sign attempts for production refs.
const signMaxRetries = 3

// signRetryBaseDelay is the initial backoff delay between retries.
const signRetryBaseDelay = 2 * time.Second

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
		return "", fmt.Errorf("sign image: %w", err)
	}

	localRef := isLocalRegistryRef(imageRef)
	maxAttempts := 1
	if !localRef {
		maxAttempts = signMaxRetries // production ref: retry on transient Rekor failures
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		referrer, err = signImageOnce(ctx, imageRef, keyPath)
		if err == nil {
			return referrer, nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		if !isRetryableSignError(err) {
			break
		}
		backoff := signRetryBaseDelay * time.Duration(1<<(attempt-1)) // 2s, 4s
		log.Printf("pack: sign attempt %d failed (transient), retrying in %v: %v", attempt, backoff, err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return "", fmt.Errorf("sign image: %w", ctx.Err())
		}
	}
	return "", lastErr
}

// signImageOnce performs a single cosign sign attempt.
func signImageOnce(ctx context.Context, imageRef, keyPath string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, externalSignatureTimeout)
	defer cancel()

	signArgs, cleanupConfig, err := buildCosignSignArgs(imageRef, keyPath)
	if err != nil {
		return "", fmt.Errorf("sign image once: %w", err)
	}
	defer cleanupConfig()
	signArgs = append(signArgs, imageRef)
	cmd := exec.CommandContext(cmdCtx, "cosign", signArgs...)
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

// isRetryableSignError returns true if the sign error is likely transient
// (Rekor outage, network issue, temporary failure) and worth retrying.
func isRetryableSignError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, pattern := range retryableSignErrorPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// retryableSignErrorPatterns are error substrings that indicate a transient
// failure (Rekor/transparency-log outage, network issue) worth retrying.
var retryableSignErrorPatterns = []string{
	"rekor",
	"tlog",
	"transparency log",
	"fulcio",
	"http 500",
	"http 502",
	"http 503",
	"status 500",
	"status 502",
	"status 503",
	"500 ",
	"502 ",
	"503 ",
	"connection refused",
	"connection reset",
	"timeout",
	"timed out",
	"temporary failure",
	"eof",
	"i/o timeout",
	"no such host",
	"server closed",
}

// CreateAgentLock creates the canonical, signed agent.lock manifest.
func CreateAgentLock(ctx context.Context, cfg LockConfig) (*AgentLock, error) {
	if err := validateLockConfig(cfg); err != nil {
		return nil, fmt.Errorf("create agent lock: %w", err)
	}

	sbom, sbomDigest, err := GenerateSBOM(ctx, cfg.BuildResult.ImageRef)
	if err != nil {
		return nil, fmt.Errorf("create agent lock: %w", err)
	}

	privateKey, keyFile, cleanup, err := preparePackageSigningMaterial(cfg)
	if err != nil {
		return nil, fmt.Errorf("create agent lock: %w", err)
	}
	defer cleanup()

	signatureReferrer, err := SignImage(ctx, cfg.BuildResult.ImageRef, keyFile)
	if err != nil {
		return nil, fmt.Errorf("create agent lock: %w", err)
	}

	publicKeyPEM, err := publicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("create agent lock: %w", err)
	}

	policyDigest, err := ComputePolicyDigestWithRoute(cfg.PolicyYAML, routeNameFromAgentYAML(cfg.AgentYAML))
	if err != nil {
		return nil, fmt.Errorf("policy validation: %w", err)
	}

	lock := assembleAgentLock(cfg, sbom, sbomDigest, string(publicKeyPEM), privateKey, signatureReferrer, policyDigest)

	pubIdentity, err := loadRequiredPublisherIdentity(cfg)
	if err != nil {
		return nil, err
	}
	attachPublisherInfo(lock, pubIdentity)

	if err := attachLockProvenance(lock, cfg, pubIdentity); err != nil {
		return nil, err
	}

	if err := signAgentLock(lock, cfg); err != nil {
		return nil, err
	}

	return lock, nil
}

// preparePackageSigningMaterial loads the package AID key and writes a temporary
// cosign key file. Caller must invoke cleanup.
func preparePackageSigningMaterial(cfg LockConfig) (privateKey *ecdsa.PrivateKey, keyFile string, cleanup func(), err error) {
	keyMaterial, err := cfg.KeyStore.Load(cfg.KeyID)
	if err != nil {
		return nil, "", func() {}, fmt.Errorf("load package identity key: %w", err)
	}
	privateKey, privateKeyPEM, err := privateKeyFromMaterial(keyMaterial)
	if err != nil {
		return nil, "", func() {}, err
	}
	keyFile, cleanup, err = writeCosignSigningKey(privateKeyPEM)
	if err != nil {
		return nil, "", func() {}, err
	}
	return privateKey, keyFile, cleanup, nil
}

// assembleAgentLock builds the unsigned lock struct fields from pack inputs.
func assembleAgentLock(cfg LockConfig, sbom []byte, sbomDigest, packageAID string, privateKey *ecdsa.PrivateKey, signatureReferrer, policyDigest string) *AgentLock {
	var caps []DeclaredCapability
	if cfg.AgentYAML != nil {
		caps = cfg.AgentYAML.Capabilities
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
		SBOM:                 sbom,
		PolicyDigest:         policyDigest,
		PolicyYAML:           cfg.PolicyYAML,
		PackageAID:           packageAID,
		PublicKeyFingerprint: PublicKeyFingerprint(&privateKey.PublicKey),
		SBOMReferrer:         "oci://" + cfg.BuildResult.ImageRef + "#sbom@sha256:" + sbomDigest,
		SignatureReferrer:    signatureReferrer,
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: cfg.SourceDateEpoch.UTC(),
			BaseImagePinned: strings.Contains(cfg.BaseImageDigest, "@sha256:") || strings.HasPrefix(cfg.BaseImageDigest, "sha256:"),
			DepsLocked:      len(cfg.BuildResult.DepsLocked) > 0,
			TarOrder:        "sorted",
		},
		CreatedAt:    cfg.SourceDateEpoch.UTC(),
		AgentYAML:    cfg.AgentYAML,
		Capabilities: caps,
		WorkflowYAML: cfg.WorkflowYAML,
	}

	// BUG-040: Build and store the communication snapshot when workflow
	// delegations are declared. The snapshot is signed into the lockfile
	// and injected into the harness at run time.
	if cfg.WorkflowYAML != nil && len(cfg.WorkflowYAML.Delegations) > 0 {
		agentName := agentYAMLString(cfg.AgentYAML, "Name", "AgentName")
		snap, err := BuildCommunicationSnapshot(
			cfg.WorkflowYAML,
			agentName,                   // workflowID derived from agent name at pack time
			"default",                   // tenantID defaulted to "default" at pack time
			agentName,                   // callerDeploymentID placeholder (set at deploy time)
			agentName,                   // callerPackageName from lock
			cfg.BuildResult.ImageDigest, // callerPackageDigest from image digest
			0,                          // snapshotGeneration starts at 0
		)
		if err != nil {
			log.Printf("pack: build communication snapshot for agent %s: %v", agentName, err)
		} else {
			lock.CommunicationSnapshot = snap
		}
	}

	return lock
}

// loadRequiredPublisherIdentity loads the publisher identity. Publisher
// identity is REQUIRED — no agent may be packed without one.
func loadRequiredPublisherIdentity(cfg LockConfig) (*identity.PublisherIdentity, error) {
	if cfg.PublisherKeyStore == nil {
		return nil, errors.New("no publisher keystore configured — run 'agentpaas identity init' before packing agents")
	}
	pubIdentity, pubErr := identity.LoadPublisherIdentity(cfg.PublisherKeyStore)
	if pubErr != nil {
		if errors.Is(pubErr, identity.ErrNoPublisherIdentity) {
			return nil, errors.New("no publisher identity — run 'agentpaas identity init' before packing agents")
		}
		// Any other identity error also fails closed.
		return nil, fmt.Errorf("load publisher identity: %w", pubErr)
	}
	return pubIdentity, nil
}

func attachPublisherInfo(lock *AgentLock, pubIdentity *identity.PublisherIdentity) {
	if pubIdentity == nil {
		return
	}
	now := time.Now().UTC()
	lock.Publisher = &PublisherInfo{
		Name:         pubIdentity.Name,
		Fingerprint:  pubIdentity.Fingerprint,
		PublicKeyPEM: pubIdentity.PublicKeyPEM,
		SignedAt:     now,
	}
}

// attachLockProvenance records either a forked or created provenance entry.
func attachLockProvenance(lock *AgentLock, cfg LockConfig, pubIdentity *identity.PublisherIdentity) error {
	lineage, lineageErr := ReadLineage(cfg.ProjectDir)
	if lineageErr != nil && !errors.Is(lineageErr, ErrLineageNotFound) {
		return fmt.Errorf("%s: %w", errLineageCorrupt, lineageErr)
	}
	if lineage != nil {
		return attachForkedProvenance(lock, cfg, pubIdentity, lineage)
	}
	if pubIdentity != nil {
		return attachCreatedProvenance(lock, cfg, pubIdentity)
	}
	return nil
}

func attachForkedProvenance(lock *AgentLock, cfg LockConfig, pubIdentity *identity.PublisherIdentity, lineage *LineageFile) error {
	if pubIdentity == nil {
		return errors.New(errForkPackNeedsIdentity)
	}
	if err := VerifyLineageParentProvenance(&lineage.Parent); err != nil {
		return fmt.Errorf("%s: %w", errLineageCorrupt, err)
	}
	parentProv := lineage.Parent.Provenance
	if len(parentProv)+1 > maxProvenanceChainLength {
		return errors.New(errProvenanceChainCap)
	}
	parentPolicyYAML, err := base64.StdEncoding.DecodeString(lineage.Parent.PolicyYAMLB64)
	if err != nil {
		return fmt.Errorf("%s: decode parent policy: %w", errLineageCorrupt, err)
	}
	polDelta, err := policy.ComputeDelta(parentPolicyYAML, cfg.PolicyYAML)
	if err != nil {
		return fmt.Errorf("policy delta: %w", err)
	}
	now := time.Now().UTC()
	entry := ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  pubIdentity.Fingerprint,
		PublisherName:         pubIdentity.Name,
		PublisherPublicKeyPEM: pubIdentity.PublicKeyPEM,
		AgentName:             lock.AgentName,
		AgentVersion:          lock.AgentVersion,
		ParentLockDigest:      lineage.Parent.LockDigest,
		ParentBundleDigest:    lineage.Parent.BundleDigest,
		ParentPolicyDigest:    lineage.Parent.PolicyDigest,
		PolicyDelta:           policyDeltaFromPolicy(polDelta),
		Timestamp:             now,
	}
	if err := signLockProvenanceEntry(&entry, cfg.PublisherKeyStore); err != nil {
		return err
	}
	lock.Provenance = append(append([]ProvenanceEntry(nil), parentProv...), entry)
	return nil
}

func attachCreatedProvenance(lock *AgentLock, cfg LockConfig, pubIdentity *identity.PublisherIdentity) error {
	now := time.Now().UTC()
	entry := ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  pubIdentity.Fingerprint,
		PublisherName:         pubIdentity.Name,
		PublisherPublicKeyPEM: pubIdentity.PublicKeyPEM,
		AgentName:             lock.AgentName,
		AgentVersion:          lock.AgentVersion,
		ParentLockDigest:      "",
		ParentBundleDigest:    "",
		ParentPolicyDigest:    "",
		PolicyDelta:           nil,
		Timestamp:             now,
	}
	if err := signLockProvenanceEntry(&entry, cfg.PublisherKeyStore); err != nil {
		return err
	}
	lock.Provenance = []ProvenanceEntry{entry}
	return nil
}

func signLockProvenanceEntry(entry *ProvenanceEntry, publisherKeyStore identity.KeyStore) error {
	entryCanonical, err := provenanceEntryCanonical(entry)
	if err != nil {
		return fmt.Errorf("provenance entry canonical: %w", err)
	}
	entryDigest := sha256.Sum256(entryCanonical)
	entrySig, err := identity.SignAsPublisher(publisherKeyStore, entryDigest[:])
	if err != nil {
		return fmt.Errorf("sign provenance entry: %w", err)
	}
	entry.EntrySignature = base64.StdEncoding.EncodeToString(entrySig)
	return nil
}

// signAgentLock attaches the package AID lockfile signature and optional
// publisher signature over the canonical lock JSON.
func signAgentLock(lock *AgentLock, cfg LockConfig) error {
	// Sign the lock with the package AID key.
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	signature, err := cfg.KeyStore.Sign(cfg.KeyID, digest[:])
	if err != nil {
		return fmt.Errorf("sign agent.lock: %w", err)
	}
	lock.LockfileSignature = base64.StdEncoding.EncodeToString(signature)

	// Sign with publisher key (after AID signature, since publisher_signature
	// is excluded from the canonical map — order doesn't matter for the digest).
	if lock.Publisher != nil && cfg.PublisherKeyStore != nil {
		pubCanonical, err := canonicalJSON(lock)
		if err != nil {
			return err
		}
		pubDigest := sha256.Sum256(pubCanonical)
		pubSig, err := identity.SignAsPublisher(cfg.PublisherKeyStore, pubDigest[:])
		if err != nil {
			return fmt.Errorf("sign publisher: %w", err)
		}
		lock.PublisherSignature = base64.StdEncoding.EncodeToString(pubSig)
	}
	return nil
}

// canonicalJSON returns the canonical JSON encoding of the lockfile
// (sorted keys, no signature fields, no whitespace). Both lockfile_signature
// and publisher_signature are excluded.
func canonicalJSON(lock *AgentLock) ([]byte, error) {
	if lock == nil {
		return nil, ErrNilLock
	}
	return json.Marshal(lockCanonicalMap(lock, false))
}

// canonicalJSONFull returns the canonical JSON encoding of the lockfile
// WITH both signatures included. Used by LockDigest.
func canonicalJSONFull(lock *AgentLock) ([]byte, error) {
	if lock == nil {
		return nil, ErrNilLock
	}
	return json.Marshal(lockCanonicalMap(lock, true))
}

// LockfileCanonicalJSON returns canonical JSON bytes for agent.lock as stored in bundles.
func LockfileCanonicalJSON(lock *AgentLock) ([]byte, error) {
	return canonicalJSONFull(lock)
}

// VerifyAgentLock verifies an agent.lock manifest.
func VerifyAgentLock(lock *AgentLock, imageRef string) error {
	if lock == nil {
		return ErrNilLock
	}
	if lock.SchemaVersion != LockSchemaVersion && lock.SchemaVersion != 1 {
		return fmt.Errorf("unsupported lock schema version %d", lock.SchemaVersion)
	}
	// v1 lock: verify AID signature only.
	// v2 lock: verify both AID signature and (if present) publisher signature.
	if err := VerifyLockfileSignature(lock); err != nil {
		return fmt.Errorf("verify agent lock: %w", err)
	}
	if lock.SchemaVersion >= 2 && lock.Publisher != nil {
		if err := VerifyPublisherSignature(lock); err != nil {
			return fmt.Errorf("publisher signature: %w", err)
		}
		if err := VerifyProvenanceSignatures(lock); err != nil {
			return fmt.Errorf("provenance signatures: %w", err)
		}
		// Verify publisher fingerprint consistency.
		pub, err := PublicKeyFromPEM([]byte(lock.Publisher.PublicKeyPEM))
		if err != nil {
			return fmt.Errorf("publisher public key: %w", err)
		}
		recomputed := PublicKeyFingerprint(pub)
		if recomputed != lock.Publisher.Fingerprint {
			return fmt.Errorf("publisher fingerprint mismatch: got %s, computed %s",
				lock.Publisher.Fingerprint, recomputed)
		}
	}
	if err := verifyRequiredDigest("sbom_digest", lock.SBOMDigest); err != nil {
		return fmt.Errorf("verify agent lock: %w", err)
	}
	if err := verifyRequiredDigest("build_input_digest", lock.BuildInputDigest); err != nil {
		return fmt.Errorf("verify agent lock: %w", err)
	}
	if err := verifySBOMReferrer(lock); err != nil {
		return fmt.Errorf("verify agent lock: %w", err)
	}
	if strings.TrimSpace(imageRef) != "" {
		if err := verifyImageSignature(lock.PackageAID, imageRef); err != nil {
			return fmt.Errorf("verify agent lock: %w", err)
		}
	}
	return nil
}

// VerifyLockfileSignature verifies the lockfile's ECDSA signature
// against the AID public key embedded in the lockfile.
func VerifyLockfileSignature(lock *AgentLock) error {
	if lock == nil {
		return ErrNilLock
	}
	pub, err := PublicKeyFromPEM([]byte(lock.PackageAID))
	if err != nil {
		return fmt.Errorf("verify lockfile signature: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(lock.LockfileSignature)
	if err != nil {
		return fmt.Errorf("decode lockfile signature: %w", err)
	}
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return fmt.Errorf("verify lockfile signature: %w", err)
	}
	digest := sha256.Sum256(canonical)
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return errors.New("lockfile signature verification failed")
	}
	return nil
}

// VerifyPublisherSignature verifies the publisher signature in a v2+ lockfile.
// It verifies the ECDSA signature over the canonical JSON (excluding both
// lockfile_signature and publisher_signature) using the publisher's public key.
func VerifyPublisherSignature(lock *AgentLock) error {
	if lock == nil {
		return ErrNilLock
	}
	if lock.Publisher == nil {
		return nil // no publisher block, nothing to verify
	}
	if lock.PublisherSignature == "" {
		return errors.New("publisher_signature is empty but publisher block is present")
	}
	pub, err := PublicKeyFromPEM([]byte(lock.Publisher.PublicKeyPEM))
	if err != nil {
		return fmt.Errorf("parse publisher public key: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(lock.PublisherSignature)
	if err != nil {
		return fmt.Errorf("decode publisher signature: %w", err)
	}
	// Both lockfile_signature and publisher_signature are excluded from
	// canonicalJSON (lockCanonicalMap with includeSignatures=false).
	canonical, err := canonicalJSON(lock)
	if err != nil {
		return fmt.Errorf("verify publisher signature: %w", err)
	}
	digest := sha256.Sum256(canonical)
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return errors.New("publisher signature verification failed")
	}
	return nil
}

// VerifyProvenanceSignatures verifies that each provenance entry's
// entry_signature is valid against its publisher's public key. This checks
// the integrity of each provenance entry independently.
func VerifyProvenanceSignatures(lock *AgentLock) error {
	if lock == nil {
		return ErrNilLock
	}
	for i, e := range lock.Provenance {
		if e.EntrySignature == "" {
			return fmt.Errorf("provenance entry %d: entry_signature is empty", i)
		}
		pub, err := PublicKeyFromPEM([]byte(e.PublisherPublicKeyPEM))
		if err != nil {
			return fmt.Errorf("provenance entry %d: parse publisher public key: %w", i, err)
		}
		signature, err := base64.StdEncoding.DecodeString(e.EntrySignature)
		if err != nil {
			return fmt.Errorf("provenance entry %d: decode entry signature: %w", i, err)
		}
		canonical, err := provenanceEntryCanonical(&e)
		if err != nil {
			return fmt.Errorf("provenance entry %d: canonical: %w", i, err)
		}
		digest := sha256.Sum256(canonical)
		if !ecdsa.VerifyASN1(pub, digest[:], signature) {
			return fmt.Errorf("provenance entry %d: signature verification failed", i)
		}
	}
	return nil
}

// provenanceEntryCanonical returns the canonical JSON of a provenance entry
// excluding its entry_signature field.
func provenanceEntryCanonical(e *ProvenanceEntry) ([]byte, error) {
	m := map[string]interface{}{
		"action":                   e.Action,
		"publisher_fingerprint":    e.PublisherFingerprint,
		"publisher_name":           e.PublisherName,
		"publisher_public_key_pem": e.PublisherPublicKeyPEM,
		"agent_name":               e.AgentName,
		"agent_version":            e.AgentVersion,
		"parent_lock_digest":       e.ParentLockDigest,
		"parent_bundle_digest":     e.ParentBundleDigest,
		"parent_policy_digest":     e.ParentPolicyDigest,
		"timestamp":                e.Timestamp,
	}
	if e.PolicyDelta != nil {
		m["policy_delta"] = e.PolicyDelta
	}
	return json.Marshal(m)
}

// LockDigest returns the SHA-256 digest of the full canonical lockfile JSON
// including both signatures. This is the value that fork entries reference
// as parent_lock_digest.
func LockDigest(lock *AgentLock) string {
	if lock == nil {
		return ""
	}
	canonical, err := canonicalJSONFull(lock)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// PublicKeyFromPEM parses a PEM-encoded ECDSA P-256 public key.
func PublicKeyFromPEM(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes) // optional value; zero on miss
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
		return fmt.Errorf("write agent lock: %w", err)
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
		return nil, fmt.Errorf("read agent lock: %w", err)
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

func lockCanonicalMap(lock *AgentLock, includeSignatures bool) map[string]interface{} {
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
	if includeSignatures {
		m["lockfile_signature"] = lock.LockfileSignature
		m["publisher_signature"] = lock.PublisherSignature
	}
	// Publisher block: include when present.
	if lock.Publisher != nil {
		m["publisher"] = map[string]interface{}{
			"name":           lock.Publisher.Name,
			"fingerprint":    lock.Publisher.Fingerprint,
			"public_key_pem": lock.Publisher.PublicKeyPEM,
			"signed_at":      lock.Publisher.SignedAt,
		}
	}
	// Provenance array: include when present. Each entry's entry_signature
	// is INCLUDED in the canonical map because entries are signed independently
	// before the lock is signed.
	if len(lock.Provenance) > 0 {
		entries := make([]map[string]interface{}, 0, len(lock.Provenance))
		for _, e := range lock.Provenance {
			entryMap := map[string]interface{}{
				"action":                   e.Action,
				"publisher_fingerprint":    e.PublisherFingerprint,
				"publisher_name":           e.PublisherName,
				"publisher_public_key_pem": e.PublisherPublicKeyPEM,
				"agent_name":               e.AgentName,
				"agent_version":            e.AgentVersion,
				"parent_lock_digest":       e.ParentLockDigest,
				"parent_bundle_digest":     e.ParentBundleDigest,
				"parent_policy_digest":     e.ParentPolicyDigest,
				"timestamp":                e.Timestamp,
				"entry_signature":          e.EntrySignature,
			}
			if e.PolicyDelta != nil {
				entryMap["policy_delta"] = e.PolicyDelta
			}
			entries = append(entries, entryMap)
		}
		m["provenance"] = entries
	}
	// Capabilities array: include when present. Sort by ID for determinism
	// so the signed canonical form is reproducible.
	if len(lock.Capabilities) > 0 {
		sorted := make([]DeclaredCapability, len(lock.Capabilities))
		copy(sorted, lock.Capabilities)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
		capEntries := make([]map[string]interface{}, 0, len(sorted))
		for _, c := range sorted {
			capEntries = append(capEntries, map[string]interface{}{
				"id":          c.ID,
				"description": c.Description,
			})
		}
		m["capabilities"] = capEntries
	}
	if lock.AgentYAML != nil {
		m["agent_yaml"] = lock.AgentYAML
	}
	if lock.WorkflowYAML != nil {
		m["workflow_yaml"] = lock.WorkflowYAML
	}
	if lock.CommunicationSnapshot != nil {
		m["communication_snapshot"] = lock.CommunicationSnapshot
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
			return nil, nil, fmt.Errorf("private key from material: %w", err)
		}
		pkcs8PEM, err := privateKeyPEM(key)
		if err != nil {
			return nil, nil, fmt.Errorf("private key from material: %w", err)
		}
		return key, pkcs8PEM, nil
	default:
		bytes, ok := exportedBytesField(material)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported key material type %T", material)
		}
		key, err := parsePrivateKeyPEM(bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("private key from material: %w", err)
		}
		pkcs8PEM, err := privateKeyPEM(key)
		if err != nil {
			return nil, nil, fmt.Errorf("private key from material: %w", err)
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
	block, _ := pem.Decode(pemBytes) // optional value; zero on miss
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

// isLocalRegistryRef returns true if the image reference points to a local registry.
func isLocalRegistryRef(imageRef string) bool {
	// Extract the registry host from the image ref.
	// Image refs: [host[:port]/]path[:tag][@digest]
	// The host is everything before the first "/".
	host := imageRef
	if idx := strings.Index(imageRef, "/"); idx > 0 {
		host = imageRef[:idx]
	}
	// Check if the host is exactly localhost or 127.0.0.1 (with optional port)
	// Must be a prefix match on the host component, not a substring of the whole ref.
	if h, _, err := net.SplitHostPort(host); err == nil { // intentionally ignored (reviewed)
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// buildCosignSignArgs constructs cosign sign CLI args for imageRef.
// Local registry refs suppress Rekor/tlog upload via --signing-config; production refs use cosign defaults.
func buildCosignSignArgs(imageRef, keyPath string) (args []string, cleanup func(), err error) {
	cleanup = func() {}
	args = []string{"sign", "--key", keyPath, "--yes"}
	if isLocalRegistryRef(imageRef) {
		signingConfigPath, cleanupConfig, cfgErr := ensureNoTlogSigningConfig()
		if cfgErr != nil {
			return nil, nil, cfgErr
		}
		cleanup = cleanupConfig
		args = append(args, "--signing-config", signingConfigPath, "--allow-insecure-registry")
	}
	return args, cleanup, nil
}

// buildCosignVerifyArgs constructs cosign verify CLI args for imageRef.
// Local registry refs skip tlog verification; production refs require Rekor transparency log.
func buildCosignVerifyArgs(imageRef, pubKeyPath string) []string {
	verifyArgs := []string{"verify"}
	if isLocalRegistryRef(imageRef) {
		verifyArgs = append(verifyArgs, "--insecure-ignore-tlog", "--allow-insecure-registry")
	}
	verifyArgs = append(verifyArgs, "--key", pubKeyPath, imageRef)
	return verifyArgs
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
	cleanup := func() { _ = os.Remove(path) } // best-effort remove
	if _, err := f.WriteString(noTlogSigningConfigJSON); err != nil {
		_ = f.Close() // best-effort close
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
	cleanup := func() { _ = os.RemoveAll(tmpDir) } // best-effort remove

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
		return fmt.Errorf("verify sbomreferrer: %w", err)
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
		return fmt.Errorf("verify image signature: %w", err)
	}
	defer cleanup()

	cmdCtx, cancel := context.WithTimeout(context.Background(), externalSignatureTimeout)
	defer cancel()
	verifyArgs := buildCosignVerifyArgs(imageRef, pubFile)
	cmd := exec.CommandContext(cmdCtx, "cosign", verifyArgs...)
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
		return "", nil, fmt.Errorf("write temp public key: %w", err)
	}
	f, err := os.CreateTemp("", "agentpaas-package-pub-*.pem")
	if err != nil {
		return "", nil, fmt.Errorf("create temp public key: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) } // best-effort remove
	if _, err := f.Write(keyPEM); err != nil {
		_ = f.Close() // best-effort close
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
		return fmt.Errorf("validate secure path: %w", err)
	}
	resolved = filepath.Clean(resolved)
	for _, protected := range []string{"/etc", "/usr", "/bin", "/sbin"} {
		if resolved == protected || strings.HasPrefix(resolved, protected+string(os.PathSeparator)) {
			return fmt.Errorf("path is in protected system directory: %s", path)
		}
	}
	// Reject symlinks in the ORIGINAL clean path, not the resolved path.
	// resolvePathSymlinks() resolves symlinks away, so checking the resolved
	// path would never find them (security bug caught by adversary test).
	if err := rejectSymlinkComponents(clean, mustExist); err != nil {
		return fmt.Errorf("validate secure path: %w", err)
	}
	return nil
}

func resolvePathSymlinks(path string, mustExist bool) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve symlinks in %s: %w", path, err)
	}
	if mustExist {
		return "", fmt.Errorf("resolve symlinks in %s: %w", path, err)
	}
	parent := filepath.Dir(path)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		return "", fmt.Errorf("resolve parent symlinks in %s: %w", parent, err)
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

func rejectSymlinkComponents(path string, mustExist bool) error {
	// macOS has standard system-level symlinks (/var → /private/var,
	// /tmp → /private/tmp, /etc → /private/etc) that are safe to traverse.
	// Only reject symlinks that are NOT in this known-safe set.
	safeSystemSymlinks := map[string]bool{
		"/var": true, "/tmp": true, "/etc": true,
	}
	current := string(os.PathSeparator)
	parts := strings.Split(strings.TrimPrefix(path, string(os.PathSeparator)), string(os.PathSeparator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && !mustExist && i == len(parts)-1 {
				return nil
			}
			return fmt.Errorf("lstat %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if safeSystemSymlinks[current] {
				continue
			}
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

// routeNameFromAgentYAML extracts the LLM route name from agent.yaml.
// Returns empty string when agent.yaml is nil or has no route configured
// (legacy v1.0 path — no route validation needed).
func routeNameFromAgentYAML(agentYAML *AgentYAML) string {
	if agentYAML == nil {
		return ""
	}
	return agentYAML.LLM.Route
}

// ComputePolicyDigest parses, validates, and computes the SHA-256 digest of
// the policy YAML. Returns empty string if yamlBytes is nil/empty (no policy
// in project — backward compat). Returns error if parsing fails or
// validation finds errors.
func ComputePolicyDigest(yamlBytes []byte) (string, error) {
	return ComputePolicyDigestWithRoute(yamlBytes, "")
}

// ComputePolicyDigestWithRoute is like ComputePolicyDigest but also performs
// route/candidate validation using the route name from agent.yaml.
// When routeName is empty, route-name-dependent checks are skipped (legacy v1.0 path).
func ComputePolicyDigestWithRoute(yamlBytes []byte, routeName string) (string, error) {
	if len(yamlBytes) == 0 {
		return "", nil
	}
	parsed, err := policy.ParsePolicy(bytes.NewReader(yamlBytes))
	if err != nil {
		return "", fmt.Errorf("parse policy.yaml: %w", err)
	}
	var errs []policy.ValidationError
	if routeName != "" && parsed.IsSchema11() {
		errs = policy.ValidatePolicyWithRoute(parsed, routeName)
	} else {
		errs = policy.ValidatePolicy(parsed)
	}
	if policy.HasErrors(errs) {
		return "", fmt.Errorf("policy.yaml validation failed: %s", policyValidationErrorString(errs))
	}
	canonical, err := canonicalPolicyJSON(parsed)
	if err != nil {
		return "", fmt.Errorf("canonicalize policy: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// policyValidationErrorString formats the first few validation errors for
// inclusion in an error message.
func policyValidationErrorString(errs []policy.ValidationError) string {
	n := len(errs)
	if n > 3 {
		n = 3
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(errs[i].Error())
	}
	if len(errs) > 3 {
		fmt.Fprintf(&b, "; ... (%d more)", len(errs)-3)
	}
	return b.String()
}

// canonicalPolicyJSON marshals the parsed policy to canonical, sorted-key JSON
// for deterministic SHA-256 digest computation.
func canonicalPolicyJSON(p *policy.Policy) ([]byte, error) {
	if p == nil {
		return nil, errors.New("policy must not be nil")
	}
	return json.Marshal(p)
}
