package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// CloudAuditEvent is an event or audit record returned by the cloud control
// plane. Payload, data, and metadata are intentionally structured maps so the
// client does not discard provider-specific fields.
type CloudAuditEvent struct {
	ID             string         `json:"id,omitempty"`
	RunID          string         `json:"run_id,omitempty"`
	Seq            int64          `json:"seq,omitempty"`
	PrevHash       string         `json:"prev_hash,omitempty"`
	RecordHash     string         `json:"record_hash,omitempty"`
	Timestamp      string         `json:"timestamp,omitempty"`
	CreatedAt      string         `json:"created_at,omitempty"`
	EventType      string         `json:"event_type,omitempty"`
	Type           string         `json:"type,omitempty"`
	Category       string         `json:"category,omitempty"`
	Action         string         `json:"action,omitempty"`
	Status         string         `json:"status,omitempty"`
	Actor          string         `json:"actor,omitempty"`
	DeploymentMode string         `json:"deployment_mode,omitempty"`
	Message        string         `json:"message,omitempty"`
	Detail         string         `json:"detail,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	HostedContext  map[string]any `json:"hosted_context,omitempty"`
}

// RunEvent and AuditEvent are descriptive aliases for callers that use the
// endpoint-specific terminology.
type RunEvent = CloudAuditEvent
type AuditEvent = CloudAuditEvent

// RunEventsResponse is the response from GET /v1/runs/:id/events.
type RunEventsResponse struct {
	RunID      string            `json:"run_id,omitempty"`
	Events     []CloudAuditEvent `json:"events,omitempty"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more,omitempty"`
	Raw        json.RawMessage   `json:"-"`
}

// AuditQuery contains the optional filters for GET /v1/audit.
type AuditQuery struct {
	Since string
	Until string
	Limit int
}

// AuditResponse is the response from GET /v1/audit.
type AuditResponse struct {
	Events     []CloudAuditEvent `json:"events,omitempty"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more,omitempty"`
	Raw        json.RawMessage   `json:"-"`
}

// AuditExportResponse is the response from GET /v1/runs/:id/audit/export.
// Raw retains the exact response body so JSON output does not lose fields that
// are added by the cloud API independently of this client.
type AuditExportResponse struct {
	RunID      string            `json:"run_id,omitempty"`
	Format     string            `json:"format,omitempty"`
	ExportedAt string            `json:"exported_at,omitempty"`
	Records    []CloudAuditEvent `json:"records,omitempty"`
	Events     []CloudAuditEvent `json:"events,omitempty"`
	Signature  string            `json:"signature,omitempty"`
	Raw        json.RawMessage   `json:"-"`
}

// AuditExport is an endpoint-specific alias for AuditExportResponse.
type AuditExport = AuditExportResponse

// MetricsResponse is the response from GET /v1/metrics. Values preserves
// metrics not yet modeled as named fields.
type MetricsResponse struct {
	RunsTotal         int             `json:"runs_total,omitempty"`
	RunsActive        int             `json:"runs_active,omitempty"`
	RunsSucceeded     int             `json:"runs_succeeded,omitempty"`
	RunsFailed        int             `json:"runs_failed,omitempty"`
	EventsTotal       int             `json:"events_total,omitempty"`
	AuditRecordsTotal int             `json:"audit_records_total,omitempty"`
	LatencyMSP50      float64         `json:"latency_ms_p50,omitempty"`
	LatencyMSP95      float64         `json:"latency_ms_p95,omitempty"`
	LatencyMSP99      float64         `json:"latency_ms_p99,omitempty"`
	Values            map[string]any  `json:"-"`
	Raw               json.RawMessage `json:"-"`
}

// CloudMetrics is an endpoint-specific alias for MetricsResponse.
type CloudMetrics = MetricsResponse

// GetRunEvents calls GET /v1/runs/:id/events with a Bearer token.
func (c *CloudClient) GetRunEvents(ctx context.Context, token, id string) (*RunEventsResponse, error) {
	if err := validateRunID("get run events", id); err != nil {
		return nil, err
	}
	body, err := c.authenticatedGet(ctx, token, "/v1/runs/"+id+"/events", "get run events")
	if err != nil {
		return nil, err
	}
	var result RunEventsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("get run events: decode response: %w", err)
	}
	return &result, nil
}

// ListRunEvents returns only the event list from GET /v1/runs/:id/events.
func (c *CloudClient) ListRunEvents(ctx context.Context, token, id string) ([]CloudAuditEvent, error) {
	result, err := c.GetRunEvents(ctx, token, id)
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}

