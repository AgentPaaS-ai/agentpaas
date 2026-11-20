package cloudclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadInput_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/inputs" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_tok" {
			t.Errorf("auth = %q", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "hello-input" {
			t.Errorf("body = %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"input_id":"inp_1","r2_key":"tenants/t1/inputs/inp_1","sha256":"` + strings.Repeat("a", 64) + `","size_bytes":11,"input_ref":{"r2_key":"tenants/t1/inputs/inp_1","sha256":"` + strings.Repeat("a", 64) + `","size_bytes":11}}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	got, err := client.UploadInput(context.Background(), "apc_tok", []byte("hello-input"), "application/octet-stream")
	if err != nil {
		t.Fatalf("UploadInput: %v", err)
	}
	if got.R2Key != "tenants/t1/inputs/inp_1" || got.SizeBytes != 11 {
		t.Fatalf("got %#v", got)
	}
}

func TestMergeInputRefIntoBody(t *testing.T) {
	out, err := MergeInputRefIntoBody(json.RawMessage(`{"city":"SEA"}`), map[string]any{
		"r2_key":     "tenants/t1/inputs/x",
		"sha256":     strings.Repeat("b", 64),
		"size_bytes": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["city"] != "SEA" {
		t.Fatalf("city lost: %#v", m)
	}
	ref, ok := m["input_ref"].(map[string]any)
	if !ok || ref["r2_key"] != "tenants/t1/inputs/x" {
		t.Fatalf("input_ref: %#v", m["input_ref"])
	}
}
