package daemon

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas"
	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/binresolve"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

// logBestEffort logs a non-fatal cleanup/secondary error without changing control flow.
func logBestEffort(op string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: best-effort %s: %v\n", op, err)
	}
}


type packKeyStoreAdapter struct {
	store identity.KeyStore
}

func (a *packKeyStoreAdapter) Load(id interface{}) (interface{}, error) {
	keyID, ok := id.(identity.KeyID)
	if !ok {
		keyID = identity.KeyID(fmt.Sprint(id))
	}
	return a.store.Load(keyID)
}

func (a *packKeyStoreAdapter) Sign(id interface{}, digest []byte) ([]byte, error) {
	keyID, ok := id.(identity.KeyID)
	if !ok {
		keyID = identity.KeyID(fmt.Sprint(id))
	}
	return a.store.Sign(keyID, digest)
}

func (s *controlServer) Pack(ctx context.Context, req *controlv1.PackRequest) (*controlv1.PackResponse, error) {
	projectDir := req.GetAgentProjectPath()
	if projectDir == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_project_path is required")
	}
	if s.homePaths == nil {
		return nil, status.Error(codes.FailedPrecondition, "daemon home paths not configured")
	}

	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "resolve project path: %v", err)
	}

	det, err := pack.DetectProject(absProjectDir)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "detect project: %v", err)
	}

	agentYAML, err := pack.LoadAgentYAML(absProjectDir)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "load agent.yaml: %v", err)
	}

	agentName := req.GetAgentName()
	if agentName == "" && agentYAML != nil {
		agentName = agentYAML.Name
	}
	if agentName == "" {
		agentName = "default"
	}

	agentVersion := req.GetAgentVersion()
	if agentVersion == "" && agentYAML != nil {
		agentVersion = agentYAML.Version
	}
	if agentVersion == "" {
		agentVersion = "latest"
	}

	var policyYAML []byte
	policyPath := filepath.Join(absProjectDir, "policy.yaml")
	if data, err := os.ReadFile(policyPath); err == nil {
		policyYAML = data
	} else if !os.IsNotExist(err) {
		return nil, status.Errorf(codes.Internal, "read policy.yaml: %v", err)
	}

	imageTag := fmt.Sprintf("agentpaas/%s:%s", agentName, agentVersion)
	harnessPath := binresolve.HarnessBinary()
	if harnessPath != "" {
		if info, err := os.Stat(harnessPath); err == nil {
			fmt.Fprintf(os.Stderr, "daemon: pack using harness %s (modified %s)\n", harnessPath, info.ModTime().Format(time.RFC3339))
		} else {
			fmt.Fprintf(os.Stderr, "daemon: pack using harness %s (mtime unknown)\n", harnessPath)
		}
	}
	sdkDir := binresolve.SDKDir(harnessPath)
	// If the SDK is not on disk (brew-only install, release tarball without
	// python/), fall back to the SDK embedded in the binary.
	if sdkDir == "" {
		embeddedSDKDir, cleanup, err := agentpaas.ExtractEmbeddedSDKToTemp()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "SDK not found on disk and embedded SDK extraction failed: %v", err)
		}
		defer cleanup()
		sdkDir = embeddedSDKDir
	}
	cfg := pack.BuildConfig{
		ProjectDir:  absProjectDir,
		Runtime:     det.Runtime,
		ImageTag:    imageTag,
		HarnessPath: harnessPath,
		SDKDir:      sdkDir,
	}
	if req.GetBaseImage() != "" {
		cfg.BaseImage = req.GetBaseImage()
	}

	result, err := pack.BuildImage(ctx, cfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "pack failed: %v", err)
	}

	log.Printf("daemon: post-build verification passed for %s (digest: %s)", imageTag, result.ImageDigest)

	registryRef, err := pack.PushImageToLocalRegistry(ctx, result.ImageRef, agentName, agentVersion)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "push to local registry: %v", err)
	}
	result.ImageRef = registryRef

	keyStore, keyID, err := s.openPackageIdentityKey(ctx, agentName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "identity keystore: %v", err)
	}
	// Publisher identities are managed by the identity CLI in the macOS
	// Keychain. Package identity material remains in the daemon's encrypted
	// file keystore, but publisher signing must read the same Keychain service
	// or `identity init` will not affect subsequent packs in this daemon.
	var publisherKeyStore identity.KeyStore
	if store, storeErr := identity.NewKeychainKeyStore("agentpaas-daemon"); storeErr == nil {
		publisherKeyStore = store
	}

	lock, err := pack.CreateAgentLock(ctx, pack.LockConfig{
		BuildResult:       result,
		AgentYAML:         agentYAML,
		Runtime:           det.Runtime,
		BaseImageDigest:   cfg.BaseImage,
		HarnessVersion:    "embedded",
		Platform:          fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		SourceDateEpoch:   time.Unix(0, 0).UTC(),
		KeyStore:          &packKeyStoreAdapter{store: keyStore},
		KeyID:             string(keyID),
		PolicyYAML:        policyYAML,
		PublisherKeyStore: publisherKeyStore,
		ProjectDir:        absProjectDir,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create agent lock: %v", err)
	}

	if err := pack.RecordDeployment(s.homePaths.Home, agentName, lock); err != nil {
		return nil, status.Errorf(codes.Internal, "record deployment: %v", err)
	}

	s.recordAudit("pack", "cli", map[string]interface{}{
		"agent_name":    agentName,
		"agent_version": agentVersion,
		"image_digest":  result.ImageDigest,
		"image_ref":     result.ImageRef,
		"runtime":       det.Runtime,
	})

	return &controlv1.PackResponse{
		ImageDigest: result.ImageDigest,
		BuildLog:    fmt.Sprintf("Built %s, digest: %s", result.ImageRef, result.ImageDigest),
	}, nil
}

// gatewaySubnetFromIP derives the /16 subnet CIDR from a gateway IP address.
// Docker bridge networks typically use /16 subnets (e.g., 172.18.0.0/16).
// Returns empty string if the IP is invalid or not IPv4.
func gatewaySubnetFromIP(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return ""
	}
	ip4 := ip.To4()
	// Zero out the last two octets to get the /16 network address
	mask := net.IPv4Mask(0xFF, 0xFF, 0x00, 0x00)
	network := ip4.Mask(mask)
	return fmt.Sprintf("%s/16", network.String())
}

// waitForGateway waits until the gateway process is ready enough that the
// harness can open route traffic. On macOS/Colima, the host cannot TCP-dial
// container-network IPs (bridged ranging is isolated), so host Dial of :15021
// never succeeds even when the gateway is healthy. Instead we:
//  1. require a container IP on the internal network (attachment success)
//  2. scrape container logs for agentgateway's ready markers
//
// This keeps startup ordered without depending on host↔container L3 routes.
func waitForGateway(ctx context.Context, rt runtime.RuntimeDriver, gatewayID runtime.ContainerID, netID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var sawIP bool
	for time.Now().Before(deadline) {
		ip, err := rt.InspectContainerIP(ctx, gatewayID, netID)
		if err == nil && ip != "" {
			sawIP = true
			// Prefer log readiness markers; fall back to elapsed time after IP is known.
			if gatewayLogsIndicateReady(ctx, rt, gatewayID) {
				return nil
			}
			// Host may be able to dial on Linux. Try 15021 before next sleep.
			conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(ip, "15021"), 250*time.Millisecond)
			if dialErr == nil {
				_ = conn.Close() // best-effort close
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if sawIP {
		// IP assignment succeeded; treat markerless timeout as ready so agent start
		// can proceed. Harness retries connect on 7799 if still warming.
		return nil
	}
	return fmt.Errorf("gateway not ready after %s", timeout)
}

func gatewayLogsIndicateReady(ctx context.Context, rt runtime.RuntimeDriver, gatewayID runtime.ContainerID) bool {
	reader, err := rt.Logs(ctx, gatewayID, runtime.LogOptions{Tail: 200})
	if err != nil {
		return false
	}
	defer func() { _ = reader.Close() }() // best-effort close
	all, err := io.ReadAll(io.LimitReader(reader, 64*1024))
	if err != nil || len(all) == 0 {
		return false
	}
	text := string(all)
	// agentgateway v1.3.0 readiness markers observed in production logs.
	return strings.Contains(text, "started bind") ||
		strings.Contains(text, "marking server ready") ||
		strings.Contains(text, "Task 'state manager' complete")
}

// verifyDeployedAgent performs all verification steps on deployed agent
// artifacts BEFORE any Docker resources are created:
//  1. Immutability check (agent.lock.sha256, image.digest, source_digest)
//  2. Lockfile signature verification
//  3. Policy digest validation against deployed policy.yaml
//  4. Legacy lock detection (must fail unless AGENTPAAS_ALLOW_LEGACY_LOCK=1)
func verifyDeployedAgent(homeDir, agentName string, auditAppender audit.AuditAppender) error {
	// Step 1: Verify deployed immutability (lock hash, signature, image.digest, source_digest).
	if err := pack.VerifyDeployedIntegrity(homeDir, agentName, auditAppender); err != nil {
		return fmt.Errorf("deployed integrity check failed: %w", err)
	}

	// Step 2: Load the lock to check PolicyDigest.
	lockPath := filepath.Join(pack.DeployedAgentPath(homeDir, agentName), "agent.lock")
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		return fmt.Errorf("read agent.lock for verification: %w", err)
	}

	// Step 3: Verify lockfile signature (redundant with VerifyDeployedIntegrity
	// but included for defense-in-depth per spec).
	if err := pack.VerifyLockfileSignature(lock); err != nil {
		return fmt.Errorf("lockfile signature verification failed: %w", err)
	}

	// Step 4: Policy digest verification.
	if lock.PolicyDigest != "" {
		deployedDir := pack.DeployedAgentPath(homeDir, agentName)
		policyPath := filepath.Join(deployedDir, "policy.yaml")
		policyData, err := os.ReadFile(policyPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("policy.yaml missing but lock has policy_digest; repack required")
			}
			return fmt.Errorf("read deployed policy.yaml: %w", err)
		}
		computedDigest, err := pack.ComputePolicyDigest(policyData)
		if err != nil {
			return fmt.Errorf("compute policy digest: %w", err)
		}
		if computedDigest != lock.PolicyDigest {
			return fmt.Errorf("policy.yaml has been modified since pack; repack required")
		}
	} else {
		// Legacy lock without policy digest.
		if os.Getenv("AGENTPAAS_ALLOW_LEGACY_LOCK") != "1" {
			return fmt.Errorf("agent.lock has no policy_digest; repack required for v0.1.2")
		}
	}

	return nil
}

