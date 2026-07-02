package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func b7T03Sentinel() string {
	return strings.Join([]string{
		"BROKERED",
		"SENTINEL",
		"d9c4f8a17e5b4260",
		"NOT",
		"VISIBLE",
		"ANYWHERE",
	}, "_")
}

type b7T03Flow struct {
	broker *Broker
	audit  *recordingAuditSink
	policy *policy.Policy
}

func TestInvisibility_B7_T03_AgentEnvironment(t *testing.T) {
	flow := newB7T03Flow(t)
	_ = requestB7T03Credential(t, flow)

	assertNoB7T03Sentinel(t, "agent environment", strings.Join(os.Environ(), "\x00"))
}

func TestInvisibility_B7_T03_ProcessArgs(t *testing.T) {
	before := append([]string(nil), os.Args...)
	flow := newB7T03Flow(t)
	_ = requestB7T03Credential(t, flow)
	after := append([]string(nil), os.Args...)

	assertNoB7T03Sentinel(t, "process args before broker flow", strings.Join(before, "\x00"))
	assertNoB7T03Sentinel(t, "process args after broker flow", strings.Join(after, "\x00"))
	if runtime.GOOS == "linux" {
		assertProcFileNoB7T03Sentinel(t, "/proc/self/cmdline")
		assertProcFileNoB7T03Sentinel(t, "/proc/self/environ")
	}
}

func TestInvisibility_B7_T03_FilesystemWalk(t *testing.T) {
	tmpDir := t.TempDir()
	flow := newB7T03Flow(t)
	injection := requestB7T03Credential(t, flow)
	writeB7T03SanitizedArtifacts(t, tmpDir, flow, injection)

	err := filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(b7T03Sentinel())) {
			return fmt.Errorf("%s contains brokered sentinel", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInvisibility_B7_T03_DaemonLogs(t *testing.T) {
	flow := newB7T03Flow(t)
	_ = requestB7T03Credential(t, flow)

	var log bytes.Buffer
	for _, record := range b7T03AuditRecords(t, flow.audit) {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal audit record: %v", err)
		}
		_, _ = log.Write(data)
		_ = log.WriteByte('\n')
	}
	assertNoB7T03Sentinel(t, "broker audit log fallback", log.String())
}

func TestInvisibility_B7_T03_GatewayLogs(t *testing.T) {
	ctx := context.Background()
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.net/redirected", http.StatusFound)
	}))
	defer func() { redirector.Close() }()

	domain, port := mustServerDomainPort(t, redirector.URL)
	flow := newB7T03FlowForPolicy(t, newBrokeredPolicy(domain, port))
	gateway := NewGateway(flow.broker, redirector.Client())

	injection := requestB7T03CredentialTo(t, flow, redirector.URL)
	for _, rendered := range []string{
		fmt.Sprint(injection),
		fmt.Sprintf("%v", injection),
		fmt.Sprintf("%+v", injection),
		fmt.Sprintf("%#v", injection),
		injection.String(),
		injection.GoString(),
	} {
		assertNoB7T03Sentinel(t, "credential injection rendering", rendered)
		if !strings.Contains(rendered, "[REDACTED]") {
			t.Fatalf("credential injection rendering missing redaction marker: %q", rendered)
		}
	}

	resp, err := gateway.Do(ctx, GatewayRequest{
		RunID:        "run-active",
		PolicyRuleID: "egress[0]",
		Method:       http.MethodGet,
		URL:          redirector.URL,
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err == nil {
		t.Fatal("Gateway Do succeeded, want credentialed redirect denial")
	}
	assertNoB7T03Sentinel(t, "gateway error", err.Error())
	assertNoB7T03Sentinel(t, "gateway audit records", fmt.Sprint(b7T03AuditRecords(t, flow.audit)))
}

func TestInvisibility_B7_T03_CLIErrors(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "unused-token", []byte(b7T03Sentinel())); err != nil {
		t.Fatalf("set unused sentinel secret: %v", err)
	}
	flow := newB7T03FlowForStore(t, store)

	_, err := flow.broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("RequestCredential error = %v, want ErrSecretNotFound", err)
	}
	assertNoB7T03Sentinel(t, "CLI-style missing secret error", err.Error())
}

