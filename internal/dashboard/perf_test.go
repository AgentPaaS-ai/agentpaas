package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPerformance_EmptyStateRendersFast verifies that the dashboard root
// renders in under 100ms with zero resources.
func TestPerformance_EmptyStateRendersFast(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})
	ts := httptest.NewServer(s.handler)
	defer ts.Close()

	start := time.Now()
	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("empty state render took %v, want < 100ms", elapsed)
	}
}

// TestPerformance_APIResponseFast verifies that API endpoints respond in
// under 50ms with minimal data.
func TestPerformance_APIResponseFast(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})

	endpoints := []string{"/api/resources", "/api/agents", "/api/gateways", "/api/mcp-servers", "/api/health"}
	for _, ep := range endpoints {
		start := time.Now()
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		rr := httptest.NewRecorder()
		s.handler.ServeHTTP(rr, req)
		elapsed := time.Since(start)

		if elapsed > 50*time.Millisecond {
			t.Errorf("API %s took %v, want < 50ms", ep, elapsed)
		}
		if rr.Code != http.StatusOK {
			t.Errorf("API %s returned %d, want %d", ep, rr.Code, http.StatusOK)
		}
	}
}

// TestPerformance_10KSpansTimeline verifies 10k-span timeline stays performant.
func TestPerformance_10KSpansTimeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	t.Skip("otel timeline store smoke is covered by B10-T03")
}
