package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

type harnessRPCServer struct {
	listener *net.UnixListener
	addr     string
	socket   string
	done     chan struct{}

	mu          sync.RWMutex
	invoke      *rpcInvokeState
	audit       AuditAppender
	router      *mcpmanager.Router
	credentials map[string]rpcCredential // Pre-loaded credential values (from sidecar file)

	// Delegation trust state (B32-T03) — injected at invoke bootstrap.
	// NEVER serialized to agent responses.
	delegationTrust *DelegationTrustState

	// Progress journal (B27) — pre-loaded at startup, wired into invoke state
	// by SetInvoke. Stored on the server so LoadProgressMetadata can populate
	// them before the first invoke.
	progressJournal  *progressJournalWriter
	progressIdentity progressIdentity

	// nowMonotonicMs supplies the monotonic millisecond timestamp used to
	// evaluate the TimeEnvelope in handleLLM (B30-T03 Part B, ceiling 5). When
	// nil, routedrun.NowMonotonicMs(nil) (time.Now().UnixMilli()) is used.
	nowMonotonicMs func() int64
}

type rpcInvokeState struct {
	budget      *BudgetEnforcer
	payload     map[string]any
	terminate   func()
	credentials map[string]rpcCredential
	mcpAllowed  map[string]map[string]bool

	// timeEnvelope is the authoritative active-time envelope (B30-T03 Part B,
	// ceiling 5). When present, the LLM HTTP client timeout is derived from
	// env.EffectiveOperationDeadlineMs(nowMs, env.ModelCallTimeoutMs). When
	// nil (legacy v0.2.3 compat path), the legacy 120s constant applies.
	timeEnvelope *routedrun.TimeEnvelope

	// Progress journal (B27)
	progressJournal  *progressJournalWriter
	progressIdentity progressIdentity
	leaseExpired     atomic.Bool
	resumeCheckpoint map[string]any // B35-provided resume data (trusted, not from trigger)
	resumeReason     string         // trusted enum: failure_continuation|operator_pause_resume

	mu              sync.Mutex
	failureEvidence *UpstreamEvidence
}

type rpcCredential struct {
	Header string
	Value  string
}

type rpcRequest struct {
	ID     string         `json:"id,omitempty"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string `json:"id,omitempty"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

func startHarnessRPCServer(appender AuditAppender) (*harnessRPCServer, error) {
	dir, err := os.MkdirTemp("", "agentpaas-rpc-*")
	if err != nil {
		return nil, fmt.Errorf("start harness rpcserver: %w", err)
	}
	socket := filepath.Join(dir, "rpc.sock")
	addr, err := net.ResolveUnixAddr("unix", socket)
	if err != nil {
		_ = os.RemoveAll(dir) // best-effort temp cleanup on error path
		return nil, fmt.Errorf("start harness rpcserver: %w", err)
	}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		_ = os.RemoveAll(dir) // best-effort temp cleanup on error path
		return nil, fmt.Errorf("start harness rpcserver: %w", err)
	}
	s := &harnessRPCServer{
		listener: listener,
		addr:     socket,
		socket:   socket,
		done:     make(chan struct{}),
		audit:    appender,
	}
	go s.serve()
	return s, nil
}

// harnessRPCServer.Addr returns the address for harness rpc server.
func (s *harnessRPCServer) Addr() string {
	return s.addr
}

// harnessRPCServer.Close closes harness rpc server.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *harnessRPCServer) Close() error {
	err := s.listener.Close()
	<-s.done
	return errors.Join(err, os.RemoveAll(filepath.Dir(s.socket)))
}

// harnessRPCServer.SetInvoke sets the invoke.
func (s *harnessRPCServer) SetInvoke(payload map[string]any, budget *BudgetEnforcer, terminate func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Extract the TimeEnvelope from the payload (B30-T03 Part B, ceiling 5).
	// Absent (legacy v0.2.3 trigger path) → nil, modelClientTimeout falls
	// back to the legacy 120s constant.
	var envPtr *routedrun.TimeEnvelope
	if env, ok := routedrun.UnmarshalTimeEnvelopeFromPayload(payload); ok {
		envPtr = &env
	}
	s.invoke = &rpcInvokeState{
		budget:           budget,
		payload:          payload,
		terminate:        terminate,
		credentials:      s.credentials, // Use pre-loaded credentials, not from payload
		mcpAllowed:       mcpAllowlistFromPayload(payload),
		progressJournal:  s.progressJournal,  // B27: pre-loaded journal writer
		progressIdentity: s.progressIdentity, // B27: pre-loaded identity
		timeEnvelope:     envPtr,
	}
}

// harnessRPCServer.ClearInvoke clears invoke.
func (s *harnessRPCServer) ClearInvoke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoke = nil
}