// validateCredentialsExist checks that all credentials declared in agent.yaml
// (LLM credential) and policy.yaml actually exist in the Keychain. This check
// runs BEFORE any Docker resources are created so missing credentials fail-closed
// with actionable guidance for the operator.
//
// For installed agents, the credential map is applied: the LOCAL secret name
// (from the manifest) is checked, not the declared ID.
//
// In test mode, uses s.secretStoreForTest. In production, uses KeychainStore.
func validateCredentialsExist(s *controlServer, agentName string, isInstalled bool, credentialMap map[string]string) error {
	// Resolve the secret store.
	var store secrets.SecretStore
	if s.secretStoreForTest != nil {
		store = s.secretStoreForTest
	} else {
		var err error
		store, err = secrets.NewKeychainStore(secretServiceName(s.homePaths.Home))
		if err != nil {
			// Keychain unavailable — fail closed.
			return fmt.Errorf("keychain unavailable: %w", err)
		}
	}

	var deployedDir string
	if isInstalled {
		// For installed agents, the state dir layout is different.
		name, pub8, ok := install.ParseInstalledAgentDir(agentName)
		if !ok {
			return fmt.Errorf("invalid installed agent ref %q", agentName)
		}
		dir, err := install.InstalledAgentPath(s.homePaths.State, name, pub8)
		if err != nil {
			return fmt.Errorf("resolve installed agent dir: %w", err)
		}
		deployedDir = dir
	} else {
		deployedDir = pack.DeployedAgentPath(s.homePaths.Home, agentName)
	}

	// Collect all credential IDs.
	credIDs := make(map[string]bool)

	// 1. LLM credential from agent.lock.
	lockPath := filepath.Join(deployedDir, "agent.lock")
	lock, err := pack.ReadAgentLock(lockPath)
	if err == nil && lock != nil && lock.AgentYAML != nil && lock.AgentYAML.LLM.Provider != "" {
		credName := lock.AgentYAML.LLM.Credential
		if credName != "" {
			credIDs[credName] = true
		}
	}

	// 2. Policy credentials from policy.yaml.
	policyPath := filepath.Join(deployedDir, "policy.yaml")
	policyData, perr := os.ReadFile(policyPath)
	if perr == nil && len(policyData) > 0 {
		parsed, perr := policy.ParsePolicy(bytes.NewReader(policyData))
		if perr == nil {
			for _, c := range parsed.Credentials {
				if c.ID != "" {
					credIDs[c.ID] = true
				}
			}
		}
	}

	// Verify each credential exists in the secret store.
	for id := range credIDs {
		// For installed agents, apply the credential map: check the local secret name.
		lookupName := id
		if isInstalled && credentialMap != nil {
			if localName, ok := credentialMap[id]; ok && localName != "" {
				lookupName = localName
			}
		}
		_, err := store.Get(context.Background(), lookupName)
		if err != nil {
			return fmt.Errorf("credential %q not found in keychain; run: agentpaas secret add %s", lookupName, lookupName)
		}
	}

	return nil
}

