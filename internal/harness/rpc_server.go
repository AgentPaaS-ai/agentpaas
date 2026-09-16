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
	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
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
	mcpManager  *mcpmanager.Manager
	credentials map[string]rpcCredential // Pre-loaded credential values (from sidecar file)
	// oauthBindings are host→credential metadata for oauth_delegated inject
	// (AGENTPAAS_OAUTH_BINDINGS_JSON). Consent URLs only — never tokens.
	oauthBindings []oauthBinding

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

	// mcpSemaphore enforces per-caller concurrent MCP call bounds (B33-T06).
	// Initialized lazily; nil (legacy) means unlimited.
	mcpSemaphore *mcpmanager.CallSemaphore

	// mcpMaxConcurrency configures the caller-side MCP concurrency limit.
	// 0 (default) uses the package-level DefaultMaxConcurrentMCPCalls.
	mcpMaxConcurrency int

	// liveCallHop is the transport for the parent-DO live-call hop after
	// an ADMITTED delegate_task. Nil skips the hop (unit tests).
	liveCallHop http.RoundTripper

	// liveCallHopSleep is the wait between waiting_seat hop retries.
	// Nil uses time.Sleep.
	liveCallHopSleep func(time.Duration)

	// liveCallParentRemainingMs overrides parentRemainingMs when non-nil.
	// Nil uses invoke budget (or 120000 when invoke/budget is unset).
	liveCallParentRemainingMs func() int64

	// liveCallOutputs holds A's invoke JSON keyed by task id. Never contains
	// endpoints or tokens.
	liveCallOutputs map[string]any

	// llmChatCompletion, when non-nil, replaces callLLMChatCompletion so tests
	// can stub a Completions.New that ignores ctx. Production leaves this nil.
	llmChatCompletion func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error)

	// loopback is the OpenAI-compatible 127.0.0.1 listener that rewrites
	// POST /v1/chat/completions into handleLLM. Nil when not started.
	loopback *openaiLoopback
}

type rpcInvokeState struct {
	budget      *BudgetEnforcer
	payload     map[string]any
	terminate   func()
	credentials map[string]rpcCredential
	mcpAllowed  map[string]map[string]bool

	// timeEnvelope is the authoritative active-time envelope (B30-T03 Part B,
	// ceiling 5). When present, the LLM HTTP client timeout is derived from
	// env.EffectiveOperationDeadlineMs(nowMs, env.ModelCallTimeoutMs) and
	// capped at maxModelClientTimeout (5 min). When nil (legacy v0.2.3
	// compat path), the legacy 120s constant applies.
	timeEnvelope *routedrun.TimeEnvelope

	// Progress journal (B27)
	progressJournal  *progressJournalWriter
	progressIdentity progressIdentity
	leaseExpired     atomic.Bool
	resumeCheckpoint map[string]any // B35-provided resume data (trusted, not from trigger)
	resumeReason     string         // trusted enum: failure_continuation|operator_pause_resume

	// Pipeline stage context (B34-T03) — nil when not a pipeline stage.
	pipelineCtx *PipelineStageContext

	mu              sync.Mutex
	failureEvidence *UpstreamEvidence
}

type rpcCredential struct {
	Header string
	Value  string
}

// oauthBinding is metadata for oauth_delegated host-match inject in handleHTTP.
// Loaded from AGENTPAAS_OAUTH_BINDINGS_JSON (no token values).
type oauthBinding struct {
	CredentialID    string `json:"credential_id"`
	HostPattern     string `json:"host_pattern"`
	ConsentURL      string `json:"consent_url"`
	EndUserIdentity string `json:"end_user_identity"`
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
		listener:        listener,
		addr:            socket,
		socket:          socket,
		done:            make(chan struct{}),
		audit:           appender,
		liveCallHop:     http.DefaultTransport,
		liveCallOutputs: make(map[string]any),
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
	var loopbackErr error
	if s.loopback != nil {
		loopbackErr = s.loopback.Close()
	}
	return errors.Join(err, loopbackErr, os.RemoveAll(filepath.Dir(s.socket)))
}

func (s *harnessRPCServer) startOpenAILoopback() error {
	lb, err := startOpenAILoopback(s)
	if err != nil {
		return err
	}
	s.loopback = lb
	return nil
}

