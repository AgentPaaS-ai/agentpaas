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

func TestCloudClient_GetRunResult_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/runs/run-failed/result" {
			t.Errorf("expected result path, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_result_token" {
			t.Errorf("Authorization = %q, want Bearer apc_result_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"run_id":"run-failed",
			"status":"failed",
			"error":"container_runtime_unavailable",
			"finished_at":"2026-07-31T10:00:00Z",
			"final_output":{"answer":42},
			"artifacts":[
				{"name":"result.json","size_bytes":123,"url":"https://cloud.example/result-token","expires_in_sec":3600},
				{"name":"logs.txt","size_bytes":18,"url":"https://cloud.example/log-token","expires_in_sec":3600}
			]
		}`))
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetRunResult(context.Background(), "apc_result_token", "run-failed")
	if err != nil {
		t.Fatalf("GetRunResult: %v", err)
	}
	if result.RunID != "run-failed" {
		t.Errorf("RunID = %q, want run-failed", result.RunID)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
	if result.Error == nil || *result.Error != "container_runtime_unavailable" {
		t.Errorf("Error = %v, want container_runtime_unavailable", result.Error)
	}
	if len(result.FinalOutput) == 0 {
		t.Fatal("FinalOutput is empty")
	}
	var finalOutput map[string]int
	if err := json.Unmarshal(result.FinalOutput, &finalOutput); err != nil {
		t.Fatalf("decode FinalOutput: %v", err)
	}
	if finalOutput["answer"] != 42 {
		t.Errorf("final_output.answer = %d, want 42", finalOutput["answer"])
	}
	if len(result.Artifacts) != 2 || result.Artifacts[1].Name != "logs.txt" {
		t.Fatalf("Artifacts = %+v, want result.json and logs.txt", result.Artifacts)
	}
}

func TestCloudClient_GetRunLogs_FetchesSignedArtifactWithoutBearer(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runs/run-logs/result":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"run_id":"run-logs",
				"status":"failed",
				"error":"agent_failed",
				"final_output":null,
				"artifacts":[{"name":"logs.txt","size_bytes":18,"url":"` + server.URL + `/signed/logs","expires_in_sec":3600}]
			}`))
		case "/signed/logs":
			if auth := r.Header.Get("Authorization"); auth != "" {
				t.Errorf("signed artifact request leaked Authorization header %q", auth)
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("worker failed\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	logs, err := NewCloudClient(server.URL).GetRunLogs(context.Background(), "apc_result_token", "run-logs")
	if err != nil {
		t.Fatalf("GetRunLogs: %v", err)
	}
	if string(logs) != "worker failed\n" {
		t.Errorf("logs = %q, want worker failed newline", logs)
	}
}

func TestCloudClient_GetRunLogs_MissingArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-no-logs","status":"succeeded","artifacts":[]}`))
	}))
	defer func() { server.Close() }()

	_, err := NewCloudClient(server.URL).GetRunLogs(context.Background(), "token", "run-no-logs")
	if !errors.Is(err, ErrRunLogsNotFound) {
		t.Fatalf("GetRunLogs error = %v, want ErrRunLogsNotFound", err)
	}
}

func TestCloudClient_GetRunResult_InvalidID(t *testing.T) {
	client := NewCloudClient("https://example.com")
	_, err := client.GetRunResult(context.Background(), "token", "run/../result")
	if err == nil {
		t.Fatal("expected invalid run ID error")
	}
	if !strings.Contains(err.Error(), "invalid id") {
		t.Errorf("error = %v, want invalid id", err)
	}
}

func TestCloudClient_FetchSignedURL_RejectsInvalidURL(t *testing.T) {
	_, err := NewCloudClient("https://example.com").FetchSignedURL(context.Background(), "file:///tmp/logs.txt")
	if err == nil {
		t.Fatal("expected invalid signed URL error")
	}
	if !strings.Contains(err.Error(), "signed URL") {
		t.Errorf("error = %v, want signed URL message", err)
	}
}