func (s *controlServer) Run(ctx context.Context, req *controlv1.RunRequest) (*controlv1.RunResponse, error) {
	agentName := req.GetAgentName()
	if agentName == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_name is required")
	}
	if s.homePaths == nil {
		return nil, status.Error(codes.FailedPrecondition, "daemon home paths not configured")
	}

	// B26: parse continuation / control fields but fail closed without mutation.
	if req.GetContinueRunId() != "" || req.GetRecoveryAction() != "" || req.GetRequestedAttemptLeaseMs() != 0 {
		return nil, notEnabledFailedPrecondition(
			"routed_run_continuation", "B35", "routed_run_continuation_not_enabled")
	}
	// B26: deployment invocation via Run is representational only.
	if strings.TrimSpace(req.GetDeploymentRef()) != "" {
		return nil, notEnabledFailedPrecondition(
			"deployment_invocation", "B28", "routed_run_invocation_not_enabled")
	}
	// Idempotency key alone on legacy Run is accepted as ignored additive field
	// (API-required only for InvokeDeployment).

	resolvedName, agentRefLabel, err := s.resolveDaemonAgentRef(agentName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	agentName = resolvedName

	// Enforce concurrent run limit before any verification or Docker resources.
	if s.activeRunCount() >= maxConcurrentRuns {
		return nil, status.Errorf(codes.ResourceExhausted,
			"concurrent run limit reached (%d/%d active); stop an existing run before starting a new one",
			s.activeRunCount(), maxConcurrentRuns)
	}

	// Detect installed (shared bundle) agents vs packed (local) agents.
	// Installed agents use a different verification and image digest path.
	isInstalled := false
	if _, _, ok := install.ParseInstalledAgentDir(agentName); ok { // intentionally ignored (reviewed)
		isInstalled = true
	}

	// B26: routed projects (Route or workflow.yaml) fail closed before Docker.
	if sig, derr := s.detectRoutedProject(agentName, isInstalled); derr != nil {
		return nil, status.Errorf(codes.Internal, "detect routed project: %v", derr)
	} else if sig != nil {
		return nil, s.failClosedRoutedRun(ctx, agentName, sig)
	}

	var imageDigest string
	var credentialMap map[string]string

	if isInstalled {
		// Installed agent verification path.
		if err := install.VerifyInstalledAgent(s.homePaths.State, agentName, s.auditWriter); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "installed agent verification failed: %v", err)
		}
		manifest, err := install.LoadManifestByRef(s.homePaths.State, agentName)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "load install manifest: %v", err)
		}
		imageDigest = manifest.LocalImageDigest
		credentialMap = manifest.CredentialMap
	} else {
		deployed, err := pack.LoadDeployedAgent(s.homePaths.Home, agentName)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "agent %q not deployed: %v (run pack first)", agentName, err)
		}
		// Verify deployed agent integrity, lockfile signature, and policy digest
		// BEFORE creating any Docker resources.
		if err := verifyDeployedAgent(s.homePaths.Home, agentName, s.auditWriter); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "deployed agent verification failed: %v", err)
		}
		imageDigest = deployed.ImageDigest
	}

	// Validate trigger payload JSON early (before CreateNetwork).
	// Invalid JSON must fail-closed as InvalidArgument without touching
	// any Docker resources.
	if triggerPayload := req.GetTriggerPayload(); len(triggerPayload) > 0 {
		var dummy map[string]any
		if err := json.Unmarshal(triggerPayload, &dummy); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid trigger payload JSON: %v", err)
		}
	}

	// Validate that all declared credentials exist in Keychain BEFORE creating
	// any Docker resources. Missing credentials fail-closed with actionable guidance.
	if err := validateCredentialsExist(s, agentName, isInstalled, credentialMap); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "docker runtime not available: %v", err)
	}

	// Verify Docker Engine version is patched (T00-B).
	// Known-vulnerable engines (< 29.5.1) fail before any Docker resources are created.
	if os.Getenv("AGENTPAAS_ALLOW_VULNERABLE_DOCKER") != "1" {
		if err := checkDockerEngineVersion(ctx, rt); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
		}
	}

	runID := generateRunID()
	netID, err := rt.CreateNetwork(ctx, runtime.NetworkSpec{
		Name:     runtime.NetworkName("internal", runID),
		Internal: true,
		Labels:   runtime.Labels(runtime.ResourceTypeNetInternal, runID),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create network: %v", err)
	}

	// Create egress network (non-internal — has internet access).
	egressNetID, err := rt.CreateNetwork(ctx, runtime.NetworkSpec{
		Name:     runtime.NetworkName("egress", runID),
		Internal: false,
		Labels:   runtime.Labels(runtime.ResourceTypeNetEgress, runID),
	})
	if err != nil {
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		return nil, status.Errorf(codes.Internal, "create egress network: %v", err)
	}

	// Create host audit directory for harness audit JSONL.
	// The container runs as UID 64000 (non-root). The bind mount exposes this
	// host directory to the container. We must ensure UID 64000 can write, so
	// we chmod 0777 after mkdir to defeat the process umask (MkdirAll applies
	// umask to the mode, yielding 0755 which denies write to "other").
	hostAuditDir := filepath.Join(s.homePaths.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(hostAuditDir, 0o777); err != nil {
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		return nil, status.Errorf(codes.Internal, "create audit dir: %v", err)
	}
	if err := os.Chmod(hostAuditDir, 0o777); err != nil {
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		return nil, status.Errorf(codes.Internal, "chmod audit dir: %v", err)
	}

	// Create gateway container (dual-homed: internal + egress).
	// The gateway config is compiled FROM THE AGENT'S OWN POLICY, not from a
	// global file. This ensures each agent gets egress rules matching its
	// policy.yaml.
	var gatewayBinds []string
	var gatewayConfigDir string

	// Read the agent's policy.yaml from the deployed/installed directory.
	var deployedDir string
	if isInstalled {
		name, pub8, ok := install.ParseInstalledAgentDir(agentName)
		if !ok {
			logBestEffort("parse installed agent dir", fmt.Errorf("invalid installed agent ref %q", agentName))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.InvalidArgument, "invalid installed agent ref %q", agentName)
		}
		var pathErr error
		deployedDir, pathErr = install.InstalledAgentPath(s.homePaths.State, name, pub8)
		if pathErr != nil {
			logBestEffort("resolve installed agent path", pathErr)
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "resolve installed agent path: %v", pathErr)
		}
	} else {
		deployedDir = pack.DeployedAgentPath(s.homePaths.Home, agentName)
	}
	agentPolicyPath := filepath.Join(deployedDir, "policy.yaml")

	policyData, err := os.ReadFile(agentPolicyPath)
	if err == nil && len(policyData) > 0 {
		// Parse and compile the policy into gateway config.
		parsedPolicy, err := policy.ParsePolicy(bytes.NewReader(policyData))
		if err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "parse agent policy: %v", err)
		}
		if errs := policy.ValidatePolicy(parsedPolicy); policy.HasErrors(errs) {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "validate agent policy: %v", errs)
		}
		compiled, err := policy.CompileGatewayConfig(parsedPolicy)
		if err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "compile gateway config: %v", err)
		}
		// Write compiled config to per-run directory.
		perRunConfigDir := filepath.Join(s.homePaths.State, "runs", runID, "gateway-config")
		if err := os.MkdirAll(perRunConfigDir, 0o700); err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "create gateway config dir: %v", err)
		}
		gatewayConfigPath := filepath.Join(perRunConfigDir, "config.yaml")
		if err := os.WriteFile(gatewayConfigPath, compiled, 0o600); err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "write gateway config: %v", err)
		}
		// Rewrite __agentpaas_secret:<id> placeholders in apiKey.keys with Keychain values
		// so agentgateway can validate ingress keys at startup/runtime.
		if err := s.rewriteGatewayConfigSecrets(gatewayConfigPath, parsedPolicy); err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.FailedPrecondition, "resolve gateway ingress secrets: %v", err)
		}
		gatewayBinds = []string{fmt.Sprintf("%s:/config.yaml:ro", gatewayConfigPath)}
		gatewayConfigDir = perRunConfigDir
	} else {
		// No policy.yaml in deployed dir — deny-all fallback.
		denyAllConfig := []byte("config:\n  dns:\n    lookupFamily: V4Only\nbinds: []\n")
		perRunConfigDir := filepath.Join(s.homePaths.State, "runs", runID, "gateway-config")
		if err := os.MkdirAll(perRunConfigDir, 0o700); err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "create gateway config dir: %v", err)
		}
		denyAllPath := filepath.Join(perRunConfigDir, "config.yaml")
		if err := os.WriteFile(denyAllPath, denyAllConfig, 0o600); err != nil {
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
			logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
			return nil, status.Errorf(codes.Internal, "write default-deny config: %v", err)
		}
		gatewayBinds = []string{fmt.Sprintf("%s:/config.yaml:ro", denyAllPath)}
		gatewayConfigDir = perRunConfigDir
	}

	gatewayID, err := rt.Create(ctx, runtime.ContainerSpec{
		Image:      runtime.GatewayImage,
		Command:    []string{"-f", "/config.yaml"},
		Labels:     runtime.Labels(runtime.ResourceTypeGateway, runID),
		NetworkIDs: []string{string(netID), string(egressNetID)}, // dual-homed
		Binds:      gatewayBinds,
		User:       "64000",
	})
	if err != nil {
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		return nil, status.Errorf(codes.Internal, "create gateway container: %v", err)
	}

	if err := rt.Start(ctx, gatewayID); err != nil {
		logBestEffort("Remove", rt.Remove(ctx, gatewayID, true))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		return nil, status.Errorf(codes.Internal, "start gateway container: %v", err)
	}

	// Wait for gateway readiness (Bug 021): with gateway-native HTTP routing
	// the harness connects immediately on the first agent.http/agent.llm call.
	// The gateway takes ~1-2s to start. Without this wait the harness gets
	// "connection refused" on port 7799. Poll the gateway's readiness port.
	// Skip in tests (AGENTPAAS_SKIP_GATEWAY_WAIT=1).
	if os.Getenv("AGENTPAAS_SKIP_GATEWAY_WAIT") == "" {
		if err := waitForGateway(ctx, rt, gatewayID, string(netID), 10*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: gateway readiness check: %v\n", err)
			// Non-fatal: proceed anyway — the agent will retry.
		}
	}

	// Discover gateway IP on internal network for HTTP proxy configuration.
	gatewayIP, err := rt.InspectContainerIP(ctx, gatewayID, string(netID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: discover gateway IP: %v\n", err)
		// Non-fatal: agent will use direct connections (which fail on internal network)
	}
	if gatewayIP != "" {
		// Validate the IP address to prevent env var injection.
		// Docker returns a valid IP, but we validate defensively.
		if ip := net.ParseIP(gatewayIP); ip == nil {
			fmt.Fprintf(os.Stderr, "daemon: gateway IP %q is not a valid IP address, skipping proxy env\n", gatewayIP)
			gatewayIP = ""
		}
	}

	proxyEnv := []string{
		"AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl",
		// Project files are copied to /app/ by the pack Dockerfile. The
		// harness default is /agent/main.py which does not exist.
		"AGENTPAAS_AGENT_PATH=/app/main.py",
	}
	if egressFirewallEnabled() {
		proxyEnv = append(proxyEnv, "AGENTPAAS_EGRESS_FIREWALL=1")
	} else {
		proxyEnv = append(proxyEnv, "AGENTPAAS_EGRESS_FIREWALL=0")
	}
	if gatewayIP != "" {
		gatewaySubnet := gatewaySubnetFromIP(gatewayIP)
		proxyEnv = append(proxyEnv,
			fmt.Sprintf("AGENTPAAS_GATEWAY_IP=%s", gatewayIP),
			fmt.Sprintf("AGENTPAAS_GATEWAY_SUBNET=%s", gatewaySubnet),
			// Gateway-native HTTP routing (Bug 021): harness rewrites
			// outbound HTTPS LLM URLs to plain HTTP against the gateway
			// and preserves the original Host header for route matching.
			fmt.Sprintf("AGENTPAAS_GATEWAY_URL=http://%s:7799", gatewayIP),
			// Forward proxy for non-LLM egress (Bug 021 regression fix):
			// The gateway-native routing only rewrites LLM provider URLs.
			// Agent code making direct HTTP calls to allowed egress domains
			// (e.g. wttr.in) needs HTTP_PROXY/HTTPS_PROXY to route through
			// the gateway, since the container is on an isolated network.
			// NO_PROXY includes the gateway IP so the harness's rewritten
			// LLM calls (already pointing at gateway:7799) are not
			// double-proxied.
			fmt.Sprintf("HTTP_PROXY=http://%s:7799", gatewayIP),
			fmt.Sprintf("HTTPS_PROXY=http://%s:7799", gatewayIP),
			fmt.Sprintf("http_proxy=http://%s:7799", gatewayIP),
			fmt.Sprintf("https_proxy=http://%s:7799", gatewayIP),
			fmt.Sprintf("NO_PROXY=localhost,127.0.0.1,%s", gatewayIP),
			fmt.Sprintf("no_proxy=localhost,127.0.0.1,%s", gatewayIP),
		)
	}

	// Write resolved credential values to a sidecar file that is bind-mounted
	// into the agent container. The harness reads this file at startup, loads
	// credential values into memory, and the daemon removes the file after the
	// run. The file is mounted read-only so the agent container cannot mutate
	// or delete it (BB-1: sidecar credentials file read-write mount fix).
	// NOTE: The file is mounted read-only (not read-write). The harness reads
	// it without deleting. If the harness crashes before reading, the container
	// is stopped and removed on failure, so the file is ephemeral.
	credsPath, credsFileWritten := s.writeCredentialsForRun(runID, deployedDir, gatewayConfigDir, credentialMap)

	// B27: Generate attempt ID early so it's available for journal key,
	// artifact workspace, and bind mounts before the container is created.
	attemptID := ""
	if aid, err := routedrun.NewAttemptID(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: generate attempt ID: %v\n", err)
	} else {
		attemptID = string(aid)
	}

	// Generate journal key for progress journal (B27 integration).
	// The key is saved to a 0600 file in the per-run attempt-secrets
	// directory and bind-mounted into the harness container. The harness
	// reads and deletes it before starting Python (sidecar pattern).
	var journalKey []byte
	journalKeyPath := ""
	if attemptID != "" {
		journalKeyPath = filepath.Join(s.homePaths.State, "runs", runID, "attempt-secrets", attemptID+".journal-key")
		journalKey = make([]byte, 32)
		if _, err := rand.Read(journalKey); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: generate journal key: %v\n", err)
			journalKeyPath = ""
			journalKey = nil
		} else if err := os.MkdirAll(filepath.Dir(journalKeyPath), 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: create attempt-secrets dir: %v\n", err)
			journalKeyPath = ""
			journalKey = nil
		} else if err := os.WriteFile(journalKeyPath, journalKey, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: save journal key: %v\n", err)
			journalKeyPath = ""
			journalKey = nil
		}
	}

	// Create artifact workspace directory (B27 integration).
	// Bind-mounted at /workspace/artifacts into the agent container.
	artifactDir := filepath.Join(s.homePaths.State, "runs", runID, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: create artifact dir: %v\n", err)
		artifactDir = ""
	}

	// Create journal host directory so tailer can observe harness writes.
	journalHostPath := ""
	if journalKey != nil && attemptID != "" {
		journalHostPath = filepath.Join(s.homePaths.State, "runs", runID, "journals", attemptID+".jsonl")
		if err := os.MkdirAll(filepath.Dir(journalHostPath), 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: create journal dir: %v\n", err)
			journalHostPath = ""
		}
	}

	agentBinds := []string{fmt.Sprintf("%s:/audit", hostAuditDir)}
	if credsFileWritten {
		agentBinds = append(agentBinds, fmt.Sprintf("%s:/agentpaas/credentials.json:ro", credsPath))
		proxyEnv = append(proxyEnv, "AGENTPAAS_CREDENTIALS_PATH=/agentpaas/credentials.json")
	}

	// B27: Bind-mount journal key, journal directory, and artifact workspace.
	// Pass env vars so the harness can locate and load the key file.
	// AGENTPAAS_RUN_ID is passed so the harness journal writer stamps
	// records with the correct run identity — the tailer verifies this.
	if journalKeyPath != "" && journalKey != nil {
		agentBinds = append(agentBinds, fmt.Sprintf("%s:/agentpaas/journal-key", journalKeyPath))
		proxyEnv = append(proxyEnv,
			"AGENTPAAS_JOURNAL_KEY_PATH=/agentpaas/journal-key",
			"AGENTPAAS_JOURNAL_PATH=/agentpaas/journals/"+attemptID+".jsonl",
			"AGENTPAAS_ATTEMPT_ID="+attemptID,
			"AGENTPAAS_RUN_ID="+runID,
		)
		if journalHostPath != "" {
			journalMountDir := filepath.Dir(journalHostPath)
			agentBinds = append(agentBinds, fmt.Sprintf("%s:/agentpaas/journals", journalMountDir))
		}
	}
	if artifactDir != "" {
		agentBinds = append(agentBinds, fmt.Sprintf("%s:/workspace/artifacts", artifactDir))
		proxyEnv = append(proxyEnv, "AGENTPAAS_ARTIFACT_DIR=/workspace/artifacts")
	}

	agentSpec := runtime.ContainerSpec{
		Labels:     runtime.LabelsWithAgentRef(runtime.ResourceTypeAgent, runID, agentRefLabel),
		NetworkIDs: []string{string(netID)},
		Binds:      agentBinds,
		Env:        proxyEnv,
	}
	if egressFirewallEnabled() {
		agentSpec.CapAdd = []string{"NET_ADMIN"}
	}

	var imageRef string
	if isInstalled {
		// Installed agents (from shared bundles) use a bare digest ref.
		// The image was loaded locally at install time and was never
		// pushed to the local registry. Using a bare sha256: digest
		// avoids network exposure and TOCTOU risk — the Docker daemon
		// resolves it directly from its local image store.
		imageRef = "sha256:" + strings.TrimPrefix(imageDigest, "sha256:")
	} else {
		imageRef = pack.LocalImageRef(agentName, imageDigest)
	}
	agentSpec.Image = imageRef
	containerID, err := rt.Create(ctx, agentSpec)
	if err != nil {
		logBestEffort("Remove", rt.Remove(ctx, gatewayID, true))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		if journalKeyPath != "" {
			logBestEffort("remove", os.RemoveAll(journalKeyPath))
		}
		return nil, status.Errorf(codes.Internal, "create container: %v", err)
	}

	if err := rt.Start(ctx, containerID); err != nil {
		logBestEffort("Remove", rt.Remove(ctx, containerID, true))
		logBestEffort("Remove", rt.Remove(ctx, gatewayID, true))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, egressNetID))
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, netID))
		if journalKeyPath != "" {
			logBestEffort("remove", os.RemoveAll(journalKeyPath))
		}
		return nil, status.Errorf(codes.Internal, "start container: %v", err)
	}

	tracked := &trackedRun{
		Container:        containerID,
		Network:          string(netID),
		EgressNetwork:    string(egressNetID),
		Gateway:          gatewayID,
		AuditDir:         hostAuditDir,
		GatewayConfigDir: gatewayConfigDir,
		AgentName:        agentName,
		StartedAt:        time.Now(),
		Status:           "running",
		InvokeDone:       make(chan struct{}),
		JournalKeyPath:   journalKeyPath,
		ArtifactDir:      artifactDir,
		JournalHostPath:  journalHostPath,
	}
	s.trackRunPtr(runID, tracked)
	if s.eventBus != nil {
		s.eventBus.RegisterRun(runID)
		s.eventBus.Publish(runID, trigger.EventRunStarted, map[string]interface{}{
			"agent_name":   agentName,
			"image_ref":    imageRef,
			"container_id": string(containerID),
			"network":      string(netID),
		})
	}

	// Start real-time audit tailer for live egress visibility.
	auditPath := filepath.Join(hostAuditDir, "harness-audit.jsonl")
	tracked.Tailer = newAuditTailer(auditPath, runID, s.auditWriter, s.auditIndex, s.eventBus)
	tracked.Tailer.start()

	// B27: Start progress journal tailer to observe harness writes and
	// persist checkpoints + progress metadata to the routed run store.
	if journalKey != nil && journalHostPath != "" && s.localStore != nil {
		tailer := routedrun.NewProgressTailer(
			journalHostPath, journalKey,
			s.localStore,
			routedrun.AttemptID(attemptID), routedrun.RunID(runID),
		)
		if s.auditWriter != nil {
			tailer.SetAuditAppender(s.auditWriter)
		}
		tailer.Start(context.Background())
		tracked.ProgressTailer = tailer
	}

	s.recordAudit("run_start", "cli", map[string]interface{}{
		"run_id":       runID,
		"agent_name":   agentName,
		"image_ref":    imageRef,
		"container_id": string(containerID),
		"network":      string(netID),
	})

	// Auto-invoke the agent after container start. In local-first P1 mode,
	// there is no separate trigger server. The daemon invokes the harness
	// directly via docker exec (the harness binds to 127.0.0.1 inside the
	// container, unreachable from the host).
	invokeCtx, cancel := context.WithCancel(context.Background())
	tracked.CancelInvoke = cancel
	go func(tr *trackedRun) {
		defer close(tr.InvokeDone)
		// B30-T03 Part B (ceiling 1): derive the invoke-context timeout from
		// the TimeEnvelope when present (durable admission path); otherwise
		// fall back to the legacy v0.2.3 2-minute timeout. The envelope is
		// set on the trackedRun by the durable path after admission
		// (InvokeDeployment → setRunTimeEnvelope).
		timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, s.invokeContextTimeout(tr))
		defer timeoutCancel()
		if stdout, err := s.invokeAgent(timeoutCtx, containerID, agentName, req.GetTriggerPayload(), tr.TimeEnvelope); err != nil {
			// Write directly to the pointer under the lock. This works
			// whether the run is still in s.runs or has been claimed by
			// Stop (claimRun deletes from the map but the pointer is
			// shared). After close(InvokeDone), Stop reads tr.Status —
			// the channel establishes happens-before, so no lock needed
			// on the read side.
			failReason := invokeFailReason(err)
			s.runMu.Lock()
			tr.Status = "failed"
			tr.InvokeErr = err
			tr.FailReason = failReason
			s.runMu.Unlock()
			// Record audit event so SummarizeRun and explain-failure
			// can see the failure (they read from the audit store, not
			// the in-memory map).
			s.recordAudit("run_failed", "daemon", map[string]interface{}{
				"run_id":       runID,
				"agent_name":   agentName,
				"container_id": string(containerID),
				"fail_reason":  failReason,
			})
			fmt.Fprintf(os.Stderr, "daemon: auto-invoke (%s): %v\n", runID, err)
			// Self-cleanup: finalizeRun ensures harness audit ingestion
			// and resource cleanup happen exactly once.
			s.finalizeRun(context.Background(), runID, tr)
		} else {
			s.runMu.Lock()
			tr.Status = "succeeded"
			tr.InvokeResponse = stdout
			s.runMu.Unlock()
			// Persist the invoke response to the run directory so it
			// survives after the run is stopped and can be retrieved
			// by summarize/timeline (BUG 11 fix).
			if s.homePaths != nil {
				respPath := filepath.Join(s.homePaths.State, "runs", runID, "invoke-response.json")
				if err := os.WriteFile(respPath, []byte(stdout), 0o644); err != nil {
					fmt.Fprintf(os.Stderr, "daemon: persist invoke response (%s): %v\n", runID, err)
				}
			}
			s.recordAudit("invoke", "daemon", map[string]interface{}{
				"run_id":     runID,
				"agent_name": agentName,
			})
			s.recordAudit("run_complete", "daemon", map[string]interface{}{
				"run_id":     runID,
				"agent_name": agentName,
				"exit_code":  0,
			})
			// Self-cleanup: finalizeRun ensures harness audit ingestion
			// and resource cleanup happen exactly once.
			s.finalizeRun(context.Background(), runID, tr)
		}
	}(tracked)

	// B26: persist legacy as one run / one attempt; return additive fields.
	if storedAttemptID, err := s.persistLegacyRunAsOneAttempt(ctx, runID, agentName, attemptID); err == nil {
		attemptID = storedAttemptID
	}
	return &controlv1.RunResponse{
		RunId:     runID,
		AttemptId: attemptID,
		Status:    "RUNNING",
	}, nil
}

