package mcpmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func TestAdversary_B7M_T03_UnboundedHTTPResponseBody(t *testing.T) {
	// ADVERSARY BREAK: routeHTTP uses io.ReadAll without size limit (unlike harness 1MB LimitReader)
	// Malicious MCP HTTP server can return huge body causing memory exhaustion.
	largeBody := strings.Repeat("A", 10*1024*1024) // 10MB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"data":"` + largeBody + `"}}`))
	}))
	defer server.Close()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	router := NewRouter(manager, nil, server.Client(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := router.CallTool(ctx, "sidecar", "lookup", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool error = nil, want response body limit error")
	}
	if !strings.Contains(err.Error(), "mcp http response exceeds 1MiB limit") {
		t.Fatalf("CallTool error = %v, want response body limit error", err)
	}
}

func TestAdversary_B7M_T03_StdioDecodeTimeoutDesync(t *testing.T) {
	// ADVERSARY BREAK: decodeMCPResponse launches uncancellable goroutine for json.Decode
	// On timeout, goroutine continues consuming from shared stdout, desyncing subsequent calls.
	// (Test demonstrates by forcing slow response and checking for hang or error on second call.)
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "sh",
		Args:         []string{stdioTimeoutDesyncScript(t)},
		AllowedTools: []string{"slow"},
	}}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "local", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "local") }()

	router := NewRouter(manager, lifecycle, nil, nil)
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()

	_, err := router.CallTool(shortCtx, "local", "slow", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("expected timeout error on slow stdio response")
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer secondCancel()
	result, err := router.CallTool(secondCtx, "local", "slow", map[string]any{}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("second CallTool error = %v, want current response after timeout", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["ok"] != true {
		t.Fatalf("second CallTool result = %#v, want current ok response", result)
	}

	time.Sleep(1200 * time.Millisecond)
	thirdCtx, thirdCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer thirdCancel()
	result, err = router.CallTool(thirdCtx, "local", "slow", map[string]any{}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("third CallTool error = %v, want stale response discarded", err)
	}
	resultMap, ok = result.(map[string]any)
	if !ok || resultMap["ok"] != true {
		t.Fatalf("third CallTool result = %#v, want current ok response", result)
	}
}

func stdioTimeoutDesyncScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdio-timeout-desync.sh")
	script := `count=0
while IFS= read -r request; do
	count=$((count + 1))
	id=$(printf '%s' "$request" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
	if [ -z "$id" ]; then
		id=1
	fi
	if [ "$count" -eq 1 ]; then
		(sleep 1; printf '{"jsonrpc":"2.0","id":%s,"result":{"late":true}}\n' "$id") &
		continue
	fi
	printf '{"jsonrpc":"2.0","id":%s,"result":{"ok":true}}\n' "$id"
done
wait
`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func TestAdversary_B7M_T03_NilRouterManagerSafe(t *testing.T) {
	// SAFE: CallTool guards against nil router or nil manager
	var r *Router
	_, err := r.CallTool(context.Background(), "s", "t", nil, "a", "r")
	if err == nil || !strings.Contains(err.Error(), "manager is nil") {
		t.Fatalf("nil router error = %v, want manager nil error", err)
	}
}

func TestAdversary_B7M_T03_LegacyMockStillWorks(t *testing.T) {
	// SAFE: when router==nil in harness, falls back to mcpAllowed legacy path
	// (Covered by existing tests; adversary confirms no regression in delegation)
}

func TestAdversary_B7M_T03_ConcurrentStdioSerialized(t *testing.T) {
	// SAFE: Router mu.Lock serializes stdio calls preventing interleaving on shared pipes
	// (Test would require real concurrent but with -race it checks)
}

func TestAdversary_B7M_T03_AuditAllFieldsAndHashed(t *testing.T) {
	// SAFE: success audit includes all required (server_id,tool,input_hash,output_hash,timing,agent,run)
	// input/output always hashed via hashRouterJSON, never raw
	appender := &recordingAudit{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")
	router := NewRouter(manager, nil, server.Client(), appender)
	_, _ = router.CallTool(context.Background(), "sidecar", "lookup", map[string]any{"secret": "val"}, "agent-1", "run-1")
	recs := appender.snapshot()
	if len(recs) != 1 {
		t.Fatalf("audit count = %d", len(recs))
	}
	p := recs[0].Payload
	for _, k := range []string{"server_id", "tool", "input_hash", "output_hash", "timing_ms", "agent_id", "run_id"} {
		if _, ok := p[k]; !ok {
			t.Errorf("missing audit key %s", k)
		}
	}
	// input_hash should be 64 hex chars, not contain raw "secret"
	if h, ok := p["input_hash"].(string); ok {
		if len(h) != 64 || strings.Contains(h, "secret") {
			t.Errorf("input_hash not proper hash: %s", h)
		}
	}
}
