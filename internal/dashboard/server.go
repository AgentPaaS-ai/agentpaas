package dashboard

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/otel"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

//go:embed dist/*
var spaFiles embed.FS

// Server serves the dashboard SPA and JSON API.
type Server struct {
	mu           sync.RWMutex
	handler      http.Handler
	addr         string
	apiKey       string
	csrfToken    string
	srv          *http.Server
	store        *otel.Store
	resourceMgr  ResourceManager
	timeline     *TimelineHandler
	logViewer    *LogViewerHandler
	auditIndexer *audit.SQLiteIndexer
	policyDir    string

	auditSigningKey     *ecdsa.PrivateKey
	auditPubKeyDER      []byte
	auditTrustAnchorDER []byte
}

// ResourceManager provides inventory data for the dashboard.
// Implemented by the daemon (not the dashboard package itself).
type ResourceManager interface {
	ListAgents(ctx context.Context) ([]AgentResource, error)
	ListGateways(ctx context.Context) ([]GatewayResource, error)
	ListMCPServers(ctx context.Context) ([]MCPServerResource, error)
}

// AgentResource represents a managed agent in the dashboard inventory.
type AgentResource struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Status       string            `json:"status"`
	ImageDigest  string            `json:"image_digest,omitempty"`
	ContainerID  string            `json:"container_id,omitempty"`
	Network      string            `json:"network,omitempty"`
	Health       string            `json:"health"`
	RestartCount int               `json:"restart_count"`
	CreatedAt    time.Time         `json:"created_at"`
	Labels       map[string]string `json:"labels,omitempty"`
	MemoryBytes  int64             `json:"memory_bytes,omitempty"`
	PidsLimit    int               `json:"pids_limit,omitempty"`
}

// GatewayResource represents a managed gateway sidecar.
type GatewayResource struct {
	ID           string    `json:"id"`
	AgentID      string    `json:"agent_id"`
	Status       string    `json:"status"`
	ImageDigest  string    `json:"image_digest,omitempty"`
	ContainerID  string    `json:"container_id,omitempty"`
	Network      string    `json:"network,omitempty"`
	EgressNet    string    `json:"egress_network,omitempty"`
	Health       string    `json:"health"`
	RestartCount int       `json:"restart_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// MCPServerResource represents a managed MCP server.
type MCPServerResource struct {
	ID           string    `json:"id"`
	AgentID      string    `json:"agent_id"`
	Status       string    `json:"status"`
	ServerType   string    `json:"type"`
	ContainerID  string    `json:"container_id,omitempty"`
	AllowedTools []string  `json:"allowed_tools"`
	Health       string    `json:"health"`
	RestartCount int       `json:"restart_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// NewServer creates a new dashboard server.
// apiKey is required for API access; if empty, API returns 401.
func NewServer(addr, apiKey string, store *otel.Store, mgr ResourceManager) *Server {
	return newServer(addr, apiKey, store, mgr, nil)
}

// NewServerWithTimeline creates a dashboard server with live run timeline support.
func NewServerWithTimeline(addr, apiKey string, store *otel.Store, mgr ResourceManager, bus *trigger.EventBus) *Server {
	return newServer(addr, apiKey, store, mgr, bus)
}

// NewServerWithAudit creates a dashboard server with an audit SQLite indexer.
func NewServerWithAudit(addr, apiKey string, store *otel.Store, mgr ResourceManager, indexer *audit.SQLiteIndexer) *Server {
	s := newServer(addr, apiKey, store, mgr, nil)
	s.auditIndexer = indexer
	return s
}

// SetAuditIndexer sets the audit SQLite indexer used by audit search routes.
func (s *Server) SetAuditIndexer(indexer *audit.SQLiteIndexer) {
	s.auditIndexer = indexer
}

// SetPolicyDir sets the base directory used to resolve relative policy paths.
func (s *Server) SetPolicyDir(policyDir string) {
	s.policyDir = policyDir
}

// SetResourceManager sets the resource inventory provider for dashboard APIs.
func (s *Server) SetResourceManager(mgr ResourceManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resourceMgr = mgr
}

// SetEventBus sets the event bus for live run timeline events. If the
// timeline handler was not created at construction time (because both bus
// and store were nil), this creates it now.
func (s *Server) SetEventBus(bus *trigger.EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timeline == nil && (bus != nil || s.store != nil) {
		s.timeline = NewTimelineHandler(bus, s.store)
	}
}

// SetAuditSigningKey sets the key material used for signed audit exports.
func (s *Server) SetAuditSigningKey(key *ecdsa.PrivateKey, pubKeyDER []byte) {
	s.auditSigningKey = key
	s.auditPubKeyDER = append([]byte(nil), pubKeyDER...)
}

// SetAuditTrustAnchor sets the public key DER used for trust-anchor display.
func (s *Server) SetAuditTrustAnchor(pubKeyDER []byte) {
	s.auditTrustAnchorDER = append([]byte(nil), pubKeyDER...)
}

func newServer(addr, apiKey string, store *otel.Store, mgr ResourceManager, bus *trigger.EventBus) *Server {
	if addr == "" {
		addr = ":8090"
	}
	s := &Server{
		addr:        addr,
		apiKey:      apiKey,
		csrfToken:   newCSRFToken(),
		store:       store,
		resourceMgr: mgr,
	}
	if bus != nil || store != nil {
		s.timeline = NewTimelineHandler(bus, store)
	}
	if store != nil {
		s.logViewer = NewLogViewerHandler(store)
		if provider, ok := mgr.(DockerArtifactProvider); ok {
			s.logViewer.artifactProvider = provider
		}
	}
	s.handler = cspMiddleware(loggingMiddleware(timelinePathValidationMiddleware(s.routes())))
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	s.mu.RLock()
	srv := s.srv
	s.mu.RUnlock()
	if srv == nil {
		return errors.New("dashboard server not initialized")
	}
	return srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	srv := s.srv
	s.mu.RUnlock()
	return srv.Shutdown(ctx)
}

func (s *Server) routes() http.Handler {
	apiMux := http.NewServeMux()
	root := http.NewServeMux()
	dist, err := fs.Sub(spaFiles, "dist")
	if err != nil {
		root.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusInternalServerError, "dashboard assets unavailable")
		})
		return root
	}
	apiMux.HandleFunc("/api/csrf", s.getOnly(s.handleCSRF))
	apiMux.HandleFunc("/api/resources", s.getOnly(s.handleResources))
	apiMux.HandleFunc("/api/agents", s.getOnly(s.handleAgents))
	apiMux.HandleFunc("/api/gateways", s.getOnly(s.handleGateways))
	apiMux.HandleFunc("/api/mcp-servers", s.getOnly(s.handleMCPServers))
	apiMux.HandleFunc("/api/health", s.handleHealth)
	apiMux.HandleFunc("/api/runs/", s.getOnly(s.handleRunAPI))
	apiMux.HandleFunc("/api/policy/diff", s.getOnly(s.ServePolicyDiff))
	apiMux.HandleFunc("/api/audit/search", s.getOnly(s.ServeAuditSearch))
	apiMux.HandleFunc("/api/audit/export", s.ServeAuditExport)
	apiMux.HandleFunc("/api/audit/verify", s.getOnly(s.ServeAuditVerify))

	root.Handle("/api/", csrfMiddleware(s.csrfToken, authMiddleware(s.apiKey, apiMux)))
	root.Handle("/", spaHandler(dist))
	return root
}

