package pipeline

import (
	"encoding/json"
	"strings"
)

const workOrderWalkMaxDepth = 8

var notesAliases = map[string]bool{
	"notes":            true,
	"note":             true,
	"supervisor_notes": true,
	"supervisornotes":  true,
	"private_notes":    true,
	"privatenotes":     true,
	"memory":           true,
	"conversation":     true,
}

var smashKeys = map[string]bool{
	"query":   true,
	"text":    true,
	"summary": true,
	"url":     true,
}

var envelopeMetaKeys = map[string]bool{
	"schema_version":          true,
	"handoff_id":              true,
	"workflow_id":             true,
	"from_node_id":            true,
	"to_node_id":              true,
	"source_node_id":          true,
	"target_node_id":          true,
	"producer_run_id":         true,
	"producer_attempt_id":     true,
	"producer_result_digest":  true,
	"sequence":                true,
	"created_at":              true,
	"classification":          true,
	"context":                 true,
	"context_json":            true,
	"artifacts":               true,
	"artifact_refs":           true,
}

func sanitizeWorkOrderKey(k string) string {
	return strings.ToLower(strings.TrimSpace(k))
}

func isNotesAlias(key string) bool {
	return notesAliases[sanitizeWorkOrderKey(key)]
}

func isSmashKey(key string) bool {
	return smashKeys[sanitizeWorkOrderKey(key)]
}

func isProtoKey(key string) bool {
	return key == "__proto__" || key == "constructor" || key == "prototype"
}

func isEnvelopeMetaKey(key string) bool {
	return envelopeMetaKeys[sanitizeWorkOrderKey(key)]
}

// ExtractWorkOrderJSON returns the child-visible work order from a prior
// stage ContextJSON. Notes, smash aliases, and parent conversation stay off
// the child. If ContextJSON has a work_order object, only that object is
// returned; otherwise the sanitized context object is used.
func ExtractWorkOrderJSON(contextJSON string) json.RawMessage {
	trimmed := strings.TrimSpace(contextJSON)
	if trimmed == "" {
		return nil
	}
	return WorkOrderFromIncoming(json.RawMessage(trimmed))
}

// WorkOrderFromIncoming extracts a work-order object from incoming handoff
// JSON. It accepts a stored context object, a full HandoffEnvelope, or an
// already-extracted work order. Envelope metadata, notes, and smash keys
// are never returned.
func WorkOrderFromIncoming(raw json.RawMessage) json.RawMessage {
	trimmed := bytesTrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil
	}

	source := workOrderSource(obj)
	if source == nil {
		return nil
	}
	cleaned := sanitizeCopiedRecord(source, 0)
	if len(cleaned) == 0 {
		return nil
	}
	out, err := json.Marshal(cleaned)
	if err != nil {
		return nil
	}
	return json.RawMessage(out)
}

func workOrderSource(obj map[string]any) map[string]any {
	if raw, ok := obj["context_json"]; ok {
		switch v := raw.(type) {
		case string:
			var inner map[string]any
			if err := json.Unmarshal([]byte(v), &inner); err == nil {
				return workOrderSource(inner)
			}
		case map[string]any:
			return workOrderSource(v)
		}
	}
	if ctx, ok := obj["context"].(map[string]any); ok {
		if val, ok := ctx["value"].(map[string]any); ok {
			return workOrderSource(val)
		}
	}
	if wo, ok := obj["work_order"].(map[string]any); ok {
		return wo
	}
	if looksLikeEnvelope(obj) {
		return nil
	}
	return obj
}

func looksLikeEnvelope(obj map[string]any) bool {
	_, hasHandoffID := obj["handoff_id"]
	_, hasWorkflowID := obj["workflow_id"]
	return hasHandoffID && hasWorkflowID
}

func sanitizeCopiedValue(value any, depth int) any {
	if depth > workOrderWalkMaxDepth {
		return nil
	}
	switch v := value.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			cleaned := sanitizeCopiedValue(item, depth+1)
			if cleaned != nil {
				out = append(out, cleaned)
			}
		}
		return out
	case map[string]any:
		return sanitizeCopiedRecord(v, depth)
	default:
		return value
	}
}

func sanitizeCopiedRecord(obj map[string]any, depth int) map[string]any {
	out := make(map[string]any)
	if depth > workOrderWalkMaxDepth {
		return out
	}
	for key, value := range obj {
		if isProtoKey(key) || isNotesAlias(key) || isSmashKey(key) || isEnvelopeMetaKey(key) {
			continue
		}
		if key == "work_order" {
			continue
		}
		cleaned := sanitizeCopiedValue(value, depth+1)
		if cleaned != nil {
			out[key] = cleaned
		}
	}
	return out
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}
