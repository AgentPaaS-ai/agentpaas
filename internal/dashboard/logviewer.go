package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/AgentPaaS-ai/agentpaas/internal/logging"
	"github.com/AgentPaaS-ai/agentpaas/internal/otel"
)

// maxLogBodyLen is the maximum length of a log body before truncation (10KB).
const maxLogBodyLen = 10 * 1024

// maxAttributeValueLen is the maximum length of an attribute value (4KB).
const maxAttributeValueLen = 4 * 1024

// LogViewerHandler serves log data from the OTel store for the dashboard.
type LogViewerHandler struct {
	store            *otel.Store
	artifactProvider DockerArtifactProvider
}

// DockerArtifactProvider provides Docker artifact metadata for the dashboard.
type DockerArtifactProvider interface {
	ListDockerArtifacts(ctx context.Context, runID string) ([]DockerArtifact, error)
}

// DockerArtifact is Docker metadata associated with a run.
type DockerArtifact struct {
	ContainerID string
	ImageDigest string
	Labels      map[string]string
	Network     string
	Health      string
	Exists      bool
}

// DockerArtifactView is a sanitized Docker artifact API response.
type DockerArtifactView struct {
	ContainerID string            `json:"container_id"`
	ImageDigest string            `json:"image_digest"`
	Labels      map[string]string `json:"labels,omitempty"`
	Network     string            `json:"network"`
	Health      string            `json:"health"`
	State       string            `json:"state"`
}

// LogViewEntry is a single log entry in the API response.
type LogViewEntry struct {
	Timestamp  string            `json:"timestamp"`
	Severity   string            `json:"severity"`
	Body       string            `json:"body"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Resource   map[string]string `json:"resource,omitempty"`
	Truncated  bool              `json:"truncated,omitempty"`
}

// SpanViewEntry is a single sanitized span in the API response.
type SpanViewEntry struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	StartTime    string            `json:"start_time"`
	EndTime      string            `json:"end_time"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Status       string            `json:"status"`
	StatusCode   string            `json:"status_code"`
	Resource     map[string]string `json:"resource,omitempty"`
	Scope        map[string]string `json:"scope,omitempty"`
}

// NewLogViewerHandler creates a log viewer handler.
func NewLogViewerHandler(store *otel.Store) *LogViewerHandler {
	return &LogViewerHandler{store: store}
}