func (s *Server) handleRunAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/timeline"):
		s.handleTimeline(w, r)
	case strings.HasSuffix(r.URL.Path, "/logs"):
		s.handleLogs(w, r)
	case strings.HasSuffix(r.URL.Path, "/spans"):
		s.handleSpans(w, r)
	case strings.HasSuffix(r.URL.Path, "/artifacts"):
		s.handleDockerArtifacts(w, r)
	case strings.HasSuffix(r.URL.Path, "/cost"):
		s.handleCost(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, "run endpoint not found")
	}
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	if s.timeline == nil {
		writeJSONError(w, http.StatusNotFound, "timeline unavailable")
		return
	}
	runID := runIDFromTimelinePath(r.URL.Path)
	if runID == "" {
		writeJSONError(w, http.StatusNotFound, "timeline not found")
		return
	}
	r.SetPathValue("runID", runID)
	s.timeline.ServeSSE(w, r)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if s.logViewer == nil {
		writeJSONError(w, http.StatusNotFound, "log viewer unavailable")
		return
	}
	runID := runIDFromRunAPIPath(r.URL.Path, "logs")
	r.SetPathValue("runID", runID)
	s.logViewer.ServeLogs(w, r)
}

func (s *Server) handleSpans(w http.ResponseWriter, r *http.Request) {
	if s.logViewer == nil {
		writeJSONError(w, http.StatusNotFound, "log viewer unavailable")
		return
	}
	runID := runIDFromRunAPIPath(r.URL.Path, "spans")
	r.SetPathValue("runID", runID)
	s.logViewer.ServeSpans(w, r)
}

func (s *Server) handleDockerArtifacts(w http.ResponseWriter, r *http.Request) {
	if s.logViewer == nil {
		writeJSONError(w, http.StatusNotFound, "log viewer unavailable")
		return
	}
	runID := runIDFromRunAPIPath(r.URL.Path, "artifacts")
	r.SetPathValue("runID", runID)
	s.logViewer.ServeDockerArtifacts(w, r)
}

func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSONError(w, http.StatusNotFound, "cost data unavailable")
		return
	}
	runID := runIDFromRunAPIPath(r.URL.Path, "cost")
	r.SetPathValue("runID", runID)
	s.ServeRunCost(w, r)
}

func (s *Server) getOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next(w, r)
	}
}

func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if requestPath != "" {
			file, err := dist.Open(requestPath)
			if err == nil {
				defer func() { _ = file.Close() }()
				if info, statErr := file.Stat(); statErr == nil && !info.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
			}
		}

		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "dashboard assets unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}

func newCSRFToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}