// cleanupRun removes the run's Docker resources (agent container, gateway
// container, internal network, egress network, gateway config dir, audit
// tailer). Called from the auto-invoke goroutine on success (so completed
// runs don't leak resources) and from Stop() on explicit termination.
// Safe to call multiple times — Remove/RemoveNetwork are idempotent on
// already-removed resources.
// checkDockerEngineVersion verifies the Docker Engine is patched against
// known vulnerabilities (CVE-2026-41567/41568/42306, fixed in 29.5.1).
// Returns nil if patched or if the version cannot be determined (fail-open
// for unknown versions to avoid blocking valid custom engines).
func checkDockerEngineVersion(ctx context.Context, rt *runtime.DockerRuntime) error {
	serverVer, err := rt.ServerVersion(ctx)
	if err != nil {
		// If we can't get the version, don't block — the daemon already
		// checks Docker reachability separately.
		return nil
	}
	parts := strings.Split(serverVer, ".")
	if len(parts) < 3 {
		// Unknown version format — fail open.
		return nil
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	patch, err3 := strconv.Atoi(strings.SplitN(parts[2], "-", 2)[0])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil
	}
	if major < 29 || (major == 29 && minor < 5) || (major == 29 && minor == 5 && patch < 1) {
		return fmt.Errorf("docker Engine %s has known vulnerabilities (CVE-2026-41567/41568/42306); upgrade to 29.5.1+ (set AGENTPAAS_ALLOW_VULNERABLE_DOCKER=1 to bypass for testing)", serverVer)
	}
	return nil
}

func (s *controlServer) cleanupRun(ctx context.Context, tr *trackedRun) {
	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return
	}
	if tr.Tailer != nil {
		tr.Tailer.stop()
	}
	if tr.Gateway != "" {
		timeout := 10 * time.Second
		logBestEffort("Stop", rt.Stop(ctx, tr.Gateway, &timeout))
		logBestEffort("Remove", rt.Remove(ctx, tr.Gateway, true))
	}
	if tr.Container != "" {
		timeout := 10 * time.Second
		logBestEffort("Stop", rt.Stop(ctx, tr.Container, &timeout))
		logBestEffort("Remove", rt.Remove(ctx, tr.Container, true))
	}
	if tr.Network != "" {
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, runtime.NetworkID(tr.Network)))
	}
	if tr.EgressNetwork != "" {
		logBestEffort("RemoveNetwork", rt.RemoveNetwork(ctx, runtime.NetworkID(tr.EgressNetwork)))
	}
	if tr.GatewayConfigDir != "" {
		logBestEffort("remove", os.RemoveAll(tr.GatewayConfigDir))
	}
}

// finalizeRun ensures harness audit ingestion and resource cleanup happen
// exactly once per run, regardless of the terminal path (auto-invoke success,
// auto-invoke failure, Stop, or Cancel). It is idempotent via sync.Once:
// if called twice (e.g. auto-invoke completes and then Stop is called),
// the second call is a no-op.
func (s *controlServer) finalizeRun(ctx context.Context, runID string, tr *trackedRun) {
	tr.finalizeOnce.Do(func() {
		// 1. Stop audit tailer (if running) — must happen before ingestion
		//    to ensure the tailer has finished reading all harness records.
		if tr.Tailer != nil {
			tr.Tailer.stop()
		}
		// 1b. Stop progress tailer and remove journal key file (B27).
		if tr.ProgressTailer != nil {
			tr.ProgressTailer.Stop()
		}
		if tr.JournalKeyPath != "" {
			logBestEffort("remove", os.RemoveAll(tr.JournalKeyPath))
		}
		// 2. Ingest harness audit records into the daemon audit chain.
		//    This reads harness-audit.jsonl, verifies the hash chain,
		//    appends valid records to s.auditWriter, and rebuilds the
		//    SQLite index. Corrupted chains produce a
		//    harness_audit_chain_broken event and skip ingestion.
		s.ingestHarnessAudit(runID, tr.AuditDir)
		// 3. Record terminal run status in the daemon audit chain.
		s.recordAudit("run_finalized", "daemon", map[string]interface{}{
			"run_id": runID,
			"status": tr.Status,
		})
		// 3b. Best-effort persist terminal status into routed run store.
		s.updateLegacyRunStatus(ctx, runID, tr.Status)
		// 4. Clean up Docker resources (containers, networks, config dirs).
		//    Safe to call even if resources are already removed — Docker
		//    Remove/RemoveNetwork are idempotent on already-removed resources.
		s.cleanupRun(ctx, tr)
	})
}

func (s *controlServer) Stop(ctx context.Context, req *controlv1.StopRequest) (*controlv1.StopResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	tracked, ok := s.claimRun(runID)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "run %q not found", runID)
	}
	containerID := tracked.Container

	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "docker runtime not available: %v", err)
	}

	if tracked.CancelInvoke != nil {
		tracked.CancelInvoke()
	}
	if tracked.InvokeDone != nil {
		select {
		case <-tracked.InvokeDone:
		case <-time.After(3 * time.Second):
			tracked.Status = "failed"
			if tracked.FailReason == "" {
				tracked.FailReason = "invoke did not complete within timeout"
			}
		}
	}

	timeout := 10 * time.Second
	if req.GetForce() {
		timeout = 0
	}
	// Auto-invoke finalize/cleanup may have already removed containers.
	// Treat "not found" as success so Stop is idempotent for completed runs.
	if containerID != "" {
		if err := rt.Stop(ctx, containerID, &timeout); err != nil && !errors.Is(err, runtime.ErrContainerNotFound) {
			return nil, status.Errorf(codes.Internal, "stop container: %v", err)
		}
	}

	// Stop and remove gateway container (best-effort; may already be cleaned).
	if tracked.Gateway != "" {
		logBestEffort("Stop", rt.Stop(ctx, tracked.Gateway, &timeout))
		logBestEffort("Remove", rt.Remove(ctx, tracked.Gateway, req.GetForce()))
	}

	finalStatus := tracked.Status
	if req.GetForce() {
		finalStatus = "cancelled"
	} else {
		switch tracked.Status {
		case "failed":
			finalStatus = "failed"
		case "succeeded":
			finalStatus = "succeeded"
		case "running":
			finalStatus = "succeeded"
		}
	}

	// finalizeRun stops the audit tailer, ingests harness audit records,
	// records terminal status, and cleans up Docker resources.
	// Safe to call even if the auto-invoke goroutine already finalized
	// this run (sync.Once guarantees idempotency).
	s.finalizeRun(ctx, runID, tracked)
	if s.eventBus != nil {
		eventType := trigger.EventRunSucceeded
		switch {
		case req.GetForce():
			eventType = trigger.EventRunCancelled
		case finalStatus == "failed":
			eventType = trigger.EventRunFailed
		}
		s.eventBus.Publish(runID, eventType, map[string]interface{}{
			"container_id": string(containerID),
			"force":        req.GetForce(),
		})
	}
	if tracked.FailReason == "" && tracked.InvokeErr != nil {
		tracked.FailReason = invokeFailReason(tracked.InvokeErr)
	}
	auditFields := map[string]interface{}{
		"run_id":       runID,
		"container_id": string(containerID),
		"force":        req.GetForce(),
		"status":       finalStatus,
	}
	if tracked.FailReason != "" && finalStatus == "failed" {
		auditFields["fail_reason"] = tracked.FailReason
	}
	s.recordAudit("run_stop", "cli", auditFields)
	return &controlv1.StopResponse{Acknowledged: true}, nil
}

func (s *controlServer) Logs(req *controlv1.LogsRequest, stream controlv1.ControlService_LogsServer) error {
	runID := req.GetRunId()
	if runID == "" {
		return status.Error(codes.InvalidArgument, "run_id is required")
	}

	containerID, _, _ := s.lookupRun(runID) // status/agent unused for log stream
	if containerID == "" {
		return status.Errorf(codes.NotFound, "run %q not found", runID)
	}

	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "docker runtime not available: %v", err)
	}

	tail := int(req.GetTail())
	if tail <= 0 {
		tail = 100
	}
	rc, err := rt.Logs(stream.Context(), containerID, runtime.LogOptions{
		Follow: req.GetFollow(),
		Tail:   tail,
	})
	if err != nil {
		return status.Errorf(codes.Internal, "get logs: %v", err)
	}
	defer func() { _ = rc.Close() }() // best-effort close

	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		entry := &controlv1.LogEntry{
			Timestamp: timestamppb.Now(),
			RunId:     runID,
			Level:     "info",
			Message:   strings.ToValidUTF8(scanner.Text(), "\ufffd"),
		}
		if err := stream.Send(entry); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *controlServer) PolicyApply(ctx context.Context, req *controlv1.PolicyApplyRequest) (*controlv1.PolicyApplyResponse, error) {
	yamlContent := req.GetPolicyYaml()
	if yamlContent == "" {
		return nil, status.Error(codes.InvalidArgument, "policy_yaml is required")
	}

	var p policy.Policy
	if err := yaml.Unmarshal([]byte(yamlContent), &p); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parse policy yaml: %v", err)
	}

	_, warnings := policy.Canonicalize(&p)
	digest, err := policy.Digest(&p)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "policy digest: %v", err)
	}

	if req.GetDryRun() {
		return &controlv1.PolicyApplyResponse{
			PolicyDigest: digest,
			Warnings:     warnings,
		}, nil
	}
	if s.homePaths == nil {
		return nil, status.Error(codes.FailedPrecondition, "daemon home paths not configured")
	}

	compiled, err := policy.CompileGatewayConfig(&p)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "compile policy: %v", err)
	}

	if err := os.MkdirAll(s.homePaths.Config, 0o700); err != nil {
		return nil, status.Errorf(codes.Internal, "create config dir: %v", err)
	}
	policyPath := filepath.Join(s.homePaths.Config, "policy.yaml")
	gatewayPath := filepath.Join(s.homePaths.Config, "gateway.yaml")
	if err := writeFileAtomic(policyPath, []byte(yamlContent), 0o600); err != nil {
		return nil, status.Errorf(codes.Internal, "write policy: %v", err)
	}
	if err := writeFileAtomic(gatewayPath, compiled, 0o600); err != nil {
		return nil, status.Errorf(codes.Internal, "write gateway config: %v", err)
	}

	canonical, _ := policy.Canonicalize(&p) // warnings already captured above
	rulesApplied := int32(len(canonical.Egress))
	return &controlv1.PolicyApplyResponse{
		PolicyDigest: digest,
		RulesApplied: rulesApplied,
		Warnings:     warnings,
	}, nil
}

