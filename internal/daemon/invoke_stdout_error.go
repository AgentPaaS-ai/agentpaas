package daemon

import (
	"encoding/json"
	"strings"
)

// invokeStdoutIndicatesError reports whether harness stdout is a completed
// invoke that still represents failure (HTTP 200 / exec exit 0 with ERROR).
func invokeStdoutIndicatesError(stdout string) bool {
	if stdout == "" {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		return false
	}
	if errObj, ok := m["error"]; ok {
		if _, isMap := errObj.(map[string]any); isMap {
			return true
		}
	}
	result, ok := m["result"].(map[string]any)
	if !ok {
		return false
	}
	if statusIsError(result["status"]) {
		return true
	}
	nested, ok := result["result"].(map[string]any)
	if !ok {
		return false
	}
	return statusIsError(nested["status"])
}

func statusIsError(v any) bool {
	s, ok := v.(string)
	return ok && strings.EqualFold(s, "ERROR")
}

// invokeStdoutErrorReason extracts a short failReason from invoke stdout.
// Default is "invoke_result_error" when JSON has no usable error field.
func invokeStdoutErrorReason(stdout string) string {
	const fallback = "invoke_result_error"
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		return fallback
	}
	if s := errorFieldString(m["error"]); s != "" {
		return s
	}
	result, _ := m["result"].(map[string]any)
	if result == nil {
		return fallback
	}
	if s := errorFieldString(result["error"]); s != "" {
		return s
	}
	if nested, ok := result["result"].(map[string]any); ok {
		if s := errorFieldString(nested["error"]); s != "" {
			return s
		}
	}
	return fallback
}

func errorFieldString(v any) string {
	switch x := v.(type) {
	case string:
		return shortenFailReason(x)
	case map[string]any:
		msg, _ := x["message"].(string)
		return shortenFailReason(msg)
	default:
		return ""
	}
}

func shortenFailReason(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
