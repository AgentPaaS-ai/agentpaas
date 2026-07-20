package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

const (
	MaxPayloadBytes = 10 * 1024 * 1024

	defaultAddr          = "127.0.0.1:8080"
	defaultImportTimeout = 60 * time.Second
)

// Config controls the harness HTTP server and Python worker.
type Config struct {
	Addr            string
	AgentPath       string
	Python          string
	ImportTimeout   time.Duration
	InvokeTimeout   time.Duration
	TerminateGrace  time.Duration
	StdoutPath      string
	StderrPath      string
	Audit           AuditAppender
	CredentialsPath string // Path to credentials.json sidecar file (empty = none)

	// Progress journal integration (B27). The daemon writes the journal key
	// to a sidecar file and bind-mounts it into the harness container. The
	// harness reads it, constructs a journal writer, and deletes the file
	// before starting Python (mirrors credentials sidecar pattern).
	JournalKeyPath string // Path to journal key sidecar file (empty = no progress)
	JournalPath    string // Path to journal file inside container (empty = no progress)
	AttemptID      string // Attempt ID for journal records
	LeaseID        string // Lease ID for journal records
	RunID          string // Run ID for journal records (must match tailer's run ID)

	// B30-T04: policy-derived resource ceilings. On the durable path
	// (InvokeDeployment), the daemon populates these from the deployment
	// policy and the harness propagates them to the Python worker via
	// AGENTPAAS_CPU_QUOTA_SECONDS / AGENTPAAS_MAX_PIDS env vars. The
	// Python runner applies them as RLIMIT_CPU / RLIMIT_NPROC.
	//
	// CPUQuotaSeconds is the per-attempt CPU-time budget in seconds.
	// 0 means unlimited CPU (bounded by the container CFS quota set by
	// the runtime driver); RLIMIT_CPU is NOT set on the durable path.
	// MaxPIDs is the per-attempt process-count limit (RLIMIT_NPROC).
	// 0 means an explicit policy decision to forbid ALL subprocesses;
	// a positive value allows that many processes for approved local
	// tools (git, grep, awk). When unset on the legacy v0.2.3 path,
	// the runner falls back to RLIMIT_NPROC=0 (legacy compat).
	//
	// On the legacy v0.2.3 path (cmd/harness), these fields are zero —
	// the runner then applies the legacy RLIMIT_CPU=30 / RLIMIT_NPROC=0
	// constants with "legacy compat" comments.
	CPUQuotaSeconds int64
	MaxPIDs         int

	// DurablePath marks the harness as running on the B30-T02 durable
	// InvokeDeployment path (vs the legacy v0.2.3 trigger path). When
	// true, the harness propagates the policy resource env vars to the
	// Python runner so it applies policy-derived rlimits; when false,
	// the runner falls back to the legacy fixed constants.
	DurablePath bool
}

// ErrorResponse is the structured failure envelope returned by lifecycle APIs.
type ErrorResponse struct {
	Status         string          `json:"status"`
	Reason         string          `json:"reason"`
	Detail         string          `json:"detail"`
	FailureContext *FailureContext `json:"failure_context,omitempty"`
}

// InvokeResponse is returned by successful /invoke calls.
type InvokeResponse struct {
	Status string         `json:"status"`
	Result map[string]any `json:"result,omitempty"`
	Stdout string         `json:"stdout"`
	Stderr string         `json:"stderr"`
}

// Server exposes the harness HTTP contract.
type Server struct {
	cfg Config

	mux    *http.ServeMux
	worker *pythonWorker
	reaper *childReaper

	mu          sync.RWMutex
	ready       bool
	importError *ErrorResponse
	closed      bool

	invokeMu sync.Mutex

	// nowMonotonicMs supplies the monotonic millisecond timestamp used to
	// evaluate the TimeEnvelope in handleInvoke (B30-T03 Part B, ceiling 3).
	// When nil, routedrun.NowMonotonicMs(nil) (time.Now().UnixMilli()) is used.
	nowMonotonicMs func() int64
}

