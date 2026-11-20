package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "test-dashboard-api-key"

func TestCSP_BlocksInlineScript(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	s.handler.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("expected Content-Security-Policy header")
	}
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("CSP must not allow inline scripts: %q", csp)
	}
	if strings.Contains(csp, "'unsafe-eval'") {
		t.Fatalf("CSP must not allow eval: %q", csp)
	}
}

func TestAPIKey_RequiredForAPI(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/resources", nil)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestCSRF_RequiredForMutatingRoutes(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestCSRF_ValidTokenAllowsMutation(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	token := fetchCSRFToken(t, s)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	req.Header.Set("X-CSRF-Token", token)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestEmptyStates_RendersForZeroResources(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	var got resourcesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Agents == nil || len(got.Agents) != 0 {
		t.Fatalf("expected empty agents array, got %#v", got.Agents)
	}
	if got.Gateways == nil || len(got.Gateways) != 0 {
		t.Fatalf("expected empty gateways array, got %#v", got.Gateways)
	}
	if got.MCPServers == nil || len(got.MCPServers) != 0 {
		t.Fatalf("expected empty mcp_servers array, got %#v", got.MCPServers)
	}
}

func TestResourceInventory_RendersManagerData(t *testing.T) {
	created := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	s := NewServer("", testAPIKey, nil, &MockResourceManager{
		Agents: []AgentResource{{
			ID:           "agent-1",
			Name:         "builder",
			Status:       "running",
			Health:       "healthy",
			RestartCount: 1,
			CreatedAt:    created,
		}},
		Gateways: []GatewayResource{{
			ID:        "gateway-1",
			AgentID:   "agent-1",
			Status:    "running",
			Health:    "healthy",
			CreatedAt: created,
		}},
		MCPServers: []MCPServerResource{{
			ID:           "mcp-1",
			AgentID:      "agent-1",
			Status:       "ready",
			ServerType:   "stdio",
			AllowedTools: []string{"shell"},
			Health:       "healthy",
			CreatedAt:    created,
		}},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	var got resourcesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Agents) != 1 || got.Agents[0].ID != "agent-1" {
		t.Fatalf("expected agent inventory from manager, got %#v", got.Agents)
	}
	if len(got.Gateways) != 1 || got.Gateways[0].ID != "gateway-1" {
		t.Fatalf("expected gateway inventory from manager, got %#v", got.Gateways)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0].ID != "mcp-1" {
		t.Fatalf("expected mcp server inventory from manager, got %#v", got.MCPServers)
	}
}

func TestSPAFallback_ServesIndexHTML(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent/route", nil)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `<div id="app"></div>`) {
		t.Fatalf("expected index.html fallback, got %q", rr.Body.String())
	}
	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected CSP header on fallback response")
	}
}

func TestStaticFiles_ServedFromEmbed(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("expected javascript content type, got %q", ct)
	}
}

func TestNoAPIKeyInLocalStorage(t *testing.T) {
	html, err := spaFiles.ReadFile("dist/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	if strings.Contains(string(html), "localStorage") {
		t.Fatal("index.html must not reference localStorage")
	}
	js, err := spaFiles.ReadFile("dist/app.js")
	if err != nil {
		t.Fatalf("read embedded app: %v", err)
	}
	if strings.Contains(string(js), "localStorage") {
		t.Fatal("app.js must not reference localStorage")
	}
}

func fetchCSRFToken(t *testing.T, s *Server) string {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected csrf status %d, got %d", http.StatusOK, rr.Code)
	}
	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode csrf response: %v", err)
	}
	token := got["csrf_token"]
	if token == "" {
		t.Fatal("expected csrf_token")
	}
	return token
}