// legacyModelClientTimeout is the v0.2.3 fixed HTTP timeout for LLM calls.
// It is used ONLY when no TimeEnvelope is available on the durable path
// (legacy compat). On the durable path the timeout is derived from
// env.EffectiveOperationDeadlineMs(nowMs, env.ModelCallTimeoutMs).
const legacyModelClientTimeout = 120 * time.Second

// modelClientTimeout returns the HTTP client timeout for an LLM call. When a
// TimeEnvelope is attached to the invoke state, the timeout is derived from
// env.EffectiveOperationDeadlineMs(nowMs, env.ModelCallTimeoutMs) — the min of
// the model-call timeout, the attempt-lease remaining, and the active time
// remaining (B30-T03 Part B, ceiling 5). When no envelope is present (legacy
// v0.2.3 compat), it falls back to legacyModelClientTimeout.
func (s *harnessRPCServer) modelClientTimeout(state *rpcInvokeState) time.Duration {
	if state != nil && state.timeEnvelope != nil {
		nowMs := routedrun.NowMonotonicMs(nil)
		if s.nowMonotonicMs != nil {
			nowMs = s.nowMonotonicMs()
		}
		deadlineMs := state.timeEnvelope.EffectiveOperationDeadlineMs(nowMs, state.timeEnvelope.ModelCallTimeoutMs)
		if deadlineMs <= 0 {
			// Envelope exhausted: allow a tiny grace (1ms) so the call surfaces
			// a structured error rather than an immediate zero-timeout panic.
			return 1 * time.Millisecond
		}
		return time.Duration(deadlineMs) * time.Millisecond
	}
	return legacyModelClientTimeout
}

// SetProgressMetadata wires the progress journal, identity, and resume
// checkpoint into the current invoke state. This is called by the daemon
// after SetInvoke but before the Python worker starts, once the journal key
// and resume data are available. T02 spec item 2 & 9.
func (s *harnessRPCServer) SetProgressMetadata(
	journal *progressJournalWriter,
	identity progressIdentity,
	resumeCheckpoint map[string]any,
	resumeReason string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invoke == nil {
		return
	}
	s.invoke.progressJournal = journal
	s.invoke.progressIdentity = identity
	s.invoke.resumeCheckpoint = resumeCheckpoint
	s.invoke.resumeReason = resumeReason
}

// harnessRPCServer.FailureEvidence failure evidence.
func (s *harnessRPCServer) FailureEvidence() *UpstreamEvidence {
	state := s.currentInvoke()
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.failureEvidence == nil {
		return nil
	}
	evidence := *state.failureEvidence
	if evidence.Headers != nil {
		evidence.Headers = cloneStringMap(evidence.Headers)
	}
	return &evidence
}

func (s *harnessRPCServer) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *harnessRPCServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }() // best-effort close
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if encErr := encoder.Encode(rpcResponse{OK: false, Error: err.Error(), Code: "invalid_json"}); encErr != nil {
				log.Printf("harness: rpc encode error response: %v", encErr)
			}
			continue
		}
		if encErr := encoder.Encode(s.handleRequest(req)); encErr != nil {
			log.Printf("harness: rpc encode response: %v", encErr)
		}
	}
}

func (s *harnessRPCServer) handleRequest(req rpcRequest) rpcResponse {
	// Delegation methods are invoked during an active agent run but
	// have their own trust state — they don't require invoke state.
	switch req.Method {
	case "delegate_task", "get_task", "list_task_events":
		return s.handleDelegationMethod(req)
	}

	state := s.currentInvoke()
	if state == nil {
		return rpcError(req.ID, "no active invoke", "no_active_invoke")
	}
	switch req.Method {
	case "llm":
		return s.handleLLM(req, state)
	case "record_iteration":
		return s.handleRecordIteration(req, state)
	case "http":
		return s.handleHTTP(req, state, false)
	case "http_with_credential":
		return s.handleHTTP(req, state, true)
	case "mcp":
		return s.handleMCP(req, state)
	case "progress":
		return s.handleProgress(req, state)
	default:
		return rpcError(req.ID, fmt.Sprintf("unknown method %q", req.Method), "unknown_method")
	}
}

func (s *harnessRPCServer) handleDelegationMethod(req rpcRequest) rpcResponse {
	switch req.Method {
	case "delegate_task":
		return s.handleDelegateTask(req)
	case "get_task":
		return s.handleGetTask(req)
	case "list_task_events":
		return s.handleListTaskEvents(req)
	default:
		return rpcError(req.ID, fmt.Sprintf("unknown delegation method %q", req.Method), "unknown_method")
	}
}

func (s *harnessRPCServer) currentInvoke() *rpcInvokeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.invoke
}

// harnessRPCServer.SetRouter sets the router.
func (s *harnessRPCServer) SetRouter(router *mcpmanager.Router) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.router = router
}

// SetCredentialsForTest directly injects credentials without file I/O.
// Intended for test use only where the sidecar file flow is impractical.
func (s *harnessRPCServer) SetCredentialsForTest(creds map[string]rpcCredential) {
	s.mu.Lock()
	s.credentials = creds
	s.mu.Unlock()
}