func TestInvisibility_B7_T03_CompiledConfig(t *testing.T) {
	p := &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "b7-t03-agent"},
		Egress: []policy.EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Credential: "api-token"},
		},
		Credentials: []policy.Credential{
			{
				ID:      "api-token",
				Type:    "brokered",
				Header:  "Authorization",
				Value:   b7T03Sentinel(),
				Service: "test-store",
			},
		},
	}

	canonical, _ := policy.Canonicalize(p)
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("marshal canonical policy: %v", err)
	}
	digest, err := policy.Digest(p)
	if err != nil {
		t.Fatalf("policy digest: %v", err)
	}
	gatewayConfig, err := policy.CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile gateway config: %v", err)
	}
	credentialRules, err := policy.CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("compile credential rules: %v", err)
	}

	assertNoB7T03Sentinel(t, "canonical policy", string(canonicalJSON))
	assertNoB7T03Sentinel(t, "policy digest", digest)
	assertNoB7T03Sentinel(t, "gateway config", string(gatewayConfig))
	assertNoB7T03Sentinel(t, "credential rules", string(credentialRules))
	if !strings.Contains(string(credentialRules), "api-token") {
		t.Fatalf("credential rules omitted credential id: %s", credentialRules)
	}
}

func TestInvisibility_B7_T03_AuditEvent(t *testing.T) {
	flow := newB7T03Flow(t)
	_ = requestB7T03Credential(t, flow)

	records := b7T03AuditRecords(t, flow.audit)
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	record := records[0]
	if record.EventType != audit.EventTypeSecretInjected {
		t.Fatalf("EventType = %q, want %q", record.EventType, audit.EventTypeSecretInjected)
	}
	if got := record.Payload["visible_to_agent"]; got != false {
		t.Fatalf("visible_to_agent = %#v, want false", got)
	}
	if _, ok := record.Payload["value"]; ok {
		t.Fatalf("audit payload includes value field: %#v", record.Payload)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal audit record: %v", err)
	}
	assertNoB7T03Sentinel(t, "audit event", string(data))
}

func TestInvisibility_B7_T03_DockerInspect(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker invisibility probes")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Docker invisibility probe is enabled only on Linux")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not found")
	}

	flow := newB7T03Flow(t)
	_ = requestB7T03Credential(t, flow)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		t.Skipf("docker daemon unavailable: %v: %s", err, out)
	}

	name := fmt.Sprintf("agentpaas-b7-t03-%d", time.Now().UnixNano())
	runCtx, runCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer runCancel()
	out, err := exec.CommandContext(runCtx, "docker", "run", "--rm", "-d", "--name", name, "--env", "AGENTPAAS_CREDENTIAL_ID=api-token", "alpine:3.20", "sleep", "30").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	defer func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer rmCancel()
		_ = exec.CommandContext(rmCtx, "docker", "rm", "-f", name).Run()
	}()

	inspectCtx, inspectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer inspectCancel()
	inspect, err := exec.CommandContext(inspectCtx, "docker", "inspect", name).CombinedOutput()
	if err != nil {
		t.Fatalf("docker inspect: %v: %s", err, inspect)
	}
	assertNoB7T03Sentinel(t, "docker inspect", string(inspect))
}

func TestInvisibility_B7_T03_ProcessList(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps command not found")
	}
	flow := newB7T03Flow(t)
	_ = requestB7T03Credential(t, flow)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "aux").CombinedOutput()
	if err != nil {
		t.Fatalf("ps aux: %v: %s", err, out)
	}
	assertNoB7T03Sentinel(t, "process list", string(out))
}