func (s *controlServer) SecretSet(ctx context.Context, req *controlv1.SecretSetRequest) (*controlv1.SecretSetResponse, error) {
	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "secret name is required")
	}

	scope := req.GetScope()
	if scope == "" {
		scope = "default"
	}
	store, err := secrets.NewKeychainStore("agentpaas-" + scope)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "keychain store: %v", err)
	}

	_, getErr := store.Get(ctx, name)
	created := getErr != nil
	return &controlv1.SecretSetResponse{Created: created}, nil
}

func (s *controlServer) SecretGrant(ctx context.Context, req *controlv1.SecretGrantRequest) (*controlv1.SecretGrantResponse, error) {
	runID := req.GetRunId()
	secretName := req.GetSecretName()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	if secretName == "" {
		return nil, status.Error(codes.InvalidArgument, "secret name is required")
	}

	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretGrants == nil {
		s.secretGrants = make(map[string]map[string]struct{})
	}
	if s.secretGrants[runID] == nil {
		s.secretGrants[runID] = make(map[string]struct{})
	}
	s.secretGrants[runID][secretName] = struct{}{}
	return &controlv1.SecretGrantResponse{Acknowledged: true}, nil
}

func (s *controlServer) SecretRevoke(ctx context.Context, req *controlv1.SecretRevokeRequest) (*controlv1.SecretRevokeResponse, error) {
	runID := req.GetRunId()
	secretName := req.GetSecretName()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	if secretName == "" {
		return nil, status.Error(codes.InvalidArgument, "secret name is required")
	}

	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if grants, ok := s.secretGrants[runID]; ok {
		delete(grants, secretName)
	}
	return &controlv1.SecretRevokeResponse{Acknowledged: true}, nil
}

func (s *controlServer) AuditQuery(ctx context.Context, req *controlv1.AuditQueryRequest) (*controlv1.AuditQueryResponse, error) {
	if s.auditIndex == nil {
		return nil, status.Error(codes.Unavailable, "audit index not initialized")
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 50
	}

	var records []audit.AuditRecord
	var err error
	if req.GetEventType() != controlv1.EventType_EVENT_TYPE_UNSPECIFIED {
		eventType := auditEventTypeFromProto(req.GetEventType())
		records, err = s.auditIndex.QueryByEventType(eventType, pageSize)
	} else {
		records, err = s.recentAuditRecords(pageSize)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "audit query: %v", err)
	}

	if req.GetAgentName() != "" {
		filterAgent := req.GetAgentName()
		if s.homePaths != nil {
			resolved, resolveErr := install.ResolveAgentRef(install.ResolveRefOpts{
				StateRoot: s.homePaths.State,
				Input:     filterAgent,
			})
			if resolveErr != nil {
				return nil, status.Errorf(codes.InvalidArgument, "%v", resolveErr)
			}
			filterAgent = resolved.DaemonKey
		}
		filtered := make([]audit.AuditRecord, 0, len(records))
		for _, record := range records {
			if auditString(record.Payload, "agent_name") == filterAgent {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}

	if req.GetRunId() != "" {
		filtered := make([]audit.AuditRecord, 0, len(records))
		for _, record := range records {
			if auditString(record.Payload, "run_id") == req.GetRunId() {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}

	entries := make([]*controlv1.AuditEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, auditRecordToEntry(record))
	}

	var chainVerification *controlv1.AuditChainVerification
	if s.homePaths != nil {
		auditPath := filepath.Join(s.homePaths.State, "audit.jsonl")
		checkpointsPath := s.auditCheckpointsPath
		if checkpointsPath == "" {
			checkpointsPath = filepath.Join(s.homePaths.State, "audit.jsonl.checkpoints")
		}
		if result, verifyErr := audit.VerifyAuditChain(auditPath, checkpointsPath, s.auditCheckpointPubKey); verifyErr == nil {
			chainVerification = auditChainVerificationToProto(result)
		}
	}

	return &controlv1.AuditQueryResponse{
		Entries:           entries,
		TotalCount:        int32(len(entries)),
		ChainVerification: chainVerification,
	}, nil
}

func (s *controlServer) AuditExport(ctx context.Context, req *controlv1.AuditExportRequest) (*controlv1.AuditExportResponse, error) {
	if s.homePaths == nil {
		return nil, status.Error(codes.FailedPrecondition, "daemon home paths not configured")
	}

	auditPath := filepath.Join(s.homePaths.State, "audit.jsonl")
	records, err := readAuditJSONL(auditPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read audit log: %v", err)
	}

	format := strings.ToLower(strings.TrimSpace(req.GetFormat()))
	if format == "" {
		format = "json"
	}

	data, err := formatAuditExport(records, format, req.GetIncludePayloads())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "format audit export: %v", err)
	}

	return &controlv1.AuditExportResponse{
		Data:       data,
		EntryCount: int32(len(records)),
	}, nil
}

func (s *controlServer) recordAudit(eventType, actor string, payload map[string]interface{}) {
	if s.auditWriter == nil {
		return
	}
	record := audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      eventType,
		DeploymentMode: "local",
		Actor:          actor,
		Payload:        payload,
	}
	if err := s.auditWriter.Append(record); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: audit append (%s): %v\n", eventType, err)
	}
	// Refresh the SQLite index so dashboard queries see the new record.
	if s.auditIndex != nil && s.homePaths != nil {
		if err := s.auditIndex.Rebuild(filepath.Join(s.homePaths.State, "audit.jsonl")); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: audit index rebuild: %v\n", err)
		}
	}
}

