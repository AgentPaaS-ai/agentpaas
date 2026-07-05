package otel

import (
	"encoding/json"

	"github.com/AgentPaaS-ai/agentpaas/internal/logging"
)

// redactString applies the platform redaction to a string value.
func redactString(s string) string {
	return logging.Redact(s)
}

// redactJSON redacts string leaves before marshaling so injected values cannot
// bypass redaction by being embedded in JSON strings.
func redactJSON(v any) (string, error) {
	data, err := json.Marshal(redactValue(v))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func redactValue(v any) any {
	switch value := v.(type) {
	case string:
		return redactString(value)
	case map[string]any:
		redacted := make(map[string]any, len(value))
		for k, item := range value {
			redacted[k] = redactValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(value))
		for i, item := range value {
			redacted[i] = redactValue(item)
		}
		return redacted
	case []string:
		redacted := make([]string, len(value))
		for i, item := range value {
			redacted[i] = redactString(item)
		}
		return redacted
	default:
		return value
	}
}
