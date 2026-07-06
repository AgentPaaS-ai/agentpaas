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
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

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
	harnessPath := resolveHarnessBinary()
	sdkDir := resolveSDKDir(harnessPath)
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

	registryRef, err := pack.PushImageToLocalRegistry(ctx, result.ImageRef, agentName, agentVersion)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "push to local registry: %v", err)
	}
	result.ImageRef = registryRef

	keyStore, keyID, err := s.openPackageIdentityKey(ctx, agentName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "identity keystore: %v", err)
	}

	lock, err := pack.CreateAgentLock(ctx, pack.LockConfig{
		BuildResult:     result,
		AgentYAML:       agentYAML,
		Runtime:         det.Runtime,
		BaseImageDigest: cfg.BaseImage,
		HarnessVersion:  "embedded",
		Platform:        fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		SourceDateEpoch: time.Unix(0, 0).UTC(),
		KeyStore:        &packKeyStoreAdapter{store: keyStore},
		KeyID:           string(keyID),
		PolicyYAML:      policyYAML,
		PublisherKeyStore: keyStore,
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
// In test mode, uses s.secretStoreForTest. In production, uses KeychainStore.
func validateCredentialsExist(s *controlServer, agentName string) error {
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

	deployedDir := pack.DeployedAgentPath(s.homePaths.Home, agentName)

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
		_, err := store.Get(context.Background(), id)
		if err != nil {
			return fmt.Errorf("credential %q not found in keychain; run: agentpaas secret add %s", id, id)
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

	deployed, err := pack.LoadDeployedAgent(s.homePaths.Home, agentName)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "agent %q not deployed: %v (run pack first)", agentName, err)
	}

	// Enforce concurrent run limit before creating any Docker resources.
	if s.activeRunCount() >= maxConcurrentRuns {
		return nil, status.Errorf(codes.ResourceExhausted,
			"concurrent run limit reached (%d/%d active); stop an existing run before starting a new one",
			s.activeRunCount(), maxConcurrentRuns)
	}

	// Verify deployed agent integrity, lockfile signature, and policy digest
	// BEFORE creating any Docker resources.
	if err := verifyDeployedAgent(s.homePaths.Home, agentName, s.auditWriter); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "deployed agent verification failed: %v", err)
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
	if err := validateCredentialsExist(s, agentName); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}

	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "docker runtime not available: %v", err)
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
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "create egress network: %v", err)
	}

	// Create host audit directory for harness audit JSONL.
	// The container runs as UID 64000 (non-root). The bind mount exposes this
	// host directory to the container. We must ensure UID 64000 can write, so
	// we chmod 0777 after mkdir to defeat the process umask (MkdirAll applies
	// umask to the mode, yielding 0755 which denies write to "other").
	hostAuditDir := filepath.Join(s.homePaths.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(hostAuditDir, 0o777); err != nil {
		_ = rt.RemoveNetwork(ctx, egressNetID)
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "create audit dir: %v", err)
	}
	if err := os.Chmod(hostAuditDir, 0o777); err != nil {
		_ = rt.RemoveNetwork(ctx, egressNetID)
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "chmod audit dir: %v", err)
	}

	// Create gateway container (dual-homed: internal + egress).
	// The gateway config is compiled FROM THE AGENT'S OWN POLICY, not from a
	// global file. This ensures each agent gets egress rules matching its
	// policy.yaml.
	var gatewayBinds []string
	var gatewayConfigDir string

	// Read the agent's policy.yaml from the deployed directory.
	deployedDir := pack.DeployedAgentPath(s.homePaths.Home, agentName)
	agentPolicyPath := filepath.Join(deployedDir, "policy.yaml")

	policyData, err := os.ReadFile(agentPolicyPath)
	if err == nil && len(policyData) > 0 {
		// Parse and compile the policy into gateway config.
		parsedPolicy, err := policy.ParsePolicy(bytes.NewReader(policyData))
		if err != nil {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
			return nil, status.Errorf(codes.Internal, "parse agent policy: %v", err)
		}
		if errs := policy.ValidatePolicy(parsedPolicy); policy.HasErrors(errs) {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
			return nil, status.Errorf(codes.Internal, "validate agent policy: %v", errs)
		}
		compiled, err := policy.CompileGatewayConfig(parsedPolicy)
		if err != nil {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
			return nil, status.Errorf(codes.Internal, "compile gateway config: %v", err)
		}
		// Write compiled config to per-run directory.
		perRunConfigDir := filepath.Join(s.homePaths.State, "runs", runID, "gateway-config")
		if err := os.MkdirAll(perRunConfigDir, 0o700); err != nil {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
			return nil, status.Errorf(codes.Internal, "create gateway config dir: %v", err)
		}
		gatewayConfigPath := filepath.Join(perRunConfigDir, "config.yaml")
		if err := os.WriteFile(gatewayConfigPath, compiled, 0o600); err != nil {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
			return nil, status.Errorf(codes.Internal, "write gateway config: %v", err)
		}
		gatewayBinds = []string{fmt.Sprintf("%s:/config.yaml:ro", gatewayConfigPath)}
		gatewayConfigDir = perRunConfigDir
	} else {
		// No policy.yaml in deployed dir — deny-all fallback.
		denyAllConfig := []byte("config:\n  dns:\n    lookupFamily: V4Only\nbinds: []\n")
		perRunConfigDir := filepath.Join(s.homePaths.State, "runs", runID, "gateway-config")
		if err := os.MkdirAll(perRunConfigDir, 0o700); err != nil {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
			return nil, status.Errorf(codes.Internal, "create gateway config dir: %v", err)
		}
		denyAllPath := filepath.Join(perRunConfigDir, "config.yaml")
		if err := os.WriteFile(denyAllPath, denyAllConfig, 0o600); err != nil {
			_ = rt.RemoveNetwork(ctx, egressNetID)
			_ = rt.RemoveNetwork(ctx, netID)
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
		_ = rt.RemoveNetwork(ctx, egressNetID)
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "create gateway container: %v", err)
	}

	if err := rt.Start(ctx, gatewayID); err != nil {
		_ = rt.Remove(ctx, gatewayID, true)
		_ = rt.RemoveNetwork(ctx, egressNetID)
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "start gateway container: %v", err)
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
			fmt.Sprintf("HTTP_PROXY=http://%s:7799", gatewayIP),
			fmt.Sprintf("HTTPS_PROXY=http://%s:7799", gatewayIP),
			fmt.Sprintf("http_proxy=http://%s:7799", gatewayIP),
			fmt.Sprintf("https_proxy=http://%s:7799", gatewayIP),
			"NO_PROXY=localhost,127.0.0.1",
			"no_proxy=localhost,127.0.0.1",
		)
	}

	// Write resolved credential values to a sidecar file that is bind-mounted
	// into the agent container. The harness reads this file at startup, loads
	// credential values into memory, and deletes the file before starting the
	// Python worker. This keeps raw secrets out of the Docker ExecWithStdin
	// payload, harness /invoke request body, and Python worker stdin.
	// NOTE: The file is mounted read-write (not :ro) so the harness can delete
	// it after loading. If the harness crashes before reading, the container
	// is stopped and removed on failure, so the file is ephemeral.
	credsPath, credsFileWritten := s.writeCredentialsForRun(runID, deployedDir, gatewayConfigDir)

	agentBinds := []string{fmt.Sprintf("%s:/audit", hostAuditDir)}
	if credsFileWritten {
		agentBinds = append(agentBinds, fmt.Sprintf("%s:/agentpaas/credentials.json", credsPath))
		proxyEnv = append(proxyEnv, "AGENTPAAS_CREDENTIALS_PATH=/agentpaas/credentials.json")
	}

	agentSpec := runtime.ContainerSpec{
		Labels:     runtime.Labels(runtime.ResourceTypeAgent, runID),
		NetworkIDs: []string{string(netID)},
		Binds:      agentBinds,
		Env:        proxyEnv,
	}
	if egressFirewallEnabled() {
		agentSpec.CapAdd = []string{"NET_ADMIN"}
	}

	imageRef := pack.LocalImageRef(agentName, deployed.ImageDigest)
	agentSpec.Image = imageRef
	containerID, err := rt.Create(ctx, agentSpec)
	if err != nil {
		_ = rt.Remove(ctx, gatewayID, true)
		_ = rt.RemoveNetwork(ctx, egressNetID)
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "create container: %v", err)
	}

	if err := rt.Start(ctx, containerID); err != nil {
		_ = rt.Remove(ctx, containerID, true)
		_ = rt.Remove(ctx, gatewayID, true)
		_ = rt.RemoveNetwork(ctx, egressNetID)
		_ = rt.RemoveNetwork(ctx, netID)
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
		timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, 2*time.Minute)
		defer timeoutCancel()
		if stdout, err := s.invokeAgent(timeoutCtx, containerID, agentName, req.GetTriggerPayload()); err != nil {
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
				_ = os.WriteFile(respPath, []byte(stdout), 0o644)
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

	_ = deployed
	return &controlv1.RunResponse{RunId: runID}, nil
}