// LoadCredentials reads credential values from a JSON file at the given path.
// The file contains an array of {id, header, value} objects. After loading,
// the credentials are stored in memory and never exposed to agent code.
// The file is mounted read-only; the daemon removes it after the run.
func (s *harnessRPCServer) LoadCredentials(path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("harness rpcserver load credentials: %w", err)
	}
	// NOTE: File is mounted read-only (BB-1); daemon cleans up after run.

	type credEntry struct {
		ID     string `json:"id"`
		Header string `json:"header"`
		Value  string `json:"value"`
	}
	var entries []credEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("unmarshal credentials file: %w", err)
	}

	creds := make(map[string]rpcCredential)
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		creds[e.ID] = rpcCredential{
			Header: e.Header,
			Value:  e.Value,
		}
	}

	s.mu.Lock()
	s.credentials = creds
	s.mu.Unlock()
	return nil
}

// LoadProgressMetadata reads the journal key from the sidecar file,
// constructs a progressJournalWriter and progressIdentity, and stores
// them on the server for use by SetInvoke. The key file is deleted after
// loading. If the key is missing or invalid, returns an error (fail-closed:
// progress requires a valid key).
func (s *harnessRPCServer) LoadProgressMetadata(cfg Config) error {
	key, err := loadJournalKey(cfg.JournalKeyPath)
	if err != nil {
		return fmt.Errorf("load journal key: %w", err)
	}
	// Delete the key file immediately so agent code cannot read it.
	if err := os.Remove(cfg.JournalKeyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("harness: failed to remove journal key file %s: %v (key may remain readable by agent)", cfg.JournalKeyPath, err)
	}

	identity := progressIdentity{
		RunID:     cfg.RunID,
		AttemptID: cfg.AttemptID,
		LeaseID:   cfg.LeaseID,
	}

	writer, err := newProgressJournalWriter(cfg.JournalPath, key, identity)
	if err != nil {
		return fmt.Errorf("create journal writer: %w", err)
	}
	s.mu.Lock()
	s.progressJournal = writer
	s.progressIdentity = identity
	s.mu.Unlock()
	return nil
}

