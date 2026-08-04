package cloudclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_GetRunEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/runs/run-42/events" {
			t.Errorf("path = %q, want /v1/runs/run-42/events", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_events" {
			t.Errorf("Authorization = %q, want Bearer apc_events", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"run_id": "run-42",
			"events": []map[string]any{
				{
					"id":         "evt-1",
					"event_type": "policy_denial",
					"timestamp":  "2026-08-04T00:00:00Z",
					"payload":    map[string]any{"host": "example.com"},
				},
			},
		})
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetRunEvents(context.Background(), "apc_events", "run-42")
	if err != nil {
		t.Fatalf("GetRunEvents: %v", err)
	}
	if result.RunID != "run-42" {
		t.Fatalf("RunID = %q, want run-42", result.RunID)
	}
	if len(result.Events) != 1 || result.Events[0].EventType != "policy_denial" {
		t.Fatalf("Events = %#v, want one policy_denial event", result.Events)
	}
	if result.Events[0].Payload["host"] != "example.com" {
		t.Fatalf("event payload = %#v, want host", result.Events[0].Payload)
	}
}

func TestCloudClient_GetAudit_QueryAndTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audit" {
			t.Errorf("path = %q, want /v1/audit", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("since") != "2026-08-01T00:00:00Z" {
			t.Errorf("since = %q", query.Get("since"))
		}
		if query.Get("until") != "2026-08-04T00:00:00Z" {
			t.Errorf("until = %q", query.Get("until"))
		}
		if query.Get("limit") != "25" {
			t.Errorf("limit = %q", query.Get("limit"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_audit" {
			t.Errorf("Authorization = %q, want Bearer apc_audit", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events":      []map[string]any{{"seq": 7, "event_type": "run_started"}},
			"next_cursor": "cursor-2",
		})
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetAudit(
		context.Background(),
		"apc_audit",
		"2026-08-01T00:00:00Z",
		"2026-08-04T00:00:00Z",
		25,
	)
	if err != nil {
		t.Fatalf("GetAudit: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Seq != 7 {
		t.Fatalf("Events = %#v, want seq 7", result.Events)
	}
	if result.NextCursor != "cursor-2" {
		t.Fatalf("NextCursor = %q, want cursor-2", result.NextCursor)
	}

	serverError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"audit_limit","message":"too many records"}`))
	}))
	defer func() { serverError.Close() }()

	_, err = NewCloudClient(serverError.URL).GetAudit(context.Background(), "token", "", "", 0)
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("GetAudit error = %v, want HTTPStatusError", err)
	}
	if statusErr.ErrorCode != "quota_exceeded" || !strings.Contains(statusErr.Message, "too many records") {
		t.Fatalf("status error = %+v", statusErr)
	}
}

func TestCloudClient_GetRunAuditExport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/runs/run-42/audit/export" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_export" {
			t.Errorf("Authorization = %q, want Bearer apc_export", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-42","format":"jsonl","records":[{"seq":1,"event_type":"run_started"}]}`))
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetRunAuditExport(context.Background(), "apc_export", "run-42")
	if err != nil {
		t.Fatalf("GetRunAuditExport: %v", err)
	}
	if result.RunID != "run-42" || result.Format != "jsonl" || len(result.Records) != 1 {
		t.Fatalf("export = %#v", result)
	}
	if !strings.Contains(string(result.Raw), `"event_type":"run_started"`) {
		t.Fatalf("raw export = %s", result.Raw)
	}
}

func TestCloudClient_GetMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/metrics" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_metrics" {
			t.Errorf("Authorization = %q, want Bearer apc_metrics", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runs_total":3,"runs_active":1,"events_total":12,"latency_ms_p95":42.5}`))
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetMetrics(context.Background(), "apc_metrics")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if result.RunsTotal != 3 || result.RunsActive != 1 || result.EventsTotal != 12 {
		t.Fatalf("metrics = %#v", result)
	}
	if result.LatencyMSP95 != 42.5 {
		t.Fatalf("LatencyMSP95 = %v, want 42.5", result.LatencyMSP95)
	}
}

func TestCloudClient_RunAuditMethodsRejectInvalidID(t *testing.T) {
	client := NewCloudClient("https://example.com")
	for _, id := range []string{"", "run/../x", "run?x", "run\x00x"} {
		if _, err := client.GetRunEvents(context.Background(), "token", id); err == nil {
			t.Errorf("GetRunEvents(%q) accepted invalid ID", id)
		}
		if _, err := client.GetRunAuditExport(context.Background(), "token", id); err == nil {
			t.Errorf("GetRunAuditExport(%q) accepted invalid ID", id)
		}
	}
}
