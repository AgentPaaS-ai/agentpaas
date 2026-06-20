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
)

const (
	MaxPayloadBytes = 10 * 1024 * 1024

	defaultAddr          = "127.0.0.1:8080"
	defaultImportTimeout = 10 * time.Second
	defaultInvokeTimeout = 30 * time.Second
)

// Config controls the harness HTTP server and Python worker.
type Config struct {
	Addr          string
	AgentPath     string
	Python        string
	ImportTimeout time.Duration
	InvokeTimeout time.Duration
	StdoutPath    string
	StderrPath    string
}

// ErrorResponse is the structured failure envelope returned by lifecycle APIs.
type ErrorResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
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

	mu          sync.RWMutex
	ready       bool
	importError *ErrorResponse
	closed      bool

	invokeMu sync.Mutex
}

// NewServer creates a harness server and performs the Python import phase.
func NewServer(cfg Config) *Server {
	cfg = normalizeConfig(cfg)
	s := &Server{
		cfg: cfg,
		mux: http.NewServeMux(),
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
		return nil
	}
	return worker.Close()
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
	s.mux.HandleFunc("/invoke", s.handleInvoke)
}

func (s *Server) startWorker() {
	worker, errResp := startPythonWorker(s.cfg)

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

	s.mu.RLock()
	worker := s.worker
	importError := s.importError
	ready := s.ready
	s.mu.RUnlock()

	if importError != nil {
		writeJSON(w, http.StatusServiceUnavailable, importError)
		return
	}
	if !ready || worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
			Status: "FAILED",
			Reason: "not_ready",
			Detail: "agent is not ready to accept invokes",
		})
		return
	}

	s.invokeMu.Lock()
	defer s.invokeMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.InvokeTimeout)
	defer cancel()
	resp, errResp := worker.Invoke(ctx, payload)
	if errResp != nil {
		writeJSON(w, http.StatusInternalServerError, errResp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func readLimitedBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	reader := http.MaxBytesReader(w, r.Body, MaxPayloadBytes)
	defer func() { _ = reader.Close() }()

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
	if cfg.InvokeTimeout == 0 {
		cfg.InvokeTimeout = defaultInvokeTimeout
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
	_ = json.NewEncoder(w).Encode(value)
}

func sanitizeResponse(value any) any {
	switch v := value.(type) {
	case ErrorResponse:
		v.Detail = sanitizeDetail(v.Detail)
		return v
	case *ErrorResponse:
		if v == nil {
			return v
		}
		cleaned := *v
		cleaned.Detail = sanitizeDetail(cleaned.Detail)
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
	host, _, err := net.SplitHostPort(addr)
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