func (s *harnessRPCServer) handleLLM(req rpcRequest, state *rpcInvokeState) rpcResponse {
	prompt := stringParam(req.Params, "prompt")

	// Read optional model override from params.
	modelOverride := stringParam(req.Params, "model")

	// Read LLM config from payload (set by daemon at invoke time).
	llmConfig, _ := state.payload["llm"].(map[string]any) // optional; nil means no config

	// Backward compat: no LLM config → fake response only in test mode.
	// In production, fail-closed with a structured error.
	if llmConfig == nil {
		if os.Getenv("AGENTPAAS_TEST_FAKE_LLM") == "1" {
			cg := guardrailsFromPayload(state.payload)
			promptAfterGuard, gerr := applyGuardrailsToText(cg, prompt, "request", state.credentials)
			if gerr != nil {
				return rpcError(req.ID, gerr.Error(), StatusGuardrailBlocked)
			}
			prompt = promptAfterGuard
			if sp := injectSystemPromptFromPayload(state.payload); sp != "" {
				prompt = combineSystemPrompt(sp, prompt)
			}
			text := "agentpaas fake llm response"
			text, gerr = applyGuardrailsToText(cg, text, "response", state.credentials)
			if gerr != nil {
				return rpcError(req.ID, gerr.Error(), StatusGuardrailBlocked)
			}
			tokens := int64(len(strings.Fields(prompt)))
			if tokens == 0 && prompt != "" {
				tokens = 1
			}
			if err := state.budget.RecordTokens(tokens); err != nil {
				if errors.Is(err, ErrBudgetExceeded) && state.terminate != nil {
					go state.terminate()
					return rpcError(req.ID, err.Error(), StatusBudgetExceeded)
				}
				return rpcError(req.ID, err.Error(), "llm_failed")
			}
			return rpcResponse{
				ID: req.ID,
				OK: true,
				Result: map[string]any{
					"text":    text,
					"content": text, // alias for common OpenAI-style usage (Bug 024)
					"tokens":  tokens,
				},
			}
		}
		return rpcError(req.ID, "llm not configured; configure llm in agent.yaml or set AGENTPAAS_TEST_FAKE_LLM=1 for testing", "llm_failed")
	}

	provider := firstString(llmConfig, "provider")
	model := firstString(llmConfig, "model")
	credentialID := firstString(llmConfig, "credential")
	maxTokensPerRequest := 0
	if budget, ok := state.payload["budget"].(map[string]any); ok {
		if value, ok := budget["max_tokens_per_request"].(int); ok {
			maxTokensPerRequest = value
		}
		if value, ok := budget["max_tokens_per_request"].(float64); ok {
			maxTokensPerRequest = int(value)
		}
	}

	// Get the provider adapter.
	adapter := llm.GetAdapter(provider)
	if adapter == nil {
		s.auditEgressDecision("harness", "", "POST", credentialID, "", "denied", "unknown llm provider: "+provider)
		return rpcError(req.ID, "unknown llm provider: "+provider, "llm_failed")
	}

	// Get credential value from state.credentials.
	cred, ok := state.credentials[credentialID]
	if !ok {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, "", "denied", "llm credential not declared")
		return rpcError(req.ID, "llm credential not declared", "credential_denied")
	}
	if strings.TrimSpace(cred.Value) == "" {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, "", "denied", "llm credential value empty")
		return rpcError(req.ID, "llm credential value is empty; re-run agentpaas secret add "+credentialID+" and repack/run", "credential_denied")
	}

	// Use model override if provided.
	if modelOverride != "" {
		model = modelOverride
	}

	// Harness-level guardrails (T16): agentgateway v1.3.0 has no route-level
	// guardrails field for host backends. Enforce request-side before egress.
	cg := guardrailsFromPayload(state.payload)
	promptAfterGuard, gerr := applyGuardrailsToText(cg, prompt, "request", state.credentials)
	if gerr != nil {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, "", "denied", gerr.Error())
		return rpcError(req.ID, gerr.Error(), StatusGuardrailBlocked)
	}
	prompt = promptAfterGuard

	// T18: inject_system_prompt (not expressible as host-backend gateway transform).
	if sp := injectSystemPromptFromPayload(state.payload); sp != "" {
		prompt = combineSystemPrompt(sp, prompt)
	}

	// Build the HTTP request.
	ctx := context.Background()
	var httpReq *http.Request
	var err error
	if maxTokensPerRequest > 0 {
		httpReq, err = adapter.BuildRequest(ctx, model, prompt, cred.Value, maxTokensPerRequest)
	} else {
		httpReq, err = adapter.BuildRequest(ctx, model, prompt, cred.Value)
	}
	if err != nil {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, "", "denied", "build request failed: "+err.Error())
		return rpcError(req.ID, err.Error(), "llm_failed")
	}

	// Rewrite URL for gateway-native HTTP routing (Bug 021). Preserve the
	// original Host header so the gateway can match routes by hostname.
	originalEndpoint := adapter.Endpoint()
	gatewayURL := os.Getenv("AGENTPAAS_GATEWAY_URL")
	if gatewayURL != "" {
		rewritten, rewriteErr := rewriteURLForGateway(originalEndpoint, gatewayURL)
		if rewriteErr != nil {
			s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, "", "denied", "gateway rewrite failed: "+rewriteErr.Error())
			return rpcError(req.ID, rewriteErr.Error(), "llm_failed")
		}
		origU, parseErr := url.Parse(originalEndpoint)
		if parseErr != nil {
			s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, "", "denied", "parse original URL: "+parseErr.Error())
			return rpcError(req.ID, parseErr.Error(), "llm_failed")
		}
		rewrittenU, parseErr := url.Parse(rewritten)
		if parseErr != nil {
			s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, "", "denied", "parse rewrittenURL: "+parseErr.Error())
			return rpcError(req.ID, parseErr.Error(), "llm_failed")
		}
		httpReq.URL = rewrittenU
		httpReq.Host = origU.Host
	}

	// Execute the HTTP request.
	// LLM calls (especially reasoning models like grok-4.3, o3, etc.) can take
	// 30+ seconds to respond. The previous 5s timeout killed requests before
	// the provider returned, causing "context deadline exceeded" failures on
	// anything requiring non-trivial reasoning.
	//
	// B30-T03 Part B (ceiling 5): the timeout is now derived from the
	// TimeEnvelope's EffectiveOperationDeadlineMs when one is available; when
	// no envelope is present (legacy v0.2.3 compat), modelClientTimeout
	// returns legacyModelClientTimeout (120s).
	//
	// BUG-033/034 fix: deny HTTP redirects. The gateway rewrites URLs to
	// http://gateway:7799/path; a 302 redirect target is an HTTPS URL that
	// the client cannot TLS-terminate directly (the gateway does TLS). Without
	// this guard, Go's default redirect follower (up to 10 hops) tries to
	// connect directly to the redirect target, bypassing the gateway's
	// egress policy and producing TLS handshake errors.
	client := &http.Client{
		Timeout: s.modelClientTimeout(state),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, "", "denied", "http request failed: "+err.Error())
		return rpcError(req.ID, err.Error(), "llm_failed")
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close

	// Read response body (1 MB limit, same as handleHTTP).
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, strconv.Itoa(resp.StatusCode), "denied", "response read failed: "+err.Error())
		return rpcError(req.ID, err.Error(), "llm_failed")
	}

	// Parse the response.
	result, err := adapter.ParseResponse(resp.StatusCode, respBody)
	if err != nil {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, strconv.Itoa(resp.StatusCode), "denied", err.Error())
		return rpcError(req.ID, err.Error(), "llm_failed")
	}

	// Response-side guardrails (same ruleset as request for regex/webhook).
	respText, gerr := applyGuardrailsToText(cg, result.Text, "response", state.credentials)
	if gerr != nil {
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, strconv.Itoa(resp.StatusCode), "denied", gerr.Error())
		return rpcError(req.ID, gerr.Error(), StatusGuardrailBlocked)
	}
	result.Text = respText

	// Record tokens (use provider tokens, fall back to word-count estimate).
	tokens := result.Tokens
	if tokens == 0 {
		tokens = int64(len(strings.Fields(result.Text)))
		if tokens == 0 && result.Text != "" {
			tokens = 1
		}
	}
	if err := state.budget.RecordTokens(tokens); err != nil {
		if errors.Is(err, ErrBudgetExceeded) && state.terminate != nil {
			go state.terminate()
			s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, strconv.Itoa(resp.StatusCode), "denied", err.Error())
			return rpcError(req.ID, err.Error(), StatusBudgetExceeded)
		}
		s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, strconv.Itoa(resp.StatusCode), "denied", err.Error())
		return rpcError(req.ID, err.Error(), "llm_failed")
	}
	if observabilityEnabled(state.payload) {
		inputTokens, outputTokens := result.InputTokens, result.OutputTokens
		if inputTokens == 0 && outputTokens == 0 {
			outputTokens = tokens
		}
		s.auditLLMResult(provider, model, inputTokens, outputTokens, result.Tokens)
	}

	// Audit allowed egress.
	respModel := result.Model
	if respModel == "" {
		respModel = model
	}
	s.auditEgressDecision("harness", adapter.Endpoint(), "POST", credentialID, strconv.Itoa(resp.StatusCode), "allowed", "")

	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"text":    result.Text,
			"content": result.Text, // alias for common OpenAI-style usage (Bug 024)
			"tokens":  tokens,
			"model":   respModel,
		},
	}
}

