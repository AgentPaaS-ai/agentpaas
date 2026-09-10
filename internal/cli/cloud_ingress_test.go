package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudHelp_MentionsIngress(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "ingress") {
		t.Fatalf("cloud --help silent on ingress:\n%s", stdout)
	}
}

func TestCloudIngressCommandRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	found, _, err := cmd.Find([]string{"cloud", "ingress"})
	if err != nil {
		t.Fatalf("Find cloud ingress: %v", err)
	}
	names := map[string]bool{}
	for _, c := range found.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"source", "connect", "connection", "sources", "connections", "events", "test-filter"} {
		if !names[want] {
			t.Errorf("missing ingress subcommand %s", want)
		}
	}
	source, _, err := cmd.Find([]string{"cloud", "ingress", "source"})
	if err != nil {
		t.Fatalf("Find cloud ingress source: %v", err)
	}
	sourceNames := map[string]bool{}
	for _, c := range source.Commands() {
		sourceNames[c.Name()] = true
	}
	for _, want := range []string{"create", "disable", "rotate", "bind-reply"} {
		if !sourceNames[want] {
			t.Errorf("missing ingress source %s", want)
		}
	}
	connection, _, err := cmd.Find([]string{"cloud", "ingress", "connection"})
	if err != nil {
		t.Fatalf("Find cloud ingress connection: %v", err)
	}
	conNames := map[string]bool{}
	for _, c := range connection.Commands() {
		conNames[c.Name()] = true
	}
	if !conNames["disable"] {
		t.Fatal("missing ingress connection disable")
	}
}

