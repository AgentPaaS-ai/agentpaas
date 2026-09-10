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
	for _, want := range []string{"source", "connect"} {
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
	if !sourceNames["create"] {
		t.Fatal("missing ingress source create")
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
