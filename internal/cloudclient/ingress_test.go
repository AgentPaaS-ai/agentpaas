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
	_, err = client.CreateIngressConnection(t.Context(), "tok", "src_1", "dep\\\\1", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid deployment id") {
		t.Fatalf("deployment backslash: err = %v", err)
	}
	_, err = client.CreateIngressConnection(t.Context(), "tok", "src_1\n", "dep_1", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("source newline: err = %v", err)
	}
}

func TestListIngressSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/ingress/sources" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"src_1","provider":"slack","label":"vb","status":"active","request_url":"https://cloud.agentpaas.ai/v1/hooks/src_1","last_event_at":null,"connection_count":2}]`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.ListIngressSources(t.Context(), "apc_tok")
	if err != nil {
		t.Fatalf("ListIngressSources: %v", err)
	}
	if len(res) != 1 || res[0].ID != "src_1" || res[0].ConnectionCount != 2 {
		t.Fatalf("res = %+v", res)
	}
}

func TestGetIngressSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/ingress/sources/src_1" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb","status":"active","request_url":"https://x/v1/hooks/src_1","connection_count":1,"connections":[{"id":"con_1","source_id":"src_1","deployment_id":"dep_1","label":"triage","filter_json":{"match":"all"},"status":"active"}]}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.GetIngressSource(t.Context(), "apc_tok", "src_1")
	if err != nil {
		t.Fatalf("GetIngressSource: %v", err)
	}
	if res.ID != "src_1" || len(res.Connections) != 1 || res.Connections[0].ID != "con_1" {
		t.Fatalf("res = %+v", res)
	}
}

func TestListIngressSourceEvents_SendsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/ingress/sources/src_1/events" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "20" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"evt_1","provider":"slack","provider_event_id":"p1","status":"admitted","matched_connection_ids":["con_1"],"created_at":"2026-01-01T00:00:00Z","payload_json":"{\"ok\":true}"}]`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.ListIngressSourceEvents(t.Context(), "apc_tok", "src_1", 20)
	if err != nil {
		t.Fatalf("ListIngressSourceEvents: %v", err)
	}
	if len(res) != 1 || res[0].ID != "evt_1" || len(res[0].MatchedConnectionIDs) != 1 {
		t.Fatalf("res = %+v", res)
	}
}

func TestDisableIngressSourceAndConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ingress/sources/src_1/disable":
			_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb","status":"disabled","request_url":"https://x/v1/hooks/src_1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ingress/connections/con_1/disable":
			_, _ = w.Write([]byte(`{"id":"con_1","source_id":"src_1","deployment_id":"dep_1","label":"x","status":"disabled"}`))
		default:
			t.Errorf("got %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	src, err := client.DisableIngressSource(t.Context(), "apc_tok", "src_1")
	if err != nil {
		t.Fatalf("DisableIngressSource: %v", err)
	}
	if src.Status != "disabled" {
		t.Fatalf("src = %+v", src)
	}
	con, err := client.DisableIngressConnection(t.Context(), "apc_tok", "con_1")
	if err != nil {
		t.Fatalf("DisableIngressConnection: %v", err)
	}
	if con.Status != "disabled" {
		t.Fatalf("con = %+v", con)
	}
}

func TestRotateIngressSource_PostsSecretNotInResponse(t *testing.T) {
	secret := "whsec_rotate_new"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/sources/src_1/rotate" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]string
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if m["secret"] != secret {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb","status":"active","request_url":"https://x/v1/hooks/src_1"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.RotateIngressSource(t.Context(), "apc_tok", "src_1", secret)
	if err != nil {
		t.Fatalf("RotateIngressSource: %v", err)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("secret leaked in response struct: %s", raw)
	}
}

func TestBindIngressSourceReply_PostsCredentialSecret(t *testing.T) {
	secret := "xoxb-test-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/sources/src_1/bind-reply" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]string
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if m["credential"] != "slack-bot-token" || m["secret"] != secret {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"credential":"slack-bot-token"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.BindIngressSourceReply(t.Context(), "apc_tok", "src_1", "slack-bot-token", secret)
	if err != nil {
		t.Fatalf("BindIngressSourceReply: %v", err)
	}
	if !res.OK || res.Credential != "slack-bot-token" {
		t.Fatalf("res = %+v", res)
	}
}

func TestTestIngressFilter(t *testing.T) {
	filter := json.RawMessage(`{"match":"all","rules":[{"field":"event.type","op":"eq","value":"payment"}]}`)
	event := json.RawMessage(`{"event":{"type":"payment"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/test-filter" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if string(m["filter"]) != string(filter) || string(m["event"]) != string(event) {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"matched":true}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.TestIngressFilter(t.Context(), "apc_tok", filter, event)
	if err != nil {
		t.Fatalf("TestIngressFilter: %v", err)
	}
	if !res.Matched {
		t.Fatalf("res = %+v", res)
	}
}

func TestIngressLifecycle_RejectsSlashIDs(t *testing.T) {
	client := NewCloudClient("http://127.0.0.1:1")
	if _, err := client.GetIngressSource(t.Context(), "tok", "src/1"); err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("get: %v", err)
	}
	if _, err := client.DisableIngressSource(t.Context(), "tok", "src\\1"); err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("disable source: %v", err)
	}
	if _, err := client.DisableIngressConnection(t.Context(), "tok", "con/1"); err == nil || !strings.Contains(err.Error(), "invalid connection id") {
		t.Fatalf("disable connection: %v", err)
	}
	if _, err := client.RotateIngressSource(t.Context(), "tok", "src/1", "s"); err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := client.BindIngressSourceReply(t.Context(), "tok", "src/1", "slack-bot-token", "xoxb-x"); err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("bind-reply: %v", err)
	}
	if _, err := client.ListIngressSourceEvents(t.Context(), "tok", "src/1", 20); err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("events: %v", err)
	}
}