// GetAudit calls GET /v1/audit with optional since, until, and limit filters.
func (c *CloudClient) GetAudit(ctx context.Context, token, since, until string, limit int) (*AuditResponse, error) {
	query := AuditQuery{Since: since, Until: until, Limit: limit}
	body, err := c.getAuditWithQuery(ctx, token, query)
	if err != nil {
		return nil, err
	}
	var result AuditResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("get audit: decode response: %w", err)
	}
	return &result, nil
}

// GetAuditWithQuery is the structured-query form of GetAudit.
func (c *CloudClient) GetAuditWithQuery(ctx context.Context, token string, query AuditQuery) (*AuditResponse, error) {
	return c.GetAudit(ctx, token, query.Since, query.Until, query.Limit)
}

// ListAudit returns only the event list from GET /v1/audit.
func (c *CloudClient) ListAudit(ctx context.Context, token string, query AuditQuery) ([]CloudAuditEvent, error) {
	result, err := c.GetAuditWithQuery(ctx, token, query)
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}

// GetRunAuditExport calls GET /v1/runs/:id/audit/export with a Bearer token.
func (c *CloudClient) GetRunAuditExport(ctx context.Context, token, id string) (*AuditExportResponse, error) {
	if err := validateRunID("get run audit export", id); err != nil {
		return nil, err
	}
	body, err := c.authenticatedGet(ctx, token, "/v1/runs/"+id+"/audit/export", "get run audit export")
	if err != nil {
		return nil, err
	}
	var result AuditExportResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("get run audit export: decode response: %w", err)
	}
	return &result, nil
}

// ExportRunAudit is an alias for GetRunAuditExport.
func (c *CloudClient) ExportRunAudit(ctx context.Context, token, id string) (*AuditExportResponse, error) {
	return c.GetRunAuditExport(ctx, token, id)
}

// GetMetrics calls GET /v1/metrics with a Bearer token.
func (c *CloudClient) GetMetrics(ctx context.Context, token string) (*MetricsResponse, error) {
	body, err := c.authenticatedGet(ctx, token, "/v1/metrics", "get metrics")
	if err != nil {
		return nil, err
	}
	var result MetricsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("get metrics: decode response: %w", err)
	}
	return &result, nil
}

func (c *CloudClient) getAuditWithQuery(ctx context.Context, token string, query AuditQuery) ([]byte, error) {
	if query.Limit < 0 {
		return nil, fmt.Errorf("get audit: invalid limit %d", query.Limit)
	}
	values := url.Values{}
	for name, value := range map[string]string{"since": query.Since, "until": query.Until} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("get audit: invalid %s filter", name)
		}
		if value != "" {
			values.Set(name, value)
		}
	}
	if query.Limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", query.Limit))
	}
	path := "/v1/audit"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return c.authenticatedGet(ctx, token, path, "get audit")
}

func (c *CloudClient) authenticatedGet(ctx context.Context, token, path, operation string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", operation, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError(operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%s: not authenticated (token may be expired or invalid)", operation)
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError(operation, resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", operation, err)
	}
	return body, nil
}

