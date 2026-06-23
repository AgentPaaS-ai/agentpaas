package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestReconnect_SSEEndpointResilient verifies the SSE timeline endpoint
// handles client disconnection gracefully.
func TestReconnect_SSEEndpointResilient(t *testing.T) {
	s := NewServer("", testAPIKey, nil, &MockResourceManager{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/test-run-id/timeline", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Errorf("unexpected status %d on SSE disconnect", rr.Code)
	}
}
