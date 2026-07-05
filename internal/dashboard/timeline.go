package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/logging"
	"github.com/AgentPaaS-ai/agentpaas/internal/otel"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

const (
	timelineSSEContentType  = "text/event-stream"
	defaultMaxSpansInMemory = 500
	defaultSpanBatchSize    = 100
	maxTimelineRunIDLength  = 256
)

var timelineHeartbeatInterval = 15 * time.Second

// TimelineHandler serves live run timeline events via SSE.
// It proxies the Block 9 EventBus SSE stream and enriches it with OTel span data.
type TimelineHandler struct {
	bus   *trigger.EventBus
	store *otel.Store
	mu    sync.RWMutex
	// maxSpansInMemory limits how many spans are held in memory before virtualization kicks in
	maxSpansInMemory int
}

// NewTimelineHandler creates a timeline handler.
func NewTimelineHandler(bus *trigger.EventBus, store *otel.Store) *TimelineHandler {
	return &TimelineHandler{
		bus:              bus,
		store:            store,
		maxSpansInMemory: defaultMaxSpansInMemory,
	}
}

// ServeSSE handles GET /api/runs/:runID/timeline.
func (h *TimelineHandler) ServeSSE(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(r.PathValue("runID"))
	if runID == "" {
		runID = runIDFromTimelinePath(r.URL.Path)
	}
	if !validTimelineRunID(runID) {
		writeJSONError(w, http.StatusBadRequest, "invalid run id")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	fromEventID := parseLastEventID(r.Header.Get("Last-Event-ID"))
	var ch <-chan trigger.RunEvent
	if h.bus != nil {
		var cancel func()
		ch, cancel = h.bus.Subscribe(runID, fromEventID)
		defer cancel()
	}

	w.Header().Set("Content-Type", timelineSSEContentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if err := h.writeExistingSpans(r.Context(), w, flusher, runID); err != nil {
		return
	}
	if h.bus == nil {
		return
	}

	ticker := time.NewTicker(timelineHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			timeline, err := runEventToTimeline(event)
			if err != nil {
				return
			}
			if err := writeTimelineEvent(w, flusher, strconv.FormatInt(event.EventID, 10), timeline.Type, timeline); err != nil {
				return
			}
			if event.IsTerminal() {
				return
			}
		case <-ticker.C:
			if err := writeTimelineHeartbeat(w, flusher); err != nil {
				return
			}
		}
	}
}

// TimelineEvent is a single event in the timeline SSE stream.
type TimelineEvent struct {
	Type      string          `json:"type"` // llm_call, mcp_call, egress_allowed, egress_denied, budget, audit, run_event
	RunID     string          `json:"run_id"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"` // type-specific payload
}

// LLMTimelineRow represents an LLM call in the timeline.
type LLMTimelineRow struct {
	SpanID            string        `json:"span_id"`
	Model             string        `json:"model"`
	Provider          string        `json:"provider"`
	InputTokens       int           `json:"input_tokens"`
	OutputTokens      int           `json:"output_tokens"`
	InputCost         float64       `json:"input_cost,omitempty"`
	OutputCost        float64       `json:"output_cost,omitempty"`
	TotalCost         float64       `json:"total_cost,omitempty"`
	Cost              float64       `json:"cost"`
	PriceTableVersion string        `json:"price_table_version,omitempty"`
	Estimated         bool          `json:"estimated"`
	Latency           time.Duration `json:"latency"`
	Status            string        `json:"status"`
}

// MCPTimelineRow represents an MCP tool call in the timeline.
type MCPTimelineRow struct {
	SpanID   string        `json:"span_id"`
	Server   string        `json:"server"`
	Tool     string        `json:"tool"`
	Duration time.Duration `json:"duration"`
	Status   string        `json:"status"` // success, denied, error
}

// EgressTimelineRow represents an egress request in the timeline.
type EgressTimelineRow struct {
	SpanID      string        `json:"span_id"`
	Destination string        `json:"destination"`
	Method      string        `json:"method"`
	StatusCode  int           `json:"status_code"`
	Allowed     bool          `json:"allowed"`
	DenyReason  string        `json:"deny_reason,omitempty"`
	Duration    time.Duration `json:"duration"`
}

// BudgetTimelineRow represents a budget marker in the timeline.
type BudgetTimelineRow struct {
	Type     string  `json:"type"` // token_limit, wall_clock, iteration_limit
	Current  float64 `json:"current"`
	Limit    float64 `json:"limit"`
	Exceeded bool    `json:"exceeded"`
}

// AuditTimelineRow represents an audit marker in the timeline.
type AuditTimelineRow struct {
	EventType string `json:"event_type"`
	Seq       int64  `json:"seq"`
	Actor     string `json:"actor"`
}

func (h *TimelineHandler) writeExistingSpans(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, runID string) error {
	if h.store == nil {
		return nil
	}
	spans, err := h.store.QuerySpans(ctx, runID, 0)
	if err != nil {
		return err
	}
	h.mu.RLock()
	maxSpans := h.maxSpansInMemory
	h.mu.RUnlock()
	if maxSpans <= 0 {
		maxSpans = defaultMaxSpansInMemory
	}
	if len(spans) > maxSpans {
		batcher := NewSpanBatcher(defaultSpanBatchSize, 0)
		for _, span := range spans {
			event, err := spanToTimelineEvent(runID, span)
			if err != nil {
				return err
			}
			if batcher.Add(event) {
				if err := writeTimelineBatch(w, flusher, batcher.Flush()); err != nil {
					return err
				}
			}
		}
		if batcher.Pending() > 0 {
			return writeTimelineBatch(w, flusher, batcher.Flush())
		}
		return nil
	}
	for _, span := range spans {
		event, err := spanToTimelineEvent(runID, span)
		if err != nil {
			return err
		}
		if err := writeTimelineEvent(w, flusher, "", event.Type, event); err != nil {
			return err
		}
	}
	return nil
}

func runEventToTimeline(event trigger.RunEvent) (TimelineEvent, error) {
	payload := struct {
		EventID   int64             `json:"event_id"`
		RunID     string            `json:"run_id"`
		Type      trigger.EventType `json:"type"`
		Timestamp time.Time         `json:"timestamp"`
		Data      any               `json:"data,omitempty"`
	}{
		EventID:   event.EventID,
		RunID:     redactedString(event.RunID),
		Type:      trigger.EventType(redactedString(string(event.Type))),
		Timestamp: event.Timestamp,
		Data:      redactTimelineValue(event.Data),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return TimelineEvent{}, err
	}
	return TimelineEvent{
		Type:      "run_event",
		RunID:     redactedString(event.RunID),
		Timestamp: event.Timestamp,
		Data:      data,
	}, nil
}

func spanToTimelineEvent(runID string, span otel.SpanRecord) (TimelineEvent, error) {
	attrs := decodeTimelineAttrs(span.Attributes)
	rowType := classifySpan(span, attrs)
	var row any
	switch rowType {
	case "llm_call":
		llmRow := LLMTimelineRow{
			SpanID:       redactedString(span.SpanID),
			Model:        redactedAttrString(attrs, "llm.model", "gen_ai.request.model"),
			Provider:     redactedAttrString(attrs, "llm.provider", "gen_ai.system"),
			InputTokens:  attrInt(attrs, "llm.input_tokens", "gen_ai.usage.input_tokens"),
			OutputTokens: attrInt(attrs, "llm.output_tokens", "gen_ai.usage.output_tokens"),
			Cost:         attrFloat(attrs, "llm.cost", "gen_ai.usage.cost"),
			Latency:      span.EndTime.Sub(span.StartTime),
			Status:       spanStatus(span),
		}
		if inputs, ok := costInputsFromAttributes(span.Attributes); ok {
			cost := ComputeCost(inputs.provider, inputs.model, inputs.inputTokens, inputs.outputTokens)
			llmRow.InputCost = cost.InputCost
			llmRow.OutputCost = cost.OutputCost
			llmRow.TotalCost = cost.TotalCost
			llmRow.Cost = cost.TotalCost
			llmRow.PriceTableVersion = cost.PriceTableVersion
			llmRow.Estimated = cost.Estimated
		}
		row = llmRow
	case "mcp_call":
		row = MCPTimelineRow{
			SpanID:   redactedString(span.SpanID),
			Server:   redactedAttrString(attrs, "mcp.server", "mcp.server_name"),
			Tool:     redactedAttrString(attrs, "mcp.tool", "mcp.tool_name"),
			Duration: span.EndTime.Sub(span.StartTime),
			Status:   spanStatus(span),
		}
	case "egress_allowed", "egress_denied":
		allowed := rowType == "egress_allowed"
		row = EgressTimelineRow{
			SpanID:      redactedString(span.SpanID),
			Destination: redactedAttrString(attrs, "egress.destination", "url.full", "http.url", "server.address", "net.peer.name"),
			Method:      redactedAttrString(attrs, "http.method", "http.request.method"),
			StatusCode:  attrInt(attrs, "http.status_code", "http.response.status_code"),
			Allowed:     allowed,
			DenyReason:  redactedAttrString(attrs, "egress.deny_reason", "agentpaas.egress.deny_reason"),
			Duration:    span.EndTime.Sub(span.StartTime),
		}
	case "budget":
		row = BudgetTimelineRow{
			Type:     redactedAttrString(attrs, "budget.type", "agentpaas.budget.type"),
			Current:  attrFloat(attrs, "budget.current", "agentpaas.budget.current"),
			Limit:    attrFloat(attrs, "budget.limit", "agentpaas.budget.limit"),
			Exceeded: attrBool(attrs, "budget.exceeded", "agentpaas.budget.exceeded"),
		}
	case "audit":
		row = AuditTimelineRow{
			EventType: redactedAttrString(attrs, "audit.event_type", "agentpaas.audit.event_type"),
			Seq:       int64(attrInt(attrs, "audit.seq", "agentpaas.audit.seq")),
			Actor:     redactedAttrString(attrs, "audit.actor", "agentpaas.audit.actor"),
		}
	default:
		rowType = "run_event"
		row = redactedSpanRecord(span)
	}
	data, err := json.Marshal(redactTimelineValue(row))
	if err != nil {
		return TimelineEvent{}, err
	}
	return TimelineEvent{
		Type:      rowType,
		RunID:     redactedString(runID),
		Timestamp: span.StartTime,
		Data:      data,
	}, nil
}

func classifySpan(span otel.SpanRecord, attrs map[string]any) string {
	name := strings.ToLower(span.Name)
	switch {
	case hasAnyAttr(attrs, "llm.model", "llm.provider", "gen_ai.request.model", "gen_ai.system") || strings.Contains(name, "llm"):
		return "llm_call"
	case hasAnyAttr(attrs, "mcp.server", "mcp.tool", "mcp.server_name", "mcp.tool_name") || strings.Contains(name, "mcp"):
		return "mcp_call"
	case hasAnyAttr(attrs, "budget.type", "agentpaas.budget.type") || strings.Contains(name, "budget"):
		return "budget"
	case hasAnyAttr(attrs, "audit.event_type", "agentpaas.audit.event_type") || strings.Contains(name, "audit"):
		return "audit"
	case hasAnyAttr(attrs, "egress.allowed", "agentpaas.egress.allowed") || strings.Contains(name, "egress"):
		if attrBool(attrs, "egress.allowed", "agentpaas.egress.allowed") {
			return "egress_allowed"
		}
		return "egress_denied"
	default:
		return "run_event"
	}
}

func writeTimelineEvent(w http.ResponseWriter, flusher http.Flusher, id string, eventType string, event TimelineEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeTimelineBatch(w http.ResponseWriter, flusher http.Flusher, events []TimelineEvent) error {
	if len(events) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string][]TimelineEvent{"events": events})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: span_batch\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeTimelineHeartbeat(w http.ResponseWriter, flusher http.Flusher) error {
	data, err := json.Marshal(TimelineEvent{
		Type:      "heartbeat",
		Timestamp: time.Now().UTC(),
		Data:      json.RawMessage(`{}`),
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func decodeTimelineAttrs(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return map[string]any{}
	}
	return attrs
}

func hasAnyAttr(attrs map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := attrs[key]; ok {
			return true
		}
	}
	return false
}

func redactedAttrString(attrs map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attrs[key]; ok {
			return redactedString(fmt.Sprint(value))
		}
	}
	return ""
}

func attrInt(attrs map[string]any, keys ...string) int {
	for _, key := range keys {
		switch value := attrs[key].(type) {
		case float64:
			return int(value)
		case int:
			return value
		case int64:
			return int(value)
		case json.Number:
			i, _ := value.Int64()
			return int(i)
		case string:
			i, _ := strconv.Atoi(value)
			return i
		}
	}
	return 0
}

func attrFloat(attrs map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch value := attrs[key].(type) {
		case float64:
			return value
		case int:
			return float64(value)
		case int64:
			return float64(value)
		case json.Number:
			f, _ := value.Float64()
			return f
		case string:
			f, _ := strconv.ParseFloat(value, 64)
			return f
		}
	}
	return 0
}