// cleanupRun removes the run's Docker resources (agent container, gateway
// container, internal network, egress network, gateway config dir, audit
// tailer). Called from the auto-invoke goroutine on success (so completed
// runs don't leak resources) and from Stop() on explicit termination.
// Safe to call multiple times — Remove/RemoveNetwork are idempotent on
// already-removed resources.
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
		_ = rt.Stop(ctx, tr.Gateway, &timeout)
		_ = rt.Remove(ctx, tr.Gateway, true)
	}
	if tr.Container != "" {
		timeout := 10 * time.Second
		_ = rt.Stop(ctx, tr.Container, &timeout)
		_ = rt.Remove(ctx, tr.Container, true)
	}
	if tr.Network != "" {
		_ = rt.RemoveNetwork(ctx, runtime.NetworkID(tr.Network))
	}
	if tr.EgressNetwork != "" {
		_ = rt.RemoveNetwork(ctx, runtime.NetworkID(tr.EgressNetwork))
	}
	if tr.GatewayConfigDir != "" {
		_ = os.RemoveAll(tr.GatewayConfigDir)
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
	if err := rt.Stop(ctx, containerID, &timeout); err != nil {
		return nil, status.Errorf(codes.Internal, "stop container: %v", err)
	}

	// Stop and remove gateway container.
	if tracked.Gateway != "" {
		_ = rt.Stop(ctx, tracked.Gateway, &timeout)
		_ = rt.Remove(ctx, tracked.Gateway, req.GetForce())
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

	containerID, _, _ := s.lookupRun(runID)
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
	defer func() { _ = rc.Close() }()

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

	canonical, _ := policy.Canonicalize(&p)
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
		_ = s.auditIndex.Rebuild(filepath.Join(s.homePaths.State, "audit.jsonl"))
	}
}

