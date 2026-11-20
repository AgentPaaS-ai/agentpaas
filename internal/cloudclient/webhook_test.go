package cloudclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudClient_UserAgentOnWhoami(t *testing.T) {
	if UserAgent != "agentpaas-cli/0.1" {
		t.Fatalf("UserAgent = %q, want agentpaas-cli/0.1", UserAgent)
	}
	saw := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"t1","tier":"trial"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	if _, err := client.Whoami(t.Context(), "apc_ua"); err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if saw != UserAgent {
		t.Fatalf("User-Agent = %q, want %q", saw, UserAgent)
	}
}

func TestPutDeploymentWebhook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/deployments/dep_1/webhook" {
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
		if m["provider"] != "generic_hmac" || m["secret"] != "s3cret" {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"configured":true,"provider":"generic_hmac","deployment_id":"dep_1"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	res, err := client.PutDeploymentWebhook(t.Context(), "apc_tok", "dep_1", "generic_hmac", "s3cret")
	if err != nil {
		t.Fatalf("PutDeploymentWebhook: %v", err)
	}
	if !res.Configured || res.Provider != "generic_hmac" || res.DeploymentID != "dep_1" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPutCompletionAndDeliveryWebhook(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"url":"https://hooks.example/x"`) {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"configured":true}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	if _, err := client.PutCompletionWebhook(t.Context(), "apc_tok", "dep_1", "https://hooks.example/x"); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if _, err := client.PutDeliveryWebhook(t.Context(), "apc_tok", "dep_1", "https://hooks.example/x"); err != nil {
		t.Fatalf("delivery: %v", err)
	}
	want := []string{
		"PUT /v1/deployments/dep_1/completion-webhook",
		"PUT /v1/deployments/dep_1/delivery-webhook",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestFireGenericHMAC_SignsBody(t *testing.T) {
	now := time.Unix(1700000000, 0)
	body := []byte(`{"hello":"world"}`)
	secret := "hook-secret"
	var gotHeader string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/deployments/dep_1/hooks/generic_hmac" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-Agentpaas-Signature")
		if got := r.Header.Get("User-Agent"); got != UserAgent {
			t.Errorf("User-Agent = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if !hmac.Equal(raw, body) {
			t.Errorf("body = %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run_hook"}`))
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	raw, err := client.FireGenericHMAC(t.Context(), "dep_1", body, secret, now)
	if err != nil {
		t.Fatalf("FireGenericHMAC: %v", err)
	}
	if !strings.Contains(string(raw), "run_hook") {
		t.Fatalf("response = %s", raw)
	}
	if gotAuth != "" {
		t.Fatalf("fire must not send tenant token, Authorization=%q", gotAuth)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", now.Unix())
	_, _ = mac.Write(body)
	want := fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(mac.Sum(nil)))
	if gotHeader != want {
		t.Fatalf("signature = %q, want %q", gotHeader, want)
	}
}

func TestPutDeploymentWebhook_RejectsSlashID(t *testing.T) {
	client := NewCloudClient("http://127.0.0.1:1")
	_, err := client.PutDeploymentWebhook(t.Context(), "tok", "dep/1", "generic_hmac", "s")
	if err == nil || !strings.Contains(err.Error(), "invalid deployment id") {
		t.Fatalf("err = %v", err)
	}
}