func attrBool(attrs map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch value := attrs[key].(type) {
		case bool:
			return value
		case string:
			parsed, _ := strconv.ParseBool(value)
			return parsed
		}
	}
	return false
}

func spanStatus(span otel.SpanRecord) string {
	status := span.StatusCode
	if status == "" || strings.EqualFold(status, "unset") {
		status = span.Status
	}
	if status == "" {
		return "success"
	}
	return redactedString(strings.ToLower(status))
}

func parseLastEventID(raw string) int64 {
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func timelinePathValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if invalidTimelineRequestPath(r.URL.Path) || invalidTimelineRequestPath(r.URL.EscapedPath()) {
			writeJSONError(w, http.StatusBadRequest, "invalid run id")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func invalidTimelineRequestPath(rawPath string) bool {
	runID := runIDFromTimelinePath(rawPath)
	if runID != "" {
		return !validTimelineRunID(runID)
	}
	if strings.HasPrefix(rawPath, "/api/runs/") && strings.HasSuffix(rawPath, "/timeline") {
		return true
	}
	for _, endpoint := range []string{"logs", "spans", "artifacts"} {
		runID = runIDFromRunAPIPath(rawPath, endpoint)
		if runID != "" {
			return !validTimelineRunID(runID)
		}
		if strings.HasPrefix(rawPath, "/api/runs/") && strings.HasSuffix(rawPath, "/"+endpoint) {
			return true
		}
	}
	return false
}

func runIDFromTimelinePath(rawPath string) string {
	return runIDFromRunAPIPath(rawPath, "timeline")
}

func runIDFromRunAPIPath(rawPath string, endpoint string) string {
	const prefix = "/api/runs/"
	suffix := "/" + endpoint
	if !strings.HasPrefix(rawPath, prefix) || !strings.HasSuffix(rawPath, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(rawPath, prefix), suffix)
}

func validTimelineRunID(runID string) bool {
	if runID == "" || len(runID) > maxTimelineRunIDLength {
		return false
	}
	if strings.ContainsAny(runID, `/\`) || strings.Contains(runID, "..") {
		return false
	}
	lower := strings.ToLower(runID)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return false
	}
	if path.Clean(runID) != runID {
		return false
	}
	for _, r := range runID {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func redactedString(s string) string {
	return logging.Redact(s)
}

func redactedSpanRecord(span otel.SpanRecord) map[string]any {
	return map[string]any{
		"id":             span.ID,
		"trace_id":       redactedString(span.TraceID),
		"span_id":        redactedString(span.SpanID),
		"parent_span_id": redactedString(span.ParentSpanID),
		"name":           redactedString(span.Name),
		"kind":           redactedString(span.Kind),
		"start_time":     span.StartTime,
		"end_time":       span.EndTime,
		"attributes":     redactedString(span.Attributes),
		"status":         redactedString(span.Status),
		"status_code":    redactedString(span.StatusCode),
		"resource":       redactedString(span.Resource),
		"scope":          redactedString(span.Scope),
	}
}

func redactTimelineValue(value any) any {
	switch v := value.(type) {
	case string:
		return redactedString(v)
	case map[string]string:
		redacted := make(map[string]string, len(v))
		for key, item := range v {
			redacted[key] = redactedString(item)
		}
		return redacted
	case map[string]any:
		redacted := make(map[string]any, len(v))
		for key, item := range v {
			redacted[key] = redactTimelineValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(v))
		for i, item := range v {
			redacted[i] = redactTimelineValue(item)
		}
		return redacted
	default:
		return v
	}
}
