package otel

import (
	"encoding/json"

	"github.com/parvezsyed/agentpaas/internal/logging"
)

// redactString applies the platform redaction to a string value.
func redactString(s string) string {
	return logging.Redact(s)
}

// redactJSON marshals v to JSON, redacts the JSON string, and returns it.
func redactJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return redactString(string(data)), nil
}