func TestCloudIngress_SourceCreateSecretStdinNotPrinted(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	secret := "super-secret-signing"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/sources" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Errorf("body: %v", err)
		}
		if m["secret"] != secret || m["provider"] != "slack" || m["label"] != "vb-editorial" {
			t.Errorf("body = %s", raw)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb-editorial","request_url":"https://cloud.agentpaas.ai/v1/hooks/src_1"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, secret, "cloud", "ingress", "source", "create",
		"--provider", "slack", "--label", "vb-editorial", "--secret-stdin")
	if err != nil {
		t.Fatalf("create: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout, "src_1") || !strings.Contains(stdout, "https://cloud.agentpaas.ai/v1/hooks/src_1") {
		t.Fatalf("stdout missing id/url: %q", stdout)
	}

	stdout, stderr, err = executeCloudCmd(t, secret, "--json", "cloud", "ingress", "source", "create",
		"--provider", "slack", "--label", "vb-editorial", "--secret-stdin")
	if err != nil {
		t.Fatalf("json create: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Fatalf("secret in json output stdout=%q stderr=%q", stdout, stderr)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("json: %v stdout=%q", err, stdout)
	}
	if m["id"] != "src_1" {
		t.Fatalf("json body = %s", stdout)
	}
}

func TestCloudIngress_ConnectPostsFilterJSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	filter := `{"match":"all","rules":[{"field":"event.channel","op":"eq","value":"C0123"}]}`
	var sawFilter json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/connections" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if string(m["source_id"]) != `"src_1"` || string(m["deployment_id"]) != `"dep_hook"` {
			t.Errorf("ids = %s", raw)
		}
		if string(m["label"]) != `"support triage"` {
			t.Errorf("label = %s", raw)
		}
		sawFilter = m["filter_json"]
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"con_1","source_id":"src_1","deployment_id":"dep_hook","label":"support triage","filter_json":` + filter + `,"status":"active"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "ingress", "connect", "src_1", "dep_hook",
		"--label", "support triage", "--filter", filter)
	if err != nil {
		t.Fatalf("connect: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if string(sawFilter) != filter {
		t.Fatalf("posted filter_json = %s want %s", sawFilter, filter)
	}
	if !strings.Contains(stdout, "con_1") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCloudIngress_ConnectRejectsSlashSourceID(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	_, _, err := executeCloudCmd(t, "", "cloud", "ingress", "connect", "src/1", "dep_hook", "--label", "x")
	if err == nil || !strings.Contains(err.Error(), "invalid source id") {
		t.Fatalf("err = %v, want invalid source id", err)
	}
}

func TestCloudIngress_SourcesAndConnections(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/ingress/sources":
			_, _ = w.Write([]byte(`[{"id":"src_1","provider":"slack","label":"vb-editorial","status":"active","request_url":"https://x/v1/hooks/src_1","last_event_at":"2026-01-01T00:00:00Z","connection_count":1}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/ingress/sources/src_1":
			_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb-editorial","status":"active","connection_count":1,"connections":[{"id":"con_1","source_id":"src_1","deployment_id":"dep_hook","label":"support triage","filter_json":{"match":"all"},"status":"active"}]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "ingress", "sources")
	if err != nil {
		t.Fatalf("sources: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "src_1") || !strings.Contains(stdout, "vb-editorial") {
		t.Fatalf("sources stdout=%q", stdout)
	}

	stdout, stderr, err = executeCloudCmd(t, "", "cloud", "ingress", "connections", "src_1")
	if err != nil {
		t.Fatalf("connections: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "con_1") || !strings.Contains(stdout, "dep_hook") {
		t.Fatalf("connections stdout=%q", stdout)
	}
}

func TestCloudIngress_DisableSourceAndConnection(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ingress/sources/src_1/disable":
			_, _ = w.Write([]byte(`{"id":"src_1","status":"disabled"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ingress/connections/con_1/disable":
			_, _ = w.Write([]byte(`{"id":"con_1","status":"disabled"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "ingress", "source", "disable", "src_1")
	if err != nil {
		t.Fatalf("source disable: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "src_1") || !strings.Contains(stdout, "disabled") {
		t.Fatalf("source disable stdout=%q", stdout)
	}

	stdout, stderr, err = executeCloudCmd(t, "", "cloud", "ingress", "connection", "disable", "con_1")
	if err != nil {
		t.Fatalf("connection disable: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "con_1") || !strings.Contains(stdout, "disabled") {
		t.Fatalf("connection disable stdout=%q", stdout)
	}
}

func TestCloudIngress_RotateSecretStdinNotPrinted(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	secret := "whsec_rotate_cli"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/sources/src_1/rotate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Errorf("body: %v", err)
		}
		if m["secret"] != secret {
			t.Errorf("body = %s", raw)
		}
		_, _ = w.Write([]byte(`{"id":"src_1","provider":"slack","label":"vb","status":"active","request_url":"https://x/v1/hooks/src_1"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, secret, "cloud", "ingress", "source", "rotate", "src_1", "--secret-stdin")
	if err != nil {
		t.Fatalf("rotate: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout, "src_1") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestCloudIngress_BindReplySecretStdinNotPrinted(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	secret := "xoxb-cli-bot-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/sources/src_1/bind-reply" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Errorf("body: %v", err)
		}
		if m["credential"] != "slack-bot-token" || m["secret"] != secret {
			t.Errorf("body = %s", raw)
		}
		_, _ = w.Write([]byte(`{"ok":true,"credential":"slack-bot-token"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, secret, "cloud", "ingress", "source", "bind-reply", "src_1",
		"--credential", "slack-bot-token", "--secret-stdin")
	if err != nil {
		t.Fatalf("bind-reply: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout, "slack-bot-token") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestCloudIngress_EventsTail(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/v1/ingress/sources/src_1/events" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("limit") != "20" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(`[{"id":"evt_1","provider":"slack","provider_event_id":"p1","status":"admitted","matched_connection_ids":["con_1"],"created_at":"2026-01-01T00:00:00Z","payload_json":"{}"}]`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "ingress", "events", "src_1", "--tail", "20")
	if err != nil {
		t.Fatalf("events: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "evt_1") || !strings.Contains(stdout, "con_1") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestCloudIngress_TestFilterPostsJSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	filter := `{"match":"all","rules":[{"field":"event.type","op":"eq","value":"payment"}]}`
	event := `{"event":{"type":"payment"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingress/test-filter" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("body: %v", err)
		}
		if string(m["filter"]) != filter || string(m["event"]) != event {
			t.Errorf("body = %s", raw)
		}
		_, _ = w.Write([]byte(`{"matched":true}`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "ingress", "test-filter", "src_1",
		"--filter", filter, "--event", event)
	if err != nil {
		t.Fatalf("test-filter: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "matched=true") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestCloudIngress_LifecycleRejectsSlashIDs(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_ingress")
	cases := [][]string{
		{"cloud", "ingress", "source", "disable", "src/1"},
		{"cloud", "ingress", "connection", "disable", "con/1"},
		{"cloud", "ingress", "connections", "src/1"},
		{"cloud", "ingress", "events", "src/1"},
		{"cloud", "ingress", "test-filter", "src/1", "--filter", "{}", "--event", "{}"},
	}
	for _, args := range cases {
		_, _, err := executeCloudCmd(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("args=%v err=%v, want invalid id", args, err)
		}
	}
}
