package harness

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAguiProxyForwardsHealthAndRoot(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		if r.URL.Path == "/" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"RUN_STARTED\"}\n\n")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(backend.Close)
	t.Setenv("AGENTPAAS_AGUI_LOOPBACK", backend.URL)

	s := &Server{mux: http.NewServeMux()}
	s.routes()

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agui/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("health body = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agui/", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "RUN_STARTED") {
		t.Fatalf("run body = %s", rec.Body.String())
	}
}