func (s *controlServer) invokeAgent(ctx context.Context, containerID runtime.ContainerID, agentName string, triggerPayload []byte, timeEnvelope *routedrun.TimeEnvelope) (string, error) {
	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return "", fmt.Errorf("runtime: %w", err)
	}

	// Wait for harness to be ready (agent import phase).
	readyCmd := []string{"python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/readyz', timeout=5)"}
	ready := false
	for i := 0; i < 30; i++ {
		_, _, exitCode, _ := rt.Exec(ctx, containerID, readyCmd) // readiness probe; non-zero/err = not ready
		if exitCode == 0 {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if !ready {
		return "", fmt.Errorf("harness /readyz not ready after 30 attempts")
	}

	// Build the invoke payload with LLM config, resolved credentials, and
	// the user's trigger payload (merged at top level).
	payload, err := s.buildInvokePayload(ctx, agentName, triggerPayload)
	if err != nil {
		return "", fmt.Errorf("build invoke payload: %w", err)
	}

	// B30-T03 Part B (ceilings 3/4/5): inject the TimeEnvelope into the
	// payload so the harness derives the /invoke context timeout, the
	// wall-clock budget, and the model-client HTTP timeout from the envelope
	// rather than the legacy v0.2.3 fixed constants. When nil (legacy trigger
	// path), the harness falls back to its documented legacy defaults.
	if timeEnvelope != nil {
		payload["time_envelope"] = timeEnvelope.MarshalForPayload()
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal invoke payload: %w", err)
	}

	// Invoke the agent. The payload is passed via stdin to keep the credential
	// value out of process args (visible via ps). The python script reads stdin.
	invokeCmd := []string{"python3", "-c",
		"import urllib.request,json,sys;" +
			"payload=sys.stdin.buffer.read();" +
			"req=urllib.request.Request('http://127.0.0.1:8080/invoke'," +
			"data=payload," +
			"headers={'Content-Type':'application/json'});" +
			"print(urllib.request.urlopen(req,timeout=60).read().decode())"}
	stdout, stderr, exitCode, err := rt.ExecWithStdin(ctx, containerID, invokeCmd, payloadJSON)
	if err != nil {
		return "", fmt.Errorf("exec invoke: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("invoke failed (exit %d): %s", exitCode, stderr)
	}

	return stdout, nil
}

// buildInvokePayload builds the invoke payload with LLM config and credential metadata.
// Credential VALUES are NEVER included in this payload — only ID and header metadata.
// Raw secrets are delivered to the harness through a side-channel credentials file.
//
// Credential metadata is collected from TWO sources:
//  1. agent.yaml's llm.credential field (LLM credential, with provider auth header)
//  2. policy.yaml's credentials section (all declared credentials, header metadata)
//
// This ensures http_with_credential("my-cred-id", ...) works for any credential
// declared in policy.yaml.
//
// triggerPayload (optional) is the user's trigger payload from RunRequest.trigger_payload
// or InvokeRequest.payload. When provided and valid JSON, its top-level keys are merged
// into the payload so the agent's handle_invoke() receives the user's input data.
// Reserved keys ("llm", "credentials", "mcp") are protected — user values for those
// keys are silently dropped to prevent clobbering daemon-injected config.
func (s *controlServer) buildInvokePayload(ctx context.Context, agentName string, triggerPayload []byte) (map[string]any, error) {
	payload := map[string]any{}

	// Merge user trigger payload first, so daemon-injected reserved keys
	// (llm, credentials, mcp) always win over user-supplied values.
	if len(triggerPayload) > 0 {
		var userPayload map[string]any
		if err := json.Unmarshal(triggerPayload, &userPayload); err != nil {
			return nil, fmt.Errorf("invalid trigger payload JSON: %w", err)
		}
		reserved := map[string]bool{"llm": true, "credentials": true, "mcp": true, "budget": true, "guardrails": true, "inject_system_prompt": true, "time_envelope": true}
		for k, v := range userPayload {
			if reserved[k] {
				continue
			}
			payload[k] = v
		}
	}

	deployedDir := pack.DeployedAgentPath(s.homePaths.Home, agentName)

	// Load the deployed agent lock to get AgentYAML with LLM config.
	lockPath := filepath.Join(deployedDir, "agent.lock")
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		// Not deployed or load failed — return payload with user keys only.
		return payload, nil
	}

	// --- Collect credential metadata into a map keyed by ID to avoid duplicates ---
	// Credential values are NEVER included. Only id and header metadata.
	credMap := make(map[string]map[string]any) // id → {id, header}

	// --- 1. Collect LLM credential metadata from agent.yaml ---
	if lock.AgentYAML != nil && lock.AgentYAML.LLM.Provider != "" {
		credentialName := lock.AgentYAML.LLM.Credential
		if credentialName != "" {
			adapter := llm.GetAdapter(lock.AgentYAML.LLM.Provider)
			headerName := "Authorization"
			if adapter != nil {
				headerName = adapter.AuthHeader()
			}

			payload["llm"] = map[string]any{
				"provider":   lock.AgentYAML.LLM.Provider,
				"model":      lock.AgentYAML.LLM.Model,
				"credential": credentialName,
			}
			credMap[credentialName] = map[string]any{
				"id":     credentialName,
				"header": headerName,
			}
		}
	}

	// --- 2. Collect policy-declared credential metadata from policy.yaml ---
	policyPath := filepath.Join(deployedDir, "policy.yaml")
	policyData, perr := os.ReadFile(policyPath)
	var parsedPolicy *policy.Policy
	if perr == nil && len(policyData) > 0 {
		parsedPolicy, perr = policy.ParsePolicy(bytes.NewReader(policyData))
		if perr == nil {
			for _, c := range parsedPolicy.Credentials {
				if c.ID == "" {
					continue
				}
				// Skip if already collected (e.g., LLM credential already added above).
				if _, exists := credMap[c.ID]; exists {
					continue
				}
				headerName := c.Header
				if headerName == "" {
					headerName = "Authorization"
				}
				credMap[c.ID] = map[string]any{
					"id":     c.ID,
					"header": headerName,
				}
			}
		}
	}

	// --- Convert credMap to slice for the payload (metadata only, no values) ---
	if len(credMap) > 0 {
		creds := make([]map[string]any, 0, len(credMap))
		for _, c := range credMap {
			creds = append(creds, c)
		}
		payload["credentials"] = creds
	}

	// --- 3. Wire llm_budget from policy.yaml to the harness budget enforcer ---
	// The harness BudgetEnforcer reads budget config from payload["budget"].
	// Without this, it always uses defaults (30s wall clock, 100k tokens).
	// The policy.yaml is already parsed above for credentials; reuse it to
	// also extract llm_budget settings.
	if parsedPolicy != nil && parsedPolicy.LLMBudget != nil {
		budget := map[string]any{}
		if parsedPolicy.LLMBudget.MaxTokens > 0 {
			budget["max_tokens"] = parsedPolicy.LLMBudget.MaxTokens
		}
		// max_tokens_per_request is enforced by the gateway localRateLimit;
		// pass it to the harness as the per-request budget too so the
		// harness BudgetEnforcer caps tokens per LLM call.
		if parsedPolicy.LLMBudget.MaxTokensPerRequest > 0 {
			budget["max_tokens_per_request"] = parsedPolicy.LLMBudget.MaxTokensPerRequest
			if _, ok := budget["max_tokens"]; !ok {
				budget["max_tokens"] = parsedPolicy.LLMBudget.MaxTokensPerRequest
			}
		}
		if len(budget) > 0 {
			payload["budget"] = budget
		}
	}

	// --- 4. Harness-level policies that gateway cannot enforce on host backends ---
	// Guardrails: agentgateway v1.3.0 has no route-level guardrails field for host backends.
	// Transformations.inject_system_prompt: not supported as a host-backend transform field.
	if parsedPolicy != nil {
		if len(parsedPolicy.Guardrails) > 0 {
			gs := make([]map[string]any, 0, len(parsedPolicy.Guardrails))
			for _, g := range parsedPolicy.Guardrails {
				entry := map[string]any{"type": g.Type}
				if g.Pattern != "" {
					entry["pattern"] = g.Pattern
				}
				if g.Action != "" {
					entry["action"] = g.Action
				}
				if g.Provider != "" {
					entry["provider"] = g.Provider
				}
				if g.Credential != "" {
					entry["credential"] = g.Credential
				}
				if g.URL != "" {
					entry["url"] = g.URL
				}
				gs = append(gs, entry)
			}
			payload["guardrails"] = gs
		}
		if parsedPolicy.Transformations != nil && parsedPolicy.Transformations.Request != nil {
			sp := parsedPolicy.Transformations.Request.InjectSystemPrompt
			if sp != "" {
				payload["inject_system_prompt"] = sp
			}
		}
		if parsedPolicy.Observability != nil {
			payload["observability"] = map[string]any{
				"cost_tracking": parsedPolicy.Observability.CostTracking,
				"otel_endpoint": parsedPolicy.Observability.OTelEndpoint,
			}
		}
	}

	return payload, nil
}

// rewriteGatewayConfigSecrets replaces __agentpaas_secret:<id> placeholders in
// the compiled gateway config with concrete Keychain secret values. agentgateway
// apiKey.keys require real key material, not env/credentials placeholders.
//
// Fail-closed for ingress API-key auth: if the declared credential cannot be
// resolved, the gateway is not started with a non-functional auth policy.
func (s *controlServer) rewriteGatewayConfigSecrets(gatewayConfigPath string, p *policy.Policy) error {
	if p == nil || p.IngressAuth == nil || p.IngressAuth.Type != "api_key" || p.IngressAuth.APIKey == nil {
		return nil
	}
	credID := strings.TrimSpace(p.IngressAuth.APIKey.Credential)
	if credID == "" {
		return fmt.Errorf("ingress_auth.api_key.credential is empty")
	}
	placeholder := policy.SecretPlaceholder(credID)

	data, err := os.ReadFile(gatewayConfigPath)
	if err != nil {
		return fmt.Errorf("read gateway config: %w", err)
	}
	if !bytes.Contains(data, []byte(placeholder)) {
		return nil
	}

	var store secrets.SecretStore
	if s.secretStoreForTest != nil {
		store = s.secretStoreForTest
	} else {
		var storeErr error
		store, storeErr = secrets.NewKeychainStore(secretServiceName(s.homePaths.Home))
		if storeErr != nil {
			return fmt.Errorf("keychain unavailable for ingress api key %q: %w", credID, storeErr)
		}
	}
	val, err := store.Get(context.Background(), credID)
	if err != nil {
		return fmt.Errorf("credential %q not found in keychain; run: agentpaas secret add %s", credID, credID)
	}
	secret := strings.TrimSpace(string(val))
	if secret == "" {
		return fmt.Errorf("credential %q is empty in keychain; re-run: agentpaas secret add %s", credID, credID)
	}
	if strings.ContainsAny(secret, "\n\r") {
		return fmt.Errorf("credential %q contains newline; rejected for gateway apiKey injection", credID)
	}

	// YAML double-quoted scalar.
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range secret {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	quoted := b.String()

	out := string(data)
	out = strings.ReplaceAll(out, "key: "+placeholder, "key: "+quoted)
	out = strings.ReplaceAll(out, `key: "`+placeholder+`"`, "key: "+quoted)
	if strings.Contains(out, placeholder) {
		return fmt.Errorf("failed to substitute ingress api key placeholder for %q", credID)
	}
	if err := os.WriteFile(gatewayConfigPath, []byte(out), 0o600); err != nil {
		return fmt.Errorf("write rewritten gateway config: %w", err)
	}
	return nil
}

// writeCredentialsForRun resolves Keychain secrets for the agent's policy
// credentials and writes them to a JSON file in the per-run gateway config
// directory. This file is bind-mounted into the agent container as read-only
// so the harness can load credential values at startup without raw secrets
// crossing Docker ExecWithStdin, harness /invoke, or Python worker stdin.
//
// Returns the host path to the credentials file and true if the file was
// written successfully. Returns ("", false) if there are no credentials or
// if Keychain resolution fails (graceful degradation).
func (s *controlServer) writeCredentialsForRun(runID string, deployedDir string, gatewayConfigDir string, credentialMap map[string]string) (string, bool) {
	if gatewayConfigDir == "" {
		return "", false
	}

	// Resolve the secret store (tests may inject FakeKeyStore).
	var store secrets.SecretStore
	if s.secretStoreForTest != nil {
		store = s.secretStoreForTest
	} else {
		var storeErr error
		store, storeErr = secrets.NewKeychainStore(secretServiceName(s.homePaths.Home))
		if storeErr != nil {
			return "", false
		}
	}

	// Collect all credential IDs that need resolution.
	credIDs := make(map[string]string) // id → default header

	// Agent.yaml LLM credential.
	lockPath := filepath.Join(deployedDir, "agent.lock")
	lock, lockErr := pack.ReadAgentLock(lockPath)
	if lockErr == nil && lock != nil && lock.AgentYAML != nil && lock.AgentYAML.LLM.Provider != "" {
		credName := lock.AgentYAML.LLM.Credential
		if credName != "" {
			adapter := llm.GetAdapter(lock.AgentYAML.LLM.Provider)
			header := "Authorization"
			if adapter != nil {
				header = adapter.AuthHeader()
			}
			credIDs[credName] = header
		}
	}

	// Policy.yaml credentials.
	policyPath := filepath.Join(deployedDir, "policy.yaml")
	policyData, perr := os.ReadFile(policyPath)
	if perr == nil && len(policyData) > 0 {
		parsed, perr := policy.ParsePolicy(bytes.NewReader(policyData))
		if perr == nil {
			for _, c := range parsed.Credentials {
				if c.ID == "" {
					continue
				}
				if _, exists := credIDs[c.ID]; exists {
					continue
				}
				header := c.Header
				if header == "" {
					header = "Authorization"
				}
				credIDs[c.ID] = header
			}
		}
	}

	if len(credIDs) == 0 {
		return "", false
	}

	// Resolve credential values from Keychain.
	type credEntry struct {
		ID     string `json:"id"`
		Header string `json:"header"`
		Value  string `json:"value"`
	}
	var entries []credEntry
	for id, header := range credIDs {
		// Apply the credential map: for installed agents, look up the local secret name.
		lookupName := id
		if credentialMap != nil {
			if localName, ok := credentialMap[id]; ok && localName != "" {
				lookupName = localName
			}
		}
		val, credErr := store.Get(context.Background(), lookupName)
		if credErr != nil {
			continue // graceful: skip unresolved credential
		}
		entries = append(entries, credEntry{
			ID:     id,
			Header: header,
			Value:  string(val),
		})
	}

	if len(entries) == 0 {
		return "", false
	}

	// Write to a JSON file in the gateway config directory.
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: marshal credentials file: %v\n", err)
		return "", false
	}

	credsPath := filepath.Join(gatewayConfigDir, "credentials.json")
	if err := os.WriteFile(credsPath, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: write credentials file: %v\n", err)
		return "", false
	}

	return credsPath, true
}

// secretServiceName derives a deterministic macOS Keychain service name from the
// home directory path. This matches the CLI convention in internal/cli/control.go.
func secretServiceName(homeDir string) string {
	sum := sha256.Sum256([]byte(homeDir))
	return "ai.agentpaas.secrets." + hex.EncodeToString(sum[:8])
}

func (s *controlServer) getOrCreateRuntime() (*runtime.DockerRuntime, error) {
	s.runtimeOnce.Do(func() {
		s.dockerRT, s.runtimeErr = runtime.NewDockerRuntime()
	})
	if s.runtimeErr != nil {
		return nil, s.runtimeErr
	}
	if s.dockerRT == nil {
		return nil, errors.New("docker runtime not initialized")
	}
	return s.dockerRT, nil
}

func (s *controlServer) trackRun(runID string, containerID runtime.ContainerID, networkID, auditDir string) {
	s.trackRunPtr(runID, &trackedRun{
		Container: containerID,
		Network:   networkID,
		AuditDir:  auditDir,
		Status:    "running",
	})
}

func (s *controlServer) trackRunPtr(runID string, tr *trackedRun) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		s.runs = make(map[string]*trackedRun)
	}
	s.runs[runID] = tr
}

// setRunTimeEnvelope is the B30-T03 Part B seam for ceiling 1: the durable
// admission path (InvokeDeployment → T05 supervisor claim) calls this to
// attach the TimeEnvelope built from the admission receipt to the tracked
// run, so the daemon's invoke-context timeout is derived from the envelope
// rather than the legacy 2-minute fallback. Returns false if no tracked run
// exists for runID (the durable path must have started the run first).
func (s *controlServer) setRunTimeEnvelope(runID string, env routedrun.TimeEnvelope) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		return false
	}
	tr, ok := s.runs[runID]
	if !ok {
		return false
	}
	tr.TimeEnvelope = &env
	return true
}

// legacyInvokeContextTimeout is the v0.2.3 fixed timeout for the daemon's
// auto-invoke goroutine. It is used ONLY when no TimeEnvelope is available
// (legacy trigger path) — the durable path derives the timeout from the
// envelope (B30-T03 Part B, ceiling 1).
const legacyInvokeContextTimeout = 2 * time.Minute

