package cloudclient

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateIngressSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/sources" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); got != UserAgent {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_tok" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]string
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if m["provider"] != "slack" || m["label"] != "vb-editorial" || m["secret"] != "s3cret" {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb-editorial","request_url":"https://cloud.agentpaas.ai/v1/hooks/src_1"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.CreateIngressSource(t.Context(), "apc_tok", "slack", "vb-editorial", "s3cret")
	if err != nil {
		t.Fatalf("CreateIngressSource: %v", err)
	}
	if res.ID != "src_1" || res.Provider != "slack" || res.Label != "vb-editorial" {
		t.Fatalf("res = %+v", res)
	}
	if res.RequestURL != "https://cloud.agentpaas.ai/v1/hooks/src_1" {
		t.Fatalf("request_url = %q", res.RequestURL)
	}
}

func TestCreateIngressConnection_PostsFilterJSON(t *testing.T) {
	filter := json.RawMessage(`{"match":"all","rules":[{"field":"event.channel","op":"eq","value":"C0123"}]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/connections" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if string(m["source_id"]) != `"src_1"` || string(m["deployment_id"]) != `"dep_1"` {
			t.Errorf("ids in body = %s", body)
		}
		if string(m["label"]) != `"support triage"` {
			t.Errorf("label in body = %s", body)
		}
		if string(m["filter_json"]) != string(filter) {
			t.Errorf("filter_json = %s want %s", m["filter_json"], filter)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"con_1","source_id":"src_1","deployment_id":"dep_1","label":"support triage","filter_json":{"match":"all"},"status":"active"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.CreateIngressConnection(t.Context(), "apc_tok", "src_1", "dep_1", "support triage", filter)
	if err != nil {
		t.Fatalf("CreateIngressConnection: %v", err)
	}
	if res.ID != "con_1" || res.Status != "active" {
		t.Fatalf("res = %+v", res)
	}
}

func TestCreateIngressConnection_OmitsEmptyFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "filter_json") {
			t.Errorf("empty filter should omit filter_json: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"con_2","source_id":"src_1","deployment_id":"dep_1","label":"all","filter_json":null,"status":"active"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	if _, err := client.CreateIngressConnection(t.Context(), "apc_tok", "src_1", "dep_1", "all", nil); err != nil {
		t.Fatalf("CreateIngressConnection: %v", err)
	}
}

func TestCreateIngressConnection_RejectsSlashIDs(t *testing.T) {
	client := NewCloudClient("http://127.0.0.1:1")
	_, err := client.CreateIngressConnection(t.Context(), "tok", "src/1", "dep_1", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("source slash: err = %v", err)
	}
	_, err = client.CreateIngressConnection(t.Context(), "tok", "src_1", "dep\\1", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid deployment id") {
		t.Fatalf("deployment backslash: err = %v", err)
	}
	_, err = client.CreateIngressConnection(t.Context(), "tok", "src_1\n", "dep_1", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("source newline: err = %v", err)
	}
}
