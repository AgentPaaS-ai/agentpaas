// Package oauth — local loopback consent listener (M13.9-T3).
//
// Binds 127.0.0.1 only, random port, single grant, 10-minute TTL.
package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// LoopbackConsent serves /oauth/start and /oauth/callback on 127.0.0.1.
type LoopbackConsent struct {
	mu       sync.Mutex
	ln       net.Listener
	srv      *http.Server
	port     int
	grantID  string
	onCode   func(code, state string) error
	expires  time.Time
	closed   bool
}

// StartLoopback binds 127.0.0.1:0 and serves until Stop or TTL.
func StartLoopback(grantID string, ttl time.Duration, onCode func(code, state string) error) (*LoopbackConsent, error) {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	lc := &LoopbackConsent{
		ln:      ln,
		port:    ln.Addr().(*net.TCPAddr).Port,
		grantID: grantID,
		onCode:  onCode,
		expires: time.Now().Add(ttl),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", lc.handleCallback)
	mux.HandleFunc("/oauth/start/", lc.handleStart)
	lc.srv = &http.Server{Handler: mux}
	go func() { _ = lc.srv.Serve(ln) }()
	return lc, nil
}

// BaseURL is http://127.0.0.1:<port>
func (l *LoopbackConsent) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", l.port)
}

// ConsentURL for the grant.
func (l *LoopbackConsent) ConsentURL() string {
	return fmt.Sprintf("%s/oauth/start/%s", l.BaseURL(), url.PathEscape(l.grantID))
}

func (l *LoopbackConsent) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if time.Now().After(l.expires) {
		http.Error(w, "expired", http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>
<h1>Authorize agent access</h1>
<p>Local AgentPaaS consent loopback. Complete OAuth in your browser; callback returns here.</p>
</body></html>`))
}

func (l *LoopbackConsent) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if time.Now().After(l.expires) {
		http.Error(w, "expired", http.StatusGone)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	l.mu.Lock()
	fn := l.onCode
	l.mu.Unlock()
	if fn != nil {
		if err := fn(code, state); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>Connected</h1><p>You can close this tab.</p></body></html>`))
}

// Stop shuts down the listener.
func (l *LoopbackConsent) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.srv != nil {
		return l.srv.Shutdown(ctx)
	}
	return l.ln.Close()
}