// invokeContextTimeout returns the timeout for the daemon's auto-invoke
// goroutine. When the tracked run carries a TimeEnvelope (durable path), the
// timeout is env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs) —
// the min of the stall timeout, the attempt-lease remaining, and the active
// time remaining. When nil (legacy v0.2.3 trigger path), it falls back to
// legacyInvokeContextTimeout (2 minutes).
func (s *controlServer) invokeContextTimeout(tr *trackedRun) time.Duration {
	if tr != nil && tr.TimeEnvelope != nil {
		nowMs := routedrun.NowMonotonicMs(nil)
		deadlineMs := tr.TimeEnvelope.EffectiveOperationDeadlineMs(nowMs, tr.TimeEnvelope.StallTimeoutMs)
		if deadlineMs <= 0 {
			// Envelope exhausted: tiny grace so the invoke surfaces a failure
			// rather than a zero-timeout panic.
			return 1 * time.Millisecond
		}
		return time.Duration(deadlineMs) * time.Millisecond
	}
	return legacyInvokeContextTimeout
}

func (s *controlServer) claimRun(runID string) (*trackedRun, bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		return nil, false
	}
	tracked, ok := s.runs[runID]
	if !ok {
		return nil, false
	}
	delete(s.runs, runID)
	return tracked, true
}

func (s *controlServer) setRunStatus(runID, status string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if tracked, ok := s.runs[runID]; ok {
		tracked.Status = status
	}
}

func invokeFailReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "harness /readyz not ready") {
		return "harness not ready (possible import failure or startup crash)"
	}
	return msg
}

func (s *controlServer) lookupRunWithStatus(runID string) (trackedRun, bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		return trackedRun{}, false
	}
	tracked, ok := s.runs[runID]
	if !ok {
		return trackedRun{}, false
	}
	return trackedRun{
		Container:    tracked.Container,
		Network:      tracked.Network,
		AuditDir:     tracked.AuditDir,
		Status:       tracked.Status,
		FailReason:   tracked.FailReason,
		CancelInvoke: tracked.CancelInvoke,
	}, true
}

// activeRunCount returns the number of currently tracked ACTIVE runs.
// Runs that have reached a terminal state (succeeded/failed/cancelled)
// do not count against the concurrent limit — only "running" runs do.
// This prevents completed-but-not-yet-stopped runs from blocking new
// invocations (BUG 7).
func (s *controlServer) activeRunCount() int {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	count := 0
	for _, tr := range s.runs {
		if tr.Status == "running" {
			count++
		}
	}
	return count
}

func (s *controlServer) lookupRun(runID string) (runtime.ContainerID, string, string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		return "", "", ""
	}
	tracked, ok := s.runs[runID]
	if !ok {
		return "", "", ""
	}
	return tracked.Container, tracked.Network, tracked.AuditDir
}

// verifyHarnessChain validates the hash chain of harness audit records.
// It checks:
// 1. Each record's prev_hash matches the previous record's record_hash
// 2. Each record's record_hash matches a recomputed hash from canonical JSON
// 3. The first (genesis) record has prev_hash == ""
// Returns nil if the chain is valid, or an error describing the break.
func verifyHarnessChain(records []audit.AuditRecord) error {
	if len(records) == 0 {
		return nil
	}
	for i, rec := range records {
		computedHash, err := rec.ComputeRecordHash()
		if err != nil {
			return fmt.Errorf("harness chain: line %d: compute hash: %w", i+1, err)
		}
		if rec.RecordHash != computedHash {
			return fmt.Errorf("harness chain: line %d: record_hash mismatch: stored %q, recomputed %q",
				i+1, rec.RecordHash, computedHash)
		}
		if i == 0 {
			if rec.PrevHash != "" {
				return fmt.Errorf("harness chain: line %d: genesis record must have empty prev_hash, got %q", i+1, rec.PrevHash)
			}
		} else {
			if rec.PrevHash != records[i-1].RecordHash {
				return fmt.Errorf("harness chain: line %d: prev_hash mismatch: got %q, expected %q",
					i+1, rec.PrevHash, records[i-1].RecordHash)
			}
		}
	}
	return nil
}

// ingestHarnessAudit reads the harness audit JSONL from the host audit
// directory and appends each record to the daemon's audit chain.
// Errors are logged but do not fail the Stop operation — the container
// is already stopped, and missing audit data is a best-effort concern.
func (s *controlServer) ingestHarnessAudit(runID, auditDir string) {
	if auditDir == "" {
		return
	}
	auditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	records, err := readAuditJSONL(auditPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: ingest harness audit (%s): %v\n", runID, err)
		// Corrupted / truncated file — emit chain_broken event.
		tamperRecord := audit.AuditRecord{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			EventType: "harness_audit_chain_broken",
			Actor:     "daemon",
			Payload: map[string]interface{}{
				"run_id": runID,
				"error":  err.Error(),
				"action": "audit_ingestion_refused",
			},
		}
		if err := s.auditWriter.Append(tamperRecord); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: audit append (harness_audit_chain_broken): %v\n", err)
		}
		return
	}
	if len(records) == 0 {
		return
	}

	if err := verifyHarnessChain(records); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: harness audit chain verification failed (%s): %v\n", runID, err)
		tamperRecord := audit.AuditRecord{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			EventType: "harness_audit_chain_broken",
			Actor:     "daemon",
			Payload: map[string]interface{}{
				"run_id": runID,
				"error":  err.Error(),
				"action": "audit_ingestion_refused",
			},
		}
		if err := s.auditWriter.Append(tamperRecord); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: audit append (harness_audit_chain_broken): %v\n", err)
		}
		return
	}

	for _, record := range records {
		// Ensure run_id is present in payload for audit queries.
		if record.Payload == nil {
			record.Payload = make(map[string]interface{})
		}
		if _, ok := record.Payload["run_id"]; !ok {
			record.Payload["run_id"] = runID
		}
		if err := s.auditWriter.Append(record); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: append harness audit record (%s): %v\n", runID, err)
		}
	}
	// Refresh the SQLite index so dashboard queries see the new records.
	if s.auditIndex != nil && s.homePaths != nil {
		if err := s.auditIndex.Rebuild(filepath.Join(s.homePaths.State, "audit.jsonl")); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: audit index rebuild: %v\n", err)
		}
	}
}

func (s *controlServer) untrackRun(runID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.runs, runID)
}

func generateRunID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
	}
	return "run-" + hex.EncodeToString(buf)
}

func (s *controlServer) openPackageIdentityKey(ctx context.Context, agentName string) (identity.KeyStore, identity.KeyID, error) {
	// Package identity material always uses the encrypted file keystore (no Keychain UI).
	store, err := s.openFileIdentityStore()
	if err != nil {
		return nil, "", err
	}
	ca, err := identity.NewLocalCA(store, &identity.TrustDomain{Host: "local.agentpaas"})
	if err != nil {
		return nil, "", err
	}
	if _, err := ca.EnsurePackageIdentityKey(agentName); err != nil {
		return nil, "", err
	}
	keyID := identity.KeyID("package_identity_" + agentName)
	_ = ctx // reserved for future cancellation of keystore ops
	return store, keyID, nil
}

// openPublisherIdentityStore opens the Keychain-backed store used by the
// identity CLI. Publisher operations must use this store; the encrypted file
// store above is reserved for daemon-owned package identity material.
func (s *controlServer) openPublisherIdentityStore() (identity.KeyStore, error) {
	return identity.NewKeychainKeyStore("agentpaas-daemon")
}

func (s *controlServer) openFileIdentityStore() (identity.KeyStore, error) {
	if s.homePaths == nil {
		return nil, fmt.Errorf("daemon home paths not configured")
	}
	passphrase, err := ensureKeystorePassphrase(s.homePaths.State)
	if err != nil {
		return nil, err
	}
	return identity.NewFileKeyStore(filepath.Join(s.homePaths.State, "keystore"), passphrase)
}

