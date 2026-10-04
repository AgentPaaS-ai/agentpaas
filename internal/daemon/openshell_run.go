package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/openshellrt"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *controlServer) useOpenShell() bool {
	return s.openshell != nil && s.testRuntime == nil
}

func (s *controlServer) runOnOpenShell(ctx context.Context, req *controlv1.RunRequest, agentName string, isInstalled bool, imageDigest string, credentialMap map[string]string, inputPrep *preparedInputFile) (*controlv1.RunResponse, error) {
	if inputPrep != nil {
		return nil, status.Error(codes.FailedPrecondition, "openshell runtime does not mount input files in this cutover")
	}
	var imageRef string
	if isInstalled {
		imageRef = "sha256:" + strings.TrimPrefix(imageDigest, "sha256:")
	} else {
		imageRef = pack.LocalImageRef(agentName, imageDigest)
	}
	deployedDir := pack.DeployedAgentPath(s.homePaths.Home, agentName)
	rules, brokerHost, credID := loadOpenShellEgress(deployedDir)
	secret, err := s.brokerSecret(credID, credentialMap)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "broker credential: %v", err)
	}
	provider, err := s.openshell.EnsureBroker(ctx, credID, secret)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "openshell provider: %v", err)
	}
	// Drop the secret from this stack frame's later logs. The provider holds it.
	secret = ""
	_ = secret

	runID := generateRunID()
	sandboxName := openShellSandboxName(runID)
	env := map[string]string{
		"AGENTPAAS_OPENSHELL":       "1",
		"AGENTPAAS_AGENT_PATH":      "/app/main.py",
		"AGENTPAAS_AUDIT_PATH":      "/tmp/harness-audit.jsonl",
		"AGENTPAAS_EGRESS_FIREWALL": "0",
		"AGENTPAAS_RUNTIME":         "openshell",
	}
	sb, err := s.openshell.CreateAgentSandbox(ctx, openshellrt.SandboxRequest{
		Name:        sandboxName,
		Image:       imageRef,
		Env:         env,
		Provider:    provider,
		Rules:       rules,
		BrokerHost:  brokerHost,
		HarnessPort: 8080,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}

	hostAuditDir := filepath.Join(s.homePaths.State, "runs", runID, "harness-audit")
	_ = os.MkdirAll(hostAuditDir, 0o700)
	tracked := &trackedRun{
		OpenShellSandbox: sb.Name,
		OpenShellService: sb.ServiceURL,
		AuditDir:         hostAuditDir,
		AgentName:        agentName,
		StartedAt:        time.Now(),
		Status:           "running",
		InvokeDone:       make(chan struct{}),
	}
	s.trackRunPtr(runID, tracked)
	s.recordAudit("run_start", "cli", map[string]interface{}{
		"run_id":     runID,
		"agent_name": agentName,
		"runtime":    "openshell",
		"sandbox":    sb.Name,
		"image_ref":  imageRef,
		"provider":   provider,
	})

	invokeCtx, cancel := context.WithCancel(context.Background())
	tracked.CancelInvoke = cancel
	go s.finishOpenShellInvoke(invokeCtx, tracked, runID, agentName, sb.ServiceURL, req.GetTriggerPayload())

	attemptID := ""
	if stored, err := s.persistLegacyRunAsOneAttempt(ctx, runID, agentName, attemptID); err == nil {
		attemptID = stored
	}
	return &controlv1.RunResponse{
		RunId:     runID,
		AttemptId: attemptID,
		Status:    "RUNNING",
	}, nil
}

func (s *controlServer) finishOpenShellInvoke(invokeCtx context.Context, tr *trackedRun, runID, agentName, serviceURL string, payload []byte) {
	defer close(tr.InvokeDone)
	timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, s.invokeContextTimeout(tr))
	defer timeoutCancel()
	stdout, err := invokeOpenShellService(timeoutCtx, serviceURL, agentName, s, payload, tr)
	if err != nil {
		failReason := invokeFailReason(err)
		s.runMu.Lock()
		tr.Status = "failed"
		tr.InvokeErr = err
		tr.FailReason = failReason
		s.runMu.Unlock()
		s.recordAudit("run_failed", "daemon", map[string]interface{}{
			"run_id":      runID,
			"agent_name":  agentName,
			"runtime":     "openshell",
			"fail_reason": failReason,
		})
		fmt.Fprintf(os.Stderr, "daemon: openshell invoke (%s): %v\n", runID, err)
		s.finalizeRun(context.Background(), runID, tr)
		return
	}
	if invokeStdoutIndicatesError(stdout) {
		failReason := invokeStdoutErrorReason(stdout)
		s.runMu.Lock()
		tr.Status = "failed"
		tr.InvokeErr = fmt.Errorf("%s", failReason)
		tr.FailReason = failReason
		s.runMu.Unlock()
		s.recordAudit("run_failed", "daemon", map[string]interface{}{
			"run_id":      runID,
			"agent_name":  agentName,
			"runtime":     "openshell",
			"fail_reason": failReason,
		})
		s.finalizeRun(context.Background(), runID, tr)
		return
	}
	s.runMu.Lock()
	tr.Status = "succeeded"
	tr.InvokeResponse = stdout
	s.runMu.Unlock()
	if s.homePaths != nil {
		respPath := filepath.Join(s.homePaths.State, "runs", runID, "invoke-response.json")
		if err := os.WriteFile(respPath, []byte(stdout), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: persist invoke response (%s): %v\n", runID, err)
		}
	}
	s.recordAudit("invoke", "daemon", map[string]interface{}{
		"run_id":     runID,
		"agent_name": agentName,
		"runtime":    "openshell",
	})
	s.recordAudit("run_complete", "daemon", map[string]interface{}{
		"run_id":     runID,
		"agent_name": agentName,
		"runtime":    "openshell",
		"exit_code":  0,
	})
	s.finalizeRun(context.Background(), runID, tr)
}

