//go:build adversary

package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdversaryB10T02_CSP_PresentOnAllRoutes(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	routesToCheck := []string{
		"/",
		"/nonexistent/path",
		"/app.js",
		"/app.css",
		"/api/health",
		"/api/resources",
		"/api/csrf",
	}
	for _, path := range routesToCheck {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if strings.HasPrefix(path, "/api/") && path != "/api/health" {
			// some require auth but header still set by middleware
		}
		s.handler.ServeHTTP(rr, req)
		csp := rr.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("ADVERSARY BREAK: CSP header missing on route %s (status %d)", path, rr.Code)
		}
		if strings.Contains(csp, "'unsafe-inline'") || strings.Contains(csp, "'unsafe-eval'") {
			t.Fatalf("ADVERSARY BREAK: CSP contains unsafe directives on %s: %q", path, csp)
		}
	}
}

func TestAdversaryB10T02_CSP_OnErrorResponses(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	errorCases := []struct {
		path   string
		method string
	}{
		{"/api/resources", http.MethodGet}, // no auth -> 401
		{"/api/agents", http.MethodPost},   // method not allowed? but 401 first?
	}
	for _, ec := range errorCases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(ec.method, ec.path, nil)
		s.handler.ServeHTTP(rr, req)
		csp := rr.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("ADVERSARY BREAK: CSP missing on error response for %s %s (status %d)", ec.method, ec.path, rr.Code)
		}
	}
}

func TestAdversaryB10T02_APIKey_Required_AllAPI_Routes_Malformed(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	apiPaths := []string{"/api/resources", "/api/agents", "/api/gateways", "/api/mcp-servers", "/api/csrf", "/api/health"}
	malformedAuths := []string{
		"",
		"Bearer",
		"Bearer ",
		"token123",
		"Bearer wrongkey",
		"Bearer " + testAPIKey + "extra",
	}
	for _, path := range apiPaths {
		for _, auth := range malformedAuths {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if auth != "" {
				req.Header.Set("Authorization", auth)
			}
			s.handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("ADVERSARY BREAK: expected 401 for %s with auth=%q, got %d", path, auth, rr.Code)
			}
		}
	}
}

func TestAdversaryB10T02_CSRF_Required_Mutating_Malformed(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	mutatingPaths := []string{"/api/health", "/api/resources"} // resources is GET only but test mutate
	for _, path := range mutatingPaths {
		// no CSRF
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		s.handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("ADVERSARY BREAK: expected 403 without CSRF on %s, got %d", path, rr.Code)
		}
		// wrong CSRF
		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		req.Header.Set("X-CSRF-Token", "wrong-token-123")
		s.handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("ADVERSARY BREAK: expected 403 with wrong CSRF on %s, got %d", path, rr.Code)
		}
	}
}

func TestAdversaryB10T02_PathTraversal_StaticFS(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	traversalPaths := []string{
		"/../../etc/passwd",
		"/..%2F..%2Fetc/passwd",
		"/%2e%2e/etc/passwd",
		"/app.js/../../secret",
	}
	for _, path := range traversalPaths {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		s.handler.ServeHTTP(rr, req)
		body := rr.Body.String()
		if strings.Contains(body, "root:") || strings.Contains(body, "/bin/bash") || strings.Contains(body, "/bin/sh") {
			t.Fatalf("ADVERSARY BREAK: path traversal succeeded on %s, body leaked: %q", path, body[:min(100, len(body))])
		}
		// should be either 200 index or 404/500 but not system file
		if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound && rr.Code != http.StatusInternalServerError {
			t.Logf("note: traversal path %s returned %d", path, rr.Code)
		}
	}
}

func TestAdversaryB10T02_NoExternalCDN_OrLocalStorageInDist(t *testing.T) {
	html, err := spaFiles.ReadFile("dist/index.html")
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	js, err := spaFiles.ReadFile("dist/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	css, err := spaFiles.ReadFile("dist/app.css")
	if err != nil {
		t.Fatalf("read css: %v", err)
	}
	combined := string(html) + string(js) + string(css)
	if strings.Contains(combined, "http://") || strings.Contains(combined, "https://") {
		t.Fatal("ADVERSARY BREAK: external URL (CDN) found in dist assets")
	}
	if strings.Contains(string(html), "localStorage") || strings.Contains(string(js), "localStorage") {
		t.Fatal("ADVERSARY BREAK: localStorage reference found in dist assets")
	}
	// sessionStorage is used (per code review) but claim only prohibits localStorage
	if !strings.Contains(string(js), "sessionStorage") {
		t.Log("note: sessionStorage not found, but not a break per claims")
	}
}

func TestAdversaryB10T02_XSS_Escaping_InAPIResponses(t *testing.T) {
	malicious := AgentResource{
		ID:     "agent-xss",
		Name:   `<script>alert(1)</script>`,
		Status: "running",
		Health: `<img src=x onerror=alert(1)>`,
		Labels: map[string]string{"evil": `"><script>`},
	}
	s := NewServer("", testAPIKey, nil, &MockResourceManager{Agents: []AgentResource{malicious}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "<script") || strings.Contains(body, "<img") || strings.Contains(body, "</script>") {
		t.Fatalf("ADVERSARY BREAK: unescaped XSS payload in JSON response: %s", body)
	}
	// verify JSON encoding
	var decoded []AgentResource
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Name != malicious.Name {
		t.Fatalf("data mismatch")
	}
}

func TestAdversaryB10T02_NoCookies_Set(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.handler.ServeHTTP(rr, req)
	if cookies := rr.Result().Cookies(); len(cookies) > 0 {
		t.Fatalf("ADVERSARY BREAK: server set cookies: %+v", cookies)
	}
	// also on API
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	s.handler.ServeHTTP(rr, req)
	if cookies := rr.Result().Cookies(); len(cookies) > 0 {
		t.Fatalf("ADVERSARY BREAK: server set cookies on API: %+v", cookies)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