func observabilityEnabled(payload map[string]any) bool {
	config, ok := payload["observability"].(map[string]any)
	if !ok {
		return false
	}
	switch value := config["cost_tracking"].(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}

func (s *harnessRPCServer) auditLLMResult(provider, model string, inputTokens, outputTokens, totalTokens int64) {
	if s.audit == nil {
		return
	}
	if totalTokens == 0 {
		totalTokens = inputTokens + outputTokens
	}
	if err := s.audit.Append(audit.AuditRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), EventType: "llm_result",
		DeploymentMode: "local", Actor: "harness",
		Payload: map[string]interface{}{
			"provider": provider, "model": model,
			"input_tokens": inputTokens, "output_tokens": outputTokens, "total_tokens": totalTokens,
			"estimated_cost_usd": llm.EstimateCost(provider, model, inputTokens, outputTokens),
		},
	}); err != nil {
		// Audit is best-effort; log but don't fail the harness.
		fmt.Fprintf(os.Stderr, "harness: audit append failed: %v\n", err)
	}
}

func (s *harnessRPCServer) handleRecordIteration(req rpcRequest, state *rpcInvokeState) rpcResponse {
	if err := state.budget.RecordIteration(); err != nil {
		if errors.Is(err, ErrBudgetExceeded) && state.terminate != nil {
			go state.terminate()
			return rpcError(req.ID, err.Error(), StatusBudgetExceeded)
		}
		return rpcError(req.ID, err.Error(), "iteration_failed")
	}
	return rpcResponse{ID: req.ID, OK: true, Result: map[string]any{"recorded": true}}
}

