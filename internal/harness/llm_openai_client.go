package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

// openaiChatCompletionsBaseURL converts a chat-completions endpoint into the
// openai-go BaseURL. Completions.New joins "chat/completions" onto this
// directory, so a full .../v1/chat/completions URL must be stripped to .../v1/.
func openaiChatCompletionsBaseURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse chat completions endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("chat completions endpoint must be absolute")
	}
	path := strings.TrimSuffix(u.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	if path == "" {
		path = "/"
	} else if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// preserveHostRoundTripper sets req.Host after openai-go's origin check so the
// gateway can match routes by the original provider host (BUG-033/034). Host
// must not be set on the Request before Client.Do — the SDK rejects a Host
// that differs from the BaseURL origin.
type preserveHostRoundTripper struct {
	host string
	next http.RoundTripper
}

func (t preserveHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	if t.host == "" {
		return next.RoundTrip(req)
	}
	cloned := req.Clone(req.Context())
	cloned.Host = t.host
	return next.RoundTrip(cloned)
}

// openaiErrorShapeRoundTripper rewrites non-2xx bodies whose "error" field is
// a JSON string. openai-go unmarshals error into apierror.Error (an object).
// OpenRouter and the credential gateway often send `"error": "…"`. 0.4's
// hand-rolled reader accepted that; Completions.New does not.
type openaiErrorShapeRoundTripper struct {
	next http.RoundTripper
}

func (t openaiErrorShapeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	resp, err := next.RoundTrip(req)
	if err != nil || resp == nil || resp.StatusCode < 400 || resp.Body == nil {
		return resp, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	rewritten := rewriteOpenAIErrorJSON(body)
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	return resp, nil
}

func rewriteOpenAIErrorJSON(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return body
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(trimmed, &s) == nil && s != "" {
			return marshalOpenAIErrorObject(s)
		}
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return marshalOpenAIErrorObject(string(trimmed))
	}
	errVal, ok := obj["error"]
	if !ok {
		return body
	}
	if s, ok := errVal.(string); ok {
		obj["error"] = map[string]any{"message": s, "type": "api_error"}
		out, err := json.Marshal(obj)
		if err != nil {
			return body
		}
		return out
	}
	return body
}

func marshalOpenAIErrorObject(msg string) []byte {
	out, err := json.Marshal(map[string]any{
		"error": map[string]any{"message": msg, "type": "api_error"},
	})
	if err != nil {
		return []byte(`{"error":{"message":"llm provider error","type":"api_error"}}`)
	}
	return out
}

// newLLMChatClient constructs an openai-go client aimed at an injectable
// BaseURL (gateway rewrite or M16 127.0.0.1 loopback). Dummy API keys are
// accepted — the gateway injects the real credential. Redirects are not
// followed (BUG-033/034). Retries are disabled so context.WithTimeout is the
// sole deadline owner. requestTimeout kills the socket even when
// Completions.New ignores ctx (Gemini SSE keepalives).
func newLLMChatClient(baseURL, originalHost, apiKey string, requestTimeout time.Duration) openai.Client {
	if apiKey == "" {
		apiKey = "dummy"
	}
	httpClient := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: openaiErrorShapeRoundTripper{
			next: preserveHostRoundTripper{host: originalHost},
		},
	}
	return openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	)
}

func requestTimeoutFromCtx(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	d := time.Until(dl)
	if d < 0 {
		return 0
	}
	return d
}

func chatCompletionText(msg openai.ChatCompletionMessage) string {
	if msg.Content != "" {
		return msg.Content
	}
	return messageReasoning(msg)
}

func messageReasoning(msg openai.ChatCompletionMessage) string {
	raw := msg.RawJSON()
	if raw == "" {
		return ""
	}
	var parsed struct {
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return parsed.Reasoning
}

func openRouterReasoningExclude(originalHost, provider string) bool {
	if strings.EqualFold(strings.TrimSpace(provider), "openrouter") {
		return true
	}
	host, _, _ := strings.Cut(originalHost, ":")
	return strings.EqualFold(host, "openrouter.ai")
}

func isSSEContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "text/event-stream")
	}
	return strings.EqualFold(mediaType, "text/event-stream")
}

func looksLikeSSE(prefix []byte) bool {
	s := strings.TrimLeft(string(prefix), "\r\n")
	return strings.HasPrefix(s, "data:") || strings.HasPrefix(s, ":") || strings.HasPrefix(s, "event:")
}

type prefixReadCloser struct {
	io.Reader
	io.Closer
}