func invokeOpenShellService(ctx context.Context, serviceURL, agentName string, s *controlServer, triggerPayload []byte, tr *trackedRun) (string, error) {
	payload, err := s.buildInvokePayload(ctx, agentName, triggerPayload)
	if err != nil {
		return "", fmt.Errorf("build invoke payload: %w", err)
	}
	if tr != nil && tr.TimeEnvelope != nil {
		payload["time_envelope"] = tr.TimeEnvelope.MarshalForPayload()
	}
	body, err := jsonMarshal(payload)
	if err != nil {
		return "", err
	}
	if err := waitOpenShellReady(ctx, serviceURL); err != nil {
		return "", err
	}
	return postOpenShell(ctx, serviceURL, "/invoke", body)
}

func waitOpenShellReady(ctx context.Context, serviceURL string) error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, last = postOpenShell(ctx, serviceURL, "/readyz", nil)
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if last == nil {
		last = fmt.Errorf("harness /readyz not ready")
	}
	return last
}

func postOpenShell(ctx context.Context, serviceURL, path string, body []byte) (string, error) {
	u, err := url.Parse(serviceURL)
	if err != nil {
		return "", fmt.Errorf("openshell service url: %w", err)
	}
	host := u.Host
	if host == "" {
		host = u.Path
	}
	port := u.Port()
	if port == "" {
		port = "17670"
	}
	target := "http://" + net.JoinHostPort("127.0.0.1", port) + path
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, reader)
	if err != nil {
		return "", err
	}
	if path == "/readyz" {
		req.Method = http.MethodGet
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("openshell service %s: HTTP %d", path, resp.StatusCode)
	}
	return string(raw), nil
}

func loadOpenShellEgress(deployedDir string) (rules []openshellrt.EgressRule, brokerHost, credID string) {
	policyPath := filepath.Join(deployedDir, "policy.yaml")
	if data, err := os.ReadFile(policyPath); err == nil {
		if parsed, err := policy.ParsePolicy(bytes.NewReader(data)); err == nil {
			for _, eg := range parsed.Egress {
				rules = append(rules, openshellrt.EgressRule{
					Domain:  eg.Domain,
					Ports:   eg.Ports,
					Methods: eg.Methods,
				})
			}
		}
	}
	lockPath := filepath.Join(deployedDir, "agent.lock")
	if lock, err := pack.ReadAgentLock(lockPath); err == nil && lock != nil && lock.AgentYAML != nil {
		credID = lock.AgentYAML.LLM.Credential
		if adapter := llm.GetAdapter(lock.AgentYAML.LLM.Provider); adapter != nil {
			brokerHost = adapter.Endpoint()
			if u, err := url.Parse(brokerHost); err == nil && u.Host != "" {
				brokerHost = u.Hostname()
			}
		}
	}
	if brokerHost == "" {
		brokerHost = "openrouter.ai"
	}
	return rules, brokerHost, credID
}

func (s *controlServer) brokerSecret(credID string, credentialMap map[string]string) (string, error) {
	if strings.TrimSpace(credID) == "" {
		return "", fmt.Errorf("agent has no llm credential")
	}
	var store secrets.SecretStore
	if s.secretStoreForTest != nil {
		store = s.secretStoreForTest
	} else {
		var err error
		store, err = secrets.NewKeychainStore(secretServiceName(s.homePaths.Home))
		if err != nil {
			return "", err
		}
	}
	lookup := credID
	if credentialMap != nil {
		if local, ok := credentialMap[credID]; ok && local != "" {
			lookup = local
		}
	}
	val, err := store.Get(context.Background(), lookup)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(val)) == "" {
		return "", fmt.Errorf("credential %q is empty", lookup)
	}
	return string(val), nil
}

// openShellSandboxNameMax is OpenShell's routable-name limit. A longer
// sandbox Name is rejected: "name exceeds maximum length (N > 19)".
const openShellSandboxNameMax = 19

// openShellSandboxName maps a run ID to an OpenShell sandbox Name.
// generateRunID is "run-" plus 16 hex (20) or a longer clock fallback, and
// that ID stays intact for state paths and tracking. The sandbox Name is a
// DNS-1123 label of at most 19 characters. The happy path keeps all 16 hex
// digits under a one-character prefix ("r" + 16 = 17). Longer IDs are hashed
// so distinct values that share a 19-character prefix do not collide.
func openShellSandboxName(runID string) string {
	const maxBody = openShellSandboxNameMax - 1
	body := strings.TrimPrefix(runID, "run-")
	if body != "" && len(body) <= maxBody && dns1123Alnum(body) {
		return "r" + body
	}
	sum := sha256.Sum256([]byte(runID))
	// "s" keeps hashed names disjoint from the "r"+entropy happy path.
	return "s" + hex.EncodeToString(sum[:8])
}

func dns1123Alnum(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
