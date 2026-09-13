package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxLLMResponseBytes = 1024 * 1024

// readLLMHTTPBody reads an LLM HTTP response. Chat-completions providers
// sometimes return text/event-stream (or a body that starts with data:) even
// when the client requested stream:false. ReadAll on a held-open SSE body
// never returns; parse until [DONE] instead.
func readLLMHTTPBody(body io.Reader, contentType string) ([]byte, error) {
	br := bufio.NewReader(io.LimitReader(body, maxLLMResponseBytes))
	if looksLikeChatCompletionSSE(br, contentType) {
		return readChatCompletionSSE(br)
	}
	return io.ReadAll(br)
}

func looksLikeChatCompletionSSE(br *bufio.Reader, contentType string) bool {
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return true
	}
	peek, _ := br.Peek(16)
	trimmed := bytes.TrimLeft(peek, "\r\n\t ")
	return bytes.HasPrefix(trimmed, []byte("data:"))
}

func readChatCompletionSSE(r *bufio.Reader) ([]byte, error) {
	var text strings.Builder
	var model string
	var usage any
	sawDone := false
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "" || strings.HasPrefix(trimmed, ":"):
				// keepalive / blank
			case strings.HasPrefix(trimmed, "data:"):
				payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if payload == "[DONE]" {
					sawDone = true
					return marshalSSEResult(text.String(), model, usage)
				}
				model, usage = accumulateSSEChunk(payload, &text, model, usage)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	if !sawDone && text.Len() == 0 {
		return nil, fmt.Errorf("llm sse response ended without content or [DONE]")
	}
	return marshalSSEResult(text.String(), model, usage)
}

func accumulateSSEChunk(payload string, text *strings.Builder, model string, usage any) (string, any) {
	var chunk map[string]any
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return model, usage
	}
	if m, ok := chunk["model"].(string); ok && m != "" {
		model = m
	}
	if u, ok := chunk["usage"]; ok && u != nil {
		usage = u
	}
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return model, usage
	}
	c0, _ := choices[0].(map[string]any)
	if c0 == nil {
		return model, usage
	}
	if msg, ok := c0["message"].(map[string]any); ok {
		if content, ok := msg["content"].(string); ok && content != "" {
			text.Reset()
			text.WriteString(content)
		}
	}
	if delta, ok := c0["delta"].(map[string]any); ok {
		if content, ok := delta["content"].(string); ok {
			text.WriteString(content)
		}
	}
	return model, usage
}

func marshalSSEResult(text, model string, usage any) ([]byte, error) {
	out := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": text},
			},
		},
		"model": model,
	}
	if usage != nil {
		out["usage"] = usage
	}
	return json.Marshal(out)
}