func (s *harnessRPCServer) handleHTTP(req rpcRequest, state *rpcInvokeState, withCredential bool) rpcResponse {
	start := time.Now()
	method := strings.ToUpper(defaultString(stringParam(req.Params, "method"), http.MethodGet))
	rawURL := stringParam(req.Params, "url")
	if rawURL == "" {
		return rpcError(req.ID, "url is required", "invalid_http_request")
	}
	body := stringParam(req.Params, "body")
	bodyMarker, bodyHash := redactedBodyEvidence(body)
	credentialValue := ""
	credID := ""

	// Rewrite for gateway-native routing (Bug 021). Original URL retained for
	// audit/evidence; Host header set to original hostname for route matching.
	requestURL := rawURL
	var originalHost string
	gatewayURL := os.Getenv("AGENTPAAS_GATEWAY_URL")
	if gatewayURL != "" {
		rewritten, rewriteErr := rewriteURLForGateway(rawURL, gatewayURL)
		if rewriteErr != nil {
			s.auditEgressDecision("harness", rawURL, method, "", "", "denied", "gateway rewrite failed: "+rewriteErr.Error())
			state.setFailureEvidence(&UpstreamEvidence{
				Availability: AvailabilityUnavailable,
				Method:       method,
				URL:          sanitizedURL(rawURL),
				TimingMS:     elapsedMS(start),
				BodyHash:     bodyHash,
				BodyRedacted: bodyMarker,
			})
			return rpcError(req.ID, rewriteErr.Error(), "invalid_http_request")
		}
		if origU, parseErr := url.Parse(rawURL); parseErr == nil {
			originalHost = origU.Host
		}
		requestURL = rewritten
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), method, requestURL, strings.NewReader(body))
	if err != nil {
		s.auditEgressDecision("harness", rawURL, method, "", "", "denied", "invalid request: "+err.Error())
		state.setFailureEvidence(&UpstreamEvidence{
			Availability: AvailabilityUnavailable,
			Method:       method,
			URL:          sanitizedURL(rawURL),
			TimingMS:     elapsedMS(start),
			BodyHash:     bodyHash,
			BodyRedacted: bodyMarker,
		})
		return rpcError(req.ID, err.Error(), "invalid_http_request")
	}
	if originalHost != "" {
		httpReq.Host = originalHost
	}
	for key, value := range stringMapParam(req.Params, "headers") {
		httpReq.Header.Set(key, value)
	}
	if withCredential {
		credID = stringParam(req.Params, "credential_id")
		cred, ok := state.credentials[credID]
		if !ok {
			s.auditEgressDecision("harness", rawURL, method, credID, "", "denied", "credential not declared")
			state.setFailureEvidence(&UpstreamEvidence{
				Availability: AvailabilityForbidden,
				Method:       method,
				URL:          sanitizedURL(rawURL),
				TimingMS:     elapsedMS(start),
				Headers:      hashedHeaders(httpReq.Header),
				BodyHash:     bodyHash,
				BodyRedacted: bodyMarker,
				Credential:   redactedCredentialEvidence(),
			})
			return rpcError(req.ID, "credential is not declared", "credential_denied")
		}
		header := defaultString(cred.Header, "Authorization")
		httpReq.Header.Set(header, cred.Value)
		credentialValue = cred.Value
	}
	// BUG-033/034 fix: deny HTTP redirects. Same rationale as handleLLM —
	// a redirect target bypasses the gateway's egress policy and may produce
	// TLS handshake errors when the client tries to connect directly.
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		// HTTP request failed — this is an egress denial when the container
		// is on an internal-only network (connection refused, timeout, no route).
		reason := "http request failed: " + err.Error()
		s.auditEgressDecision("harness", rawURL, method, "", "", "denied", reason)
		headers := hashedHeaders(httpReq.Header)
		if withCredential {
			headers["credential"] = sha256HexString(redactedCredentialEvidence())
		}
		state.setFailureEvidence(&UpstreamEvidence{
			Availability: AvailabilityUnavailable,
			Method:       method,
			URL:          sanitizedURL(rawURL),
			TimingMS:     elapsedMS(start),
			Headers:      headers,
			BodyHash:     bodyHash,
			BodyRedacted: bodyMarker,
			Credential:   redactedCredentialEvidence(),
		})
		return rpcError(req.ID, err.Error(), "http_failed")
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		s.auditEgressDecision("harness", rawURL, method, "", strconv.Itoa(resp.StatusCode), "denied", "response read failed: "+err.Error())
		state.setFailureEvidence(&UpstreamEvidence{
			StatusCode:   resp.StatusCode,
			Availability: availabilityFromStatus(resp.StatusCode),
			Method:       method,
			URL:          sanitizedURL(rawURL),
			TimingMS:     elapsedMS(start),
			Headers:      hashedHeaders(resp.Header),
			BodyHash:     bodyHash,
			BodyRedacted: bodyMarker,
		})
		return rpcError(req.ID, err.Error(), "http_failed")
	}
	// Gateway-native policy denials return HTTP 403 with an explicit body.
	// Treat these as egress_denied (not allowed) even though TCP to the gateway succeeded.
	bodyStr := redactCredentialValue(string(respBody), credentialValue)
	if isGatewayEgressDenied(resp.StatusCode, bodyStr) {
		reason := "gateway denied egress"
		if bodyStr != "" {
			reason = bodyStr
		}
		s.auditEgressDecision("harness", rawURL, method, "", strconv.Itoa(resp.StatusCode), "denied", reason)
		state.setFailureEvidence(&UpstreamEvidence{
			StatusCode:   resp.StatusCode,
			Availability: AvailabilityForbidden,
			Method:       method,
			URL:          sanitizedURL(rawURL),
			TimingMS:     elapsedMS(start),
			Headers:      hashedHeaders(resp.Header),
			BodyHash:     bodyHash,
			BodyRedacted: bodyMarker,
		})
		return rpcError(req.ID, reason, "http_failed")
	}
	// BUG-033: detect and audit HTTP redirects. Redirects are NOT followed
	// (CheckRedirect returns ErrUseLastResponse). We log the redirect target
	// so the user can see what domain the server tried to redirect to, and
	// decide whether to add it to the egress policy.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		redirectTarget := resp.Header.Get("Location")
		reason := fmt.Sprintf("redirect not followed: %d → %s", resp.StatusCode, redirectTarget)
		s.auditEgressDecision("harness", rawURL, method, credID, strconv.Itoa(resp.StatusCode), "denied", reason)
		return rpcResponse{
			ID: req.ID,
			OK: true,
			Result: map[string]any{
				"status":       resp.StatusCode,
				"status_code":  resp.StatusCode,
				"headers":      redactedHeaders(resp.Header),
				"body":         bodyStr,
				"redirect_url": redirectTarget,
			},
		}
	}
	// HTTP request succeeded — record as allowed egress.
	s.auditEgressDecision("harness", rawURL, method, credID, strconv.Itoa(resp.StatusCode), "allowed", "")
	// Expose both "status" (canonical) and "status_code" (common alias).
	// Agents that check either must see the real HTTP status; missing the
	// alias caused false "Failed to fetch" errors after successful egress.
	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"status":      resp.StatusCode,
			"status_code": resp.StatusCode,
			"headers":     redactedHeaders(resp.Header),
			"body":        bodyStr,
		},
	}
}

