package dashboard

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/otel"
)

//go:embed dist/*
var spaFiles embed.FS

// Server serves the dashboard SPA and JSON API.
type Server struct {
	mu          sync.RWMutex
	handler     http.Handler
	addr        string
	apiKey      string
	csrfToken   string
	srv         *http.Server
	store       *otel.Store
	resourceMgr ResourceManager
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
	s.handler = cspMiddleware(loggingMiddleware(s.routes()))
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

	root.Handle("/api/", csrfMiddleware(s.csrfToken, authMiddleware(s.apiKey, apiMux)))
	root.Handle("/", spaHandler(dist))
	return root
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
