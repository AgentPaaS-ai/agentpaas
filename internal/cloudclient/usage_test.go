package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_GetUsage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/usage" {
			t.Errorf("expected /v1/usage, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_usage_token" {
			t.Errorf("Authorization = %q, want Bearer apc_usage_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tier":                  "trial",
			"concurrency_limit":     5,
			"concurrency_active":    0,
			"agent_limit":           10,
			"agents_used":           1,
			"cpu_minute_limit":      100,
			"cpu_minutes_used":      1.25,
			"cpu_minutes_remaining": 98.75,
			"usage_period_start":    "2026-07-01T00:00:00Z",
			"trial_expires_at":      "2026-07-28T00:00:00Z",
			"days_remaining":        27,
			"meter": map[string]interface{}{
				"formula":                "(finished_at-started_at)+sleep_tail",
				"sleep_tail_sec_default": 30,
				"note":                   "awake estimate; CF invoice reconcile later",
			},
		})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	result, err := client.GetUsage(context.Background(), "apc_usage_token")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if result.Tier != "trial" {
		t.Errorf("Tier = %q, want trial", result.Tier)
	}
	if result.ConcurrencyLimit != 5 || result.ConcurrencyActive != 0 {
		t.Errorf("concurrency = %d/%d, want 0/5", result.ConcurrencyActive, result.ConcurrencyLimit)
	}
	if result.AgentLimit != 10 || result.AgentsUsed != 1 {
		t.Errorf("agents = %d/%d, want 1/10", result.AgentsUsed, result.AgentLimit)
	}
	if result.CPUMinuteLimit != 100 || result.CPUMinutesUsed != 1.25 {
		t.Errorf("cpu = %v/%d, want 1.25/100", result.CPUMinutesUsed, result.CPUMinuteLimit)
	}
	if result.CPUMinutesRemaining == nil || *result.CPUMinutesRemaining != 98.75 {
		t.Errorf("CPUMinutesRemaining = %v, want 98.75", result.CPUMinutesRemaining)
	}
	if result.UsagePeriodStart != "2026-07-01T00:00:00Z" {
		t.Errorf("UsagePeriodStart = %q", result.UsagePeriodStart)
	}
	if result.TrialExpiresAt != "2026-07-28T00:00:00Z" {
		t.Errorf("TrialExpiresAt = %q", result.TrialExpiresAt)
	}
	if result.DaysRemaining == nil || *result.DaysRemaining != 27 {
		t.Errorf("DaysRemaining = %v, want 27", result.DaysRemaining)
	}
	if result.Meter.Formula != "(finished_at-started_at)+sleep_tail" {
		t.Errorf("Meter.Formula = %q", result.Meter.Formula)
	}
	if result.Meter.SleepTailSecDefault != 30 {
		t.Errorf("Meter.SleepTailSecDefault = %d, want 30", result.Meter.SleepTailSecDefault)
	}
	if result.Meter.Note != "awake estimate; CF invoice reconcile later" {
		t.Errorf("Meter.Note = %q", result.Meter.Note)
	}
}

func TestCloudClient_GetUsage_NullableFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tier":"pro",
			"concurrency_limit":0,
			"concurrency_active":0,
			"agent_limit":0,
			"agents_used":0,
			"cpu_minute_limit":0,
			"cpu_minutes_used":0,
			"cpu_minutes_remaining":null,
			"usage_period_start":"2026-07-01T00:00:00Z",
			"trial_expires_at":"",
			"days_remaining":null,
			"meter":{"formula":"formula","sleep_tail_sec_default":30,"note":"note"}
		}`))
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetUsage(context.Background(), "token")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if result.CPUMinutesRemaining != nil {
		t.Errorf("CPUMinutesRemaining = %v, want nil", result.CPUMinutesRemaining)
	}
	if result.DaysRemaining != nil {
		t.Errorf("DaysRemaining = %v, want nil", result.DaysRemaining)
	}
}

func TestCloudClient_GetUsage_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	_, err := NewCloudClient(server.URL).GetUsage(context.Background(), "bad_token")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_GetUsage_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer func() { server.Close() }()

	_, err := NewCloudClient(server.URL).GetUsage(context.Background(), "token")
	if err == nil {
		t.Fatal("expected error for 502 status")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should mention status 502, got: %v", err)
	}
}