func (s *controlServer) invokeAgent(ctx context.Context, containerID runtime.ContainerID, agentName string, triggerPayload []byte) (string, error) {
	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return "", fmt.Errorf("runtime: %w", err)
	}

	// Wait for harness to be ready (agent import phase).
	readyCmd := []string{"python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/readyz', timeout=5)"}
	ready := false
	for i := 0; i < 30; i++ {
		_, _, exitCode, _ := rt.Exec(ctx, containerID, readyCmd)
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
		reserved := map[string]bool{"llm": true, "credentials": true, "mcp": true}
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
	if perr == nil && len(policyData) > 0 {
		parsedPolicy, perr := policy.ParsePolicy(bytes.NewReader(policyData))
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

	return payload, nil
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
func (s *controlServer) writeCredentialsForRun(runID string, deployedDir string, gatewayConfigDir string) (string, bool) {
	if gatewayConfigDir == "" {
		return "", false
	}

	// Resolve the secret store.
	store, err := secrets.NewKeychainStore(secretServiceName(s.homePaths.Home))
	if err != nil {
		return "", false
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
		val, credErr := store.Get(context.Background(), id)
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
		_ = s.auditWriter.Append(tamperRecord)
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
		_ = s.auditWriter.Append(tamperRecord)
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
		_ = s.auditIndex.Rebuild(filepath.Join(s.homePaths.State, "audit.jsonl"))
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
	store, err := s.openIdentityStore()
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
	_ = ctx
	return store, keyID, nil
}

func (s *controlServer) openIdentityStore() (identity.KeyStore, error) {
	if goruntime.GOOS == "darwin" {
		if store, err := identity.NewKeychainKeyStore("agentpaas-daemon"); err == nil {
			return store, nil
		}
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
		Verified:          len(result.Issues) == 0,
		AuditRecordCount:  result.AuditRecordCount,
		AuditHeadSeq:      result.AuditHeadSeq,
		CheckpointCount:   int32(result.CheckpointCount),
		Issues:            issues,
	}
}

func auditRecordToEntry(record audit.AuditRecord) *controlv1.AuditEntry {
	var payload []byte
	if record.Payload != nil {
		payload, _ = json.Marshal(record.Payload)
	}
	ts, _ := time.Parse(time.RFC3339Nano, record.Timestamp)
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
	defer func() { _ = f.Close() }()

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
		_ = w.Write([]string{"seq", "timestamp", "event_type", "deployment_mode", "actor"})
		for _, record := range records {
			_ = w.Write([]string{
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

// resolveExecutable returns the path to the current executable. Tests may override it.
var resolveExecutable = os.Executable

// resolveHarnessBinary finds the agentpaas-harness binary for container images.
// It prefers the linux/arm64 cross-compile (agentpaas-harness-linux) over the
// darwin/arm64 Mac binary. Returns an empty string if not found; pack.BuildImage
// will then fall back to its own exec.LookPath and produce a clear error.
func resolveHarnessBinary() string {
	exePath, err := resolveExecutable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		if p := harnessCandidate(filepath.Join(exeDir, "agentpaas-harness-linux")); p != "" {
			return p
		}
		if p := harnessCandidate(filepath.Join(exeDir, "..", "bin", "agentpaas-harness-linux")); p != "" {
			return p
		}
		if p := harnessCandidate(filepath.Join(exeDir, "agentpaas-harness")); p != "" {
			return p
		}
	}
	if p, err := exec.LookPath("agentpaas-harness-linux"); err == nil {
		return p
	}
	if p, err := exec.LookPath("agentpaas-harness"); err == nil {
		return p
	}
	return ""
}

func harnessCandidate(path string) string {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// resolveSDKDir finds the Python SDK directory (containing agentpaas_sdk)
// relative to the harness binary. The SDK lives in a "python/" subdirectory
// alongside the harness binary (e.g. /usr/local/bin → /usr/local/python).
// If not found there, it checks common repo locations.
func resolveSDKDir(harnessPath string) string {
	if harnessPath == "" {
		return ""
	}

	// Check sibling "python" directory: <harnessDir>/../python
	harnessDir := filepath.Dir(harnessPath)
	candidates := []string{
		filepath.Join(filepath.Dir(harnessDir), "python"),
		filepath.Join(harnessDir, "python"),
	}

	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(c, "agentpaas_sdk")); err == nil && info.IsDir() {
			return c
		}
	}

	// Check if the daemon binary is running from a repo build (bin/ directory)
	if exePath, err := resolveExecutable(); err == nil {
		exeDir := filepath.Dir(exePath)
		// If exeDir is bin/, check ../python
		repoPython := filepath.Join(exeDir, "..", "python")
		if info, err := os.Stat(filepath.Join(repoPython, "agentpaas_sdk")); err == nil && info.IsDir() {
			return repoPython
		}
	}

	return ""
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
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
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
	schedule := &trigger.CronSchedule{
		Expr:              req.GetExpr(),
		AgentName:         req.GetAgentName(),
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

// ListRuns returns all currently tracked agent runs.
func (s *controlServer) ListRuns(ctx context.Context, req *controlv1.ListRunsRequest) (*controlv1.ListRunsResponse, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	runs := make([]*controlv1.RunInfo, 0, len(s.runs))
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