// ServeLogs handles GET /api/runs/:runID/logs.
func (h *LogViewerHandler) ServeLogs(w http.ResponseWriter, r *http.Request) {
	runID, ok := logViewerRunID(w, r, "logs")
	if !ok {
		return
	}
	if h.store == nil {
		writeJSONError(w, http.StatusNotFound, "log viewer unavailable")
		return
	}
	records, err := h.store.QueryLogs(r.Context(), runID, 0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query logs failed")
		return
	}
	entries := make([]LogViewEntry, 0, len(records))
	for _, record := range records {
		body, truncated := sanitizeStringWithTruncated(record.Body, maxLogBodyLen)
		entries = append(entries, LogViewEntry{
			Timestamp:  sanitizeString(record.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), maxAttributeValueLen),
			Severity:   sanitizeString(record.Severity, maxAttributeValueLen),
			Body:       body,
			Attributes: sanitizeJSONMap(record.Attributes, maxAttributeValueLen),
			Resource:   sanitizeJSONMap(record.Resource, maxAttributeValueLen),
			Truncated:  truncated,
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

// ServeSpans handles GET /api/runs/:runID/spans.
func (h *LogViewerHandler) ServeSpans(w http.ResponseWriter, r *http.Request) {
	runID, ok := logViewerRunID(w, r, "spans")
	if !ok {
		return
	}
	if h.store == nil {
		writeJSONError(w, http.StatusNotFound, "log viewer unavailable")
		return
	}
	records, err := h.store.QuerySpans(r.Context(), runID, 0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query spans failed")
		return
	}
	entries := make([]SpanViewEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, SpanViewEntry{
			TraceID:      sanitizeString(record.TraceID, maxAttributeValueLen),
			SpanID:       sanitizeString(record.SpanID, maxAttributeValueLen),
			ParentSpanID: sanitizeString(record.ParentSpanID, maxAttributeValueLen),
			Name:         sanitizeString(record.Name, maxAttributeValueLen),
			Kind:         sanitizeString(record.Kind, maxAttributeValueLen),
			StartTime:    sanitizeString(record.StartTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), maxAttributeValueLen),
			EndTime:      sanitizeString(record.EndTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), maxAttributeValueLen),
			Attributes:   sanitizeJSONMap(record.Attributes, maxAttributeValueLen),
			Status:       sanitizeString(record.Status, maxAttributeValueLen),
			StatusCode:   sanitizeString(record.StatusCode, maxAttributeValueLen),
			Resource:     sanitizeJSONMap(record.Resource, maxAttributeValueLen),
			Scope:        sanitizeJSONMap(record.Scope, maxAttributeValueLen),
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

// ServeDockerArtifacts handles GET /api/runs/:runID/artifacts.
func (h *LogViewerHandler) ServeDockerArtifacts(w http.ResponseWriter, r *http.Request) {
	runID, ok := logViewerRunID(w, r, "artifacts")
	if !ok {
		return
	}
	if h.artifactProvider == nil {
		writeJSON(w, http.StatusOK, []DockerArtifactView{})
		return
	}
	artifacts, err := h.artifactProvider.ListDockerArtifacts(r.Context(), runID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query artifacts failed")
		return
	}
	views := make([]DockerArtifactView, 0, len(artifacts))
	for _, artifact := range artifacts {
		state := "present"
		health := artifact.Health
		if !artifact.Exists {
			state = "reconciled"
			health = "reconciled"
		}
		views = append(views, DockerArtifactView{
			ContainerID: sanitizeString(artifact.ContainerID, maxAttributeValueLen),
			ImageDigest: sanitizeString(artifact.ImageDigest, maxAttributeValueLen),
			Labels:      sanitizeMap(artifact.Labels, maxAttributeValueLen),
			Network:     sanitizeString(artifact.Network, maxAttributeValueLen),
			Health:      sanitizeString(health, maxAttributeValueLen),
			State:       sanitizeString(state, maxAttributeValueLen),
		})
	}
	writeJSON(w, http.StatusOK, views)
}

// sanitizeString applies the full pipeline: redact, escape control chars, ensure valid UTF-8, HTML-escape, truncate.
func sanitizeString(input string, maxLen int) string {
	s, _ := sanitizeStringWithTruncated(input, maxLen) // truncation flag unused
	return s
}

func sanitizeStringWithTruncated(input string, maxLen int) (string, bool) {
	s := logging.Redact(input)
	s = escapeControlChars(s)
	s = validUTF8(s)
	s = html.EscapeString(s)
	if maxLen >= 0 && len(s) > maxLen {
		return s[:maxLen] + "...", true
	}
	return s, false
}

// escapeControlChars replaces all control characters (except \n, \r, \t) with hex escapes.
func escapeControlChars(s string) string {
	var b strings.Builder
	for _, r := range s {
		if shouldEscapeControl(r) {
			fmt.Fprintf(&b, "\\x%02x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// validUTF8 removes invalid UTF-8 sequences, replacing with U+FFFD.
func validUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "\uFFFD")
}

// sanitizeMap applies sanitizeString to all keys and values in a map.
func sanitizeMap(m map[string]string, maxLen int) map[string]string {
	if len(m) == 0 {
		return nil
	}
	sanitized := make(map[string]string, len(m))
	for key, value := range m {
		sanitized[sanitizeString(key, maxLen)] = sanitizeString(value, maxLen)
	}
	return sanitized
}

func sanitizeJSONMap(raw string, maxLen int) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return map[string]string{"value": sanitizeString(raw, maxLen)}
	}
	if len(values) == 0 {
		return nil
	}
	sanitized := make(map[string]string, len(values))
	for key, value := range values {
		sanitized[sanitizeString(key, maxLen)] = sanitizeString(jsonValueString(value), maxLen)
	}
	return sanitized
}

func jsonValueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}

func shouldEscapeControl(r rune) bool {
	if r == '\n' || r == '\r' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

func logViewerRunID(w http.ResponseWriter, r *http.Request, endpoint string) (string, bool) {
	runID := strings.TrimSpace(r.PathValue("runID"))
	if runID == "" {
		runID = runIDFromRunAPIPath(r.URL.Path, endpoint)
	}
	if !validTimelineRunID(runID) {
		writeJSONError(w, http.StatusBadRequest, "invalid run id")
		return "", false
	}
	return runID, true
}