func ensureKeystorePassphrase(stateDir string) (string, error) {
	passPath := filepath.Join(stateDir, "keystore.pass")
	if data, err := os.ReadFile(passPath); err == nil {
		pass := strings.TrimSpace(string(data))
		if pass != "" {
			return pass, nil
		}
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate passphrase: %w", err)
	}
	pass := hex.EncodeToString(buf)
	if err := os.WriteFile(passPath, []byte(pass+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write passphrase: %w", err)
	}
	return pass, nil
}

func (s *controlServer) recentAuditRecords(limit int) ([]audit.AuditRecord, error) {
	count, err := s.auditIndex.RecordCount()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	start := int64(count) - int64(limit) + 1
	if start < 1 {
		start = 1
	}
	records := make([]audit.AuditRecord, 0, limit)
	for seq := int64(count); seq >= start; seq-- {
		record, queryErr := s.auditIndex.QueryBySeq(seq)
		if queryErr != nil {
			return records, queryErr
		}
		records = append(records, *record)
	}
	return records, nil
}

func auditChainVerificationToProto(result *audit.VerificationResult) *controlv1.AuditChainVerification {
	if result == nil {
		return nil
	}
	issues := make([]*controlv1.AuditCheckpointIssue, 0, len(result.Issues))
	for _, issue := range result.Issues {
		if issue == nil {
			continue
		}
		issues = append(issues, &controlv1.AuditCheckpointIssue{
			Type:    string(issue.Type),
			Message: issue.Message,
			Seq:     issue.Seq,
			Line:    int32(issue.Line),
		})
	}
	return &controlv1.AuditChainVerification{
		Verified:         len(result.Issues) == 0,
		AuditRecordCount: result.AuditRecordCount,
		AuditHeadSeq:     result.AuditHeadSeq,
		CheckpointCount:  int32(result.CheckpointCount),
		Issues:           issues,
	}
}

func auditRecordToEntry(record audit.AuditRecord) *controlv1.AuditEntry {
	var payload []byte
	if record.Payload != nil {
		payload, _ = json.Marshal(record.Payload) // best-effort; nil payload on failure
	}
	ts, _ := time.Parse(time.RFC3339Nano, record.Timestamp) // zero time if unparseable
	return &controlv1.AuditEntry{
		EventId:   strconv.FormatInt(record.Seq, 10),
		EventType: protoEventTypeFromAudit(record.EventType),
		AgentName: auditString(record.Payload, "agent_name"),
		RunId:     auditString(record.Payload, "run_id"),
		Timestamp: timestamppb.New(ts),
		Payload:   payload,
	}
}

func auditEventTypeFromProto(eventType controlv1.EventType) string {
	switch eventType {
	case controlv1.EventType_EVENT_TYPE_INVOKE:
		return "invoke"
	case controlv1.EventType_EVENT_TYPE_CANCEL:
		return "cancel"
	case controlv1.EventType_EVENT_TYPE_POLICY_APPLY:
		return "policy_apply"
	case controlv1.EventType_EVENT_TYPE_POLICY_DENIAL:
		return "policy_denied"
	case controlv1.EventType_EVENT_TYPE_SECRET_SET:
		return "secret_set"
	case controlv1.EventType_EVENT_TYPE_SECRET_GRANT:
		return "secret_grant"
	case controlv1.EventType_EVENT_TYPE_SECRET_REVOKE:
		return "secret_revoke"
	case controlv1.EventType_EVENT_TYPE_PACK:
		return "pack"
	case controlv1.EventType_EVENT_TYPE_RUN:
		return "run_start"
	case controlv1.EventType_EVENT_TYPE_STOP:
		return "run_stop"
	default:
		return strings.ToLower(strings.TrimPrefix(eventType.String(), "EVENT_TYPE_"))
	}
}

func protoEventTypeFromAudit(eventType string) controlv1.EventType {
	switch eventType {
	case "invoke":
		return controlv1.EventType_EVENT_TYPE_INVOKE
	case "cancel":
		return controlv1.EventType_EVENT_TYPE_CANCEL
	case "policy_apply":
		return controlv1.EventType_EVENT_TYPE_POLICY_APPLY
	case "policy_denied":
		return controlv1.EventType_EVENT_TYPE_POLICY_DENIAL
	case "secret_set":
		return controlv1.EventType_EVENT_TYPE_SECRET_SET
	case "secret_grant":
		return controlv1.EventType_EVENT_TYPE_SECRET_GRANT
	case "secret_revoke":
		return controlv1.EventType_EVENT_TYPE_SECRET_REVOKE
	case "pack":
		return controlv1.EventType_EVENT_TYPE_PACK
	case "run_start", "run":
		return controlv1.EventType_EVENT_TYPE_RUN
	case "run_stop", "stop":
		return controlv1.EventType_EVENT_TYPE_STOP
	default:
		return controlv1.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func readAuditJSONL(path string) ([]audit.AuditRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }() // best-effort close

	var records []audit.AuditRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse audit line: %w", err)
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func formatAuditExport(records []audit.AuditRecord, format string, includePayloads bool) ([]byte, error) {
	switch format {
	case "json":
		if !includePayloads {
			stripped := make([]map[string]interface{}, 0, len(records))
			for _, record := range records {
				stripped = append(stripped, map[string]interface{}{
					"seq":             record.Seq,
					"timestamp":       record.Timestamp,
					"event_type":      record.EventType,
					"deployment_mode": record.DeploymentMode,
					"actor":           record.Actor,
				})
			}
			return json.Marshal(stripped)
		}
		return json.Marshal(records)
	case "ndjson":
		var b strings.Builder
		for _, record := range records {
			exportRecord := record
			if !includePayloads {
				exportRecord.Payload = nil
			}
			line, err := json.Marshal(exportRecord)
			if err != nil {
				return nil, err
			}
			b.Write(line)
			b.WriteByte('\n')
		}
		return []byte(b.String()), nil
	case "csv":
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		// csv.Writer buffers; errors reported by w.Error() after Flush below.
		_ = w.Write([]string{"seq", "timestamp", "event_type", "deployment_mode", "actor"}) // best-effort write; error checked via Flush/Error
		for _, record := range records {
			_ = w.Write([]string{ // best-effort write; error checked via Flush/Error
				strconv.FormatInt(record.Seq, 10),
				record.Timestamp,
				record.EventType,
				record.DeploymentMode,
				record.Actor,
			})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return nil, err
		}
		return []byte(buf.String()), nil
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func (s *controlServer) reconcileOrphanedContainers(ctx context.Context) {
	rt, err := s.getOrCreateRuntime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: runtime unavailable: %v\n", err)
		return
	}

	s.runMu.Lock()
	knownRuns := make(map[string]struct{}, len(s.runs))
	for runID := range s.runs {
		knownRuns[runID] = struct{}{}
	}
	s.runMu.Unlock()

	var removals int

	// List internal and egress networks once for cleanup.
	internalNetworks, netErr := rt.ListNetworks(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelResourceType+"="+runtime.ResourceTypeNetInternal,
	)
	if netErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: list internal networks: %v\n", netErr)
	}

	egressNetworks, egressNetErr := rt.ListNetworks(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelResourceType+"="+runtime.ResourceTypeNetEgress,
	)
	if egressNetErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: list egress networks: %v\n", egressNetErr)
	}

	containers, err := rt.ListContainers(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelResourceType+"="+runtime.ResourceTypeAgent,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: list containers: %v\n", err)
	} else {
		for _, c := range containers {
			if _, known := knownRuns[c.RunID]; known {
				continue
			}
			action := "removed"
			if c.Status == runtime.ContainerStatusRunning {
				timeout := 10 * time.Second
				if err := rt.Stop(ctx, runtime.ContainerID(c.ID), &timeout); err != nil {
					fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: stop container %s: %v\n", c.ID, err)
				} else {
					action = "stopped_and_removed"
				}
			}
			if err := rt.Remove(ctx, runtime.ContainerID(c.ID), true); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove container %s: %v\n", c.ID, err)
				s.recordAudit("container_reconciled", "daemon", map[string]interface{}{
					"run_id":       c.RunID,
					"container_id": c.ID,
					"action":       "remove_failed",
				})
				continue
			}
			removals++

			if netErr == nil {
				for _, net := range internalNetworks {
					if net.Labels[runtime.LabelRunID] == c.RunID {
						if err := rt.RemoveNetwork(ctx, runtime.NetworkID(net.ID)); err != nil {
							fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove network %s: %v\n", net.ID, err)
						} else {
							removals++
						}
					}
				}
			}

			s.recordAudit("container_reconciled", "daemon", map[string]interface{}{
				"run_id":       c.RunID,
				"container_id": c.ID,
				"action":       action,
			})
		}
	}

	gatewayContainers, err := rt.ListContainers(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelResourceType+"="+runtime.ResourceTypeGateway,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: list gateway containers: %v\n", err)
	} else {
		for _, c := range gatewayContainers {
			if _, known := knownRuns[c.RunID]; known {
				continue
			}
			action := "removed"
			if c.Status == runtime.ContainerStatusRunning {
				timeout := 10 * time.Second
				if err := rt.Stop(ctx, runtime.ContainerID(c.ID), &timeout); err != nil {
					fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: stop gateway %s: %v\n", c.ID, err)
				} else {
					action = "stopped_and_removed"
				}
			}
			if err := rt.Remove(ctx, runtime.ContainerID(c.ID), true); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove gateway %s: %v\n", c.ID, err)
				s.recordAudit("container_reconciled", "daemon", map[string]interface{}{
					"run_id":       c.RunID,
					"container_id": c.ID,
					"action":       "remove_failed",
				})
				continue
			}
			removals++

			if egressNetErr == nil {
				for _, net := range egressNetworks {
					if net.Labels[runtime.LabelRunID] == c.RunID {
						if err := rt.RemoveNetwork(ctx, runtime.NetworkID(net.ID)); err != nil {
							fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove egress network %s: %v\n", net.ID, err)
						} else {
							removals++
						}
					}
				}
			}

			s.recordAudit("container_reconciled", "daemon", map[string]interface{}{
				"run_id":       c.RunID,
				"container_id": c.ID,
				"action":       action,
			})
		}
	}

	networks, err := rt.ListNetworks(ctx, runtime.LabelManagedBy+"="+runtime.ManagedByValue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: list managed networks: %v\n", err)
	} else {
		for _, net := range networks {
			runID := net.Labels[runtime.LabelRunID]
			if runID == "" {
				continue
			}
			if _, known := knownRuns[runID]; known {
				continue
			}
			if net.Labels[runtime.LabelResourceType] != runtime.ResourceTypeNetInternal {
				continue
			}
			if err := rt.RemoveNetwork(ctx, runtime.NetworkID(net.ID)); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove orphaned network %s: %v\n", net.ID, err)
			} else {
				removals++
			}
		}
	}

	if egressNetErr == nil {
		for _, net := range egressNetworks {
			runID := net.Labels[runtime.LabelRunID]
			if runID == "" {
				continue
			}
			if _, known := knownRuns[runID]; known {
				continue
			}
			if err := rt.RemoveNetwork(ctx, runtime.NetworkID(net.ID)); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove orphaned egress network %s: %v\n", net.ID, err)
			} else {
				removals++
			}
		}
	}

	s.recordAudit("reconciliation_complete", "daemon", map[string]interface{}{
		"removals": removals,
	})
}

// writeFileAtomic replaces path with data using a same-directory temp file and rename,
// so concurrent readers (e.g. Run bind-mounting gateway.yaml) never see a partial write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName) // best-effort temp cleanup
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // best-effort close before return
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close() // best-effort close before return
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// CronAdd creates a new cron schedule that auto-invokes an agent.
func (s *controlServer) CronAdd(ctx context.Context, req *controlv1.CronAddRequest) (*controlv1.CronAddResponse, error) {
	if req.GetAgentName() == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_name is required")
	}
	if req.GetExpr() == "" {
		return nil, status.Error(codes.InvalidArgument, "expr is required")
	}
	if s.cronScheduler == nil {
		return nil, status.Error(codes.FailedPrecondition, "cron scheduler not available")
	}
	agentName := req.GetAgentName()
	if s.homePaths != nil {
		resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
			StateRoot: s.homePaths.State,
			Input:     agentName,
		})
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		agentName = resolved.DaemonKey
	}
	schedule := &trigger.CronSchedule{
		Expr:              req.GetExpr(),
		AgentName:         agentName,
		AgentVersion:      req.GetAgentVersion(),
		Timezone:          req.GetTimezone(),
		MissedRunPolicy:   req.GetMissedRunPolicy(),
		ConcurrencyPolicy: req.GetConcurrencyPolicy(),
		Payload:           req.GetPayload(),
		ContentType:       req.GetContentType(),
	}
	scheduleID, err := s.cronScheduler.AddSchedule(ctx, schedule)
	if err != nil {
		return nil, err // AddSchedule returns proper gRPC status errors
	}
	return &controlv1.CronAddResponse{
		Schedule: cronScheduleToProto(schedule, scheduleID),
	}, nil
}

// ListRuns returns all currently tracked agent runs, merging in-memory
// tracking with persisted store records when available (survives restart).
func (s *controlServer) ListRuns(ctx context.Context, req *controlv1.ListRunsRequest) (*controlv1.ListRunsResponse, error) {
	_ = req // ListRuns currently ignores filter fields
	s.runMu.Lock()
	runs := make([]*controlv1.RunInfo, 0, len(s.runs))
	seen := make(map[string]struct{}, len(s.runs))
	for runID, tr := range s.runs {
		info := &controlv1.RunInfo{
			RunId:     runID,
			AgentName: tr.AgentName,
			Status:    tr.Status,
		}
		if !tr.StartedAt.IsZero() {
			info.StartedAt = timestamppb.New(tr.StartedAt)
		}
		runs = append(runs, info)
		seen[runID] = struct{}{}
	}
	s.runMu.Unlock()

	// Merge persisted store runs (legacy one/one + any durable records).
	if s.runStore != nil {
		if stored, err := s.runStore.ListRuns(ctx, ""); err == nil {
			for _, r := range stored {
				id := string(r.RunID)
				if _, ok := seen[id]; ok {
					// Enrich in-memory entry with hierarchy IDs.
					for _, info := range runs {
						if info.RunId == id {
							info.WorkflowId = string(r.WorkflowID)
							break
						}
					}
					continue
				}
				info := &controlv1.RunInfo{
					RunId:      id,
					Status:     r.Status.String(),
					WorkflowId: string(r.WorkflowID),
				}
				if !r.CreatedAt.IsZero() {
					info.StartedAt = timestamppb.New(r.CreatedAt)
				}
				runs = append(runs, info)
			}
		}
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].GetRunId() < runs[j].GetRunId()
	})
	return &controlv1.ListRunsResponse{Runs: runs}, nil
}

// CronList lists all cron schedules.
func (s *controlServer) CronList(ctx context.Context, req *controlv1.CronListRequest) (*controlv1.CronListResponse, error) {
	if s.cronScheduler == nil {
		return nil, status.Error(codes.FailedPrecondition, "cron scheduler not available")
	}
	schedules := s.cronScheduler.ListSchedules()
	result := make([]*controlv1.CronScheduleInfo, 0, len(schedules))
	for _, sch := range schedules {
		result = append(result, cronScheduleToProto(sch, sch.ScheduleID))
	}
	return &controlv1.CronListResponse{Schedules: result}, nil
}

// CronRemove removes a cron schedule.
func (s *controlServer) CronRemove(ctx context.Context, req *controlv1.CronRemoveRequest) (*controlv1.CronRemoveResponse, error) {
	if req.GetScheduleId() == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	if s.cronScheduler == nil {
		return nil, status.Error(codes.FailedPrecondition, "cron scheduler not available")
	}
	if err := s.cronScheduler.RemoveSchedule(ctx, req.GetScheduleId()); err != nil {
		return nil, err
	}
	return &controlv1.CronRemoveResponse{Removed: true}, nil
}

func cronScheduleToProto(s *trigger.CronSchedule, scheduleID string) *controlv1.CronScheduleInfo {
	return &controlv1.CronScheduleInfo{
		ScheduleId:        scheduleID,
		Expr:              s.Expr,
		AgentName:         s.AgentName,
		AgentVersion:      s.AgentVersion,
		Timezone:          s.Timezone,
		MissedRunPolicy:   s.MissedRunPolicy,
		ConcurrencyPolicy: s.ConcurrencyPolicy,
		Payload:           s.Payload,
		ContentType:       s.ContentType,
	}
}