// NewServer creates a harness server and performs the Python import phase.
func NewServer(cfg Config) *Server {
	cfg = normalizeConfig(cfg)
	s := &Server{
		cfg:    cfg,
		mux:    http.NewServeMux(),
		reaper: startChildReaper(),
	}
	s.routes()
	s.startWorker()
	return s
}

// ListenAndServe serves the harness HTTP API on the configured localhost address.
func (s *Server) ListenAndServe(ctx context.Context) error {
	server := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Server.ServeHTTP serves http.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Close stops the Python worker.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	worker := s.worker
	s.mu.Unlock()

	if worker == nil {
		if s.reaper != nil {
			s.reaper.Stop()
		}
		return nil
	}
	err := worker.Close()
	if s.reaper != nil {
		s.reaper.Stop()
	}
	return err
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
	s.mux.HandleFunc("/invoke", s.handleInvoke)
}

func (s *Server) startWorker() {
	worker, errResp := startPythonWorker(s.cfg, s.reaper)

	s.mu.Lock()
	defer s.mu.Unlock()
	if errResp != nil {
		s.importError = errResp
		return
	}
	s.worker = worker
	s.ready = true
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "OK"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	ready := s.ready
	importError := s.importError
	s.mu.RUnlock()

	if importError != nil {
		writeJSON(w, http.StatusServiceUnavailable, importError)
		return
	}
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
			Status: "FAILED",
			Reason: "not_ready",
			Detail: "agent import has not completed",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "OK"})
}

func (s *Server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := readLimitedBody(w, r)
	if err != nil {
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status: "FAILED",
			Reason: "invalid_json",
			Detail: err.Error(),
		})
		return
	}

	s.invokeMu.Lock()
	defer s.invokeMu.Unlock()

	worker, errResp := s.workerForInvoke()
	if errResp != nil {
		writeJSON(w, http.StatusServiceUnavailable, errResp)
		return
	}

	meta := newInvokeMetadata(payload, s.cfg)
	// B30-T03 Part B (ceiling 4): attach the TimeEnvelope from the payload to
	// the budget config so the wall-clock budget is derived from
	// ActiveTimeRemainingMs when present (legacy 120s fallback otherwise).
	bCfg := budgetFromPayload(payload)
	if env, ok := routedrun.UnmarshalTimeEnvelopeFromPayload(payload); ok {
		bCfg.TimeEnvelope = &env
	}
	budget := newBudgetEnforcer(bCfg, meta.runID, meta.invokeID, s.cfg.Audit, time.Now)
	// B30-T03 Part B (ceiling 3): derive the /invoke context timeout from the
	// TimeEnvelope carried in the payload (via the durable admission receipt)
	// when present; otherwise fall back to the configured InvokeTimeout (the
	// legacy v0.2.3 300s default). The env var AGENTPAAS_INVOKE_TIMEOUT
	// remains a legacy compat override.
	ctx, cancel := contextWithOptionalTimeout(r.Context(), s.invokeTimeoutForPayload(payload))
	defer cancel()
	resp, invokeErr, evidence := worker.Invoke(ctx, payload, budget, s.cfg.TerminateGrace)
	if invokeErr != nil {
		invokeErr = attachFailureContext(invokeErr, buildFailureContext(invokeErr, meta, evidence), s.cfg.Audit)
		writeJSON(w, http.StatusInternalServerError, invokeErr)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// invokeTimeoutForPayload returns the /invoke context timeout. When the
// payload carries a TimeEnvelope (B30-T03 Part B, ceiling 3), the timeout is
// derived from env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs) —
// the min of the stall timeout, the attempt-lease remaining, and the active
// time remaining. When no envelope is present (legacy v0.2.3 compat), it
// falls back to s.cfg.InvokeTimeout (the 300s default from
// AGENTPAAS_INVOKE_TIMEOUT or its legacy default).
func (s *Server) invokeTimeoutForPayload(payload map[string]any) time.Duration {
	if env, ok := routedrun.UnmarshalTimeEnvelopeFromPayload(payload); ok {
		nowMs := routedrun.NowMonotonicMs(nil)
		if s.nowMonotonicMs != nil {
			nowMs = s.nowMonotonicMs()
		}
		deadlineMs := env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs)
		if deadlineMs <= 0 {
			// Envelope exhausted: tiny grace so the call surfaces a structured
			// error rather than a zero-timeout panic.
			return 1 * time.Millisecond
		}
		return time.Duration(deadlineMs) * time.Millisecond
	}
	return s.cfg.InvokeTimeout
}