func (s *harnessRPCServer) openaiLoopbackBaseURL() string {
	if s == nil || s.loopback == nil {
		return ""
	}
	return s.loopback.baseURL()
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
// (legacy compat). Weather/other agents keep this 120s default. On the
// durable path the timeout is derived from
// env.EffectiveOperationDeadlineMs(nowMs, env.ModelCallTimeoutMs).
const legacyModelClientTimeout = 120 * time.Second

// maxModelClientTimeout is the hard cap for a single model HTTP call
// (pitch-filter / Gemini). Envelope remaining time may be a 30 min lease;
// the socket must still die by 5 minutes.
const maxModelClientTimeout = 5 * time.Minute

// rpcReadTimeoutSlack is added to the model-call deadline so the Python
// RPC read wait is strictly longer than the harness LLM timeout.
const rpcReadTimeoutSlack = 10 * time.Second

// rpcReadTimeoutFor returns the Python RPC line-read deadline for a model
// call of the given duration (model deadline + slack). A 130s-only RPC
// ceiling cannot cover a 300s model call.
func rpcReadTimeoutFor(modelTimeout time.Duration) time.Duration {
	if modelTimeout <= 0 {
		modelTimeout = maxModelClientTimeout
	}
	return modelTimeout + rpcReadTimeoutSlack
}

// modelClientTimeout returns the HTTP client timeout for an LLM call. When a
// TimeEnvelope is attached to the invoke state, the timeout is derived from
// env.EffectiveOperationDeadlineMs(nowMs, env.ModelCallTimeoutMs) — the min of
// the model-call timeout, the attempt-lease remaining, and the active time
// remaining (B30-T03 Part B, ceiling 5). When no envelope is present (legacy
// v0.2.3 compat), it falls back to legacyModelClientTimeout.
//
// The result is hard-capped at maxModelClientTimeout (5 min) even when the
// run lease is much longer (e.g. 30 min). A hung socket fail-closes with
// llm_failed; a finished upstream generation must not leave /invoke open.
func (s *harnessRPCServer) modelClientTimeout(state *rpcInvokeState) time.Duration {
	d := legacyModelClientTimeout
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
		d = time.Duration(deadlineMs) * time.Millisecond
	}
	if d > maxModelClientTimeout {
		return maxModelClientTimeout
	}
	return d
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
		resp := s.handleRequest(req)
		if req.Method == "llm" {
			log.Printf("harness: rpc writing llm response id=%s ok=%t code=%s", resp.ID, resp.OK, resp.Code)
		}
		if encErr := encoder.Encode(resp); encErr != nil {
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
	case "mcp_list_tools":
		return s.handleMCPList(req, state, "tools")
	case "mcp_list_prompts":
		return s.handleMCPList(req, state, "prompts")
	case "progress":
		return s.handleProgress(req, state)
	case "workflow_input":
		return s.handleWorkflowInput(req, state)
	case "commit_handoff":
		return s.handleCommitHandoff(req, state)
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
func (s *harnessRPCServer) SetRouter(router *mcpmanager.Router, manager *mcpmanager.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.router = router
	s.mcpManager = manager
}

// SetMCPMaxConcurrency sets the caller-side MCP concurrent call limit.
// 0 means use DefaultMaxConcurrentMCPCalls. Called before first MCP call.
func (s *harnessRPCServer) SetMCPMaxConcurrency(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mcpMaxConcurrency = max
}

// getMCPSemaphore returns the per-caller MCP concurrency semaphore, creating
// one if needed.
func (s *harnessRPCServer) getMCPSemaphore() *mcpmanager.CallSemaphore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mcpSemaphore == nil {
		limit := s.mcpMaxConcurrency
		if limit <= 0 {
			limit = mcpmanager.DefaultMaxConcurrentMCPCalls
		}
		s.mcpSemaphore = mcpmanager.NewCallSemaphore(limit)
	}
	return s.mcpSemaphore
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

// LoadCredentialsFromJSON parses a JSON array of {id, header, value} objects
// and stores them as credentials in memory. This is the cloud CF equivalent
// of LoadCredentials when the platform provides credentials via
// AGENTPAAS_CREDENTIALS_JSON instead of a mounted file.
// Credential values are never logged or included in error messages.
func (s *harnessRPCServer) LoadCredentialsFromJSON(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}

	type credEntry struct {
		ID     string `json:"id"`
		Header string `json:"header"`
		Value  string `json:"value"`
	}
	var entries []credEntry
	if err := json.Unmarshal([]byte(jsonStr), &entries); err != nil {
		return fmt.Errorf("harness rpcserver load credentials from json: %w", err)
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

// LoadOAuthBindingsFromJSON parses AGENTPAAS_OAUTH_BINDINGS_JSON:
// [{credential_id, host_pattern, consent_url, end_user_identity}].
// Metadata only — never contains tokens.
func (s *harnessRPCServer) LoadOAuthBindingsFromJSON(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	var entries []oauthBinding
	if err := json.Unmarshal([]byte(jsonStr), &entries); err != nil {
		return fmt.Errorf("harness rpcserver load oauth bindings from json: %w", err)
	}
	out := make([]oauthBinding, 0, len(entries))
	for _, e := range entries {
		if e.CredentialID == "" || e.HostPattern == "" {
			continue
		}
		out = append(out, e)
	}
	s.mu.Lock()
	s.oauthBindings = out
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

// LoadDelegationSnapshot reads the delegation snapshot sidecar file
// (BUG-040) and constructs the DelegationTrustState. The daemon writes
// the pre-built CommunicationSnapshot and per-binding capability tokens
// to a JSON file and bind-mounts it read-only. The harness reads it at
// startup and injects it into the RPC server.
func (s *harnessRPCServer) LoadDelegationSnapshot(path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("harness rpcserver load delegation snapshot: %w", err)
	}
	if err := s.applyDelegationSnapshot(data); err != nil {
		return fmt.Errorf("unmarshal delegation snapshot file: %w", err)
	}
	return nil
}

// LoadDelegationSnapshotJSON parses AGENTPAAS_DELEGATION_SNAPSHOT_JSON into
// the same DelegationTrustState as LoadDelegationSnapshot. Cloud CF sets
// the JSON env and does not set the snapshot path.
func (s *harnessRPCServer) LoadDelegationSnapshotJSON(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	if err := s.applyDelegationSnapshot([]byte(jsonStr)); err != nil {
		return fmt.Errorf("unmarshal delegation snapshot json: %w", err)
	}
	return nil
}

func (s *harnessRPCServer) applyDelegationSnapshot(data []byte) error {
	var sidecar struct {
		Snapshot            delegation.CommunicationSnapshot `json:"snapshot"`
		BindingCapabilities map[string]string                `json:"binding_capabilities"`
		NetworkAlias        string                           `json:"network_alias"`
		WorkflowID          string                           `json:"workflow_id"`
		WorkflowKind        string                           `json:"workflow_kind"`
		Standalone          bool                             `json:"standalone"`
		LiveCallForbidden   bool                             `json:"live_call_forbidden"`
		CalleeIngressAllow  []delegation.CalleeIngressRule   `json:"callee_ingress_allow"`
	}
	if err := json.Unmarshal(data, &sidecar); err != nil {
		return err
	}

	liveCallForbidden := sidecar.LiveCallForbidden || sidecar.WorkflowKind == "pipeline"
	standaloneLive := (sidecar.WorkflowKind == "standalone_live" || sidecar.Standalone) &&
		!liveCallForbidden && len(sidecar.Snapshot.Bindings) > 0

	// Cloud standalone_live sidecars omit snapshot_digest and
	// callee_ingress_allow. Synthesize both so AuthorizeDelegation can
	// ADMIT. Do not mint WorkflowID. Do not do this for phone_call /
	// pipeline / child — those must keep fail-closed authz.
	if standaloneLive {
		if sidecar.Snapshot.SnapshotDigest == "" {
			dg, err := delegation.ComputeSnapshotDigest(&sidecar.Snapshot)
			if err != nil {
				return fmt.Errorf("standalone_live snapshot digest: %w", err)
			}
			sidecar.Snapshot.SnapshotDigest = dg
		}
		if len(sidecar.CalleeIngressAllow) == 0 {
			bindingIDs := make([]string, len(sidecar.Snapshot.Bindings))
			for i, b := range sidecar.Snapshot.Bindings {
				bindingIDs[i] = b.BindingID
			}
			sidecar.CalleeIngressAllow = []delegation.CalleeIngressRule{{
				CallerPackageName:   sidecar.Snapshot.CallerPackageName,
				CallerPackageDigest: sidecar.Snapshot.CallerPackageDigest,
				AllowedBindings:     bindingIDs,
				MaxDataClass:        string(delegation.ClassificationInternal),
			}}
		}
	}

	dts := &DelegationTrustState{
		Snapshot:            sidecar.Snapshot,
		BindingCapabilities: sidecar.BindingCapabilities,
		NetworkAlias:        sidecar.NetworkAlias,
		Store:               delegation.NewMemoryStore(),
		CalleeIngressAllow:  sidecar.CalleeIngressAllow,
		LiveCallForbidden:   liveCallForbidden,
		StandaloneLive:      standaloneLive,
	}

	s.setDelegationTrustState(dts)
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
			promptAfterPII, presp := s.enforceRequestPII(req, state, prompt)
			if presp != nil {
				return *presp
			}
			prompt = promptAfterPII
			text := "agentpaas fake llm response"
			text, gerr = applyGuardrailsToText(cg, text, "response", state.credentials)
			if gerr != nil {
				return rpcError(req.ID, gerr.Error(), StatusGuardrailBlocked)
			}
			text, presp = s.enforceResponsePII(req, state, text)
			if presp != nil {
				return *presp
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
	// Inspect the combined prompt so system-prompt PII cannot bypass request PII.
	if sp := injectSystemPromptFromPayload(state.payload); sp != "" {
		prompt = combineSystemPrompt(sp, prompt)
	}
	promptAfterPII, presp := s.enforceRequestPII(req, state, prompt)
	if presp != nil {
		return *presp
	}
	prompt = promptAfterPII

	// One deadline owner: context.WithTimeout from modelClientTimeout /
	// TimeEnvelope (up to 5 min). callLLMChatCompletion also sets
	// http.Client.Timeout and option.WithRequestTimeout to that deadline so
	// the socket dies even when Completions.New ignores ctx (Gemini SSE).
	timeout := s.modelClientTimeout(state)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Rewrite URL for gateway-native HTTP routing (Bug 021). Preserve the
	// original Host so the gateway can match routes by hostname.
	originalEndpoint := adapter.Endpoint()
	requestEndpoint := originalEndpoint
	originalHost := ""
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
		originalHost = origU.Host
		requestEndpoint = rewritten
	}
	baseURL, err := openaiChatCompletionsBaseURL(requestEndpoint)
	if err != nil {
		s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, "", "denied", "openai-go base URL: "+err.Error())
		return rpcError(req.ID, err.Error(), "llm_failed")
	}

	fn := callLLMChatCompletion
	if s.llmChatCompletion != nil {
		fn = s.llmChatCompletion
	}
	type llmCall struct {
		result *llm.LLMResult
		err    error
	}
	ch := make(chan llmCall, 1)
	go func() {
		out, callErr := fn(ctx, baseURL, originalHost, cred.Value, model, prompt, maxTokensPerRequest, provider)
		ch <- llmCall{out, callErr}
	}()
	var result *llm.LLMResult
	select {
	case <-ctx.Done():
		log.Printf("harness: llm ctx.Done wins vs Completions.New err=%v", ctx.Err())
		err = ctx.Err()
	case o := <-ch:
		log.Printf("harness: llm Completions.New returns vs ctx.Done err=%t", o.err != nil)
		result, err = o.result, o.err
	}
	if err != nil {
		failReason := err.Error()
		if piiFromPayload(state.payload) != nil {
			failReason = "llm_failed"
		}
		log.Printf("harness: llm chat completion failed: %s", failReason)
		status := llmHTTPStatusFromError(err)
		s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, status, "denied", failReason)
		return rpcError(req.ID, failReason, "llm_failed")
	}

	const llmHTTPOK = "200"

	// Response-side guardrails (same ruleset as request for regex/webhook).
	respText, gerr := applyGuardrailsToText(cg, result.Text, "response", state.credentials)
	if gerr != nil {
		s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, llmHTTPOK, "denied", gerr.Error())
		return rpcError(req.ID, gerr.Error(), StatusGuardrailBlocked)
	}
	respText, presp = s.enforceResponsePII(req, state, respText)
	if presp != nil {
		return *presp
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
			s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, llmHTTPOK, "denied", err.Error())
			return rpcError(req.ID, err.Error(), StatusBudgetExceeded)
		}
		s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, llmHTTPOK, "denied", err.Error())
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
	s.auditEgressDecision("harness", originalEndpoint, "POST", credentialID, llmHTTPOK, "allowed", "")

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