func (r *RunEventsResponse) UnmarshalJSON(data []byte) error {
	r.Raw = append(r.Raw[:0], data...)
	if events, ok, err := decodeEventArray(data); ok || err != nil {
		if err != nil {
			return err
		}
		r.Events = events
		return nil
	}
	var wire struct {
		RunID      string            `json:"run_id"`
		Events     []CloudAuditEvent `json:"events"`
		Records    []CloudAuditEvent `json:"records"`
		Items      []CloudAuditEvent `json:"items"`
		Data       json.RawMessage   `json:"data"`
		NextCursor string            `json:"next_cursor"`
		HasMore    bool              `json:"has_more"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.RunID, r.NextCursor, r.HasMore = wire.RunID, wire.NextCursor, wire.HasMore
	r.Events = firstEventList(wire.Events, wire.Records, wire.Items)
	if len(r.Events) == 0 && len(wire.Data) > 0 {
		events, _, err := decodeEventArray(wire.Data)
		if err != nil {
			return err
		}
		r.Events = events
	}
	return nil
}

func (r RunEventsResponse) MarshalJSON() ([]byte, error) {
	if len(r.Raw) > 0 {
		return r.Raw, nil
	}
	return json.Marshal(struct {
		RunID      string            `json:"run_id,omitempty"`
		Events     []CloudAuditEvent `json:"events,omitempty"`
		NextCursor string            `json:"next_cursor,omitempty"`
		HasMore    bool              `json:"has_more,omitempty"`
	}{r.RunID, r.Events, r.NextCursor, r.HasMore})
}

func (r *AuditResponse) UnmarshalJSON(data []byte) error {
	r.Raw = append(r.Raw[:0], data...)
	if events, ok, err := decodeEventArray(data); ok || err != nil {
		if err != nil {
			return err
		}
		r.Events = events
		return nil
	}
	var wire struct {
		Events     []CloudAuditEvent `json:"events"`
		Records    []CloudAuditEvent `json:"records"`
		Items      []CloudAuditEvent `json:"items"`
		Data       json.RawMessage   `json:"data"`
		NextCursor string            `json:"next_cursor"`
		HasMore    bool              `json:"has_more"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.NextCursor, r.HasMore = wire.NextCursor, wire.HasMore
	r.Events = firstEventList(wire.Events, wire.Records, wire.Items)
	if len(r.Events) == 0 && len(wire.Data) > 0 {
		events, _, err := decodeEventArray(wire.Data)
		if err != nil {
			return err
		}
		r.Events = events
	}
	return nil
}

func (r AuditResponse) MarshalJSON() ([]byte, error) {
	if len(r.Raw) > 0 {
		return r.Raw, nil
	}
	return json.Marshal(struct {
		Events     []CloudAuditEvent `json:"events,omitempty"`
		NextCursor string            `json:"next_cursor,omitempty"`
		HasMore    bool              `json:"has_more,omitempty"`
	}{r.Events, r.NextCursor, r.HasMore})
}

func (r *AuditExportResponse) UnmarshalJSON(data []byte) error {
	r.Raw = append(r.Raw[:0], data...)
	if events, ok, err := decodeEventArray(data); ok || err != nil {
		if err != nil {
			return err
		}
		r.Records = events
		return nil
	}
	var wire struct {
		RunID      string            `json:"run_id"`
		Format     string            `json:"format"`
		ExportedAt string            `json:"exported_at"`
		Records    []CloudAuditEvent `json:"records"`
		Events     []CloudAuditEvent `json:"events"`
		Data       json.RawMessage   `json:"data"`
		Signature  string            `json:"signature"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.RunID, r.Format, r.ExportedAt, r.Signature = wire.RunID, wire.Format, wire.ExportedAt, wire.Signature
	r.Records = firstEventList(wire.Records, wire.Events)
	r.Events = wire.Events
	if len(r.Records) == 0 && len(wire.Data) > 0 {
		events, _, err := decodeEventArray(wire.Data)
		if err != nil {
			return err
		}
		r.Records = events
	}
	return nil
}

func (r AuditExportResponse) MarshalJSON() ([]byte, error) {
	if len(r.Raw) > 0 {
		return r.Raw, nil
	}
	return json.Marshal(struct {
		RunID      string            `json:"run_id,omitempty"`
		Format     string            `json:"format,omitempty"`
		ExportedAt string            `json:"exported_at,omitempty"`
		Records    []CloudAuditEvent `json:"records,omitempty"`
		Events     []CloudAuditEvent `json:"events,omitempty"`
		Signature  string            `json:"signature,omitempty"`
	}{r.RunID, r.Format, r.ExportedAt, r.Records, r.Events, r.Signature})
}

func (r *MetricsResponse) UnmarshalJSON(data []byte) error {
	type metricsWire MetricsResponse
	var wire metricsWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = MetricsResponse(wire)
	r.Values = make(map[string]any)
	if err := json.Unmarshal(data, &r.Values); err != nil {
		return err
	}
	r.Raw = append(r.Raw[:0], data...)
	return nil
}

func (r MetricsResponse) MarshalJSON() ([]byte, error) {
	if len(r.Raw) > 0 {
		return r.Raw, nil
	}
	type metricsWire MetricsResponse
	return json.Marshal(metricsWire(r))
}

func decodeEventArray(data []byte) ([]CloudAuditEvent, bool, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil, false, nil
	}
	if !strings.HasPrefix(trimmed, "[") {
		return nil, false, nil
	}
	var events []CloudAuditEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, true, err
	}
	return events, true, nil
}

func firstEventList(lists ...[]CloudAuditEvent) []CloudAuditEvent {
	for _, events := range lists {
		if events != nil {
			return events
		}
	}
	return nil
}