func (s *Server) workerForInvoke() (*pythonWorker, *ErrorResponse) {
	s.mu.RLock()
	worker := s.worker
	importError := s.importError
	ready := s.ready
	s.mu.RUnlock()

	if importError != nil {
		return nil, importError
	}
	if !ready || worker == nil {
		return nil, &ErrorResponse{
			Status: "FAILED",
			Reason: "not_ready",
			Detail: "agent is not ready to accept invokes",
		}
	}
	if !worker.isClosed() {
		return worker, nil
	}

	worker, errResp := startPythonWorker(s.cfg, s.reaper)

	s.mu.Lock()
	defer s.mu.Unlock()
	if errResp != nil {
		s.ready = false
		s.importError = errResp
		return nil, errResp
	}
	s.worker = worker
	s.ready = true
	s.importError = nil
	return worker, nil
}

func readLimitedBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	reader := http.MaxBytesReader(w, r.Body, MaxPayloadBytes)
	defer func() { _ = reader.Close() }() // best-effort close

	body, err := io.ReadAll(reader)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{
				Status: "FAILED",
				Reason: "payload_too_large",
				Detail: fmt.Sprintf("request body exceeds %d bytes", MaxPayloadBytes),
			})
			return nil, err
		}
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status: "FAILED",
			Reason: "read_failed",
			Detail: err.Error(),
		})
		return nil, err
	}
	return body, nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.Python == "" {
		cfg.Python = "python3"
	}
	if cfg.ImportTimeout == 0 {
		cfg.ImportTimeout = defaultImportTimeout
	}
	if cfg.TerminateGrace == 0 {
		cfg.TerminateGrace = defaultTerminateGrace
	}
	if cfg.StdoutPath == "" {
		cfg.StdoutPath = filepath.Join(os.TempDir(), fmt.Sprintf("agentpaas-harness-%d.stdout.log", time.Now().UnixNano()))
	}
	if cfg.StderrPath == "" {
		cfg.StderrPath = filepath.Join(os.TempDir(), fmt.Sprintf("agentpaas-harness-%d.stderr.log", time.Now().UnixNano()))
	}
	return cfg
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	value = sanitizeResponse(value)
	_ = json.NewEncoder(w).Encode(value) // best-effort encode to client
}

func sanitizeResponse(value any) any {
	switch v := value.(type) {
	case ErrorResponse:
		v.Detail = sanitizeDetail(v.Detail)
		if v.FailureContext != nil {
			v.FailureContext.RedactedDetail = sanitizeDetail(v.FailureContext.RedactedDetail)
		}
		return v
	case *ErrorResponse:
		if v == nil {
			return v
		}
		cleaned := *v
		cleaned.Detail = sanitizeDetail(cleaned.Detail)
		if cleaned.FailureContext != nil {
			ctx := *cleaned.FailureContext
			ctx.RedactedDetail = sanitizeDetail(ctx.RedactedDetail)
			cleaned.FailureContext = &ctx
		}
		return &cleaned
	default:
		return value
	}
}

func sanitizeDetail(detail string) string {
	return strings.Map(func(r rune) rune {
		if r == '\u2028' || r == '\u2029' || r == '\x7f' || (unicode.IsControl(r) && r != '\t') {
			return ' '
		}
		return r
	}, detail)
}

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr) // intentionally ignored (reviewed)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
