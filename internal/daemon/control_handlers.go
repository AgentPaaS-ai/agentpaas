package daemon

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/identity"
	"github.com/parvezsyed/agentpaas/internal/pack"
	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/runtime"
	"github.com/parvezsyed/agentpaas/internal/secrets"
	"github.com/parvezsyed/agentpaas/internal/trigger"
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

func (s *stubControlServer) Pack(ctx context.Context, req *controlv1.PackRequest) (*controlv1.PackResponse, error) {
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

	imageTag := fmt.Sprintf("agentpaas/%s:%s", agentName, agentVersion)
	cfg := pack.BuildConfig{
		ProjectDir:  absProjectDir,
		Runtime:     det.Runtime,
		ImageTag:    imageTag,
		HarnessPath: resolveHarnessBinary(),
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

func (s *stubControlServer) Run(ctx context.Context, req *controlv1.RunRequest) (*controlv1.RunResponse, error) {
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

	imageRef := pack.LocalImageRef(agentName, deployed.ImageDigest)
	containerID, err := rt.Create(ctx, runtime.ContainerSpec{
		Image:      imageRef,
		Labels:     runtime.Labels(runtime.ResourceTypeAgent, runID),
		NetworkIDs: []string{string(netID)},
	})
	if err != nil {
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "create container: %v", err)
	}

	if err := rt.Start(ctx, containerID); err != nil {
		_ = rt.Remove(ctx, containerID, true)
		_ = rt.RemoveNetwork(ctx, netID)
		return nil, status.Errorf(codes.Internal, "start container: %v", err)
	}

	s.trackRun(runID, containerID, string(netID))
	if s.eventBus != nil {
		s.eventBus.RegisterRun(runID)
		s.eventBus.Publish(runID, trigger.EventRunStarted, map[string]interface{}{
			"agent_name":   agentName,
			"image_ref":    imageRef,
			"container_id": string(containerID),
			"network":      string(netID),
		})
	}
	s.recordAudit("run_start", "cli", map[string]interface{}{
		"run_id":       runID,
		"agent_name":   agentName,
		"image_ref":    imageRef,
		"container_id": string(containerID),
		"network":      string(netID),
	})
	_ = deployed
	return &controlv1.RunResponse{RunId: runID}, nil
}

func (s *stubControlServer) Stop(ctx context.Context, req *controlv1.StopRequest) (*controlv1.StopResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	containerID, netID := s.lookupRun(runID)
	if containerID == "" {
		return nil, status.Errorf(codes.NotFound, "run %q not found", runID)
	}

	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "docker runtime not available: %v", err)
	}

	timeout := 10 * time.Second
	if req.GetForce() {
		timeout = 0
	}
	if err := rt.Stop(ctx, containerID, &timeout); err != nil {
		return nil, status.Errorf(codes.Internal, "stop container: %v", err)
	}
	_ = rt.Remove(ctx, containerID, req.GetForce())
	if netID != "" {
		_ = rt.RemoveNetwork(ctx, runtime.NetworkID(netID))
	}
	s.untrackRun(runID)
	if s.eventBus != nil {
		eventType := trigger.EventRunSucceeded
		if req.GetForce() {
			eventType = trigger.EventRunCancelled
		}
		s.eventBus.Publish(runID, eventType, map[string]interface{}{
			"container_id": string(containerID),
			"force":        req.GetForce(),
		})
	}
	s.recordAudit("run_stop", "cli", map[string]interface{}{
		"run_id":       runID,
		"container_id": string(containerID),
		"force":        req.GetForce(),
	})
	return &controlv1.StopResponse{Acknowledged: true}, nil
}

func (s *stubControlServer) Logs(req *controlv1.LogsRequest, stream controlv1.ControlService_LogsServer) error {
	runID := req.GetRunId()
	if runID == "" {
		return status.Error(codes.InvalidArgument, "run_id is required")
	}

	containerID, _ := s.lookupRun(runID)
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
			Message:   scanner.Text(),
		}
		if err := stream.Send(entry); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *stubControlServer) PolicyApply(ctx context.Context, req *controlv1.PolicyApplyRequest) (*controlv1.PolicyApplyResponse, error) {
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
	if err := os.WriteFile(policyPath, []byte(yamlContent), 0o600); err != nil {
		return nil, status.Errorf(codes.Internal, "write policy: %v", err)
	}
	if err := os.WriteFile(gatewayPath, compiled, 0o600); err != nil {
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

func (s *stubControlServer) SecretSet(ctx context.Context, req *controlv1.SecretSetRequest) (*controlv1.SecretSetResponse, error) {
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

func (s *stubControlServer) SecretGrant(ctx context.Context, req *controlv1.SecretGrantRequest) (*controlv1.SecretGrantResponse, error) {
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

func (s *stubControlServer) SecretRevoke(ctx context.Context, req *controlv1.SecretRevokeRequest) (*controlv1.SecretRevokeResponse, error) {
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

func (s *stubControlServer) AuditQuery(ctx context.Context, req *controlv1.AuditQueryRequest) (*controlv1.AuditQueryResponse, error) {
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
	return &controlv1.AuditQueryResponse{
		Entries:    entries,
		TotalCount: int32(len(entries)),
	}, nil
}

func (s *stubControlServer) AuditExport(ctx context.Context, req *controlv1.AuditExportRequest) (*controlv1.AuditExportResponse, error) {
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

func (s *stubControlServer) recordAudit(eventType, actor string, payload map[string]interface{}) {
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

func (s *stubControlServer) getOrCreateRuntime() (*runtime.DockerRuntime, error) {
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

func (s *stubControlServer) trackRun(runID string, containerID runtime.ContainerID, networkID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		s.runs = make(map[string]trackedRun)
	}
	s.runs[runID] = trackedRun{Container: containerID, Network: networkID}
}

// activeRunCount returns the number of currently tracked active runs.
func (s *stubControlServer) activeRunCount() int {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return len(s.runs)
}

func (s *stubControlServer) lookupRun(runID string) (runtime.ContainerID, string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		return "", ""
	}
	tracked, ok := s.runs[runID]
	if !ok {
		return "", ""
	}
	return tracked.Container, tracked.Network
}

func (s *stubControlServer) untrackRun(runID string) {
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

func (s *stubControlServer) openPackageIdentityKey(ctx context.Context, agentName string) (identity.KeyStore, identity.KeyID, error) {
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

func (s *stubControlServer) openIdentityStore() (identity.KeyStore, error) {
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

func (s *stubControlServer) recentAuditRecords(limit int) ([]audit.AuditRecord, error) {
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