func (s *harnessRPCServer) enforceRequestPII(req rpcRequest, state *rpcInvokeState, prompt string) (string, *rpcResponse) {
	cfg := piiFromPayload(state.payload)
	out, err := applyPIIToText(cfg, prompt)
	if err == nil {
		return out, nil
	}
	reason := err.Error()
	if aerr := s.auditPIIDecision(reason); aerr != nil {
		resp := rpcError(req.ID, "pii_enforcement_unavailable", StatusPIIBlocked)
		return "", &resp
	}
	resp := rpcError(req.ID, reason, StatusPIIBlocked)
	return "", &resp
}

func (s *harnessRPCServer) enforceResponsePII(req rpcRequest, state *rpcInvokeState, text string) (string, *rpcResponse) {
	cfg := piiFromPayload(state.payload)
	out, err := applyPIIToText(cfg, text)
	if err == nil {
		if cfg != nil && s.audit != nil {
			if aerr := s.auditPIIDecision("pii_masked"); aerr != nil {
				resp := rpcError(req.ID, "pii_enforcement_unavailable", StatusPIIBlocked)
				return "", &resp
			}
		}
		return out, nil
	}
	reason := err.Error()
	if aerr := s.auditPIIDecision(reason); aerr != nil {
		resp := rpcError(req.ID, "pii_enforcement_unavailable", StatusPIIBlocked)
		return "", &resp
	}
	resp := rpcError(req.ID, reason, StatusPIIBlocked)
	return "", &resp
}