func TestInvisibility_B7_T03_ShellHistoryFixture(t *testing.T) {
	tmpDir := t.TempDir()
	historyFixture := filepath.Join(tmpDir, ".zsh_history")
	actualHistory := filepath.Join(tmpDir, ".agentpaas_test_history")
	t.Setenv("HOME", tmpDir)
	t.Setenv("HISTFILE", actualHistory)
	if err := os.WriteFile(historyFixture, []byte("agent secret set api-token\nagent run deploy\n"), 0o600); err != nil {
		t.Fatalf("write history fixture: %v", err)
	}
	if err := os.WriteFile(actualHistory, []byte("agent secret set api-token\n"), 0o600); err != nil {
		t.Fatalf("write actual history fixture: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null")
	cmd.Stdin = strings.NewReader(b7T03Sentinel())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stdin secret fixture command: %v: %s", err, out)
	}

	for _, path := range []string{historyFixture, actualHistory} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read history %s: %v", path, err)
		}
		assertNoB7T03Sentinel(t, path, string(data))
	}
}

func newB7T03Flow(t *testing.T) b7T03Flow {
	t.Helper()
	return newB7T03FlowForPolicy(t, newBrokeredPolicy("api.example.com", 443))
}

func newB7T03FlowForPolicy(t *testing.T, p *policy.Policy) b7T03Flow {
	t.Helper()
	store := NewFakeKeyStore()
	if err := store.Set(context.Background(), "api-token", []byte(b7T03Sentinel())); err != nil {
		t.Fatalf("set sentinel secret: %v", err)
	}
	return newB7T03FlowForStoreAndPolicy(t, store, p)
}

func newB7T03FlowForStore(t *testing.T, store SecretStore) b7T03Flow {
	t.Helper()
	return newB7T03FlowForStoreAndPolicy(t, store, newBrokeredPolicy("api.example.com", 443))
}

func newB7T03FlowForStoreAndPolicy(t *testing.T, store SecretStore, p *policy.Policy) b7T03Flow {
	t.Helper()
	auditSink := &recordingAuditSink{}
	return b7T03Flow{
		broker: newTestBroker(t, store, auditSink, p),
		audit:  auditSink,
		policy: p,
	}
}

func requestB7T03Credential(t *testing.T, flow b7T03Flow) CredentialInjection {
	t.Helper()
	return requestB7T03CredentialTo(t, flow, "https://api.example.com/v1")
}

func requestB7T03CredentialTo(t *testing.T, flow b7T03Flow, destination string) CredentialInjection {
	t.Helper()
	injection, err := flow.broker.RequestCredential(context.Background(), "run-active", "egress[0]", destination, http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if injection.HeaderValue != b7T03Sentinel() {
		t.Fatalf("HeaderValue = %q, want sentinel", injection.HeaderValue)
	}
	return injection
}

func writeB7T03SanitizedArtifacts(t *testing.T, dir string, flow b7T03Flow, injection CredentialInjection) {
	t.Helper()
	auditJSON, err := json.MarshalIndent(b7T03AuditRecords(t, flow.audit), "", "  ")
	if err != nil {
		t.Fatalf("marshal audit records: %v", err)
	}
	gatewayConfig, err := policy.CompileGatewayConfig(flow.policy)
	if err != nil {
		t.Fatalf("compile gateway config: %v", err)
	}
	credentialRules, err := policy.CompileCredentialRules(flow.policy)
	if err != nil {
		t.Fatalf("compile credential rules: %v", err)
	}
	files := map[string][]byte{
		"audit.json":           auditJSON,
		"gateway.yaml":         gatewayConfig,
		"credential_rules.yml": credentialRules,
		"injection.txt":        []byte(injection.String()),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("write sanitized artifact %s: %v", name, err)
		}
	}
}

func b7T03AuditRecords(t *testing.T, sink *recordingAuditSink) []audit.AuditRecord {
	t.Helper()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.records) == 0 {
		t.Fatal("expected at least one audit record")
	}
	records := make([]audit.AuditRecord, len(sink.records))
	copy(records, sink.records)
	return records
}

func assertProcFileNoB7T03Sentinel(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			t.Skipf("%s unavailable: %v", path, err)
		}
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	assertNoB7T03Sentinel(t, path, string(data))
}

func assertNoB7T03Sentinel(t *testing.T, surface, data string) {
	t.Helper()
	if strings.Contains(data, b7T03Sentinel()) {
		t.Fatalf("%s leaked brokered sentinel", surface)
	}
}
