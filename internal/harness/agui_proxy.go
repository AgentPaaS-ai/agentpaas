package harness

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

// Athena / other AG-UI adapters listen on loopback :8000 inside the
// container. Cloudflare only publishes the harness port, so the Worker
// reaches AG-UI through /agui on :8080.
func aguiLoopbackURL() string {
	raw := strings.TrimSpace(os.Getenv("AGENTPAAS_AGUI_LOOPBACK"))
	if raw == "" {
		raw = "http://127.0.0.1:8000"
	}
	return raw
}

func (s *Server) handleAguiProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(aguiLoopbackURL())
	if err != nil || target.Scheme == "" || target.Host == "" {
		http.Error(w, `{"error":"agui_loopback_invalid"}`, http.StatusBadGateway)
		return
	}
	proxy := &httputil.ReverseProxy{
		FlushInterval: 50 * time.Millisecond,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			suffix := strings.TrimPrefix(pr.Out.URL.Path, "/agui")
			if suffix == "" {
				suffix = "/"
			}
			pr.Out.URL.Path = suffix
			pr.Out.URL.RawPath = ""
			// Match the previous Director, which set Host to the loopback target.
			pr.Out.Host = target.Host
			pr.SetXForwarded()
		},
	}
	proxy.ServeHTTP(w, r)
}