func (s *harnessRPCServer) auditPIIDecision(reason string) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      "egress_denied",
		DeploymentMode: "local",
		Actor:          "harness",
		Payload: map[string]interface{}{
			"decision": "denied",
			"reason":   reason,
		},
	})
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
	if _, perr := applyPIIToText(piiFromPayload(state.payload), rawURL+"\n"+body); perr != nil {
		return rpcError(req.ID, perr.Error(), StatusPIIBlocked)
	}
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
		httpReq.Header.Set(header, authorizationHeaderValue(header, cred.Value))
		credentialValue = cred.Value
	}

	// M13.9: oauth_delegated host-match for agent.http() (direct harness egress).
	// Pre-resolved tokens live in credentials; pending consent in oauthBindings.
	if errResp := s.applyOAuthBinding(req.ID, rawURL, method, httpReq, state, start, bodyHash, bodyMarker, &credentialValue); errResp != nil {
		return *errResp
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
	inspectText := fmt.Sprint(input)
	if rawInput, marshalErr := json.Marshal(input); marshalErr == nil {
		inspectText = string(rawInput)
	}
	if _, perr := applyPIIToText(piiFromPayload(state.payload), inspectText); perr != nil {
		return rpcError(req.ID, perr.Error(), StatusPIIBlocked)
	}

	// B33-T06: enforce per-caller MCP concurrency bound.
	sem := s.getMCPSemaphore()
	releaseSem, err := sem.Acquire()
	if err != nil {
		s.auditMCPDenied(serverID, tool, err.Error())
		return rpcError(req.ID, err.Error(), mcpmanager.ErrCodeOverloaded)
	}
	defer releaseSem()

	// B33-T06: enforce input request size bound.
	if rawInput, err := json.Marshal(input); err == nil {
		if checkErr := mcpmanager.CheckRequestSize(rawInput); checkErr != nil {
			s.auditMCPDenied(serverID, tool, checkErr.Error())
			return rpcError(req.ID, checkErr.Error(), mcpmanager.ErrCodeBodyTooLarge)
		}
	}

	// B33-T05: derive a context deadline from the B30 operation envelope.
	// When a TimeEnvelope is present on the invoke state, use the effective
	// operation deadline as the MCP call timeout; otherwise use a
	// configurable bound (default 30s). This replaces the legacy fixed 5s
	// stdioResponseTimeout with the authoritative B30 deadline.
	ctx, cancel := s.mcpCallContext(state)
	defer cancel()

	s.mu.RLock()
	router := s.router
	s.mu.RUnlock()
	if router != nil {
		result, err := router.CallTool(ctx, serverID, tool, input, "harness", "test-run")
		if err != nil {
			code := mcpErrorCode(err)
			s.auditMCPDenied(serverID, tool, err.Error())
			state.setFailureEvidence(&UpstreamEvidence{
				Availability: AvailabilityForbidden,
				TimingMS:     elapsedMS(start),
				BodyHash:     inputHash,
				BodyRedacted: "[REDACTED:body]",
			})
			return rpcError(req.ID, err.Error(), code)
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
	// No router is installed. The production path always installs a Router
	// (B33-T05); this branch is retained for backward-compat with tests that
	// don't install one. AGENTPAAS_TEST_FAKE_MCP=1 provides a synthetic
	// response ONLY when no managed bindings are configured — managed
	// service calls must never succeed synthetically.
	if os.Getenv("AGENTPAAS_TEST_FAKE_MCP") == "1" {
		// Managed services: reject synthetic success even in test mode.
		// The synthetic path is for external stdio/HTTP MCP compatibility
		// tests only. Managed bindings must go through the real router.
		if isManagedBinding(state, serverID) {
			s.auditMCPDenied(serverID, tool, "managed binding: synthetic success forbidden")
			state.setFailureEvidence(&UpstreamEvidence{
				Availability: AvailabilityForbidden,
				TimingMS:     elapsedMS(start),
				BodyHash:     inputHash,
				BodyRedacted: "[REDACTED:body]",
			})
			return rpcError(req.ID, "managed MCP binding requires real router; synthetic success is forbidden", mcpServiceNotEnabledCode)
		}
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

func (s *harnessRPCServer) handleMCPList(req rpcRequest, state *rpcInvokeState, kind string) rpcResponse {
	serverID := stringParam(req.Params, "server_id")
	listName := "list_" + kind

	s.mu.RLock()
	router := s.router
	s.mu.RUnlock()
	if router == nil {
		if state == nil || state.mcpAllowed[serverID] == nil {
			s.auditMCPDenied(serverID, listName, "undeclared")
			return rpcError(req.ID, "mcp server/tool is not declared", "mcp_denied")
		}
		s.auditMCPDenied(serverID, listName, mcpServiceNotEnabledReason)
		return rpcError(req.ID, mcpServiceNotEnabledReason, mcpServiceNotEnabledCode)
	}

	ctx, cancel := s.mcpCallContext(state)
	defer cancel()
	var (
		result any
		err    error
	)
	switch kind {
	case "prompts":
		result, err = router.ListPrompts(ctx, serverID)
	default:
		result, err = router.ListTools(ctx, serverID)
	}
	if err != nil {
		s.auditMCPDenied(serverID, listName, err.Error())
		return rpcError(req.ID, err.Error(), mcpErrorCode(err))
	}
	return rpcResponse{
		ID:     req.ID,
		OK:     true,
		Result: result,
	}
}

// mcpCallContext creates a context with a deadline derived from the invoke
// state's TimeEnvelope (B30 operation deadline). When no envelope is present,
// falls back to a 120s bound.
//
// B30-T03 Part B (legacy/compat): mcpDefaultTimeoutSeconds is a documented
// ceiling on the non-envelope fallback path. This path is exercised only
// when the harness is not on the durable InvokeDeployment path (v0.2.3
// legacy compat). On the durable path the effective operation deadline from
// the TimeEnvelope takes precedence. Registered in b30T01Ceilings.
func (s *harnessRPCServer) mcpCallContext(state *rpcInvokeState) (context.Context, context.CancelFunc) {
	const mcpDefaultTimeoutSeconds = 120
	if state != nil && state.timeEnvelope != nil {
		nowMs := routedrun.NowMonotonicMs(nil)
		if s.nowMonotonicMs != nil {
			nowMs = s.nowMonotonicMs()
		}
		deadlineMs := state.timeEnvelope.EffectiveOperationDeadlineMs(nowMs, state.timeEnvelope.StallTimeoutMs)
		if deadlineMs > 0 {
			// TimeEnvelope takes precedence; do not cap long envelopes at 30s.
			return context.WithTimeout(context.Background(), time.Duration(deadlineMs)*time.Millisecond)
		}
	}
	return context.WithTimeout(context.Background(), mcpDefaultTimeoutSeconds*time.Second) // legacy/compat fallback
}

// isManagedBinding checks whether the given serverID corresponds to a managed
// AgentPaaS service binding (as opposed to an external stdio/HTTP MCP server).
// Managed bindings are identified by having their transport set to
// "agentpaas-service" in the invoke payload or registered servers.
func isManagedBinding(state *rpcInvokeState, serverID string) bool {
	if state == nil || state.payload == nil {
		return false
	}
	// Check if any mcp_server entry has transport "agentpaas-service".
	for _, item := range listParam(state.payload, "mcp_servers") {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := firstString(obj, "server_id", "id", "name")
		if id != serverID {
			continue
		}
		transport := firstString(obj, "transport")
		if transport == "agentpaas-service" {
			return true
		}
	}
	return false
}

// mcpErrorCode maps known MCP errors to stable typed codes for the RPC response.
// Unknown errors fall back to "mcp_error".
func mcpErrorCode(err error) string {
	var typed *mcpmanager.TypedError
	if errors.As(err, &typed) {
		return typed.Code
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "timed out"):
		return "mcp_timeout"
	case ctxErr(err):
		return "mcp_timeout"
	case strings.Contains(msg, "not allowed"):
		return "mcp_denied"
	case strings.Contains(msg, "not declared"):
		return "mcp_denied"
	case strings.Contains(msg, "not found"):
		return "mcp_service_not_found"
	case strings.Contains(msg, "not ready"):
		return "mcp_service_not_ready"
	case strings.Contains(msg, "crashed"):
		return "mcp_service_crashed"
	case strings.Contains(msg, "overloaded") || strings.Contains(msg, "concurrency limit"):
		return mcpmanager.ErrCodeOverloaded
	case strings.Contains(msg, "exceeds") && strings.Contains(msg, "byte"):
		return mcpmanager.ErrCodeBodyTooLarge
	case strings.Contains(msg, "depth") && strings.Contains(msg, "exceeds"):
		return mcpmanager.ErrCodeDepthTooDeep
	default:
		return "mcp_error"
	}
}

func ctxErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
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

// applyOAuthBinding enforces oauth_delegated host-match before client.Do.
// If a pre-resolved credential exists, inject Authorization. Otherwise return
// authorization_required with consent_url (metadata only, no tokens).
func (s *harnessRPCServer) applyOAuthBinding(
	reqID, rawURL, method string,
	httpReq *http.Request,
	state *rpcInvokeState,
	start time.Time,
	bodyHash, bodyMarker string,
	credentialValue *string,
) *rpcResponse {
	host := requestHost(rawURL)
	if host == "" {
		return nil
	}
	s.mu.RLock()
	bindings := s.oauthBindings
	s.mu.RUnlock()
	var match *oauthBinding
	for i := range bindings {
		if strings.EqualFold(bindings[i].HostPattern, host) {
			match = &bindings[i]
			break
		}
	}
	if match == nil {
		return nil
	}
	if cred, ok := state.credentials[match.CredentialID]; ok && cred.Value != "" {
		header := defaultString(cred.Header, "Authorization")
		// Inject only when not already set (withCredential path may have set it).
		if httpReq.Header.Get(header) == "" {
			httpReq.Header.Set(header, authorizationHeaderValue(header, cred.Value))
			if credentialValue != nil {
				*credentialValue = cred.Value
			}
		}
		return nil
	}
	// Consent still required — return structured RPC error for the agent SDK.
	grantID := grantIDFromConsentURL(match.ConsentURL)
	payload := map[string]string{
		"error":         "authorization_required",
		"consent_url":   match.ConsentURL,
		"grant_id":      grantID,
		"credential_id": match.CredentialID,
	}
	msg, err := json.Marshal(payload)
	if err != nil {
		msg = []byte(`{"error":"authorization_required"}`)
	}
	s.auditEgressDecision("harness", rawURL, method, match.CredentialID, "", "denied", "oauth authorization required")
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
	resp := rpcError(reqID, string(msg), "authorization_required")
	return &resp
}

func requestHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

func grantIDFromConsentURL(consentURL string) string {
	if consentURL == "" {
		return ""
	}
	u, err := url.Parse(consentURL)
	if err != nil {
		return ""
	}
	// .../oauth/start/<grant_id>
	const marker = "/oauth/start/"
	path := u.Path
	idx := strings.Index(path, marker)
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(marker):]
	if rest == "" {
		return ""
	}
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		rest = rest[:slash]
	}
	return rest
}