// redactCredentialValue prevents a credential from becoming agent-visible when
// an upstream echoes request headers (for example, httpbin /headers). The
// broker may inject a secret into an outbound request, but no response body,
// invoke result, or persisted run artifact may carry that value back.
func redactCredentialValue(body, credentialValue string) string {
	if credentialValue == "" {
		return body
	}
	return strings.ReplaceAll(body, credentialValue, "[REDACTED:credential]")
}

// isGatewayEgressDenied detects agentgateway allowlist denials under native HTTP routing.
func isGatewayEgressDenied(statusCode int, body string) bool {
	if statusCode != http.StatusForbidden {
		return false
	}
	lb := strings.ToLower(body)
	return strings.Contains(lb, "egress denied") ||
		strings.Contains(lb, "domain not in allowlist") ||
		strings.Contains(lb, "not in allowlist")
}

// mcpServiceNotEnabledCode is the typed error name reserved by B26
// (internal/daemon/routed_handlers.go) for the managed MCP service-not-enabled
// path. The no-router harness branch reuses it so the same typed denial
// surfaces regardless of which layer rejects the call.
const mcpServiceNotEnabledCode = "agentpaas_mcp_service_not_enabled"

// mcpServiceNotEnabledReason is the audit/payload reason string for the
// production no-router MCP path. It carries the typed code name so consumers
// can match on the stable identifier rather than a free-form message.
const mcpServiceNotEnabledReason = "agentpaas_mcp_service_not_enabled: managed mcp service is not enabled until B33"

func (s *harnessRPCServer) handleMCP(req rpcRequest, state *rpcInvokeState) rpcResponse {
	start := time.Now()
	serverID := stringParam(req.Params, "server_id")
	tool := stringParam(req.Params, "tool")
	input := req.Params["input"]
	inputHash := hashJSONValue(input)
	s.mu.RLock()
	router := s.router
	s.mu.RUnlock()
	if router != nil {
		result, err := router.CallTool(context.Background(), serverID, tool, input, "harness", "test-run")
		if err != nil {
			s.auditMCPDenied(serverID, tool, err.Error())
			state.setFailureEvidence(&UpstreamEvidence{
				Availability: AvailabilityForbidden,
				TimingMS:     elapsedMS(start),
				BodyHash:     inputHash,
				BodyRedacted: "[REDACTED:body]",
			})
			return rpcError(req.ID, err.Error(), "mcp_error")
		}
		s.auditMCPCall(serverID, tool, inputHash, hashJSONValue(result), elapsedMS(start))
		return rpcResponse{
			ID:     req.ID,
			OK:     true,
			Result: result,
		}
	}
	if !state.mcpAllowed[serverID][tool] {
		s.auditMCPDenied(serverID, tool, "undeclared")
		state.setFailureEvidence(&UpstreamEvidence{
			Availability: AvailabilityForbidden,
			TimingMS:     elapsedMS(start),
			BodyHash:     inputHash,
			BodyRedacted: "[REDACTED:body]",
		})
		return rpcError(req.ID, "mcp server/tool is not declared", "mcp_denied")
	}
	// No router is installed. In production this is a governed path that must
	// fail closed with a typed not-enabled error — a synthetic {ok: true} would
	// be a false-success defect (same class as B20 C8). B33 wires the real
	// production router in v0.4; until then the managed MCP service is not
	// enabled. Explicit test mode (AGENTPAAS_TEST_FAKE_MCP=1) keeps the
	// synthetic result for fixtures that opt in, mirroring
	// AGENTPAAS_TEST_FAKE_LLM. External stdio/HTTP MCP compatibility (router
	// installed) is unaffected — the router != nil branch above handles it.
	if os.Getenv("AGENTPAAS_TEST_FAKE_MCP") == "1" {
		result := map[string]any{
			"server_id": serverID,
			"tool":      tool,
			"result":    map[string]any{"ok": true},
		}
		s.auditMCPCall(serverID, tool, inputHash, hashJSONValue(result), elapsedMS(start))
		return rpcResponse{
			ID:     req.ID,
			OK:     true,
			Result: result,
		}
	}
	s.auditMCPDenied(serverID, tool, mcpServiceNotEnabledReason)
	state.setFailureEvidence(&UpstreamEvidence{
		Availability: AvailabilityForbidden,
		TimingMS:     elapsedMS(start),
		BodyHash:     inputHash,
		BodyRedacted: "[REDACTED:body]",
	})
	return rpcError(req.ID, mcpServiceNotEnabledReason, mcpServiceNotEnabledCode)
}