func readChatCompletionResponse(resp *http.Response) (*llm.LLMResult, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("llm chat completion: empty response")
	}
	if isSSEContentType(resp.Header.Get("Content-Type")) {
		return readStreamingChatCompletion(resp)
	}
	br := bufio.NewReader(resp.Body)
	prefix, peekErr := br.Peek(6)
	resp.Body = prefixReadCloser{Reader: br, Closer: resp.Body}
	if peekErr != nil && peekErr != io.EOF && peekErr != bufio.ErrBufferFull {
		_ = resp.Body.Close()
		return nil, peekErr
	}
	if looksLikeSSE(prefix) {
		return readStreamingChatCompletion(resp)
	}
	defer func() { _ = resp.Body.Close() }()
	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var completion openai.ChatCompletion
	if err := json.Unmarshal(contents, &completion); err != nil {
		return nil, err
	}
	return llmResultFromCompletion(&completion)
}

func llmResultFromCompletion(completion *openai.ChatCompletion) (*llm.LLMResult, error) {
	if completion == nil {
		return nil, errors.New("llm chat completion: empty response")
	}
	text := ""
	if len(completion.Choices) > 0 {
		text = chatCompletionText(completion.Choices[0].Message)
	}
	return &llm.LLMResult{
		Text:         text,
		Tokens:       completion.Usage.TotalTokens,
		Model:        completion.Model,
		InputTokens:  completion.Usage.PromptTokens,
		OutputTokens: completion.Usage.CompletionTokens,
	}, nil
}

// sseChunkText reads delta.content and message.content from an SSE chunk.
// Gemini/OpenRouter may emit a completed message object instead of deltas.
func sseChunkText(chunk openai.ChatCompletionChunk) (deltaContent, messageContent string) {
	if len(chunk.Choices) == 0 {
		return "", ""
	}
	ch := chunk.Choices[0]
	deltaContent = ch.Delta.Content
	raw := ch.RawJSON()
	if raw == "" {
		raw = chunk.RawJSON()
	}
	if raw == "" {
		return deltaContent, ""
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return deltaContent, ""
	}
	if parsed.Message.Content != "" {
		return deltaContent, parsed.Message.Content
	}
	if len(parsed.Choices) > 0 && parsed.Choices[0].Message.Content != "" {
		return deltaContent, parsed.Choices[0].Message.Content
	}
	return deltaContent, ""
}

// readStreamingChatCompletion consumes an SSE body with openai-go's
// NewStreaming decoder (ssestream.Next) and returns message.content.
func readStreamingChatCompletion(resp *http.Response) (*llm.LLMResult, error) {
	stream := ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(resp), nil)
	defer func() { _ = stream.Close() }()
	var delta strings.Builder
	var messageContent string
	var model string
	var usage openai.CompletionUsage
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage.TotalTokens != 0 {
			usage = chunk.Usage
		}
		d, msg := sseChunkText(chunk)
		if msg != "" {
			messageContent = msg
		}
		if d != "" {
			delta.WriteString(d)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	text := messageContent
	if text == "" {
		text = delta.String()
	}
	return &llm.LLMResult{
		Text:         text,
		Tokens:       usage.TotalTokens,
		Model:        model,
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}, nil
}

func callLLMChatCompletion(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
	timeout := requestTimeoutFromCtx(ctx)
	client := newLLMChatClient(baseURL, originalHost, apiKey, timeout)
	params := openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	}
	if maxTokens > 0 {
		params.MaxTokens = openai.Int(int64(maxTokens))
	}
	opts := []option.RequestOption{option.WithJSONSet("stream", false)}
	if timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(timeout))
	}
	if openRouterReasoningExclude(originalHost, provider) {
		opts = append(opts, option.WithJSONSet("reasoning", map[string]any{"exclude": true}))
	}
	// Skip Completions.New JSON decode so a text/event-stream body cannot hang
	// the decoder. JSON is decoded below; SSE uses NewStreaming's ssestream.
	var raw *http.Response
	opts = append(opts, option.WithResponseBodyInto(&raw))
	start := time.Now()
	deadlineMs := int64(-1)
	if dl, ok := ctx.Deadline(); ok {
		deadlineMs = time.Until(dl).Milliseconds()
	}
	log.Printf("harness: llm Completions.New start model=%s deadline_ms=%d rss_bytes=%d stream=false",
		model, deadlineMs, optionalRSSBytes())
	_, err := client.Chat.Completions.New(ctx, params, opts...)
	if err != nil {
		log.Printf("harness: llm Completions.New returned err=%t elapsed=%s", true, time.Since(start).Round(time.Millisecond))
		return nil, err
	}
	result, rerr := readChatCompletionResponse(raw)
	log.Printf("harness: llm Completions.New returned err=%t elapsed=%s", rerr != nil, time.Since(start).Round(time.Millisecond))
	return result, rerr
}

func optionalRSSBytes() int64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return int64(ms.Sys)
}

func llmHTTPStatusFromError(err error) string {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return strconv.Itoa(apiErr.StatusCode)
	}
	return ""
}
