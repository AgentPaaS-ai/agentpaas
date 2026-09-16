package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const openaiLoopbackAPIKey = "sk-agentpaas-loopback"

const maxChatCompletionsBody = 1 << 20

type openaiLoopback struct {
	rpc      *harnessRPCServer
	listener net.Listener
	server   *http.Server
	addr     string
	done     chan struct{}

	closeOnce sync.Once
	closeErr  error
}

type chatCompletionRequest struct {
	Model    string                  `json:"model"`
	Messages []chatCompletionMessage `json:"messages"`
	Stream   bool                    `json:"stream"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func startOpenAILoopback(rpc *harnessRPCServer) (*openaiLoopback, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("openai loopback listen: %w", err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || !tcpAddr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		_ = ln.Close()
		return nil, fmt.Errorf("openai loopback must bind 127.0.0.1, got %v", ln.Addr())
	}

	lb := &openaiLoopback{
		rpc:      rpc,
		listener: ln,
		addr:     ln.Addr().String(),
		done:     make(chan struct{}),
	}
	lb.server = &http.Server{
		Handler:           http.HandlerFunc(lb.serveHTTP),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      maxModelClientTimeout + rpcReadTimeoutSlack,
		IdleTimeout:       30 * time.Second,
		ConnContext:       loopbackConnContext,
	}
	go func() {
		defer close(lb.done)
		_ = lb.server.Serve(ln)
	}()
	return lb, nil
}

func (l *openaiLoopback) Addr() string {
	if l == nil {
		return ""
	}
	return l.addr
}

func (l *openaiLoopback) baseURL() string {
	if l == nil || l.addr == "" {
		return ""
	}
	return "http://" + l.addr + "/v1"
}

func (l *openaiLoopback) Close() error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		if l.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			l.closeErr = l.server.Shutdown(ctx)
		}
		if l.done != nil {
			<-l.done
		}
	})
	return l.closeErr
}

func (l *openaiLoopback) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-AgentPaaS-Loopback", "1")
	path := strings.TrimRight(r.URL.Path, "/")
	if path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validLoopbackBearer(r.Header.Get("Authorization")) {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid api key")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatCompletionsBody)
	var body chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Stream {
		writeOpenAIError(w, http.StatusBadRequest, "streaming not supported on loopback")
		return
	}

	state := l.rpc.currentInvoke()
	if state == nil {
		writeOpenAIError(w, http.StatusConflict, "no active invoke")
		return
	}

	params := map[string]any{"prompt": promptFromChatMessages(body.Messages)}
	if body.Model != "" {
		params["model"] = body.Model
	}
	resp := l.rpc.handleLLM(rpcRequest{ID: "loopback", Method: "llm", Params: params}, state)
	if !resp.OK {
		writeOpenAIError(w, http.StatusBadGateway, resp.Error)
		return
	}
	writeLoopbackChatCompletion(w, body.Model, resp)
}

func validLoopbackBearer(auth string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix)) == openaiLoopbackAPIKey
}

func promptFromChatMessages(messages []chatCompletionMessage) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		text := chatContentText(msg.Content)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func chatContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if text := chatContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if text, ok := v["content"].(string); ok {
			return text
		}
	}
	return ""
}

func writeLoopbackChatCompletion(w http.ResponseWriter, reqModel string, resp rpcResponse) {
	result, _ := resp.Result.(map[string]any)
	text := ""
	model := reqModel
	var tokens int64
	if result != nil {
		if v, ok := result["text"].(string); ok && v != "" {
			text = v
		} else if v, ok := result["content"].(string); ok {
			text = v
		}
		if v, ok := result["model"].(string); ok && v != "" {
			model = v
		}
		switch v := result["tokens"].(type) {
		case int64:
			tokens = v
		case int:
			tokens = int64(v)
		case float64:
			tokens = int64(v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      "chatcmpl-agentpaas-loopback",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": tokens,
			"total_tokens":      tokens,
		},
	})
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func envName(item string) string {
	if i := strings.IndexByte(item, '='); i >= 0 {
		return item[:i]
	}
	return item
}

func isWorkloadOpenAIEnv(item string) bool {
	name := strings.ToUpper(envName(item))
	switch name {
	case "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_API_BASE", "OPENAI_API_HOST",
		"OPENAI_ORG_ID", "OPENAI_ORGANIZATION",
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
		"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "AZURE_OPENAI_BASE_URL",
		"GOOGLE_API_KEY", "GEMINI_API_KEY", "GEMINI_API_ENDPOINT",
		"GOOGLE_GENERATIVE_AI_API_KEY":
		return true
	}
	if strings.Contains(name, "API_KEY") &&
		(strings.Contains(name, "OPENAI") ||
			strings.Contains(name, "ANTHROPIC") ||
			strings.Contains(name, "GEMINI") ||
			strings.Contains(name, "AZURE") ||
			strings.Contains(name, "GOOGLE")) {
		return true
	}
	return false
}

func isWorkloadProxyEnv(item string) bool {
	switch strings.ToUpper(envName(item)) {
	case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		return true
	}
	return false
}

func loopbackConnContext(ctx context.Context, c net.Conn) context.Context {
	addr, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok || addr.IP == nil || !addr.IP.IsLoopback() {
		_ = c.Close()
	}
	return ctx
}

var loopbackDeniedProviderHosts = []string{
	"api.openai.com",
	"api.anthropic.com",
	"openai.azure.com",
	"generativelanguage.googleapis.com",
}

func loopbackDeniesProviderHostBypass() bool {
	return len(loopbackDeniedProviderHosts) > 0
}

func workerEnvOpenAI(base []string, rpcAddr, openaiBaseURL string) []string {
	env := workerEnv(base, rpcAddr)
	out := make([]string, 0, len(env)+6)
	for _, item := range env {
		if isWorkloadOpenAIEnv(item) || isWorkloadProxyEnv(item) {
			continue
		}
		out = append(out, item)
	}
	if openaiBaseURL == "" {
		return out
	}
	out = append(out,
		"OPENAI_BASE_URL="+openaiBaseURL,
		"OPENAI_API_BASE="+openaiBaseURL,
		"OPENAI_API_KEY="+openaiLoopbackAPIKey,
		"AGENTPAAS_LOOPBACK_PIN=1",
		"AGENTPAAS_EGRESS_DENY=1",
	)
	return out
}