func (s *harnessRPCServer) auditEgressDecision(actor, destination, method, credentialID, statusCode, decision, reason string) {
	if s.audit == nil {
		return
	}
	payload := map[string]interface{}{
		"destination": destination,
		"method":      method,
		"decision":    decision,
	}
	if credentialID != "" {
		payload["credential_id"] = credentialID
	}
	if statusCode != "" {
		payload["status_code"] = statusCode
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if err := s.audit.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      "egress_" + decision,
		DeploymentMode: "local",
		Actor:          actor,
		Payload:        payload,
	}); err != nil {
		log.Printf("harness: audit append failed (egress_%s): %v", decision, err)
	}
}

func (s *harnessRPCServer) auditMCPDenied(serverID, tool, reason string) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      "mcp_denied",
		DeploymentMode: "local",
		Actor:          "harness",
		Payload: map[string]interface{}{
			"server_id": serverID,
			"tool":      tool,
			"reason":    reason,
		},
	}); err != nil {
		log.Printf("harness: audit append failed (mcp_denied): %v", err)
	}
}

func (s *harnessRPCServer) auditMCPCall(serverID, tool, inputHash, outputHash string, timingMS int64) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      "mcp_call",
		DeploymentMode: "local",
		Actor:          "harness",
		Payload: map[string]interface{}{
			"server_id":   serverID,
			"tool":        tool,
			"input_hash":  inputHash,
			"output_hash": outputHash,
			"timing_ms":   timingMS,
		},
	}); err != nil {
		log.Printf("harness: audit append failed (mcp_call): %v", err)
	}
}

func (state *rpcInvokeState) setFailureEvidence(evidence *UpstreamEvidence) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.failureEvidence = evidence
}

func elapsedMS(start time.Time) int64 {
	return max(int64(time.Since(start)/time.Millisecond), int64(0))
}

func availabilityFromStatus(status int) string {
	switch status {
	case http.StatusTooManyRequests:
		return AvailabilityRateLimited
	case http.StatusForbidden, http.StatusUnauthorized:
		return AvailabilityForbidden
	default:
		return AvailabilityAvailable
	}
}

func hashJSONValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return sha256HexString(fmt.Sprintf("%v", value))
	}
	return sha256HexString(string(encoded))
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func rpcError(id, message, code string) rpcResponse {
	return rpcResponse{ID: id, OK: false, Error: message, Code: code}
}

func mcpAllowlistFromPayload(payload map[string]any) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, item := range listParam(payload, "mcp_servers") {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		serverID := firstString(obj, "server_id", "id", "name")
		if serverID == "" {
			continue
		}
		tools := make(map[string]bool)
		for _, rawTool := range listParam(obj, "tools") {
			switch v := rawTool.(type) {
			case string:
				tools[v] = true
			case map[string]any:
				name := firstString(v, "name", "tool")
				if name != "" {
					tools[name] = true
				}
			}
		}
		out[serverID] = tools
	}
	return out
}

func redactedHeaders(headers http.Header) map[string]string {
	return hashedHeaders(headers)
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, _ := params[key].(string) // optional string param
	return value
}

func stringMapParam(params map[string]any, key string) map[string]string {
	out := make(map[string]string)
	raw, ok := params[key].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		if text, ok := v.(string); ok {
			out[k] = text
		}
	}
	return out
}

func listParam(params map[string]any, key string) []any {
	if params == nil {
		return nil
	}
	items, _ := params[key].([]any) // optional array param
	return items
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// rewriteURLForGateway rewrites an outbound URL so traffic goes to the
// agentgateway listener instead of the provider directly. The caller must set
// req.Host to the original hostname so the gateway can match routes.
// When gatewayURL is empty (test mode / no gateway), the original URL is returned.
func rewriteURLForGateway(rawURL, gatewayURL string) (string, error) {
	if gatewayURL == "" {
		return rawURL, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("rewrite urlfor gateway: %w", err)
	}
	gw, err := url.Parse(gatewayURL)
	if err != nil {
		return "", fmt.Errorf("rewrite urlfor gateway: %w", err)
	}
	// https://openrouter.ai/v1/chat/completions
	//   → http://gateway:7799/v1/chat/completions
	u.Scheme = "http"
	u.Host = gw.Host
	return u.String(), nil
}
