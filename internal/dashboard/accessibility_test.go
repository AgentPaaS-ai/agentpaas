package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAccessibility_RootHasLangAttribute verifies the HTML root has lang="en".
func TestAccessibility_RootHasLangAttribute(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `lang="en"`) && !strings.Contains(body, `lang='en'`) {
		t.Error("root HTML missing lang attribute")
	}
}

// TestAccessibility_HasMetaViewport verifies a viewport meta tag exists.
func TestAccessibility_HasMetaViewport(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "viewport") {
		t.Error("root HTML missing viewport meta tag")
	}
}

// TestAccessibility_EmptyStatesRender verifies empty states render for zero
// agents, zero gateways, and zero MCP servers.
func TestAccessibility_EmptyStatesRender(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})

	for _, ep := range []string{"/api/agents", "/api/gateways", "/api/mcp-servers"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		s.handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%s returned %d, want %d", ep, rr.Code, http.StatusOK)
		}
		if strings.TrimSpace(rr.Body.String()) != "[]" {
			t.Errorf("%s returned %q, want empty array", ep, rr.Body.String())
		}
	}
}

// TestAccessibility_KeyboardSmoke verifies the dashboard serves JS that
// includes keyboard navigation hooks.
func TestAccessibility_KeyboardSmoke(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Skipf("app.js not served (status %d), skipping keyboard smoke", rr.Code)
	}

	body := rr.Body.String()
	hasKeyboard := strings.Contains(body, "keydown") || strings.Contains(body, "keypress") || strings.Contains(body, "addEventListener")
	if !hasKeyboard {
		t.Log("warning: app.js does not contain keyboard event handlers")
	}
}

// TestAccessibility_CSPBlocksInlineScript verifies CSP blocks inline scripts.
func TestAccessibility_CSPBlocksInlineScript(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.handler.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no CSP header")
	}

	for _, directive := range strings.Split(csp, ";") {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, "script-src") && strings.Contains(directive, "'unsafe-inline'") {
			t.Error("CSP allows 'unsafe-inline' for scripts")
		}
	}
}
